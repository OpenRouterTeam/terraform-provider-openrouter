package acceptance

// Offline coverage for the three workspace-scoped guardrail policy resources
// (openrouter_guardrail_policy_budget, _moderation, _model_availability).
//
// The live acceptance suite cannot exercise these routes yet: the policy
// backend is behind a flag on the test org, so every policy path answers 404
// there and a live test would prove nothing either way. This stub enforces the
// contract the provider has to satisfy and needs no credentials and no
// network, so it runs in the default offline gate.
//
// What it locks down, in the order the generated resources get it wrong:
//
//   - update sends the whole config, not just the fields that changed. The
//     route replaces the config wholesale, so a diff-shaped update silently
//     drops every field the caller left alone.
//   - an empty subject list reaches the wire as [], which is how assignments
//     are cleared. The SDK tags these fields `omitzero`, so an empty list that
//     serializes as nothing makes clearing inexpressible while Terraform
//     believes it succeeded.
//   - workspace_id travels as the documented query parameter on read and
//     delete. A workspace-scoped key needs no value to read its own policies
//     and an organization key cannot read without one, so a provider that
//     drops the parameter is untestable rather than wrong.
//   - a 404 on read removes the resource instead of failing the plan, and a
//     delete of something already gone is accepted.
//   - import hydrates from the policy id alone.
//
// Run: go test ./internal/acceptance -run TestStubGuardrailPolicy -v

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const (
	stubPolicyID    = "55555555-5555-5555-5555-555555555555"
	stubWorkspaceID = "66666666-6666-6666-6666-666666666666"
)

// guardrailPolicyStub answers like the Management API's policy routes: policies
// are stored per type, reads answer with the detail shape, and a read after a
// delete is a 404.
type guardrailPolicyStub struct {
	mu sync.Mutex
	// policies is keyed by type slug ("budget", "moderation", ...).
	policies map[string]map[string]any
	// createBodies and updateBodies are keyed the same way; last write wins,
	// which is what the assertions inspect.
	createBodies  map[string]map[string]any
	updateBodies  map[string]map[string]any
	getQueries    []url.Values
	deleteQueries []url.Values
	deleted       map[string]bool
}

func newGuardrailPolicyStub(t *testing.T) (*httptest.Server, *guardrailPolicyStub) {
	t.Helper()
	s := &guardrailPolicyStub{
		policies:     map[string]map[string]any{},
		createBodies: map[string]map[string]any{},
		updateBodies: map[string]map[string]any{},
		deleted:      map[string]bool{},
	}

	writeJSON := func(w http.ResponseWriter, code int, body any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(body)
	}
	notFound := func(w http.ResponseWriter, message string) {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": map[string]any{"code": 404, "message": message},
		})
	}

	// The detail shape is what read hydrates from, and it is the only shape
	// carrying the subject lists, so the stub always answers every required
	// field rather than relying on the provider tolerating omissions.
	detail := func(p map[string]any) map[string]any {
		list := func(key string) []any {
			if v, ok := p[key].([]any); ok {
				return v
			}
			return []any{}
		}
		isDefault, ok := p["is_workspace_default"].(bool)
		if !ok {
			isDefault = false
		}
		return map[string]any{
			"id":                        p["id"],
			"name":                      p["name"],
			"policy_type":               p["policy_type"],
			"config":                    p["config"],
			"is_workspace_default":      isDefault,
			"member_ids":                list("member_ids"),
			"api_key_hashes":            list("api_key_hashes"),
			"opted_out_member_ids":      list("opted_out_member_ids"),
			"opted_out_api_key_hashes":  list("opted_out_api_key_hashes"),
			"overridden_member_ids":     []any{},
			"overridden_api_key_hashes": []any{},
			"updated_at":                "2026-11-11T00:00:00Z",
		}
	}
	// The short shape is what create and update answer with.
	short := func(p map[string]any) map[string]any {
		return map[string]any{
			"id":          p["id"],
			"name":        p["name"],
			"policy_type": p["policy_type"],
			"config":      p["config"],
		}
	}

	mux := http.NewServeMux()
	for _, slug := range []string{"budget", "moderation", "model-availability"} {
		mux.HandleFunc("POST /guardrails/policies/"+slug, func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"code": 400, "message": "bad json"}})
				return
			}
			body["id"] = stubPolicyID
			s.mu.Lock()
			s.createBodies[slug] = body
			s.policies[slug] = body
			s.mu.Unlock()
			writeJSON(w, http.StatusCreated, map[string]any{"data": short(body)})
		})

		mux.HandleFunc("GET /guardrails/policies/"+slug+"/{id}", func(w http.ResponseWriter, r *http.Request) {
			s.mu.Lock()
			s.getQueries = append(s.getQueries, r.URL.Query())
			p, ok := s.policies[slug]
			gone := s.deleted[slug]
			s.mu.Unlock()
			if !ok || gone {
				notFound(w, "Policy not found.")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"data": detail(p)})
		})

		mux.HandleFunc("PUT /guardrails/policies/"+slug+"/{id}", func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"code": 400, "message": "bad json"}})
				return
			}
			s.mu.Lock()
			s.updateBodies[slug] = body
			// The route replaces what it is given, so the stored policy is the
			// body plus whatever the body did not name.
			p := s.policies[slug]
			for k, v := range body {
				p[k] = v
			}
			s.mu.Unlock()
			writeJSON(w, http.StatusOK, map[string]any{"data": short(p)})
		})

		mux.HandleFunc("DELETE /guardrails/policies/"+slug+"/{id}", func(w http.ResponseWriter, r *http.Request) {
			s.mu.Lock()
			s.deleteQueries = append(s.deleteQueries, r.URL.Query())
			_, ok := s.policies[slug]
			already := s.deleted[slug]
			s.deleted[slug] = true
			s.mu.Unlock()
			if !ok || already {
				// The generated resource accepts 200 or 404 here, which is what
				// makes a destroy of something already gone a no-op.
				notFound(w, "Policy not found.")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"data": true})
		})
	}

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, s
}

func (s *guardrailPolicyStub) lastUpdate(t *testing.T, slug string) map[string]any {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	body, ok := s.updateBodies[slug]
	if !ok {
		t.Fatalf("no update reached the stub for %s", slug)
	}
	return body
}

func (s *guardrailPolicyStub) lastCreate(t *testing.T, slug string) map[string]any {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	body, ok := s.createBodies[slug]
	if !ok {
		t.Fatalf("no create reached the stub for %s", slug)
	}
	return body
}

// stubProviderBlock renders a provider block pointed at the stub server.
func stubProviderBlock(serverURL string) string {
	return fmt.Sprintf(`
provider "openrouter" {
  api_key    = "sk-stub-not-a-real-key"
  server_url = %q
}
`, serverURL)
}

// stubPolicyResource renders one policy resource of the named type. attrs
// carries the type-specific body, and must set name.
func stubPolicyResource(slug, policyType, attrs string) string {
	return fmt.Sprintf(`
resource "openrouter_guardrail_policy_%s" "test" {
  policy_type = %q
%s}
`, slug, policyType, attrs)
}

// guardrailPolicyStubConfig renders a provider block and one policy resource.
func guardrailPolicyStubConfig(serverURL, slug, policyType, attrs string) string {
	return stubProviderBlock(serverURL) + stubPolicyResource(slug, policyType, attrs)
}

// TestStubGuardrailPolicyBudgetUpdateSendsWholeConfig is the regression test
// for the whole-config replacement rule: a step that changes only the name must
// still send every configured config field.
func TestStubGuardrailPolicyBudgetUpdateSendsWholeConfig(t *testing.T) {
	t.Setenv("TF_ACC", "1")

	srv, stub := newGuardrailPolicyStub(t)

	config := func(name string) string {
		return guardrailPolicyStubConfig(srv.URL, "budget", "budget", fmt.Sprintf(`
  config = {
    include_byok_in_budgets = true
    limit_usd               = 50
    reset_interval          = "monthly"
  }
  name = %q
`, name))
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config("stub budget"),
				Check:  resource.TestCheckResourceAttrSet("openrouter_guardrail_policy_budget.test", "id"),
			},
			{
				Config: config("renamed budget"),
				Check:  resource.TestCheckResourceAttr("openrouter_guardrail_policy_budget.test", "name", "renamed budget"),
			},
		},
	})

	body := stub.lastUpdate(t, "budget")
	if got := body["name"]; got != "renamed budget" {
		t.Errorf("update sent name=%v, want %q", got, "renamed budget")
	}
	// policy_type is create-only: the update request schema has no such field,
	// so sending it would be a contract violation rather than a requirement.
	if got, present := body["policy_type"]; present {
		t.Errorf("update sent policy_type=%v; the type is fixed at create and the route does not accept it", got)
	}
	if got := stub.lastCreate(t, "budget")["policy_type"]; got != "budget" {
		t.Errorf("create sent policy_type=%v, want %q", got, "budget")
	}
	cfg, ok := body["config"].(map[string]any)
	if !ok {
		t.Fatalf("update sent config as %T, want an object", body["config"])
	}
	want := map[string]any{
		"include_byok_in_budgets": true,
		"limit_usd":               float64(50),
		"reset_interval":          "monthly",
	}
	for k, v := range want {
		got, present := cfg[k]
		if !present {
			t.Errorf("update dropped config.%s; the route replaces the whole config, so an absent field is lost", k)
			continue
		}
		if !reflect.DeepEqual(got, v) {
			t.Errorf("update sent config.%s=%v (%T), want %v (%T)", k, got, got, v, v)
		}
	}
}

// TestStubGuardrailPolicyBudgetEmptySubjectListsArriveAsEmptyArrays covers the
// clearing path. Removing every member from a policy is expressed by sending an
// empty list; if the SDK's omitzero tag swallowed it, Terraform would record an
// empty list while the API kept the old assignments.
func TestStubGuardrailPolicyBudgetEmptySubjectListsArriveAsEmptyArrays(t *testing.T) {
	t.Setenv("TF_ACC", "1")

	srv, stub := newGuardrailPolicyStub(t)

	config := func(name string) string {
		return guardrailPolicyStubConfig(srv.URL, "budget", "budget", fmt.Sprintf(`
  config = {
    include_byok_in_budgets = false
  }
  api_key_hashes = []
  member_ids     = []
  name           = %q
`, name))
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{Config: config("stub budget")},
			{
				Config: config("renamed budget"),
				Check:  resource.TestCheckResourceAttr("openrouter_guardrail_policy_budget.test", "member_ids.#", "0"),
			},
		},
	})

	for _, tc := range []struct {
		what string
		body map[string]any
	}{
		{"create", stub.lastCreate(t, "budget")},
		{"update", stub.lastUpdate(t, "budget")},
	} {
		for _, key := range []string{"member_ids", "api_key_hashes"} {
			got, present := tc.body[key]
			if !present {
				t.Errorf("%s omitted %s; an empty list has to reach the wire as [] to clear assignments", tc.what, key)
				continue
			}
			list, ok := got.([]any)
			if !ok {
				t.Errorf("%s sent %s as %T, want an array", tc.what, key, got)
				continue
			}
			if len(list) != 0 {
				t.Errorf("%s sent %s with %d entries, want 0: %v", tc.what, key, len(list), list)
			}
		}
	}
}

// TestStubGuardrailPolicyWorkspaceIDSentAsQueryParameter proves the selector
// travels where the API documents it: as a query parameter on read and delete,
// not in the path or the body.
func TestStubGuardrailPolicyWorkspaceIDSentAsQueryParameter(t *testing.T) {
	t.Setenv("TF_ACC", "1")

	srv, stub := newGuardrailPolicyStub(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: guardrailPolicyStubConfig(srv.URL, "budget", "budget", fmt.Sprintf(`
  config = {
    include_byok_in_budgets = true
  }
  name         = "stub policy"
  workspace_id = %q
`, stubWorkspaceID)),
				Check: resource.TestCheckResourceAttr("openrouter_guardrail_policy_budget.test", "workspace_id", stubWorkspaceID),
			},
		},
	})

	stub.mu.Lock()
	defer stub.mu.Unlock()
	if len(stub.getQueries) == 0 {
		t.Fatal("read never reached the stub")
	}
	for i, q := range stub.getQueries {
		if got := q.Get("workspace_id"); got != stubWorkspaceID {
			t.Errorf("read call %d sent workspace_id=%q, want %q", i, got, stubWorkspaceID)
		}
	}
	if len(stub.deleteQueries) == 0 {
		t.Fatal("destroy never issued a DELETE")
	}
	for i, q := range stub.deleteQueries {
		if got := q.Get("workspace_id"); got != stubWorkspaceID {
			t.Errorf("delete call %d sent workspace_id=%q, want %q", i, got, stubWorkspaceID)
		}
	}
}

// TestStubGuardrailPolicyVanishedLeavesState covers a policy deleted out from
// under Terraform: read must drop it from state so the next plan re-creates it,
// with no error, and the destroy that follows must accept a 404.
func TestStubGuardrailPolicyVanishedLeavesState(t *testing.T) {
	t.Setenv("TF_ACC", "1")

	srv, stub := newGuardrailPolicyStub(t)
	cfg := guardrailPolicyStubConfig(srv.URL, "budget", "budget", `
  config = {
    include_byok_in_budgets = true
  }
  name = "stub policy"
`)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				PreConfig: func() {
					stub.mu.Lock()
					stub.deleted["budget"] = true
					stub.mu.Unlock()
				},
				// No Config here on purpose: RefreshState works against the
				// config from the previous step, and a step cannot carry both.
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

// TestStubGuardrailPolicyImportByPolicyID covers import: the policy id alone
// has to hydrate state. workspace_id is ignored because no response model
// carries it, so import cannot recover it.
func TestStubGuardrailPolicyImportByPolicyID(t *testing.T) {
	t.Setenv("TF_ACC", "1")

	srv, _ := newGuardrailPolicyStub(t)
	cfg := guardrailPolicyStubConfig(srv.URL, "budget", "budget", fmt.Sprintf(`
  config = {
    include_byok_in_budgets = true
  }
  name         = "stub policy"
  workspace_id = %q
`, stubWorkspaceID))

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				ResourceName:            "openrouter_guardrail_policy_budget.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"workspace_id"},
			},
		},
	})
}

// TestStubGuardrailPolicyOtherFamiliesLifecycle is the smoke test for the two
// families that share the generated lifecycle code with budget: they have to
// create, read and destroy against their own routes.
func TestStubGuardrailPolicyOtherFamiliesLifecycle(t *testing.T) {
	t.Setenv("TF_ACC", "1")

	srv, stub := newGuardrailPolicyStub(t)

	config := stubProviderBlock(srv.URL) +
		stubPolicyResource("moderation", "moderation", `
  config = {}
  name   = "stub moderation policy"
`) +
		stubPolicyResource("model_availability", "model_availability", `
  config = {}
  name   = "stub model availability policy"
`)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("openrouter_guardrail_policy_moderation.test", "id"),
					resource.TestCheckResourceAttrSet("openrouter_guardrail_policy_model_availability.test", "id"),
					resource.TestCheckResourceAttr("openrouter_guardrail_policy_moderation.test", "policy_type", "moderation"),
					resource.TestCheckResourceAttr("openrouter_guardrail_policy_model_availability.test", "policy_type", "model_availability"),
				),
			},
		},
	})

	for _, tc := range []struct{ slug, policyType string }{
		{"moderation", "moderation"},
		// The route slug is hyphenated and the type value is underscored, which
		// is a trap for anyone writing this config by hand.
		{"model-availability", "model_availability"},
	} {
		if got := stub.lastCreate(t, tc.slug)["policy_type"]; got != tc.policyType {
			t.Errorf("%s create sent policy_type=%v, want %q", tc.slug, got, tc.policyType)
		}
	}
}
