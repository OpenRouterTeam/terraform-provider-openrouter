package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	tfTypes "github.com/OpenRouterTeam/terraform-provider-openrouter/internal/provider/types"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/shared"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &GuardrailPolicyAccountModelAvailabilityResource{}
var _ resource.ResourceWithImportState = &GuardrailPolicyAccountModelAvailabilityResource{}
var _ resource.ResourceWithConfigValidators = &GuardrailPolicyAccountModelAvailabilityResource{}

// NewGuardrailPolicyAccountModelAvailabilityResource declares the account (workspace-wide) model availability
// policy: the layer that covers every workspace a policy is assigned to, including members
// and API keys that hold a policy of their own.
//
// Hand-written rather than generated because the route has no get-by-id, so the read is a
// list filtered on the policy id. `guardrailpolicyaccount.go` carries the shared route
// contract, `guardrailpolicyaccount_lifecycle.go` everything all three account policy
// resources share, and `docs/resources/guardrail_policy_account_model_availability.md` the user-facing
// behaviour.
func NewGuardrailPolicyAccountModelAvailabilityResource() resource.Resource {
	return &GuardrailPolicyAccountModelAvailabilityResource{
		guardrailPolicyAccountBase: guardrailPolicyAccountModelAvailabilityBase(),
	}
}

// GuardrailPolicyAccountModelAvailabilityResource defines the resource implementation. Everything
// except the name, the description and the generated types is embedded.
type GuardrailPolicyAccountModelAvailabilityResource struct {
	guardrailPolicyAccountBase[
		tfTypes.GuardrailModelAvailabilityPolicyAuthoringConfig,
		shared.GuardrailModelAvailabilityPolicyAuthoringConfig,
		shared.ModelAvailabilityAccountGuardrailPolicy,
	]
}

// GuardrailPolicyAccountModelAvailabilityResourceModel describes the resource data model.
type GuardrailPolicyAccountModelAvailabilityResourceModel = guardrailPolicyAccountModel[tfTypes.GuardrailModelAvailabilityPolicyAuthoringConfig]

func (r *GuardrailPolicyAccountModelAvailabilityResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_guardrail_policy_account_model_availability"
}

func (r *GuardrailPolicyAccountModelAvailabilityResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = guardrailPolicyAccountSchema(
		generatedGuardrailPolicyConfigSchemaAttribute(ctx, NewGuardrailPolicyModelAvailabilityResource()),
		"An account (workspace-wide) model-availability policy: which models, providers and data regions the covered workspaces may use, plus ZDR enforcement and the training and publication toggles. It is what an account-wide baseline belongs on, because it binds members and API keys that hold a model-availability policy of their own.\n\nA policy that is not the default covers exactly `workspace_ids`. The default covers every workspace except `opted_out_workspace_ids`, in which case `workspace_ids` must be left unset. Only one policy of a type can be the account default. A workspace that holds its own workspace-wide policy of this type does not inherit the account default at all.\n\nImport takes the bare policy id.",
	)
}

// guardrailPolicyAccountModelAvailabilityBase wires the generated types and the six mappings this
// family owns into the shared lifecycle.
func guardrailPolicyAccountModelAvailabilityBase() guardrailPolicyAccountBase[tfTypes.GuardrailModelAvailabilityPolicyAuthoringConfig, shared.GuardrailModelAvailabilityPolicyAuthoringConfig, shared.ModelAvailabilityAccountGuardrailPolicy] {
	return guardrailPolicyAccountBase[tfTypes.GuardrailModelAvailabilityPolicyAuthoringConfig, shared.GuardrailModelAvailabilityPolicyAuthoringConfig, shared.ModelAvailabilityAccountGuardrailPolicy]{
		ops: guardrailPolicyAccountOps[tfTypes.GuardrailModelAvailabilityPolicyAuthoringConfig, shared.GuardrailModelAvailabilityPolicyAuthoringConfig, shared.ModelAvailabilityAccountGuardrailPolicy]{
			policyType: guardrailPolicyAccountTypeModelAvailability,
			variant: func(policy shared.AccountGuardrailPolicy) *shared.ModelAvailabilityAccountGuardrailPolicy {
				if policy.Type != shared.AccountGuardrailPolicyTypeModelAvailability {
					return nil
				}
				return policy.ModelAvailabilityAccountGuardrailPolicy
			},
			fields: func(row *shared.ModelAvailabilityAccountGuardrailPolicy) guardrailPolicyAccountRowFields {
				return guardrailPolicyAccountRowFields{
					IsDefault:                row.IsDefault,
					IsSynced:                 row.IsSynced,
					IsWorkspaceDefaultSynced: row.IsWorkspaceDefaultSynced,
					Name:                     row.Name,
					UpdatedAt:                row.UpdatedAt,
					WorkspaceIDs:             row.WorkspaceIds,
					OptedOutWorkspaceIDs:     row.OptedOutWorkspaceIds,
				}
			},
			flatten: func(row *shared.ModelAvailabilityAccountGuardrailPolicy) (*tfTypes.GuardrailModelAvailabilityPolicyAuthoringConfig, diag.Diagnostics) {
				return flattenGuardrailModelAvailabilityConfig(&row.Config)
			},
			expand: expandGuardrailModelAvailabilityConfig,
			wrap: func(config shared.GuardrailModelAvailabilityPolicyAuthoringConfig, coverage guardrailPolicyAccountCoverage) shared.AccountGuardrailPolicyRequest {
				isDefault := coverage.IsDefault
				return shared.CreateAccountGuardrailPolicyRequestModelAvailability(shared.ModelAvailabilityAccountGuardrailPolicyRequest{
					Config:               config,
					Name:                 coverage.Name,
					IsDefault:            &isDefault,
					OptedOutWorkspaceIds: coverage.OptedOutWorkspaceIDs,
					WorkspaceIds:         coverage.WorkspaceIDs,
				})
			},
		},
	}
}
