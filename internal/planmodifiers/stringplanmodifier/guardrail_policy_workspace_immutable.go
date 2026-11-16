package stringplanmodifier

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ planmodifier.String = StringGuardrailPolicyWorkspaceImmutablePlanModifier{}

// StringGuardrailPolicyWorkspaceImmutablePlanModifier holds a guardrail policy
// in the workspace it was created in.
//
// The route that updates a policy answers a body carrying a different
// workspace_id with 400 "Policy belongs to a different workspace", so an
// in-place change is an apply that cannot succeed; the modifier replaces the
// resource instead, which is the change the configuration asked for.
//
// Removing workspace_id is refused at plan time rather than planned as null.
// The attribute is Optional without Computed, so a null plan value is what the
// update would send, and an update that omits workspace_id leaves the policy
// where it is: the stored value would stay on the server while state recorded
// null. Keeping the stored value in the plan instead is not available, because
// a plan modifier may not write a value for a non-computed attribute, and the
// read that follows needs the workspace as its scope, which no response
// carries.
type StringGuardrailPolicyWorkspaceImmutablePlanModifier struct{}

// Description describes the plan modification in plain text formatting.
func (v StringGuardrailPolicyWorkspaceImmutablePlanModifier) Description(_ context.Context) string {
	return "Replaces the policy when the workspace changes, and refuses a plan that removes the workspace."
}

// MarkdownDescription describes the plan modification in Markdown formatting.
func (v StringGuardrailPolicyWorkspaceImmutablePlanModifier) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

// PlanModifyString performs the plan modification.
func (v StringGuardrailPolicyWorkspaceImmutablePlanModifier) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	// Creating or destroying the resource: nothing is stored to compare against
	// or to lose.
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	if guardrailPolicyWorkspaceRemoved(req.StateValue, req.PlanValue) {
		resp.Diagnostics.AddError(
			"workspace_id cannot be removed",
			"A guardrail policy stays in the workspace it was created in. The API answers an update that omits workspace_id by keeping the policy where it is, so removing the attribute would leave something else in charge of where the policy lives while Terraform recorded no workspace, and the read that follows needs that workspace as its scope. Keep the configured value, or declare a separate policy in the destination workspace and remove this one deliberately.",
		)
		return
	}
	if guardrailPolicyWorkspaceRequiresReplace(req.StateValue, req.PlanValue) {
		resp.RequiresReplace = true
	}
}

// GuardrailPolicyWorkspaceImmutable returns a plan modifier that replaces a
// guardrail policy whose workspace_id changes and refuses a plan that removes
// it.
func GuardrailPolicyWorkspaceImmutable() planmodifier.String {
	return StringGuardrailPolicyWorkspaceImmutablePlanModifier{}
}

// guardrailPolicyWorkspaceRemoved reports whether a stored workspace is absent
// from the plan, which an update would not act on.
func guardrailPolicyWorkspaceRemoved(state, plan types.String) bool {
	return !state.IsNull() && !state.IsUnknown() && plan.IsNull()
}

// guardrailPolicyWorkspaceRequiresReplace reports whether the configuration
// points at a workspace other than the stored one.
func guardrailPolicyWorkspaceRequiresReplace(state, plan types.String) bool {
	if state.IsNull() || state.IsUnknown() || plan.IsNull() || plan.IsUnknown() {
		return false
	}
	return !plan.Equal(state)
}
