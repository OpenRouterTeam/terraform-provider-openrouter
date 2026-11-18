package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/shared"
)

// The account policy family's read is a list filtered on the policy id, because the
// route publishes no get-by-id. These unit tests drive that read directly: they pin the
// two things the Terraform-level tests cannot isolate, which are the type check that
// makes a mismatched import fail loudly and the rule that a non-200 is an error rather
// than an absence.
func accountPolicyTestClient(t *testing.T, serverURL string) *sdk.OpenRouter {
	t.Helper()
	key := "sk-or-v1-stub"
	return sdk.New(
		sdk.WithServerURL(serverURL),
		sdk.WithSecurity(shared.Security{APIKey: &key}),
	)
}

func accountPolicyListServer(t *testing.T, policies []map[string]any, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/guardrails/policies/account" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if r.URL.RawQuery != "" {
			t.Errorf("the read sent ?%s; the route returns the full list only when offset and limit are omitted", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		if status != http.StatusOK {
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{"code": status, "message": "Not Found - Resource does not exist"},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": policies, "total_count": len(policies)})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func accountPolicyRow(id, policyType, name string) map[string]any {
	return map[string]any{
		"id":                           id,
		"name":                         name,
		"policy_type":                  policyType,
		"config":                       map[string]any{},
		"created_at":                   "2026-11-18T00:00:00Z",
		"updated_at":                   "2026-11-18T00:00:00Z",
		"is_default":                   false,
		"is_synced":                    false,
		"is_workspace_default_synced":  false,
		"workspace_count":              0,
		"workspace_ids":                []any{},
		"opted_out_workspace_ids":      []any{},
	}
}

func TestGuardrailPolicyAccountLookupFindsTheRowByID(t *testing.T) {
	srv := accountPolicyListServer(t, []map[string]any{
		accountPolicyRow("stub-b", "moderation", "another type"),
		accountPolicyRow("stub-a", "model_availability", "the baseline"),
	}, http.StatusOK)
	client := accountPolicyTestClient(t, srv.URL)

	policy, found, diags := guardrailPolicyAccountLookup(context.Background(), client, "model_availability", "stub-a")
	if diags.HasError() {
		t.Fatalf("lookup reported %v", diags)
	}
	if !found || policy == nil {
		t.Fatal("lookup did not find the policy that the list carries")
	}
	if got := guardrailPolicyAccountID(*policy); got != "stub-a" {
		t.Errorf("lookup resolved id %q, want %q", got, "stub-a")
	}
}

func TestGuardrailPolicyAccountLookupRefusesAMismatchedType(t *testing.T) {
	srv := accountPolicyListServer(t, []map[string]any{
		accountPolicyRow("stub-a", "budget", "a budget policy"),
	}, http.StatusOK)
	client := accountPolicyTestClient(t, srv.URL)

	_, found, diags := guardrailPolicyAccountLookup(context.Background(), client, "moderation", "stub-a")
	if !diags.HasError() {
		t.Fatal("a policy of another type resolved without an error; importing it into the wrong resource would hydrate a row of another family")
	}
	if found {
		t.Error("a mismatched type was reported as found")
	}
	if got := diags[0].Summary(); !strings.Contains(got, "policy type does not match") {
		t.Errorf("diagnostic summary is %q, want it to name the type mismatch", got)
	}
}

func TestGuardrailPolicyAccountLookupReportsAnAbsentIDAsNotFound(t *testing.T) {
	srv := accountPolicyListServer(t, []map[string]any{
		accountPolicyRow("stub-a", "budget", "a budget policy"),
	}, http.StatusOK)
	client := accountPolicyTestClient(t, srv.URL)

	policy, found, diags := guardrailPolicyAccountLookup(context.Background(), client, "budget", "stub-missing")
	if diags.HasError() {
		t.Fatalf("an absent policy reported %v; a list that returns in full without the id is the absence signal, not an error", diags)
	}
	if found || policy != nil {
		t.Error("an absent id was reported as found")
	}
}

// TestGuardrailPolicyAccountLookupTreatsANon200AsAnError is the rule that keeps a route
// which is unavailable (the route answers 404 for an organization without the guardrails
// backend) from reading as a deleted policy: a read that treated it as absence would drop
// the resource from state and plan a recreate against a policy that still exists.
func TestGuardrailPolicyAccountLookupTreatsANon200AsAnError(t *testing.T) {
	srv := accountPolicyListServer(t, nil, http.StatusNotFound)
	client := accountPolicyTestClient(t, srv.URL)

	_, found, diags := guardrailPolicyAccountLookup(context.Background(), client, "budget", "stub-a")
	if !diags.HasError() {
		t.Fatal("a 404 from the list route resolved without an error; that read is indistinguishable from an unavailable route")
	}
	if found {
		t.Error("a failed read was reported as found")
	}
}
