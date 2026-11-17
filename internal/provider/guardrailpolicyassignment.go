package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/shared"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// guardrailPolicyAssignmentTypes are the policy types the assignment route
// accepts. The list is the request enum, not the set of routed policy families:
// `rate_limit` is accepted here even though no rate-limit policy family has a
// route of its own, and a config that names it validates and then fails at the
// API if no such policy exists.
var guardrailPolicyAssignmentTypes = []string{
	"budget",
	"model_availability",
	"moderation",
	"rate_limit",
}

func guardrailPolicyAssignmentTypeValidator() validator.String {
	return stringvalidator.OneOf(guardrailPolicyAssignmentTypes...)
}

// maxGuardrailPolicyAssignmentAttempts bounds the compare-and-swap retries. Each
// attempt is one write preceded by a fresh read, so the first write plus one
// retry absorbs the ordinary case: something else moved this subject's assignment
// for this policy type between the read and the write.
//
// The racing writer is not a sibling resource. This route sets the assignment for
// a policy type rather than adding to it — verified live: assigning a second
// policy of one type to a subject that already held one left only the second —
// so one subject holds one policy per type and no two resources of this family
// contend for the same state. A conflict that outlives the retry means another
// apply, the dashboard or another API client is writing continuously, which is
// worth reporting rather than retrying through.
const maxGuardrailPolicyAssignmentAttempts = 2

// guardrailPolicyAssignment identifies one policy's assignment to one subject.
type guardrailPolicyAssignment struct {
	// SubjectID is an API key hash or an organization member user ID.
	SubjectID string
	// PolicyType is one of guardrailPolicyAssignmentTypes.
	PolicyType string
	// PolicyID is the policy being assigned or removed.
	PolicyID string
}

// guardrailPolicyAssignmentRead is the answer to one assignment read.
type guardrailPolicyAssignmentRead struct {
	// State is the stored assignment state, nil when the route answered 404.
	State *shared.GuardrailPolicyAssignmentState
	// NotFound reports a 404. On this route that means the subject or the
	// workspace scope is absent rather than that nothing is assigned: a subject
	// with no policies of a type still answers 200 with empty lists, which the
	// live route was observed doing for all four types.
	NotFound bool
}

// guardrailPolicyAssignmentAPI is the pair of SDK calls one subject kind needs.
// The two subject kinds are different routes with their subject in a different
// field, so the shared read-modify-write below is written against these two
// functions rather than against an interface carrying every parameter.
type guardrailPolicyAssignmentAPI struct {
	// subjectPath names the configuration attribute holding the subject, so an
	// error caused by the key or the member is reported against that attribute
	// rather than against the resource as a whole. The sibling hand-written
	// assignments report a subject failure the same way.
	subjectPath path.Path
	read        func(ctx context.Context, a guardrailPolicyAssignment, workspaceID string) (guardrailPolicyAssignmentRead, diag.Diagnostics)
	// change assigns or removes one policy, reporting whether the API answered
	// 409 and what it said. That status covers both a stale expected_assignment
	// and a removal of a policy the subject does not hold, so it is never
	// interpreted; the message is carried for the report a failure ends with.
	change func(ctx context.Context, a guardrailPolicyAssignment, workspaceID string, assigned bool, expected shared.GuardrailPolicyAssignmentState) (conflict bool, conflictMessage string, diags diag.Diagnostics)
}

// assignmentHolds reports whether the subject holds the policy directly.
//
// The inherited list is deliberately ignored. A policy the subject receives
// from the workspace default is not an assignment, cannot be removed by this
// route, and is not something this resource manages: comparing against it would
// make an inherited policy read as an assignment Terraform does not own.
func assignmentHolds(state shared.GuardrailPolicyAssignmentState, policyID string) bool {
	for _, id := range state.PolicyIds {
		if id == policyID {
			return true
		}
	}
	return false
}

// reconcileGuardrailPolicyAssignment makes the stored assignment match want.
//
// The route is a compare-and-swap: a write must carry the state the caller last
// read, and a state that moved answers 409. That status is also the answer to
// removing a policy the subject does not hold, so the two conflicts cannot be
// told apart by status code. They are therefore not interpreted. Every write is
// preceded by a read that decides whether a write is needed at all, and a 409
// is resolved by reading again rather than by being treated as success or as
// fatal: reading first is what makes a destroy of an already-removed assignment
// a no-op instead of a failed apply.
func reconcileGuardrailPolicyAssignment(ctx context.Context, api guardrailPolicyAssignmentAPI, a guardrailPolicyAssignment, workspaceID string, want bool) diag.Diagnostics {
	var diags diag.Diagnostics

	for attempt := 0; ; attempt++ {
		got, readDiags := api.read(ctx, a, workspaceID)
		diags.Append(readDiags...)
		if diags.HasError() {
			return diags
		}

		if got.NotFound {
			if !want {
				// The subject or the scope went away, so nothing is left to
				// remove and the destroy completes.
				return diags
			}
			diags.AddAttributeError(
				api.subjectPath,
				"subject not found",
				fmt.Sprintf("The API does not know this subject in this workspace, so policy %q could not be assigned and nothing was assigned. The API key or the member may have been deleted, or the workspace scope may not be the one that holds it.", a.PolicyID),
			)
			return diags
		}

		if assignmentHolds(*got.State, a.PolicyID) == want {
			// Already in the wanted state, either because nothing changed since
			// the last apply or because a re-read after a conflict found it so.
			return diags
		}

		conflict, conflictMessage, changeDiags := api.change(ctx, a, workspaceID, want, *got.State)
		diags.Append(changeDiags...)
		if diags.HasError() {
			return diags
		}
		if !conflict {
			return diags
		}
		if attempt+1 >= maxGuardrailPolicyAssignmentAttempts {
			detail := "The API returned no explanation."
			if conflictMessage != "" {
				detail = fmt.Sprintf("The API said: %s.", conflictMessage)
			}
			diags.AddError(
				fmt.Sprintf("assignments changed while %s policy %q", assignmentVerb(want), a.PolicyID),
				fmt.Sprintf("The API answered 409 for the state read immediately before the write, %d times, so this apply never observed a stable state to write against. The route also answers 409 for a removal that was never an assignment, but that case is decided by the read in front of each write and does not reach here. %s Re-run once whatever else is writing this assignment stops.", maxGuardrailPolicyAssignmentAttempts, detail),
			)
			return diags
		}
	}
}

func assignmentVerb(assigned bool) string {
	if assigned {
		return "assigning"
	}
	return "removing"
}

// guardrailPolicyAssignmentSubjectGone reports whether a 400 from this route means
// the subject no longer exists rather than that the request was malformed.
//
// Measured against the live route, because the status alone does not say: a
// deleted API key answers 400 "One or more API keys were not found", a member who
// is not in the workspace answers 400 "The policy assignment subject is
// unavailable in this workspace", and a malformed request (a missing scope, an
// unknown policy type) answers 400 "Invalid guardrail policy request". Only the
// two subject messages are drift; the rest is an error the caller has to see.
//
// The route reports these cases in prose rather than in distinct codes, so a
// rewording upstream stops matching. That direction fails safe: the case falls
// back to an error rather than to a resource silently dropped from state.
func guardrailPolicyAssignmentSubjectGone(badRequest *shared.BadRequestResponse) bool {
	if badRequest == nil || badRequest.Error.Message == "" {
		return false
	}
	message := badRequest.Error.Message
	return strings.Contains(message, "API keys were not found") ||
		strings.Contains(message, "subject is unavailable in this workspace")
}

// guardrailPolicyAssignmentConflictMessage reads the API's explanation from a
// 409 body, so a failure caused by continuous concurrent writes quotes the
// route rather than only this provider's account of it.
func guardrailPolicyAssignmentConflictMessage(res *shared.ConflictResponse) string {
	if res != nil && res.Error.Message != "" {
		return res.Error.Message
	}
	return ""
}

// guardrailPolicyAssignmentID renders the identifier this resource stores: the
// subject, the policy type and the policy, which together are the assignment.
//
// The workspace is deliberately not part of it, even though the read needs one.
// The workspace is an attribute with a value of its own, and an identifier that
// changed shape depending on whether a workspace had been resolved would make
// two forms of the same assignment look like two assignments. An organization
// management key importing this identifier adds the workspace prefix; the
// missing scope is answered with 400 rather than by reading the wrong
// workspace, which is what the error message says.
func guardrailPolicyAssignmentID(a guardrailPolicyAssignment) string {
	return strings.Join([]string{a.SubjectID, a.PolicyType, a.PolicyID}, "/")
}

// parseGuardrailPolicyAssignmentID splits an import identifier, which is
// <subject>/<policy_type>/<policy_id> for a workspace-scoped key and
// <workspace_id>/<subject>/<policy_type>/<policy_id> for an organization
// management key. The subject kind is not part of the identifier because the
// resource type already decides it.
func parseGuardrailPolicyAssignmentID(id string) (workspaceID string, a guardrailPolicyAssignment, err error) {
	format := "expected an import ID in the format <key hash or member ID>/<policy_type>/<policy_id>, or <workspace_id>/<key hash or member ID>/<policy_type>/<policy_id>"

	parts := strings.Split(id, "/")
	switch len(parts) {
	case 3:
		// A workspace-scoped key's own workspace is the scope, so its
		// identifier carries none and the read omits the parameter.
	case 4:
		if parts[0] == "" {
			return "", a, fmt.Errorf("%s, got %q", format, id)
		}
		workspaceID, parts = parts[0], parts[1:]
	default:
		return "", a, fmt.Errorf("%s, got %q", format, id)
	}

	// The workspace is checked in its own branch above: an absent one is the
	// documented form for a workspace-scoped key, so it cannot be empty-checked
	// alongside the three parts that must always be present.
	for _, part := range parts {
		if part == "" {
			return "", a, fmt.Errorf("%s, got %q", format, id)
		}
	}

	a = guardrailPolicyAssignment{SubjectID: parts[0], PolicyType: parts[1], PolicyID: parts[2]}
	if !guardrailPolicyAssignmentTypeIsKnown(a.PolicyType) {
		return "", a, fmt.Errorf("policy_type %q is not one of %s", a.PolicyType, strings.Join(guardrailPolicyAssignmentTypes, ", "))
	}
	return workspaceID, a, nil
}

func guardrailPolicyAssignmentTypeIsKnown(policyType string) bool {
	for _, known := range guardrailPolicyAssignmentTypes {
		if known == policyType {
			return true
		}
	}
	return false
}

// guardrailPolicyAssignmentOptionalString renders an absent workspace as the
// omitted parameter the route documents for a workspace-scoped key, whose own
// workspace the API applies by default.
func guardrailPolicyAssignmentOptionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
