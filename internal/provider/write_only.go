package provider

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Write-only credentials (Terraform 1.11+) are read from the operation's
// configuration, are always null in plan and state, and so cannot produce a
// diff on their own. Each write-only credential is paired with a non-secret
// integer version attribute: changing the version is how a practitioner asks
// for the credential to be sent again.

// writeOnlyVersionChanged reports whether the planned value of the version
// attribute at versionPath differs from the prior state, meaning the paired
// write-only credential should be sent to the API. A null planned version means
// the practitioner is not using the write-only credential.
func writeOnlyVersionChanged(ctx context.Context, plan tfsdk.Plan, state tfsdk.State, versionPath path.Path) (bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	var planned, prior types.Int64
	diags.Append(plan.GetAttribute(ctx, versionPath, &planned)...)
	diags.Append(state.GetAttribute(ctx, versionPath, &prior)...)
	if diags.HasError() || planned.IsNull() || planned.IsUnknown() {
		return false, diags
	}

	return prior.IsNull() || prior.ValueInt64() != planned.ValueInt64(), diags
}

// jsonStringLeaves returns the string values inside s when s is a JSON string,
// object or array. Map attributes such as `config` and `config_secrets_wo`
// hold JSON-encoded values, so the credential is `"value"` in state and
// configuration but a bare `value` in the API's error text and debug dumps.
func jsonStringLeaves(s string) []string {
	if s == "" || !strings.ContainsAny(s[:1], `"{[`) {
		return nil
	}
	var decoded any
	if json.Unmarshal([]byte(s), &decoded) != nil {
		return nil
	}

	var leaves []string
	var walk func(any)
	walk = func(v any) {
		switch v := v.(type) {
		case string:
			if v != "" {
				leaves = append(leaves, v)
			}
		case []any:
			for _, item := range v {
				walk(item)
			}
		case map[string]any:
			for _, item := range v {
				walk(item)
			}
		}
	}
	walk(decoded)

	return leaves
}
