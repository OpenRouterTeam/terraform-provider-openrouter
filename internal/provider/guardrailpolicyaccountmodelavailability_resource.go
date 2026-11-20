package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	tfTypes "github.com/OpenRouterTeam/terraform-provider-openrouter/internal/provider/types"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/shared"
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
// a list filtered on the policy id. `guardrailpolicyaccount.go` carries the shared route
// contract, `guardrailpolicyaccount_lifecycle.go` the lifecycle all three account policy
// resources share, and `docs/resources/guardrail_policy_account_model_availability.md` the
// user-facing behaviour.
func NewGuardrailPolicyAccountModelAvailabilityResource() resource.Resource {
	return &GuardrailPolicyAccountModelAvailabilityResource{ops: guardrailPolicyAccountModelAvailabilityOps()}
}

// GuardrailPolicyAccountModelAvailabilityResource defines the resource implementation.
type GuardrailPolicyAccountModelAvailabilityResource struct {
	// Provider configured SDK client.
	client *sdk.OpenRouter

	// The family's half of the shared lifecycle.
	ops guardrailPolicyAccountOps[GuardrailPolicyAccountModelAvailabilityResourceModel]
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
	resp.Schema = guardrailPolicyAccountSchema(
		generatedGuardrailPolicyConfigSchemaAttribute(ctx, NewGuardrailPolicyModelAvailabilityResource()),
		"An account (workspace-wide) model-availability policy: which models, providers and data regions the covered workspaces may use, plus ZDR enforcement and the training and publication toggles. It is what an account-wide baseline belongs on, because it binds members and API keys that hold a model-availability policy of their own.\n\nA policy that is not the default covers exactly `workspace_ids`. The default covers every workspace except `opted_out_workspace_ids`, in which case `workspace_ids` must be left unset. Only one policy of a type can be the account default. A workspace that holds its own workspace-wide policy of this type does not inherit the account default at all.\n\nImport takes the bare policy id.",
	)
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
	guardrailPolicyAccountCreate(ctx, r.client, r.ops, req, resp)
}

func (r *GuardrailPolicyAccountModelAvailabilityResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	guardrailPolicyAccountRead(ctx, r.client, r.ops, req, resp)
}

func (r *GuardrailPolicyAccountModelAvailabilityResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	guardrailPolicyAccountUpdate(ctx, r.client, r.ops, req, resp)
}

func (r *GuardrailPolicyAccountModelAvailabilityResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	guardrailPolicyAccountDelete(ctx, r.client, r.ops, req, resp)
}

func (r *GuardrailPolicyAccountModelAvailabilityResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	guardrailPolicyAccountImportState(ctx, req, resp)
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

	config, configDiags := flattenGuardrailModelAvailabilityConfig(&resp.Config)
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
func (r *GuardrailPolicyAccountModelAvailabilityResourceModel) toSharedAccountGuardrailPolicyRequest() (*shared.ModelAvailabilityAccountGuardrailPolicyRequest, diag.Diagnostics) {
	var diags diag.Diagnostics

	config, configDiags := expandGuardrailModelAvailabilityConfig(r.Config)
	diags.Append(configDiags...)
	if diags.HasError() {
		return nil, diags
	}

	body := &shared.ModelAvailabilityAccountGuardrailPolicyRequest{
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

// guardrailPolicyAccountModelAvailabilityOps wires the mappings this family owns into the shared
// lifecycle: the policy type the routes resolve an id by, the model's accessors, and the
// two generated shapes only this file can name.
func guardrailPolicyAccountModelAvailabilityOps() guardrailPolicyAccountOps[GuardrailPolicyAccountModelAvailabilityResourceModel] {
	return guardrailPolicyAccountOps[GuardrailPolicyAccountModelAvailabilityResourceModel]{
		policyType: guardrailPolicyAccountTypeModelAvailability,
		id:         func(m *GuardrailPolicyAccountModelAvailabilityResourceModel) string { return m.ID.ValueString() },
		setID:      func(m *GuardrailPolicyAccountModelAvailabilityResourceModel, id string) { m.ID = types.StringValue(id) },
		isDefault:  func(m *GuardrailPolicyAccountModelAvailabilityResourceModel) bool { return m.IsDefault.ValueBool() },
		refreshFromRow: func(ctx context.Context, m *GuardrailPolicyAccountModelAvailabilityResourceModel, policy shared.AccountGuardrailPolicy) diag.Diagnostics {
			row := m.accountPolicyVariant(policy)
			if row == nil {
				return guardrailPolicyAccountUnexpectedRow()
			}
			return m.refreshFromRow(ctx, row)
		},
		writeBody: func(ctx context.Context, m *GuardrailPolicyAccountModelAvailabilityResourceModel) (shared.AccountGuardrailPolicyRequest, diag.Diagnostics) {
			body, diags := m.toSharedAccountGuardrailPolicyRequest()
			if diags.HasError() {
				return shared.AccountGuardrailPolicyRequest{}, diags
			}
			return shared.CreateAccountGuardrailPolicyRequestModelAvailability(*body), diags
		},
	}
}
