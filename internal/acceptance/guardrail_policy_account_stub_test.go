package acceptance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// The account (workspace-wide) policy family is hand-written because the route has no
// get-by-id, so its read is a list filtered on the policy id. This stub serves that
// route with the shapes and the two 400s the live API answered on 2026-11-18, and the
// tests below pin the three behaviours the store cannot be read through the API to
// check: the read sends no pagination, a write always carries the coverage it holds, and
// the account default is never sent a workspace list.
type guardrailPolicyAccountStub struct {
	mu sync.Mutex
	// policies is keyed by policy id, because the list route returns every type at
	// once and the resource has to find its own row among them.
	policies map[string]map[string]any
	// updates records every PUT body in order. It is a slice rather than a map keyed by
	// id because the test that reads it runs after the case's closing destroy, which for
	// a floor policy is a real DELETE and takes the id out of policies with it.
	updates      []map[string]any
	createBodies []map[string]any
	listQueries  []string
	deleteCalls  int
}

func newGuardrailPolicyAccountStub(t *testing.T) (*httptest.Server, *guardrailPolicyAccountStub) {
	t.Helper()
	s := &guardrailPolicyAccountStub{
		policies: map[string]map[string]any{},
	}
	writeJSON := func(w http.ResponseWriter, code int, body any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(body)
	}
	badRequest := func(w http.ResponseWriter, message string) {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]any{"code": 400, "message": message},
		})
	}

	// full is the shape the list and every write answer with, including the
	// projection the live route applies: for the account default, workspace_ids is
	// the set of workspaces currently resolving to it, and it excludes the opt-outs.
	full := func(p map[string]any) map[string]any {
		list := func(key string) []any {
			if v, ok := p[key].([]any); ok {
				return v
			}
			return []any{}
		}
		out := map[string]any{
			"id":                          p["id"],
			"name":                        p["name"],
			"policy_type":                 p["policy_type"],
			"config":                      p["config"],
			"created_at":                  "2026-11-18T00:00:00Z",
			"updated_at":                  "2026-11-18T00:00:00Z",
			"is_default":                  p["is_default"],
			"is_synced":                   false,
			"is_workspace_default_synced": false,
			"workspace_ids":               list("workspace_ids"),
			"opted_out_workspace_ids":     list("opted_out_workspace_ids"),
		}
		if d, _ := p["is_default"].(bool); d {
			// Everything the stub knows about, minus what the policy opted out of.
			known := []any{"stub-workspace-a", "stub-workspace-b", "stub-workspace-c"}
			for _, o := range list("opted_out_workspace_ids") {
				for i, k := range known {
					if k == o {
						known = append(known[:i], known[i+1:]...)
						break
					}
				}
			}
			out["workspace_ids"] = known
		}
		out["workspace_count"] = len(out["workspace_ids"].([]any))
		return out
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /guardrails/policies/account", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.listQueries = append(s.listQueries, r.URL.RawQuery)
		data := []any{}
		// Reverse insertion order, so a provider that assumed the newest row was
		// first, or the only one, would fail here rather than pass by luck.
		ids := make([]string, 0, len(s.policies))
		for id := range s.policies {
			ids = append(ids, id)
		}
		for i := len(ids) - 1; i >= 0; i-- {
			data = append(data, full(s.policies[ids[i]]))
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": data, "total_count": len(data)})
	})
	mux.HandleFunc("POST /guardrails/policies/account", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			badRequest(w, "Invalid guardrail policy request")
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		body["id"] = fmt.Sprintf("stub-account-%d", len(s.policies)+1)
		if body["is_default"] == nil {
			body["is_default"] = false
		}
		s.createBodies = append(s.createBodies, body)
		s.policies[body["id"].(string)] = body
		writeJSON(w, http.StatusCreated, map[string]any{"data": full(body)})
	})
	mux.HandleFunc("PUT /guardrails/policies/account/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			badRequest(w, "Invalid guardrail policy request")
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		existing, ok := s.policies[id]
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": map[string]any{"code": 404, "message": "Account policy not found"}})
			return
		}
		// The two combinations the live route refuses, with its own wording.
		isDefault, _ := body["is_default"].(bool)
		if isDefault && len(asList(body["workspace_ids"])) > 0 {
			badRequest(w, "The account default covers workspaces through opt-outs")
			return
		}
		if !isDefault && len(asList(body["opted_out_workspace_ids"])) > 0 {
			badRequest(w, "Only the account default can have default assignment fields")
			return
		}
		store := map[string]any{
			"id":                      id,
			"name":                    body["name"],
			"policy_type":             existing["policy_type"],
			"config":                  body["config"],
			"is_default":              isDefault,
			"workspace_ids":           asList(body["workspace_ids"]),
			"opted_out_workspace_ids": asList(body["opted_out_workspace_ids"]),
		}
		s.policies[id] = store
		s.updates = append(s.updates, body)
		writeJSON(w, http.StatusOK, map[string]any{"data": full(store)})
	})
	mux.HandleFunc("DELETE /guardrails/policies/account/{id}", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.deleteCalls++
		delete(s.policies, r.PathValue("id"))
		writeJSON(w, http.StatusOK, map[string]any{"data": true})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, s
}

func asList(v any) []any {
	if l, ok := v.([]any); ok {
		return l
	}
	return []any{}
}

func (s *guardrailPolicyAccountStub) lastCreate(t *testing.T) map[string]any {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.createBodies) == 0 {
		t.Fatal("no create reached the stub")
	}
	return s.createBodies[len(s.createBodies)-1]
}

func (s *guardrailPolicyAccountStub) lastUpdate(t *testing.T) map[string]any {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.updates) == 0 {
		t.Fatal("no update reached the stub")
	}
	return s.updates[len(s.updates)-1]
}

func (s *guardrailPolicyAccountStub) listWasUnpaginated(t *testing.T) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.listQueries) == 0 {
		t.Fatal("no list read reached the stub")
	}
	for _, q := range s.listQueries {
		if q != "" {
			t.Errorf("the read sent ?%s; the route returns the full list only when both offset and limit are omitted", q)
		}
	}
}

func accountStubConfig(serverURL, tfName, attrs string) string {
	return stubProviderBlock(serverURL) + fmt.Sprintf(`
resource %q "test" {
%s}
`, tfName, attrs)
}

// TestStubGuardrailPolicyAccountCreateAndRead covers the whole happy path: a create
// whose body carries the type, an unpaginated list read that finds the row, and state
// hydrated from the list rather than from the write response.
func TestStubGuardrailPolicyAccountCreateAndRead(t *testing.T) {
	t.Setenv("TF_ACC", "1")

	srv, stub := newGuardrailPolicyAccountStub(t)

	cfg := accountStubConfig(srv.URL, "openrouter_guardrail_policy_account_budget", `
  config = {
    include_byok_in_budgets = true
    limit_usd               = 50
    reset_interval          = "monthly"
  }
  name = "engineering budget"
  workspace_ids = ["stub-workspace-a"]
`)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("openrouter_guardrail_policy_account_budget.test", "id"),
					resource.TestCheckResourceAttr("openrouter_guardrail_policy_account_budget.test", "is_default", "false"),
					resource.TestCheckResourceAttr("openrouter_guardrail_policy_account_budget.test", "workspace_ids.#", "1"),
					resource.TestCheckResourceAttr("openrouter_guardrail_policy_account_budget.test", "config.limit_usd", "50"),
				),
			},
		},
	})

	create := stub.lastCreate(t)
	if got := create["policy_type"]; got != "budget" {
		t.Errorf("create sent policy_type=%v, want %q", got, "budget")
	}
	if got, present := create["is_default"]; !present || got != false {
		t.Errorf("create sent is_default=%v (present=%v), want an explicit false: the route replaces coverage wholesale and an absent flag is not a false", got, present)
	}
	if got := asList(create["workspace_ids"]); !reflect.DeepEqual(got, []any{"stub-workspace-a"}) {
		t.Errorf("create sent workspace_ids=%v, want [stub-workspace-a]", got)
	}
	stub.listWasUnpaginated(t)
}

// TestStubGuardrailPolicyAccountUpdateOmittingCoverageKeepsStoredCoverage is the
// preservation test. Step two omits workspace_ids entirely, so a provider that built the
// body from the configuration alone would send none and the route would clear the
// coverage: measured 2026-11-18, a PUT without workspace_ids left the policy covering
// zero workspaces.
func TestStubGuardrailPolicyAccountUpdateOmittingCoverageKeepsStoredCoverage(t *testing.T) {
	t.Setenv("TF_ACC", "1")

	srv, stub := newGuardrailPolicyAccountStub(t)

	// tamper guards against a stub that answered an update from the stored policy
	// rather than from the body it received.
	first := accountStubConfig(srv.URL, "openrouter_guardrail_policy_account_budget", `
  config = {
    include_byok_in_budgets = false
    limit_usd               = 50
    reset_interval          = "monthly"
  }
  name = "engineering budget"
  workspace_ids = ["stub-workspace-a"]
`)
	second := accountStubConfig(srv.URL, "openrouter_guardrail_policy_account_budget", `
  config = {
    include_byok_in_budgets = false
    limit_usd               = 50
    reset_interval          = "monthly"
  }
  name = "renamed budget"
`)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: first,
			},
			{
				Config: second,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("openrouter_guardrail_policy_account_budget.test", "name", "renamed budget"),
					resource.TestCheckResourceAttr("openrouter_guardrail_policy_account_budget.test", "workspace_ids.#", "1"),
					resource.TestCheckResourceAttr("openrouter_guardrail_policy_account_budget.test", "workspace_ids.0", "stub-workspace-a"),
				),
			},
		},
	})

	body := stub.lastUpdate(t)
	if got := asList(body["workspace_ids"]); !reflect.DeepEqual(got, []any{"stub-workspace-a"}) {
		t.Errorf("the update sent workspace_ids=%v, want the stored [stub-workspace-a]: an omitted coverage list has to travel from state, because the route clears what it is not sent", got)
	}
}

// TestStubGuardrailPolicyAccountDefaultSendsNoWorkspaceIDs pins the rule the live route
// enforces with 400 "The account default covers workspaces through opt-outs": once a
// policy is the default, workspace_ids is neither configured nor sent, and the state
// carries the resolved coverage the read returns instead.
func TestStubGuardrailPolicyAccountDefaultSendsNoWorkspaceIDs(t *testing.T) {
	t.Setenv("TF_ACC", "1")

	srv, stub := newGuardrailPolicyAccountStub(t)

	floor := accountStubConfig(srv.URL, "openrouter_guardrail_policy_account_model_availability", `
  config = {
    enforce_zdr = true
  }
  name = "account baseline"
  workspace_ids = ["stub-workspace-a"]
`)
	asDefault := accountStubConfig(srv.URL, "openrouter_guardrail_policy_account_model_availability", `
  config = {
    enforce_zdr = true
  }
  is_default = true
  name       = "account baseline"
  opted_out_workspace_ids = ["stub-workspace-b"]
`)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{Config: floor},
			{
				Config: asDefault,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("openrouter_guardrail_policy_account_model_availability.test", "is_default", "true"),
					resource.TestCheckResourceAttr("openrouter_guardrail_policy_account_model_availability.test", "opted_out_workspace_ids.#", "1"),
					// The projection: everything else the stub knows about.
					resource.TestCheckResourceAttr("openrouter_guardrail_policy_account_model_availability.test", "workspace_ids.#", "2"),
				),
			},
		},
	})

	body := stub.lastUpdate(t)
	if got, present := body["workspace_ids"]; present {
		t.Errorf("the update sent workspace_ids=%v; the route refuses the account default that carries one", got)
	}
	if got, present := body["is_default"]; !present || got != true {
		t.Errorf("the update sent is_default=%v (present=%v), want an explicit true", got, present)
	}
}

// TestStubGuardrailPolicyAccountRejectsCoverageWithTheWrongDefault covers the two
// combinations the route answers with 400, at plan time rather than at apply.
func TestStubGuardrailPolicyAccountRejectsCoverageWithTheWrongDefault(t *testing.T) {
	t.Setenv("TF_ACC", "1")

	srv, _ := newGuardrailPolicyAccountStub(t)

	defaultWithWorkspaces := accountStubConfig(srv.URL, "openrouter_guardrail_policy_account_budget", `
  config = {
    include_byok_in_budgets = false
    limit_usd               = 50
    reset_interval          = "monthly"
  }
  is_default    = true
  name          = "bad default"
  workspace_ids = ["stub-workspace-a"]
`)
	optedOutOnAFloor := accountStubConfig(srv.URL, "openrouter_guardrail_policy_account_budget", `
  config = {
    include_byok_in_budgets = false
    limit_usd               = 50
    reset_interval          = "monthly"
  }
  is_default              = false
  name                    = "bad floor"
  opted_out_workspace_ids = ["stub-workspace-a"]
  workspace_ids           = ["stub-workspace-b"]
`)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config:      defaultWithWorkspaces,
				ExpectError: regexp.MustCompile(`The account default covers workspaces through opt-outs`),
			},
			{
				Config:      optedOutOnAFloor,
				ExpectError: regexp.MustCompile(`Only the account default can have default assignment fields`),
			},
		},
	})
}

// TestStubGuardrailPolicyAccountForgetsTheDefaultInsteadOfDeletingIt covers the one
// delete this provider does not send: the route accepts a DELETE for the account default
// with no complaint and there is no undelete, so the destroy forgets the policy instead
// of releasing the baseline for every workspace that resolves to it.
func TestStubGuardrailPolicyAccountRefusesToDeleteTheDefault(t *testing.T) {
	t.Setenv("TF_ACC", "1")

	srv, stub := newGuardrailPolicyAccountStub(t)

	cfg := accountStubConfig(srv.URL, "openrouter_guardrail_policy_account_moderation", `
  config = {
    content_filter_builtins = [
      {
        action = "block"
        slug   = "secrets"
      }
    ]
  }
  is_default = true
  name       = "account default moderation"
`)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{Config: cfg},
		},
	})

	if stub.deleteCalls != 0 {
		t.Errorf("%d DELETE(s) reached the stub; the destroy has to be refused before it reaches the route", stub.deleteCalls)
	}
}
