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
		hideOnce: map[string]bool{},
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
		// updatedAt moves on a PUT, so a test can tell state taken from the write response
		// from state taken from a read: a read sees the same row either way, and only the
		// response to the write that just happened carries the new value first.
		updatedAt := any("2026-11-18T00:00:00Z")
		if v, ok := p["updated_at"]; ok {
			updatedAt = v
		}
		out := map[string]any{
			"id":                          p["id"],
			"name":                        p["name"],
			"policy_type":                 p["policy_type"],
			"config":                      p["config"],
			"created_at":                  "2026-11-18T00:00:00Z",
			"updated_at":                  updatedAt,
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
			"updated_at":              "2026-11-18T00:05:00Z",
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

// accountPolicyFamily is one of the three account policy families the account route serves.
// The family differences are the provider's own mapping code, so a rule proven against one
// says nothing about the other two: every scenario below runs against all three.
type accountPolicyFamily struct {
	// name is how the family is spelled in the resource type and in a subtest's name.
	name string
	// resource is the resource type, so a case can build its address and its configuration.
	resource string
	// config is the account policy config block alone, indented, with a leading newline so
	// a case can append the coverage attributes it needs.
	config string
	// configKey and configWant are one value inside config that a case asserts reached
	// state, because a case that checked only ids and names would pass with the config
	// dropped on the floor.
	configKey  string
	configWant string
}

// addr is the Terraform address of the single resource each case declares.
func (f accountPolicyFamily) addr() string { return f.resource + ".test" }

var accountPolicyFamilies = []accountPolicyFamily{
	{
		name:     "budget",
		resource: "openrouter_guardrail_policy_account_budget",
		config: `
  config = {
    include_byok_in_budgets = true
    limit_usd               = 50
    reset_interval          = "monthly"
  }`,
		configKey:  "config.limit_usd",
		configWant: "50",
	},
	{
		name:     "moderation",
		resource: "openrouter_guardrail_policy_account_moderation",
		config: `
  config = {
    content_filter_builtins = [
      {
        action = "block"
        slug   = "secrets"
      }
    ]
  }`,
		configKey:  "config.content_filter_builtins.#",
		configWant: "1",
	},
	{
		name:     "model_availability",
		resource: "openrouter_guardrail_policy_account_model_availability",
		config: `
  config = {
    enforce_zdr = true
  }`,
		configKey:  "config.enforce_zdr",
		configWant: "true",
	},
}

// famConfig is the family's config block with the coverage attributes a case needs.
func famConfig(srv string, fam accountPolicyFamily, tail string) string {
	return accountStubConfig(srv, fam.resource, fam.config+tail)
}

// accountPolicyScenario is one behaviour every account policy family has to exhibit. The
// runner takes the family rather than a prepared configuration, because what differs
// between families is the mapping inside each resource file.
type accountPolicyScenario struct {
	name string
	run  func(t *testing.T, fam accountPolicyFamily)
}

var accountPolicyScenarios = []accountPolicyScenario{
	{"create and read", runAccountPolicyCreateAndRead},
	{"update omitting coverage keeps stored coverage", runAccountPolicyUpdateKeepsStoredCoverage},
	{"default sends no workspace ids", runAccountPolicyDefaultSendsNoWorkspaceIDs},
	{"rejects coverage with the wrong default", runAccountPolicyRejectsCoverageWithWrongDefault},
	{"rejects workspace ids on a stored default", runAccountPolicyRejectsWorkspaceIDsOnStoredDefault},
	{"refuses to destroy the default", runAccountPolicyRefusesToDestroyDefault},
	{"refuses opt-outs while the flag is unset", runAccountPolicyRefusesOptOutsWithoutFlag},
	{"demotion turns the projection into assignments", runAccountPolicyDemotionTurnsProjectionIntoAssignments},
	{"create takes state from the write response", runAccountPolicyCreateTakesStateFromWriteResponse},
	{"update takes state from the write response", runAccountPolicyUpdateTakesStateFromWriteResponse},
}

// TestStubGuardrailPolicyAccountRules runs every account policy scenario against every
// family. Each pair is its own subtest with its own stub server, so a failure names the
// family and the behaviour rather than only the file.
func TestStubGuardrailPolicyAccountRules(t *testing.T) {
	t.Setenv("TF_ACC", "1")

	for _, fam := range accountPolicyFamilies {
		for _, sc := range accountPolicyScenarios {
			t.Run(fam.name+"/"+sc.name, func(t *testing.T) {
				sc.run(t, fam)
			})
		}
	}
}

// TestStubGuardrailPolicyAccountRuleCoverage pins the size of the sweep above. The table is
// what makes every scenario run for every family, so the count is the property: a scenario
// added without a family entry, or a fourth family added without scenarios, has to change
// this number deliberately. Before this sweep the two generated families had no stub
// coverage of their own, which is the whole reason the sweep exists.
func TestStubGuardrailPolicyAccountRuleCoverage(t *testing.T) {
	if got, want := len(accountPolicyFamilies)*len(accountPolicyScenarios), 30; got != want {
		t.Errorf("%d family/scenario pairs in the table, want %d", got, want)
	}
}

// runAccountPolicyCreateAndRead covers the whole happy path: a create whose body carries
// the type and whose state comes from the write response, and the read Terraform issues
// once the resource exists, which must stay unpaginated.
func runAccountPolicyCreateAndRead(t *testing.T, fam accountPolicyFamily) {
	srv, stub := newGuardrailPolicyAccountStub(t)

	cfg := famConfig(srv.URL, fam, `
  name          = "engineering policy"
  workspace_ids = ["stub-workspace-a"]
`)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(fam.addr(), "id"),
					resource.TestCheckResourceAttr(fam.addr(), "is_default", "false"),
					resource.TestCheckResourceAttr(fam.addr(), "workspace_ids.#", "1"),
					resource.TestCheckResourceAttr(fam.addr(), fam.configKey, fam.configWant),
				),
			},
		},
	})

	create := stub.lastCreate(t)
	if got := create["policy_type"]; got != fam.name {
		t.Errorf("create sent policy_type=%v, want %q", got, fam.name)
	}
	if got, present := create["is_default"]; !present || got != false {
		t.Errorf("create sent is_default=%v (present=%v), want an explicit false: the route replaces coverage wholesale and an absent flag is not a false", got, present)
	}
	if got := asList(create["workspace_ids"]); !reflect.DeepEqual(got, []any{"stub-workspace-a"}) {
		t.Errorf("create sent workspace_ids=%v, want [stub-workspace-a]", got)
	}
	stub.listWasUnpaginated(t)
}

// runAccountPolicyUpdateKeepsStoredCoverage is the preservation test. Step two omits
// workspace_ids entirely, so a provider that built the body from the configuration alone
// would send none and the route would clear the coverage: measured 2026-11-18, a PUT
// without workspace_ids left the policy covering zero workspaces.
func runAccountPolicyUpdateKeepsStoredCoverage(t *testing.T, fam accountPolicyFamily) {
	srv, stub := newGuardrailPolicyAccountStub(t)

	// Rebuilding the body from state rather than from the configuration is what this
	// pins, so step two changes the name alone and the coverage has to survive it.
	first := famConfig(srv.URL, fam, `
  name          = "engineering policy"
  workspace_ids = ["stub-workspace-a"]
`)
	second := famConfig(srv.URL, fam, `
  name = "renamed policy"
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
					resource.TestCheckResourceAttr(fam.addr(), "name", "renamed policy"),
					resource.TestCheckResourceAttr(fam.addr(), "workspace_ids.#", "1"),
					resource.TestCheckResourceAttr(fam.addr(), "workspace_ids.0", "stub-workspace-a"),
					resource.TestCheckResourceAttr(fam.addr(), fam.configKey, fam.configWant),
				),
			},
		},
	})

	body := stub.lastUpdate(t)
	if got := asList(body["workspace_ids"]); !reflect.DeepEqual(got, []any{"stub-workspace-a"}) {
		t.Errorf("the update sent workspace_ids=%v, want the stored [stub-workspace-a]: an omitted coverage list has to travel from state, because the route clears what it is not sent", got)
	}
}

// runAccountPolicyDefaultSendsNoWorkspaceIDs pins the rule the live route enforces with 400
// "The account default covers workspaces through opt-outs": once a policy is the default,
// workspace_ids is neither configured nor sent, and the state carries the resolved coverage
// the read returns instead.
func runAccountPolicyDefaultSendsNoWorkspaceIDs(t *testing.T, fam accountPolicyFamily) {
	srv, stub := newGuardrailPolicyAccountStub(t)

	floor := famConfig(srv.URL, fam, `
  name          = "account baseline"
  workspace_ids = ["stub-workspace-a"]
`)
	asDefault := famConfig(srv.URL, fam, `
  is_default = true
  name       = "account baseline"
  opted_out_workspace_ids = ["stub-workspace-b"]
`)
	// Only for cleanup: a destroy of a policy holding the default flag is refused by
	// design, and the refusal scenario is the one that asserts it.
	demotedForCleanup := famConfig(srv.URL, fam, `
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
					resource.TestCheckResourceAttr(fam.addr(), "is_default", "true"),
					resource.TestCheckResourceAttr(fam.addr(), "opted_out_workspace_ids.#", "1"),
					// The projection: everything else the stub knows about.
					resource.TestCheckResourceAttr(fam.addr(), "workspace_ids.#", "2"),
				),
			},
			{Config: demotedForCleanup},
		},
	})

	// The first PUT is the promotion; the demotion that lets the case clean up is the
	// second, so the wire shape under test is read by position rather than as the last.
	stub.mu.Lock()
	body := stub.updates[0]
	stub.mu.Unlock()
	if got, present := body["workspace_ids"]; present {
		t.Errorf("the update sent workspace_ids=%v; the route refuses the account default that carries one", got)
	}
	if got, present := body["is_default"]; !present || got != true {
		t.Errorf("the update sent is_default=%v (present=%v), want an explicit true", got, present)
	}
}

// runAccountPolicyRejectsCoverageWithWrongDefault covers the two combinations the route
// answers with 400, at plan time rather than at apply.
func runAccountPolicyRejectsCoverageWithWrongDefault(t *testing.T, fam accountPolicyFamily) {
	srv, _ := newGuardrailPolicyAccountStub(t)

	defaultWithWorkspaces := famConfig(srv.URL, fam, `
  is_default    = true
  name          = "bad default"
  workspace_ids = ["stub-workspace-a"]
`)
	optedOutOnAFloor := famConfig(srv.URL, fam, `
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

// runAccountPolicyRejectsWorkspaceIDsOnStoredDefault covers the coverage combination the
// config validator cannot see. `is_default` is Optional+Computed, so a policy that is
// already the default while the configuration leaves the flag out carries `true` from state
// into the plan. The write path then takes its `is_default` branch, sends the flag alone,
// and drops a configured `workspace_ids` with no error: the apply looks successful, the
// policy still covers every workspace, and every later plan shows the same diff. The first
// step stores the default, the second configures the list with the flag omitted, and the
// plan modifier on `workspace_ids` is the only place that pair is visible. It reads the
// detail sentence rather than the summary, because the summary is the route's own wording
// and the config validator uses it too.
func runAccountPolicyRejectsWorkspaceIDsOnStoredDefault(t *testing.T, fam accountPolicyFamily) {
	srv, _ := newGuardrailPolicyAccountStub(t)

	asDefault := famConfig(srv.URL, fam, `
  is_default = true
  name       = "stored default"
`)
	withWorkspaces := famConfig(srv.URL, fam, `
  name          = "stored default"
  workspace_ids = ["stub-workspace-a"]
`)
	// Only for cleanup: the refused plan leaves the default in state, and a destroy of a
	// policy holding the flag is refused by design.
	demotedForCleanup := famConfig(srv.URL, fam, `
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

// runAccountPolicyRefusesToDestroyDefault covers the one destroy this provider refuses to
// send. The route accepts a DELETE for the policy holding the default flag with no
// complaint and there is no undelete, so sending it would release the baseline for every
// workspace that resolves to it. It is not skipped either: a destroy that reports success
// while the policy still enforces would hand back a resource nobody manages, and writing
// the configuration again creates a second policy rather than adopting that one. The
// scenario asserts both halves: the error reaches the operator, and no DELETE reaches the
// route.
func runAccountPolicyRefusesToDestroyDefault(t *testing.T, fam accountPolicyFamily) {
	srv, stub := newGuardrailPolicyAccountStub(t)

	cfg := famConfig(srv.URL, fam, `
  is_default = true
  name       = "account default policy"
`)
	demoted := famConfig(srv.URL, fam, `
  is_default = false
  name       = "account default policy"
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

// runAccountPolicyRefusesOptOutsWithoutFlag covers the combination the route rejects but a
// plan-time validator cannot see: `is_default` is Optional+Computed, so a configuration
// that omits it reaches the validator as null and the write carries an effective false.
// Opt-outs in that shape have to fail the write rather than be dropped, because dropping
// them would leave the configuration describing coverage that no apply stores.
func runAccountPolicyRefusesOptOutsWithoutFlag(t *testing.T, fam accountPolicyFamily) {
	srv, stub := newGuardrailPolicyAccountStub(t)

	cfg := famConfig(srv.URL, fam, `
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

// runAccountPolicyDemotionTurnsProjectionIntoAssignments pins what unsetting `is_default`
// actually does. The state of a default carries the resolved coverage, so the demoting
// write sends that resolved set as explicit assignments: the policy keeps covering the same
// workspaces, and the coverage is no longer automatic. The scenario also runs the same
// configuration twice, so a state that disagreed with the read would fail the framework's
// own consistency check rather than pass unnoticed.
func runAccountPolicyDemotionTurnsProjectionIntoAssignments(t *testing.T, fam accountPolicyFamily) {
	srv, stub := newGuardrailPolicyAccountStub(t)

	asDefault := famConfig(srv.URL, fam, `
  is_default = true
  name       = "account baseline"
  opted_out_workspace_ids = ["stub-workspace-b"]
`)
	demoted := famConfig(srv.URL, fam, `
  is_default = false
  name       = "account baseline"
`)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: asDefault,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(fam.addr(), "workspace_ids.#", "2"),
					resource.TestCheckResourceAttr(fam.addr(), "opted_out_workspace_ids.#", "1"),
				),
			},
			{
				Config: demoted,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(fam.addr(), "is_default", "false"),
					// The two workspaces the default resolved to become explicit floor
					// coverage, and the opt-out list is gone because a floor cannot carry one.
					resource.TestCheckResourceAttr(fam.addr(), "workspace_ids.#", "2"),
					resource.TestCheckResourceAttr(fam.addr(), "opted_out_workspace_ids.#", "0"),
				),
			},
			// Same configuration again: a plan that is not empty here would mean state and
			// the API disagree about the coverage this write produced.
			{Config: demoted},
		},
	})

	body := stub.lastUpdate(t)
	if got := asList(body["workspace_ids"]); len(got) != 2 {
		t.Errorf("the demoting write sent workspace_ids=%v, want the two workspaces the default resolved to", got)
	}
	if got, present := body["opted_out_workspace_ids"]; present && len(asList(got)) > 0 {
		t.Errorf("the demoting write sent opted_out_workspace_ids=%v; a floor cannot carry one", got)
	}
}

// runAccountPolicyCreateTakesStateFromWriteResponse pins the create path: state comes from
// the write response and no list read follows the write. The stub leaves the row out of the
// next list read after a write, so a create that still read the list would find nothing and
// fail here rather than pass by luck. Measured on the live route 2026-11-20 with a
// non-default budget policy, the 201 answer's field set is identical to that policy's row in
// the list and its config came back byte-equal to what was sent.
//
// The step allows a non-empty plan because the hiding is exactly the case where the read
// Terraform runs after the apply cannot find the row: the resource drops out of state and
// the plan wants to recreate it. That is the world this test builds, not a provider defect.
func runAccountPolicyCreateTakesStateFromWriteResponse(t *testing.T, fam accountPolicyFamily) {
	srv, stub := newGuardrailPolicyAccountStub(t)
	stub.hideAfterNextWrite = true

	cfg := famConfig(srv.URL, fam, `
  name          = "engineering policy"
  workspace_ids = ["stub-workspace-a"]
`)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config:             cfg,
				ExpectNonEmptyPlan: true,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(fam.addr(), "id", "stub-account-1"),
					resource.TestCheckResourceAttr(fam.addr(), "name", "engineering policy"),
					resource.TestCheckResourceAttr(fam.addr(), fam.configKey, fam.configWant),
					resource.TestCheckResourceAttr(fam.addr(), "workspace_ids.#", "1"),
				),
			},
		},
	})

	// The reads are counted rather than assumed: a create reads the list zero times, so the
	// only read this case sees is the one Terraform issues after the apply. A create that
	// still read would either fail on the hidden row or add a second read here.
	stub.mu.Lock()
	reads := len(stub.listQueries)
	stub.mu.Unlock()
	if reads != 1 {
		t.Errorf("the account policy list was read %d time(s); want 1, the read Terraform issues after the apply, because a create reads it zero times", reads)
	}
}

// runAccountPolicyUpdateTakesStateFromWriteResponse is the update half of the same property.
// The hide is armed for the update step alone, because the row has to be in the list for
// Terraform to plan an update rather than a create: the update's own read is what must not
// happen. Measured on the live route 2026-11-20 with a non-default budget policy, the PUT's
// 200 carries the same field set the list does and its updated_at is the post-update value.
func runAccountPolicyUpdateTakesStateFromWriteResponse(t *testing.T, fam accountPolicyFamily) {
	srv, stub := newGuardrailPolicyAccountStub(t)

	first := famConfig(srv.URL, fam, `
  name          = "engineering policy"
  workspace_ids = ["stub-workspace-a"]
`)
	second := famConfig(srv.URL, fam, `
  name          = "renamed policy"
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
					resource.TestCheckResourceAttr(fam.addr(), "name", "renamed policy"),
					resource.TestCheckResourceAttr(fam.addr(), "workspace_ids.#", "1"),
					resource.TestCheckResourceAttr(fam.addr(), "updated_at", "2026-11-18T00:05:00Z"),
				),
			},
		},
	})

	// Three reads: one after step 1's apply, one to refresh before step 2's plan and one
	// after step 2's apply. The update reads none; while it still read, this counted four.
	stub.mu.Lock()
	reads := len(stub.listQueries)
	stub.mu.Unlock()
	if reads != 3 {
		t.Errorf("the account policy list was read %d time(s); want 3, the reads Terraform issues around the two applies, because an update reads it zero times", reads)
	}
}
