package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	tfTypes "github.com/OpenRouterTeam/terraform-provider-openrouter/internal/provider/types"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/operations"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/shared"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &GuardrailPolicyAccountBudgetResource{}
var _ resource.ResourceWithImportState = &GuardrailPolicyAccountBudgetResource{}
var _ resource.ResourceWithConfigValidators = &GuardrailPolicyAccountBudgetResource{}

// NewGuardrailPolicyAccountBudgetResource declares the account (workspace-wide)
// budget policy: the layer that covers every workspace a policy is assigned to,
// including members and API keys that hold a policy of their own.
//
// Hand-written rather than generated because the route has no get-by-id, so the read is
// a list filtered on the policy id. `guardrailpolicyaccount.go` carries the shared
// route contract, and `docs/resources/guardrail_policy_account_budget.md` the user-facing
// behaviour.
func NewGuardrailPolicyAccountBudgetResource() resource.Resource {
	return &GuardrailPolicyAccountBudgetResource{}
}

// GuardrailPolicyAccountBudgetResource defines the resource implementation.
type GuardrailPolicyAccountBudgetResource struct {
	// Provider configured SDK client.
	client *sdk.OpenRouter
}

// GuardrailPolicyAccountBudgetResourceModel describes the resource data model.
type GuardrailPolicyAccountBudgetResourceModel struct {
	Config                   *tfTypes.GuardrailBudgetPolicyAuthoringConfig `tfsdk:"config"`
	ID                       types.String                                  `tfsdk:"id"`
	IsDefault                types.Bool                                    `tfsdk:"is_default"`
	IsSynced                 types.Bool                                    `tfsdk:"is_synced"`
	IsWorkspaceDefaultSynced types.Bool                                    `tfsdk:"is_workspace_default_synced"`
	Name                     types.String                                  `tfsdk:"name"`
	OptedOutWorkspaceIDs     []types.String                                `tfsdk:"opted_out_workspace_ids"`
	UpdatedAt                types.String                                  `tfsdk:"updated_at"`
	WorkspaceIDs             []types.String                                `tfsdk:"workspace_ids"`
}

func (r *GuardrailPolicyAccountBudgetResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_guardrail_policy_account_budget"
}

func (r *GuardrailPolicyAccountBudgetResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "An account (workspace-wide) budget policy: the spend limit every workspace it covers is measured against, and the account default when `is_default` is true.\n\nThis is the layer that binds members and API keys that hold a budget policy of their own, which the workspace default does not: a subject's own policy replaces the default it would otherwise inherit.\n\nA policy that is not the default covers exactly `workspace_ids`. The default covers every workspace except `opted_out_workspace_ids`, in which case `workspace_ids` must be left unset. Only one policy of a type can be the account default.\n\nImport takes the bare policy id.",
		Attributes: map[string]schema.Attribute{
			"config": generatedGuardrailPolicyConfigSchemaAttribute(ctx, NewGuardrailPolicyBudgetResource()),
			"id": schema.StringAttribute{
				Computed:    true,
				Description: `Policy ID`,
			},
			"is_default": schema.BoolAttribute{
				Computed:    true,
				Optional:    true,
				Description: `Cover every workspace in the account except ` + "`" + `opted_out_workspace_ids` + "`" + `. Set it to ` + "`" + `false` + "`" + ` to unset that status. Only one policy of a type can be the account default: declaring it on a second policy of the same type unsets it on the first and clears its coverage, so two policies that both declare it change on every run. Destroying a policy that holds this status is refused: the route deletes the account default with no complaint and cannot restore it, so a destroy fails and names the two ways forward.`,
			},
			"is_synced": schema.BoolAttribute{
				Computed:    true,
				Description: `Kept in sync with a v1 guardrail; saving changes creates an account copy`,
			},
			"is_workspace_default_synced": schema.BoolAttribute{
				Computed:    true,
				Description: `Synced from a v1 workspace default guardrail`,
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: `Policy name`,
				Validators: []validator.String{
					stringvalidator.UTF8LengthBetween(1, 255),
				},
			},
			"opted_out_workspace_ids": schema.ListAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.List{
					guardrailPolicyAccountOptOutsModifier{},
				},
				Optional:    true,
				ElementType: types.StringType,
				Description: `Workspaces left out of a default policy. Only the account default accepts these, and the API answers a policy that is not the default with 400 when it carries them, so flipping ` + "`" + `is_default` + "`" + ` off drops them: these require an effective ` + "`" + `is_default = true` + "`" + `, and a write that carries them while the flag is false or unset is refused.`,
			},
			"updated_at": schema.StringAttribute{
				Computed: true,
			},
			"workspace_ids": schema.ListAttribute{
				Computed:    true,
				Optional:    true,
				ElementType: types.StringType,
				PlanModifiers: []planmodifier.List{
					guardrailPolicyAccountWorkspaceIDsModifier{},
				},
				Description: `Workspaces a policy covers when it is not the default. For the account default this attribute reads back the workspaces currently resolving to it, which is more than what was configured, and the provider does not send it while ` + "`" + `is_default` + "`" + ` is true.`,
			},
		},
	}
}

// ConfigValidators refuses the two coverage combinations the route answers with 400, at
// plan time. It is config-level because each rule relates two attributes.
func (r *GuardrailPolicyAccountBudgetResource) ConfigValidators(ctx context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{
		guardrailPolicyAccountCoverageValidator{},
	}
}

func (r *GuardrailPolicyAccountBudgetResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	// Prevent panic if the provider has not been configured.
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*sdk.OpenRouter)

	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *sdk.OpenRouter, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)

		return
	}

	r.client = client
}

func (r *GuardrailPolicyAccountBudgetResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data *GuardrailPolicyAccountBudgetResourceModel
	var plan types.Object

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(plan.As(ctx, &data, basetypes.ObjectAsOptions{
		UnhandledNullAsEmpty:    true,
		UnhandledUnknownAsEmpty: true,
	})...)

	if resp.Diagnostics.HasError() {
		return
	}

	body, bodyDiags := data.toSharedAccountGuardrailPolicyRequest()
	resp.Diagnostics.Append(bodyDiags...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.client.GuardrailPolicies.CreateAccountPolicy(ctx, shared.CreateAccountGuardrailPolicyRequestBudget(*body))
	if err != nil {
		resp.Diagnostics.AddError("failure to invoke API", redactSensitiveValues(ctx, err.Error()))
		if res != nil && res.RawResponse != nil {
			resp.Diagnostics.AddError("unexpected http request/response", debugResponse(res.RawResponse))
		}
		return
	}
	if res == nil {
		resp.Diagnostics.AddError("unexpected response from API", fmt.Sprintf("%v", res))
		return
	}
	if res.StatusCode == 409 {
		resp.Diagnostics.AddError(
			"Resource Already Exists",
			"When creating this resource, the API indicated that this resource already exists. You can bring the existing resource under management using Terraform import functionality or retry with a unique configuration.",
		)
		return
	}
	if res.StatusCode != 201 {
		resp.Diagnostics.AddError(fmt.Sprintf("unexpected response from API. Got an unexpected response code %v", res.StatusCode),
			guardrailPolicyAccountResponseMessage(res.BadRequestResponse, res.ConflictResponse, res.NotFoundResponse, res.RawResponse))
		return
	}
	if res.AccountGuardrailPolicyResponse == nil {
		resp.Diagnostics.AddError("unexpected response from API. Got an unexpected response body", debugResponse(res.RawResponse))
		return
	}

	// The write response carries the new id and not the read model, so take the id from
	// it and read the policy back the way every other operation does.
	data.ID = types.StringValue(guardrailPolicyAccountID(res.AccountGuardrailPolicyResponse.Data))
	if data.ID.ValueString() == "" {
		resp.Diagnostics.AddError("unexpected response from API", "the created policy carried no id")
		return
	}

	policy, found, lookupDiags := guardrailPolicyAccountLookup(ctx, r.client, guardrailPolicyAccountTypeBudget, data.ID.ValueString())
	resp.Diagnostics.Append(lookupDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		resp.Diagnostics.AddError(
			"the policy is not in the account policy list after it was created",
			"The API answered the create with a policy id that the account policy list does not return. The policy exists; re-running the plan should read it.",
		)
		return
	}

	row := data.accountPolicyVariant(*policy)
	if row == nil {
		resp.Diagnostics.AddError("unexpected response from API", "the account policy list returned a policy of a type this resource cannot read")
		return
	}

	resp.Diagnostics.Append(data.refreshFromRow(ctx, row)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(refreshPlan(ctx, plan, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *GuardrailPolicyAccountBudgetResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data *GuardrailPolicyAccountBudgetResourceModel
	var item types.Object

	resp.Diagnostics.Append(req.State.Get(ctx, &item)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(item.As(ctx, &data, basetypes.ObjectAsOptions{
		UnhandledNullAsEmpty:    true,
		UnhandledUnknownAsEmpty: true,
	})...)

	if resp.Diagnostics.HasError() {
		return
	}

	policy, found, lookupDiags := guardrailPolicyAccountLookup(ctx, r.client, guardrailPolicyAccountTypeBudget, data.ID.ValueString())
	resp.Diagnostics.Append(lookupDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		// An account policy list that returns in full without the id is the only
		// absence signal these routes give: there is no get-by-id to answer 404 for
		// a single policy. A route that is unavailable answers non-200 above and
		// reaches this as an error rather than as absence.
		resp.State.RemoveResource(ctx)
		return
	}

	row := data.accountPolicyVariant(*policy)
	if row == nil {
		resp.Diagnostics.AddError("unexpected response from API", "the account policy list returned a policy of a type this resource cannot read")
		return
	}

	resp.Diagnostics.Append(data.refreshFromRow(ctx, row)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *GuardrailPolicyAccountBudgetResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data *GuardrailPolicyAccountBudgetResourceModel
	var plan types.Object

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	merge(ctx, req, resp, &data)
	if resp.Diagnostics.HasError() {
		return
	}

	body, bodyDiags := data.toSharedAccountGuardrailPolicyRequest()
	resp.Diagnostics.Append(bodyDiags...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.client.GuardrailPolicies.UpdateAccountPolicy(ctx, operations.UpdateAccountPolicyRequest{
		ID:   data.ID.ValueString(),
		Body: shared.CreateAccountGuardrailPolicyRequestBudget(*body),
	})
	if err != nil {
		resp.Diagnostics.AddError("failure to invoke API", redactSensitiveValues(ctx, err.Error()))
		if res != nil && res.RawResponse != nil {
			resp.Diagnostics.AddError("unexpected http request/response", debugResponse(res.RawResponse))
		}
		return
	}
	if res == nil {
		resp.Diagnostics.AddError("unexpected response from API", fmt.Sprintf("%v", res))
		return
	}
	if res.StatusCode != 200 {
		resp.Diagnostics.AddError(fmt.Sprintf("unexpected response from API. Got an unexpected response code %v", res.StatusCode),
			guardrailPolicyAccountResponseMessage(res.BadRequestResponse, res.ConflictResponse, res.NotFoundResponse, res.RawResponse))
		return
	}
	if res.AccountGuardrailPolicyResponse == nil {
		resp.Diagnostics.AddError("unexpected response from API. Got an unexpected response body", debugResponse(res.RawResponse))
		return
	}

	// See Create: the write response is not the read model, and for an update the id is
	// already the one state holds.
	policy, found, lookupDiags := guardrailPolicyAccountLookup(ctx, r.client, guardrailPolicyAccountTypeBudget, data.ID.ValueString())
	resp.Diagnostics.Append(lookupDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		resp.Diagnostics.AddError(
			"the policy is not in the account policy list after it was updated",
			"The API answered the update with a policy id that the account policy list does not return. Re-running the plan should read it.",
		)
		return
	}

	row := data.accountPolicyVariant(*policy)
	if row == nil {
		resp.Diagnostics.AddError("unexpected response from API", "the account policy list returned a policy of a type this resource cannot read")
		return
	}

	resp.Diagnostics.Append(data.refreshFromRow(ctx, row)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(refreshPlan(ctx, plan, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *GuardrailPolicyAccountBudgetResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data *GuardrailPolicyAccountBudgetResourceModel
	var item types.Object

	resp.Diagnostics.Append(req.State.Get(ctx, &item)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(item.As(ctx, &data, basetypes.ObjectAsOptions{
		UnhandledNullAsEmpty:    true,
		UnhandledUnknownAsEmpty: true,
	})...)

	if resp.Diagnostics.HasError() {
		return
	}

	// The destroy of the account default is refused, not sent and not silently skipped.
	// Measured 2026-11-18: DELETE answers 200 for a policy that holds the default flag,
	// with no refusal, and there is no undelete, so sending it would release the baseline
	// for every workspace that resolves to the policy with no way back. Skipping it is
	// the other failure: a destroy that reports success while the policy still enforces
	// hands back a resource nobody manages, and writing the configuration again creates a
	// second policy rather than adopting that one. An error keeps the resource in state
	// and lets the operator choose, and the two paths are named in the message.
	if data.IsDefault.ValueBool() {
		resp.Diagnostics.AddAttributeError(
			path.Root("is_default"),
			"refusing to destroy the account default",
			"This policy is the account default for its type. The API deletes it with no complaint and cannot restore it, and every workspace that resolves to it would lose this policy, so the provider refuses. To remove it: set `is_default = false`, apply, then destroy. To leave it enforcing and stop managing it here: `terraform state rm` on this resource address.",
		)
		return
	}

	res, err := r.client.GuardrailPolicies.DeleteAccountPolicy(ctx, operations.DeleteAccountPolicyRequest{
		ID: data.ID.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("failure to invoke API", redactSensitiveValues(ctx, err.Error()))
		if res != nil && res.RawResponse != nil {
			resp.Diagnostics.AddError("unexpected http request/response", debugResponse(res.RawResponse))
		}
		return
	}
	if res == nil {
		resp.Diagnostics.AddError("unexpected response from API", fmt.Sprintf("%v", res))
		return
	}
	// A 404 is success, the way it is in the three generated workspace-scoped siblings (the
	// same `case 200, 404` appears in each): a delete of a policy that is already gone is
	// satisfied, and re-running the destroy after an out-of-band removal cannot fail. An
	// absence becomes state removal in the read, which is where it is reported.
	switch res.StatusCode {
	case 200, 404:
		break
	default:
		resp.Diagnostics.AddError(fmt.Sprintf("unexpected response from API. Got an unexpected response code %v", res.StatusCode),
			guardrailPolicyAccountResponseMessage(res.BadRequestResponse, res.ConflictResponse, res.NotFoundResponse, res.RawResponse))
		return
	}
}

// ImportState takes the bare policy id. The list read is account-wide, so unlike the
// workspace-scoped policy resources this family needs no workspace prefix, and the
// resource's own type supplies the `policy_type` the route resolves the id by.
func (r *GuardrailPolicyAccountBudgetResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID == "" {
		resp.Diagnostics.AddError("invalid import ID", "expected a policy id, got an empty string")
		return
	}
	if strings.Contains(req.ID, "/") {
		resp.Diagnostics.AddError(
			"invalid import ID",
			fmt.Sprintf("expected a bare policy id, got %q. An account policy is not scoped to a workspace, so this resource's import identifier carries no workspace prefix.", req.ID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

// accountPolicyVariant unwraps the union the list read returns. A nil result means the
// row carries another policy type; the read already refused that case, so this is a
// guard for the write paths rather than a user-facing branch.
func (r *GuardrailPolicyAccountBudgetResourceModel) accountPolicyVariant(policy shared.AccountGuardrailPolicy) *shared.BudgetAccountGuardrailPolicy {
	if policy.Type != shared.AccountGuardrailPolicyTypeBudget {
		return nil
	}
	return policy.BudgetAccountGuardrailPolicy
}

// refreshFromRow hydrates the attributes the list returns. `id` is deliberately not taken
// from here: the identifier that resolved the row is the one state keeps.
func (r *GuardrailPolicyAccountBudgetResourceModel) refreshFromRow(ctx context.Context, resp *shared.BudgetAccountGuardrailPolicy) diag.Diagnostics {
	var diags diag.Diagnostics

	if resp == nil {
		diags.AddError("unexpected response from API", "the account policy row was absent, so there is nothing to read")
		return diags
	}

	r.IsDefault = types.BoolValue(resp.IsDefault)
	r.IsSynced = types.BoolValue(resp.IsSynced)
	r.IsWorkspaceDefaultSynced = types.BoolValue(resp.IsWorkspaceDefaultSynced)
	r.Name = types.StringValue(resp.Name)
	r.UpdatedAt = types.StringPointerValue(resp.UpdatedAt)

	r.WorkspaceIDs = make([]types.String, 0, len(resp.WorkspaceIds))
	for _, v := range resp.WorkspaceIds {
		r.WorkspaceIDs = append(r.WorkspaceIDs, types.StringValue(v))
	}
	r.OptedOutWorkspaceIDs = make([]types.String, 0, len(resp.OptedOutWorkspaceIds))
	for _, v := range resp.OptedOutWorkspaceIds {
		r.OptedOutWorkspaceIDs = append(r.OptedOutWorkspaceIDs, types.StringValue(v))
	}

	config, configDiags := flattenGuardrailBudgetConfig(&resp.Config)
	diags.Append(configDiags...)
	if diags.HasError() {
		// Reachable from Create as well as Read. A remote policy exists by the time Create gets here,
		// so if this ever fires the error has to name that policy's id rather than leave it to be found.
		return diags
	}
	r.Config = config

	return diags
}

// toSharedAccountGuardrailPolicyRequest renders the write body for both create and
// update.
//
// The route replaces a policy and its coverage wholesale, so every field travels on
// every write and an omitted field is a clear rather than a no-op: a PUT whose body
// carried no `workspace_ids` left the policy covering no workspaces at all, measured
// 2026-11-18. The two exclusivity rules the route enforces are applied here as well as at
// plan time, so a merged state holding a stale value cannot reach it:
//
//   - while `is_default` is true, `workspace_ids` is not sent. The route answers that
//     combination with 400 "The account default covers workspaces through opt-outs",
//     and for a default the read's `workspace_ids` is resolved coverage rather than
//     configuration.
//   - while the effective `is_default` is false, a non-empty `opted_out_workspace_ids`
//     is refused instead of sent or dropped. The route answers that combination with 400
//     "Only the account default can have default assignment fields", and silently
//     omitting the list would make the configuration describe coverage that no apply
//     stores, so the write fails and says which flag it needs.
//
// `is_default` itself always travels, as an explicit true or false, because the route
// treats an absent value as no change while the provider's state always knows which one
// the configuration asked for.
func (r *GuardrailPolicyAccountBudgetResourceModel) toSharedAccountGuardrailPolicyRequest() (*shared.BudgetAccountGuardrailPolicyRequest, diag.Diagnostics) {
	var diags diag.Diagnostics

	config, configDiags := expandGuardrailBudgetConfig(r.Config)
	diags.Append(configDiags...)
	if diags.HasError() {
		return nil, diags
	}

	body := &shared.BudgetAccountGuardrailPolicyRequest{
		Config: config,
		Name:   r.Name.ValueString(),
	}

	isDefault := r.IsDefault.ValueBool()
	body.IsDefault = &isDefault

	if isDefault {
		body.OptedOutWorkspaceIds = guardrailPolicyAccountValueStrings(r.OptedOutWorkspaceIDs)
		return body, diags
	}

	// `is_default` is Optional+Computed, so a configuration that omits it arrives here
	// null or unknown and becomes false on the wire, which is what the route does with an
	// absent flag too. Opt-outs configured while the effective flag is false are refused
	// here rather than dropped: it is the same combination the route answers with "Only
	// the account default can have default assignment fields", and dropping them would
	// leave a configuration describing coverage no apply can store, because the read
	// returns the empty list the route actually holds. An unresolved list counts as
	// configured here: this path cannot tell it from a non-empty one, and dropping it would
	// be the same silence. A list carried from state never arrives, because the plan
	// modifier pins it to empty when the configuration does not write it.
	if len(r.OptedOutWorkspaceIDs) > 0 {
		diags.AddAttributeError(
			path.Root("opted_out_workspace_ids"),
			"Only the account default can have default assignment fields",
			"`opted_out_workspace_ids` describes the account default's coverage, so it requires an effective `is_default = true`. This configuration leaves the flag unset or false, which the route answers with 400 rather than storing these workspaces, so the provider refuses instead of dropping them.",
		)
		return body, diags
	}

	body.WorkspaceIds = guardrailPolicyAccountValueStrings(r.WorkspaceIDs)

	return body, diags
}
