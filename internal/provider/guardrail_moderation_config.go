package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"

	tfTypes "github.com/OpenRouterTeam/terraform-provider-openrouter/internal/provider/types"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/shared"
)

// The account resource delegates moderation config conversion to the generated
// workspace-scoped model methods. Only the account resource calls these wrappers; the
// generated resource calls its own model methods directly, and schema reuse is handled
// separately by generatedGuardrailPolicyConfigSchemaAttribute.
//
// The delegated method is the workspace-scoped create request builder, so the account
// resource's `config` surface follows that wire shape: a field the spec adds to it lands
// here without an account-level change, which is why
// .speakeasy/speakeasy-modifications-overlay.yaml names the delegation.
//
// Diagnostics are propagated rather than dropped: the generated methods report none for
// these conversions today, and any diagnostic a later generation reports reaches the write
// or the refresh path instead of being swallowed. An error there aborts the write; a
// warning is reported and does not. Only the request's `Config` is reused here, and the read
// direction is the generated refresh mapping rather than this one. The generated methods
// ignore ctx, so context.Background() stands in for the request context this model-level
// builder does not carry.
func flattenGuardrailModerationConfig(resp *shared.GuardrailModerationPolicyConfig) (*tfTypes.GuardrailModerationPolicyConfig, diag.Diagnostics) {
	var model GuardrailPolicyModerationResourceModel
	diags := model.RefreshFromSharedModerationGuardrailPolicy(context.Background(), &shared.ModerationGuardrailPolicy{Config: *resp})
	if diags.HasError() {
		return nil, diags
	}
	return model.Config, diags
}

func expandGuardrailModerationConfig(c *tfTypes.GuardrailModerationPolicyConfig) (shared.GuardrailModerationPolicyConfig, diag.Diagnostics) {
	model := GuardrailPolicyModerationResourceModel{Config: c}
	request, diags := model.ToSharedCreateModerationGuardrailPolicyRequest(context.Background())
	if diags.HasError() {
		return shared.GuardrailModerationPolicyConfig{}, diags
	}
	return request.Config, diags
}
