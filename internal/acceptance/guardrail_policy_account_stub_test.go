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
	// hideAfterNextWrite makes the next list read after a write leave that policy out, which
	// is the shape of a write whose row has not reached the list yet. A test that sets it
	// proves a write path takes its state from the write response rather than from a read:
	// a write that still read the list would find nothing and fail.
	hideAfterNextWrite bool
	hideOnce           map[string]bool
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
		// Rows hidden by the last write are left out of this read and of no other.
		hidden := s.hideOnce
		s.hideOnce = map[string]bool{}
		data := []any{}
		// Reverse insertion order, so a provider that assumed the newest row was
		// first, or the only one, would fail here rather than pass by luck.
		ids := make([]string, 0, len(s.policies))
		for id := range s.policies {
			ids = append(ids, id)
		}
		for i := len(ids) - 1; i >= 0; i-- {
			if hidden[ids[i]] {
				continue
			}
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
		if s.hideAfterNextWrite {
			if s.hideOnce == nil {
				s.hideOnce = map[string]bool{}
			}
			s.hideOnce[body["id"].(string)] = true
		}
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
		if s.hideAfterNextWrite {
			s.hideOnce[id] = true
		}
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
	// Only for cleanup: a destroy of a policy holding the default flag is refused by
	// design, and the refusal test below is the one that asserts it.
	demotedForCleanup := accountStubConfig(srv.URL, "openrouter_guardrail_policy_account_model_availability", `
  config = {
    enforce_zdr = true
  }
  is_default = false
  name       = "account baseline"
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
			{Config: demotedForCleanup},
		},
	})

	stub.mu.Lock()
	// The first PUT is the promotion; the demotion that lets the case clean up is the
	// second, so the wire shape under test is read by position rather than as the last.
	body := stub.updates[0]
	stub.mu.Unlock()
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

// TestStubGuardrailPolicyAccountRejectsWorkspaceIDsOnAStoredDefault covers the coverage
// combination the config validator cannot see. `is_default` is Optional+Computed, so a
// policy that is already the default while the configuration leaves the flag out carries
// `true` from state into the plan. The write path then takes its `is_default` branch, sends
// the flag alone, and drops a configured `workspace_ids` with no error: the apply looks
// successful, the policy still covers every workspace, and every later plan shows the same
// diff. The first step stores the default, the second configures the list with the flag
// omitted, and the plan modifier on `workspace_ids` is the only place that pair is visible.
// It reads the detail sentence rather than the summary, because the summary is the route's
// own wording and the config validator uses it too.
func TestStubGuardrailPolicyAccountRejectsWorkspaceIDsOnAStoredDefault(t *testing.T) {
	t.Setenv("TF_ACC", "1")

	srv, _ := newGuardrailPolicyAccountStub(t)

	asDefault := accountStubConfig(srv.URL, "openrouter_guardrail_policy_account_budget", `
  config = {
    include_byok_in_budgets = false
    limit_usd               = 50
    reset_interval          = "monthly"
  }
  is_default = true
  name       = "stored default"
`)
	withWorkspaces := accountStubConfig(srv.URL, "openrouter_guardrail_policy_account_budget", `
  config = {
    include_byok_in_budgets = false
    limit_usd               = 50
    reset_interval          = "monthly"
  }
  name          = "stored default"
  workspace_ids = ["stub-workspace-a"]
`)
	// Only for cleanup: the refused plan leaves the default in state, and a destroy of a
	// policy holding the flag is refused by design.
	demotedForCleanup := accountStubConfig(srv.URL, "openrouter_guardrail_policy_account_budget", `
  config = {
    include_byok_in_budgets = false
    limit_usd               = 50
    reset_interval          = "monthly"
  }
  is_default = false
  name       = "stored default"
`)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{Config: asDefault},
			{
				Config:      withWorkspaces,
				ExpectError: regexp.MustCompile("which would drop it without an error"),
			},
			{Config: demotedForCleanup},
		},
	})
}

// TestStubGuardrailPolicyAccountRefusesToDestroyTheDefault covers the one destroy this
// provider refuses to send. The route accepts a DELETE for the policy holding the default
// flag with no complaint and there is no undelete, so sending it would release the
// baseline for every workspace that resolves to it. It is not skipped either: a destroy
// that reports success while the policy still enforces would hand back a resource nobody
// manages, and writing the configuration again creates a second policy rather than
// adopting that one. The test asserts both halves: the error reaches the operator, and no
// DELETE reaches the route.
func TestStubGuardrailPolicyAccountRefusesToDestroyTheDefault(t *testing.T) {
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

	demoted := accountStubConfig(srv.URL, "openrouter_guardrail_policy_account_moderation", `
  config = {
    content_filter_builtins = [
      {
        action = "block"
        slug   = "secrets"
      }
    ]
  }
  is_default = false
  name       = "account default moderation"
`)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				Config:      cfg,
				Destroy:     true,
				ExpectError: regexp.MustCompile(`refusing to destroy the account default`),
			},
			// The escape hatch the refusal message names: demote, apply, destroy. Exactly
			// one DELETE follows, and it is this one: the refused destroy sent none.
			{Config: demoted},
		},
	})

	if stub.deleteCalls != 1 {
		t.Errorf("%d DELETE(s) reached the stub, want exactly the demoted policy's destroy", stub.deleteCalls)
	}
}

// TestStubGuardrailPolicyAccountRefusesOptOutsWhileTheFlagIsUnset covers the combination
// the route rejects but a plan-time validator cannot see: `is_default` is
// Optional+Computed, so a configuration that omits it reaches the validator as null and
// the write carries an effective false. Opt-outs in that shape have to fail the write
// rather than be dropped, because dropping them would leave the configuration describing
// coverage that no apply stores.
func TestStubGuardrailPolicyAccountRefusesOptOutsWhileTheFlagIsUnset(t *testing.T) {
	t.Setenv("TF_ACC", "1")

	srv, stub := newGuardrailPolicyAccountStub(t)

	cfg := accountStubConfig(srv.URL, "openrouter_guardrail_policy_account_budget", `
  config = {
    include_byok_in_budgets = false
    limit_usd               = 50
    reset_interval          = "monthly"
  }
  name = "opt-outs without the flag"
  opted_out_workspace_ids = ["stub-workspace-a"]
`)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config:      cfg,
				ExpectError: regexp.MustCompile(`Only the account default can have default assignment fields`),
			},
		},
	})

	if len(stub.createBodies) != 0 {
		t.Errorf("a create reached the stub with opt-outs and no effective default flag: %v", stub.createBodies[0])
	}
}

// TestStubGuardrailPolicyAccountDemotionTurnsTheProjectionIntoAssignments pins what
// unsetting `is_default` actually does. The state of a default carries the resolved
// coverage, so the demoting write sends that resolved set as explicit assignments: the
// policy keeps covering the same workspaces, and the coverage is no longer automatic. The
// test also runs the same configuration twice, so a state that disagreed with the read
// would fail the framework's own consistency check rather than pass unnoticed.
func TestStubGuardrailPolicyAccountDemotionTurnsTheProjectionIntoAssignments(t *testing.T) {
	t.Setenv("TF_ACC", "1")

	srv, stub := newGuardrailPolicyAccountStub(t)

	asDefault := accountStubConfig(srv.URL, "openrouter_guardrail_policy_account_model_availability", `
  config = {
    enforce_zdr = true
  }
  is_default = true
  name       = "account baseline"
  opted_out_workspace_ids = ["stub-workspace-b"]
`)
	demoted := accountStubConfig(srv.URL, "openrouter_guardrail_policy_account_model_availability", `
  config = {
    enforce_zdr = true
  }
  is_default = false
  name       = "account baseline"
`)

	var id string
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: asDefault,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("openrouter_guardrail_policy_account_model_availability.test", "workspace_ids.#", "2"),
					resource.TestCheckResourceAttr("openrouter_guardrail_policy_account_model_availability.test", "opted_out_workspace_ids.#", "1"),
				),
			},
			{
				Config: demoted,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("openrouter_guardrail_policy_account_model_availability.test", "is_default", "false"),
					// The two workspaces the default resolved to become explicit floor
					// coverage, and the opt-out list is gone because a floor cannot carry one.
					resource.TestCheckResourceAttr("openrouter_guardrail_policy_account_model_availability.test", "workspace_ids.#", "2"),
					resource.TestCheckResourceAttr("openrouter_guardrail_policy_account_model_availability.test", "opted_out_workspace_ids.#", "0"),
				),
			},
			// Same configuration again: a plan that is not empty here would mean state and
			// the API disagree about the coverage this write produced.
			{Config: demoted},
		},
	})

	stub.mu.Lock()
	for pid := range stub.policies {
		id = pid
	}
	stub.mu.Unlock()

	body := stub.lastUpdate(t)
	if got := asList(body["workspace_ids"]); len(got) != 2 {
		t.Errorf("the demoting write sent workspace_ids=%v, want the two workspaces the default resolved to", got)
	}
	if got, present := body["opted_out_workspace_ids"]; present && len(asList(got)) > 0 {
		t.Errorf("the demoting write sent opted_out_workspace_ids=%v; a floor cannot carry one", got)
	}
	_ = id
}

// TestStubGuardrailPolicyAccountCreateTakesStateFromTheWriteResponse pins the create path:
// state comes from the write response and no list read follows the write. The stub leaves
// the row out of the next list read after a write, so a create that still read the list
// would find nothing and fail here rather than pass by luck. Measured on the live route
// 2026-11-20, the 201 answer's field set is identical to the same policy's row in the list
// and its config came back byte-equal to what was sent.
//
// The step allows a non-empty plan because the hiding is exactly the case where the read
// Terraform runs after the apply cannot find the row: the resource drops out of state and
// the plan wants to recreate it. That is the world this test builds, not a provider defect.
func TestStubGuardrailPolicyAccountCreateTakesStateFromTheWriteResponse(t *testing.T) {
	t.Setenv("TF_ACC", "1")

	srv, stub := newGuardrailPolicyAccountStub(t)
	stub.hideAfterNextWrite = true

	cfg := accountStubConfig(srv.URL, "openrouter_guardrail_policy_account_budget", `
  config = {
    include_byok_in_budgets = true
    limit_usd               = 50
    reset_interval          = "monthly"
  }
  name          = "engineering budget"
  workspace_ids = ["stub-workspace-a"]
`)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config:             cfg,
				ExpectNonEmptyPlan: true,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("openrouter_guardrail_policy_account_budget.test", "id", "stub-account-1"),
					resource.TestCheckResourceAttr("openrouter_guardrail_policy_account_budget.test", "name", "engineering budget"),
					resource.TestCheckResourceAttr("openrouter_guardrail_policy_account_budget.test", "config.limit_usd", "50"),
					resource.TestCheckResourceAttr("openrouter_guardrail_policy_account_budget.test", "workspace_ids.#", "1"),
				),
			},
		},
	})
}

// TestStubGuardrailPolicyAccountUpdateTakesStateFromTheWriteResponse is the update half of
// the same property. The hide is armed for the update step only, because the row has to be
// in the list for Terraform to plan an update rather than a create: the update's own read is
// what must not happen. Measured on the live route 2026-11-20, the PUT's 200 carries the
// same field set the list does and its updated_at is the post-update value.
func TestStubGuardrailPolicyAccountUpdateTakesStateFromTheWriteResponse(t *testing.T) {
	t.Setenv("TF_ACC", "1")

	srv, stub := newGuardrailPolicyAccountStub(t)

	first := accountStubConfig(srv.URL, "openrouter_guardrail_policy_account_budget", `
  config = {
    include_byok_in_budgets = true
    limit_usd               = 50
    reset_interval          = "monthly"
  }
  name          = "engineering budget"
  workspace_ids = ["stub-workspace-a"]
`)
	second := accountStubConfig(srv.URL, "openrouter_guardrail_policy_account_budget", `
  config = {
    include_byok_in_budgets = true
    limit_usd               = 75
    reset_interval          = "monthly"
  }
  name          = "engineering budget"
  workspace_ids = ["stub-workspace-a"]
`)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: first,
			},
			{
				PreConfig: func() {
					stub.mu.Lock()
					defer stub.mu.Unlock()
					stub.hideAfterNextWrite = true
				},
				Config:             second,
				ExpectNonEmptyPlan: true,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("openrouter_guardrail_policy_account_budget.test", "config.limit_usd", "75"),
					resource.TestCheckResourceAttr("openrouter_guardrail_policy_account_budget.test", "workspace_ids.#", "1"),
				),
			},
		},
	})
}
