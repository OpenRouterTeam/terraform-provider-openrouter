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
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	tfTypes "github.com/OpenRouterTeam/terraform-provider-openrouter/internal/provider/types"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/operations"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/shared"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &GuardrailPolicyAccountModelAvailabilityResource{}
var _ resource.ResourceWithImportState = &GuardrailPolicyAccountModelAvailabilityResource{}
var _ resource.ResourceWithConfigValidators = &GuardrailPolicyAccountModelAvailabilityResource{}

// NewGuardrailPolicyAccountModelAvailabilityResource declares the account (workspace-wide)
// model availability policy: the layer that covers every workspace a policy is assigned to,
// including members and API keys that hold a policy of their own.
//
// Hand-written rather than generated because the route has no get-by-id, so the read is
// a list filtered on the policy id. `guardrailpolicyaccount.go` carries the shared
// route contract, and `docs/resources/guardrail_policy_account_model_availability.md` the user-facing
// behaviour.
func NewGuardrailPolicyAccountModelAvailabilityResource() resource.Resource {
	return &GuardrailPolicyAccountModelAvailabilityResource{}
}

// GuardrailPolicyAccountModelAvailabilityResource defines the resource implementation.
type GuardrailPolicyAccountModelAvailabilityResource struct {
	// Provider configured SDK client.
	client *sdk.OpenRouter
}

// GuardrailPolicyAccountModelAvailabilityResourceModel describes the resource data model.
type GuardrailPolicyAccountModelAvailabilityResourceModel struct {
	Config                   *tfTypes.GuardrailModelAvailabilityPolicyAuthoringConfig `tfsdk:"config"`
	ID                       types.String                                             `tfsdk:"id"`
	IsDefault                types.Bool                                               `tfsdk:"is_default"`
	IsSynced                 types.Bool                                               `tfsdk:"is_synced"`
	IsWorkspaceDefaultSynced types.Bool                                               `tfsdk:"is_workspace_default_synced"`
	Name                     types.String                                             `tfsdk:"name"`
	OptedOutWorkspaceIDs     []types.String                                           `tfsdk:"opted_out_workspace_ids"`
	UpdatedAt                types.String                                             `tfsdk:"updated_at"`
	WorkspaceIDs             []types.String                                           `tfsdk:"workspace_ids"`
}

func (r *GuardrailPolicyAccountModelAvailabilityResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_guardrail_policy_account_model_availability"
}

func (r *GuardrailPolicyAccountModelAvailabilityResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "An account (workspace-wide) model-availability policy: which models, providers and data regions the covered workspaces may use, plus ZDR enforcement and the training and publication toggles. It is what an account-wide baseline belongs on, because it binds members and API keys that hold a model-availability policy of their own.\n\nA policy that is not the default covers exactly `workspace_ids`. The default covers every workspace except `opted_out_workspace_ids`, in which case `workspace_ids` must be left unset. Only one policy of a type can be the account default. A workspace that holds its own workspace-wide policy of this type does not inherit the account default at all.\n\nImport takes the bare policy id.",
		Attributes: map[string]schema.Attribute{
			"config": schema.SingleNestedAttribute{
				Required: true,
				Attributes: map[string]schema.Attribute{
					"allow_safety_retention_google": schema.BoolAttribute{
						Computed: true,
						Optional: true,
					},
					"allow_safety_retention_other": schema.BoolAttribute{
						Computed: true,
						Optional: true,
					},
					"allowed_data_regions": schema.ListAttribute{
						Computed:    true,
						Optional:    true,
						ElementType: types.StringType,
						Validators: []validator.List{
							listvalidator.SizeAtLeast(1),
						},
					},
					"allowed_models": schema.ListAttribute{
						Computed:    true,
						Optional:    true,
						ElementType: types.StringType,
					},
					"allowed_providers": schema.ListAttribute{
						Computed:    true,
						Optional:    true,
						ElementType: types.StringType,
					},
					"enable_free_model_publication": schema.BoolAttribute{
						Computed: true,
						Optional: true,
					},
					"enable_free_model_training": schema.BoolAttribute{
						Computed: true,
						Optional: true,
					},
					"enable_paid_model_training": schema.BoolAttribute{
						Computed: true,
						Optional: true,
					},
					"enforce_first_party_harness_models": schema.BoolAttribute{
						Computed: true,
						Optional: true,
					},
					"enforce_zdr": schema.BoolAttribute{
						Computed: true,
						Optional: true,
					},
					"enforce_zdr_anthropic": schema.BoolAttribute{
						Computed: true,
						Optional: true,
					},
					"enforce_zdr_google": schema.BoolAttribute{
						Computed: true,
						Optional: true,
					},
					"enforce_zdr_openai": schema.BoolAttribute{
						Computed: true,
						Optional: true,
					},
					"enforce_zdr_other": schema.BoolAttribute{
						Computed: true,
						Optional: true,
					},
					"enforce_zdr_xai": schema.BoolAttribute{
						Computed: true,
						Optional: true,
					},
					"ignored_models": schema.ListAttribute{
						Computed:    true,
						Optional:    true,
						ElementType: types.StringType,
					},
					"ignored_providers": schema.ListAttribute{
						Computed:    true,
						Optional:    true,
						ElementType: types.StringType,
					},
					"platform_banned_authors": schema.ListAttribute{
						Computed:    true,
						Optional:    true,
						ElementType: types.StringType,
					},
					"platform_banned_models": schema.ListAttribute{
						Computed:    true,
						Optional:    true,
						ElementType: types.StringType,
					},
					"platform_banned_providers": schema.ListAttribute{
						Computed:    true,
						Optional:    true,
						ElementType: types.StringType,
					},
					"require_eu_data_region": schema.BoolAttribute{
						Computed: true,
						Optional: true,
					},
				},
			},
			"id": schema.StringAttribute{
				Computed:    true,
				Description: `Policy ID`,
			},
			"is_default": schema.BoolAttribute{
				Computed:    true,
				Optional:    true,
				Description: `Cover every workspace in the account except ` + "`" + `opted_out_workspace_ids` + "`" + `. Set it to ` + "`" + `false` + "`" + ` to unset that status. Only one policy of a type can be the account default: declaring it on a second policy of the same type unsets it on the first and clears its coverage, so two policies that both declare it change on every run. Destroying a policy that holds this status removes it from state without deleting it, because the route would release the baseline for every workspace that resolves to it.`,
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
				Computed:    true,
				Optional:    true,
				ElementType: types.StringType,
				Description: `Workspaces left out of a default policy. Only the account default accepts these, and the API answers a policy that is not the default with 400 when it carries them, so flipping ` + "`" + `is_default` + "`" + ` off drops them.`,
			},
			"updated_at": schema.StringAttribute{
				Computed: true,
			},
			"workspace_ids": schema.ListAttribute{
				Computed:    true,
				Optional:    true,
				ElementType: types.StringType,
				Description: `Workspaces a policy covers when it is not the default. For the account default this attribute reads back the workspaces currently resolving to it, which is more than what was configured, and the provider does not send it while ` + "`" + `is_default` + "`" + ` is true.`,
			},
		},
	}
}

// ConfigValidators refuses the two coverage combinations the route answers with 400, at
// plan time. It is config-level because each rule relates two attributes.
func (r *GuardrailPolicyAccountModelAvailabilityResource) ConfigValidators(ctx context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{
		guardrailPolicyAccountCoverageValidator{},
	}
}

func (r *GuardrailPolicyAccountModelAvailabilityResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *GuardrailPolicyAccountModelAvailabilityResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data *GuardrailPolicyAccountModelAvailabilityResourceModel
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

	res, err := r.client.GuardrailPolicies.CreateAccountPolicy(ctx, shared.CreateAccountGuardrailPolicyRequestModelAvailability(*body))
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

	policy, found, lookupDiags := guardrailPolicyAccountLookup(ctx, r.client, guardrailPolicyAccountTypeModelAvailability, data.ID.ValueString())
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

func (r *GuardrailPolicyAccountModelAvailabilityResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data *GuardrailPolicyAccountModelAvailabilityResourceModel
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

	policy, found, lookupDiags := guardrailPolicyAccountLookup(ctx, r.client, guardrailPolicyAccountTypeModelAvailability, data.ID.ValueString())
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

func (r *GuardrailPolicyAccountModelAvailabilityResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data *GuardrailPolicyAccountModelAvailabilityResourceModel
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
		Body: shared.CreateAccountGuardrailPolicyRequestModelAvailability(*body),
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
	policy, found, lookupDiags := guardrailPolicyAccountLookup(ctx, r.client, guardrailPolicyAccountTypeModelAvailability, data.ID.ValueString())
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

func (r *GuardrailPolicyAccountModelAvailabilityResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data *GuardrailPolicyAccountModelAvailabilityResourceModel
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

	// A destroy of the account default does not reach the route. Measured 2026-11-18:
	// DELETE succeeds for a policy that holds the default flag, with no refusal, so
	// sending it would release the baseline for every workspace that resolves to the
	// policy, silently and with no undelete. The resource is forgotten instead, which
	// leaves the policy in place and managed by nobody: write the configuration back, or
	// delete the policy deliberately from the dashboard.
	if data.IsDefault.ValueBool() {
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
func (r *GuardrailPolicyAccountModelAvailabilityResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
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
func (r *GuardrailPolicyAccountModelAvailabilityResourceModel) accountPolicyVariant(policy shared.AccountGuardrailPolicy) *shared.ModelAvailabilityAccountGuardrailPolicy {
	if policy.Type != shared.AccountGuardrailPolicyTypeModelAvailability {
		return nil
	}
	return policy.ModelAvailabilityAccountGuardrailPolicy
}

// refreshFromRow hydrates the attributes the list returns. `id` is deliberately not taken
// from here: the identifier that resolved the row is the one state keeps.
func (r *GuardrailPolicyAccountModelAvailabilityResourceModel) refreshFromRow(ctx context.Context, resp *shared.ModelAvailabilityAccountGuardrailPolicy) diag.Diagnostics {
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

	r.flattenConfig(resp)

	return diags
}

// flattenConfig lifts the config out of the row the way the generated workspace-scoped
// sibling does for its own response shape.
func (r *GuardrailPolicyAccountModelAvailabilityResourceModel) flattenConfig(resp *shared.ModelAvailabilityAccountGuardrailPolicy) {
	r.Config = &tfTypes.GuardrailModelAvailabilityPolicyAuthoringConfig{}
	r.Config.AllowSafetyRetentionGoogle = types.BoolPointerValue(resp.Config.AllowSafetyRetentionGoogle)
	r.Config.AllowSafetyRetentionOther = types.BoolPointerValue(resp.Config.AllowSafetyRetentionOther)
	if resp.Config.AllowedDataRegions != nil {
		r.Config.AllowedDataRegions = make([]types.String, 0, len(resp.Config.AllowedDataRegions))
		for _, v := range resp.Config.AllowedDataRegions {
			r.Config.AllowedDataRegions = append(r.Config.AllowedDataRegions, types.StringValue(v))
		}
	} else {
		r.Config.AllowedDataRegions = nil
	}
	if resp.Config.AllowedModels != nil {
		r.Config.AllowedModels = make([]types.String, 0, len(resp.Config.AllowedModels))
		for _, v := range resp.Config.AllowedModels {
			r.Config.AllowedModels = append(r.Config.AllowedModels, types.StringValue(v))
		}
	} else {
		r.Config.AllowedModels = nil
	}
	if resp.Config.AllowedProviders != nil {
		r.Config.AllowedProviders = make([]types.String, 0, len(resp.Config.AllowedProviders))
		for _, v := range resp.Config.AllowedProviders {
			r.Config.AllowedProviders = append(r.Config.AllowedProviders, types.StringValue(v))
		}
	} else {
		r.Config.AllowedProviders = nil
	}
	r.Config.EnableFreeModelPublication = types.BoolPointerValue(resp.Config.EnableFreeModelPublication)
	r.Config.EnableFreeModelTraining = types.BoolPointerValue(resp.Config.EnableFreeModelTraining)
	r.Config.EnablePaidModelTraining = types.BoolPointerValue(resp.Config.EnablePaidModelTraining)
	r.Config.EnforceFirstPartyHarnessModels = types.BoolPointerValue(resp.Config.EnforceFirstPartyHarnessModels)
	r.Config.EnforceZdr = types.BoolPointerValue(resp.Config.EnforceZdr)
	r.Config.EnforceZdrAnthropic = types.BoolPointerValue(resp.Config.EnforceZdrAnthropic)
	r.Config.EnforceZdrGoogle = types.BoolPointerValue(resp.Config.EnforceZdrGoogle)
	r.Config.EnforceZdrOpenai = types.BoolPointerValue(resp.Config.EnforceZdrOpenai)
	r.Config.EnforceZdrOther = types.BoolPointerValue(resp.Config.EnforceZdrOther)
	r.Config.EnforceZdrXai = types.BoolPointerValue(resp.Config.EnforceZdrXai)
	if resp.Config.IgnoredModels != nil {
		r.Config.IgnoredModels = make([]types.String, 0, len(resp.Config.IgnoredModels))
		for _, v := range resp.Config.IgnoredModels {
			r.Config.IgnoredModels = append(r.Config.IgnoredModels, types.StringValue(v))
		}
	} else {
		r.Config.IgnoredModels = nil
	}
	if resp.Config.IgnoredProviders != nil {
		r.Config.IgnoredProviders = make([]types.String, 0, len(resp.Config.IgnoredProviders))
		for _, v := range resp.Config.IgnoredProviders {
			r.Config.IgnoredProviders = append(r.Config.IgnoredProviders, types.StringValue(v))
		}
	} else {
		r.Config.IgnoredProviders = nil
	}
	if resp.Config.PlatformBannedAuthors != nil {
		r.Config.PlatformBannedAuthors = make([]types.String, 0, len(resp.Config.PlatformBannedAuthors))
		for _, v := range resp.Config.PlatformBannedAuthors {
			r.Config.PlatformBannedAuthors = append(r.Config.PlatformBannedAuthors, types.StringValue(v))
		}
	} else {
		r.Config.PlatformBannedAuthors = nil
	}
	if resp.Config.PlatformBannedModels != nil {
		r.Config.PlatformBannedModels = make([]types.String, 0, len(resp.Config.PlatformBannedModels))
		for _, v := range resp.Config.PlatformBannedModels {
			r.Config.PlatformBannedModels = append(r.Config.PlatformBannedModels, types.StringValue(v))
		}
	} else {
		r.Config.PlatformBannedModels = nil
	}
	if resp.Config.PlatformBannedProviders != nil {
		r.Config.PlatformBannedProviders = make([]types.String, 0, len(resp.Config.PlatformBannedProviders))
		for _, v := range resp.Config.PlatformBannedProviders {
			r.Config.PlatformBannedProviders = append(r.Config.PlatformBannedProviders, types.StringValue(v))
		}
	} else {
		r.Config.PlatformBannedProviders = nil
	}
	r.Config.RequireEuDataRegion = types.BoolPointerValue(resp.Config.RequireEuDataRegion)
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
//   - while `is_default` is false, `opted_out_workspace_ids` is not sent, because the
//     route answers that with 400 "Only the account default can have default assignment
//     fields". Flipping a default off therefore drops its opt-outs.
//
// `is_default` itself always travels, as an explicit true or false, because the route
// treats an absent value as no change while the provider's state always knows which one
// the configuration asked for.
func (r *GuardrailPolicyAccountModelAvailabilityResourceModel) toSharedAccountGuardrailPolicyRequest() (*shared.ModelAvailabilityAccountGuardrailPolicyRequest, diag.Diagnostics) {
	var diags diag.Diagnostics

	allowSafetyRetentionGoogle := new(bool)
	if !r.Config.AllowSafetyRetentionGoogle.IsUnknown() && !r.Config.AllowSafetyRetentionGoogle.IsNull() {
		*allowSafetyRetentionGoogle = r.Config.AllowSafetyRetentionGoogle.ValueBool()
	} else {
		allowSafetyRetentionGoogle = nil
	}
	allowSafetyRetentionOther := new(bool)
	if !r.Config.AllowSafetyRetentionOther.IsUnknown() && !r.Config.AllowSafetyRetentionOther.IsNull() {
		*allowSafetyRetentionOther = r.Config.AllowSafetyRetentionOther.ValueBool()
	} else {
		allowSafetyRetentionOther = nil
	}
	var allowedDataRegions []string
	if r.Config.AllowedDataRegions != nil {
		allowedDataRegions = make([]string, 0, len(r.Config.AllowedDataRegions))
		for allowedDataRegionsIndex := range r.Config.AllowedDataRegions {
			allowedDataRegions = append(allowedDataRegions, r.Config.AllowedDataRegions[allowedDataRegionsIndex].ValueString())
		}
	}
	var allowedModels []string
	if r.Config.AllowedModels != nil {
		allowedModels = make([]string, 0, len(r.Config.AllowedModels))
		for allowedModelsIndex := range r.Config.AllowedModels {
			allowedModels = append(allowedModels, r.Config.AllowedModels[allowedModelsIndex].ValueString())
		}
	}
	var allowedProviders []string
	if r.Config.AllowedProviders != nil {
		allowedProviders = make([]string, 0, len(r.Config.AllowedProviders))
		for allowedProvidersIndex := range r.Config.AllowedProviders {
			allowedProviders = append(allowedProviders, r.Config.AllowedProviders[allowedProvidersIndex].ValueString())
		}
	}
	enableFreeModelPublication := new(bool)
	if !r.Config.EnableFreeModelPublication.IsUnknown() && !r.Config.EnableFreeModelPublication.IsNull() {
		*enableFreeModelPublication = r.Config.EnableFreeModelPublication.ValueBool()
	} else {
		enableFreeModelPublication = nil
	}
	enableFreeModelTraining := new(bool)
	if !r.Config.EnableFreeModelTraining.IsUnknown() && !r.Config.EnableFreeModelTraining.IsNull() {
		*enableFreeModelTraining = r.Config.EnableFreeModelTraining.ValueBool()
	} else {
		enableFreeModelTraining = nil
	}
	enablePaidModelTraining := new(bool)
	if !r.Config.EnablePaidModelTraining.IsUnknown() && !r.Config.EnablePaidModelTraining.IsNull() {
		*enablePaidModelTraining = r.Config.EnablePaidModelTraining.ValueBool()
	} else {
		enablePaidModelTraining = nil
	}
	enforceFirstPartyHarnessModels := new(bool)
	if !r.Config.EnforceFirstPartyHarnessModels.IsUnknown() && !r.Config.EnforceFirstPartyHarnessModels.IsNull() {
		*enforceFirstPartyHarnessModels = r.Config.EnforceFirstPartyHarnessModels.ValueBool()
	} else {
		enforceFirstPartyHarnessModels = nil
	}
	enforceZdr := new(bool)
	if !r.Config.EnforceZdr.IsUnknown() && !r.Config.EnforceZdr.IsNull() {
		*enforceZdr = r.Config.EnforceZdr.ValueBool()
	} else {
		enforceZdr = nil
	}
	enforceZdrAnthropic := new(bool)
	if !r.Config.EnforceZdrAnthropic.IsUnknown() && !r.Config.EnforceZdrAnthropic.IsNull() {
		*enforceZdrAnthropic = r.Config.EnforceZdrAnthropic.ValueBool()
	} else {
		enforceZdrAnthropic = nil
	}
	enforceZdrGoogle := new(bool)
	if !r.Config.EnforceZdrGoogle.IsUnknown() && !r.Config.EnforceZdrGoogle.IsNull() {
		*enforceZdrGoogle = r.Config.EnforceZdrGoogle.ValueBool()
	} else {
		enforceZdrGoogle = nil
	}
	enforceZdrOpenai := new(bool)
	if !r.Config.EnforceZdrOpenai.IsUnknown() && !r.Config.EnforceZdrOpenai.IsNull() {
		*enforceZdrOpenai = r.Config.EnforceZdrOpenai.ValueBool()
	} else {
		enforceZdrOpenai = nil
	}
	enforceZdrOther := new(bool)
	if !r.Config.EnforceZdrOther.IsUnknown() && !r.Config.EnforceZdrOther.IsNull() {
		*enforceZdrOther = r.Config.EnforceZdrOther.ValueBool()
	} else {
		enforceZdrOther = nil
	}
	enforceZdrXai := new(bool)
	if !r.Config.EnforceZdrXai.IsUnknown() && !r.Config.EnforceZdrXai.IsNull() {
		*enforceZdrXai = r.Config.EnforceZdrXai.ValueBool()
	} else {
		enforceZdrXai = nil
	}
	var ignoredModels []string
	if r.Config.IgnoredModels != nil {
		ignoredModels = make([]string, 0, len(r.Config.IgnoredModels))
		for ignoredModelsIndex := range r.Config.IgnoredModels {
			ignoredModels = append(ignoredModels, r.Config.IgnoredModels[ignoredModelsIndex].ValueString())
		}
	}
	var ignoredProviders []string
	if r.Config.IgnoredProviders != nil {
		ignoredProviders = make([]string, 0, len(r.Config.IgnoredProviders))
		for ignoredProvidersIndex := range r.Config.IgnoredProviders {
			ignoredProviders = append(ignoredProviders, r.Config.IgnoredProviders[ignoredProvidersIndex].ValueString())
		}
	}
	var platformBannedAuthors []string
	if r.Config.PlatformBannedAuthors != nil {
		platformBannedAuthors = make([]string, 0, len(r.Config.PlatformBannedAuthors))
		for platformBannedAuthorsIndex := range r.Config.PlatformBannedAuthors {
			platformBannedAuthors = append(platformBannedAuthors, r.Config.PlatformBannedAuthors[platformBannedAuthorsIndex].ValueString())
		}
	}
	var platformBannedModels []string
	if r.Config.PlatformBannedModels != nil {
		platformBannedModels = make([]string, 0, len(r.Config.PlatformBannedModels))
		for platformBannedModelsIndex := range r.Config.PlatformBannedModels {
			platformBannedModels = append(platformBannedModels, r.Config.PlatformBannedModels[platformBannedModelsIndex].ValueString())
		}
	}
	var platformBannedProviders []string
	if r.Config.PlatformBannedProviders != nil {
		platformBannedProviders = make([]string, 0, len(r.Config.PlatformBannedProviders))
		for platformBannedProvidersIndex := range r.Config.PlatformBannedProviders {
			platformBannedProviders = append(platformBannedProviders, r.Config.PlatformBannedProviders[platformBannedProvidersIndex].ValueString())
		}
	}
	requireEuDataRegion := new(bool)
	if !r.Config.RequireEuDataRegion.IsUnknown() && !r.Config.RequireEuDataRegion.IsNull() {
		*requireEuDataRegion = r.Config.RequireEuDataRegion.ValueBool()
	} else {
		requireEuDataRegion = nil
	}
	config := shared.GuardrailModelAvailabilityPolicyAuthoringConfig{
		AllowSafetyRetentionGoogle:     allowSafetyRetentionGoogle,
		AllowSafetyRetentionOther:      allowSafetyRetentionOther,
		AllowedDataRegions:             allowedDataRegions,
		AllowedModels:                  allowedModels,
		AllowedProviders:               allowedProviders,
		EnableFreeModelPublication:     enableFreeModelPublication,
		EnableFreeModelTraining:        enableFreeModelTraining,
		EnablePaidModelTraining:        enablePaidModelTraining,
		EnforceFirstPartyHarnessModels: enforceFirstPartyHarnessModels,
		EnforceZdr:                     enforceZdr,
		EnforceZdrAnthropic:            enforceZdrAnthropic,
		EnforceZdrGoogle:               enforceZdrGoogle,
		EnforceZdrOpenai:               enforceZdrOpenai,
		EnforceZdrOther:                enforceZdrOther,
		EnforceZdrXai:                  enforceZdrXai,
		IgnoredModels:                  ignoredModels,
		IgnoredProviders:               ignoredProviders,
		PlatformBannedAuthors:          platformBannedAuthors,
		PlatformBannedModels:           platformBannedModels,
		PlatformBannedProviders:        platformBannedProviders,
		RequireEuDataRegion:            requireEuDataRegion,
	}

	body := &shared.ModelAvailabilityAccountGuardrailPolicyRequest{
		Config: config,
		Name:   r.Name.ValueString(),
	}

	isDefault := r.IsDefault.ValueBool()
	body.IsDefault = &isDefault

	if isDefault {
		body.OptedOutWorkspaceIds = guardrailPolicyAccountValueStrings(r.OptedOutWorkspaceIDs)
	} else {
		body.WorkspaceIds = guardrailPolicyAccountValueStrings(r.WorkspaceIDs)
	}

	return body, diags
}
