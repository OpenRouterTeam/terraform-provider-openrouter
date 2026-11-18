package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/types"

	tfTypes "github.com/OpenRouterTeam/terraform-provider-openrouter/internal/provider/types"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/shared"
)

// The budget config is shared by the hand-written account resource
// (guardrailpolicyaccountbudget_resource.go) and the generated workspace-scoped resource (guardrailpolicybudget_resource.go). The account
// resource carried a hand copy of the generated declaration and both mapping directions; these two
// functions are the single implementation of the mapping, and the schema attribute is taken from the
// generated resource instead of restated. See guardrail_policy_config_schema.go.
//
// Both resources hold *tfTypes.GuardrailBudgetPolicyAuthoringConfig on the Terraform side, so one pair of functions
// serves both with no adapter.

func flattenGuardrailBudgetConfig(resp *shared.GuardrailBudgetPolicyConfig) *tfTypes.GuardrailBudgetPolicyAuthoringConfig {
	config := &tfTypes.GuardrailBudgetPolicyAuthoringConfig{}
	config.IncludeByokInBudgets = types.BoolPointerValue(resp.IncludeByokInBudgets)
	config.LimitUsd = types.Float64PointerValue(resp.LimitUsd)
	if resp.ResetInterval != nil {
		config.ResetInterval = types.StringValue(string(*resp.ResetInterval))
	} else {
		config.ResetInterval = types.StringNull()
	}
	return config
}

func expandGuardrailBudgetConfig(c *tfTypes.GuardrailBudgetPolicyAuthoringConfig) shared.GuardrailBudgetPolicyAuthoringConfig {
	includeByokInBudgets := c.IncludeByokInBudgets.ValueBool()

	limitUsd := new(float64)
	if !c.LimitUsd.IsUnknown() && !c.LimitUsd.IsNull() {
		*limitUsd = c.LimitUsd.ValueFloat64()
	} else {
		limitUsd = nil
	}
	resetInterval := new(shared.GuardrailBudgetPolicyAuthoringConfigResetInterval)
	if !c.ResetInterval.IsUnknown() && !c.ResetInterval.IsNull() {
		*resetInterval = shared.GuardrailBudgetPolicyAuthoringConfigResetInterval(c.ResetInterval.ValueString())
	} else {
		resetInterval = nil
	}
	config := shared.GuardrailBudgetPolicyAuthoringConfig{
		IncludeByokInBudgets: includeByokInBudgets,
		LimitUsd:             limitUsd,
		ResetInterval:        resetInterval,
	}
	return config
}
