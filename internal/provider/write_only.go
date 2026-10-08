package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
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
	trimmed := strings.TrimLeft(s, " \t\r\n")
	if trimmed == "" || !strings.ContainsAny(trimmed[:1], `"{[`) {
		return nil
	}
	var decoded any
	if json.Unmarshal([]byte(trimmed), &decoded) != nil {
		return nil
	}

	return stringLeaves(decoded)
}

// stringLeaves returns the non-empty strings inside a decoded JSON value.
func stringLeaves(decoded any) []string {
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

// secretJSONValidator checks that a write-only value is valid JSON whose
// strings are long enough to redact. Unlike validators.IsValidJSON and
// jsontypes.NormalizedType, its diagnostics never include the value.
type secretJSONValidator struct{}

var _ validator.String = secretJSONValidator{}

func (secretJSONValidator) Description(context.Context) string {
	return fmt.Sprintf("value must be valid JSON, and each non-empty string in it at least %d characters long", minSensitiveValueLength)
}

func (v secretJSONValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (secretJSONValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	var decoded any
	if json.Unmarshal([]byte(req.ConfigValue.ValueString()), &decoded) != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid JSON", fmt.Sprintf("The value of %s is not valid JSON. The value is not shown because it is write-only.", req.Path))
		return
	}
	for _, leaf := range stringLeaves(decoded) {
		if utf8.RuneCountInString(leaf) < minSensitiveValueLength {
			resp.Diagnostics.AddAttributeError(req.Path, "Credential too short",
				fmt.Sprintf("The value of %s contains a string shorter than %d characters. Values that short cannot be redacted from logs and error messages.", req.Path, minSensitiveValueLength))
			return
		}
	}
}

// redactConfigObjectsInBody returns a JSON body with the credential-bearing
// fields of every `config` object replaced, keeping only allowlisted public
// fields. Write-only credentials are not in state, so value-based redaction
// cannot find them in a refresh or data source read; this hides them
// structurally. Bodies of other endpoints, and non-JSON bodies, are returned
// unchanged.
func redactConfigObjectsInBody(req *http.Request, body string) string {
	if req == nil || req.URL == nil || !strings.Contains(req.URL.Path, "/observability/destinations") {
		return body
	}
	var decoded any
	if json.Unmarshal([]byte(body), &decoded) != nil {
		return body
	}
	if !redactConfigObjects(decoded) {
		return body
	}
	redacted, err := json.Marshal(decoded)
	if err != nil {
		return body
	}
	return string(redacted)
}

func redactConfigObjects(v any) bool {
	changed := false
	switch v := v.(type) {
	case []any:
		for _, item := range v {
			changed = redactConfigObjects(item) || changed
		}
	case map[string]any:
		for key, item := range v {
			if cfg, ok := item.(map[string]any); ok && key == "config" {
				for field := range cfg {
					if !isObservabilityPublicConfigKey(field) {
						cfg[field] = "(sensitive)"
						changed = true
					}
				}
				continue
			}
			changed = redactConfigObjects(item) || changed
		}
	}
	return changed
}

// redactResponseBodyForDump replaces the response body with its structurally
// redacted form before the response is dumped into a diagnostic. The SDK has
// already consumed the body by then.
func redactResponseBodyForDump(res *http.Response) {
	if res == nil || res.Body == nil {
		return
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return
	}
	redacted := redactConfigObjectsInBody(res.Request, string(body))
	res.Body = io.NopCloser(strings.NewReader(redacted))
	res.ContentLength = int64(len(redacted))
}
