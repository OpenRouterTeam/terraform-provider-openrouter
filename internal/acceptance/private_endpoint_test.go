package acceptance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
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
	Prompt     string  `json:"prompt"`
	Completion string  `json:"completion"`
	CacheRead  *string `json:"cache_read"`
	CacheWrite *string `json:"cache_write"`
}

type stubPrivateEndpoint struct {
	ID                  string         `json:"id"`
	Status              string         `json:"status"`
	ModelPermaslug      string         `json:"model_permaslug"`
	ModelSlug           string         `json:"model_slug"`
	ModelName           string         `json:"model_name"`
	ProviderName        string         `json:"provider_name"`
	ProviderSlug        *string        `json:"provider_slug"`
	BaseURL             *string        `json:"base_url"`
	UpstreamModelID     string         `json:"upstream_model_id"`
	DeclaredZDR         *bool          `json:"declared_zdr"`
	DeclaredRegion      *string        `json:"declared_region"`
	DeclaredParameters  []string       `json:"declared_parameters"`
	TestedParameters    []string       `json:"tested_parameters"`
	MaxCompletionTokens any            `json:"max_completion_tokens"`
	Settings            map[string]any `json:"settings"`
	Pricing             stubPricing    `json:"pricing"`
	CreatedAt           string         `json:"created_at"`
}

// Create and PATCH return the limited managed summary. GET returns the full
// endpoint, including the settings, tested parameters and effective token cap.
// Active endpoints accept only pricing, settings and declared_parameters edits.
type stubPrivateEndpoints struct {
	mu             sync.Mutex
	nextID         int
	endpoints      map[string]*stubPrivateEndpoint
	creates        []map[string]any
	createKeys     []string
	byKey          map[string]string
	deletes        []string
	patches        []map[string]any
	puts           int
	getsAfterPatch int
	awaitingGet    bool
	failGets       int
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
	if r.Method == http.MethodPut {
		s.puts++
		writeError(http.StatusMethodNotAllowed, "PUT is obsolete; use PATCH /private-endpoints/{id}")
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
	case len(parts) == 2 && r.Method == http.MethodPatch:
		s.patch(w, r, endpoint, writeError)
		return
	case len(parts) == 2 && r.Method == http.MethodGet:
		if s.failGets > 0 {
			s.failGets--
			writeError(http.StatusForbidden, "Forbidden")
			return
		}
		if s.awaitingGet {
			s.getsAfterPatch++
			s.awaitingGet = false
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
	_ = json.NewEncoder(w).Encode(map[string]any{"data": stubDetailFiltered(*endpoint)})
}

// stubDetailFiltered mirrors the API read model: declared_parameters is returned as
// stored minus every name a validation run has already proven.
func stubDetailFiltered(endpoint stubPrivateEndpoint) stubPrivateEndpoint {
	filtered := endpoint
	filtered.DeclaredParameters = nil
	for _, name := range endpoint.DeclaredParameters {
		proven := false
		for _, tested := range endpoint.TestedParameters {
			if tested == name {
				proven = true
				break
			}
		}
		if !proven {
			filtered.DeclaredParameters = append(filtered.DeclaredParameters, name)
		}
	}
	return filtered
}

func (s *stubPrivateEndpoints) patch(w http.ResponseWriter, r *http.Request, endpoint *stubPrivateEndpoint, writeError func(int, string)) {
	var raw map[string]any
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil || len(raw) == 0 {
		writeError(http.StatusBadRequest, "Invalid or empty PATCH body")
		return
	}
	s.patches = append(s.patches, raw)
	for _, key := range []string{"base_url", "upstream_model_id", "declared_region", "declared_zdr"} {
		if _, present := raw[key]; present && endpoint.Status != "draft" {
			writeError(http.StatusConflict, "draft-only field on active endpoint: "+key)
			return
		}
	}
	for key := range raw {
		switch key {
		case "pricing", "settings", "declared_parameters":
		default:
			writeError(http.StatusBadRequest, "Unexpected PATCH field: "+key)
			return
		}
	}
	encoded, _ := json.Marshal(raw)
	var body struct {
		Pricing            *stubPricing   `json:"pricing"`
		Settings           map[string]any `json:"settings"`
		DeclaredParameters *[]string      `json:"declared_parameters"`
	}
	if err := json.Unmarshal(encoded, &body); err != nil ||
		(body.Pricing != nil && (body.Pricing.Prompt == "" || body.Pricing.Completion == "")) {
		writeError(http.StatusBadRequest, "Invalid PATCH body")
		return
	}
	if body.Pricing != nil {
		// Pricing replaces all four rates. Omitted cache rates return to the
		// fixture's pricing strategy defaults, not their earlier values.
		endpoint.Pricing = stubPricingDefaults(*body.Pricing)
	}
	if body.Settings != nil {
		stubMergePrivateEndpointSettings(endpoint.Settings, body.Settings)
		endpoint.MaxCompletionTokens = endpoint.Settings["max_completion_tokens"]
	}
	if body.DeclaredParameters != nil {
		endpoint.DeclaredParameters = *body.DeclaredParameters
	}
	s.awaitingGet = true
	// Deliberately omit settings, pricing, tested_parameters and the effective
	// max_completion_tokens. Only the subsequent full GET is authoritative.
	_ = json.NewEncoder(w).Encode(map[string]any{"data": stubSummary(endpoint)})
}

func stubPricingDefaults(pricing stubPricing) stubPricing {
	if pricing.CacheRead == nil {
		value := "0.000001"
		pricing.CacheRead = &value
	}
	if pricing.CacheWrite == nil {
		value := "0.000003"
		pricing.CacheWrite = &value
	}
	return pricing
}

func stubPrivateEndpointSettings() map[string]any {
	return map[string]any{
		"completions":    map[string]any{"chat": true, "text": false},
		"context_length": 128000, "max_prompt_tokens": 120000, "max_completion_tokens": 4096,
		"excluded_parameters": []string{},
		"inputs":              map[string]any{"audio": false, "base64_files": false, "base64_images": true, "file_urls": false, "image_urls": true},
		"quantization":        "unknown",
		"reasoning":           map[string]any{"return_mechanism": nil, "supported": false},
		"tool_choice":         map[string]any{"auto": true, "function": true, "none": true, "required": true},
	}
}

func stubMergePrivateEndpointSettings(settings, patch map[string]any) {
	for key, value := range patch {
		if nested, ok := value.(map[string]any); ok {
			if existing, ok := settings[key].(map[string]any); ok {
				stubMergePrivateEndpointSettings(existing, nested)
				continue
			}
		}
		settings[key] = value
	}
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
		ModelPermaslug     string         `json:"model_permaslug"`
		ProviderSlug       string         `json:"provider_slug"`
		UpstreamModelID    string         `json:"upstream_model_id"`
		BaseURL            *string        `json:"base_url"`
		DeclaredZDR        *bool          `json:"declared_zdr"`
		DeclaredRegion     *string        `json:"declared_region"`
		DeclaredParameters []string       `json:"declared_parameters"`
		Settings           map[string]any `json:"settings"`
		Pricing            *stubPricing   `json:"pricing"`
		Activate           *struct {
			WorkspaceID string `json:"workspace_id"`
		} `json:"activate"`
	}
	_ = json.Unmarshal(encoded, &body)
	s.nextID++
	endpoint := &stubPrivateEndpoint{
		ID:                 fmt.Sprintf("00000000-0000-4000-8000-%012d", s.nextID),
		Status:             "draft",
		ModelPermaslug:     body.ModelPermaslug,
		ModelSlug:          body.ModelPermaslug,
		ModelName:          "GPT-4o",
		ProviderName:       "OpenAI",
		ProviderSlug:       &body.ProviderSlug,
		BaseURL:            body.BaseURL,
		UpstreamModelID:    body.UpstreamModelID,
		DeclaredZDR:        body.DeclaredZDR,
		DeclaredRegion:     body.DeclaredRegion,
		DeclaredParameters: body.DeclaredParameters,
		TestedParameters:   []string{},
		Settings:           stubPrivateEndpointSettings(),
		Pricing:            stubPricingDefaults(stubPricing{Prompt: "0.0000025", Completion: "0.00001"}),
		CreatedAt:          "2026-09-26T12:00:00Z",
	}
	if body.Pricing != nil {
		endpoint.Pricing = stubPricingDefaults(*body.Pricing)
	}
	if endpoint.DeclaredParameters == nil {
		endpoint.DeclaredParameters = []string{}
	}
	stubMergePrivateEndpointSettings(endpoint.Settings, body.Settings)
	endpoint.MaxCompletionTokens = endpoint.Settings["max_completion_tokens"]
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
		endpoint.TestedParameters = []string{"tools", "stop"}
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

func (s *stubPrivateEndpoints) checkPatches(want int, wantLastBody string) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.puts != 0 || len(s.patches) != want {
			return fmt.Errorf("server saw %d PUT and %d PATCH calls, want 0 PUT and %d PATCH", s.puts, len(s.patches), want)
		}
		if s.getsAfterPatch != want || s.awaitingGet {
			return fmt.Errorf("server saw %d full GETs after %d PATCH calls, want one refresh per PATCH", s.getsAfterPatch, want)
		}
		if wantLastBody == "" {
			return nil
		}
		var expected map[string]any
		if err := json.Unmarshal([]byte(wantLastBody), &expected); err != nil {
			return fmt.Errorf("invalid expected PATCH body: %w", err)
		}
		if want == 0 || !reflect.DeepEqual(s.patches[want-1], expected) {
			return fmt.Errorf("PATCH bodies = %#v, want last body %#v", s.patches, expected)
		}
		return nil
	}
}

// Settings contains optional computed attributes, so unknown fields may be
// populated from prior state by the provider. Assert the exact top-level group
// and every configured nested value without prescribing those computed fields.
func (s *stubPrivateEndpoints) checkSettingsPatch(want int, maxCompletionTokens int, audio, required bool) resource.TestCheckFunc {
	return resource.ComposeAggregateTestCheckFunc(
		s.checkPatches(want, ""),
		func(_ *terraform.State) error {
			s.mu.Lock()
			defer s.mu.Unlock()
			if len(s.patches) != want || want == 0 {
				return fmt.Errorf("server saw %d PATCH calls, want %d", len(s.patches), want)
			}
			body := s.patches[want-1]
			settings, ok := body["settings"].(map[string]any)
			if !ok || len(body) != 1 {
				return fmt.Errorf("PATCH body = %#v, want only settings", body)
			}
			inputs, _ := settings["inputs"].(map[string]any)
			toolChoice, _ := settings["tool_choice"].(map[string]any)
			if settings["max_completion_tokens"] != float64(maxCompletionTokens) || inputs["audio"] != audio || toolChoice["required"] != required {
				return fmt.Errorf("PATCH settings = %#v, want max_completion_tokens=%d, inputs.audio=%t, tool_choice.required=%t", settings, maxCompletionTokens, audio, required)
			}
			return nil
		},
	)
}

func (s *stubPrivateEndpoints) checkDeleteCount(want int) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if len(s.deletes) != want {
			return fmt.Errorf("server saw %d deletes, want %d", len(s.deletes), want)
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
  base_url          = "https://deployment.example.com/v1"
  declared_zdr      = true
  declared_region   = "us"
  pricing = {
    prompt     = %q
    completion = "0.000008"
    cache_read = "0.0000005"
    cache_write = "0.0000025"
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
					api.checkPatches(1, `{"pricing":{"prompt":"0.0000015","completion":"0.000008","cache_read":"0.0000005","cache_write":"0.0000025"}}`),
					api.checkDeleteCount(0),
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
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "pricing.prompt", "0.0000015"),
					api.checkPatches(2, `{"pricing":{"prompt":"0.0000015","completion":"0.000008","cache_read":"0.0000005","cache_write":"0.0000025"}}`),
				),
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

func privateEndpointMutableConfig(serverURL, prompt string, maxCompletionTokens int, audio, required bool, declaredParameters string) string {
	config := privateEndpointConfig(serverURL, "gpt-4o-prod", prompt)
	return strings.Replace(config, "  pricing = {", fmt.Sprintf(`  declared_parameters = %s
  settings = {
    max_completion_tokens = %d
    inputs = { audio = %t }
    tool_choice = { required = %t }
  }
  pricing = {`, declaredParameters, maxCompletionTokens, audio, required), 1)
}

func TestStubPrivateEndpointSettingsOnlyUpdate(t *testing.T) {
	srv, api := newStubPrivateEndpoints(t)
	const name = "openrouter_private_endpoint.test"
	initial := privateEndpointMutableConfig(srv.URL, "0.000002", 4096, false, true, `["temperature", "response_format"]`)
	updated := privateEndpointMutableConfig(srv.URL, "0.000002", 8192, true, false, `["temperature", "response_format"]`)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		CheckDestroy:             api.checkDeletesWithoutDraftOnly,
		Steps: []resource.TestStep{
			{Config: initial, Check: api.checkPatches(0, "")},
			{
				Config: updated,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(name, plancheck.ResourceActionUpdate)},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "id", "00000000-0000-4000-8000-000000000001"),
					resource.TestCheckResourceAttr(name, "status", "active"),
					resource.TestCheckResourceAttr(name, "settings.max_completion_tokens", "8192"),
					resource.TestCheckResourceAttr(name, "settings.inputs.audio", "true"),
					resource.TestCheckResourceAttr(name, "settings.tool_choice.required", "false"),
					// Unconfigured defaults survive the partial settings update.
					resource.TestCheckResourceAttr(name, "settings.context_length", "128000"),
					resource.TestCheckResourceAttr(name, "settings.inputs.image_urls", "true"),
					resource.TestCheckResourceAttr(name, "settings.tool_choice.auto", "true"),
					resource.TestCheckResourceAttr(name, "pricing.prompt", "0.000002"),
					resource.TestCheckResourceAttr(name, "declared_parameters.#", "2"),
					// The limited PATCH response has neither of these computed fields.
					resource.TestCheckResourceAttr(name, "max_completion_tokens", "8192"),
					resource.TestCheckResourceAttr(name, "tested_parameters.#", "2"),
					resource.TestCheckResourceAttr(name, "tested_parameters.0", "tools"),
					resource.TestCheckResourceAttr(name, "tested_parameters.1", "stop"),
					api.checkSettingsPatch(1, 8192, true, false),
					api.checkCreateCount(1),
					api.checkDeleteCount(0),
				),
			},
			{Config: updated, PlanOnly: true},
			{Config: updated, Check: api.checkSettingsPatch(1, 8192, true, false)},
		},
	})
}

func TestStubPrivateEndpointPricingUpdatePreservesCacheRates(t *testing.T) {
	srv, api := newStubPrivateEndpoints(t)
	const name = "openrouter_private_endpoint.test"
	initial := privateEndpointMutableConfig(srv.URL, "0.000002", 4096, false, true, `["temperature"]`)
	updated := privateEndpointMutableConfig(srv.URL, "0.0000015", 4096, false, true, `["temperature"]`)
	cacheUpdated := strings.ReplaceAll(strings.ReplaceAll(updated, `cache_read = "0.0000005"`, `cache_read = "0.0000004"`), `cache_write = "0.0000025"`, `cache_write = "0.000002"`)
	inPlace := resource.ConfigPlanChecks{
		PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(name, plancheck.ResourceActionUpdate)},
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		CheckDestroy:             api.checkDeletesWithoutDraftOnly,
		Steps: []resource.TestStep{
			{Config: initial},
			{
				Config:           updated,
				ConfigPlanChecks: inPlace,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "id", "00000000-0000-4000-8000-000000000001"),
					resource.TestCheckResourceAttr(name, "pricing.prompt", "0.0000015"),
					resource.TestCheckResourceAttr(name, "pricing.completion", "0.000008"),
					resource.TestCheckResourceAttr(name, "pricing.cache_read", "0.0000005"),
					resource.TestCheckResourceAttr(name, "pricing.cache_write", "0.0000025"),
					resource.TestCheckResourceAttr(name, "max_completion_tokens", "4096"),
					resource.TestCheckResourceAttr(name, "tested_parameters.#", "2"),
					api.checkPatches(1, `{"pricing":{"prompt":"0.0000015","completion":"0.000008","cache_read":"0.0000005","cache_write":"0.0000025"}}`),
					api.checkCreateCount(1),
					api.checkDeleteCount(0),
				),
			},
			{
				Config:           cacheUpdated,
				ConfigPlanChecks: inPlace,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "pricing.cache_read", "0.0000004"),
					resource.TestCheckResourceAttr(name, "pricing.cache_write", "0.000002"),
					api.checkPatches(2, `{"pricing":{"prompt":"0.0000015","completion":"0.000008","cache_read":"0.0000004","cache_write":"0.000002"}}`),
					api.checkCreateCount(1),
					api.checkDeleteCount(0),
				),
			},
			{Config: cacheUpdated, PlanOnly: true},
		},
	})
}

func TestStubPrivateEndpointPricingUpdatePreservesComputedCacheRates(t *testing.T) {
	srv, api := newStubPrivateEndpoints(t)
	const name = "openrouter_private_endpoint.test"
	config := func(prompt string) string {
		value := privateEndpointConfig(srv.URL, "gpt-4o-prod", prompt)
		value = strings.Replace(value, `    cache_read = "0.0000005"`, "", 1)
		return strings.Replace(value, `    cache_write = "0.0000025"`, "", 1)
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{Config: config("0.000002")},
			{
				Config: config("0.0000015"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(name, plancheck.ResourceActionUpdate)},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "pricing.cache_read", "0.000001"),
					resource.TestCheckResourceAttr(name, "pricing.cache_write", "0.000003"),
					api.checkPatches(1, `{"pricing":{"prompt":"0.0000015","completion":"0.000008","cache_read":"0.000001","cache_write":"0.000003"}}`),
					api.checkCreateCount(1),
					api.checkDeleteCount(0),
				),
			},
		},
	})
}

func TestStubPrivateEndpointDeclaredParametersOnlyUpdate(t *testing.T) {
	srv, api := newStubPrivateEndpoints(t)
	const name = "openrouter_private_endpoint.test"
	initial := privateEndpointMutableConfig(srv.URL, "0.000002", 4096, false, true, `["temperature", "response_format"]`)
	updated := privateEndpointMutableConfig(srv.URL, "0.000002", 4096, false, true, `["structured_outputs"]`)
	cleared := privateEndpointMutableConfig(srv.URL, "0.000002", 4096, false, true, `[]`)
	inPlace := resource.ConfigPlanChecks{
		PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(name, plancheck.ResourceActionUpdate)},
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		CheckDestroy:             api.checkDeletesWithoutDraftOnly,
		Steps: []resource.TestStep{
			{Config: initial},
			{
				Config:           updated,
				ConfigPlanChecks: inPlace,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "id", "00000000-0000-4000-8000-000000000001"),
					resource.TestCheckResourceAttr(name, "declared_parameters.#", "1"),
					resource.TestCheckResourceAttr(name, "declared_parameters.0", "structured_outputs"),
					resource.TestCheckResourceAttr(name, "tested_parameters.#", "2"),
					resource.TestCheckResourceAttr(name, "tested_parameters.0", "tools"),
					resource.TestCheckResourceAttr(name, "tested_parameters.1", "stop"),
					resource.TestCheckResourceAttr(name, "max_completion_tokens", "4096"),
					api.checkPatches(1, `{"declared_parameters":["structured_outputs"]}`),
					api.checkCreateCount(1),
					api.checkDeleteCount(0),
				),
			},
			{
				Config:           cleared,
				ConfigPlanChecks: inPlace,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "declared_parameters.#", "0"),
					resource.TestCheckResourceAttr(name, "tested_parameters.#", "2"),
					api.checkPatches(2, `{"declared_parameters":[]}`),
					api.checkCreateCount(1),
					api.checkDeleteCount(0),
				),
			},
			{Config: cleared, PlanOnly: true},
		},
	})
}

func TestStubPrivateEndpointCombinedUpdateUsesOnePatch(t *testing.T) {
	srv, api := newStubPrivateEndpoints(t)
	const name = "openrouter_private_endpoint.test"
	initial := privateEndpointMutableConfig(srv.URL, "0.000002", 4096, false, true, `["temperature"]`)
	updated := privateEndpointMutableConfig(srv.URL, "0.0000015", 8192, true, false, `["structured_outputs"]`)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		CheckDestroy:             api.checkDeletesWithoutDraftOnly,
		Steps: []resource.TestStep{
			{Config: initial},
			{
				Config: updated,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(name, plancheck.ResourceActionUpdate)},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					api.checkPatches(1, ""),
					api.checkCreateCount(1),
					api.checkDeleteCount(0),
					resource.TestCheckResourceAttr(name, "id", "00000000-0000-4000-8000-000000000001"),
					resource.TestCheckResourceAttr(name, "max_completion_tokens", "8192"),
					resource.TestCheckResourceAttr(name, "tested_parameters.#", "2"),
					func(_ *terraform.State) error {
						api.mu.Lock()
						defer api.mu.Unlock()
						if len(api.patches) != 1 {
							return fmt.Errorf("server saw %d PATCH calls, want 1", len(api.patches))
						}
						body := api.patches[0]
						wantPricing := map[string]any{"prompt": "0.0000015", "completion": "0.000008", "cache_read": "0.0000005", "cache_write": "0.0000025"}
						settings, _ := body["settings"].(map[string]any)
						inputs, _ := settings["inputs"].(map[string]any)
						toolChoice, _ := settings["tool_choice"].(map[string]any)
						if len(body) != 3 || !reflect.DeepEqual(body["pricing"], wantPricing) ||
							!reflect.DeepEqual(body["declared_parameters"], []any{"structured_outputs"}) ||
							settings["max_completion_tokens"] != float64(8192) || inputs["audio"] != true || toolChoice["required"] != false {
							return fmt.Errorf("PATCH body = %#v, want only the three changed groups with planned values", body)
						}
						return nil
					},
				),
			},
			{Config: updated, PlanOnly: true},
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
					api.checkPatches(0, ""),
					api.checkDeleteCount(0),
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
					api.checkPatches(0, ""),
					api.checkDeleteCount(0),
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
			{
				Config: otherWorkspace, ConfigPlanChecks: inPlace,
				Check: resource.ComposeAggregateTestCheckFunc(api.checkPatches(0, ""), api.checkCreateCount(1), api.checkDeleteCount(0)),
			},
			{
				Config: withoutActivate, ConfigPlanChecks: inPlace,
				Check: resource.ComposeAggregateTestCheckFunc(api.checkPatches(0, ""), api.checkCreateCount(1), api.checkDeleteCount(0)),
			},
			{
				Config: withoutActivate,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr(name, "activate"),
					resource.TestCheckResourceAttr(name, "status", "active"),
					api.checkCreateCount(1),
					api.checkPatches(0, ""),
					api.checkDeleteCount(0),
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

// The API only accepts HTTPS base URLs; any other scheme fails at plan time
// instead of with a 400 on create.
func TestStubPrivateEndpointRejectsNonHTTPSBaseURL(t *testing.T) {
	srv, api := newStubPrivateEndpoints(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config:      privateEndpointBaseURLConfig(srv.URL, "http://contoso.openai.azure.com", privateEndpointWorkspaceID),
				ExpectError: regexp.MustCompile(`must match pattern`),
			},
		},
	})
	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.creates) != 0 {
		t.Fatalf("server saw %d creates for an http base_url, want 0", len(api.creates))
	}
}

// The reconciliation must also put a proven name back where the configuration had
// it. The read model filters by membership, not by order, and Terraform compares
// the list element-wise, so appending the proven names at the end of the list is
// itself a change.
func TestStubPrivateEndpointDeclaredParametersKeepConfiguredOrder(t *testing.T) {
	srv, api := newStubPrivateEndpoints(t)
	const name = "openrouter_private_endpoint.test"
	// The activate step proves `tools`, so the API stops returning it inside
	// declared_parameters. The reconciliation has to put it back where the
	// configuration had it: declared_parameters is a list, so a different order
	// is a different value to Terraform and the apply fails as an inconsistent
	// result.
	config := privateEndpointMutableConfig(srv.URL, "0.000002", 4096, false, true, `["tools", "temperature"]`)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		CheckDestroy:             api.checkDeletesWithoutDraftOnly,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "declared_parameters.#", "2"),
					resource.TestCheckResourceAttr(name, "declared_parameters.0", "tools"),
					resource.TestCheckResourceAttr(name, "declared_parameters.1", "temperature"),
					resource.TestCheckResourceAttr(name, "tested_parameters.#", "2"),
					api.checkCreateCount(1),
				),
			},
			{
				// Same config again: the order must stay settled, so the step
				// fails on any PATCH the refresh would have provoked.
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "declared_parameters.0", "tools"),
					resource.TestCheckResourceAttr(name, "declared_parameters.1", "temperature"),
					api.checkPatches(0, ""),
				),
			},
		},
	})
}

// A validation run moves a proven name out of the declared list, and the detail
// response only returns the names that are still undeclared. Declaring a name that
// the endpoint has already proven must not produce a plan on every refresh.
func TestStubPrivateEndpointDeclaredParameterSurvivesPromotion(t *testing.T) {
	srv, api := newStubPrivateEndpoints(t)
	const name = "openrouter_private_endpoint.test"
	config := privateEndpointMutableConfig(srv.URL, "0.000002", 4096, false, true, `["tools"]`)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		CheckDestroy:             api.checkDeletesWithoutDraftOnly,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "declared_parameters.#", "1"),
					resource.TestCheckResourceAttr(name, "declared_parameters.0", "tools"),
					resource.TestCheckResourceAttr(name, "tested_parameters.#", "2"),
					api.checkCreateCount(1),
					api.checkDeleteCount(0),
				),
			},
			{
				// Identical config: the step fails if the read dropped the proven
				// name and the resulting plan proposes it again.
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "declared_parameters.#", "1"),
					resource.TestCheckResourceAttr(name, "declared_parameters.0", "tools"),
					api.checkPatches(0, ""),
				),
			},
			{
				// Explicitly removing a proven name must still clear it: the
				// reconciliation restores from the write's intended collection,
				// not from stale state.
				Config: privateEndpointMutableConfig(srv.URL, "0.000002", 4096, false, true, `[]`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "declared_parameters.#", "0"),
					api.checkPatches(1, `{"declared_parameters":[]}`),
				),
			},
		},
	})
}
