package provider

import (
	"context"

	tfTypes "github.com/OpenRouterTeam/terraform-provider-openrouter/internal/provider/types"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/shared"
)

// The account resource delegates moderation config conversion to the generated
// workspace-scoped model methods. Only the account resource calls these wrappers;
// the generated resource calls its own model methods directly. Schema reuse is
// handled separately by generatedGuardrailPolicyConfigSchemaAttribute.
//
// The generated methods currently return no diagnostics for these conversions.
// The throwaway model's non-config fields are discarded, not used for account scope.
func flattenGuardrailModerationConfig(resp *shared.GuardrailModerationPolicyConfig) *tfTypes.GuardrailModerationPolicyConfig {
	var model GuardrailPolicyModerationResourceModel
	_ = model.RefreshFromSharedModerationGuardrailPolicy(context.Background(), &shared.ModerationGuardrailPolicy{Config: *resp})
	return model.Config
}

func expandGuardrailModerationConfig(c *tfTypes.GuardrailModerationPolicyConfig) shared.GuardrailModerationPolicyConfig {
	model := GuardrailPolicyModerationResourceModel{Config: c}
	request, _ := model.ToSharedCreateModerationGuardrailPolicyRequest(context.Background())
	return request.Config
}
