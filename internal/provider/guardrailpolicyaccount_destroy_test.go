package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
// stub suite: it has three branches a step cannot distinguish (the remote row is the default,
// the row cannot be read, the DELETE fails with a status other than 200 or 404) and one of
// them ends in a warning, which the acceptance framework does not surface at all. These tests
// call Delete directly with a client pointed at a stub, so each branch is asserted on the
// diagnostics it produces and on the DELETEs that reached the route.

// accountPolicyDestroyStub serves the two routes a destroy uses: the list the guard reads
// before the DELETE, and the DELETE itself. `rows` is what the list answers with,
// `listStatus` is the status it answers with instead of 200, `deleteMessage` is the sentence a
// failed DELETE carries as the route's own explanation, and `deletes` counts every DELETE that
// reached the route, which is what separates a refused destroy from one that was sent.
type accountPolicyDestroyStub struct {
	rows          []map[string]any
	listStatus    int
	deleteStatus  int
	deleteMessage string
	deletes       int
}

func newAccountPolicyDestroyServer(t *testing.T, stub *accountPolicyDestroyStub) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /guardrails/policies/account", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if stub.listStatus != 0 && stub.listStatus != http.StatusOK {
			w.WriteHeader(stub.listStatus)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{"code": stub.listStatus, "message": "Not Found - Resource does not exist"},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": stub.rows, "total_count": len(stub.rows)})
	})
	mux.HandleFunc("DELETE /guardrails/policies/account/{id}", func(w http.ResponseWriter, r *http.Request) {
		stub.deletes++
		w.Header().Set("Content-Type", "application/json")
		if stub.deleteStatus != 0 && stub.deleteStatus != http.StatusOK {
			w.WriteHeader(stub.deleteStatus)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{"code": stub.deleteStatus, "message": stub.deleteMessage},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": true})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// accountPolicyDestroyRow is a budget row whose `is_default` the route reports. It is the same
// shape the list returns, because that is what the guard reads.
func accountPolicyDestroyRow(id string, isDefault bool) map[string]any {
	row := accountPolicyRow(id, "budget", "a budget policy")
	row["is_default"] = isDefault
	return row
}

// runAccountPolicyDestroy calls Delete on the budget resource with a state that says the
// policy is not the default, which is the state a policy promoted outside Terraform leaves
// behind. The resource is the budget member of the family; the destroy path is shared, so a
// case here covers all three.
func runAccountPolicyDestroy(t *testing.T, client *sdk.OpenRouter, id string) diag.Diagnostics {
	t.Helper()
	ctx := context.Background()
	r := NewGuardrailPolicyAccountBudgetResource()

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
	if diags := state.SetAttribute(ctx, path.Root("is_default"), types.BoolValue(false)); diags.HasError() {
		t.Fatalf("setting the state flag reported %v", diags)
	}

	var resp resource.DeleteResponse
	resp.State = state
	r.Delete(ctx, resource.DeleteRequest{State: state}, &resp)
	return resp.Diagnostics
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
	stub := &accountPolicyDestroyStub{rows: []map[string]any{accountPolicyDestroyRow("stub-account-1", true)}}
	srv := newAccountPolicyDestroyServer(t, stub)
	client := accountPolicyTestClient(t, srv.URL)

	diags := runAccountPolicyDestroy(t, client, "stub-account-1")
	if !diags.HasError() {
		t.Fatal("a destroy of a policy the server reports as the default resolved without an error; the DELETE releases the account baseline and cannot be undone")
	}
	if got := diags[0].Summary(); !strings.Contains(got, "refusing to destroy the account default") {
		t.Errorf("diagnostic summary is %q, want the destroy refusal", got)
	}
	if stub.deletes != 0 {
		t.Errorf("%d DELETE(s) reached the route, want none: the refusal has to stand in front of the request", stub.deletes)
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
	stub := &accountPolicyDestroyStub{
		rows:          []map[string]any{accountPolicyDestroyRow("stub-account-1", false)},
		deleteStatus:  http.StatusBadRequest,
		deleteMessage: "the account policy could not be deleted",
	}
	srv := newAccountPolicyDestroyServer(t, stub)
	client := accountPolicyTestClient(t, srv.URL)

	diags := runAccountPolicyDestroy(t, client, "stub-account-1")
	if !diags.HasError() {
		t.Fatal("a DELETE answered 400 and the destroy reported success; the policy is still enforced remotely")
	}
	if got := diags[0].Summary(); !strings.Contains(got, "unexpected response code 400") {
		t.Errorf("diagnostic summary is %q, want it to name the status the route answered", got)
	}
	if got := diags[0].Detail(); !strings.Contains(got, stub.deleteMessage) {
		t.Errorf("diagnostic detail is %q, want the route's own sentence: an operator who sees only a status code cannot tell what the route refused", got)
	}
	if stub.deletes != 1 {
		t.Errorf("%d DELETE(s) reached the route, want exactly the one the path sends", stub.deletes)
	}
}

// TestGuardrailPolicyAccountDestroyWarnsWhenTheListCannotBeRead covers the branch that keeps
// the guard from becoming a wall. The list route answers 404 for an organization whose
// guardrails backend was withdrawn, and a list that cannot be read can show neither that the
// policy is the default nor that removing it is safe, so the destroy warns and goes: the state
// check is the primary protection and a 404 from the DELETE itself is already success. The
// warning is the assertion, and it is the reason these cases call Delete rather than run a
// Terraform step, because a step does not fail on a warning and the framework offers no check
// for one.
func TestGuardrailPolicyAccountDestroyWarnsWhenTheListCannotBeRead(t *testing.T) {
	stub := &accountPolicyDestroyStub{
		rows:       []map[string]any{accountPolicyDestroyRow("stub-account-1", false)},
		listStatus: http.StatusNotFound,
	}
	srv := newAccountPolicyDestroyServer(t, stub)
	client := accountPolicyTestClient(t, srv.URL)

	diags := runAccountPolicyDestroy(t, client, "stub-account-1")
	if diags.HasError() {
		t.Fatalf("an unreadable list blocked the destroy: %v. The check it guards is a second opinion, and the destroy has to proceed on state", diags)
	}
	if len(diags) != 1 {
		t.Fatalf("the destroy reported %d diagnostic(s), want the one warning: %v", len(diags), diags)
	}
	if diags[0].Severity() != diag.SeverityWarning {
		t.Errorf("the destroy reported %v, want a warning: a list this path cannot read must not fail a destroy", diags[0])
	}
	if got := diags[0].Summary(); !strings.Contains(got, "could not verify the account default") {
		t.Errorf("diagnostic summary is %q, want it to name what could not be checked", got)
	}
	if stub.deletes != 1 {
		t.Errorf("%d DELETE(s) reached the route, want exactly the one the path sends", stub.deletes)
	}
}
