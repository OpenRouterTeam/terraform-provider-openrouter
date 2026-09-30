package acceptance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const organizationSettingsOrgID = "org_2dHFtVWx2n56w6HkM0000000000"

// Organization management keys belong to a Clerk organization, whose ID
// carries the org_ prefix.
var orgIDPattern = regexp.MustCompile(`^org_[A-Za-z0-9]+$`)

// stubOrganizationSettings serves GET/PATCH /organization/settings for one
// organization and records every request so the tests can prove that
// destroy leaves the settings untouched.
type stubOrganizationSettings struct {
	mu       sync.Mutex
	filtered bool
	requests []string
}

func newStubOrganizationSettings(t *testing.T, filtered bool) (*httptest.Server, *stubOrganizationSettings) {
	t.Helper()
	api := &stubOrganizationSettings{filtered: filtered}
	srv := httptest.NewServer(http.HandlerFunc(api.serveHTTP))
	t.Cleanup(srv.Close)
	return srv, api
}

func (s *stubOrganizationSettings) serveHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, r.Method+" "+r.URL.Path)
	w.Header().Set("Content-Type", "application/json")
	writeError := func(status int, message string) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"code": status, "message": message},
		})
	}
	if r.URL.Path != "/organization/settings" {
		writeError(http.StatusNotFound, "Not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
	case http.MethodPatch:
		// The API rejects unknown fields and an empty body, so decode into a
		// pointer and fail unless exactly the one supported field is sent.
		var body map[string]*bool
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil ||
			len(body) != 1 || body["is_filtered_model_catalog_enabled"] == nil {
			writeError(http.StatusBadRequest, "Invalid request body")
			return
		}
		s.filtered = *body["is_filtered_model_catalog_enabled"]
	default:
		writeError(http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{
			"id":                                organizationSettingsOrgID,
			"is_filtered_model_catalog_enabled": s.filtered,
		},
	})
}

func (s *stubOrganizationSettings) set(filtered bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.filtered = filtered
}

func (s *stubOrganizationSettings) checkFiltered(want bool) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.filtered != want {
			return fmt.Errorf("server is_filtered_model_catalog_enabled = %t, want %t", s.filtered, want)
		}
		return nil
	}
}

// checkNoWrites fails if any request other than a GET reached the server
// since the last call, proving that destroy only forgets the resource.
func (s *stubOrganizationSettings) checkNoWrites() resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, req := range s.requests {
			if req != "GET /organization/settings" {
				return fmt.Errorf("unexpected request after the last apply: %s", req)
			}
		}
		return nil
	}
}

func (s *stubOrganizationSettings) resetRequests() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = nil
}

func stubOrganizationSettingsConfig(serverURL string, filtered bool) string {
	return fmt.Sprintf(`
provider "openrouter" {
  api_key    = "local-test-only"
  server_url = %q
}
resource "openrouter_organization_settings" "test" {
  is_filtered_model_catalog_enabled = %t
}
`, serverURL, filtered)
}

// UnitTest intentionally bypasses TF_ACC: every request goes to a local fixture.
// Create and update both PATCH the singleton; a remote change is picked up by
// refresh and planned as an in-place update; destroy sends no write.
func TestStubOrganizationSettingsLifecycle(t *testing.T) {
	srv, api := newStubOrganizationSettings(t, false)
	check := func(filtered bool) resource.TestCheckFunc {
		return resource.ComposeAggregateTestCheckFunc(
			resource.TestCheckResourceAttr("openrouter_organization_settings.test", "id", organizationSettingsOrgID),
			resource.TestCheckResourceAttr("openrouter_organization_settings.test", "is_filtered_model_catalog_enabled", fmt.Sprint(filtered)),
			api.checkFiltered(filtered),
		)
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		CheckDestroy: resource.ComposeAggregateTestCheckFunc(
			api.checkNoWrites(),
			api.checkFiltered(false),
		),
		Steps: []resource.TestStep{
			{Config: stubOrganizationSettingsConfig(srv.URL, true), Check: check(true)},
			{Config: stubOrganizationSettingsConfig(srv.URL, true), PlanOnly: true},
			{
				Config: stubOrganizationSettingsConfig(srv.URL, false),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("openrouter_organization_settings.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: check(false),
			},
			{
				PreConfig: func() { api.set(true) },
				Config:    stubOrganizationSettingsConfig(srv.URL, false),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("openrouter_organization_settings.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: check(false),
			},
			{
				PreConfig: api.resetRequests,
				Config:    stubOrganizationSettingsConfig(srv.URL, false),
				PlanOnly:  true,
			},
		},
	})
}

// TestAccOrganizationSettings_Lifecycle flips the acceptance organization's
// toggle on and back off. Destroy does not reset the setting, so the last
// step leaves it off.
func TestAccOrganizationSettings_Lifecycle(t *testing.T) {
	config := func(filtered bool) string {
		return providerConfig() + fmt.Sprintf(`
resource "openrouter_organization_settings" "test" {
  is_filtered_model_catalog_enabled = %t
}
`, filtered)
	}
	check := func(filtered bool) resource.TestCheckFunc {
		return resource.ComposeAggregateTestCheckFunc(
			resource.TestMatchResourceAttr("openrouter_organization_settings.test", "id", orgIDPattern),
			resource.TestCheckResourceAttr("openrouter_organization_settings.test", "is_filtered_model_catalog_enabled", fmt.Sprint(filtered)),
		)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{Config: config(true), Check: check(true)},
			{Config: config(true), PlanOnly: true},
			{Config: config(false), Check: check(false)},
			{Config: config(false), PlanOnly: true},
		},
	})
}
