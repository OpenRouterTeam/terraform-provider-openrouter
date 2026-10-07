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
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// stubPrivateProviders mirrors the /private-providers API: the slug is derived
// from the name, create requires every data policy key, PATCH merges fields and
// the data policy over the stored provider and rejects retention days without
// retention, and delete is refused while a private endpoint runs under it.
type stubPrivateProviders struct {
	mu            sync.Mutex
	providers     map[string]map[string]any
	patches       []map[string]any
	liveEndpoints bool
}

var stubSlugSeparators = regexp.MustCompile(`[^a-z0-9]+`)

func newStubPrivateProviders(t *testing.T) (*httptest.Server, *stubPrivateProviders) {
	t.Helper()
	api := &stubPrivateProviders{providers: make(map[string]map[string]any)}
	srv := httptest.NewServer(http.HandlerFunc(api.serveHTTP))
	t.Cleanup(srv.Close)
	return srv, api
}

func (s *stubPrivateProviders) serveHTTP(w http.ResponseWriter, r *http.Request) {
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
	if parts[0] != "private-providers" || len(parts) > 2 {
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
	provider, ok := s.providers[parts[1]]
	if !ok {
		writeError(http.StatusNotFound, "Private provider not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
	case http.MethodPatch:
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(http.StatusBadRequest, "Invalid body")
			return
		}
		s.patches = append(s.patches, body)
		if message := stubPatchProvider(provider, body); message != "" {
			writeError(http.StatusBadRequest, message)
			return
		}
	case http.MethodDelete:
		if s.liveEndpoints {
			writeError(http.StatusConflict, "Delete the private endpoints under this provider first")
			return
		}
		delete(s.providers, parts[1])
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"slug": parts[1]}})
		return
	default:
		writeError(http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"data": provider})
}

func (s *stubPrivateProviders) create(w http.ResponseWriter, r *http.Request, writeError func(int, string)) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(http.StatusBadRequest, "Invalid body")
		return
	}
	policy, _ := body["data_policy"].(map[string]any)
	for _, field := range []string{"training", "retains_prompts", "prompt_retention_days"} {
		if _, ok := policy[field]; !ok {
			writeError(http.StatusBadRequest, "data_policy."+field+" is required")
			return
		}
	}
	name, _ := body["name"].(string)
	slug := strings.Trim(stubSlugSeparators.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "-"), "-")
	if _, taken := s.providers[slug]; taken {
		writeError(http.StatusConflict, "A provider with this name already exists")
		return
	}
	provider := map[string]any{
		"name":               name,
		"slug":               slug,
		"display_name":       body["display_name"],
		"base_url":           body["base_url"],
		"privacy_policy_url": body["privacy_policy_url"],
		"headquarters":       body["headquarters"],
		"datacenters":        []any{},
		"data_policy":        body["data_policy"],
		"created_at":         "2026-10-07T00:00:00.000Z",
	}
	if datacenters, ok := body["datacenters"]; ok {
		provider["datacenters"] = datacenters
	}
	s.providers[slug] = provider
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{"data": provider})
}

// stubPatchProvider applies a PATCH body the way the API does and returns the
// 400 message for a merged policy that keeps retention days without retaining
// prompts. Like the API, it never clears stored days on its own.
func stubPatchProvider(provider, body map[string]any) string {
	for _, field := range []string{"display_name", "base_url", "privacy_policy_url", "headquarters", "datacenters"} {
		if value, ok := body[field]; ok {
			provider[field] = value
		}
	}
	patch, ok := body["data_policy"].(map[string]any)
	if !ok {
		return ""
	}
	merged := map[string]any{}
	for field, value := range provider["data_policy"].(map[string]any) {
		merged[field] = value
	}
	for field, value := range patch {
		merged[field] = value
	}
	if merged["retains_prompts"] == false && merged["prompt_retention_days"] != nil {
		return "prompt_retention_days must be null unless retains_prompts is true"
	}
	provider["data_policy"] = merged
	return ""
}

func (s *stubPrivateProviders) setLiveEndpoints(live bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.liveEndpoints = live
}

func (s *stubPrivateProviders) checkLastPatchOmits(field string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if len(s.patches) == 0 {
			return fmt.Errorf("no PATCH request was sent")
		}
		if _, ok := s.patches[len(s.patches)-1][field]; ok {
			return fmt.Errorf("last PATCH body carries %q: %v", field, s.patches[len(s.patches)-1])
		}
		return nil
	}
}

// checkStoredNull fails unless the stored provider has each field set to null.
// A field under the data policy is named "data_policy.<field>".
func (s *stubPrivateProviders) checkStoredNull(slug string, fields ...string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		provider, ok := s.providers[slug]
		if !ok {
			return fmt.Errorf("private provider %q is not stored", slug)
		}
		for _, field := range fields {
			object, key := provider, field
			if policyField, isPolicy := strings.CutPrefix(field, "data_policy."); isPolicy {
				object, key = provider["data_policy"].(map[string]any), policyField
			}
			if value := object[key]; value != nil {
				return fmt.Errorf("stored %s is %v, want null", field, value)
			}
		}
		return nil
	}
}

func (s *stubPrivateProviders) checkDestroyed(*terraform.State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.providers) != 0 {
		return fmt.Errorf("%d private providers remain after destroy", len(s.providers))
	}
	return nil
}

func privateProviderConfig(serverURL, name, displayName, details, dataPolicy string) string {
	return stubBudgetProviderConfig(serverURL) + fmt.Sprintf(`
resource "openrouter_private_provider" "test" {
  name         = %q
  display_name = %q
  base_url     = "https://inference.example.com/v1"
  datacenters  = ["US"]
  %s
  data_policy  = %s
}
`, name, displayName, details, dataPolicy)
}

const (
	privateProviderRetains30Days = `{
    training              = false
    retains_prompts       = true
    prompt_retention_days = 30
  }`
	privateProviderRetainsWithoutLimit = `{
    training        = false
    retains_prompts = true
  }`
	privateProviderRetainsNothing = `{
    training        = false
    retains_prompts = false
  }`
	privateProviderDetails = `headquarters       = "US"
  privacy_policy_url = "https://acme.example/privacy"`
)

// UnitTest intentionally bypasses TF_ACC: every request goes to a local fixture.
func TestStubPrivateProviderLifecycle(t *testing.T) {
	srv, api := newStubPrivateProviders(t)
	const name = "openrouter_private_provider.test"
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		CheckDestroy:             api.checkDestroyed,
		Steps: []resource.TestStep{
			{
				Config: privateProviderConfig(srv.URL, "Acme Inference", "Acme", privateProviderDetails, privateProviderRetains30Days),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "slug", "acme-inference"),
					resource.TestCheckResourceAttr(name, "headquarters", "US"),
					resource.TestCheckResourceAttr(name, "data_policy.prompt_retention_days", "30"),
					resource.TestCheckResourceAttr(name, "datacenters.0", "US"),
					resource.TestCheckResourceAttrSet(name, "created_at"),
				),
			},
			{Config: privateProviderConfig(srv.URL, "Acme Inference", "Acme", privateProviderDetails, privateProviderRetains30Days), PlanOnly: true},
			{
				// Removing the days while retention stays on clears them.
				Config: privateProviderConfig(srv.URL, "Acme Inference", "Acme", privateProviderDetails, privateProviderRetainsWithoutLimit),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(name, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "data_policy.retains_prompts", "true"),
					resource.TestCheckNoResourceAttr(name, "data_policy.prompt_retention_days"),
					api.checkStoredNull("acme-inference", "data_policy.prompt_retention_days"),
				),
			},
			{
				// Turning retention off and removing the optional details sends
				// explicit nulls, so the stored values clear.
				Config: privateProviderConfig(srv.URL, "Acme Inference", "Acme Labs", "", privateProviderRetainsNothing),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(name, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "slug", "acme-inference"),
					resource.TestCheckResourceAttr(name, "display_name", "Acme Labs"),
					resource.TestCheckResourceAttr(name, "data_policy.retains_prompts", "false"),
					resource.TestCheckNoResourceAttr(name, "headquarters"),
					resource.TestCheckNoResourceAttr(name, "privacy_policy_url"),
					api.checkStoredNull("acme-inference", "headquarters", "privacy_policy_url"),
					api.checkLastPatchOmits("name"),
					api.checkLastPatchOmits("slug"),
				),
			},
			{Config: privateProviderConfig(srv.URL, "Acme Inference", "Acme Labs", "", privateProviderRetainsNothing), PlanOnly: true},
			{
				Config: privateProviderConfig(srv.URL, "Acme Labs", "Acme Labs", "", privateProviderRetainsNothing),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(name, plancheck.ResourceActionDestroyBeforeCreate),
					},
				},
				Check: resource.TestCheckResourceAttr(name, "slug", "acme-labs"),
			},
			{
				ResourceName:                         name,
				ImportState:                          true,
				ImportStateId:                        "acme-labs",
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "slug",
			},
		},
	})
}

// A provider with a live private endpoint fails to destroy with the API's 409
// and stays in state until the endpoint is gone.
func TestStubPrivateProviderDeleteConflictKeepsProvider(t *testing.T) {
	srv, api := newStubPrivateProviders(t)
	config := privateProviderConfig(srv.URL, "Acme Inference", "Acme", "", privateProviderRetainsNothing)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		CheckDestroy:             api.checkDestroyed,
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig:   func() { api.setLiveEndpoints(true) },
				Config:      stubBudgetProviderConfig(srv.URL),
				ExpectError: regexp.MustCompile(`409`),
			},
			{
				PreConfig: func() { api.setLiveEndpoints(false) },
				Config:    stubBudgetProviderConfig(srv.URL),
			},
		},
	})
}
