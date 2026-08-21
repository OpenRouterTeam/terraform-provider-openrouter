package acceptance

// DEV-856 deterministic repro / regression coverage against an in-process
// stub of the OpenRouter Management API. Unlike the TestAcc* tests in this
// package these use resource.UnitTest, so they run without TF_ACC and without
// a management key: the provider is pointed at an httptest server
// implementing the documented /observability/destinations endpoints.
//
// They still need a `terraform` binary, so each one calls
// requireTerraformBinary first rather than letting the harness reach out to
// releases.hashicorp.com mid-test.
//
// Run: go test ./internal/acceptance -run TestStubObservabilityDestination -v

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

const stubResourceAddr = "openrouter_observability_destination.test"

/*
 * Placeholder credentials for the S3 variant. AKIAIOSFODNN7EXAMPLE is AWS's
 * own published documentation example; the secret is a self-describing
 * literal. Neither is a credential, and the s3 config fields are Sensitive in
 * the schema, so Terraform masks them in all plan output.
 */
const (
	stubS3AccessKeyID     = "AKIAIOSFODNN7EXAMPLE"
	stubS3SecretAccessKey = "EXAMPLE-NOT-A-REAL-SECRET"
	stubProviderAPIKey    = "stub-api-key-not-a-credential"
)

// requireTerraformBinary skips rather than letting the harness download a
// Terraform release, so a network-restricted runner reports "skipped for a
// stated reason" instead of an opaque failure.
func requireTerraformBinary(t *testing.T) {
	t.Helper()
	if os.Getenv("TF_ACC_TERRAFORM_PATH") != "" {
		return
	}
	if _, err := exec.LookPath("terraform"); err != nil {
		t.Skip("terraform binary not on PATH and TF_ACC_TERRAFORM_PATH unset; stub tests need a local Terraform")
	}
}

//#region stub Management API

// stubAPI is an in-process stand-in for the /observability/destinations
// endpoints. Tests keep the returned value so they can mutate the stored
// destination out of band between refreshes.
type stubAPI struct {
	url string

	mu   sync.Mutex
	body map[string]any

	// createTransform models normalization the API applies when echoing a
	// newly created destination; readTransform models normalization applied
	// to every later GET/PATCH response. Either may be nil.
	createTransform func(map[string]any)
	readTransform   func(map[string]any)
}

type stubOpts struct {
	createTransform func(map[string]any)
	readTransform   func(map[string]any)
}

func newStubAPI(t *testing.T, opts stubOpts) *stubAPI {
	t.Helper()
	s := &stubAPI{
		createTransform: opts.createTransform,
		readTransform:   opts.readTransform,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/observability/destinations", s.handleCollection)
	mux.HandleFunc("/observability/destinations/", s.handleItem)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	s.url = srv.URL
	return s
}

func (s *stubAPI) handleCollection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	req, ok := decodeBody(w, r)
	if !ok {
		return
	}

	s.mu.Lock()
	req["id"] = "dest_stub_1"
	req["created_at"] = "2026-08-19T00:00:00Z"
	req["updated_at"] = "2026-08-19T00:00:00Z"
	if _, ok := req["workspace_id"]; !ok {
		req["workspace_id"] = "ws_stub"
	}
	s.body = req
	s.mu.Unlock()

	s.write(w, http.StatusCreated, s.createTransform)
}

func (s *stubAPI) handleItem(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.write(w, http.StatusOK, s.readTransform)
	case http.MethodDelete:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	case http.MethodPut, http.MethodPatch:
		req, ok := decodeBody(w, r)
		if !ok {
			return
		}
		s.mu.Lock()
		for k, v := range req {
			s.body[k] = v
		}
		s.body["updated_at"] = "2026-08-19T00:01:00Z"
		s.mu.Unlock()
		s.write(w, http.StatusOK, s.readTransform)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func decodeBody(w http.ResponseWriter, r *http.Request) (map[string]any, bool) {
	b, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return nil, false
	}
	var req map[string]any
	if err := json.Unmarshal(b, &req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return nil, false
	}
	return req, true
}

// write renders the stored destination through transform and emits it under
// the documented {"data": ...} envelope.
func (s *stubAPI) write(w http.ResponseWriter, status int, transform func(map[string]any)) {
	s.mu.Lock()
	out := make(map[string]any, len(s.body))
	for k, v := range s.body {
		out[k] = v
	}
	if cfg, ok := out["config"].(map[string]any); ok {
		cfgCopy := make(map[string]any, len(cfg))
		for k, v := range cfg {
			cfgCopy[k] = v
		}
		out["config"] = cfgCopy
	}
	s.mu.Unlock()

	if transform != nil {
		transform(out)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"data": out})
}

// setRemoteConfigKey mutates the stored destination the way a console edit or
// another IaC run would: the next GET reports a config Terraform never wrote.
func (s *stubAPI) setRemoteConfigKey(t *testing.T, key string, value any) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, ok := s.body["config"].(map[string]any)
	if !ok {
		t.Fatalf("stub has no stored config to mutate (body keys: %v)", mapKeys(s.body))
	}
	cfg[key] = value
}

func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

//#endregion

//#region configs

func stubProviderBlock(serverURL string) string {
	return fmt.Sprintf(`
provider "openrouter" {
  api_key    = %q
  server_url = %q
}
`, stubProviderAPIKey, serverURL)
}

func stubWebhookConfig(serverURL, url string) string {
	return stubProviderBlock(serverURL) + fmt.Sprintf(`
resource "openrouter_observability_destination" "test" {
  name    = "tf-stub-diff"
  type    = "webhook"
  enabled = false
  config = {
    url    = jsonencode(%q)
    method = jsonencode("POST")
  }
}
`, url)
}

// stubS3Config is the customer-shaped S3 destination from DEV-856: only the
// four fields the API requires, with prefix/pathTemplate deliberately left
// unset so the response's defaulted values have somewhere to leak into.
func stubS3Config(serverURL string) string {
	return stubProviderBlock(serverURL) + fmt.Sprintf(`
resource "openrouter_observability_destination" "test" {
  name    = "tf-stub-s3"
  type    = "s3"
  enabled = false
  config = {
    bucketName      = jsonencode("openrouter-traces-example")
    region          = jsonencode("us-east-1")
    accessKeyId     = jsonencode(%q)
    secretAccessKey = jsonencode(%q)
  }
}
`, stubS3AccessKeyID, stubS3SecretAccessKey)
}

//#endregion

// TestStubObservabilityDestinationPerpetualDiff applies a minimal webhook
// destination against a stub that normalizes its READ responses (trailing
// slash on the url, an injected empty headers map) and asserts the plan after
// apply is empty. Read does not refresh the flat `config` map, so the
// normalization never reaches state and the plan stays clean. This is the
// baseline the DEV-856 repro has to be measured against: read-side
// normalization on its own does not produce a perpetual diff.
func TestStubObservabilityDestinationPerpetualDiff(t *testing.T) {
	requireTerraformBinary(t)

	srv := newStubAPI(t, stubOpts{
		readTransform: func(out map[string]any) {
			if _, ok := out["filter_rules"]; !ok {
				out["filter_rules"] = map[string]any{"enabled": true, "groups": []any{}}
			}
			if cfg, ok := out["config"].(map[string]any); ok {
				if u, ok := cfg["url"].(string); ok {
					cfg["url"] = u + "/"
				}
				cfg["headers"] = map[string]any{}
			}
		},
	})

	config := stubWebhookConfig(srv.url, "https://example.com/tf-stub")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check:  resource.TestCheckResourceAttrSet(stubResourceAddr, "id"),
			},
			{
				Config:   config,
				PlanOnly: true,
			},
		},
	})
}

// TestStubObservabilityDestinationS3ImmediatePlanAfterCreate is the
// customer-shaped DEV-856 scenario, and it guards the invariant that broke
// when a candidate fix refreshed the flat `config` map from the create
// response: state must contain exactly the keys the configuration declares.
//
// The stub echoes the create request verbatim — no API-side normalization at
// all. The SDK still materializes `prefix` and `pathTemplate` on the typed S3
// config from its `default:` struct tags, so any code that serializes that
// struct back into the flat map writes two keys the practitioner never wrote.
// `config` is Required and not Computed, so Terraform rejects the apply
// outright with "Provider produced inconsistent result after apply".
//
// The assertions are deliberately key-level rather than plan-level: they name
// the two non-sensitive keys that must not appear, so a failure is legible
// without printing any config value.
func TestStubObservabilityDestinationS3ImmediatePlanAfterCreate(t *testing.T) {
	requireTerraformBinary(t)

	srv := newStubAPI(t, stubOpts{})
	config := stubS3Config(srv.url)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(stubResourceAddr, "id"),
					resource.TestCheckResourceAttr(stubResourceAddr, "type", "s3"),
					// State must hold exactly the four keys the config
					// declares; anything the response contributed is drift.
					resource.TestCheckResourceAttr(stubResourceAddr, "config.%", "4"),
					resource.TestCheckNoResourceAttr(stubResourceAddr, "config.prefix"),
					resource.TestCheckNoResourceAttr(stubResourceAddr, "config.pathTemplate"),
				),
			},
			{
				Config:   config,
				PlanOnly: true,
			},
		},
	})
}

// TestStubObservabilityDestinationCreateResponseNormalization isolates the
// Create call site against genuine API-side normalization: the stub rewrites
// the webhook url in its create response only. Whatever the provider stores
// for `config` after create, the immediately following plan must be empty —
// a just-applied configuration that re-plans is the DEV-856 symptom.
func TestStubObservabilityDestinationCreateResponseNormalization(t *testing.T) {
	requireTerraformBinary(t)

	srv := newStubAPI(t, stubOpts{
		createTransform: func(out map[string]any) {
			if cfg, ok := out["config"].(map[string]any); ok {
				if u, ok := cfg["url"].(string); ok {
					cfg["url"] = u + "/"
				}
			}
		},
	})

	config := stubWebhookConfig(srv.url, "https://example.com/tf-stub-create-norm")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check:  resource.TestCheckResourceAttrSet(stubResourceAddr, "id"),
			},
			{
				Config:   config,
				PlanOnly: true,
			},
		},
	})
}

// TestStubObservabilityDestinationUpdateConvergence isolates the Update
// lifecycle stage: an in-place config change must apply and then re-plan
// empty, even while the API normalizes its read responses. Update converges
// on the plan's config without consulting the update response, so no
// response-driven refresh is needed here.
func TestStubObservabilityDestinationUpdateConvergence(t *testing.T) {
	requireTerraformBinary(t)

	srv := newStubAPI(t, stubOpts{
		readTransform: func(out map[string]any) {
			if cfg, ok := out["config"].(map[string]any); ok {
				cfg["headers"] = map[string]any{}
			}
		},
	})

	initial := stubWebhookConfig(srv.url, "https://example.com/tf-stub-before")
	updated := stubWebhookConfig(srv.url, "https://example.com/tf-stub-after")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: initial,
				Check:  resource.TestCheckResourceAttrSet(stubResourceAddr, "id"),
			},
			{
				Config: updated,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(stubResourceAddr, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.TestCheckResourceAttr(
					stubResourceAddr, "config.url", `"https://example.com/tf-stub-after"`,
				),
			},
			{
				Config:   updated,
				PlanOnly: true,
			},
		},
	})
}

// TestStubObservabilityDestinationReadDetectsOutOfBandChange replaces the
// placeholder "out-of-band" step that previously only described the scenario
// in a comment. Here the stub's stored destination really is mutated between
// refreshes, so the next GET reports a config Terraform never wrote.
//
// Read is the only lifecycle stage that can notice: it is where remote state
// is reconciled. The refreshed plan is empty today, so the provider silently
// reports a resource as converged while the remote config differs.
//
// This is skipped as a known unfixed defect rather than deleted, so the gap
// stays visible in every run log. Naively refreshing the flat map from the
// Get response does NOT fix it: the SDK's `default:` struct tags synthesize
// config keys the practitioner never wrote (prefix, pathTemplate on s3), so
// the refresh reintroduces the DEV-856 perpetual diff. Any real fix has to
// reconcile only keys the configuration actually declares.
func TestStubObservabilityDestinationReadDetectsOutOfBandChange(t *testing.T) {
	t.Skip("DEV-856 follow-up: Read never refreshes the flat `config` map, so out-of-band config edits are invisible to plan; needs its own issue before a fix lands — https://linear.app/openrouter/issue/DEV-856")

	requireTerraformBinary(t)

	srv := newStubAPI(t, stubOpts{})
	config := stubWebhookConfig(srv.url, "https://example.com/tf-stub-oob")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check:  resource.TestCheckResourceAttrSet(stubResourceAddr, "id"),
			},
			{
				// Somebody changes the destination in the console.
				PreConfig: func() {
					srv.setRemoteConfigKey(t, "url", "https://evil.example.com/exfil")
				},
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectNonEmptyPlan(),
						plancheck.ExpectResourceAction(stubResourceAddr, plancheck.ResourceActionUpdate),
					},
				},
			},
		},
	})
}

// TestStubObservabilityDestinationImport is skipped for a tracked product bug
// rather than papered over with ImportStateVerifyIgnore, which would turn a
// real defect into a silent pass. The generated Read mapper never assigns
// r.Type, so an imported destination carries no `type` at all and
// ImportStateVerify fails structurally — independently of anything #204 does
// to the flat `config` map.
func TestStubObservabilityDestinationImport(t *testing.T) {
	t.Skip("DEV-872: generated Read mapper never assigns 'type', so import produces incomplete state and ImportStateVerify fails; remove this skip when DEV-872 lands — https://linear.app/openrouter/issue/DEV-872")

	requireTerraformBinary(t)

	srv := newStubAPI(t, stubOpts{})
	config := stubWebhookConfig(srv.url, "https://example.com/tf-stub-import")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check:  resource.TestCheckResourceAttrSet(stubResourceAddr, "id"),
			},
			{
				ResourceName:      stubResourceAddr,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}
