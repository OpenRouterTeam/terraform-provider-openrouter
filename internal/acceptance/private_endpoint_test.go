package acceptance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const privateEndpointWorkspaceID = "550e8400-e29b-41d4-a716-446655440000"

type stubPricing struct {
	Prompt     string `json:"prompt"`
	Completion string `json:"completion"`
}

type stubPrivateEndpoint struct {
	ID              string      `json:"id"`
	Status          string      `json:"status"`
	ModelPermaslug  string      `json:"model_permaslug"`
	ModelSlug       string      `json:"model_slug"`
	ModelName       string      `json:"model_name"`
	ProviderName    string      `json:"provider_name"`
	ProviderSlug    string      `json:"provider_slug"`
	BaseURL         *string     `json:"base_url"`
	UpstreamModelID string      `json:"upstream_model_id"`
	DeclaredZDR     *bool       `json:"declared_zdr"`
	DeclaredRegion  *string     `json:"declared_region"`
	Pricing         stubPricing `json:"pricing"`
	CreatedAt       string      `json:"created_at"`
}

// Mirrors the /private-endpoints contract: create returns the summary shape
// (no pricing or upstream fields), reads return the full endpoint, and only
// pricing is editable once the endpoint left draft.
type stubPrivateEndpoints struct {
	mu        sync.Mutex
	nextID    int
	endpoints map[string]*stubPrivateEndpoint
	creates   []map[string]any
}

func newStubPrivateEndpoints(t *testing.T) (*httptest.Server, *stubPrivateEndpoints) {
	t.Helper()
	api := &stubPrivateEndpoints{endpoints: make(map[string]*stubPrivateEndpoint)}
	srv := httptest.NewServer(http.HandlerFunc(api.serveHTTP))
	t.Cleanup(srv.Close)
	return srv, api
}

func (s *stubPrivateEndpoints) serveHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	writeError := func(status int, message string) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"code": status, "message": message},
		})
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if parts[0] != "private-endpoints" || len(parts) > 3 {
		writeError(http.StatusNotFound, "Not found")
		return
	}
	if len(parts) == 1 {
		if r.Method != http.MethodPost {
			writeError(http.StatusMethodNotAllowed, "Method not allowed")
			return
		}
		s.create(w, r, writeError)
		return
	}
	endpoint, ok := s.endpoints[parts[1]]
	if !ok {
		writeError(http.StatusNotFound, "Private endpoint not found")
		return
	}
	switch {
	case len(parts) == 3 && parts[2] == "pricing" && r.Method == http.MethodPut:
		var body struct {
			Pricing stubPricing `json:"pricing"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Pricing.Prompt == "" {
			writeError(http.StatusBadRequest, "Invalid pricing")
			return
		}
		endpoint.Pricing = body.Pricing
	case len(parts) == 2 && r.Method == http.MethodGet:
	case len(parts) == 2 && r.Method == http.MethodDelete:
		delete(s.endpoints, endpoint.ID)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"id": endpoint.ID}})
		return
	default:
		writeError(http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"data": endpoint})
}

func (s *stubPrivateEndpoints) create(w http.ResponseWriter, r *http.Request, writeError func(int, string)) {
	var raw map[string]any
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeError(http.StatusBadRequest, "Invalid body")
		return
	}
	s.creates = append(s.creates, raw)
	encoded, _ := json.Marshal(raw)
	var body struct {
		ModelPermaslug  string       `json:"model_permaslug"`
		ProviderSlug    string       `json:"provider_slug"`
		UpstreamModelID string       `json:"upstream_model_id"`
		BaseURL         *string      `json:"base_url"`
		DeclaredZDR     *bool        `json:"declared_zdr"`
		DeclaredRegion  *string      `json:"declared_region"`
		Pricing         *stubPricing `json:"pricing"`
		Activate        *struct {
			WorkspaceID string `json:"workspace_id"`
		} `json:"activate"`
	}
	_ = json.Unmarshal(encoded, &body)
	s.nextID++
	endpoint := &stubPrivateEndpoint{
		ID:              fmt.Sprintf("00000000-0000-4000-8000-%012d", s.nextID),
		Status:          "draft",
		ModelPermaslug:  body.ModelPermaslug,
		ModelSlug:       body.ModelPermaslug,
		ModelName:       "GPT-4o",
		ProviderName:    "OpenAI",
		ProviderSlug:    body.ProviderSlug,
		BaseURL:         body.BaseURL,
		UpstreamModelID: body.UpstreamModelID,
		DeclaredZDR:     body.DeclaredZDR,
		DeclaredRegion:  body.DeclaredRegion,
		Pricing:         stubPricing{Prompt: "0.0000025", Completion: "0.00001"},
		CreatedAt:       "2026-09-26T12:00:00Z",
	}
	if body.Pricing != nil {
		endpoint.Pricing = *body.Pricing
	}
	if body.Activate != nil {
		if body.Activate.WorkspaceID != privateEndpointWorkspaceID {
			writeError(http.StatusNotFound, "Workspace not found")
			return
		}
		endpoint.Status = "active"
	}
	s.endpoints[endpoint.ID] = endpoint
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
		"id": endpoint.ID, "status": endpoint.Status, "model_permaslug": endpoint.ModelPermaslug,
		"model_slug": endpoint.ModelSlug, "model_name": endpoint.ModelName,
		"provider_name": endpoint.ProviderName, "created_at": endpoint.CreatedAt,
		"declared_zdr": endpoint.DeclaredZDR, "declared_region": endpoint.DeclaredRegion,
	}})
}

func (s *stubPrivateEndpoints) checkCreateCount(want int) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if len(s.creates) != want {
			return fmt.Errorf("server saw %d creates, want %d", len(s.creates), want)
		}
		return nil
	}
}

func (s *stubPrivateEndpoints) checkLastCreateActivated() resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		activate, ok := s.creates[len(s.creates)-1]["activate"].(map[string]any)
		if !ok || activate["workspace_id"] != privateEndpointWorkspaceID {
			return fmt.Errorf("create body activate = %v, want workspace %s", s.creates[len(s.creates)-1]["activate"], privateEndpointWorkspaceID)
		}
		return nil
	}
}

func (s *stubPrivateEndpoints) setPricing(prompt string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, endpoint := range s.endpoints {
		endpoint.Pricing.Prompt = prompt
	}
}

func privateEndpointConfig(serverURL, upstreamModelID, prompt string) string {
	return stubBudgetProviderConfig(serverURL) + fmt.Sprintf(`
resource "openrouter_private_endpoint" "test" {
  model_permaslug   = "openai/gpt-4o"
  provider_slug     = "openai"
  upstream_model_id = %q
  declared_zdr      = true
  declared_region   = "us"
  pricing = {
    prompt     = %q
    completion = "0.000008"
  }
  activate = {
    workspace_id = %q
  }
}
`, upstreamModelID, prompt, privateEndpointWorkspaceID)
}

// UnitTest intentionally bypasses TF_ACC: every request goes to a local fixture.
func TestStubPrivateEndpointLifecycle(t *testing.T) {
	srv, api := newStubPrivateEndpoints(t)
	const name = "openrouter_private_endpoint.test"
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: privateEndpointConfig(srv.URL, "gpt-4o-2024-08-06", "0.000002"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "status", "active"),
					resource.TestCheckResourceAttr(name, "pricing.prompt", "0.000002"),
					resource.TestCheckResourceAttr(name, "declared_region", "us"),
					resource.TestCheckResourceAttrSet(name, "id"),
					api.checkCreateCount(1),
					api.checkLastCreateActivated(),
				),
			},
			{Config: privateEndpointConfig(srv.URL, "gpt-4o-2024-08-06", "0.000002"), PlanOnly: true},
			{
				Config: privateEndpointConfig(srv.URL, "gpt-4o-2024-08-06", "0.0000015"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(name, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "pricing.prompt", "0.0000015"),
					api.checkCreateCount(1),
				),
			},
			{
				PreConfig: func() { api.setPricing("0.000003") },
				Config:    privateEndpointConfig(srv.URL, "gpt-4o-2024-08-06", "0.0000015"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(name, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.TestCheckResourceAttr(name, "pricing.prompt", "0.0000015"),
			},
			{
				Config: privateEndpointConfig(srv.URL, "gpt-4o-2024-11-20", "0.0000015"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(name, plancheck.ResourceActionDestroyBeforeCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "upstream_model_id", "gpt-4o-2024-11-20"),
					api.checkCreateCount(2),
				),
			},
			{
				ResourceName:            name,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"activate", "created_at"},
			},
		},
	})
}
