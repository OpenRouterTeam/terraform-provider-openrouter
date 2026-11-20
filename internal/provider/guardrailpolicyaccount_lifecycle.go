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

	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/operations"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/shared"
)

// The account (workspace-wide) policy family has three resources whose five lifecycle
// operations are one operation against one route, differing only in the generated types
// they name. This file carries everything they share: the model, the schema, the
// hydration, the write rules and the operations themselves.
//
// `guardrailpolicyaccount.go` carries the route contract. The three
// `guardrailpolicyaccount*_resource.go` files carry what genuinely differs, which is the
// resource name and schema description, the generated types, and the six functions in the
// descriptor below. A rule stated here is stated once; every review round on this family
// so far has found a rule applied to one member of three.

// guardrailPolicyAccountModel is the data model all three resources publish. Its only
// per-family part is the authoring config type, so the three resource files alias it
// rather than each declaring the same nine fields.
type guardrailPolicyAccountModel[C any] struct {
	Config                   *C             `tfsdk:"config"`
	ID                       types.String   `tfsdk:"id"`
	IsDefault                types.Bool     `tfsdk:"is_default"`
	IsSynced                 types.Bool     `tfsdk:"is_synced"`
	IsWorkspaceDefaultSynced types.Bool     `tfsdk:"is_workspace_default_synced"`
	Name                     types.String   `tfsdk:"name"`
	OptedOutWorkspaceIDs     []types.String `tfsdk:"opted_out_workspace_ids"`
	UpdatedAt                types.String   `tfsdk:"updated_at"`
	WorkspaceIDs             []types.String `tfsdk:"workspace_ids"`
}

// guardrailPolicyAccountRowFields is the scalar half of a row. Every family's row carries
// these under the same names, but each row is a generated type of its own, and Go cannot
// read a field by name across types, so a family hands them over through this struct.
// Everything done with them is shared.
type guardrailPolicyAccountRowFields struct {
	IsDefault                bool
	IsSynced                 bool
	IsWorkspaceDefaultSynced bool
	Name                     string
	UpdatedAt                *string
	WorkspaceIDs             []string
	OptedOutWorkspaceIDs     []string
}

// guardrailPolicyAccountCoverage is what a write body carries besides the config: the name
// and the two exclusivity rules' inputs, which are identical across the three families.
type guardrailPolicyAccountCoverage struct {
	Name                 string
	IsDefault            bool
	OptedOutWorkspaceIDs []string
	WorkspaceIDs         []string
}

// guardrailPolicyAccountOps is the per-family half of the lifecycle: the policy type the
// routes resolve an id by, and five mappings between the family's generated types and this
// file's model. The descriptor is parameterized on the family's three types, so a family
// that wires a mapping to the wrong one fails to compile rather than at runtime.
type guardrailPolicyAccountOps[C any, A any, R any] struct {
	// policyType is the value the routes resolve a policy id by. A row found under another
	// type is a mismatched import rather than this resource's policy, and
	// guardrailPolicyAccountLookup reports it.
	policyType string

	// variant unwraps the union row the list and the write responses carry.
	variant func(shared.AccountGuardrailPolicy) *R

	// fields reads the scalars this file hydrates from.
	fields func(*R) guardrailPolicyAccountRowFields

	// flatten converts a row's config into the model's authoring config.
	flatten func(*R) (*C, diag.Diagnostics)

	// expand converts the model's authoring config into the config the write body takes.
	expand func(*C) (A, diag.Diagnostics)

	// wrap builds the union body both write routes take from the expanded config and the
	// coverage this file computed.
	wrap func(A, guardrailPolicyAccountCoverage) shared.AccountGuardrailPolicyRequest
}

// guardrailPolicyAccountUnexpectedRow is the diagnostic for a union row that carries
// another family's type. The read refuses that case by type through the lookup, so this is
// a guard for the write responses rather than a user-facing branch.
func guardrailPolicyAccountUnexpectedRow() diag.Diagnostics {
	var diags diag.Diagnostics
	diags.AddError("unexpected response from API", "the account policy carried a type this resource cannot read")
	return diags
}

// guardrailPolicyAccountMissingObject is the diagnostic for an absent plan or state. It
// replaces what would otherwise be a nil dereference; the framework does not hand these
// operations an absent object, so it is unreachable in practice.
func guardrailPolicyAccountMissingObject() diag.Diagnostics {
	var diags diag.Diagnostics
	diags.AddError(
		"unexpected empty object from the framework",
		"The plan or prior state this operation was given was absent, so there was nothing to read or write. This is always an error in the provider; please report it.",
	)
	return diags
}

// guardrailPolicyAccountSchema builds the schema all three resources publish. The attribute
// map is identical across the family; only the description and the `config` attribute
// differ, and the attribute is built from each family's own generated workspace-scoped
// resource, so both are passed in.
func guardrailPolicyAccountSchema(configAttribute schema.Attribute, markdownDescription string) schema.Schema {
	return schema.Schema{
		MarkdownDescription: markdownDescription,
		Attributes: map[string]schema.Attribute{
			"config": configAttribute,
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

// guardrailPolicyAccountHydrate copies a union row onto the model. `id` is deliberately not
// taken from here: the identifier that resolved the row is the one state keeps.
func guardrailPolicyAccountHydrate[C any, A any, R any](model *guardrailPolicyAccountModel[C], policy shared.AccountGuardrailPolicy, ops guardrailPolicyAccountOps[C, A, R]) diag.Diagnostics {
	row := ops.variant(policy)
	if row == nil {
		return guardrailPolicyAccountUnexpectedRow()
	}

	fields := ops.fields(row)

	model.IsDefault = types.BoolValue(fields.IsDefault)
	model.IsSynced = types.BoolValue(fields.IsSynced)
	model.IsWorkspaceDefaultSynced = types.BoolValue(fields.IsWorkspaceDefaultSynced)
	model.Name = types.StringValue(fields.Name)
	model.UpdatedAt = types.StringPointerValue(fields.UpdatedAt)

	model.WorkspaceIDs = make([]types.String, 0, len(fields.WorkspaceIDs))
	for _, v := range fields.WorkspaceIDs {
		model.WorkspaceIDs = append(model.WorkspaceIDs, types.StringValue(v))
	}
	model.OptedOutWorkspaceIDs = make([]types.String, 0, len(fields.OptedOutWorkspaceIDs))
	for _, v := range fields.OptedOutWorkspaceIDs {
		model.OptedOutWorkspaceIDs = append(model.OptedOutWorkspaceIDs, types.StringValue(v))
	}

	config, configDiags := ops.flatten(row)
	// Reachable from a write as well as from the read: a remote policy exists by the time a
	// write gets here, so if this ever fires the error has to name that policy's id rather
	// than leave it to be found.
	if configDiags.HasError() {
		return configDiags
	}
	model.Config = config

	return configDiags
}

// guardrailPolicyAccountWriteBody renders the body both write routes take.
//
// The route replaces a policy and its coverage wholesale, so every field travels on every
// write and an omitted field is a clear rather than a no-op: a PUT whose body carried no
// `workspace_ids` left the policy covering no workspaces at all, measured 2026-11-18. The
// two exclusivity rules the route enforces are applied here as well as at plan time, so a
// merged state holding a stale value cannot reach it:
//
//   - while `is_default` is true, `workspace_ids` is not sent. The route answers that
//     combination with 400 "The account default covers workspaces through opt-outs", and for
//     a default the read's `workspace_ids` is resolved coverage rather than configuration.
//   - while the effective `is_default` is false, a non-empty `opted_out_workspace_ids` is
//     refused instead of sent or dropped. The route answers that combination with 400 "Only
//     the account default can have default assignment fields", and silently omitting the
//     list would make the configuration describe coverage that no apply stores, so the write
//     fails and says which flag it needs.
//
// `is_default` itself always travels, as an explicit true or false, because the route treats
// an absent value as no change while the provider's state always knows which one the
// configuration asked for.
func guardrailPolicyAccountWriteBody[C any, A any, R any](model *guardrailPolicyAccountModel[C], ops guardrailPolicyAccountOps[C, A, R]) (shared.AccountGuardrailPolicyRequest, diag.Diagnostics) {
	var diags diag.Diagnostics

	config, configDiags := ops.expand(model.Config)
	diags.Append(configDiags...)
	if diags.HasError() {
		return shared.AccountGuardrailPolicyRequest{}, diags
	}

	coverage := guardrailPolicyAccountCoverage{
		Name:      model.Name.ValueString(),
		IsDefault: model.IsDefault.ValueBool(),
	}

	if coverage.IsDefault {
		coverage.OptedOutWorkspaceIDs = guardrailPolicyAccountValueStrings(model.OptedOutWorkspaceIDs)
		return ops.wrap(config, coverage), diags
	}

	// `is_default` is Optional+Computed, so a configuration that omits it arrives here null or
	// unknown and becomes false on the wire, which is what the route does with an absent flag
	// too. Opt-outs configured while the effective flag is false are refused here rather than
	// dropped: it is the same combination the route answers with "Only the account default can
	// have default assignment fields", and dropping them would leave a configuration
	// describing coverage no apply can store, because the read returns the empty list the
	// route actually holds. The check sees a known list or nothing: this path decodes the plan
	// with UnhandledUnknownAsEmpty, so a list that is still unresolved arrives as empty and is
	// not refused here. A list carried from state never arrives, because the plan modifier pins
	// it to empty when the configuration does not write it.
	if len(model.OptedOutWorkspaceIDs) > 0 {
		diags.AddAttributeError(
			path.Root("opted_out_workspace_ids"),
			"Only the account default can have default assignment fields",
			"`opted_out_workspace_ids` describes the account default's coverage, so it requires an effective `is_default = true`. This configuration leaves the flag unset or false, which the route answers with 400 rather than storing these workspaces, so the provider refuses instead of dropping them.",
		)
		return shared.AccountGuardrailPolicyRequest{}, diags
	}

	coverage.WorkspaceIDs = guardrailPolicyAccountValueStrings(model.WorkspaceIDs)
	return ops.wrap(config, coverage), diags
}

// guardrailPolicyAccountBase is what every resource in the family embeds. It carries the
// client and the descriptor and implements the seven methods that are the same for all
// three, so each resource file declares only its name, its description and its types. The
// three public resource types stay concrete and greppable, and the provider's registry
// keeps constructing them through three New…Resource functions.
type guardrailPolicyAccountBase[C any, A any, R any] struct {
	// Provider configured SDK client.
	client *sdk.OpenRouter

	ops guardrailPolicyAccountOps[C, A, R]
}

func (b *guardrailPolicyAccountBase[C, A, R]) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

	b.client = client
}

// ConfigValidators refuses the two coverage combinations the route answers with 400, at plan
// time. It is config-level because each rule relates two attributes.
func (b *guardrailPolicyAccountBase[C, A, R]) ConfigValidators(ctx context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{
		guardrailPolicyAccountCoverageValidator{},
	}
}

func (b *guardrailPolicyAccountBase[C, A, R]) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data *guardrailPolicyAccountModel[C]
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
	if data == nil {
		resp.Diagnostics.Append(guardrailPolicyAccountMissingObject()...)
		return
	}

	body, bodyDiags := guardrailPolicyAccountWriteBody(data, b.ops)
	resp.Diagnostics.Append(bodyDiags...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := b.client.GuardrailPolicies.CreateAccountPolicy(ctx, body)
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

	// The write response carries the whole row, not only the id, so state comes from the
	// response and no list read follows a write. That removes the window in which a read
	// issued milliseconds after a write could miss the row just created. Measured on the live
	// route 2026-11-20 with a non-default budget policy: the 201 answer's field set is
	// identical to that policy's row in the list, twelve keys on both sides, and its config
	// came back byte-equal to what was sent. The default case is measured on the update path,
	// where the response carries the resolved workspace projection.
	data.ID = types.StringValue(guardrailPolicyAccountID(res.AccountGuardrailPolicyResponse.Data))
	if data.ID.ValueString() == "" {
		resp.Diagnostics.AddError("unexpected response from API", "the created policy carried no id")
		return
	}

	resp.Diagnostics.Append(guardrailPolicyAccountHydrate(data, res.AccountGuardrailPolicyResponse.Data, b.ops)...)
	if resp.Diagnostics.HasError() {
		// The policy exists remotely by now, so a failure here has to name it: without the id
		// in the error nobody can import the policy that was just created.
		resp.Diagnostics.AddError(
			"the policy was created but could not be read back into state",
			fmt.Sprintf("Policy %s exists in the account and is not in state. Once the error above is resolved, import it by that id.", data.ID.ValueString()),
		)
		return
	}

	resp.Diagnostics.Append(refreshPlan(ctx, plan, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (b *guardrailPolicyAccountBase[C, A, R]) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data *guardrailPolicyAccountModel[C]
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
	if data == nil {
		resp.Diagnostics.Append(guardrailPolicyAccountMissingObject()...)
		return
	}

	policy, found, lookupDiags := guardrailPolicyAccountLookup(ctx, b.client, b.ops.policyType, data.ID.ValueString())
	resp.Diagnostics.Append(lookupDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		// An account policy list that returns in full without the id is the only absence
		// signal these routes give: there is no get-by-id to answer 404 for a single policy.
		// A route that is unavailable answers non-200 above and reaches this as an error
		// rather than as absence.
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(guardrailPolicyAccountHydrate(data, *policy, b.ops)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (b *guardrailPolicyAccountBase[C, A, R]) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data *guardrailPolicyAccountModel[C]
	var plan types.Object

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	merge(ctx, req, resp, &data)
	if resp.Diagnostics.HasError() {
		return
	}
	if data == nil {
		resp.Diagnostics.Append(guardrailPolicyAccountMissingObject()...)
		return
	}

	body, bodyDiags := guardrailPolicyAccountWriteBody(data, b.ops)
	resp.Diagnostics.Append(bodyDiags...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := b.client.GuardrailPolicies.UpdateAccountPolicy(ctx, operations.UpdateAccountPolicyRequest{
		ID:   data.ID.ValueString(),
		Body: body,
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

	// See Create: the write response carries the whole row, so an update takes its state from
	// it rather than re-reading the list. Measured on the live route 2026-11-20: a non-default
	// budget policy's PUT answered with the same field set the list returns and an updated_at
	// that had advanced while created_at held, and the account's budget default answered with
	// the same resolved workspace_ids projection the list gives it, the same four ids and
	// workspace_count 4 on both sides. So a default policy needs no special case here. The id
	// is already the one state holds.
	resp.Diagnostics.Append(guardrailPolicyAccountHydrate(data, res.AccountGuardrailPolicyResponse.Data, b.ops)...)
	if resp.Diagnostics.HasError() {
		// Lower stakes than the create path, because state already holds the id, but the same
		// rule applies: a failure here has to name the policy it is about.
		resp.Diagnostics.AddError(
			"the policy was updated but could not be read back into state",
			fmt.Sprintf("Policy %s exists in the account with the update applied. Once the error above is resolved, import it by that id.", data.ID.ValueString()),
		)
		return
	}

	resp.Diagnostics.Append(refreshPlan(ctx, plan, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (b *guardrailPolicyAccountBase[C, A, R]) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data *guardrailPolicyAccountModel[C]
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
	if data == nil {
		resp.Diagnostics.Append(guardrailPolicyAccountMissingObject()...)
		return
	}

	// The destroy of the account default is refused, not sent and not silently skipped.
	// Measured 2026-11-18: DELETE answers 200 for a policy that holds the default flag, with
	// no refusal, and there is no undelete, so sending it would release the baseline for every
	// workspace that resolves to the policy with no way back. Skipping it is the other
	// failure: a destroy that reports success while the policy still enforces hands back a
	// resource nobody manages, and writing the configuration again creates a second policy
	// rather than adopting that one. An error keeps the resource in state and lets the
	// operator choose, and the two paths are named in the message.
	if data.IsDefault.ValueBool() {
		resp.Diagnostics.AddAttributeError(
			path.Root("is_default"),
			"refusing to destroy the account default",
			"This policy is the account default for its type. The API deletes it with no complaint and cannot restore it, and every workspace that resolves to it would lose this policy, so the provider refuses. To remove it: set `is_default = false`, apply, then destroy. To leave it enforcing and stop managing it here: `terraform state rm` on this resource address.",
		)
		return
	}

	res, err := b.client.GuardrailPolicies.DeleteAccountPolicy(ctx, operations.DeleteAccountPolicyRequest{
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

// guardrailPolicyAccountImportState takes the bare policy id. The list read is account-wide,
// so unlike the workspace-scoped policy resources this family needs no workspace prefix, and
// the resource's own type supplies the `policy_type` the route resolves the id by.
func (b *guardrailPolicyAccountBase[C, A, R]) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
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
