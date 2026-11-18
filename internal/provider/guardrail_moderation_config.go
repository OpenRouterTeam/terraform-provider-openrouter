package provider

import (
	"encoding/json"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/types"

	tfTypes "github.com/OpenRouterTeam/terraform-provider-openrouter/internal/provider/types"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/shared"
)

// The moderation config is shared by the hand-written account resource (
// guardrailpolicyaccountmoderation_resource.go) and the generated workspace-scoped resource
// (guardrailpolicymoderation_resource.go). It was hand-copied into the account resource when that
// family was written, which duplicated the schema block and both mapping directions; these two
// functions are the single implementation, and the account resource no longer carries a copy.
//
// Both resources hold *tfTypes.GuardrailModerationPolicyConfig on the Terraform side and
// shared.GuardrailModerationPolicyConfig on the wire side, so one expand and one flatten serve both
// with no adapter between them.

func flattenGuardrailModerationConfig(resp *shared.GuardrailModerationPolicyConfig) *tfTypes.GuardrailModerationPolicyConfig {
	config := &tfTypes.GuardrailModerationPolicyConfig{}
	if resp.Armor == nil {
		config.Armor = nil
	} else {
		config.Armor = &tfTypes.Armor{}
		if resp.Armor.ClassifierContext == nil {
			config.Armor.ClassifierContext = nil
		} else {
			config.Armor.ClassifierContext = &tfTypes.ArmorContext{}
			if len(resp.Armor.ClassifierContext.AdditionalProperties) > 0 {
				config.Armor.ClassifierContext.AdditionalProperties = make(map[string]jsontypes.Normalized, len(resp.Armor.ClassifierContext.AdditionalProperties))
				for key, value := range resp.Armor.ClassifierContext.AdditionalProperties {
					result, _ := json.Marshal(value)
					config.Armor.ClassifierContext.AdditionalProperties[key] = jsontypes.NewNormalizedValue(string(result))
				}
			}
			if resp.Armor.ClassifierContext.AutoMode == nil {
				config.Armor.ClassifierContext.AutoMode = nil
			} else {
				config.Armor.ClassifierContext.AutoMode = &tfTypes.ArmorAutoMode{}
				config.Armor.ClassifierContext.AutoMode.Allow = []tfTypes.Allow{}

				for _, allowItem := range resp.Armor.ClassifierContext.AutoMode.Allow {
					var allow tfTypes.Allow

					if allowItem.Str != nil {
						allow.Str = types.StringPointerValue(allowItem.Str)
					}
					if allowItem.ArmorRule != nil {
						allow.ArmorRule = &tfTypes.ArmorRule{}
						allow.ArmorRule.ID = types.StringValue(allowItem.ArmorRule.ID)
						allow.ArmorRule.Instructions = types.StringValue(allowItem.ArmorRule.Instructions)
						allow.ArmorRule.Name = types.StringPointerValue(allowItem.ArmorRule.Name)
						allow.ArmorRule.Threshold = types.Float64PointerValue(allowItem.ArmorRule.Threshold)
					}

					config.Armor.ClassifierContext.AutoMode.Allow = append(config.Armor.ClassifierContext.AutoMode.Allow, allow)
				}
				config.Armor.ClassifierContext.AutoMode.Environment = []tfTypes.Environment{}

				for _, environmentItem := range resp.Armor.ClassifierContext.AutoMode.Environment {
					var environment tfTypes.Environment

					if environmentItem.Str != nil {
						environment.Str = types.StringPointerValue(environmentItem.Str)
					}
					if environmentItem.ArmorEnvironmentEntry != nil {
						environment.ArmorEnvironmentEntry = &tfTypes.ArmorEnvironmentEntry{}
						environment.ArmorEnvironmentEntry.Name = types.StringValue(environmentItem.ArmorEnvironmentEntry.Name)
						environment.ArmorEnvironmentEntry.Value = types.StringValue(environmentItem.ArmorEnvironmentEntry.Value)
					}

					config.Armor.ClassifierContext.AutoMode.Environment = append(config.Armor.ClassifierContext.AutoMode.Environment, environment)
				}
				config.Armor.ClassifierContext.AutoMode.HardDeny = []tfTypes.HardDeny{}

				for _, hardDenyItem := range resp.Armor.ClassifierContext.AutoMode.HardDeny {
					var hardDeny tfTypes.HardDeny

					if hardDenyItem.Str != nil {
						hardDeny.Str = types.StringPointerValue(hardDenyItem.Str)
					}
					if hardDenyItem.ArmorRule != nil {
						hardDeny.ArmorRule = &tfTypes.ArmorRule{}
						hardDeny.ArmorRule.ID = types.StringValue(hardDenyItem.ArmorRule.ID)
						hardDeny.ArmorRule.Instructions = types.StringValue(hardDenyItem.ArmorRule.Instructions)
						hardDeny.ArmorRule.Name = types.StringPointerValue(hardDenyItem.ArmorRule.Name)
						hardDeny.ArmorRule.Threshold = types.Float64PointerValue(hardDenyItem.ArmorRule.Threshold)
					}

					config.Armor.ClassifierContext.AutoMode.HardDeny = append(config.Armor.ClassifierContext.AutoMode.HardDeny, hardDeny)
				}
				config.Armor.ClassifierContext.AutoMode.SoftDeny = []tfTypes.SoftDeny{}

				for _, softDenyItem := range resp.Armor.ClassifierContext.AutoMode.SoftDeny {
					var softDeny tfTypes.SoftDeny

					if softDenyItem.Str != nil {
						softDeny.Str = types.StringPointerValue(softDenyItem.Str)
					}
					if softDenyItem.ArmorRule != nil {
						softDeny.ArmorRule = &tfTypes.ArmorRule{}
						softDeny.ArmorRule.ID = types.StringValue(softDenyItem.ArmorRule.ID)
						softDeny.ArmorRule.Instructions = types.StringValue(softDenyItem.ArmorRule.Instructions)
						softDeny.ArmorRule.Name = types.StringPointerValue(softDenyItem.ArmorRule.Name)
						softDeny.ArmorRule.Threshold = types.Float64PointerValue(softDenyItem.ArmorRule.Threshold)
					}

					config.Armor.ClassifierContext.AutoMode.SoftDeny = append(config.Armor.ClassifierContext.AutoMode.SoftDeny, softDeny)
				}
			}
			config.Armor.ClassifierContext.V = types.Float64Value(resp.Armor.ClassifierContext.V)
		}
		if resp.Armor.Mode != nil {
			config.Armor.Mode = types.StringValue(string(*resp.Armor.Mode))
		} else {
			config.Armor.Mode = types.StringNull()
		}
		if resp.Armor.OnError != nil {
			config.Armor.OnError = types.StringValue(string(*resp.Armor.OnError))
		} else {
			config.Armor.OnError = types.StringNull()
		}
		config.Armor.UseCcStyleDefaultRules = types.BoolPointerValue(resp.Armor.UseCcStyleDefaultRules)
	}
	config.BroadcastModelArmorFlaggedRequests = types.BoolPointerValue(resp.BroadcastModelArmorFlaggedRequests)
	config.ContentFilterBlocksRequests = types.BoolPointerValue(resp.ContentFilterBlocksRequests)
	if resp.ContentFilterBuiltins != nil {
		config.ContentFilterBuiltins = []tfTypes.ContentFilterBuiltin{}

		for _, contentFilterBuiltinsItem := range resp.ContentFilterBuiltins {
			var contentFilterBuiltins tfTypes.ContentFilterBuiltin

			contentFilterBuiltins.Action = types.StringValue(string(contentFilterBuiltinsItem.Action))
			contentFilterBuiltins.Label = types.StringPointerValue(contentFilterBuiltinsItem.Label)
			if contentFilterBuiltinsItem.ScanScope != nil {
				contentFilterBuiltins.ScanScope = types.StringValue(string(*contentFilterBuiltinsItem.ScanScope))
			} else {
				contentFilterBuiltins.ScanScope = types.StringNull()
			}
			contentFilterBuiltins.Slug = types.StringValue(string(contentFilterBuiltinsItem.Slug))

			config.ContentFilterBuiltins = append(config.ContentFilterBuiltins, contentFilterBuiltins)
		}
	} else {
		config.ContentFilterBuiltins = nil
	}
	if resp.ContentFilterPatterns != nil {
		config.ContentFilterPatterns = make([]types.String, 0, len(resp.ContentFilterPatterns))
		for _, v := range resp.ContentFilterPatterns {
			config.ContentFilterPatterns = append(config.ContentFilterPatterns, types.StringValue(v))
		}
	} else {
		config.ContentFilterPatterns = nil
	}
	if resp.ContentFilters != nil {
		config.ContentFilters = []tfTypes.ContentFilter{}

		for _, contentFiltersItem := range resp.ContentFilters {
			var contentFilters tfTypes.ContentFilter

			contentFilters.Action = types.StringValue(string(contentFiltersItem.Action))
			contentFilters.Label = types.StringPointerValue(contentFiltersItem.Label)
			contentFilters.Pattern = types.StringValue(contentFiltersItem.Pattern)

			config.ContentFilters = append(config.ContentFilters, contentFilters)
		}
	} else {
		config.ContentFilters = nil
	}
	config.EnableModelArmorInjectionProtection = types.BoolPointerValue(resp.EnableModelArmorInjectionProtection)
	if resp.LakeraInjectionProtectionMode != nil {
		config.LakeraInjectionProtectionMode = types.StringValue(string(*resp.LakeraInjectionProtectionMode))
	} else {
		config.LakeraInjectionProtectionMode = types.StringNull()
	}
	if resp.ModelArmorInjectionConfidenceLevel != nil {
		config.ModelArmorInjectionConfidenceLevel = types.StringValue(string(*resp.ModelArmorInjectionConfidenceLevel))
	} else {
		config.ModelArmorInjectionConfidenceLevel = types.StringNull()
	}
	if resp.ModelArmorInjectionProtectionMode != nil {
		config.ModelArmorInjectionProtectionMode = types.StringValue(string(*resp.ModelArmorInjectionProtectionMode))
	} else {
		config.ModelArmorInjectionProtectionMode = types.StringNull()
	}
	if resp.PiiFilters != nil {
		config.PiiFilters = []tfTypes.PiiFilter{}

		for _, piiFiltersItem := range resp.PiiFilters {
			var piiFilters tfTypes.PiiFilter

			piiFilters.Action = types.StringValue(string(piiFiltersItem.Action))
			piiFilters.EntityType = types.StringValue(piiFiltersItem.EntityType)
			piiFilters.ScoreThreshold = types.Float64PointerValue(piiFiltersItem.ScoreThreshold)

			config.PiiFilters = append(config.PiiFilters, piiFilters)
		}
	} else {
		config.PiiFilters = nil
	}
	if resp.PlatformImageModeratedProviders != nil {
		config.PlatformImageModeratedProviders = make([]types.String, 0, len(resp.PlatformImageModeratedProviders))
		for _, v := range resp.PlatformImageModeratedProviders {
			config.PlatformImageModeratedProviders = append(config.PlatformImageModeratedProviders, types.StringValue(v))
		}
	} else {
		config.PlatformImageModeratedProviders = nil
	}
	if resp.PlatformModeratedProviders != nil {
		config.PlatformModeratedProviders = make([]types.String, 0, len(resp.PlatformModeratedProviders))
		for _, v := range resp.PlatformModeratedProviders {
			config.PlatformModeratedProviders = append(config.PlatformModeratedProviders, types.StringValue(v))
		}
	} else {
		config.PlatformModeratedProviders = nil
	}
	return config
}

func expandGuardrailModerationConfig(c *tfTypes.GuardrailModerationPolicyConfig) shared.GuardrailModerationPolicyConfig {
	var armor *shared.Armor
	if c.Armor != nil {
		var classifierContext *shared.ArmorContext
		if c.Armor.ClassifierContext != nil {
			var autoMode *shared.ArmorAutoMode
			if c.Armor.ClassifierContext.AutoMode != nil {
				allow := make([]shared.Allow, 0, len(c.Armor.ClassifierContext.AutoMode.Allow))
				for allowItem := range c.Armor.ClassifierContext.AutoMode.Allow {
					if !c.Armor.ClassifierContext.AutoMode.Allow[allowItem].Str.IsUnknown() && !c.Armor.ClassifierContext.AutoMode.Allow[allowItem].Str.IsNull() {
						str := c.Armor.ClassifierContext.AutoMode.Allow[allowItem].Str.ValueString()

						allow = append(allow, shared.Allow{
							Str: &str,
						})
					}
					if c.Armor.ClassifierContext.AutoMode.Allow[allowItem].ArmorRule != nil {
						id := c.Armor.ClassifierContext.AutoMode.Allow[allowItem].ArmorRule.ID.ValueString()

						instructions := c.Armor.ClassifierContext.AutoMode.Allow[allowItem].ArmorRule.Instructions.ValueString()

						name := new(string)
						if !c.Armor.ClassifierContext.AutoMode.Allow[allowItem].ArmorRule.Name.IsUnknown() && !c.Armor.ClassifierContext.AutoMode.Allow[allowItem].ArmorRule.Name.IsNull() {
							*name = c.Armor.ClassifierContext.AutoMode.Allow[allowItem].ArmorRule.Name.ValueString()
						} else {
							name = nil
						}
						threshold := new(float64)
						if !c.Armor.ClassifierContext.AutoMode.Allow[allowItem].ArmorRule.Threshold.IsUnknown() && !c.Armor.ClassifierContext.AutoMode.Allow[allowItem].ArmorRule.Threshold.IsNull() {
							*threshold = c.Armor.ClassifierContext.AutoMode.Allow[allowItem].ArmorRule.Threshold.ValueFloat64()
						} else {
							threshold = nil
						}
						armorRule := shared.ArmorRule{
							ID:           id,
							Instructions: instructions,
							Name:         name,
							Threshold:    threshold,
						}
						allow = append(allow, shared.Allow{
							ArmorRule: &armorRule,
						})
					}
				}
				environment := make([]shared.Environment, 0, len(c.Armor.ClassifierContext.AutoMode.Environment))
				for environmentItem := range c.Armor.ClassifierContext.AutoMode.Environment {
					if !c.Armor.ClassifierContext.AutoMode.Environment[environmentItem].Str.IsUnknown() && !c.Armor.ClassifierContext.AutoMode.Environment[environmentItem].Str.IsNull() {
						str1 := c.Armor.ClassifierContext.AutoMode.Environment[environmentItem].Str.ValueString()

						environment = append(environment, shared.Environment{
							Str: &str1,
						})
					}
					if c.Armor.ClassifierContext.AutoMode.Environment[environmentItem].ArmorEnvironmentEntry != nil {
						name1 := c.Armor.ClassifierContext.AutoMode.Environment[environmentItem].ArmorEnvironmentEntry.Name.ValueString()

						value := c.Armor.ClassifierContext.AutoMode.Environment[environmentItem].ArmorEnvironmentEntry.Value.ValueString()

						armorEnvironmentEntry := shared.ArmorEnvironmentEntry{
							Name:  name1,
							Value: value,
						}
						environment = append(environment, shared.Environment{
							ArmorEnvironmentEntry: &armorEnvironmentEntry,
						})
					}
				}
				hardDeny := make([]shared.HardDeny, 0, len(c.Armor.ClassifierContext.AutoMode.HardDeny))
				for hardDenyItem := range c.Armor.ClassifierContext.AutoMode.HardDeny {
					if !c.Armor.ClassifierContext.AutoMode.HardDeny[hardDenyItem].Str.IsUnknown() && !c.Armor.ClassifierContext.AutoMode.HardDeny[hardDenyItem].Str.IsNull() {
						str2 := c.Armor.ClassifierContext.AutoMode.HardDeny[hardDenyItem].Str.ValueString()

						hardDeny = append(hardDeny, shared.HardDeny{
							Str: &str2,
						})
					}
					if c.Armor.ClassifierContext.AutoMode.HardDeny[hardDenyItem].ArmorRule != nil {
						id1 := c.Armor.ClassifierContext.AutoMode.HardDeny[hardDenyItem].ArmorRule.ID.ValueString()

						instructions1 := c.Armor.ClassifierContext.AutoMode.HardDeny[hardDenyItem].ArmorRule.Instructions.ValueString()

						name2 := new(string)
						if !c.Armor.ClassifierContext.AutoMode.HardDeny[hardDenyItem].ArmorRule.Name.IsUnknown() && !c.Armor.ClassifierContext.AutoMode.HardDeny[hardDenyItem].ArmorRule.Name.IsNull() {
							*name2 = c.Armor.ClassifierContext.AutoMode.HardDeny[hardDenyItem].ArmorRule.Name.ValueString()
						} else {
							name2 = nil
						}
						threshold1 := new(float64)
						if !c.Armor.ClassifierContext.AutoMode.HardDeny[hardDenyItem].ArmorRule.Threshold.IsUnknown() && !c.Armor.ClassifierContext.AutoMode.HardDeny[hardDenyItem].ArmorRule.Threshold.IsNull() {
							*threshold1 = c.Armor.ClassifierContext.AutoMode.HardDeny[hardDenyItem].ArmorRule.Threshold.ValueFloat64()
						} else {
							threshold1 = nil
						}
						armorRule1 := shared.ArmorRule{
							ID:           id1,
							Instructions: instructions1,
							Name:         name2,
							Threshold:    threshold1,
						}
						hardDeny = append(hardDeny, shared.HardDeny{
							ArmorRule: &armorRule1,
						})
					}
				}
				softDeny := make([]shared.SoftDeny, 0, len(c.Armor.ClassifierContext.AutoMode.SoftDeny))
				for softDenyItem := range c.Armor.ClassifierContext.AutoMode.SoftDeny {
					if !c.Armor.ClassifierContext.AutoMode.SoftDeny[softDenyItem].Str.IsUnknown() && !c.Armor.ClassifierContext.AutoMode.SoftDeny[softDenyItem].Str.IsNull() {
						str3 := c.Armor.ClassifierContext.AutoMode.SoftDeny[softDenyItem].Str.ValueString()

						softDeny = append(softDeny, shared.SoftDeny{
							Str: &str3,
						})
					}
					if c.Armor.ClassifierContext.AutoMode.SoftDeny[softDenyItem].ArmorRule != nil {
						id2 := c.Armor.ClassifierContext.AutoMode.SoftDeny[softDenyItem].ArmorRule.ID.ValueString()

						instructions2 := c.Armor.ClassifierContext.AutoMode.SoftDeny[softDenyItem].ArmorRule.Instructions.ValueString()

						name3 := new(string)
						if !c.Armor.ClassifierContext.AutoMode.SoftDeny[softDenyItem].ArmorRule.Name.IsUnknown() && !c.Armor.ClassifierContext.AutoMode.SoftDeny[softDenyItem].ArmorRule.Name.IsNull() {
							*name3 = c.Armor.ClassifierContext.AutoMode.SoftDeny[softDenyItem].ArmorRule.Name.ValueString()
						} else {
							name3 = nil
						}
						threshold2 := new(float64)
						if !c.Armor.ClassifierContext.AutoMode.SoftDeny[softDenyItem].ArmorRule.Threshold.IsUnknown() && !c.Armor.ClassifierContext.AutoMode.SoftDeny[softDenyItem].ArmorRule.Threshold.IsNull() {
							*threshold2 = c.Armor.ClassifierContext.AutoMode.SoftDeny[softDenyItem].ArmorRule.Threshold.ValueFloat64()
						} else {
							threshold2 = nil
						}
						armorRule2 := shared.ArmorRule{
							ID:           id2,
							Instructions: instructions2,
							Name:         name3,
							Threshold:    threshold2,
						}
						softDeny = append(softDeny, shared.SoftDeny{
							ArmorRule: &armorRule2,
						})
					}
				}
				autoMode = &shared.ArmorAutoMode{
					Allow:       allow,
					Environment: environment,
					HardDeny:    hardDeny,
					SoftDeny:    softDeny,
				}
			}
			v := c.Armor.ClassifierContext.V.ValueFloat64()

			additionalProperties := make(map[string]interface{})
			for additionalPropertiesKey := range c.Armor.ClassifierContext.AdditionalProperties {
				var additionalPropertiesInst interface{}
				_ = json.Unmarshal([]byte(c.Armor.ClassifierContext.AdditionalProperties[additionalPropertiesKey].ValueString()), &additionalPropertiesInst)
				additionalProperties[additionalPropertiesKey] = additionalPropertiesInst
			}
			classifierContext = &shared.ArmorContext{
				AutoMode:             autoMode,
				V:                    v,
				AdditionalProperties: additionalProperties,
			}
		}
		mode := new(shared.GuardrailModerationPolicyConfigMode)
		if !c.Armor.Mode.IsUnknown() && !c.Armor.Mode.IsNull() {
			*mode = shared.GuardrailModerationPolicyConfigMode(c.Armor.Mode.ValueString())
		} else {
			mode = nil
		}
		onError := new(shared.GuardrailModerationPolicyConfigOnError)
		if !c.Armor.OnError.IsUnknown() && !c.Armor.OnError.IsNull() {
			*onError = shared.GuardrailModerationPolicyConfigOnError(c.Armor.OnError.ValueString())
		} else {
			onError = nil
		}
		useCcStyleDefaultRules := new(bool)
		if !c.Armor.UseCcStyleDefaultRules.IsUnknown() && !c.Armor.UseCcStyleDefaultRules.IsNull() {
			*useCcStyleDefaultRules = c.Armor.UseCcStyleDefaultRules.ValueBool()
		} else {
			useCcStyleDefaultRules = nil
		}
		armor = &shared.Armor{
			ClassifierContext:      classifierContext,
			Mode:                   mode,
			OnError:                onError,
			UseCcStyleDefaultRules: useCcStyleDefaultRules,
		}
	}
	broadcastModelArmorFlaggedRequests := new(bool)
	if !c.BroadcastModelArmorFlaggedRequests.IsUnknown() && !c.BroadcastModelArmorFlaggedRequests.IsNull() {
		*broadcastModelArmorFlaggedRequests = c.BroadcastModelArmorFlaggedRequests.ValueBool()
	} else {
		broadcastModelArmorFlaggedRequests = nil
	}
	contentFilterBlocksRequests := new(bool)
	if !c.ContentFilterBlocksRequests.IsUnknown() && !c.ContentFilterBlocksRequests.IsNull() {
		*contentFilterBlocksRequests = c.ContentFilterBlocksRequests.ValueBool()
	} else {
		contentFilterBlocksRequests = nil
	}
	var contentFilterBuiltins []shared.ContentFilterBuiltin
	if c.ContentFilterBuiltins != nil {
		contentFilterBuiltins = make([]shared.ContentFilterBuiltin, 0, len(c.ContentFilterBuiltins))
		for contentFilterBuiltinsIndex := range c.ContentFilterBuiltins {
			action := shared.GuardrailModerationPolicyConfigContentFilterBuiltinAction(c.ContentFilterBuiltins[contentFilterBuiltinsIndex].Action.ValueString())
			label := new(string)
			if !c.ContentFilterBuiltins[contentFilterBuiltinsIndex].Label.IsUnknown() && !c.ContentFilterBuiltins[contentFilterBuiltinsIndex].Label.IsNull() {
				*label = c.ContentFilterBuiltins[contentFilterBuiltinsIndex].Label.ValueString()
			} else {
				label = nil
			}
			scanScope := new(shared.ScanScope)
			if !c.ContentFilterBuiltins[contentFilterBuiltinsIndex].ScanScope.IsUnknown() && !c.ContentFilterBuiltins[contentFilterBuiltinsIndex].ScanScope.IsNull() {
				*scanScope = shared.ScanScope(c.ContentFilterBuiltins[contentFilterBuiltinsIndex].ScanScope.ValueString())
			} else {
				scanScope = nil
			}
			slug := shared.Slug(c.ContentFilterBuiltins[contentFilterBuiltinsIndex].Slug.ValueString())
			contentFilterBuiltins = append(contentFilterBuiltins, shared.ContentFilterBuiltin{
				Action:    action,
				Label:     label,
				ScanScope: scanScope,
				Slug:      slug,
			})
		}
	}
	var contentFilterPatterns []string
	if c.ContentFilterPatterns != nil {
		contentFilterPatterns = make([]string, 0, len(c.ContentFilterPatterns))
		for contentFilterPatternsIndex := range c.ContentFilterPatterns {
			contentFilterPatterns = append(contentFilterPatterns, c.ContentFilterPatterns[contentFilterPatternsIndex].ValueString())
		}
	}
	var contentFilters []shared.ContentFilter
	if c.ContentFilters != nil {
		contentFilters = make([]shared.ContentFilter, 0, len(c.ContentFilters))
		for contentFiltersIndex := range c.ContentFilters {
			action1 := shared.GuardrailModerationPolicyConfigContentFilterAction(c.ContentFilters[contentFiltersIndex].Action.ValueString())
			label1 := new(string)
			if !c.ContentFilters[contentFiltersIndex].Label.IsUnknown() && !c.ContentFilters[contentFiltersIndex].Label.IsNull() {
				*label1 = c.ContentFilters[contentFiltersIndex].Label.ValueString()
			} else {
				label1 = nil
			}
			pattern := c.ContentFilters[contentFiltersIndex].Pattern.ValueString()

			contentFilters = append(contentFilters, shared.ContentFilter{
				Action:  action1,
				Label:   label1,
				Pattern: pattern,
			})
		}
	}
	enableModelArmorInjectionProtection := new(bool)
	if !c.EnableModelArmorInjectionProtection.IsUnknown() && !c.EnableModelArmorInjectionProtection.IsNull() {
		*enableModelArmorInjectionProtection = c.EnableModelArmorInjectionProtection.ValueBool()
	} else {
		enableModelArmorInjectionProtection = nil
	}
	lakeraInjectionProtectionMode := new(shared.LakeraInjectionProtectionMode)
	if !c.LakeraInjectionProtectionMode.IsUnknown() && !c.LakeraInjectionProtectionMode.IsNull() {
		*lakeraInjectionProtectionMode = shared.LakeraInjectionProtectionMode(c.LakeraInjectionProtectionMode.ValueString())
	} else {
		lakeraInjectionProtectionMode = nil
	}
	modelArmorInjectionConfidenceLevel := new(shared.ModelArmorInjectionConfidenceLevel)
	if !c.ModelArmorInjectionConfidenceLevel.IsUnknown() && !c.ModelArmorInjectionConfidenceLevel.IsNull() {
		*modelArmorInjectionConfidenceLevel = shared.ModelArmorInjectionConfidenceLevel(c.ModelArmorInjectionConfidenceLevel.ValueString())
	} else {
		modelArmorInjectionConfidenceLevel = nil
	}
	modelArmorInjectionProtectionMode := new(shared.ModelArmorInjectionProtectionMode)
	if !c.ModelArmorInjectionProtectionMode.IsUnknown() && !c.ModelArmorInjectionProtectionMode.IsNull() {
		*modelArmorInjectionProtectionMode = shared.ModelArmorInjectionProtectionMode(c.ModelArmorInjectionProtectionMode.ValueString())
	} else {
		modelArmorInjectionProtectionMode = nil
	}
	var piiFilters []shared.PiiFilter
	if c.PiiFilters != nil {
		piiFilters = make([]shared.PiiFilter, 0, len(c.PiiFilters))
		for piiFiltersIndex := range c.PiiFilters {
			action2 := shared.PiiFilterAction(c.PiiFilters[piiFiltersIndex].Action.ValueString())
			entityType := c.PiiFilters[piiFiltersIndex].EntityType.ValueString()

			scoreThreshold := new(float64)
			if !c.PiiFilters[piiFiltersIndex].ScoreThreshold.IsUnknown() && !c.PiiFilters[piiFiltersIndex].ScoreThreshold.IsNull() {
				*scoreThreshold = c.PiiFilters[piiFiltersIndex].ScoreThreshold.ValueFloat64()
			} else {
				scoreThreshold = nil
			}
			piiFilters = append(piiFilters, shared.PiiFilter{
				Action:         action2,
				EntityType:     entityType,
				ScoreThreshold: scoreThreshold,
			})
		}
	}
	var platformImageModeratedProviders []string
	if c.PlatformImageModeratedProviders != nil {
		platformImageModeratedProviders = make([]string, 0, len(c.PlatformImageModeratedProviders))
		for platformImageModeratedProvidersIndex := range c.PlatformImageModeratedProviders {
			platformImageModeratedProviders = append(platformImageModeratedProviders, c.PlatformImageModeratedProviders[platformImageModeratedProvidersIndex].ValueString())
		}
	}
	var platformModeratedProviders []string
	if c.PlatformModeratedProviders != nil {
		platformModeratedProviders = make([]string, 0, len(c.PlatformModeratedProviders))
		for platformModeratedProvidersIndex := range c.PlatformModeratedProviders {
			platformModeratedProviders = append(platformModeratedProviders, c.PlatformModeratedProviders[platformModeratedProvidersIndex].ValueString())
		}
	}
	config := shared.GuardrailModerationPolicyConfig{
		Armor:                               armor,
		BroadcastModelArmorFlaggedRequests:  broadcastModelArmorFlaggedRequests,
		ContentFilterBlocksRequests:         contentFilterBlocksRequests,
		ContentFilterBuiltins:               contentFilterBuiltins,
		ContentFilterPatterns:               contentFilterPatterns,
		ContentFilters:                      contentFilters,
		EnableModelArmorInjectionProtection: enableModelArmorInjectionProtection,
		LakeraInjectionProtectionMode:       lakeraInjectionProtectionMode,
		ModelArmorInjectionConfidenceLevel:  modelArmorInjectionConfidenceLevel,
		ModelArmorInjectionProtectionMode:   modelArmorInjectionProtectionMode,
		PiiFilters:                          piiFilters,
		PlatformImageModeratedProviders:     platformImageModeratedProviders,
		PlatformModeratedProviders:          platformModeratedProviders,
	}
	return config
}
