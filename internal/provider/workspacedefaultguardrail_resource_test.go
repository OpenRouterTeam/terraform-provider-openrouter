package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const (
	fakeWorkspaceID = "11111111-1111-1111-1111-111111111111"
	fakeGuardrailID = "22222222-2222-2222-2222-222222222222"
)

// fakeAPI models the platform's lazy materialization of the workspace
// default guardrail: the workspace always reports default_guardrail_id, but
// GET /guardrails/{id} is 404 until the first PATCH.
type fakeAPI struct {
	mu               sync.Mutex
	workspaceDeleted bool
	materialized     bool
	guardrail        map[string]any
	patches          int
	posts            int
}

func (f *fakeAPI) handler(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /workspaces/"+fakeWorkspaceID, func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		deleted := f.workspaceDeleted
		f.mu.Unlock()
		if deleted {
			writeJSON(w, 404, map[string]any{"error": map[string]any{"code": 404, "message": "not found"}})
			return
		}
		writeJSON(w, 200, map[string]any{"data": map[string]any{
			"id":                                  fakeWorkspaceID,
			"name":                                "acme",
			"slug":                                "acme",
			"created_at":                          "2026-01-01T00:00:00Z",
			"default_guardrail_id":                fakeGuardrailID,
			"io_logging_api_key_ids":              []int64{},
			"io_logging_sampling_rate":            0,
			"is_data_discount_logging_enabled":    false,
			"is_observability_broadcast_enabled":  false,
			"is_observability_io_logging_enabled": false,
		}})
	})
	mux.HandleFunc("GET /workspaces/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 404, map[string]any{"error": map[string]any{"code": 404, "message": "not found"}})
	})
	mux.HandleFunc("GET /guardrails/"+fakeGuardrailID, func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if !f.materialized {
			writeJSON(w, 404, map[string]any{"error": map[string]any{"code": 404, "message": "Guardrail not found"}})
			return
		}
		writeJSON(w, 200, map[string]any{"data": f.guardrail})
	})
	mux.HandleFunc("PATCH /guardrails/"+fakeGuardrailID, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode PATCH body: %v", err)
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.patches++
		if !f.materialized {
			f.materialized = true
			f.guardrail = map[string]any{
				"id":                      fakeGuardrailID,
				"name":                    fmt.Sprintf("Workspace %s Default", fakeWorkspaceID),
				"workspace_id":            fakeWorkspaceID,
				"created_at":              "2026-01-01T00:00:00Z",
				"updated_at":              "2026-01-01T00:00:00Z",
				"include_byok_in_budgets": false,
			}
		}
		for k, v := range body {
			f.guardrail[k] = v
		}
		writeJSON(w, 200, map[string]any{"data": f.guardrail})
	})
	mux.HandleFunc("POST /guardrails", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.posts++
		f.mu.Unlock()
		t.Errorf("unexpected POST /guardrails: the default guardrail must never be created")
		writeJSON(w, 500, map[string]any{})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		writeJSON(w, 500, map[string]any{})
	})
	return mux
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func testProviderFactories() map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		"openrouter": providerserver.NewProtocol6WithError(New("test")()),
	}
}

func defaultGuardrailConfig(serverURL string, limit float64) string {
	return fmt.Sprintf(`
provider "openrouter" {
  api_key    = "sk-or-mgmt-test"
  server_url = %q
}

resource "openrouter_workspace_default_guardrail" "this" {
  workspace_id  = %q
  limit_usd     = %v
  enforce_zdr   = true
  allowed_models = ["openai/gpt-4o"]
}
`, serverURL, fakeWorkspaceID, limit)
}

// Fresh workspace, default never touched: apply PATCHes it into existence and
// a subsequent plan is clean.
func TestWorkspaceDefaultGuardrail_CreateOnUnmaterializedDefault(t *testing.T) {
	api := &fakeAPI{}
	srv := httptest.NewServer(api.handler(t))
	defer srv.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: defaultGuardrailConfig(srv.URL, 100),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("openrouter_workspace_default_guardrail.this", "id", fakeGuardrailID),
					resource.TestCheckResourceAttr("openrouter_workspace_default_guardrail.this", "workspace_id", fakeWorkspaceID),
					resource.TestCheckResourceAttr("openrouter_workspace_default_guardrail.this", "name", "Workspace "+fakeWorkspaceID+" Default"),
					resource.TestCheckResourceAttr("openrouter_workspace_default_guardrail.this", "limit_usd", "100"),
				),
			},
			{
				Config:   defaultGuardrailConfig(srv.URL, 100),
				PlanOnly: true,
			},
			{
				Config: defaultGuardrailConfig(srv.URL, 250),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("openrouter_workspace_default_guardrail.this", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.TestCheckResourceAttr("openrouter_workspace_default_guardrail.this", "limit_usd", "250"),
			},
		},
	})

	if api.posts != 0 {
		t.Fatalf("POST /guardrails called %d times", api.posts)
	}
	if api.patches != 2 {
		t.Fatalf("expected 2 PATCH calls (create, update), got %d", api.patches)
	}
}

// Import by workspace id while GET /guardrails/{id} still returns 404: the
// resource imports as present-and-unconfigured, and the following apply is an
// in-place update, never a replacement or a create.
func TestWorkspaceDefaultGuardrail_ImportUnmaterializedDefault(t *testing.T) {
	api := &fakeAPI{}
	srv := httptest.NewServer(api.handler(t))
	defer srv.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config:             defaultGuardrailConfig(srv.URL, 100),
				ResourceName:       "openrouter_workspace_default_guardrail.this",
				ImportState:        true,
				ImportStateId:      fakeWorkspaceID,
				ImportStatePersist: true,
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if len(states) != 1 {
						return fmt.Errorf("expected 1 imported instance, got %d", len(states))
					}
					if got := states[0].Attributes["id"]; got != fakeGuardrailID {
						return fmt.Errorf("imported id = %q, want %q", got, fakeGuardrailID)
					}
					if got := states[0].Attributes["limit_usd"]; got != "" {
						return fmt.Errorf("imported limit_usd = %q, want unset", got)
					}
					return nil
				},
			},
			{
				Config: defaultGuardrailConfig(srv.URL, 100),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("openrouter_workspace_default_guardrail.this", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("openrouter_workspace_default_guardrail.this", "id", fakeGuardrailID),
					resource.TestCheckResourceAttr("openrouter_workspace_default_guardrail.this", "limit_usd", "100"),
				),
			},
		},
	})

	if api.posts != 0 {
		t.Fatalf("POST /guardrails called %d times", api.posts)
	}
}

// Workspace gone: the workspace GET is the existence oracle, so Read drops
// the resource from state, the plan proposes creating it again, and Create
// then reports the missing workspace.
func TestWorkspaceDefaultGuardrail_WorkspaceDeletedRemovesFromState(t *testing.T) {
	api := &fakeAPI{}
	srv := httptest.NewServer(api.handler(t))
	defer srv.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: defaultGuardrailConfig(srv.URL, 100),
			},
			{
				PreConfig: func() {
					api.mu.Lock()
					defer api.mu.Unlock()
					api.workspaceDeleted = true
				},
				Config:      defaultGuardrailConfig(srv.URL, 100),
				ExpectError: regexp.MustCompile(`workspace not found`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("openrouter_workspace_default_guardrail.this", plancheck.ResourceActionCreate),
					},
				},
			},
		},
	})
}

// Materialized guardrail later reads as 404 while the workspace still exists:
// Read keeps the resource, with its configuration cleared, so the plan is an
// in-place update that re-applies the configuration.
func TestWorkspaceDefaultGuardrail_DematerializedReadsAsUnconfigured(t *testing.T) {
	api := &fakeAPI{}
	srv := httptest.NewServer(api.handler(t))
	defer srv.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: defaultGuardrailConfig(srv.URL, 100),
			},
			{
				PreConfig: func() {
					api.mu.Lock()
					defer api.mu.Unlock()
					api.materialized = false
				},
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("openrouter_workspace_default_guardrail.this", "id", fakeGuardrailID),
					resource.TestCheckResourceAttr("openrouter_workspace_default_guardrail.this", "workspace_id", fakeWorkspaceID),
					resource.TestCheckNoResourceAttr("openrouter_workspace_default_guardrail.this", "limit_usd"),
					resource.TestCheckNoResourceAttr("openrouter_workspace_default_guardrail.this", "name"),
				),
			},
			{
				Config: defaultGuardrailConfig(srv.URL, 100),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("openrouter_workspace_default_guardrail.this", plancheck.ResourceActionUpdate),
					},
				},
			},
		},
	})
}
