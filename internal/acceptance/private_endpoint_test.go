package acceptance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

const (
	privateEndpointWorkspaceID       = "550e8400-e29b-41d4-a716-446655440000"
	privateEndpointNoByokWorkspaceID = "550e8400-e29b-41d4-a716-4466554400ff"
)

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
	ProviderSlug    *string     `json:"provider_slug"`
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
	mu          sync.Mutex
	nextID      int
	endpoints   map[string]*stubPrivateEndpoint
	creates     []map[string]any
	createKeys  []string
	byKey       map[string]string
	deletes     []string
	pricingPuts int
	failGets    int
	// timeoutCreates answers that many creates with 408 after committing
	// them, the way the API's timeout middleware leaves the handler running.
	timeoutCreates int
	// retainedDraftStatus answers the next activating create with this status
	// and the kept draft in the body, as a failed activation step does.
	retainedDraftStatus int
}

func newStubPrivateEndpoints(t *testing.T) (*httptest.Server, *stubPrivateEndpoints) {
	t.Helper()
	api := &stubPrivateEndpoints{endpoints: make(map[string]*stubPrivateEndpoint), byKey: make(map[string]string)}
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
		s.pricingPuts++
		endpoint.Pricing = body.Pricing
	case len(parts) == 2 && r.Method == http.MethodGet:
		if s.failGets > 0 {
			s.failGets--
			writeError(http.StatusForbidden, "Forbidden")
			return
		}
	case len(parts) == 2 && r.Method == http.MethodDelete:
		if r.URL.Query().Get("draft_only") == "true" && endpoint.Status != "draft" {
			writeError(http.StatusConflict, "not_draft")
			return
		}
		s.deletes = append(s.deletes, r.URL.RawQuery)
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
	key := r.Header.Get("Idempotency-Key")
	s.createKeys = append(s.createKeys, key)
	if id, ok := s.byKey[key]; ok && key != "" {
		if endpoint, ok := s.endpoints[id]; ok {
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": stubSummary(endpoint)})
			return
		}
	}
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
		ProviderSlug:    &body.ProviderSlug,
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
	if body.BaseURL != nil {
		normalized := stubNormalizeBaseURL(*body.BaseURL)
		endpoint.BaseURL = &normalized
	}
	if key != "" {
		s.byKey[key] = endpoint.ID
	}
	if body.Activate != nil && s.retainedDraftStatus != 0 {
		status := s.retainedDraftStatus
		s.retainedDraftStatus = 0
		s.endpoints[endpoint.ID] = endpoint
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"code": status, "message": "invalid_base_url"},
			"data":  map[string]any{"endpoint": stubSummary(endpoint), "validation": nil},
		})
		return
	}
	if body.Activate != nil && body.Activate.WorkspaceID == privateEndpointNoByokWorkspaceID {
		s.endpoints[endpoint.ID] = endpoint
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"code": 422, "message": "Validation failed; the draft was kept so it can be fixed and activated"},
			"data": map[string]any{
				"endpoint":   stubSummary(endpoint),
				"validation": map[string]any{"passed": false, "checks": []map[string]any{{"name": "auth_ok", "passed": false, "reason": "no_byok_key"}}},
			},
		})
		return
	}
	if body.Activate != nil {
		if body.Activate.WorkspaceID != privateEndpointWorkspaceID {
			writeError(http.StatusNotFound, "Workspace not found")
			return
		}
		endpoint.Status = "active"
	}
	s.endpoints[endpoint.ID] = endpoint
	if s.timeoutCreates > 0 {
		s.timeoutCreates--
		writeError(http.StatusRequestTimeout, "Operation timed out. Please try again later.")
		return
	}
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{"data": stubSummary(endpoint)})
}

// stubNormalizeBaseURL applies the API's normalizeBaseUrl: URL serialization
// lowercases the scheme and host, then one trailing "/" and a trailing
// "/chat/completions" are stripped.
func stubNormalizeBaseURL(raw string) string {
	scheme, rest, _ := strings.Cut(raw, "://")
	host, path, hasPath := strings.Cut(rest, "/")
	normalized := strings.ToLower(scheme) + "://" + strings.ToLower(host)
	if hasPath {
		normalized += "/" + path
	}
	normalized = strings.TrimSuffix(normalized, "/")
	return strings.TrimSuffix(normalized, "/chat/completions")
}

func stubSummary(endpoint *stubPrivateEndpoint) map[string]any {
	return map[string]any{
		"id": endpoint.ID, "status": endpoint.Status, "model_permaslug": endpoint.ModelPermaslug,
		"model_slug": endpoint.ModelSlug, "model_name": endpoint.ModelName,
		"provider_name": endpoint.ProviderName, "created_at": endpoint.CreatedAt,
		"declared_zdr": endpoint.DeclaredZDR, "declared_region": endpoint.DeclaredRegion,
	}
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

func (s *stubPrivateEndpoints) checkEndpointCount(want int) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if len(s.endpoints) != want {
			return fmt.Errorf("server holds %d endpoints, want %d", len(s.endpoints), want)
		}
		return nil
	}
}

func (s *stubPrivateEndpoints) checkPricingPuts(want int) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.pricingPuts != want {
			return fmt.Errorf("server saw %d PUT /pricing calls, want %d", s.pricingPuts, want)
		}
		return nil
	}
}

// checkDeletesWithoutDraftOnly verifies a destroy never sends draft_only: it
// only belongs to the retained-draft cleanup, not to the resource.
func (s *stubPrivateEndpoints) checkDeletesWithoutDraftOnly(_ *terraform.State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.deletes) == 0 {
		return fmt.Errorf("server saw no deletes")
	}
	for _, query := range s.deletes {
		if query != "" {
			return fmt.Errorf("destroy sent query %q, want none", query)
		}
	}
	return nil
}

func (s *stubPrivateEndpoints) unlistProviders() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, endpoint := range s.endpoints {
		endpoint.ProviderSlug = nil
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
		CheckDestroy:             api.checkDeletesWithoutDraftOnly,
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
						// Server-assigned attributes keep their state value in the plan.
						plancheck.ExpectKnownValue(name, tfjsonpath.New("id"), knownvalue.NotNull()),
						plancheck.ExpectKnownValue(name, tfjsonpath.New("status"), knownvalue.StringExact("active")),
						plancheck.ExpectKnownValue(name, tfjsonpath.New("provider_name"), knownvalue.StringExact("OpenAI")),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "pricing.prompt", "0.0000015"),
					api.checkCreateCount(1),
					api.checkPricingPuts(1),
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
				ImportStateVerifyIgnore: []string{"activate"},
			},
		},
	})
}

func TestStubPrivateEndpointFailedActivationDeletesRetainedDraft(t *testing.T) {
	srv, api := newStubPrivateEndpoints(t)
	config := strings.Replace(privateEndpointConfig(srv.URL, "gpt-4o-prod", "0.000002"), privateEndpointWorkspaceID, privateEndpointNoByokWorkspaceID, 1)
	failedApply := resource.TestStep{Config: config, ExpectError: regexp.MustCompile(`Got an unexpected response code 422`)}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps:                    []resource.TestStep{failedApply, failedApply},
	})
	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.creates) != 2 || len(api.endpoints) != 0 {
		t.Fatalf("creates=%d remaining endpoints=%d, want 2 creates and no stranded drafts", len(api.creates), len(api.endpoints))
	}
	for _, query := range api.deletes {
		if query != "draft_only=true" {
			t.Fatalf("cleanup delete query = %q, want draft_only=true", query)
		}
	}
}

func TestStubPrivateEndpointRejectsPartialPricing(t *testing.T) {
	srv, api := newStubPrivateEndpoints(t)
	config := strings.Replace(privateEndpointConfig(srv.URL, "gpt-4o-prod", "0.000002"), `completion = "0.000008"`, "", 1)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{Config: config, ExpectError: regexp.MustCompile(`pricing.completion: value must be configured`)},
		},
	})
	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.creates) != 0 {
		t.Fatalf("server saw %d creates for an invalid pricing block, want 0", len(api.creates))
	}
}

func TestStubPrivateEndpointFailedReadAfterCreateKeepsEndpointInState(t *testing.T) {
	srv, api := newStubPrivateEndpoints(t)
	const name = "openrouter_private_endpoint.test"
	config := privateEndpointConfig(srv.URL, "gpt-4o-prod", "0.000002")
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				PreConfig:   func() { api.failGets = 1 },
				Config:      config,
				ExpectError: regexp.MustCompile(`Got an unexpected response code 403`),
			},
			{
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(name, plancheck.ResourceActionDestroyBeforeCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "status", "active"),
					api.checkCreateCount(2),
					api.checkEndpointCount(1),
				),
			},
		},
	})
}

func TestStubPrivateEndpointUnlistedProviderKeepsConfiguredSlug(t *testing.T) {
	srv, api := newStubPrivateEndpoints(t)
	const name = "openrouter_private_endpoint.test"
	config := privateEndpointConfig(srv.URL, "gpt-4o-prod", "0.000002")
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig: api.unlistProviders,
				Config:    config,
				PlanOnly:  true,
			},
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "provider_slug", "openai"),
					api.checkCreateCount(1),
				),
			},
		},
	})
}

func privateEndpointBaseURLConfig(serverURL, baseURL, workspaceID string) string {
	return stubBudgetProviderConfig(serverURL) + fmt.Sprintf(`
resource "openrouter_private_endpoint" "test" {
  model_permaslug   = "openai/gpt-4o"
  provider_slug     = "openai"
  upstream_model_id = "gpt-4o-2024-08-06"
  base_url          = %q
  pricing = {
    prompt     = "0.000002"
    completion = "0.000008"
  }
  activate = {
    workspace_id = %q
  }
}
`, baseURL, workspaceID)
}

// The API stores base_url normalized. A config spelled differently must
// converge instead of replacing the endpoint on every apply.
func TestStubPrivateEndpointBaseURLNormalizationConverges(t *testing.T) {
	srv, api := newStubPrivateEndpoints(t)
	const name = "openrouter_private_endpoint.test"
	const raw = "HTTPS://Contoso.OpenAI.Azure.com/openai/v1/chat/completions/"
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: privateEndpointBaseURLConfig(srv.URL, raw, privateEndpointWorkspaceID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "base_url", raw),
					api.checkCreateCount(1),
				),
			},
			{Config: privateEndpointBaseURLConfig(srv.URL, raw, privateEndpointWorkspaceID), PlanOnly: true},
			{
				Config: privateEndpointBaseURLConfig(srv.URL, raw, privateEndpointWorkspaceID),
				Check:  api.checkCreateCount(1),
			},
			// Another spelling of the same URL is recorded in place, without a
			// replacement or a pricing write.
			{
				Config: privateEndpointBaseURLConfig(srv.URL, "https://contoso.openai.azure.com/openai/v1/", privateEndpointWorkspaceID),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(name, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "base_url", "https://contoso.openai.azure.com/openai/v1/"),
					api.checkCreateCount(1),
					api.checkPricingPuts(0),
				),
			},
			{
				Config: privateEndpointBaseURLConfig(srv.URL, "https://contoso.openai.azure.com/openai/v2", privateEndpointWorkspaceID),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(name, plancheck.ResourceActionDestroyBeforeCreate),
					},
				},
				Check: api.checkCreateCount(2),
			},
		},
	})
}

// privateEndpointForgetConfig drops the endpoint from state without deleting
// it, so a later step can import it.
const privateEndpointForgetConfig = `
removed {
  from = openrouter_private_endpoint.test
  lifecycle {
    destroy = false
  }
}
`

// activate is create-time input and base_url is read back normalized, so an
// imported endpoint whose config sets both must not be replaced.
func TestStubPrivateEndpointImportKeepsEndpoint(t *testing.T) {
	srv, api := newStubPrivateEndpoints(t)
	const name = "openrouter_private_endpoint.test"
	config := privateEndpointBaseURLConfig(srv.URL, "https://contoso.openai.azure.com/", privateEndpointWorkspaceID)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{Config: config},
			{Config: stubBudgetProviderConfig(srv.URL) + privateEndpointForgetConfig},
			{
				Config:             config,
				ResourceName:       name,
				ImportState:        true,
				ImportStateId:      "00000000-0000-4000-8000-000000000001",
				ImportStatePersist: true,
			},
			{
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(name, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "activate.workspace_id", privateEndpointWorkspaceID),
					resource.TestCheckResourceAttr(name, "base_url", "https://contoso.openai.azure.com/"),
					api.checkCreateCount(1),
					api.checkPricingPuts(0),
				),
			},
			{Config: config, PlanOnly: true},
		},
	})
}

// Changing or removing activate after create changes nothing on the server:
// no replacement and no new pricing version.
func TestStubPrivateEndpointActivateIsCreateOnly(t *testing.T) {
	srv, api := newStubPrivateEndpoints(t)
	const name = "openrouter_private_endpoint.test"
	config := privateEndpointConfig(srv.URL, "gpt-4o-prod", "0.000002")
	otherWorkspace := strings.Replace(config, privateEndpointWorkspaceID, "550e8400-e29b-41d4-a716-4466554400aa", 1)
	withoutActivate := strings.Replace(config, fmt.Sprintf("activate = {\n    workspace_id = %q\n  }", privateEndpointWorkspaceID), "", 1)
	if withoutActivate == config {
		t.Fatal("config has no activate block to remove")
	}
	inPlace := resource.ConfigPlanChecks{
		PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(name, plancheck.ResourceActionUpdate)},
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{Config: config},
			{Config: otherWorkspace, ConfigPlanChecks: inPlace},
			{Config: withoutActivate, ConfigPlanChecks: inPlace},
			{
				Config: withoutActivate,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr(name, "activate"),
					resource.TestCheckResourceAttr(name, "status", "active"),
					api.checkCreateCount(1),
					api.checkPricingPuts(0),
				),
			},
		},
	})
}

// A create the API commits and then answers with 408 is retried with the same
// Idempotency-Key, so the retry returns that endpoint instead of a second one.
func TestStubPrivateEndpointCreateTimeoutRetriesIdempotently(t *testing.T) {
	srv, api := newStubPrivateEndpoints(t)
	const name = "openrouter_private_endpoint.test"
	api.timeoutCreates = 1
	config := privateEndpointConfig(srv.URL, "gpt-4o-prod", "0.000002")
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "status", "active"),
					api.checkCreateCount(2),
					api.checkEndpointCount(1),
				),
			},
			{Config: config, PlanOnly: true},
		},
	})
	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.createKeys) != 2 || api.createKeys[0] == "" || api.createKeys[0] != api.createKeys[1] {
		t.Fatalf("create Idempotency-Keys = %q, want one non-empty key reused by the retry", api.createKeys)
	}
}

// Each resource create sends its own key: a key derived from config would
// replay an endpoint an earlier create made (the server remembers keys of
// deleted endpoints too).
func TestStubPrivateEndpointCreatesUseFreshIdempotencyKeys(t *testing.T) {
	srv, api := newStubPrivateEndpoints(t)
	config := privateEndpointConfig(srv.URL, "gpt-4o-prod", "0.000002")
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{Config: config},
			{Config: config, Destroy: true},
			{Config: config, Check: api.checkCreateCount(2)},
		},
	})
	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.createKeys) != 2 || api.createKeys[0] == api.createKeys[1] {
		t.Fatalf("create Idempotency-Keys = %q, want a different key per create", api.createKeys)
	}
}

// A failed activation step can return the kept draft with a status outside
// the validation-failure set, such as 400 invalid_base_url. It is still
// cleaned up.
func TestStubPrivateEndpointRetainedDraftWithAnyStatusIsDeleted(t *testing.T) {
	srv, api := newStubPrivateEndpoints(t)
	api.retainedDraftStatus = http.StatusBadRequest
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config:      privateEndpointConfig(srv.URL, "gpt-4o-prod", "0.000002"),
				ExpectError: regexp.MustCompile(`Got an unexpected response code 400`),
			},
		},
	})
	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.endpoints) != 0 || len(api.deletes) != 1 || api.deletes[0] != "draft_only=true" {
		t.Fatalf("endpoints=%d deletes=%q, want the kept draft deleted with draft_only=true", len(api.endpoints), api.deletes)
	}
}

// A failed activation answered with 5xx is retried with the same
// Idempotency-Key, and the replay returns the kept draft as 201. The draft is
// deleted rather than saved as though activation had succeeded.
func TestStubPrivateEndpointReplayedDraftAfterRetriedActivationIsDeleted(t *testing.T) {
	srv, api := newStubPrivateEndpoints(t)
	api.retainedDraftStatus = http.StatusBadGateway
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config:      privateEndpointConfig(srv.URL, "gpt-4o-prod", "0.000002"),
				ExpectError: regexp.MustCompile(`Private endpoint activation did not complete`),
			},
		},
		CheckDestroy: func(state *terraform.State) error {
			if len(state.RootModule().Resources) != 0 {
				return fmt.Errorf("state holds %d resources, want none", len(state.RootModule().Resources))
			}
			return nil
		},
	})
	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.createKeys) != 2 || api.createKeys[0] != api.createKeys[1] {
		t.Fatalf("create Idempotency-Keys = %q, want the 502 retried once with the same key", api.createKeys)
	}
	if len(api.endpoints) != 0 || len(api.deletes) != 1 || api.deletes[0] != "draft_only=true" {
		t.Fatalf("endpoints=%d deletes=%q, want the replayed draft deleted with draft_only=true", len(api.endpoints), api.deletes)
	}
}
