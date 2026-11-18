package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/types"

	tfTypes "github.com/OpenRouterTeam/terraform-provider-openrouter/internal/provider/types"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/shared"
)

// The modelavailability config is shared by the hand-written account resource
// (guardrailpolicyaccountmodelavailability_resource.go) and the generated workspace-scoped resource (guardrailpolicymodelavailability_resource.go). The account
// resource carried a hand copy of the generated declaration and both mapping directions; these two
// functions are the single implementation of the mapping, and the schema attribute is taken from the
// generated resource instead of restated. See guardrail_policy_config_schema.go.
//
// Both resources hold *tfTypes.GuardrailModelAvailabilityPolicyAuthoringConfig on the Terraform side, so one pair of functions
// serves both with no adapter.

func flattenGuardrailModelAvailabilityConfig(resp *shared.GuardrailModelAvailabilityPolicyConfig) *tfTypes.GuardrailModelAvailabilityPolicyAuthoringConfig {
	config := &tfTypes.GuardrailModelAvailabilityPolicyAuthoringConfig{}
	config.AllowSafetyRetentionGoogle = types.BoolPointerValue(resp.AllowSafetyRetentionGoogle)
	config.AllowSafetyRetentionOther = types.BoolPointerValue(resp.AllowSafetyRetentionOther)
	if resp.AllowedDataRegions != nil {
		config.AllowedDataRegions = make([]types.String, 0, len(resp.AllowedDataRegions))
		for _, v := range resp.AllowedDataRegions {
			config.AllowedDataRegions = append(config.AllowedDataRegions, types.StringValue(v))
		}
	} else {
		config.AllowedDataRegions = nil
	}
	if resp.AllowedModels != nil {
		config.AllowedModels = make([]types.String, 0, len(resp.AllowedModels))
		for _, v := range resp.AllowedModels {
			config.AllowedModels = append(config.AllowedModels, types.StringValue(v))
		}
	} else {
		config.AllowedModels = nil
	}
	if resp.AllowedProviders != nil {
		config.AllowedProviders = make([]types.String, 0, len(resp.AllowedProviders))
		for _, v := range resp.AllowedProviders {
			config.AllowedProviders = append(config.AllowedProviders, types.StringValue(v))
		}
	} else {
		config.AllowedProviders = nil
	}
	config.EnableFreeModelPublication = types.BoolPointerValue(resp.EnableFreeModelPublication)
	config.EnableFreeModelTraining = types.BoolPointerValue(resp.EnableFreeModelTraining)
	config.EnablePaidModelTraining = types.BoolPointerValue(resp.EnablePaidModelTraining)
	config.EnforceFirstPartyHarnessModels = types.BoolPointerValue(resp.EnforceFirstPartyHarnessModels)
	config.EnforceZdr = types.BoolPointerValue(resp.EnforceZdr)
	config.EnforceZdrAnthropic = types.BoolPointerValue(resp.EnforceZdrAnthropic)
	config.EnforceZdrGoogle = types.BoolPointerValue(resp.EnforceZdrGoogle)
	config.EnforceZdrOpenai = types.BoolPointerValue(resp.EnforceZdrOpenai)
	config.EnforceZdrOther = types.BoolPointerValue(resp.EnforceZdrOther)
	config.EnforceZdrXai = types.BoolPointerValue(resp.EnforceZdrXai)
	if resp.IgnoredModels != nil {
		config.IgnoredModels = make([]types.String, 0, len(resp.IgnoredModels))
		for _, v := range resp.IgnoredModels {
			config.IgnoredModels = append(config.IgnoredModels, types.StringValue(v))
		}
	} else {
		config.IgnoredModels = nil
	}
	if resp.IgnoredProviders != nil {
		config.IgnoredProviders = make([]types.String, 0, len(resp.IgnoredProviders))
		for _, v := range resp.IgnoredProviders {
			config.IgnoredProviders = append(config.IgnoredProviders, types.StringValue(v))
		}
	} else {
		config.IgnoredProviders = nil
	}
	if resp.PlatformBannedAuthors != nil {
		config.PlatformBannedAuthors = make([]types.String, 0, len(resp.PlatformBannedAuthors))
		for _, v := range resp.PlatformBannedAuthors {
			config.PlatformBannedAuthors = append(config.PlatformBannedAuthors, types.StringValue(v))
		}
	} else {
		config.PlatformBannedAuthors = nil
	}
	if resp.PlatformBannedModels != nil {
		config.PlatformBannedModels = make([]types.String, 0, len(resp.PlatformBannedModels))
		for _, v := range resp.PlatformBannedModels {
			config.PlatformBannedModels = append(config.PlatformBannedModels, types.StringValue(v))
		}
	} else {
		config.PlatformBannedModels = nil
	}
	if resp.PlatformBannedProviders != nil {
		config.PlatformBannedProviders = make([]types.String, 0, len(resp.PlatformBannedProviders))
		for _, v := range resp.PlatformBannedProviders {
			config.PlatformBannedProviders = append(config.PlatformBannedProviders, types.StringValue(v))
		}
	} else {
		config.PlatformBannedProviders = nil
	}
	config.RequireEuDataRegion = types.BoolPointerValue(resp.RequireEuDataRegion)
	return config
}

func expandGuardrailModelAvailabilityConfig(c *tfTypes.GuardrailModelAvailabilityPolicyAuthoringConfig) shared.GuardrailModelAvailabilityPolicyAuthoringConfig {
	allowSafetyRetentionGoogle := new(bool)
	if !c.AllowSafetyRetentionGoogle.IsUnknown() && !c.AllowSafetyRetentionGoogle.IsNull() {
		*allowSafetyRetentionGoogle = c.AllowSafetyRetentionGoogle.ValueBool()
	} else {
		allowSafetyRetentionGoogle = nil
	}
	allowSafetyRetentionOther := new(bool)
	if !c.AllowSafetyRetentionOther.IsUnknown() && !c.AllowSafetyRetentionOther.IsNull() {
		*allowSafetyRetentionOther = c.AllowSafetyRetentionOther.ValueBool()
	} else {
		allowSafetyRetentionOther = nil
	}
	var allowedDataRegions []string
	if c.AllowedDataRegions != nil {
		allowedDataRegions = make([]string, 0, len(c.AllowedDataRegions))
		for allowedDataRegionsIndex := range c.AllowedDataRegions {
			allowedDataRegions = append(allowedDataRegions, c.AllowedDataRegions[allowedDataRegionsIndex].ValueString())
		}
	}
	var allowedModels []string
	if c.AllowedModels != nil {
		allowedModels = make([]string, 0, len(c.AllowedModels))
		for allowedModelsIndex := range c.AllowedModels {
			allowedModels = append(allowedModels, c.AllowedModels[allowedModelsIndex].ValueString())
		}
	}
	var allowedProviders []string
	if c.AllowedProviders != nil {
		allowedProviders = make([]string, 0, len(c.AllowedProviders))
		for allowedProvidersIndex := range c.AllowedProviders {
			allowedProviders = append(allowedProviders, c.AllowedProviders[allowedProvidersIndex].ValueString())
		}
	}
	enableFreeModelPublication := new(bool)
	if !c.EnableFreeModelPublication.IsUnknown() && !c.EnableFreeModelPublication.IsNull() {
		*enableFreeModelPublication = c.EnableFreeModelPublication.ValueBool()
	} else {
		enableFreeModelPublication = nil
	}
	enableFreeModelTraining := new(bool)
	if !c.EnableFreeModelTraining.IsUnknown() && !c.EnableFreeModelTraining.IsNull() {
		*enableFreeModelTraining = c.EnableFreeModelTraining.ValueBool()
	} else {
		enableFreeModelTraining = nil
	}
	enablePaidModelTraining := new(bool)
	if !c.EnablePaidModelTraining.IsUnknown() && !c.EnablePaidModelTraining.IsNull() {
		*enablePaidModelTraining = c.EnablePaidModelTraining.ValueBool()
	} else {
		enablePaidModelTraining = nil
	}
	enforceFirstPartyHarnessModels := new(bool)
	if !c.EnforceFirstPartyHarnessModels.IsUnknown() && !c.EnforceFirstPartyHarnessModels.IsNull() {
		*enforceFirstPartyHarnessModels = c.EnforceFirstPartyHarnessModels.ValueBool()
	} else {
		enforceFirstPartyHarnessModels = nil
	}
	enforceZdr := new(bool)
	if !c.EnforceZdr.IsUnknown() && !c.EnforceZdr.IsNull() {
		*enforceZdr = c.EnforceZdr.ValueBool()
	} else {
		enforceZdr = nil
	}
	enforceZdrAnthropic := new(bool)
	if !c.EnforceZdrAnthropic.IsUnknown() && !c.EnforceZdrAnthropic.IsNull() {
		*enforceZdrAnthropic = c.EnforceZdrAnthropic.ValueBool()
	} else {
		enforceZdrAnthropic = nil
	}
	enforceZdrGoogle := new(bool)
	if !c.EnforceZdrGoogle.IsUnknown() && !c.EnforceZdrGoogle.IsNull() {
		*enforceZdrGoogle = c.EnforceZdrGoogle.ValueBool()
	} else {
		enforceZdrGoogle = nil
	}
	enforceZdrOpenai := new(bool)
	if !c.EnforceZdrOpenai.IsUnknown() && !c.EnforceZdrOpenai.IsNull() {
		*enforceZdrOpenai = c.EnforceZdrOpenai.ValueBool()
	} else {
		enforceZdrOpenai = nil
	}
	enforceZdrOther := new(bool)
	if !c.EnforceZdrOther.IsUnknown() && !c.EnforceZdrOther.IsNull() {
		*enforceZdrOther = c.EnforceZdrOther.ValueBool()
	} else {
		enforceZdrOther = nil
	}
	enforceZdrXai := new(bool)
	if !c.EnforceZdrXai.IsUnknown() && !c.EnforceZdrXai.IsNull() {
		*enforceZdrXai = c.EnforceZdrXai.ValueBool()
	} else {
		enforceZdrXai = nil
	}
	var ignoredModels []string
	if c.IgnoredModels != nil {
		ignoredModels = make([]string, 0, len(c.IgnoredModels))
		for ignoredModelsIndex := range c.IgnoredModels {
			ignoredModels = append(ignoredModels, c.IgnoredModels[ignoredModelsIndex].ValueString())
		}
	}
	var ignoredProviders []string
	if c.IgnoredProviders != nil {
		ignoredProviders = make([]string, 0, len(c.IgnoredProviders))
		for ignoredProvidersIndex := range c.IgnoredProviders {
			ignoredProviders = append(ignoredProviders, c.IgnoredProviders[ignoredProvidersIndex].ValueString())
		}
	}
	var platformBannedAuthors []string
	if c.PlatformBannedAuthors != nil {
		platformBannedAuthors = make([]string, 0, len(c.PlatformBannedAuthors))
		for platformBannedAuthorsIndex := range c.PlatformBannedAuthors {
			platformBannedAuthors = append(platformBannedAuthors, c.PlatformBannedAuthors[platformBannedAuthorsIndex].ValueString())
		}
	}
	var platformBannedModels []string
	if c.PlatformBannedModels != nil {
		platformBannedModels = make([]string, 0, len(c.PlatformBannedModels))
		for platformBannedModelsIndex := range c.PlatformBannedModels {
			platformBannedModels = append(platformBannedModels, c.PlatformBannedModels[platformBannedModelsIndex].ValueString())
		}
	}
	var platformBannedProviders []string
	if c.PlatformBannedProviders != nil {
		platformBannedProviders = make([]string, 0, len(c.PlatformBannedProviders))
		for platformBannedProvidersIndex := range c.PlatformBannedProviders {
			platformBannedProviders = append(platformBannedProviders, c.PlatformBannedProviders[platformBannedProvidersIndex].ValueString())
		}
	}
	requireEuDataRegion := new(bool)
	if !c.RequireEuDataRegion.IsUnknown() && !c.RequireEuDataRegion.IsNull() {
		*requireEuDataRegion = c.RequireEuDataRegion.ValueBool()
	} else {
		requireEuDataRegion = nil
	}
	config := shared.GuardrailModelAvailabilityPolicyAuthoringConfig{
		AllowSafetyRetentionGoogle:     allowSafetyRetentionGoogle,
		AllowSafetyRetentionOther:      allowSafetyRetentionOther,
		AllowedDataRegions:             allowedDataRegions,
		AllowedModels:                  allowedModels,
		AllowedProviders:               allowedProviders,
		EnableFreeModelPublication:     enableFreeModelPublication,
		EnableFreeModelTraining:        enableFreeModelTraining,
		EnablePaidModelTraining:        enablePaidModelTraining,
		EnforceFirstPartyHarnessModels: enforceFirstPartyHarnessModels,
		EnforceZdr:                     enforceZdr,
		EnforceZdrAnthropic:            enforceZdrAnthropic,
		EnforceZdrGoogle:               enforceZdrGoogle,
		EnforceZdrOpenai:               enforceZdrOpenai,
		EnforceZdrOther:                enforceZdrOther,
		EnforceZdrXai:                  enforceZdrXai,
		IgnoredModels:                  ignoredModels,
		IgnoredProviders:               ignoredProviders,
		PlatformBannedAuthors:          platformBannedAuthors,
		PlatformBannedModels:           platformBannedModels,
		PlatformBannedProviders:        platformBannedProviders,
		RequireEuDataRegion:            requireEuDataRegion,
	}
	return config
}
