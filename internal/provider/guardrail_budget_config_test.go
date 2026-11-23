package provider

import (
	tfTypes "github.com/OpenRouterTeam/terraform-provider-openrouter/internal/provider/types"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/shared"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"testing"
)

// Frozen inputs and outputs captured from the hand mappers at f2d345f3.
// Do not regenerate these expectations from the wrappers or generated methods.
func TestGuardrailBudgetConfigLegacyEquivalence(t *testing.T) {
	t.Run("flatten", func(t *testing.T) {
		cases := []guardrailConfigFixture[*shared.GuardrailBudgetPolicyConfig, *tfTypes.GuardrailBudgetPolicyAuthoringConfig]{
			{name: "nil-and-null", input: guardrailConfigFixturePtr(shared.GuardrailBudgetPolicyConfig{}), want: guardrailConfigFixturePtr(tfTypes.GuardrailBudgetPolicyAuthoringConfig{})},
			{name: "present-null-and-empty", input: guardrailConfigFixturePtr(shared.GuardrailBudgetPolicyConfig{IncludeByokInBudgets: guardrailConfigFixturePtr(false),
				LimitUsd:      guardrailConfigFixturePtr(float64(0)),
				ResetInterval: guardrailConfigFixturePtr(shared.GuardrailInterval(""))}), want: guardrailConfigFixturePtr(tfTypes.GuardrailBudgetPolicyAuthoringConfig{IncludeByokInBudgets: types.BoolValue(false),
				LimitUsd:      types.Float64Value(0),
				ResetInterval: types.StringValue("")})},
			{name: "explicit-false-zero-and-empty", input: guardrailConfigFixturePtr(shared.GuardrailBudgetPolicyConfig{IncludeByokInBudgets: guardrailConfigFixturePtr(false),
				LimitUsd:      guardrailConfigFixturePtr(float64(0)),
				ResetInterval: guardrailConfigFixturePtr(shared.GuardrailInterval(""))}), want: guardrailConfigFixturePtr(tfTypes.GuardrailBudgetPolicyAuthoringConfig{IncludeByokInBudgets: types.BoolValue(false),
				LimitUsd:      types.Float64Value(0),
				ResetInterval: types.StringValue("")})},
			{name: "populated", input: guardrailConfigFixturePtr(shared.GuardrailBudgetPolicyConfig{IncludeByokInBudgets: guardrailConfigFixturePtr(true),
				LimitUsd:      guardrailConfigFixturePtr(float64(12.75)),
				ResetInterval: guardrailConfigFixturePtr(shared.GuardrailInterval("config.ResetInterval"))}), want: guardrailConfigFixturePtr(tfTypes.GuardrailBudgetPolicyAuthoringConfig{IncludeByokInBudgets: types.BoolValue(true),
				LimitUsd:      types.Float64Value(12.75),
				ResetInterval: types.StringValue("config.ResetInterval")})},
			{name: "unknown-scalars", input: guardrailConfigFixturePtr(shared.GuardrailBudgetPolicyConfig{IncludeByokInBudgets: guardrailConfigFixturePtr(true),
				LimitUsd:      guardrailConfigFixturePtr(float64(12.75)),
				ResetInterval: guardrailConfigFixturePtr(shared.GuardrailInterval("config.ResetInterval"))}), want: guardrailConfigFixturePtr(tfTypes.GuardrailBudgetPolicyAuthoringConfig{IncludeByokInBudgets: types.BoolValue(true),
				LimitUsd:      types.Float64Value(12.75),
				ResetInterval: types.StringValue("config.ResetInterval")})},
			{name: "mixed", input: guardrailConfigFixturePtr(shared.GuardrailBudgetPolicyConfig{ResetInterval: guardrailConfigFixturePtr(shared.GuardrailInterval(""))}), want: guardrailConfigFixturePtr(tfTypes.GuardrailBudgetPolicyAuthoringConfig{ResetInterval: types.StringValue("")})},
			{name: "isolated-IncludeByokInBudgets", input: guardrailConfigFixturePtr(shared.GuardrailBudgetPolicyConfig{IncludeByokInBudgets: guardrailConfigFixturePtr(true)}), want: guardrailConfigFixturePtr(tfTypes.GuardrailBudgetPolicyAuthoringConfig{IncludeByokInBudgets: types.BoolValue(true)})},
			{name: "isolated-LimitUsd", input: guardrailConfigFixturePtr(shared.GuardrailBudgetPolicyConfig{LimitUsd: guardrailConfigFixturePtr(float64(12.75))}), want: guardrailConfigFixturePtr(tfTypes.GuardrailBudgetPolicyAuthoringConfig{LimitUsd: types.Float64Value(12.75)})},
			{name: "isolated-ResetInterval", input: guardrailConfigFixturePtr(shared.GuardrailBudgetPolicyConfig{ResetInterval: guardrailConfigFixturePtr(shared.GuardrailInterval("config.ResetInterval"))}), want: guardrailConfigFixturePtr(tfTypes.GuardrailBudgetPolicyAuthoringConfig{ResetInterval: types.StringValue("config.ResetInterval")})},
		}
		checkGuardrailConfigFixtures(t, cases, flattenGuardrailBudgetConfig)
	})
	t.Run("expand", func(t *testing.T) {
		cases := []guardrailConfigFixture[*tfTypes.GuardrailBudgetPolicyAuthoringConfig, shared.GuardrailBudgetPolicyAuthoringConfig]{
			{name: "nil-and-null", input: guardrailConfigFixturePtr(tfTypes.GuardrailBudgetPolicyAuthoringConfig{}), want: shared.GuardrailBudgetPolicyAuthoringConfig{}},
			{name: "present-null-and-empty", input: guardrailConfigFixturePtr(tfTypes.GuardrailBudgetPolicyAuthoringConfig{}), want: shared.GuardrailBudgetPolicyAuthoringConfig{}},
			{name: "explicit-false-zero-and-empty", input: guardrailConfigFixturePtr(tfTypes.GuardrailBudgetPolicyAuthoringConfig{IncludeByokInBudgets: types.BoolValue(false),
				LimitUsd:      types.Float64Value(0),
				ResetInterval: types.StringValue("")}), want: shared.GuardrailBudgetPolicyAuthoringConfig{LimitUsd: guardrailConfigFixturePtr(float64(0)),
				ResetInterval: guardrailConfigFixturePtr(shared.GuardrailBudgetPolicyAuthoringConfigResetInterval(""))}},
			{name: "populated", input: guardrailConfigFixturePtr(tfTypes.GuardrailBudgetPolicyAuthoringConfig{IncludeByokInBudgets: types.BoolValue(true),
				LimitUsd:      types.Float64Value(12.75),
				ResetInterval: types.StringValue("config.ResetInterval")}), want: shared.GuardrailBudgetPolicyAuthoringConfig{IncludeByokInBudgets: true,
				LimitUsd:      guardrailConfigFixturePtr(float64(12.75)),
				ResetInterval: guardrailConfigFixturePtr(shared.GuardrailBudgetPolicyAuthoringConfigResetInterval("config.ResetInterval"))}},
			{name: "unknown-scalars", input: guardrailConfigFixturePtr(tfTypes.GuardrailBudgetPolicyAuthoringConfig{IncludeByokInBudgets: types.BoolUnknown(),
				LimitUsd:      types.Float64Unknown(),
				ResetInterval: types.StringUnknown()}), want: shared.GuardrailBudgetPolicyAuthoringConfig{}},
			{name: "mixed", input: guardrailConfigFixturePtr(tfTypes.GuardrailBudgetPolicyAuthoringConfig{IncludeByokInBudgets: types.BoolUnknown(),
				LimitUsd: types.Float64Unknown()}), want: shared.GuardrailBudgetPolicyAuthoringConfig{}},
			{name: "isolated-IncludeByokInBudgets", input: guardrailConfigFixturePtr(tfTypes.GuardrailBudgetPolicyAuthoringConfig{IncludeByokInBudgets: types.BoolValue(true)}), want: shared.GuardrailBudgetPolicyAuthoringConfig{IncludeByokInBudgets: true}},
			{name: "isolated-LimitUsd", input: guardrailConfigFixturePtr(tfTypes.GuardrailBudgetPolicyAuthoringConfig{LimitUsd: types.Float64Value(12.75)}), want: shared.GuardrailBudgetPolicyAuthoringConfig{LimitUsd: guardrailConfigFixturePtr(float64(12.75))}},
			{name: "isolated-ResetInterval", input: guardrailConfigFixturePtr(tfTypes.GuardrailBudgetPolicyAuthoringConfig{ResetInterval: types.StringValue("config.ResetInterval")}), want: shared.GuardrailBudgetPolicyAuthoringConfig{ResetInterval: guardrailConfigFixturePtr(shared.GuardrailBudgetPolicyAuthoringConfigResetInterval("config.ResetInterval"))}},
		}
		checkGuardrailConfigFixtures(t, cases, expandGuardrailBudgetConfig)
	})
}
