package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"encoding/json"
	tfTypes "github.com/OpenRouterTeam/terraform-provider-openrouter/internal/provider/types"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/operations"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/shared"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/validators"
	speakeasy_float64validators "github.com/OpenRouterTeam/terraform-provider-openrouter/internal/validators/float64validators"
	speakeasy_objectvalidators "github.com/OpenRouterTeam/terraform-provider-openrouter/internal/validators/objectvalidators"
	speakeasy_stringvalidators "github.com/OpenRouterTeam/terraform-provider-openrouter/internal/validators/stringvalidators"
	jsontypes "github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/float64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/mapvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/objectvalidator"
	"regexp"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &GuardrailPolicyAccountModerationResource{}
var _ resource.ResourceWithImportState = &GuardrailPolicyAccountModerationResource{}
var _ resource.ResourceWithConfigValidators = &GuardrailPolicyAccountModerationResource{}

// NewGuardrailPolicyAccountModerationResource declares the account (workspace-wide)
// moderation policy: the layer that covers every workspace a policy is assigned to,
// including members and API keys that hold a policy of their own.
//
// Hand-written rather than generated because the route has no get-by-id, so the read is
// a list filtered on the policy id. `guardrailpolicyaccount.go` carries the shared
// route contract, and `docs/resources/guardrail_policy_account_moderation.md` the user-facing
// behaviour.
func NewGuardrailPolicyAccountModerationResource() resource.Resource {
	return &GuardrailPolicyAccountModerationResource{}
}

// GuardrailPolicyAccountModerationResource defines the resource implementation.
type GuardrailPolicyAccountModerationResource struct {
	// Provider configured SDK client.
	client *sdk.OpenRouter
}

// GuardrailPolicyAccountModerationResourceModel describes the resource data model.
type GuardrailPolicyAccountModerationResourceModel struct {
	Config                   *tfTypes.GuardrailModerationPolicyConfig `tfsdk:"config"`
	ID                       types.String                             `tfsdk:"id"`
	IsDefault                types.Bool                               `tfsdk:"is_default"`
	IsSynced                 types.Bool                               `tfsdk:"is_synced"`
	IsWorkspaceDefaultSynced types.Bool                               `tfsdk:"is_workspace_default_synced"`
	Name                     types.String                             `tfsdk:"name"`
	OptedOutWorkspaceIDs     []types.String                           `tfsdk:"opted_out_workspace_ids"`
	UpdatedAt                types.String                             `tfsdk:"updated_at"`
	WorkspaceIDs             []types.String                           `tfsdk:"workspace_ids"`
}

func (r *GuardrailPolicyAccountModerationResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_guardrail_policy_account_moderation"
}

func (r *GuardrailPolicyAccountModerationResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "An account (workspace-wide) moderation policy: the content-filtering surface the covered workspaces' traffic passes through, including Model Armor, the built-in content filters, PII filters and the platform-moderated provider lists.\n\nA policy that is not the default covers exactly `workspace_ids`. The default covers every workspace except `opted_out_workspace_ids`, in which case `workspace_ids` must be left unset. Only one policy of a type can be the account default.\n\nImport takes the bare policy id.",
		Attributes: map[string]schema.Attribute{
			"config": schema.SingleNestedAttribute{
				Required: true,
				Attributes: map[string]schema.Attribute{
					"armor": schema.SingleNestedAttribute{
						Optional: true,
						Attributes: map[string]schema.Attribute{
							"classifier_context": schema.SingleNestedAttribute{
								Computed: true,
								Optional: true,
								Attributes: map[string]schema.Attribute{
									"additional_properties": schema.MapAttribute{
										Computed:    true,
										Optional:    true,
										ElementType: jsontypes.NormalizedType{},
										Validators: []validator.Map{
											mapvalidator.ValueStringsAre(validators.IsValidJSON()),
										},
									},
									"auto_mode": schema.SingleNestedAttribute{
										Computed: true,
										Optional: true,
										Attributes: map[string]schema.Attribute{
											"allow": schema.ListNestedAttribute{
												Computed: true,
												Optional: true,
												NestedObject: schema.NestedAttributeObject{
													Validators: []validator.Object{
														speakeasy_objectvalidators.NotNull(),
													},
													Attributes: map[string]schema.Attribute{
														"armor_rule": schema.SingleNestedAttribute{
															Optional: true,
															Attributes: map[string]schema.Attribute{
																"id": schema.StringAttribute{
																	Computed:    true,
																	Optional:    true,
																	Description: `Stable id, unique within its source; reported as the deciding rule when it fires. Not Null`,
																	Validators: []validator.String{
																		speakeasy_stringvalidators.NotNull(),
																		stringvalidator.UTF8LengthBetween(1, 64),
																		stringvalidator.RegexMatches(regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`), "must match pattern "+regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`).String()),
																	},
																},
																"instructions": schema.StringAttribute{
																	Computed:    true,
																	Optional:    true,
																	Description: `What the rule matches, written for the classifier. Not Null`,
																	Validators: []validator.String{
																		speakeasy_stringvalidators.NotNull(),
																		stringvalidator.UTF8LengthBetween(1, 8000),
																	},
																},
																"name": schema.StringAttribute{
																	Computed:    true,
																	Optional:    true,
																	Description: `Display name shown in verdict explanations; defaults to the id.`,
																	Validators: []validator.String{
																		stringvalidator.UTF8LengthBetween(1, 120),
																	},
																},
																"threshold": schema.Float64Attribute{
																	Computed:    true,
																	Optional:    true,
																	Description: `Probability at or above which the rule counts as matched. Defaults to 0.6 for hard-deny and soft-deny rules and 0.7 for allow rules (an exception needs stronger evidence to clear a matched soft deny).`,
																},
															},
															Validators: []validator.Object{
																objectvalidator.ConflictsWith(path.Expressions{
																	path.MatchRelative().AtParent().AtName("str"),
																}...),
															},
														},
														"str": schema.StringAttribute{
															Optional: true,
															Validators: []validator.String{
																stringvalidator.ConflictsWith(path.Expressions{
																	path.MatchRelative().AtParent().AtName("armor_rule"),
																}...),
																stringvalidator.UTF8LengthBetween(1, 8122),
															},
														},
													},
												},
											},
											"environment": schema.ListNestedAttribute{
												Computed: true,
												Optional: true,
												NestedObject: schema.NestedAttributeObject{
													Validators: []validator.Object{
														speakeasy_objectvalidators.NotNull(),
													},
													Attributes: map[string]schema.Attribute{
														"armor_environment_entry": schema.SingleNestedAttribute{
															Optional: true,
															Attributes: map[string]schema.Attribute{
																"name": schema.StringAttribute{
																	Computed:    true,
																	Optional:    true,
																	Description: `Not Null`,
																	Validators: []validator.String{
																		speakeasy_stringvalidators.NotNull(),
																		stringvalidator.UTF8LengthBetween(1, 120),
																	},
																},
																"value": schema.StringAttribute{
																	Computed:    true,
																	Optional:    true,
																	Description: `Not Null`,
																	Validators: []validator.String{
																		speakeasy_stringvalidators.NotNull(),
																		stringvalidator.UTF8LengthAtMost(4000),
																	},
																},
															},
															Validators: []validator.Object{
																objectvalidator.ConflictsWith(path.Expressions{
																	path.MatchRelative().AtParent().AtName("str"),
																}...),
															},
														},
														"str": schema.StringAttribute{
															Optional: true,
															Validators: []validator.String{
																stringvalidator.ConflictsWith(path.Expressions{
																	path.MatchRelative().AtParent().AtName("armor_environment_entry"),
																}...),
																stringvalidator.UTF8LengthBetween(1, 4122),
															},
														},
													},
												},
											},
											"hard_deny": schema.ListNestedAttribute{
												Computed: true,
												Optional: true,
												NestedObject: schema.NestedAttributeObject{
													Validators: []validator.Object{
														speakeasy_objectvalidators.NotNull(),
													},
													Attributes: map[string]schema.Attribute{
														"armor_rule": schema.SingleNestedAttribute{
															Optional: true,
															Attributes: map[string]schema.Attribute{
																"id": schema.StringAttribute{
																	Computed:    true,
																	Optional:    true,
																	Description: `Stable id, unique within its source; reported as the deciding rule when it fires. Not Null`,
																	Validators: []validator.String{
																		speakeasy_stringvalidators.NotNull(),
																		stringvalidator.UTF8LengthBetween(1, 64),
																		stringvalidator.RegexMatches(regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`), "must match pattern "+regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`).String()),
																	},
																},
																"instructions": schema.StringAttribute{
																	Computed:    true,
																	Optional:    true,
																	Description: `What the rule matches, written for the classifier. Not Null`,
																	Validators: []validator.String{
																		speakeasy_stringvalidators.NotNull(),
																		stringvalidator.UTF8LengthBetween(1, 8000),
																	},
																},
																"name": schema.StringAttribute{
																	Computed:    true,
																	Optional:    true,
																	Description: `Display name shown in verdict explanations; defaults to the id.`,
																	Validators: []validator.String{
																		stringvalidator.UTF8LengthBetween(1, 120),
																	},
																},
																"threshold": schema.Float64Attribute{
																	Computed:    true,
																	Optional:    true,
																	Description: `Probability at or above which the rule counts as matched. Defaults to 0.6 for hard-deny and soft-deny rules and 0.7 for allow rules (an exception needs stronger evidence to clear a matched soft deny).`,
																},
															},
															Validators: []validator.Object{
																objectvalidator.ConflictsWith(path.Expressions{
																	path.MatchRelative().AtParent().AtName("str"),
																}...),
															},
														},
														"str": schema.StringAttribute{
															Optional: true,
															Validators: []validator.String{
																stringvalidator.ConflictsWith(path.Expressions{
																	path.MatchRelative().AtParent().AtName("armor_rule"),
																}...),
																stringvalidator.UTF8LengthBetween(1, 8122),
															},
														},
													},
												},
											},
											"soft_deny": schema.ListNestedAttribute{
												Computed: true,
												Optional: true,
												NestedObject: schema.NestedAttributeObject{
													Validators: []validator.Object{
														speakeasy_objectvalidators.NotNull(),
													},
													Attributes: map[string]schema.Attribute{
														"armor_rule": schema.SingleNestedAttribute{
															Optional: true,
															Attributes: map[string]schema.Attribute{
																"id": schema.StringAttribute{
																	Computed:    true,
																	Optional:    true,
																	Description: `Stable id, unique within its source; reported as the deciding rule when it fires. Not Null`,
																	Validators: []validator.String{
																		speakeasy_stringvalidators.NotNull(),
																		stringvalidator.UTF8LengthBetween(1, 64),
																		stringvalidator.RegexMatches(regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`), "must match pattern "+regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`).String()),
																	},
																},
																"instructions": schema.StringAttribute{
																	Computed:    true,
																	Optional:    true,
																	Description: `What the rule matches, written for the classifier. Not Null`,
																	Validators: []validator.String{
																		speakeasy_stringvalidators.NotNull(),
																		stringvalidator.UTF8LengthBetween(1, 8000),
																	},
																},
																"name": schema.StringAttribute{
																	Computed:    true,
																	Optional:    true,
																	Description: `Display name shown in verdict explanations; defaults to the id.`,
																	Validators: []validator.String{
																		stringvalidator.UTF8LengthBetween(1, 120),
																	},
																},
																"threshold": schema.Float64Attribute{
																	Computed:    true,
																	Optional:    true,
																	Description: `Probability at or above which the rule counts as matched. Defaults to 0.6 for hard-deny and soft-deny rules and 0.7 for allow rules (an exception needs stronger evidence to clear a matched soft deny).`,
																},
															},
															Validators: []validator.Object{
																objectvalidator.ConflictsWith(path.Expressions{
																	path.MatchRelative().AtParent().AtName("str"),
																}...),
															},
														},
														"str": schema.StringAttribute{
															Optional: true,
															Validators: []validator.String{
																stringvalidator.ConflictsWith(path.Expressions{
																	path.MatchRelative().AtParent().AtName("armor_rule"),
																}...),
																stringvalidator.UTF8LengthBetween(1, 8122),
															},
														},
													},
												},
											},
										},
									},
									"v": schema.Float64Attribute{
										Computed:    true,
										Optional:    true,
										Description: `Not Null`,
										Validators: []validator.Float64{
											speakeasy_float64validators.NotNull(),
										},
									},
								},
								Description: `The Armor context: Claude Code's ` + "`" + `classifier_context` + "`" + ` (v1), with ` + "`" + `auto_mode.{hard_deny,soft_deny,allow}` + "`" + ` rules as strings or ` + "`" + `{ id, instructions, threshold? }` + "`" + ` objects and ` + "`" + `auto_mode.environment` + "`" + ` entries as ` + "`" + `"Name: value"` + "`" + ` strings or ` + "`" + `{ name, value }` + "`" + ` objects. ` + "`" + `threshold` + "`" + ` applies when Armor evaluates; when the provider evaluates natively (Anthropic dangerous-tool-use beta) rules are judged at the provider's own boundary.`,
							},
							"mode": schema.StringAttribute{
								Computed:    true,
								Optional:    true,
								Description: `` + "`" + `enforce` + "`" + ` withholds blocked calls; ` + "`" + `audit` + "`" + ` evaluates and reports the same verdicts without withholding any calls, regardless of ` + "`" + `on_error` + "`" + `. Default ` + "`" + `enforce` + "`" + `. must be one of ["enforce", "audit"]`,
								Validators: []validator.String{
									stringvalidator.OneOf(
										"enforce",
										"audit",
									),
								},
							},
							"on_error": schema.StringAttribute{
								Computed:    true,
								Optional:    true,
								Description: `When the classifier cannot answer (timeout, upstream error, context too long) the call is reported ` + "`" + `unavailable` + "`" + `; ` + "`" + `block` + "`" + ` rejects it and ` + "`" + `log` + "`" + ` releases it. Default ` + "`" + `block` + "`" + `. must be one of ["log", "block"]`,
								Validators: []validator.String{
									stringvalidator.OneOf(
										"log",
										"block",
									),
								},
							},
							"use_cc_style_default_rules": schema.BoolAttribute{
								Computed:    true,
								Optional:    true,
								Description: `Prepend Claude Code's built-in auto-mode rule set (1 hard-deny, 72 soft-deny, 17 allow, 21 environment slots) ahead of every configured rule. Defaults to true on the Anthropic Messages API and false elsewhere.`,
							},
						},
					},
					"broadcast_model_armor_flagged_requests": schema.BoolAttribute{
						Computed: true,
						Optional: true,
					},
					"content_filter_blocks_requests": schema.BoolAttribute{
						Computed: true,
						Optional: true,
					},
					"content_filter_builtins": schema.ListNestedAttribute{
						Computed: true,
						Optional: true,
						NestedObject: schema.NestedAttributeObject{
							Validators: []validator.Object{
								speakeasy_objectvalidators.NotNull(),
							},
							Attributes: map[string]schema.Attribute{
								"action": schema.StringAttribute{
									Computed:    true,
									Optional:    true,
									Description: `Not Null; must be one of ["redact", "block", "flag"]`,
									Validators: []validator.String{
										speakeasy_stringvalidators.NotNull(),
										stringvalidator.OneOf(
											"redact",
											"block",
											"flag",
										),
									},
								},
								"label": schema.StringAttribute{
									Computed: true,
									Optional: true,
									Validators: []validator.String{
										stringvalidator.UTF8LengthAtMost(100),
									},
								},
								"scan_scope": schema.StringAttribute{
									Computed:    true,
									Optional:    true,
									Description: `must be one of ["user_only", "all_messages"]`,
									Validators: []validator.String{
										stringvalidator.OneOf(
											"user_only",
											"all_messages",
										),
									},
								},
								"slug": schema.StringAttribute{
									Computed:    true,
									Optional:    true,
									Description: `Not Null; must be one of ["email", "phone", "ssn", "credit-card", "ip-address", "secrets", "person-name", "address", "regex-prompt-injection"]`,
									Validators: []validator.String{
										speakeasy_stringvalidators.NotNull(),
										stringvalidator.OneOf(
											"email",
											"phone",
											"ssn",
											"credit-card",
											"ip-address",
											"secrets",
											"person-name",
											"address",
											"regex-prompt-injection",
										),
									},
								},
							},
						},
					},
					"content_filter_patterns": schema.ListAttribute{
						Computed:    true,
						Optional:    true,
						ElementType: types.StringType,
					},
					"content_filters": schema.ListNestedAttribute{
						Computed: true,
						Optional: true,
						NestedObject: schema.NestedAttributeObject{
							Validators: []validator.Object{
								speakeasy_objectvalidators.NotNull(),
							},
							Attributes: map[string]schema.Attribute{
								"action": schema.StringAttribute{
									Computed:    true,
									Optional:    true,
									Description: `Not Null; must be one of ["redact", "block", "flag"]`,
									Validators: []validator.String{
										speakeasy_stringvalidators.NotNull(),
										stringvalidator.OneOf(
											"redact",
											"block",
											"flag",
										),
									},
								},
								"label": schema.StringAttribute{
									Computed: true,
									Optional: true,
									Validators: []validator.String{
										stringvalidator.UTF8LengthAtMost(100),
									},
								},
								"pattern": schema.StringAttribute{
									Computed:    true,
									Optional:    true,
									Description: `Not Null`,
									Validators: []validator.String{
										speakeasy_stringvalidators.NotNull(),
										stringvalidator.UTF8LengthBetween(1, 100000),
									},
								},
							},
						},
					},
					"enable_model_armor_injection_protection": schema.BoolAttribute{
						Computed: true,
						Optional: true,
					},
					"lakera_injection_protection_mode": schema.StringAttribute{
						Computed:    true,
						Optional:    true,
						Description: `must be one of ["block", "flag"]`,
						Validators: []validator.String{
							stringvalidator.OneOf(
								"block",
								"flag",
							),
						},
					},
					"model_armor_injection_confidence_level": schema.StringAttribute{
						Computed:    true,
						Optional:    true,
						Description: `must be one of ["low", "medium", "high"]`,
						Validators: []validator.String{
							stringvalidator.OneOf(
								"low",
								"medium",
								"high",
							),
						},
					},
					"model_armor_injection_protection_mode": schema.StringAttribute{
						Computed:    true,
						Optional:    true,
						Description: `must be one of ["block", "flag"]`,
						Validators: []validator.String{
							stringvalidator.OneOf(
								"block",
								"flag",
							),
						},
					},
					"pii_filters": schema.ListNestedAttribute{
						Computed: true,
						Optional: true,
						NestedObject: schema.NestedAttributeObject{
							Validators: []validator.Object{
								speakeasy_objectvalidators.NotNull(),
							},
							Attributes: map[string]schema.Attribute{
								"action": schema.StringAttribute{
									Computed:    true,
									Optional:    true,
									Description: `Not Null; must be one of ["redact", "block", "flag"]`,
									Validators: []validator.String{
										speakeasy_stringvalidators.NotNull(),
										stringvalidator.OneOf(
											"redact",
											"block",
											"flag",
										),
									},
								},
								"entity_type": schema.StringAttribute{
									Computed:    true,
									Optional:    true,
									Description: `Not Null`,
									Validators: []validator.String{
										speakeasy_stringvalidators.NotNull(),
									},
								},
								"score_threshold": schema.Float64Attribute{
									Computed: true,
									Optional: true,
									Validators: []validator.Float64{
										float64validator.Between(0, 1),
									},
								},
							},
						},
					},
					"platform_image_moderated_providers": schema.ListAttribute{
						Computed:    true,
						Optional:    true,
						ElementType: types.StringType,
					},
					"platform_moderated_providers": schema.ListAttribute{
						Computed:    true,
						Optional:    true,
						ElementType: types.StringType,
					},
				},
			},
			"id": schema.StringAttribute{
				Computed:    true,
				Description: `Policy ID`,
			},
			"is_default": schema.BoolAttribute{
				Computed:    true,
				Optional:    true,
				Description: `Cover every workspace in the account except ` + "`" + `opted_out_workspace_ids` + "`" + `. Set it to ` + "`" + `false` + "`" + ` to unset that status. Only one policy of a type can be the account default: declaring it on a second policy of the same type unsets it on the first and clears its coverage, so two policies that both declare it change on every run. Destroying a policy that holds this status removes it from state without deleting it, because the route would release the baseline for every workspace that resolves to it.`,
			},
			"is_synced": schema.BoolAttribute{
				Computed:    true,
				Description: `Kept in sync with a v1 guardrail; saving changes creates an account copy`,
			},
			"is_workspace_default_synced": schema.BoolAttribute{
				Computed:    true,
				Description: `Synced from a v1 workspace default guardrail`,
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: `Policy name`,
				Validators: []validator.String{
					stringvalidator.UTF8LengthBetween(1, 255),
				},
			},
			"opted_out_workspace_ids": schema.ListAttribute{
				Computed:    true,
				Optional:    true,
				ElementType: types.StringType,
				Description: `Workspaces left out of a default policy. Only the account default accepts these, and the API answers a policy that is not the default with 400 when it carries them, so flipping ` + "`" + `is_default` + "`" + ` off drops them.`,
			},
			"updated_at": schema.StringAttribute{
				Computed: true,
			},
			"workspace_ids": schema.ListAttribute{
				Computed:    true,
				Optional:    true,
				ElementType: types.StringType,
				Description: `Workspaces a policy covers when it is not the default. For the account default this attribute reads back the workspaces currently resolving to it, which is more than what was configured, and the provider does not send it while ` + "`" + `is_default` + "`" + ` is true.`,
			},
		},
	}
}

// ConfigValidators refuses the two coverage combinations the route answers with 400, at
// plan time. It is config-level because each rule relates two attributes.
func (r *GuardrailPolicyAccountModerationResource) ConfigValidators(ctx context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{
		guardrailPolicyAccountCoverageValidator{},
	}
}

func (r *GuardrailPolicyAccountModerationResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	// Prevent panic if the provider has not been configured.
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*sdk.OpenRouter)

	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *sdk.OpenRouter, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)

		return
	}

	r.client = client
}

func (r *GuardrailPolicyAccountModerationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data *GuardrailPolicyAccountModerationResourceModel
	var plan types.Object

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(plan.As(ctx, &data, basetypes.ObjectAsOptions{
		UnhandledNullAsEmpty:    true,
		UnhandledUnknownAsEmpty: true,
	})...)

	if resp.Diagnostics.HasError() {
		return
	}

	body, bodyDiags := data.toSharedAccountGuardrailPolicyRequest()
	resp.Diagnostics.Append(bodyDiags...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.client.GuardrailPolicies.CreateAccountPolicy(ctx, shared.CreateAccountGuardrailPolicyRequestModeration(*body))
	if err != nil {
		resp.Diagnostics.AddError("failure to invoke API", redactSensitiveValues(ctx, err.Error()))
		if res != nil && res.RawResponse != nil {
			resp.Diagnostics.AddError("unexpected http request/response", debugResponse(res.RawResponse))
		}
		return
	}
	if res == nil {
		resp.Diagnostics.AddError("unexpected response from API", fmt.Sprintf("%v", res))
		return
	}
	if res.StatusCode == 409 {
		resp.Diagnostics.AddError(
			"Resource Already Exists",
			"When creating this resource, the API indicated that this resource already exists. You can bring the existing resource under management using Terraform import functionality or retry with a unique configuration.",
		)
		return
	}
	if res.StatusCode != 201 {
		resp.Diagnostics.AddError(fmt.Sprintf("unexpected response from API. Got an unexpected response code %v", res.StatusCode),
			guardrailPolicyAccountResponseMessage(res.BadRequestResponse, res.ConflictResponse, res.NotFoundResponse, res.RawResponse))
		return
	}
	if res.AccountGuardrailPolicyResponse == nil {
		resp.Diagnostics.AddError("unexpected response from API. Got an unexpected response body", debugResponse(res.RawResponse))
		return
	}

	// The write response carries the new id and not the read model, so take the id from
	// it and read the policy back the way every other operation does.
	data.ID = types.StringValue(guardrailPolicyAccountID(res.AccountGuardrailPolicyResponse.Data))
	if data.ID.ValueString() == "" {
		resp.Diagnostics.AddError("unexpected response from API", "the created policy carried no id")
		return
	}

	policy, found, lookupDiags := guardrailPolicyAccountLookup(ctx, r.client, guardrailPolicyAccountTypeModeration, data.ID.ValueString())
	resp.Diagnostics.Append(lookupDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		resp.Diagnostics.AddError(
			"the policy is not in the account policy list after it was created",
			"The API answered the create with a policy id that the account policy list does not return. The policy exists; re-running the plan should read it.",
		)
		return
	}

	row := data.accountPolicyVariant(*policy)
	if row == nil {
		resp.Diagnostics.AddError("unexpected response from API", "the account policy list returned a policy of a type this resource cannot read")
		return
	}

	resp.Diagnostics.Append(data.refreshFromRow(ctx, row)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(refreshPlan(ctx, plan, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *GuardrailPolicyAccountModerationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data *GuardrailPolicyAccountModerationResourceModel
	var item types.Object

	resp.Diagnostics.Append(req.State.Get(ctx, &item)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(item.As(ctx, &data, basetypes.ObjectAsOptions{
		UnhandledNullAsEmpty:    true,
		UnhandledUnknownAsEmpty: true,
	})...)

	if resp.Diagnostics.HasError() {
		return
	}

	policy, found, lookupDiags := guardrailPolicyAccountLookup(ctx, r.client, guardrailPolicyAccountTypeModeration, data.ID.ValueString())
	resp.Diagnostics.Append(lookupDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		// An account policy list that returns in full without the id is the only
		// absence signal these routes give: there is no get-by-id to answer 404 for
		// a single policy. A route that is unavailable answers non-200 above and
		// reaches this as an error rather than as absence.
		resp.State.RemoveResource(ctx)
		return
	}

	row := data.accountPolicyVariant(*policy)
	if row == nil {
		resp.Diagnostics.AddError("unexpected response from API", "the account policy list returned a policy of a type this resource cannot read")
		return
	}

	resp.Diagnostics.Append(data.refreshFromRow(ctx, row)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *GuardrailPolicyAccountModerationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data *GuardrailPolicyAccountModerationResourceModel
	var plan types.Object

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	merge(ctx, req, resp, &data)
	if resp.Diagnostics.HasError() {
		return
	}

	body, bodyDiags := data.toSharedAccountGuardrailPolicyRequest()
	resp.Diagnostics.Append(bodyDiags...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.client.GuardrailPolicies.UpdateAccountPolicy(ctx, operations.UpdateAccountPolicyRequest{
		ID:   data.ID.ValueString(),
		Body: shared.CreateAccountGuardrailPolicyRequestModeration(*body),
	})
	if err != nil {
		resp.Diagnostics.AddError("failure to invoke API", redactSensitiveValues(ctx, err.Error()))
		if res != nil && res.RawResponse != nil {
			resp.Diagnostics.AddError("unexpected http request/response", debugResponse(res.RawResponse))
		}
		return
	}
	if res == nil {
		resp.Diagnostics.AddError("unexpected response from API", fmt.Sprintf("%v", res))
		return
	}
	if res.StatusCode != 200 {
		resp.Diagnostics.AddError(fmt.Sprintf("unexpected response from API. Got an unexpected response code %v", res.StatusCode),
			guardrailPolicyAccountResponseMessage(res.BadRequestResponse, res.ConflictResponse, res.NotFoundResponse, res.RawResponse))
		return
	}
	if res.AccountGuardrailPolicyResponse == nil {
		resp.Diagnostics.AddError("unexpected response from API. Got an unexpected response body", debugResponse(res.RawResponse))
		return
	}

	// See Create: the write response is not the read model, and for an update the id is
	// already the one state holds.
	policy, found, lookupDiags := guardrailPolicyAccountLookup(ctx, r.client, guardrailPolicyAccountTypeModeration, data.ID.ValueString())
	resp.Diagnostics.Append(lookupDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		resp.Diagnostics.AddError(
			"the policy is not in the account policy list after it was updated",
			"The API answered the update with a policy id that the account policy list does not return. Re-running the plan should read it.",
		)
		return
	}

	row := data.accountPolicyVariant(*policy)
	if row == nil {
		resp.Diagnostics.AddError("unexpected response from API", "the account policy list returned a policy of a type this resource cannot read")
		return
	}

	resp.Diagnostics.Append(data.refreshFromRow(ctx, row)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(refreshPlan(ctx, plan, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *GuardrailPolicyAccountModerationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data *GuardrailPolicyAccountModerationResourceModel
	var item types.Object

	resp.Diagnostics.Append(req.State.Get(ctx, &item)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(item.As(ctx, &data, basetypes.ObjectAsOptions{
		UnhandledNullAsEmpty:    true,
		UnhandledUnknownAsEmpty: true,
	})...)

	if resp.Diagnostics.HasError() {
		return
	}

	// A destroy of the account default does not reach the route. Measured 2026-11-18:
	// DELETE succeeds for a policy that holds the default flag, with no refusal, so
	// sending it would release the baseline for every workspace that resolves to the
	// policy, silently and with no undelete. The resource is forgotten instead, which
	// leaves the policy in place and managed by nobody: write the configuration back, or
	// delete the policy deliberately from the dashboard.
	if data.IsDefault.ValueBool() {
		return
	}

	res, err := r.client.GuardrailPolicies.DeleteAccountPolicy(ctx, operations.DeleteAccountPolicyRequest{
		ID: data.ID.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("failure to invoke API", redactSensitiveValues(ctx, err.Error()))
		if res != nil && res.RawResponse != nil {
			resp.Diagnostics.AddError("unexpected http request/response", debugResponse(res.RawResponse))
		}
		return
	}
	if res == nil {
		resp.Diagnostics.AddError("unexpected response from API", fmt.Sprintf("%v", res))
		return
	}
	switch res.StatusCode {
	case 200, 404:
		break
	default:
		resp.Diagnostics.AddError(fmt.Sprintf("unexpected response from API. Got an unexpected response code %v", res.StatusCode),
			guardrailPolicyAccountResponseMessage(res.BadRequestResponse, res.ConflictResponse, res.NotFoundResponse, res.RawResponse))
		return
	}
}

// ImportState takes the bare policy id. The list read is account-wide, so unlike the
// workspace-scoped policy resources this family needs no workspace prefix, and the
// resource's own type supplies the `policy_type` the route resolves the id by.
func (r *GuardrailPolicyAccountModerationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID == "" {
		resp.Diagnostics.AddError("invalid import ID", "expected a policy id, got an empty string")
		return
	}
	if strings.Contains(req.ID, "/") {
		resp.Diagnostics.AddError(
			"invalid import ID",
			fmt.Sprintf("expected a bare policy id, got %q. An account policy is not scoped to a workspace, so this resource's import identifier carries no workspace prefix.", req.ID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

// accountPolicyVariant unwraps the union the list read returns. A nil result means the
// row carries another policy type; the read already refused that case, so this is a
// guard for the write paths rather than a user-facing branch.
func (r *GuardrailPolicyAccountModerationResourceModel) accountPolicyVariant(policy shared.AccountGuardrailPolicy) *shared.ModerationAccountGuardrailPolicy {
	if policy.Type != shared.AccountGuardrailPolicyTypeModeration {
		return nil
	}
	return policy.ModerationAccountGuardrailPolicy
}

// refreshFromRow hydrates the attributes the list returns. `id` is deliberately not taken
// from here: the identifier that resolved the row is the one state keeps.
func (r *GuardrailPolicyAccountModerationResourceModel) refreshFromRow(ctx context.Context, resp *shared.ModerationAccountGuardrailPolicy) diag.Diagnostics {
	var diags diag.Diagnostics

	if resp == nil {
		diags.AddError("unexpected response from API", "the account policy row was absent, so there is nothing to read")
		return diags
	}

	r.IsDefault = types.BoolValue(resp.IsDefault)
	r.IsSynced = types.BoolValue(resp.IsSynced)
	r.IsWorkspaceDefaultSynced = types.BoolValue(resp.IsWorkspaceDefaultSynced)
	r.Name = types.StringValue(resp.Name)
	r.UpdatedAt = types.StringPointerValue(resp.UpdatedAt)

	r.WorkspaceIDs = make([]types.String, 0, len(resp.WorkspaceIds))
	for _, v := range resp.WorkspaceIds {
		r.WorkspaceIDs = append(r.WorkspaceIDs, types.StringValue(v))
	}
	r.OptedOutWorkspaceIDs = make([]types.String, 0, len(resp.OptedOutWorkspaceIds))
	for _, v := range resp.OptedOutWorkspaceIds {
		r.OptedOutWorkspaceIDs = append(r.OptedOutWorkspaceIDs, types.StringValue(v))
	}

	r.flattenConfig(resp)

	return diags
}

// flattenConfig lifts the config out of the row the way the generated workspace-scoped
// sibling does for its own response shape.
func (r *GuardrailPolicyAccountModerationResourceModel) flattenConfig(resp *shared.ModerationAccountGuardrailPolicy) {
	r.Config = &tfTypes.GuardrailModerationPolicyConfig{}
	if resp.Config.Armor == nil {
		r.Config.Armor = nil
	} else {
		r.Config.Armor = &tfTypes.Armor{}
		if resp.Config.Armor.ClassifierContext == nil {
			r.Config.Armor.ClassifierContext = nil
		} else {
			r.Config.Armor.ClassifierContext = &tfTypes.ArmorContext{}
			if len(resp.Config.Armor.ClassifierContext.AdditionalProperties) > 0 {
				r.Config.Armor.ClassifierContext.AdditionalProperties = make(map[string]jsontypes.Normalized, len(resp.Config.Armor.ClassifierContext.AdditionalProperties))
				for key, value := range resp.Config.Armor.ClassifierContext.AdditionalProperties {
					result, _ := json.Marshal(value)
					r.Config.Armor.ClassifierContext.AdditionalProperties[key] = jsontypes.NewNormalizedValue(string(result))
				}
			}
			if resp.Config.Armor.ClassifierContext.AutoMode == nil {
				r.Config.Armor.ClassifierContext.AutoMode = nil
			} else {
				r.Config.Armor.ClassifierContext.AutoMode = &tfTypes.ArmorAutoMode{}
				r.Config.Armor.ClassifierContext.AutoMode.Allow = []tfTypes.Allow{}

				for _, allowItem := range resp.Config.Armor.ClassifierContext.AutoMode.Allow {
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

					r.Config.Armor.ClassifierContext.AutoMode.Allow = append(r.Config.Armor.ClassifierContext.AutoMode.Allow, allow)
				}
				r.Config.Armor.ClassifierContext.AutoMode.Environment = []tfTypes.Environment{}

				for _, environmentItem := range resp.Config.Armor.ClassifierContext.AutoMode.Environment {
					var environment tfTypes.Environment

					if environmentItem.Str != nil {
						environment.Str = types.StringPointerValue(environmentItem.Str)
					}
					if environmentItem.ArmorEnvironmentEntry != nil {
						environment.ArmorEnvironmentEntry = &tfTypes.ArmorEnvironmentEntry{}
						environment.ArmorEnvironmentEntry.Name = types.StringValue(environmentItem.ArmorEnvironmentEntry.Name)
						environment.ArmorEnvironmentEntry.Value = types.StringValue(environmentItem.ArmorEnvironmentEntry.Value)
					}

					r.Config.Armor.ClassifierContext.AutoMode.Environment = append(r.Config.Armor.ClassifierContext.AutoMode.Environment, environment)
				}
				r.Config.Armor.ClassifierContext.AutoMode.HardDeny = []tfTypes.HardDeny{}

				for _, hardDenyItem := range resp.Config.Armor.ClassifierContext.AutoMode.HardDeny {
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

					r.Config.Armor.ClassifierContext.AutoMode.HardDeny = append(r.Config.Armor.ClassifierContext.AutoMode.HardDeny, hardDeny)
				}
				r.Config.Armor.ClassifierContext.AutoMode.SoftDeny = []tfTypes.SoftDeny{}

				for _, softDenyItem := range resp.Config.Armor.ClassifierContext.AutoMode.SoftDeny {
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

					r.Config.Armor.ClassifierContext.AutoMode.SoftDeny = append(r.Config.Armor.ClassifierContext.AutoMode.SoftDeny, softDeny)
				}
			}
			r.Config.Armor.ClassifierContext.V = types.Float64Value(resp.Config.Armor.ClassifierContext.V)
		}
		if resp.Config.Armor.Mode != nil {
			r.Config.Armor.Mode = types.StringValue(string(*resp.Config.Armor.Mode))
		} else {
			r.Config.Armor.Mode = types.StringNull()
		}
		if resp.Config.Armor.OnError != nil {
			r.Config.Armor.OnError = types.StringValue(string(*resp.Config.Armor.OnError))
		} else {
			r.Config.Armor.OnError = types.StringNull()
		}
		r.Config.Armor.UseCcStyleDefaultRules = types.BoolPointerValue(resp.Config.Armor.UseCcStyleDefaultRules)
	}
	r.Config.BroadcastModelArmorFlaggedRequests = types.BoolPointerValue(resp.Config.BroadcastModelArmorFlaggedRequests)
	r.Config.ContentFilterBlocksRequests = types.BoolPointerValue(resp.Config.ContentFilterBlocksRequests)
	if resp.Config.ContentFilterBuiltins != nil {
		r.Config.ContentFilterBuiltins = []tfTypes.ContentFilterBuiltin{}

		for _, contentFilterBuiltinsItem := range resp.Config.ContentFilterBuiltins {
			var contentFilterBuiltins tfTypes.ContentFilterBuiltin

			contentFilterBuiltins.Action = types.StringValue(string(contentFilterBuiltinsItem.Action))
			contentFilterBuiltins.Label = types.StringPointerValue(contentFilterBuiltinsItem.Label)
			if contentFilterBuiltinsItem.ScanScope != nil {
				contentFilterBuiltins.ScanScope = types.StringValue(string(*contentFilterBuiltinsItem.ScanScope))
			} else {
				contentFilterBuiltins.ScanScope = types.StringNull()
			}
			contentFilterBuiltins.Slug = types.StringValue(string(contentFilterBuiltinsItem.Slug))

			r.Config.ContentFilterBuiltins = append(r.Config.ContentFilterBuiltins, contentFilterBuiltins)
		}
	} else {
		r.Config.ContentFilterBuiltins = nil
	}
	if resp.Config.ContentFilterPatterns != nil {
		r.Config.ContentFilterPatterns = make([]types.String, 0, len(resp.Config.ContentFilterPatterns))
		for _, v := range resp.Config.ContentFilterPatterns {
			r.Config.ContentFilterPatterns = append(r.Config.ContentFilterPatterns, types.StringValue(v))
		}
	} else {
		r.Config.ContentFilterPatterns = nil
	}
	if resp.Config.ContentFilters != nil {
		r.Config.ContentFilters = []tfTypes.ContentFilter{}

		for _, contentFiltersItem := range resp.Config.ContentFilters {
			var contentFilters tfTypes.ContentFilter

			contentFilters.Action = types.StringValue(string(contentFiltersItem.Action))
			contentFilters.Label = types.StringPointerValue(contentFiltersItem.Label)
			contentFilters.Pattern = types.StringValue(contentFiltersItem.Pattern)

			r.Config.ContentFilters = append(r.Config.ContentFilters, contentFilters)
		}
	} else {
		r.Config.ContentFilters = nil
	}
	r.Config.EnableModelArmorInjectionProtection = types.BoolPointerValue(resp.Config.EnableModelArmorInjectionProtection)
	if resp.Config.LakeraInjectionProtectionMode != nil {
		r.Config.LakeraInjectionProtectionMode = types.StringValue(string(*resp.Config.LakeraInjectionProtectionMode))
	} else {
		r.Config.LakeraInjectionProtectionMode = types.StringNull()
	}
	if resp.Config.ModelArmorInjectionConfidenceLevel != nil {
		r.Config.ModelArmorInjectionConfidenceLevel = types.StringValue(string(*resp.Config.ModelArmorInjectionConfidenceLevel))
	} else {
		r.Config.ModelArmorInjectionConfidenceLevel = types.StringNull()
	}
	if resp.Config.ModelArmorInjectionProtectionMode != nil {
		r.Config.ModelArmorInjectionProtectionMode = types.StringValue(string(*resp.Config.ModelArmorInjectionProtectionMode))
	} else {
		r.Config.ModelArmorInjectionProtectionMode = types.StringNull()
	}
	if resp.Config.PiiFilters != nil {
		r.Config.PiiFilters = []tfTypes.PiiFilter{}

		for _, piiFiltersItem := range resp.Config.PiiFilters {
			var piiFilters tfTypes.PiiFilter

			piiFilters.Action = types.StringValue(string(piiFiltersItem.Action))
			piiFilters.EntityType = types.StringValue(piiFiltersItem.EntityType)
			piiFilters.ScoreThreshold = types.Float64PointerValue(piiFiltersItem.ScoreThreshold)

			r.Config.PiiFilters = append(r.Config.PiiFilters, piiFilters)
		}
	} else {
		r.Config.PiiFilters = nil
	}
	if resp.Config.PlatformImageModeratedProviders != nil {
		r.Config.PlatformImageModeratedProviders = make([]types.String, 0, len(resp.Config.PlatformImageModeratedProviders))
		for _, v := range resp.Config.PlatformImageModeratedProviders {
			r.Config.PlatformImageModeratedProviders = append(r.Config.PlatformImageModeratedProviders, types.StringValue(v))
		}
	} else {
		r.Config.PlatformImageModeratedProviders = nil
	}
	if resp.Config.PlatformModeratedProviders != nil {
		r.Config.PlatformModeratedProviders = make([]types.String, 0, len(resp.Config.PlatformModeratedProviders))
		for _, v := range resp.Config.PlatformModeratedProviders {
			r.Config.PlatformModeratedProviders = append(r.Config.PlatformModeratedProviders, types.StringValue(v))
		}
	} else {
		r.Config.PlatformModeratedProviders = nil
	}
}

// toSharedAccountGuardrailPolicyRequest renders the write body for both create and
// update.
//
// The route replaces a policy and its coverage wholesale, so every field travels on
// every write and an omitted field is a clear rather than a no-op: a PUT whose body
// carried no `workspace_ids` left the policy covering no workspaces at all, measured
// 2026-11-18. The two exclusivity rules the route enforces are applied here as well as at
// plan time, so a merged state holding a stale value cannot reach it:
//
//   - while `is_default` is true, `workspace_ids` is not sent. The route answers that
//     combination with 400 "The account default covers workspaces through opt-outs",
//     and for a default the read's `workspace_ids` is resolved coverage rather than
//     configuration.
//   - while `is_default` is false, `opted_out_workspace_ids` is not sent, because the
//     route answers that with 400 "Only the account default can have default assignment
//     fields". Flipping a default off therefore drops its opt-outs.
//
// `is_default` itself always travels, as an explicit true or false, because the route
// treats an absent value as no change while the provider's state always knows which one
// the configuration asked for.
func (r *GuardrailPolicyAccountModerationResourceModel) toSharedAccountGuardrailPolicyRequest() (*shared.ModerationAccountGuardrailPolicyRequest, diag.Diagnostics) {
	var diags diag.Diagnostics

	var armor *shared.Armor
	if r.Config.Armor != nil {
		var classifierContext *shared.ArmorContext
		if r.Config.Armor.ClassifierContext != nil {
			var autoMode *shared.ArmorAutoMode
			if r.Config.Armor.ClassifierContext.AutoMode != nil {
				allow := make([]shared.Allow, 0, len(r.Config.Armor.ClassifierContext.AutoMode.Allow))
				for allowItem := range r.Config.Armor.ClassifierContext.AutoMode.Allow {
					if !r.Config.Armor.ClassifierContext.AutoMode.Allow[allowItem].Str.IsUnknown() && !r.Config.Armor.ClassifierContext.AutoMode.Allow[allowItem].Str.IsNull() {
						str := r.Config.Armor.ClassifierContext.AutoMode.Allow[allowItem].Str.ValueString()

						allow = append(allow, shared.Allow{
							Str: &str,
						})
					}
					if r.Config.Armor.ClassifierContext.AutoMode.Allow[allowItem].ArmorRule != nil {
						id := r.Config.Armor.ClassifierContext.AutoMode.Allow[allowItem].ArmorRule.ID.ValueString()

						instructions := r.Config.Armor.ClassifierContext.AutoMode.Allow[allowItem].ArmorRule.Instructions.ValueString()

						name := new(string)
						if !r.Config.Armor.ClassifierContext.AutoMode.Allow[allowItem].ArmorRule.Name.IsUnknown() && !r.Config.Armor.ClassifierContext.AutoMode.Allow[allowItem].ArmorRule.Name.IsNull() {
							*name = r.Config.Armor.ClassifierContext.AutoMode.Allow[allowItem].ArmorRule.Name.ValueString()
						} else {
							name = nil
						}
						threshold := new(float64)
						if !r.Config.Armor.ClassifierContext.AutoMode.Allow[allowItem].ArmorRule.Threshold.IsUnknown() && !r.Config.Armor.ClassifierContext.AutoMode.Allow[allowItem].ArmorRule.Threshold.IsNull() {
							*threshold = r.Config.Armor.ClassifierContext.AutoMode.Allow[allowItem].ArmorRule.Threshold.ValueFloat64()
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
				environment := make([]shared.Environment, 0, len(r.Config.Armor.ClassifierContext.AutoMode.Environment))
				for environmentItem := range r.Config.Armor.ClassifierContext.AutoMode.Environment {
					if !r.Config.Armor.ClassifierContext.AutoMode.Environment[environmentItem].Str.IsUnknown() && !r.Config.Armor.ClassifierContext.AutoMode.Environment[environmentItem].Str.IsNull() {
						str1 := r.Config.Armor.ClassifierContext.AutoMode.Environment[environmentItem].Str.ValueString()

						environment = append(environment, shared.Environment{
							Str: &str1,
						})
					}
					if r.Config.Armor.ClassifierContext.AutoMode.Environment[environmentItem].ArmorEnvironmentEntry != nil {
						name1 := r.Config.Armor.ClassifierContext.AutoMode.Environment[environmentItem].ArmorEnvironmentEntry.Name.ValueString()

						value := r.Config.Armor.ClassifierContext.AutoMode.Environment[environmentItem].ArmorEnvironmentEntry.Value.ValueString()

						armorEnvironmentEntry := shared.ArmorEnvironmentEntry{
							Name:  name1,
							Value: value,
						}
						environment = append(environment, shared.Environment{
							ArmorEnvironmentEntry: &armorEnvironmentEntry,
						})
					}
				}
				hardDeny := make([]shared.HardDeny, 0, len(r.Config.Armor.ClassifierContext.AutoMode.HardDeny))
				for hardDenyItem := range r.Config.Armor.ClassifierContext.AutoMode.HardDeny {
					if !r.Config.Armor.ClassifierContext.AutoMode.HardDeny[hardDenyItem].Str.IsUnknown() && !r.Config.Armor.ClassifierContext.AutoMode.HardDeny[hardDenyItem].Str.IsNull() {
						str2 := r.Config.Armor.ClassifierContext.AutoMode.HardDeny[hardDenyItem].Str.ValueString()

						hardDeny = append(hardDeny, shared.HardDeny{
							Str: &str2,
						})
					}
					if r.Config.Armor.ClassifierContext.AutoMode.HardDeny[hardDenyItem].ArmorRule != nil {
						id1 := r.Config.Armor.ClassifierContext.AutoMode.HardDeny[hardDenyItem].ArmorRule.ID.ValueString()

						instructions1 := r.Config.Armor.ClassifierContext.AutoMode.HardDeny[hardDenyItem].ArmorRule.Instructions.ValueString()

						name2 := new(string)
						if !r.Config.Armor.ClassifierContext.AutoMode.HardDeny[hardDenyItem].ArmorRule.Name.IsUnknown() && !r.Config.Armor.ClassifierContext.AutoMode.HardDeny[hardDenyItem].ArmorRule.Name.IsNull() {
							*name2 = r.Config.Armor.ClassifierContext.AutoMode.HardDeny[hardDenyItem].ArmorRule.Name.ValueString()
						} else {
							name2 = nil
						}
						threshold1 := new(float64)
						if !r.Config.Armor.ClassifierContext.AutoMode.HardDeny[hardDenyItem].ArmorRule.Threshold.IsUnknown() && !r.Config.Armor.ClassifierContext.AutoMode.HardDeny[hardDenyItem].ArmorRule.Threshold.IsNull() {
							*threshold1 = r.Config.Armor.ClassifierContext.AutoMode.HardDeny[hardDenyItem].ArmorRule.Threshold.ValueFloat64()
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
				softDeny := make([]shared.SoftDeny, 0, len(r.Config.Armor.ClassifierContext.AutoMode.SoftDeny))
				for softDenyItem := range r.Config.Armor.ClassifierContext.AutoMode.SoftDeny {
					if !r.Config.Armor.ClassifierContext.AutoMode.SoftDeny[softDenyItem].Str.IsUnknown() && !r.Config.Armor.ClassifierContext.AutoMode.SoftDeny[softDenyItem].Str.IsNull() {
						str3 := r.Config.Armor.ClassifierContext.AutoMode.SoftDeny[softDenyItem].Str.ValueString()

						softDeny = append(softDeny, shared.SoftDeny{
							Str: &str3,
						})
					}
					if r.Config.Armor.ClassifierContext.AutoMode.SoftDeny[softDenyItem].ArmorRule != nil {
						id2 := r.Config.Armor.ClassifierContext.AutoMode.SoftDeny[softDenyItem].ArmorRule.ID.ValueString()

						instructions2 := r.Config.Armor.ClassifierContext.AutoMode.SoftDeny[softDenyItem].ArmorRule.Instructions.ValueString()

						name3 := new(string)
						if !r.Config.Armor.ClassifierContext.AutoMode.SoftDeny[softDenyItem].ArmorRule.Name.IsUnknown() && !r.Config.Armor.ClassifierContext.AutoMode.SoftDeny[softDenyItem].ArmorRule.Name.IsNull() {
							*name3 = r.Config.Armor.ClassifierContext.AutoMode.SoftDeny[softDenyItem].ArmorRule.Name.ValueString()
						} else {
							name3 = nil
						}
						threshold2 := new(float64)
						if !r.Config.Armor.ClassifierContext.AutoMode.SoftDeny[softDenyItem].ArmorRule.Threshold.IsUnknown() && !r.Config.Armor.ClassifierContext.AutoMode.SoftDeny[softDenyItem].ArmorRule.Threshold.IsNull() {
							*threshold2 = r.Config.Armor.ClassifierContext.AutoMode.SoftDeny[softDenyItem].ArmorRule.Threshold.ValueFloat64()
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
			v := r.Config.Armor.ClassifierContext.V.ValueFloat64()

			additionalProperties := make(map[string]interface{})
			for additionalPropertiesKey := range r.Config.Armor.ClassifierContext.AdditionalProperties {
				var additionalPropertiesInst interface{}
				_ = json.Unmarshal([]byte(r.Config.Armor.ClassifierContext.AdditionalProperties[additionalPropertiesKey].ValueString()), &additionalPropertiesInst)
				additionalProperties[additionalPropertiesKey] = additionalPropertiesInst
			}
			classifierContext = &shared.ArmorContext{
				AutoMode:             autoMode,
				V:                    v,
				AdditionalProperties: additionalProperties,
			}
		}
		mode := new(shared.GuardrailModerationPolicyConfigMode)
		if !r.Config.Armor.Mode.IsUnknown() && !r.Config.Armor.Mode.IsNull() {
			*mode = shared.GuardrailModerationPolicyConfigMode(r.Config.Armor.Mode.ValueString())
		} else {
			mode = nil
		}
		onError := new(shared.GuardrailModerationPolicyConfigOnError)
		if !r.Config.Armor.OnError.IsUnknown() && !r.Config.Armor.OnError.IsNull() {
			*onError = shared.GuardrailModerationPolicyConfigOnError(r.Config.Armor.OnError.ValueString())
		} else {
			onError = nil
		}
		useCcStyleDefaultRules := new(bool)
		if !r.Config.Armor.UseCcStyleDefaultRules.IsUnknown() && !r.Config.Armor.UseCcStyleDefaultRules.IsNull() {
			*useCcStyleDefaultRules = r.Config.Armor.UseCcStyleDefaultRules.ValueBool()
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
	if !r.Config.BroadcastModelArmorFlaggedRequests.IsUnknown() && !r.Config.BroadcastModelArmorFlaggedRequests.IsNull() {
		*broadcastModelArmorFlaggedRequests = r.Config.BroadcastModelArmorFlaggedRequests.ValueBool()
	} else {
		broadcastModelArmorFlaggedRequests = nil
	}
	contentFilterBlocksRequests := new(bool)
	if !r.Config.ContentFilterBlocksRequests.IsUnknown() && !r.Config.ContentFilterBlocksRequests.IsNull() {
		*contentFilterBlocksRequests = r.Config.ContentFilterBlocksRequests.ValueBool()
	} else {
		contentFilterBlocksRequests = nil
	}
	var contentFilterBuiltins []shared.ContentFilterBuiltin
	if r.Config.ContentFilterBuiltins != nil {
		contentFilterBuiltins = make([]shared.ContentFilterBuiltin, 0, len(r.Config.ContentFilterBuiltins))
		for contentFilterBuiltinsIndex := range r.Config.ContentFilterBuiltins {
			action := shared.GuardrailModerationPolicyConfigContentFilterBuiltinAction(r.Config.ContentFilterBuiltins[contentFilterBuiltinsIndex].Action.ValueString())
			label := new(string)
			if !r.Config.ContentFilterBuiltins[contentFilterBuiltinsIndex].Label.IsUnknown() && !r.Config.ContentFilterBuiltins[contentFilterBuiltinsIndex].Label.IsNull() {
				*label = r.Config.ContentFilterBuiltins[contentFilterBuiltinsIndex].Label.ValueString()
			} else {
				label = nil
			}
			scanScope := new(shared.ScanScope)
			if !r.Config.ContentFilterBuiltins[contentFilterBuiltinsIndex].ScanScope.IsUnknown() && !r.Config.ContentFilterBuiltins[contentFilterBuiltinsIndex].ScanScope.IsNull() {
				*scanScope = shared.ScanScope(r.Config.ContentFilterBuiltins[contentFilterBuiltinsIndex].ScanScope.ValueString())
			} else {
				scanScope = nil
			}
			slug := shared.Slug(r.Config.ContentFilterBuiltins[contentFilterBuiltinsIndex].Slug.ValueString())
			contentFilterBuiltins = append(contentFilterBuiltins, shared.ContentFilterBuiltin{
				Action:    action,
				Label:     label,
				ScanScope: scanScope,
				Slug:      slug,
			})
		}
	}
	var contentFilterPatterns []string
	if r.Config.ContentFilterPatterns != nil {
		contentFilterPatterns = make([]string, 0, len(r.Config.ContentFilterPatterns))
		for contentFilterPatternsIndex := range r.Config.ContentFilterPatterns {
			contentFilterPatterns = append(contentFilterPatterns, r.Config.ContentFilterPatterns[contentFilterPatternsIndex].ValueString())
		}
	}
	var contentFilters []shared.ContentFilter
	if r.Config.ContentFilters != nil {
		contentFilters = make([]shared.ContentFilter, 0, len(r.Config.ContentFilters))
		for contentFiltersIndex := range r.Config.ContentFilters {
			action1 := shared.GuardrailModerationPolicyConfigContentFilterAction(r.Config.ContentFilters[contentFiltersIndex].Action.ValueString())
			label1 := new(string)
			if !r.Config.ContentFilters[contentFiltersIndex].Label.IsUnknown() && !r.Config.ContentFilters[contentFiltersIndex].Label.IsNull() {
				*label1 = r.Config.ContentFilters[contentFiltersIndex].Label.ValueString()
			} else {
				label1 = nil
			}
			pattern := r.Config.ContentFilters[contentFiltersIndex].Pattern.ValueString()

			contentFilters = append(contentFilters, shared.ContentFilter{
				Action:  action1,
				Label:   label1,
				Pattern: pattern,
			})
		}
	}
	enableModelArmorInjectionProtection := new(bool)
	if !r.Config.EnableModelArmorInjectionProtection.IsUnknown() && !r.Config.EnableModelArmorInjectionProtection.IsNull() {
		*enableModelArmorInjectionProtection = r.Config.EnableModelArmorInjectionProtection.ValueBool()
	} else {
		enableModelArmorInjectionProtection = nil
	}
	lakeraInjectionProtectionMode := new(shared.LakeraInjectionProtectionMode)
	if !r.Config.LakeraInjectionProtectionMode.IsUnknown() && !r.Config.LakeraInjectionProtectionMode.IsNull() {
		*lakeraInjectionProtectionMode = shared.LakeraInjectionProtectionMode(r.Config.LakeraInjectionProtectionMode.ValueString())
	} else {
		lakeraInjectionProtectionMode = nil
	}
	modelArmorInjectionConfidenceLevel := new(shared.ModelArmorInjectionConfidenceLevel)
	if !r.Config.ModelArmorInjectionConfidenceLevel.IsUnknown() && !r.Config.ModelArmorInjectionConfidenceLevel.IsNull() {
		*modelArmorInjectionConfidenceLevel = shared.ModelArmorInjectionConfidenceLevel(r.Config.ModelArmorInjectionConfidenceLevel.ValueString())
	} else {
		modelArmorInjectionConfidenceLevel = nil
	}
	modelArmorInjectionProtectionMode := new(shared.ModelArmorInjectionProtectionMode)
	if !r.Config.ModelArmorInjectionProtectionMode.IsUnknown() && !r.Config.ModelArmorInjectionProtectionMode.IsNull() {
		*modelArmorInjectionProtectionMode = shared.ModelArmorInjectionProtectionMode(r.Config.ModelArmorInjectionProtectionMode.ValueString())
	} else {
		modelArmorInjectionProtectionMode = nil
	}
	var piiFilters []shared.PiiFilter
	if r.Config.PiiFilters != nil {
		piiFilters = make([]shared.PiiFilter, 0, len(r.Config.PiiFilters))
		for piiFiltersIndex := range r.Config.PiiFilters {
			action2 := shared.PiiFilterAction(r.Config.PiiFilters[piiFiltersIndex].Action.ValueString())
			entityType := r.Config.PiiFilters[piiFiltersIndex].EntityType.ValueString()

			scoreThreshold := new(float64)
			if !r.Config.PiiFilters[piiFiltersIndex].ScoreThreshold.IsUnknown() && !r.Config.PiiFilters[piiFiltersIndex].ScoreThreshold.IsNull() {
				*scoreThreshold = r.Config.PiiFilters[piiFiltersIndex].ScoreThreshold.ValueFloat64()
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
	if r.Config.PlatformImageModeratedProviders != nil {
		platformImageModeratedProviders = make([]string, 0, len(r.Config.PlatformImageModeratedProviders))
		for platformImageModeratedProvidersIndex := range r.Config.PlatformImageModeratedProviders {
			platformImageModeratedProviders = append(platformImageModeratedProviders, r.Config.PlatformImageModeratedProviders[platformImageModeratedProvidersIndex].ValueString())
		}
	}
	var platformModeratedProviders []string
	if r.Config.PlatformModeratedProviders != nil {
		platformModeratedProviders = make([]string, 0, len(r.Config.PlatformModeratedProviders))
		for platformModeratedProvidersIndex := range r.Config.PlatformModeratedProviders {
			platformModeratedProviders = append(platformModeratedProviders, r.Config.PlatformModeratedProviders[platformModeratedProvidersIndex].ValueString())
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

	body := &shared.ModerationAccountGuardrailPolicyRequest{
		Config: config,
		Name:   r.Name.ValueString(),
	}

	isDefault := r.IsDefault.ValueBool()
	body.IsDefault = &isDefault

	if isDefault {
		body.OptedOutWorkspaceIds = guardrailPolicyAccountValueStrings(r.OptedOutWorkspaceIDs)
	} else {
		body.WorkspaceIds = guardrailPolicyAccountValueStrings(r.WorkspaceIDs)
	}

	return body, diags
}
