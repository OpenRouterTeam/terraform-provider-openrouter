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
	key := "sk-stub-key"
	return sdk.New(
		sdk.WithServerURL(serverURL),
		sdk.WithSecurity(shared.Security{APIKey: &key}),
	)
}

func accountPolicyListServer(t *testing.T, policies []map[string]any, status int) *httptest.Server {
	t.Helper()
	return accountPolicyListServerWithTotal(t, policies, len(policies), status)
}

// accountPolicyListServerWithTotal serves a page whose length and reported `total_count`
// need not agree, which is what a clamped response looks like.
func accountPolicyListServerWithTotal(t *testing.T, policies []map[string]any, total, status int) *httptest.Server {
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
		_ = json.NewEncoder(w).Encode(map[string]any{"data": policies, "total_count": total})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func accountPolicyRow(id, policyType, name string) map[string]any {
	return map[string]any{
		"id":                          id,
		"name":                        name,
		"policy_type":                 policyType,
		"config":                      map[string]any{},
		"created_at":                  "2026-11-18T00:00:00Z",
		"updated_at":                  "2026-11-18T00:00:00Z",
		"is_default":                  false,
		"is_synced":                   false,
		"is_workspace_default_synced": false,
		"workspace_count":             0,
		"workspace_ids":               []any{},
		"opted_out_workspace_ids":     []any{},
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
//
// The summary is asserted as well as the error, because the branch below it also reports an
// error when the route answers without a parseable body: a check for an error alone passes
// with the non-200 branch deleted, which is how this case could have gone on looking like
// coverage of the status rule while covering only the nil-body one.
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
	if got := diags[0].Summary(); !strings.Contains(got, "404") {
		t.Errorf("diagnostic summary is %q, want it to name the status the route answered", got)
	}
	if got := diags[0].Detail(); got == "" {
		t.Error("the diagnostic carried no detail; an operator who sees a status alone cannot tell what the route did")
	}
}

// TestGuardrailPolicyAccountLookupRefusesATruncatedList covers a list that arrives shorter
// than the `total_count` it reports. The read asks for the whole list by sending neither
// `offset` nor `limit`, so a short page is a clamped response rather than proof that the
// policy is gone: reading it as a miss would drop the resource from state and plan a
// recreate against a policy the account still holds.
func TestGuardrailPolicyAccountLookupRefusesATruncatedList(t *testing.T) {
	srv := accountPolicyListServerWithTotal(t, []map[string]any{
		accountPolicyRow("stub-a", "budget", "another policy"),
	}, 40, http.StatusOK)
	client := accountPolicyTestClient(t, srv.URL)

	_, found, diags := guardrailPolicyAccountLookup(context.Background(), client, "budget", "stub-missing")
	if !diags.HasError() {
		t.Fatal("a truncated list resolved without an error; the missing row is not evidence that the policy is gone")
	}
	if found {
		t.Error("a truncated list was reported as found")
	}
	if got := diags[0].Summary(); !strings.Contains(got, "truncated") {
		t.Errorf("diagnostic summary is %q, want it to name the truncation", got)
	}
}

// TestGuardrailPolicyAccountLookupRefusesAContradictoryTotal covers the pages that report a
// total smaller than themselves, which cannot be reconciled with their own contents, so
// completeness cannot be inferred from them and absence must not be declared. That is the
// recreate-against-a-live-policy failure the truncation guard covers from the other direction.
//
// The policy under lookup is absent from every page here, which is the only case these two
// completeness checks are asked about: a page that carries the policy returns from the loop
// above whatever its total claims, and
// TestGuardrailPolicyAccountLookupUsesARowFromAContradictoryPage pins that half deliberately.
//
// The two shapes are here because only one of them is a special case: a total of zero beside a
// non-empty page is the obvious contradiction, and two rows beside a total of one is the same
// defect one step less obvious, which a check for zero alone would pass silently.
func TestGuardrailPolicyAccountLookupRefusesAContradictoryTotal(t *testing.T) {
	for _, tc := range []struct {
		name  string
		rows  []map[string]any
		total int
	}{
		{
			name:  "rows reported beside a total of zero",
			rows:  []map[string]any{accountPolicyRow("stub-a", "budget", "a budget policy")},
			total: 0,
		},
		{
			name: "a page longer than its own total",
			rows: []map[string]any{
				accountPolicyRow("stub-a", "budget", "a budget policy"),
				accountPolicyRow("stub-b", "budget", "another budget policy"),
			},
			total: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := accountPolicyListServerWithTotal(t, tc.rows, tc.total, http.StatusOK)
			client := accountPolicyTestClient(t, srv.URL)

			_, found, diags := guardrailPolicyAccountLookup(context.Background(), client, "budget", "stub-missing")
			if !diags.HasError() {
				t.Fatal("a page that outnumbers its own total reported an absent policy as gone; neither completeness nor absence can be read from it")
			}
			if found {
				t.Error("a contradictory page was reported as found")
			}
			if got := diags[0].Summary(); !strings.Contains(got, "could not be interpreted") {
				t.Errorf("diagnostic summary is %q, want it to name the uninterpretable page", got)
			}
		})
	}
}

// TestGuardrailPolicyAccountLookupUsesARowFromAContradictoryPage is the other half, and it is
// the half a destroy depends on. The page and its total disagree, but the row the lookup is
// after is present with the right id and type, and that row is the policy this resource manages
// however much of the rest of the page is missing: the contradiction is evidence about
// completeness, not about the rows that arrived. Refusing it would be worse than trusting it,
// because the destroy path reads through this same function before its DELETE to learn whether
// the policy is the account default, and a refusal becomes a warning there and lets the delete
// go. Measured 2026-11-22: with the consistency check in front of the loop, a destroy deleted a
// policy that the same response had marked as its type's default.
func TestGuardrailPolicyAccountLookupUsesARowFromAContradictoryPage(t *testing.T) {
	srv := accountPolicyListServerWithTotal(t, []map[string]any{
		accountPolicyRow("stub-a", "budget", "the managed policy"),
		accountPolicyRow("stub-b", "budget", "another budget policy"),
	}, 1, http.StatusOK)
	client := accountPolicyTestClient(t, srv.URL)

	policy, found, diags := guardrailPolicyAccountLookup(context.Background(), client, "budget", "stub-a")
	if diags.HasError() {
		t.Fatalf("a matched row was refused because the page contradicted its own total: %v", diags)
	}
	if !found || policy == nil {
		t.Fatal("the row the lookup was after was not returned")
	}
	if got := guardrailPolicyAccountID(*policy); got != "stub-a" {
		t.Errorf("lookup resolved id %q, want stub-a", got)
	}
}
