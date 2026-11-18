package provider

import (
	"context"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/operations"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/shared"
)

// The account (workspace-wide) guardrail policy family is hand-written, and this file
// carries the part its three resources share: the read, the coverage rules and the
// error messages.
//
// Why hand-written: the route publishes POST /guardrails/policies/account,
// GET /guardrails/policies/account (a list) and PUT and DELETE on
// /guardrails/policies/account/{id}, and **no get-by-id**, so a generated resource has
// no read to build. Everything below is measured against that route on 2026-11-18; the
// probe transcripts are in the pull request that added these resources.
const (
	guardrailPolicyAccountTypeBudget            = "budget"
	guardrailPolicyAccountTypeModelAvailability = "model_availability"
	guardrailPolicyAccountTypeModeration        = "moderation"
)

// guardrailPolicyAccountLookup lists every account policy and returns the one whose id
// matches, as the union row the list carries. It is the whole read path for this family.
//
// Three measured facts shape it:
//
//   - Omitting both `offset` and `limit` returns the full list, so the read passes
//     neither and cannot miss a policy behind a default page size.
//   - The id is resolved per policy type: a PUT whose body named a different
//     `policy_type` answered 404 "Account policy not found". A row found by id under
//     another type is therefore a mismatched import rather than the policy this resource
//     manages, and that is reported instead of hydrated.
//   - The route answers 404 for an organization without the guardrails backend, which a
//     status alone cannot separate from a genuine miss. A non-200 is an error, never an
//     absence: a resource that dropped itself from state on that read would plan a
//     recreate against a policy that still exists.
func guardrailPolicyAccountLookup(ctx context.Context, client *sdk.OpenRouter, policyType, id string) (*shared.AccountGuardrailPolicy, bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	res, err := client.GuardrailPolicies.ListAccountPolicies(ctx, operations.ListAccountPoliciesRequest{})
	if err != nil {
		diags.AddError("failure to invoke API", redactSensitiveValues(ctx, err.Error()))
		if res != nil && res.RawResponse != nil {
			diags.AddError("unexpected http request/response", debugResponse(res.RawResponse))
		}
		return nil, false, diags
	}
	if res == nil {
		diags.AddError("unexpected response from API", "the account policy list returned no response")
		return nil, false, diags
	}
	if res.StatusCode != 200 {
		diags.AddError(
			fmt.Sprintf("unexpected response from API. Got an unexpected response code %v", res.StatusCode),
			guardrailPolicyAccountResponseMessage(res.BadRequestResponse, nil, res.NotFoundResponse, res.RawResponse),
		)
		return nil, false, diags
	}
	if res.ListAccountGuardrailPoliciesResponse == nil {
		diags.AddError("unexpected response from API. Got an unexpected response body", debugResponse(res.RawResponse))
		return nil, false, diags
	}

	for i := range res.ListAccountGuardrailPoliciesResponse.Data {
		policy := res.ListAccountGuardrailPoliciesResponse.Data[i]
		if guardrailPolicyAccountID(policy) != id {
			continue
		}
		if string(policy.Type) != policyType {
			diags.AddError(
				"policy type does not match this resource",
				fmt.Sprintf(
					"Policy %s is a %q policy, and this resource manages %q. Import it into the resource for its type.",
					id, string(policy.Type), policyType,
				),
			)
			return nil, false, diags
		}
		return &policy, true, diags
	}

	return nil, false, diags
}

// guardrailPolicyAccountID reads the policy id out of a union row, which is how both the
// write responses and the list carry it.
func guardrailPolicyAccountID(policy shared.AccountGuardrailPolicy) string {
	switch policy.Type {
	case shared.AccountGuardrailPolicyTypeBudget:
		if policy.BudgetAccountGuardrailPolicy != nil {
			return policy.BudgetAccountGuardrailPolicy.ID
		}
	case shared.AccountGuardrailPolicyTypeModelAvailability:
		if policy.ModelAvailabilityAccountGuardrailPolicy != nil {
			return policy.ModelAvailabilityAccountGuardrailPolicy.ID
		}
	case shared.AccountGuardrailPolicyTypeModeration:
		if policy.ModerationAccountGuardrailPolicy != nil {
			return policy.ModerationAccountGuardrailPolicy.ID
		}
	}
	return ""
}

// guardrailPolicyAccountOptOutsModifier drops a carried opt-out list from the plan when the
// policy stops being the account default.
//
// `opted_out_workspace_ids` is Optional+Computed, so Terraform carries the state's value
// into the plan when the configuration leaves it out, and the write then sends it. For a
// policy that is no longer the default the list is meaningless: the route answers it with
// 400 "Only the account default can have default assignment fields". The configuration is
// the only place a carried list is distinguishable from a written one, which is why this is
// a plan modifier rather than a check in the write path: clearing it here keeps the plan
// equal to what will be stored, so the apply and the read agree on the same list. A list
// the configuration does write is left alone, and the config validator refuses that
// combination instead.
type guardrailPolicyAccountOptOutsModifier struct{}

func (guardrailPolicyAccountOptOutsModifier) Description(context.Context) string {
	return "drops a carried opted-out workspace list when the policy is not the account default"
}

func (guardrailPolicyAccountOptOutsModifier) MarkdownDescription(context.Context) string {
	return "drops a carried opted-out workspace list when the policy is not the account default"
}

func (guardrailPolicyAccountOptOutsModifier) PlanModifyList(ctx context.Context, req planmodifier.ListRequest, resp *planmodifier.ListResponse) {
	configured := !req.ConfigValue.IsNull() && !req.ConfigValue.IsUnknown() && len(req.ConfigValue.Elements()) > 0
	if configured {
		return
	}

	var isDefault types.Bool
	if diags := req.Plan.GetAttribute(ctx, path.Root("is_default"), &isDefault); diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}
	// An unknown flag is not a false one: on a create the configuration may be about to
	// make this policy the default, so the value stays as planned and the write path
	// decides from the flag that actually travels.
	if isDefault.IsNull() || isDefault.IsUnknown() || isDefault.ValueBool() {
		return
	}
	if req.PlanValue.IsNull() {
		return
	}
	if !req.PlanValue.IsUnknown() && len(req.PlanValue.Elements()) == 0 {
		return
	}

	// Unknown counts as "nothing the configuration asked for": measured in the test suite,
	// the plan marks this attribute unknown when the configuration omits it and the policy
	// stops being the default, and the write path is handed a list it must not send. Pinning
	// it to an empty list keeps the plan equal to what will be stored, so the read agrees.
	resp.PlanValue = types.ListValueMust(types.StringType, []attr.Value{})
}

// guardrailPolicyAccountCoverageValidator refuses the two coverage combinations the
// route answers with 400, at plan time rather than at apply.
//
// It is a config-level validator rather than two attribute validators because each rule
// is about the relationship between attributes:
//
//	is_default = true  with workspace_ids            -> "The account default covers workspaces through opt-outs"
//	is_default = false with opted_out_workspace_ids  -> "Only the account default can have default assignment fields"
//
// The quoting is deliberate: the wording is the API's, measured on 2026-11-18, so a
// reader who searches for it lands on the same sentence.
type guardrailPolicyAccountCoverageValidator struct{}

func (guardrailPolicyAccountCoverageValidator) Description(context.Context) string {
	return "refuses a coverage combination the account policy route rejects"
}

func (guardrailPolicyAccountCoverageValidator) MarkdownDescription(context.Context) string {
	return "refuses a coverage combination the account policy route rejects"
}

func (guardrailPolicyAccountCoverageValidator) ValidateResource(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	// Every attribute here is read as a framework value rather than a Go scalar, because a
	// config-level validator sees what the configuration says and nothing else:
	// `is_default` is Optional+Computed, so an omitted flag arrives null even when the
	// state holds true. Both rules are conditional on the flag, so an omitted flag means
	// neither can be decided here. The route decides it instead, and its own sentence is
	// what the error path renders.
	var baseline types.Bool
	if resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("is_default"), &baseline)...); resp.Diagnostics.HasError() {
		return
	}
	if baseline.IsNull() || baseline.IsUnknown() {
		return
	}

	var scoped types.List
	if resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("workspace_ids"), &scoped)...); resp.Diagnostics.HasError() {
		return
	}
	scopedSet := !scoped.IsNull() && !scoped.IsUnknown() && len(scoped.Elements()) > 0

	if baseline.ValueBool() {
		if scopedSet {
			resp.Diagnostics.AddAttributeError(
				path.Root("workspace_ids"),
				"The account default covers workspaces through opt-outs",
				"The account default already covers every workspace except the ones in `opted_out_workspace_ids`, so `workspace_ids` cannot be set alongside `is_default = true`. Drop one of the two.",
			)
		}
		return
	}

	var optedOut types.List
	if resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("opted_out_workspace_ids"), &optedOut)...); resp.Diagnostics.HasError() {
		return
	}
	if !optedOut.IsNull() && !optedOut.IsUnknown() && len(optedOut.Elements()) > 0 {
		resp.Diagnostics.AddAttributeError(
			path.Root("opted_out_workspace_ids"),
			"Only the account default can have default assignment fields",
			"`opted_out_workspace_ids` describes the account default's coverage, so it cannot be set while `is_default` is false. Set `is_default = true`, or move the excluded workspaces out of `workspace_ids` instead.",
		)
	}
}

// guardrailPolicyAccountResponseMessage renders the API's own explanation for a failed
// write, so an operator sees the route's sentence rather than only a status code. Every
// message measured on these routes is prose, which is why the fallback is the response
// dump rather than a status-to-sentence table invented here.
func guardrailPolicyAccountResponseMessage(badRequest *shared.BadRequestResponse, conflict *shared.ConflictResponse, notFound *shared.NotFoundResponse, raw *http.Response) string {
	for _, message := range []string{
		guardrailPolicyAccountBadRequestMessage(badRequest),
		guardrailPolicyAccountConflictMessage(conflict),
		guardrailPolicyAccountNotFoundMessage(notFound),
	} {
		if message != "" {
			return message
		}
	}
	if raw != nil {
		return debugResponse(raw)
	}
	return "the API returned no explanation"
}

func guardrailPolicyAccountBadRequestMessage(res *shared.BadRequestResponse) string {
	if res == nil {
		return ""
	}
	return res.Error.Message
}

func guardrailPolicyAccountConflictMessage(res *shared.ConflictResponse) string {
	if res == nil {
		return ""
	}
	return res.Error.Message
}

func guardrailPolicyAccountNotFoundMessage(res *shared.NotFoundResponse) string {
	if res == nil {
		return ""
	}
	return res.Error.Message
}

// guardrailPolicyAccountValueStrings renders a framework list as the slice the request
// carries. An empty list renders as nil so the generated `omitzero` tag omits the field,
// which is the shape the route reads as "no coverage" for these two attributes.
func guardrailPolicyAccountValueStrings(values []types.String) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v.IsNull() || v.IsUnknown() {
			continue
		}
		out = append(out, v.ValueString())
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
