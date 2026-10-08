package acceptance

// Write-only credentials (issue #686), tested against an in-process stub of the
// Management API. Like stub_repro_test.go these ignore the live acceptance
// lane's credentials. They need Terraform 1.11+, and use an ephemeral
// variable as the credential source so that, as in real use, the secret never
// appears as a literal in configuration that Terraform persists.
//
// Every test asserts the credential reaches the API AND is absent from state
// and from the saved plan. The legacy tests are the negative control: the same
// scan finds the marker when the legacy attributes are used, so a green write-only
// run cannot be a scan that never looks.
//
// Run: go test ./internal/acceptance -run TestStubWriteOnly -v

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/config"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"
)

const (
	woMarker  = "tf-synthetic-secret-marker"
	woMarker2 = "tf-synthetic-secret-marker-rotated"
)

type recordedRequest struct {
	Method string
	Path   string
	Body   map[string]any
}

// writeOnlyStub implements the BYOK and observability destination endpoints
// and records every request body.
type writeOnlyStub struct {
	mu       sync.Mutex
	requests []recordedRequest
	byok     map[string]any
	dest     map[string]any
	// failUpdates makes PATCH return a server error.
	failUpdates bool
	// maskCredentials makes destination reads replace config values for
	// credential keys with a mask, as the documented API does.
	maskCredentials bool
}

func (s *writeOnlyStub) record(r *http.Request, body map[string]any) {
	s.requests = append(s.requests, recordedRequest{Method: r.Method, Path: r.URL.Path, Body: body})
}

// bodies returns the recorded request bodies for a method and path prefix.
func (s *writeOnlyStub) bodies(method, pathPrefix string) []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []map[string]any
	for _, r := range s.requests {
		if r.Method == method && strings.HasPrefix(r.Path, pathPrefix) {
			out = append(out, r.Body)
		}
	}
	return out
}

func (s *writeOnlyStub) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func readBody(r *http.Request) map[string]any {
	b, _ := io.ReadAll(r.Body)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}

func newWriteOnlyStub(t *testing.T) (*writeOnlyStub, *httptest.Server) {
	t.Helper()
	s := &writeOnlyStub{}
	mux := http.NewServeMux()

	mux.HandleFunc("/byok", func(w http.ResponseWriter, r *http.Request) {
		body := readBody(r)
		s.mu.Lock()
		defer s.mu.Unlock()
		s.record(r, body)
		s.byok = map[string]any{
			"id": "byok_stub_1", "provider": body["provider"], "workspace_id": "ws_stub",
			"label": "sk-…cdef", "name": body["name"], "disabled": false, "is_fallback": false,
			"is_required": false, "is_byok_only": false, "declared_zdr": nil, "declared_region": nil,
			"allowed_models": nil, "allowed_api_key_hashes": nil, "allowed_user_ids": nil,
			"sort_order": 0, "created_at": "2026-08-19T00:00:00Z",
		}
		writeJSON(w, http.StatusCreated, map[string]any{"data": s.byok})
	})
	mux.HandleFunc("/byok/", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if r.Method == http.MethodPatch {
			body = readBody(r)
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		s.record(r, body)
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, map[string]any{"data": s.byok})
		case http.MethodPatch:
			if s.failUpdates {
				writeJSON(w, http.StatusForbidden, map[string]any{"error": map[string]any{"code": 403, "message": "stub failure"}})
				return
			}
			for k, v := range body {
				if k != "key" { // the API never echoes the credential
					s.byok[k] = v
				}
			}
			writeJSON(w, http.StatusOK, map[string]any{"data": s.byok})
		case http.MethodDelete:
			writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/observability/destinations", func(w http.ResponseWriter, r *http.Request) {
		body := readBody(r)
		s.mu.Lock()
		defer s.mu.Unlock()
		s.record(r, body)
		body["id"] = "dest_stub_1"
		body["workspace_id"] = "ws_stub"
		body["created_at"] = "2026-08-19T00:00:00Z"
		body["updated_at"] = "2026-08-19T00:00:00Z"
		for k, v := range map[string]any{
			"regions": []any{}, "privacy_mode": false, "sampling_rate": 1, "api_key_hashes": nil,
			"broadcast_generation_cost": false, "broadcast_generation_identity": false,
			"broadcast_generation_request_context": false,
			"filter_rules":                         map[string]any{"enabled": true, "groups": []any{}},
		} {
			if _, ok := body[k]; !ok {
				body[k] = v
			}
		}
		s.dest = body
		writeJSON(w, http.StatusCreated, map[string]any{"data": s.destView()})
	})
	mux.HandleFunc("/observability/destinations/", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if r.Method == http.MethodPatch {
			body = readBody(r)
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		s.record(r, body)
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, map[string]any{"data": s.destView()})
		case http.MethodPatch:
			if s.failUpdates {
				writeJSON(w, http.StatusForbidden, map[string]any{"error": map[string]any{"code": 403, "message": "stub failure"}})
				return
			}
			// Documented behavior: omitted config fields keep their value.
			for k, v := range body {
				if k == "config" {
					cfg, _ := s.dest["config"].(map[string]any)
					for ck, cv := range v.(map[string]any) {
						cfg[ck] = cv
					}
					continue
				}
				s.dest[k] = v
			}
			s.dest["updated_at"] = "2026-08-19T00:01:00Z"
			writeJSON(w, http.StatusOK, map[string]any{"data": s.destView()})
		case http.MethodDelete:
			writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return s, srv
}

// destView is the stored destination as the API reports it: the raw config by
// default, or with credential values masked.
func (s *writeOnlyStub) destView() map[string]any {
	out := map[string]any{}
	for k, v := range s.dest {
		out[k] = v
	}
	cfg := map[string]any{}
	for k, v := range s.dest["config"].(map[string]any) {
		cfg[k] = v
	}
	if s.maskCredentials {
		if _, ok := cfg["headers"]; ok {
			cfg["headers"] = map[string]any{"Authorization": "Bearer ****"}
		}
	}
	out["config"] = cfg
	return out
}

// planContainsNoMarker fails the step if the marker appears anywhere in the
// plan Terraform produced, including its variable values and prior state.
type planContainsNoMarker struct{ marker string }

func (p planContainsNoMarker) CheckPlan(_ context.Context, req plancheck.CheckPlanRequest, resp *plancheck.CheckPlanResponse) {
	raw, err := json.Marshal(req.Plan)
	if err != nil {
		resp.Error = err
		return
	}
	if strings.Contains(string(raw), p.marker) {
		resp.Error = fmt.Errorf("the plan JSON contains the credential marker")
	}
}

// stateMarker returns a check that the marker is (want=true) or is not
// (want=false) in any attribute of any managed resource in state.
func stateMarker(marker string, want bool) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		var found []string
		for addr, rs := range s.RootModule().Resources {
			if rs.Type == "" || strings.HasPrefix(addr, "data.") {
				continue
			}
			for k, v := range rs.Primary.Attributes {
				if strings.Contains(v, marker) {
					found = append(found, addr+"."+k)
				}
			}
		}
		if want && len(found) == 0 {
			return fmt.Errorf("negative control failed: marker not found in state, so the scan cannot detect leaks")
		}
		if !want && len(found) > 0 {
			return fmt.Errorf("credential marker persisted in state at: %s", strings.Join(found, ", "))
		}
		return nil
	}
}

func requestHas(s *writeOnlyStub, method, pathPrefix string, check func(body map[string]any) error) resource.TestCheckFunc {
	return func(*terraform.State) error {
		bodies := s.bodies(method, pathPrefix)
		if len(bodies) == 0 {
			return fmt.Errorf("no %s %s request recorded", method, pathPrefix)
		}
		return check(bodies[len(bodies)-1])
	}
}

func woTestCase(t *testing.T, steps []resource.TestStep) {
	t.Helper()
	t.Setenv("TF_ACC", "1")
	resource.Test(t, resource.TestCase{
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_11_0)},
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps:                    steps,
	})
}

// persisted rewrites a config to use an ordinary (non-ephemeral) variable, which
// Terraform requires for any attribute that is stored in state.
func persisted(cfg string) string {
	return strings.Replace(cfg, "ephemeral = true", "ephemeral = false", 1)
}

func secretVar(v string) config.Variables {
	return config.Variables{"secret": config.StringVariable(v)}
}

// ---------------------------------------------------------------------------
// BYOK
// ---------------------------------------------------------------------------

func byokConfig(serverURL, body string) string {
	return fmt.Sprintf(`
variable "secret" {
  type      = string
  sensitive = true
  ephemeral = true
}
provider "openrouter" {
  api_key    = "sk-or-stub"
  server_url = %q
}
resource "openrouter_byok_key" "test" {
  provider_slug = "openai"
%s
}
`, serverURL, body)
}

func TestStubWriteOnlyByokLifecycle(t *testing.T) {
	stub, srv := newWriteOnlyStub(t)
	const addr = "openrouter_byok_key.test"
	noMarker := resource.ComposeTestCheckFunc(stateMarker(woMarker, false), stateMarker(woMarker2, false),
		resource.TestCheckNoResourceAttr(addr, "key"), resource.TestCheckNoResourceAttr(addr, "key_wo"))
	planChecks := resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{planContainsNoMarker{woMarker}, planContainsNoMarker{woMarker2}}}

	woTestCase(t, []resource.TestStep{
		{
			// Create: the credential reaches the API, never state or plan.
			Config:           byokConfig(srv.URL, "  name = \"wo\"\n  key_wo = var.secret\n  key_wo_version = 1\n"),
			ConfigVariables:  secretVar(woMarker),
			ConfigPlanChecks: planChecks,
			Check: resource.ComposeTestCheckFunc(noMarker,
				resource.TestCheckResourceAttr(addr, "key_wo_version", "1"),
				requestHas(stub, http.MethodPost, "/byok", func(b map[string]any) error {
					if b["key"] != woMarker {
						return fmt.Errorf("create request key = %v, want the credential", b["key"])
					}
					return nil
				})),
		},
		{
			// Metadata-only update with an unchanged version must not resend it.
			PreConfig:        stub.reset,
			Config:           byokConfig(srv.URL, "  name = \"wo-renamed\"\n  key_wo = var.secret\n  key_wo_version = 1\n"),
			ConfigVariables:  secretVar(woMarker),
			ConfigPlanChecks: planChecks,
			Check: resource.ComposeTestCheckFunc(noMarker,
				requestHas(stub, http.MethodPatch, "/byok/", func(b map[string]any) error {
					if _, ok := b["key"]; ok {
						return fmt.Errorf("metadata-only update resent the credential")
					}
					if b["name"] != "wo-renamed" {
						return fmt.Errorf("update request name = %v", b["name"])
					}
					return nil
				})),
		},
		{
			// Changing only the credential does nothing: write-only values
			// cannot diff, so rotation needs a version bump.
			Config:          byokConfig(srv.URL, "  name = \"wo-renamed\"\n  key_wo = var.secret\n  key_wo_version = 1\n"),
			ConfigVariables: secretVar(woMarker2),
			PlanOnly:        true,
		},
		{
			// A failed rotation leaves the old version in state, so the
			// rotation is still pending and the next apply retries it.
			PreConfig: func() {
				stub.mu.Lock()
				stub.failUpdates = true
				stub.mu.Unlock()
			},
			Config:          byokConfig(srv.URL, "  name = \"wo-renamed\"\n  key_wo = var.secret\n  key_wo_version = 2\n"),
			ConfigVariables: secretVar(woMarker2),
			ExpectError:     regexp.MustCompile(`(?i)unexpected response`),
		},
		{
			Config:             byokConfig(srv.URL, "  name = \"wo-renamed\"\n  key_wo = var.secret\n  key_wo_version = 2\n"),
			ConfigVariables:    secretVar(woMarker2),
			PlanOnly:           true,
			ExpectNonEmptyPlan: true,
		},
		{
			// Rotation: a version bump sends the new credential once.
			PreConfig: func() {
				stub.mu.Lock()
				stub.failUpdates = false
				stub.mu.Unlock()
				stub.reset()
			},
			Config:           byokConfig(srv.URL, "  name = \"wo-renamed\"\n  key_wo = var.secret\n  key_wo_version = 2\n"),
			ConfigVariables:  secretVar(woMarker2),
			ConfigPlanChecks: planChecks,
			Check: resource.ComposeTestCheckFunc(noMarker,
				resource.TestCheckResourceAttr(addr, "key_wo_version", "2"),
				requestHas(stub, http.MethodPatch, "/byok/", func(b map[string]any) error {
					if b["key"] != woMarker2 {
						return fmt.Errorf("rotation request key = %v, want the new credential", b["key"])
					}
					return nil
				})),
		},
		{
			// Import needs no credential. The version is config-only, so it
			// is not part of the imported state.
			ResourceName:            addr,
			ImportState:             true,
			ImportStateVerify:       true,
			ImportStateVerifyIgnore: []string{"key_wo_version"},
			ConfigVariables:         secretVar(woMarker2),
			Config:                  byokConfig(srv.URL, "  name = \"wo-renamed\"\n  key_wo = var.secret\n  key_wo_version = 2\n"),
		},
	})
}

// The legacy `key` still works and is persisted. This is the negative control
// for the scan, and the migration test: moving to key_wo updates in place and
// clears the legacy value from state.
func TestStubWriteOnlyByokMigratesFromLegacy(t *testing.T) {
	stub, srv := newWriteOnlyStub(t)
	const addr = "openrouter_byok_key.test"

	woTestCase(t, []resource.TestStep{
		{
			Config:          persisted(byokConfig(srv.URL, "  name = \"legacy\"\n  key = var.secret\n")),
			ConfigVariables: secretVar(woMarker),
			Check: resource.ComposeTestCheckFunc(
				stateMarker(woMarker, true), // control: legacy persists the credential
				resource.TestCheckResourceAttr(addr, "key", woMarker)),
		},
		{
			PreConfig:       stub.reset,
			Config:          byokConfig(srv.URL, "  name = \"legacy\"\n  key_wo = var.secret\n  key_wo_version = 1\n"),
			ConfigVariables: secretVar(woMarker2),
			ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
				plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate), // in place, no replacement
			}},
			Check: resource.ComposeTestCheckFunc(
				stateMarker(woMarker, false), stateMarker(woMarker2, false),
				resource.TestCheckNoResourceAttr(addr, "key"),
				requestHas(stub, http.MethodPatch, "/byok/", func(b map[string]any) error {
					if b["key"] != woMarker2 {
						return fmt.Errorf("migration request key = %v, want the write-only credential", b["key"])
					}
					return nil
				})),
		},
	})
}

func TestStubWriteOnlyByokValidation(t *testing.T) {
	_, srv := newWriteOnlyStub(t)

	woTestCase(t, []resource.TestStep{
		{
			Config:          persisted(byokConfig(srv.URL, "  key = var.secret\n  key_wo = var.secret\n  key_wo_version = 1\n")),
			ConfigVariables: secretVar(woMarker),
			PlanOnly:        true,
			ExpectError:     regexp.MustCompile(`Attribute "key" cannot be specified when "key_wo" is specified`),
		},
		{
			Config:          byokConfig(srv.URL, "  key_wo = var.secret\n"),
			ConfigVariables: secretVar(woMarker),
			PlanOnly:        true,
			ExpectError:     regexp.MustCompile(`key_wo_version`),
		},
		{
			Config:          byokConfig(srv.URL, "  name = \"no-credential\"\n"),
			ConfigVariables: secretVar(woMarker),
			ExpectError:     regexp.MustCompile(`Missing credential`),
		},
	})
}

// ---------------------------------------------------------------------------
// Broadcast
// ---------------------------------------------------------------------------

func destConfig(serverURL, body string) string {
	return fmt.Sprintf(`
variable "secret" {
  type      = string
  sensitive = true
  ephemeral = true
}
provider "openrouter" {
  api_key    = "sk-or-stub"
  server_url = %q
}
resource "openrouter_observability_destination" "test" {
  name    = "tf-stub-wo"
  type    = "webhook"
  enabled = false
%s
}
data "openrouter_observability_destination" "default" {
  id = openrouter_observability_destination.test.id
}
data "openrouter_observability_destination" "filtered" {
  id                       = openrouter_observability_destination.test.id
  include_sensitive_config = false
}
`, serverURL, body)
}

const destAddr = "openrouter_observability_destination.test"

func destWriteOnlyBody(method string, version int) string {
	return fmt.Sprintf(`  config = {
    url    = jsonencode("https://example.com/hook")
    method = jsonencode(%q)
  }
  config_secrets_wo = {
    headers = jsonencode({ Authorization = "Bearer ${var.secret}" })
  }
  config_secrets_wo_version = %d
`, method, version)
}

func TestStubWriteOnlyBroadcastLifecycle(t *testing.T) {
	for name, mask := range map[string]bool{"raw response echo": false, "masked response echo": true} {
		t.Run(name, func(t *testing.T) {
			stub, srv := newWriteOnlyStub(t)
			stub.maskCredentials = mask
			planChecks := resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{planContainsNoMarker{woMarker}, planContainsNoMarker{woMarker2}}}
			noMarker := resource.ComposeTestCheckFunc(stateMarker(woMarker, false), stateMarker(woMarker2, false),
				resource.TestCheckNoResourceAttr(destAddr, "config_secrets_wo"),
				resource.TestCheckNoResourceAttr(destAddr, "webhook.config.headers.Authorization"),
				// Public settings declared in config stay visible.
				resource.TestCheckResourceAttr(destAddr, "webhook.config.url", "https://example.com/hook"),
				resource.TestCheckResourceAttr(destAddr, "webhook.config.method", "POST"))

			woTestCase(t, []resource.TestStep{
				{
					Config:           destConfig(srv.URL, destWriteOnlyBody("POST", 1)),
					ConfigVariables:  secretVar(woMarker),
					ConfigPlanChecks: planChecks,
					Check: resource.ComposeTestCheckFunc(noMarker,
						resource.TestCheckResourceAttr(destAddr, "config_secrets_wo_version", "1"),
						// The credential reached the API, merged into config.
						requestHas(stub, http.MethodPost, "/observability/destinations", func(b map[string]any) error {
							cfg, _ := b["config"].(map[string]any)
							headers, _ := cfg["headers"].(map[string]any)
							if headers["Authorization"] != "Bearer "+woMarker || cfg["url"] != "https://example.com/hook" {
								return fmt.Errorf("create request config = %v, want credentials merged with public settings", cfg)
							}
							return nil
						}),
						// Data sources keep today's behavior by default and
						// drop credentials on request.
						resource.TestCheckNoResourceAttr("data.openrouter_observability_destination.filtered", "webhook.config.headers.Authorization"),
						resource.TestCheckNoResourceAttr("data.openrouter_observability_destination.filtered", "webhook.config.url"),
						resource.TestCheckResourceAttrSet("data.openrouter_observability_destination.default", "webhook.config.url")),
				},
				{
					// Public-only update with an unchanged version: credentials
					// are neither resent nor needed, and the stored ones survive.
					PreConfig:        stub.reset,
					Config:           destConfig(srv.URL, destWriteOnlyBody("PUT", 1)),
					ConfigVariables:  secretVar(woMarker),
					ConfigPlanChecks: planChecks,
					Check: resource.ComposeTestCheckFunc(
						stateMarker(woMarker, false),
						resource.TestCheckResourceAttr(destAddr, "webhook.config.method", "PUT"),
						requestHas(stub, http.MethodPatch, "/observability/destinations/", func(b map[string]any) error {
							cfg, _ := b["config"].(map[string]any)
							if _, ok := cfg["headers"]; ok {
								return fmt.Errorf("public-only update resent credentials: %v", cfg)
							}
							if cfg["method"] != "PUT" {
								return fmt.Errorf("update request config = %v", cfg)
							}
							return nil
						})),
				},
				{
					PreConfig: func() {
						stub.mu.Lock()
						stub.failUpdates = true
						stub.mu.Unlock()
					},
					Config:          destConfig(srv.URL, destWriteOnlyBody("PUT", 2)),
					ConfigVariables: secretVar(woMarker2),
					ExpectError:     regexp.MustCompile(`(?i)unexpected response`),
				},
				{
					// The failed rotation did not record the new version.
					Config:             destConfig(srv.URL, destWriteOnlyBody("PUT", 2)),
					ConfigVariables:    secretVar(woMarker2),
					PlanOnly:           true,
					ExpectNonEmptyPlan: true,
				},
				{
					PreConfig: func() {
						stub.mu.Lock()
						stub.failUpdates = false
						stub.mu.Unlock()
						stub.reset()
					},
					Config:           destConfig(srv.URL, destWriteOnlyBody("PUT", 2)),
					ConfigVariables:  secretVar(woMarker2),
					ConfigPlanChecks: planChecks,
					Check: resource.ComposeTestCheckFunc(noMarkerPUT(), stateMarker(woMarker2, false),
						resource.TestCheckResourceAttr(destAddr, "config_secrets_wo_version", "2"),
						requestHas(stub, http.MethodPatch, "/observability/destinations/", func(b map[string]any) error {
							cfg, _ := b["config"].(map[string]any)
							headers, _ := cfg["headers"].(map[string]any)
							if headers["Authorization"] != "Bearer "+woMarker2 {
								return fmt.Errorf("rotation request config = %v, want the new credential", cfg)
							}
							return nil
						})),
				},
			})
		})
	}
}

func noMarkerPUT() resource.TestCheckFunc {
	return resource.TestCheckResourceAttr(destAddr, "webhook.config.method", "PUT")
}

// Negative control: with the legacy `config`, the same raw echo puts the
// credential in state twice, once in `config` and once in the computed copy.
func TestStubWriteOnlyBroadcastLegacyPersists(t *testing.T) {
	_, srv := newWriteOnlyStub(t)

	woTestCase(t, []resource.TestStep{{
		Config: persisted(destConfig(srv.URL, `  config = {
    url     = jsonencode("https://example.com/hook")
    headers = jsonencode({ Authorization = "Bearer ${var.secret}" })
  }
`)),
		ConfigVariables: secretVar(woMarker),
		Check: resource.ComposeTestCheckFunc(
			stateMarker(woMarker, true),
			resource.TestCheckResourceAttr(destAddr, "webhook.config.headers.Authorization", "Bearer "+woMarker),
			// include_sensitive_config = false drops the computed copy.
			resource.TestCheckNoResourceAttr("data.openrouter_observability_destination.filtered", "webhook.config.headers.Authorization")),
	}})
}

func TestStubWriteOnlyBroadcastValidation(t *testing.T) {
	_, srv := newWriteOnlyStub(t)

	woTestCase(t, []resource.TestStep{
		{
			Config: destConfig(srv.URL, `  config = {
    url     = jsonencode("https://example.com/hook")
    headers = jsonencode({})
  }
  config_secrets_wo = {
    headers = jsonencode({ Authorization = "Bearer ${var.secret}" })
  }
  config_secrets_wo_version = 1
`),
			ConfigVariables: secretVar(woMarker),
			PlanOnly:        true,
			ExpectError:     regexp.MustCompile(`Overlapping configuration key`),
		},
		{
			Config: destConfig(srv.URL, `  config_secrets_wo = {
    headers = jsonencode({ Authorization = "Bearer ${var.secret}" })
  }
`),
			ConfigVariables: secretVar(woMarker),
			PlanOnly:        true,
			ExpectError:     regexp.MustCompile(`config_secrets_wo_version`),
		},
		{
			Config: destConfig(srv.URL, `  config_secrets_wo = {
    headers = "not json ${var.secret}"
  }
  config_secrets_wo_version = 1
`),
			ConfigVariables: secretVar(woMarker),
			PlanOnly:        true,
			ExpectError:     regexp.MustCompile(`Invalid JSON`),
		},
		{
			// A key the API returns in plain text cannot be write-only.
			Config: destConfig(srv.URL, `  config_secrets_wo = {
    username = jsonencode(var.secret)
  }
  config_secrets_wo_version = 1
`),
			ConfigVariables: secretVar(woMarker),
			PlanOnly:        true,
			ExpectError:     regexp.MustCompile(`Public configuration key`),
		},
		{
			Config:          destConfig(srv.URL, ""),
			ConfigVariables: secretVar(woMarker),
			ExpectError:     regexp.MustCompile(`Missing configuration`),
		},
	})
}

// Import starts from an id only, so the first read must not copy the raw
// credential echo into state.
func TestStubWriteOnlyBroadcastImportFiltersRawEcho(t *testing.T) {
	_, srv := newWriteOnlyStub(t)

	woTestCase(t, []resource.TestStep{
		{
			Config:          destConfig(srv.URL, destWriteOnlyBody("POST", 1)),
			ConfigVariables: secretVar(woMarker),
			Check:           stateMarker(woMarker, false),
		},
		{
			Config:          destConfig(srv.URL, destWriteOnlyBody("POST", 1)),
			ConfigVariables: secretVar(woMarker),
			ResourceName:    destAddr,
			ImportState:     true,
			ImportStateCheck: func(states []*terraform.InstanceState) error {
				if len(states) != 1 {
					return fmt.Errorf("imported %d resources, want 1", len(states))
				}
				attrs := states[0].Attributes
				for k, v := range attrs {
					if strings.Contains(v, woMarker) {
						return fmt.Errorf("imported state contains the credential at %s", k)
					}
				}
				if _, ok := attrs["webhook.config.headers.Authorization"]; ok {
					return fmt.Errorf("imported state kept credential headers")
				}
				if attrs["webhook.config.method"] != "POST" {
					return fmt.Errorf("imported state lost public settings: method = %q", attrs["webhook.config.method"])
				}
				return nil
			},
		},
	})
}
