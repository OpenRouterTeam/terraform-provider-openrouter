package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	tfTypes "github.com/OpenRouterTeam/terraform-provider-openrouter/internal/provider/types"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/shared"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &GuardrailPolicyAccountBudgetResource{}
var _ resource.ResourceWithImportState = &GuardrailPolicyAccountBudgetResource{}
var _ resource.ResourceWithConfigValidators = &GuardrailPolicyAccountBudgetResource{}

// NewGuardrailPolicyAccountBudgetResource declares the account (workspace-wide) budget
// policy: the layer that covers every workspace a policy is assigned to, including members
// and API keys that hold a policy of their own.
//
// Hand-written rather than generated because the route has no get-by-id, so the read is a
// list filtered on the policy id. `guardrailpolicyaccount.go` carries the shared route
// contract, `guardrailpolicyaccount_lifecycle.go` everything all three account policy
// resources share, and `docs/resources/guardrail_policy_account_budget.md` the user-facing
// behaviour.
func NewGuardrailPolicyAccountBudgetResource() resource.Resource {
	return &GuardrailPolicyAccountBudgetResource{
		guardrailPolicyAccountBase: guardrailPolicyAccountBudgetBase(),
	}
}

// GuardrailPolicyAccountBudgetResource defines the resource implementation. Everything
// except the name, the description and the generated types is embedded.
type GuardrailPolicyAccountBudgetResource struct {
	guardrailPolicyAccountBase[
		tfTypes.GuardrailBudgetPolicyAuthoringConfig,
		shared.GuardrailBudgetPolicyAuthoringConfig,
		shared.BudgetAccountGuardrailPolicy,
	]
}

// GuardrailPolicyAccountBudgetResourceModel describes the resource data model.
type GuardrailPolicyAccountBudgetResourceModel = guardrailPolicyAccountModel[tfTypes.GuardrailBudgetPolicyAuthoringConfig]

func (r *GuardrailPolicyAccountBudgetResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_guardrail_policy_account_budget"
}

func (r *GuardrailPolicyAccountBudgetResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = guardrailPolicyAccountSchema(
		generatedGuardrailPolicyConfigSchemaAttribute(ctx, NewGuardrailPolicyBudgetResource()),
		"An account (workspace-wide) budget policy: the spend limit every workspace it covers is measured against, and the account default when `is_default` is true.\n\nThis is the layer that binds members and API keys that hold a budget policy of their own, which the workspace default does not: a subject's own policy replaces the default it would otherwise inherit.\n\nA policy that is not the default covers exactly `workspace_ids`. The default covers every workspace except `opted_out_workspace_ids`, in which case `workspace_ids` must be left unset. Only one policy of a type can be the account default.\n\nImport takes the bare policy id.",
	)
}

// guardrailPolicyAccountBudgetBase wires the generated types and the six mappings this
// family owns into the shared lifecycle.
func guardrailPolicyAccountBudgetBase() guardrailPolicyAccountBase[tfTypes.GuardrailBudgetPolicyAuthoringConfig, shared.GuardrailBudgetPolicyAuthoringConfig, shared.BudgetAccountGuardrailPolicy] {
	return guardrailPolicyAccountBase[tfTypes.GuardrailBudgetPolicyAuthoringConfig, shared.GuardrailBudgetPolicyAuthoringConfig, shared.BudgetAccountGuardrailPolicy]{
		ops: guardrailPolicyAccountOps[tfTypes.GuardrailBudgetPolicyAuthoringConfig, shared.GuardrailBudgetPolicyAuthoringConfig, shared.BudgetAccountGuardrailPolicy]{
			policyType: guardrailPolicyAccountTypeBudget,
			variant: func(policy shared.AccountGuardrailPolicy) *shared.BudgetAccountGuardrailPolicy {
				if policy.Type != shared.AccountGuardrailPolicyTypeBudget {
					return nil
				}
				return policy.BudgetAccountGuardrailPolicy
			},
			fields: func(row *shared.BudgetAccountGuardrailPolicy) guardrailPolicyAccountRowFields {
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
			flatten: func(row *shared.BudgetAccountGuardrailPolicy) (*tfTypes.GuardrailBudgetPolicyAuthoringConfig, diag.Diagnostics) {
				return flattenGuardrailBudgetConfig(&row.Config)
			},
			expand: expandGuardrailBudgetConfig,
			wrap: func(config shared.GuardrailBudgetPolicyAuthoringConfig, coverage guardrailPolicyAccountCoverage) shared.AccountGuardrailPolicyRequest {
				isDefault := coverage.IsDefault
				return shared.CreateAccountGuardrailPolicyRequestBudget(shared.BudgetAccountGuardrailPolicyRequest{
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
