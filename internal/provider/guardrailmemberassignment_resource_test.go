package provider

import "testing"

func TestParseGuardrailMemberAssignmentID(t *testing.T) {
	workspaceID, guardrailID, userID, err := parseGuardrailMemberAssignmentID("ws-1/gr-1/user_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if workspaceID != "ws-1" || guardrailID != "gr-1" || userID != "user_1" {
		t.Fatalf("got (%q, %q, %q)", workspaceID, guardrailID, userID)
	}
	if got := guardrailMemberAssignmentID(workspaceID, guardrailID, userID); got != "ws-1/gr-1/user_1" {
		t.Fatalf("round trip = %q", got)
	}

	for _, id := range []string{"", "gr-1/user_1", "ws-1//user_1", "/gr-1/user_1", "ws-1/gr-1/", "ws-1/gr-1/user_1/extra"} {
		if _, _, _, err := parseGuardrailMemberAssignmentID(id); err == nil {
			t.Errorf("parseGuardrailMemberAssignmentID(%q) returned no error", id)
		}
	}
}
