//lint:file-ignore U1000 Called only from persistent edits to the three generated guardrail policy resources, which the generator lints before applying them.

package provider

import (
	"fmt"
	"strings"
)

// guardrailPolicyImportID splits a guardrail policy import identifier into its
// workspace scope and its policy id.
//
// Read needs a workspace scope. The route resolves it from a `workspace_id`
// query parameter, falling back to the key's own workspace, and that fallback
// exists only for a workspace-scoped key: an organization management key that
// omits the parameter gets a 400. The API never returns the workspace, so state
// cannot recover it afterwards and the import identifier is the only channel
// that can supply it. Both forms are accepted, and what the user needs decides
// which one they write:
//
//	<workspace_id>/<policy_id>  an organization management key, which has no
//	                            default workspace to fall back to
//	<policy_id>                 a workspace-scoped key, whose own workspace the
//	                            API applies by default
func guardrailPolicyImportID(id string) (workspaceID, policyID string, err error) {
	if !strings.Contains(id, "/") {
		if id == "" {
			return "", "", fmt.Errorf("expected an import ID in the format <workspace_id>/<policy_id> or a policy id, got %q", id)
		}
		return "", id, nil
	}

	parts := strings.Split(id, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("expected an import ID in the format <workspace_id>/<policy_id>, got %q", id)
	}
	return parts[0], parts[1], nil
}
