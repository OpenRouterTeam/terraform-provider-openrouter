package provider

import (
	"context"

	tfTypes "github.com/OpenRouterTeam/terraform-provider-openrouter/internal/provider/types"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/shared"
)

// The account resource delegates budget config conversion to the generated
// workspace-scoped model methods. Only the account resource calls these wrappers;
// the generated resource calls its own model methods directly. Schema reuse is
// handled separately by generatedGuardrailPolicyConfigSchemaAttribute.
//
// The generated methods currently return no diagnostics for these conversions.
// The throwaway model's non-config fields are discarded, not used for account scope.
func flattenGuardrailBudgetConfig(resp *shared.GuardrailBudgetPolicyConfig) *tfTypes.GuardrailBudgetPolicyAuthoringConfig {
	var model GuardrailPolicyBudgetResourceModel
	_ = model.RefreshFromSharedBudgetGuardrailPolicy(context.Background(), &shared.BudgetGuardrailPolicy{Config: *resp})
	return model.Config
}

func expandGuardrailBudgetConfig(c *tfTypes.GuardrailBudgetPolicyAuthoringConfig) shared.GuardrailBudgetPolicyAuthoringConfig {
	model := GuardrailPolicyBudgetResourceModel{Config: c}
	request, _ := model.ToSharedCreateBudgetGuardrailPolicyRequest(context.Background())
	return request.Config
}
