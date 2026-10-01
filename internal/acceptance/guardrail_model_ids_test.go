package acceptance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// stubGuardrails mimics the guardrail endpoints' model id handling: the API
// accepts a slug or canonical slug in allowed_models/ignored_models and
// stores canonical slugs, in order, without duplicates, rejecting unknown ids
// (openrouter-web packages/guardrails/helpers/resolve-model-identifiers.ts).
// GET /models serves real entries from the public models list. It records
// the Authorization header of each request: the provider must list models
// anonymously but call the guardrail endpoints with its key.
type stubGuardrails struct {
	mu             sync.Mutex
	models         []map[string]any
	canonical      map[string]string
	guardrail      map[string]any
	modelsGets     int
	modelsAuth     map[string]bool
	guardrailsAuth map[string]bool
}

const stubGuardrailID = "6f0d6c9e-2a43-4b8f-9a55-3c2b7f1d0e11"

func newStubGuardrails(t *testing.T) (*httptest.Server, *stubGuardrails) {
	t.Helper()
	raw, err := os.ReadFile("testdata/guardrail_models.json")
	if err != nil {
		t.Fatal(err)
	}
	api := &stubGuardrails{canonical: map[string]string{}, modelsAuth: map[string]bool{}, guardrailsAuth: map[string]bool{}}
	if err := json.Unmarshal(raw, &api.models); err != nil {
		t.Fatal(err)
	}
	for _, m := range api.models {
		id, slug := m["id"].(string), m["canonical_slug"].(string)
		api.canonical[id] = slug
		api.canonical[slug] = slug
	}
	srv := httptest.NewServer(http.HandlerFunc(api.serveHTTP))
	t.Cleanup(srv.Close)
	return srv, api
}

func (s *stubGuardrails) serveHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	write := func(status int, body any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}
	if strings.HasPrefix(r.URL.Path, "/guardrails") {
		s.guardrailsAuth[r.Header.Get("Authorization")] = true
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/models":
		s.modelsGets++
		s.modelsAuth[r.Header.Get("Authorization")] = true
		write(http.StatusOK, map[string]any{"data": s.models, "total_count": len(s.models), "links": map[string]any{}})
	case r.Method == http.MethodPost && r.URL.Path == "/guardrails":
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if msg := s.resolve(body); msg != "" {
			write(http.StatusBadRequest, map[string]any{"error": map[string]any{"code": 400, "message": msg}})
			return
		}
		body["id"] = stubGuardrailID
		body["created_at"] = "2026-10-01T00:00:00Z"
		body["include_byok_in_budgets"] = false
		s.guardrail = body
		write(http.StatusCreated, map[string]any{"data": s.guardrail})
	case r.URL.Path == "/guardrails/"+stubGuardrailID && s.guardrail != nil:
		switch r.Method {
		case http.MethodGet:
			write(http.StatusOK, map[string]any{"data": s.guardrail})
		case http.MethodPatch:
			body := map[string]any{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if msg := s.resolve(body); msg != "" {
				write(http.StatusBadRequest, map[string]any{"error": map[string]any{"code": 400, "message": msg}})
				return
			}
			for k, v := range body {
				s.guardrail[k] = v
			}
			write(http.StatusOK, map[string]any{"data": s.guardrail})
		case http.MethodDelete:
			s.guardrail = nil
			write(http.StatusOK, map[string]any{"deleted": true})
		}
	default:
		write(http.StatusNotFound, map[string]any{"error": map[string]any{"code": 404, "message": "not found"}})
	}
}

func (s *stubGuardrails) resolve(body map[string]any) string {
	for _, field := range []string{"allowed_models", "ignored_models"} {
		ids, ok := body[field].([]any)
		if !ok {
			continue
		}
		var out []any
		seen := map[string]bool{}
		for _, id := range ids {
			c, ok := s.canonical[id.(string)]
			if !ok {
				return fmt.Sprintf("Invalid %s: %s", field, id)
			}
			if !seen[c] {
				seen[c] = true
				out = append(out, c)
			}
		}
		body[field] = out
	}
	return ""
}

func (s *stubGuardrails) setAllowedModels(ids ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]any, len(ids))
	for i, id := range ids {
		out[i] = id
	}
	s.guardrail["allowed_models"] = out
}

func (s *stubGuardrails) checkStored(field string, want ...string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		got := fmt.Sprint(s.guardrail[field])
		if exp := fmt.Sprint(toAny(want)); got != exp {
			return fmt.Errorf("server %s = %s, want %s", field, got, exp)
		}
		return nil
	}
}

func toAny(v []string) []any {
	out := make([]any, len(v))
	for i, s := range v {
		out[i] = s
	}
	return out
}

func stubGuardrailConfig(serverURL string, allowed, ignored []string) string {
	list := func(v []string) string {
		return `["` + strings.Join(v, `", "`) + `"]`
	}
	return stubBudgetProviderConfig(serverURL) + fmt.Sprintf(`
resource "openrouter_guardrail" "test" {
  name           = "model-ids"
  allowed_models = %s
  ignored_models = %s
}
`, list(allowed), list(ignored))
}

// #347: a config naming models by slug (not canonical slug) must converge.
// Each Config step also fails if the plan after apply is not empty.
func TestStubGuardrailModelIDsConverge(t *testing.T) {
	srv, api := newStubGuardrails(t)
	allowed := []string{"deepseek/deepseek-v4-flash-0731", "anthropic/claude-sonnet-5.5-20260928"}
	ignored := []string{"openai/gpt-6-luna"}
	config := stubGuardrailConfig(srv.URL, allowed, ignored)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("openrouter_guardrail.test", "allowed_models.0", "deepseek/deepseek-v4-flash-0731"),
					resource.TestCheckResourceAttr("openrouter_guardrail.test", "allowed_models.1", "anthropic/claude-sonnet-5.5-20260928"),
					resource.TestCheckResourceAttr("openrouter_guardrail.test", "ignored_models.0", "openai/gpt-6-luna"),
					api.checkStored("allowed_models", "deepseek/deepseek-v4-flash-20260731", "anthropic/claude-sonnet-5.5-20260928"),
					api.checkStored("ignored_models", "openai/gpt-6-luna-20260922"),
				),
			},
			{Config: config, PlanOnly: true},
			// Two ids for one model: the API keeps one.
			{
				Config: stubGuardrailConfig(srv.URL, []string{"anthropic/claude-sonnet-5.5", "anthropic/claude-sonnet-5.5:batch"}, ignored),
				Check:  api.checkStored("allowed_models", "anthropic/claude-sonnet-5.5-20260928"),
			},
			// An out-of-band change still shows as drift and is reverted.
			{
				PreConfig: func() { api.setAllowedModels("deepseek/deepseek-v4-pro-20260813") },
				Config:    stubGuardrailConfig(srv.URL, []string{"anthropic/claude-sonnet-5.5", "anthropic/claude-sonnet-5.5:batch"}, ignored),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("openrouter_guardrail.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: api.checkStored("allowed_models", "anthropic/claude-sonnet-5.5-20260928"),
			},
			// Import has no prior list, so it stores the API's canonical slugs.
			{
				ResourceName:  "openrouter_guardrail.test",
				ImportState:   true,
				ImportStateId: stubGuardrailID,
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if got := states[0].Attributes["allowed_models.0"]; got != "anthropic/claude-sonnet-5.5-20260928" {
						return fmt.Errorf("imported allowed_models.0 = %q, want the canonical slug", got)
					}
					return nil
				},
			},
		},
	})

	api.mu.Lock()
	defer api.mu.Unlock()
	if api.modelsGets == 0 {
		t.Fatal("the provider never listed models")
	}
	if fmt.Sprint(api.modelsAuth) != "map[:true]" {
		t.Errorf("GET /models Authorization headers = %v, want only empty", api.modelsAuth)
	}
	if fmt.Sprint(api.guardrailsAuth) != "map[Bearer local-test-only:true]" {
		t.Errorf("guardrail Authorization headers = %v, want only the provider key", api.guardrailsAuth)
	}
}
