package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk"
)

// The destroy path is the one operation that cannot be driven through the Terraform-level
// stub suite: it has four branches a step cannot distinguish (state says the policy is the
// default, the remote row says so, the row cannot be read, and the DELETE answers a status
// other than 200 or 404) and one of them ends in a warning, which the acceptance framework
// does not surface. These tests call Delete directly with a client pointed at a stub, so each
// branch is asserted on the diagnostics it produces and on the DELETEs that reached the route,
// including which policy each DELETE named.

// accountPolicyDestroyFamilies is every resource in the family. Delete's pre-read goes through
// the family's own `variant` and `fields` functions, so a broken default-flag read on one
// family passes a sweep of the others; a single-family sweep here would repeat the gap the
// family-wide import sweep was added to close.
var accountPolicyDestroyFamilies = []struct {
	name string
	make func() resource.Resource
}{
	{"budget", NewGuardrailPolicyAccountBudgetResource},
	{"moderation", NewGuardrailPolicyAccountModerationResource},
	{"model_availability", NewGuardrailPolicyAccountModelAvailabilityResource},
}

// accountPolicyDestroyStub serves the two routes a destroy uses: the list the guard reads
// before the DELETE, and the DELETE itself. `rows` is what the list answers with, `listStatus`
// is the status it answers with instead of 200, `deleteMessage` is the sentence a failed
// DELETE carries as the route's own explanation, and the counters record what reached the
// route. Every field is read and written behind the mutex: the handlers run on the server's
// goroutines and the assertions on the test's, which is the shape that reads clean in a single
// run and fails under -race.
type accountPolicyDestroyStub struct {
	mu            sync.Mutex
	rows          []map[string]any
	listStatus    int
	deleteStatus  int
	deleteMessage string
	// totalOverride is what the list reports as its total; zero means "the number of rows
	// served", which is the honest case. A non-zero value that differs from the page is how a
	// case builds a response whose completeness cannot be trusted.
	totalOverride int

	listReads  int
	deletedIDs []string
}

func (s *accountPolicyDestroyStub) listReadCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listReads
}

func (s *accountPolicyDestroyStub) deleteTargets() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.deletedIDs))
	copy(out, s.deletedIDs)
	return out
}

func newAccountPolicyDestroyServer(t *testing.T, stub *accountPolicyDestroyStub) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /guardrails/policies/account", func(w http.ResponseWriter, r *http.Request) {
		stub.mu.Lock()
		stub.listReads++
		status, rows, total := stub.listStatus, stub.rows, stub.totalOverride
		stub.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if status != 0 && status != http.StatusOK {
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{"code": status, "message": "Not Found - Resource does not exist"},
			})
			return
		}
		if total == 0 {
			total = len(rows)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": rows, "total_count": total})
	})
	mux.HandleFunc("DELETE /guardrails/policies/account/{id}", func(w http.ResponseWriter, r *http.Request) {
		stub.mu.Lock()
		stub.deletedIDs = append(stub.deletedIDs, r.PathValue("id"))
		status, message := stub.deleteStatus, stub.deleteMessage
		stub.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if status != 0 && status != http.StatusOK {
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{"code": status, "message": message},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": true})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// accountPolicyDestroyRow is a row of the family the resource manages, carrying the
// `is_default` the route reports for it. It is the shape the list returns, because that is what
// the guard reads.
func accountPolicyDestroyRow(id, policyType string, isDefault bool) map[string]any {
	row := accountPolicyRow(id, policyType, "an account policy")
	row["is_default"] = isDefault
	return row
}

// otherPolicyType is a wire policy type that is not the named family's, for the case where the
// id state holds resolves to a policy this resource does not manage.
func otherPolicyType(family string) string {
	for _, fam := range accountPolicyDestroyFamilies {
		if fam.name != family {
			return fam.name
		}
	}
	return family
}

// runAccountPolicyDestroy calls Delete on the named family's resource with a state whose
// `is_default` the caller sets. Passing false is the state a policy promoted outside Terraform
// leaves behind; passing true is the state the provider's own first refusal reads.
func runAccountPolicyDestroy(t *testing.T, fam func() resource.Resource, client *sdk.OpenRouter, id string, stateIsDefault bool) diag.Diagnostics {
	t.Helper()
	ctx := context.Background()
	r := fam()

	var configureResp resource.ConfigureResponse
	r.(resource.ResourceWithConfigure).Configure(ctx, resource.ConfigureRequest{ProviderData: client}, &configureResp)
	if configureResp.Diagnostics.HasError() {
		t.Fatalf("configuring the resource reported %v", configureResp.Diagnostics)
	}

	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("reading the resource schema reported %v", schemaResp.Diagnostics)
	}

	objType, ok := schemaResp.Schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatal("the account policy schema is not an object type")
	}
	values := make(map[string]tftypes.Value, len(objType.AttributeTypes))
	for name, attrType := range objType.AttributeTypes {
		values[name] = tftypes.NewValue(attrType, nil)
	}
	state := tfsdk.State{Schema: schemaResp.Schema, Raw: tftypes.NewValue(objType, values)}
	if diags := state.SetAttribute(ctx, path.Root("id"), types.StringValue(id)); diags.HasError() {
		t.Fatalf("setting the state id reported %v", diags)
	}
	if diags := state.SetAttribute(ctx, path.Root("is_default"), types.BoolValue(stateIsDefault)); diags.HasError() {
		t.Fatalf("setting the state flag reported %v", diags)
	}

	var resp resource.DeleteResponse
	resp.State = state
	r.Delete(ctx, resource.DeleteRequest{State: state}, &resp)
	return resp.Diagnostics
}

// requireDeletedOnly checks that the DELETEs that reached the route named exactly the policies
// the caller expects. A count alone cannot tell a destroy of the managed policy from one aimed
// at another: the id the provider deletes comes from state, so a state the stub never handed
// out would look identical in a count.
func requireDeletedOnly(t *testing.T, stub *accountPolicyDestroyStub, want ...string) {
	t.Helper()
	got := stub.deleteTargets()
	if len(got) != len(want) {
		t.Fatalf("%d DELETE(s) reached the route, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("DELETE %d named %q, want %q", i+1, got[i], want[i])
		}
	}
}

// TestGuardrailPolicyAccountDestroyRefusesAPolicyTheServerCallsDefault is the guard the state
// check cannot provide. `is_default` is Optional+Computed and the provider only knows what its
// own state holds, so a policy promoted to the account default outside Terraform leaves
// `is_default = false` behind, and a destroy running with refresh disabled never reads the
// row. The route deletes a default with no complaint and there is no undelete, so the flag
// that protects every workspace resolving to the policy has to come from the server. Measured
// 2026-11-18: that DELETE answers 200. This case asserts both halves: the refusal reaches the
// operator, and no DELETE is sent.
func TestGuardrailPolicyAccountDestroyRefusesAPolicyTheServerCallsDefault(t *testing.T) {
	for _, fam := range accountPolicyDestroyFamilies {
		t.Run(fam.name, func(t *testing.T) {
			stub := &accountPolicyDestroyStub{
				rows: []map[string]any{accountPolicyDestroyRow("stub-account-1", fam.name, true)},
			}
			srv := newAccountPolicyDestroyServer(t, stub)
			client := accountPolicyTestClient(t, srv.URL)

			diags := runAccountPolicyDestroy(t, fam.make, client, "stub-account-1", false)
			if !diags.HasError() {
				t.Fatal("a destroy of a policy the server reports as the default resolved without an error; the DELETE releases the account baseline and cannot be undone")
			}
			if got := diags[0].Summary(); !strings.Contains(got, "refusing to destroy the account default") {
				t.Errorf("diagnostic summary is %q, want the destroy refusal", got)
			}
			requireDeletedOnly(t, stub)
			if n := stub.listReadCount(); n != 1 {
				t.Errorf("the list was read %d time(s), want the one the guard makes before the DELETE", n)
			}
		})
	}
}

// TestGuardrailPolicyAccountDestroyRefusesAStateDefaultBeforeReadingTheList isolates the
// state-side refusal from the server-side one. Both refusals carry the same message, and the
// acceptance scenario for this path has state and server agreeing, so deleting the state check
// would leave every other test green while a destroy with refresh disabled sent a DELETE for a
// policy the state itself calls the default. The list here says the opposite, so the refusal
// can only have come from state, and the read count proves it came before the read.
func TestGuardrailPolicyAccountDestroyRefusesAStateDefaultBeforeReadingTheList(t *testing.T) {
	for _, fam := range accountPolicyDestroyFamilies {
		t.Run(fam.name, func(t *testing.T) {
			stub := &accountPolicyDestroyStub{
				rows: []map[string]any{accountPolicyDestroyRow("stub-account-1", fam.name, false)},
			}
			srv := newAccountPolicyDestroyServer(t, stub)
			client := accountPolicyTestClient(t, srv.URL)

			diags := runAccountPolicyDestroy(t, fam.make, client, "stub-account-1", true)
			if !diags.HasError() {
				t.Fatal("a destroy of a policy state calls the account default resolved without an error")
			}
			if got := diags[0].Summary(); !strings.Contains(got, "refusing to destroy the account default") {
				t.Errorf("diagnostic summary is %q, want the destroy refusal", got)
			}
			if n := stub.listReadCount(); n != 0 {
				t.Errorf("the list was read %d time(s); the state check is the primary protection and stands in front of the read", n)
			}
			requireDeletedOnly(t, stub)
		})
	}
}

// TestGuardrailPolicyAccountDestroyReportsAFailedDelete covers the switch's `default:` arm, the
// one branch of the delete path no test had run: the stub suite answers 200 and 404 only, and
// 404 is the case that means "already gone". A status that is neither has to fail the destroy,
// because reporting success would hand back a resource that is still enforcing, and the route's
// own sentence has to reach the operator rather than only a status code.
//
// The status is a 400 rather than the 500 this case was first written with, because the
// generated SDK's default retry config retries `5XX` (InitialInterval 500 ms, MaxInterval 60 s,
// Exponent 1.5, MaxElapsedTime 3,600,000 ms), so a 500 is re-sent for up to an hour before this
// arm is reached at all: the case hung on the retry loop, with the DELETE counted several times
// and no diagnostic produced. That is also the shape of the earlier unexplained run whose log
// held three deletes and no error. The arm is reachable in production exactly where a documented
// status is not retried, which is what this uses.
func TestGuardrailPolicyAccountDestroyReportsAFailedDelete(t *testing.T) {
	for _, fam := range accountPolicyDestroyFamilies {
		t.Run(fam.name, func(t *testing.T) {
			stub := &accountPolicyDestroyStub{
				rows:          []map[string]any{accountPolicyDestroyRow("stub-account-1", fam.name, false)},
				deleteStatus:  http.StatusBadRequest,
				deleteMessage: "the account policy could not be deleted",
			}
			srv := newAccountPolicyDestroyServer(t, stub)
			client := accountPolicyTestClient(t, srv.URL)

			diags := runAccountPolicyDestroy(t, fam.make, client, "stub-account-1", false)
			if !diags.HasError() {
				t.Fatal("a DELETE answered 400 and the destroy reported success; the policy is still enforced remotely")
			}
			if got := diags[0].Summary(); !strings.Contains(got, "unexpected response code 400") {
				t.Errorf("diagnostic summary is %q, want it to name the status the route answered", got)
			}
			if got := diags[0].Detail(); !strings.Contains(got, stub.deleteMessage) {
				t.Errorf("diagnostic detail is %q, want the route's own sentence: an operator who sees only a status code cannot tell what the route refused", got)
			}
			requireDeletedOnly(t, stub, "stub-account-1")
		})
	}
}

// TestGuardrailPolicyAccountDestroyWarnsAndProceedsWhenTheCheckCannotBeMade covers the branch
// that keeps the guard from becoming a wall. A list the path cannot use leaves it unable to say
// whether the policy is the default, and the state check in front of it is the primary
// protection, so the destroy warns with the reason and goes. The warning is the assertion, and
// it is why these cases call Delete rather than run a Terraform step: a step does not fail on a
// warning and the framework offers no check for one.
//
// Three shapes reach that arm and they are not the same failure, which is what the assertion on
// the reason is for: the route refusing the read, a page whose contents cannot be trusted at
// all, and a page that answers for the id with a policy of another family. A message that named
// only one of those causes would be wrong for the other two, so each case requires its own
// reason to arrive.
func TestGuardrailPolicyAccountDestroyWarnsAndProceedsWhenTheCheckCannotBeMade(t *testing.T) {
	for _, fam := range accountPolicyDestroyFamilies {
		for _, tc := range []struct {
			name   string
			reason string
			rows   []map[string]any
			total  int
			status int
		}{
			{
				name:   "the list route refuses the read",
				reason: "404",
				status: http.StatusNotFound,
			},
			{
				name:   "the page contradicts its own total and does not carry the policy",
				reason: "could not be interpreted",
				rows: []map[string]any{
					accountPolicyDestroyRow("stub-account-2", fam.name, false),
					accountPolicyDestroyRow("stub-account-3", fam.name, false),
				},
				total: 1,
			},
			{
				name:   "the id resolves to another family's policy",
				reason: "policy type does not match",
				rows: []map[string]any{
					accountPolicyDestroyRow("stub-account-1", otherPolicyType(fam.name), false),
				},
			},
		} {
			t.Run(fam.name+"/"+tc.name, func(t *testing.T) {
				stub := &accountPolicyDestroyStub{rows: tc.rows, listStatus: tc.status, totalOverride: tc.total}
				srv := newAccountPolicyDestroyServer(t, stub)
				client := accountPolicyTestClient(t, srv.URL)

				diags := runAccountPolicyDestroy(t, fam.make, client, "stub-account-1", false)
				if diags.HasError() {
					t.Fatalf("a check that could not be made blocked the destroy: %v. It is a second opinion, and the destroy has to proceed on state", diags)
				}
				if len(diags) != 1 {
					t.Fatalf("the destroy reported %d diagnostic(s), want the one warning: %v", len(diags), diags)
				}
				if diags[0].Severity() != diag.SeverityWarning {
					t.Errorf("the destroy reported %v, want a warning: a check this path cannot make must not fail a destroy", diags[0])
				}
				if got := diags[0].Summary(); !strings.Contains(got, "could not verify the account default") {
					t.Errorf("diagnostic summary is %q, want it to name what could not be checked", got)
				}
				if got := diags[0].Detail(); !strings.Contains(got, tc.reason) {
					t.Errorf("diagnostic detail is %q, want it to carry the reason the check did not complete (%q); a fixed sentence would be wrong for the other shapes this arm serves", got, tc.reason)
				}
				requireDeletedOnly(t, stub, "stub-account-1")
			})
		}
	}
}

// TestGuardrailPolicyAccountDestroyRefusesAContradictoryPageThatNamesTheDefault is the case that
// separates the two jobs the lookup does. A page and its total can disagree while the page still
// carries the managed policy marked as its type's default, and the destroy's guard has to reach
// that row: the contradiction is evidence about completeness, not about the row. Measured
// 2026-11-22: with the consistency check in front of the row loop the destroy errored, the error
// became the warning above, and the DELETE went, deleting a policy the same response had called
// the default. This is why the checks sit after the loop.
func TestGuardrailPolicyAccountDestroyRefusesAContradictoryPageThatNamesTheDefault(t *testing.T) {
	for _, fam := range accountPolicyDestroyFamilies {
		t.Run(fam.name, func(t *testing.T) {
			stub := &accountPolicyDestroyStub{
				rows: []map[string]any{
					accountPolicyDestroyRow("stub-account-1", fam.name, true),
					accountPolicyDestroyRow("stub-account-2", fam.name, false),
				},
				totalOverride: 1,
			}
			srv := newAccountPolicyDestroyServer(t, stub)
			client := accountPolicyTestClient(t, srv.URL)

			diags := runAccountPolicyDestroy(t, fam.make, client, "stub-account-1", false)
			if !diags.HasError() {
				t.Fatal("a page that carries the managed policy as its type's default was treated as unreadable, and the destroy went: the contradiction is about the page, not about the row")
			}
			if got := diags[0].Summary(); !strings.Contains(got, "refusing to destroy the account default") {
				t.Errorf("diagnostic summary is %q, want the destroy refusal", got)
			}
			requireDeletedOnly(t, stub)
		})
	}
}
