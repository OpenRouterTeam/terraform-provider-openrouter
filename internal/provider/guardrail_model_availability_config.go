package provider

import (
	"context"

	tfTypes "github.com/OpenRouterTeam/terraform-provider-openrouter/internal/provider/types"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/shared"
)

// The account resource delegates model availability config conversion to the generated
// workspace-scoped model methods. Only the account resource calls these wrappers;
// the generated resource calls its own model methods directly. Schema reuse is
// handled separately by generatedGuardrailPolicyConfigSchemaAttribute.
//
// The generated methods currently return no diagnostics for these conversions.
// The throwaway model's non-config fields are discarded, not used for account scope.
func flattenGuardrailModelAvailabilityConfig(resp *shared.GuardrailModelAvailabilityPolicyConfig) *tfTypes.GuardrailModelAvailabilityPolicyAuthoringConfig {
	var model GuardrailPolicyModelAvailabilityResourceModel
	_ = model.RefreshFromSharedModelAvailabilityGuardrailPolicy(context.Background(), &shared.ModelAvailabilityGuardrailPolicy{Config: *resp})
	return model.Config
}

func expandGuardrailModelAvailabilityConfig(c *tfTypes.GuardrailModelAvailabilityPolicyAuthoringConfig) shared.GuardrailModelAvailabilityPolicyAuthoringConfig {
	model := GuardrailPolicyModelAvailabilityResourceModel{Config: c}
	request, _ := model.ToSharedCreateModelAvailabilityGuardrailPolicyRequest(context.Background())
	return request.Config
}
