package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	tfTypes "github.com/OpenRouterTeam/terraform-provider-openrouter/internal/provider/types"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/shared"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &GuardrailPolicyAccountModerationResource{}
var _ resource.ResourceWithImportState = &GuardrailPolicyAccountModerationResource{}
var _ resource.ResourceWithConfigValidators = &GuardrailPolicyAccountModerationResource{}

// NewGuardrailPolicyAccountModerationResource declares the account (workspace-wide) moderation
// policy: the layer that covers every workspace a policy is assigned to, including members
// and API keys that hold a policy of their own.
//
// Hand-written rather than generated because the route has no get-by-id, so the read is a
// list filtered on the policy id. `guardrailpolicyaccount.go` carries the shared route
// contract, `guardrailpolicyaccount_lifecycle.go` everything all three account policy
// resources share, and `docs/resources/guardrail_policy_account_moderation.md` the user-facing
// behaviour.
func NewGuardrailPolicyAccountModerationResource() resource.Resource {
	return &GuardrailPolicyAccountModerationResource{
		guardrailPolicyAccountBase: guardrailPolicyAccountModerationBase(),
	}
}

// GuardrailPolicyAccountModerationResource defines the resource implementation. Everything
// except the name, the description and the generated types is embedded.
type GuardrailPolicyAccountModerationResource struct {
	guardrailPolicyAccountBase[
		tfTypes.GuardrailModerationPolicyConfig,
		shared.GuardrailModerationPolicyConfig,
		shared.ModerationAccountGuardrailPolicy,
	]
}

// GuardrailPolicyAccountModerationResourceModel describes the resource data model.
type GuardrailPolicyAccountModerationResourceModel = guardrailPolicyAccountModel[tfTypes.GuardrailModerationPolicyConfig]

func (r *GuardrailPolicyAccountModerationResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_guardrail_policy_account_moderation"
}

func (r *GuardrailPolicyAccountModerationResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = guardrailPolicyAccountSchema(
		generatedGuardrailPolicyConfigSchemaAttribute(ctx, NewGuardrailPolicyModerationResource()),
		"An account (workspace-wide) moderation policy: the content-filtering surface the covered workspaces' traffic passes through, including Model Armor, the built-in content filters, PII filters and the platform-moderated provider lists.\n\nA policy that is not the default covers exactly `workspace_ids`. The default covers every workspace except `opted_out_workspace_ids`, in which case `workspace_ids` must be left unset. Only one policy of a type can be the account default.\n\nImport takes the bare policy id.",
	)
}

// guardrailPolicyAccountModerationBase wires the generated types and the six mappings this
// family owns into the shared lifecycle.
func guardrailPolicyAccountModerationBase() guardrailPolicyAccountBase[tfTypes.GuardrailModerationPolicyConfig, shared.GuardrailModerationPolicyConfig, shared.ModerationAccountGuardrailPolicy] {
	return guardrailPolicyAccountBase[tfTypes.GuardrailModerationPolicyConfig, shared.GuardrailModerationPolicyConfig, shared.ModerationAccountGuardrailPolicy]{
		ops: guardrailPolicyAccountOps[tfTypes.GuardrailModerationPolicyConfig, shared.GuardrailModerationPolicyConfig, shared.ModerationAccountGuardrailPolicy]{
			policyType: guardrailPolicyAccountTypeModeration,
			variant: func(policy shared.AccountGuardrailPolicy) *shared.ModerationAccountGuardrailPolicy {
				if policy.Type != shared.AccountGuardrailPolicyTypeModeration {
					return nil
				}
				return policy.ModerationAccountGuardrailPolicy
			},
			fields: func(row *shared.ModerationAccountGuardrailPolicy) guardrailPolicyAccountRowFields {
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
			flatten: func(row *shared.ModerationAccountGuardrailPolicy) (*tfTypes.GuardrailModerationPolicyConfig, diag.Diagnostics) {
				return flattenGuardrailModerationConfig(&row.Config)
			},
			expand: expandGuardrailModerationConfig,
			wrap: func(config shared.GuardrailModerationPolicyConfig, coverage guardrailPolicyAccountCoverage) shared.AccountGuardrailPolicyRequest {
				isDefault := coverage.IsDefault
				return shared.CreateAccountGuardrailPolicyRequestModeration(shared.ModerationAccountGuardrailPolicyRequest{
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
