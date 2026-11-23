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
//
// Two cases keep the update path rather than the replacement. A policy whose
// state records no workspace, which is what a create under a workspace-scoped
// key and an import by bare id both produce, has nothing to compare against,
// and a value Terraform cannot resolve until apply is unknown at plan time; in
// both the update reaches the API, which answers a change with 400. A policy
// that replaced a legacy default guardrail is one the API refuses to delete, so
// a replacement of one fails at the destroy half.
type StringGuardrailPolicyWorkspaceImmutablePlanModifier struct {
	// assignment words the descriptions and the removal diagnostic for an
	// assignment rather than for a policy. An assignment's update path never
	// reaches the API, so the policy text would describe a route the resource
	// does not call, and its scope is what the read and the destroy need.
	assignment bool
}

// Description describes the plan modification in plain text formatting.
func (v StringGuardrailPolicyWorkspaceImmutablePlanModifier) Description(_ context.Context) string {
	if v.assignment {
		return "Replaces the assignment when the workspace changes or is added, and refuses a plan that removes the workspace."
	}
	return "Replaces the policy when the workspace changes or is added, and refuses a plan that removes the workspace."
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
		// Attribute-scoped, matching how the sibling resources report a
		// resource that belongs to another workspace.
		detail := "A guardrail policy stays in the workspace it was created in. The API answers an update that omits workspace_id by keeping the policy where it is, so removing the attribute would leave something else in charge of where the policy lives while Terraform recorded no workspace, and the read that follows needs that workspace as its scope. Keep the configured value, or declare a separate policy in the destination workspace and remove this one deliberately."
		if v.assignment {
			detail = "An assignment is addressed with a workspace scope, and no response carries that scope back, so state is the only place it is recorded: removing the attribute would leave a later read resolving the scope from the credential instead of from the assignment. Keep the configured value, or destroy this assignment and declare the one you mean."
		}
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"workspace_id cannot be removed",
			detail,
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

// GuardrailPolicyAssignmentWorkspaceImmutable returns the same behaviour worded
// for an assignment: a changed scope replaces the resource, and a plan that
// removes it is refused with a diagnostic that names the assignment and the read
// that needs the scope.
func GuardrailPolicyAssignmentWorkspaceImmutable() planmodifier.String {
	return StringGuardrailPolicyWorkspaceImmutablePlanModifier{assignment: true}
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
