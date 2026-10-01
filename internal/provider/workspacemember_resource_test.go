package provider

import "testing"

func TestParseWorkspaceMemberID(t *testing.T) {
	workspaceID, userID, err := parseWorkspaceMemberID("ws-1/user_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if workspaceID != "ws-1" || userID != "user_1" {
		t.Fatalf("got (%q, %q)", workspaceID, userID)
	}
	if got := workspaceMemberID(workspaceID, userID); got != "ws-1/user_1" {
		t.Fatalf("round trip = %q", got)
	}

	for _, id := range []string{"", "ws-1", "ws-1/", "/user_1", "ws-1/user_1/extra"} {
		if _, _, err := parseWorkspaceMemberID(id); err == nil {
			t.Errorf("parseWorkspaceMemberID(%q) returned no error", id)
		}
	}
}
