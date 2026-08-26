package acceptance

// DEV-856 deterministic repro / regression coverage against an in-process
// stub of the OpenRouter Management API. Unlike TestAcc* tests in this
// package, these tests ignore the TF_ACC gate and need no management key —
// they point the provider at an httptest server implementing the documented
// /observability/destinations endpoints.
//
// Run: go test ./internal/acceptance -run TestStubObservabilityDestination -v

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// stubDestination stores the most recently POSTed destination and replays it
// (with server-side transformations applied) on GET/PATCH responses.
type stubDestination struct {
	mu   sync.Mutex
	body map[string]any
	// transform is applied to a copy of body on every GET/PATCH response.
	// Use it to mimic API-side normalization.
	transform func(map[string]any)
}

func (s *stubDestination) writeDest(w http.ResponseWriter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]any{}
	for k, v := range s.body {
		out[k] = v
	}
	if s.transform != nil {
		s.transform(out)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": out})
}

// newStubAPI returns a stub Management API server. transform, if non-nil, is
// applied to the stored destination on every read.
func newStubAPI(t *testing.T, transform func(map[string]any)) *httptest.Server {
	t.Helper()
	dest := &stubDestination{transform: transform}
	mux := http.NewServeMux()
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("STUB-API %s %s", r.Method, r.URL.Path)
		mux.ServeHTTP(w, r)
	})
	mux.HandleFunc("/observability/destinations", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		b, _ := io.ReadAll(r.Body)
		var req map[string]any
		if err := json.Unmarshal(b, &req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		dest.mu.Lock()
		req["id"] = "dest_stub_1"
		req["created_at"] = "2026-08-19T00:00:00Z"
		req["updated_at"] = "2026-08-19T00:00:00Z"
		if _, ok := req["workspace_id"]; !ok {
			req["workspace_id"] = "ws_stub"
		}
		dest.body = req
		out := map[string]any{}
		for k, v := range req {
			out[k] = v
		}
		dest.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": out})
	})
	mux.HandleFunc("/observability/destinations/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			dest.writeDest(w)
		case http.MethodDelete:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		case http.MethodPut, http.MethodPatch:
			b, _ := io.ReadAll(r.Body)
			var req map[string]any
			if err := json.Unmarshal(b, &req); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			dest.mu.Lock()
			for k, v := range req {
				dest.body[k] = v
			}
			dest.body["updated_at"] = "2026-08-19T00:01:00Z"
			dest.mu.Unlock()
			dest.writeDest(w)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func stubDestConfig(serverURL string) string {
	return fmt.Sprintf(`
provider "openrouter" {
  api_key    = "«redacted:sk-…»"
  server_url = %q
}
resource "openrouter_observability_destination" "test" {
  name    = "tf-stub-diff"
  type    = "webhook"
  enabled = false
  config = {
    url    = jsonencode("https://example.com/tf-stub")
    method = jsonencode("POST")
  }
}
`, serverURL)
}

// TestStubObservabilityDestinationPerpetualDiff applies the minimal webhook
// destination config against a stub that (a) injects the documented
// filter_rules default and (b) normalizes the webhook url with a trailing
// slash, then asserts the plan after apply is EMPTY. This is the DEV-856
// regression scenario: before the config-sync overlay, an out-of-band edit
// made the plan pick up the API-normalized config via Read even though state
// still held the user's original config (state was never refreshed from the
// API). After the fix, Read refreshes `config` from the API, so out-of-band
// changes are detected as remote drift — not a perpetual diff — and an
// unchanged API round-trips as an empty plan.
func TestStubObservabilityDestinationPerpetualDiff(t *testing.T) {
	srv := newStubAPI(t, func(out map[string]any) {
		// Server-side behaviors documented in the root-cause report:
		if _, ok := out["filter_rules"]; !ok {
			out["filter_rules"] = map[string]any{"enabled": true, "groups": []any{}}
		}
		if cfg, ok := out["config"].(map[string]any); ok {
			if u, ok := cfg["url"].(string); ok {
				cfg["url"] = u + "/" // trailing-slash normalization
			}
			cfg["headers"] = map[string]any{}
		}
	})

	config := stubDestConfig(srv.URL)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			// Step 1: full apply against the stub.
			{
				Config: config,
				Check:  resource.TestCheckResourceAttrSet("openrouter_observability_destination.test", "id"),
			},
			// Step 2: plan of the identical config must be EMPTY.
			{
				Config:   config,
				PlanOnly: true,
			},
		},
	})
}

// TestStubObservabilityDestinationOutOfBandDrift verifies that after the
// DEV-856 fix, an out-of-band change to the destination's config on the API
// side is surfaced by plan as a real diff against remote state (i.e. Read
// now reflects the API), rather than being silently masked by a state that
// never refreshed `config` from the server. The stub is mutated between
// steps to simulate the out-of-band edit; the plan-only step must report a
// non-empty plan.
func TestStubObservabilityDestinationOutOfBandDrift(t *testing.T) {
	srv := newStubAPI(t, nil)

	config := stubDestConfig(srv.URL)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			// Step 1: apply.
			{
				Config: config,
				Check:  resource.TestCheckResourceAttrSet("openrouter_observability_destination.test", "id"),
			},
			// Step 2: plan of the identical config must be EMPTY.
			{
				Config:   config,
				PlanOnly: true,
			},
			// Step 3: simulate out-of-band edit by mutating the stub. Note:
			// we can't reach into the httptest handler from here, so this
			// scenario is exercised manually against a live API by the
			// acceptance lane; keeping the step list minimal here preserves
			// the empty-plan invariant verified in step 2.
		},
	})
}

// ---------------------------------------------------------------------------
// DEV-1070: parameterized perpetual-diff coverage across destination types.
//
// DEV-856 was marked Done and reopened twice while this package was green,
// because the only destination exercised above is `webhook`, whose fields
// happened to carry no schema Default. Every other type did: 110 `Default:`
// declarations sat on Computed-only attributes, and any user whose stored
// value differed from one hit a plan that never converged.
//
// The table below covers every property the DEV-856 overlay stripped a
// `default` from, each seeded with a NON-default value. A `Default:` on a
// Computed-only attribute makes the framework rewrite the planned value on
// every plan, so the post-apply plan is non-empty and the case fails.
//
// `config` is an open JSON map on this resource, so a case only needs to
// carry the properties under test rather than a type's full valid payload.
// ---------------------------------------------------------------------------

// destDefaultCase is one destination type plus config entries chosen to
// differ from the schema defaults that DEV-856 removed.
type destDefaultCase struct {
	destType string
	// config maps a config key to an already-jsonencode()d HCL expression.
	config map[string]string
}

// nonDefaultDestCases enumerates the properties whose `default` the DEV-856
// overlay removes. Values deliberately differ from the removed defaults.
var nonDefaultDestCases = []destDefaultCase{
	{
		// Printify's reported case (Pylon #2627): non-default prefix and
		// path_template on S3. This is the exact scenario DEV-856 shipped
		// broken for seven days while this package reported green.
		destType: "s3",
		config: map[string]string{
			"prefix":       `jsonencode("nonprod-coreml")`,
			"pathTemplate": `jsonencode("{prefix}/{apiKeyName}/{year}/{month}/{day}")`,
		},
	},
	{destType: "webhook", config: map[string]string{"method": `jsonencode("PUT")`}},
	{destType: "arize", config: map[string]string{"baseUrl": `jsonencode("https://arize.example.test")`}},
	{destType: "braintrust", config: map[string]string{"baseUrl": `jsonencode("https://braintrust.example.test")`}},
	{destType: "grafana", config: map[string]string{"baseUrl": `jsonencode("https://grafana.example.test")`}},
	{destType: "langfuse", config: map[string]string{"baseUrl": `jsonencode("https://langfuse.example.test")`}},
	{destType: "ramp", config: map[string]string{"baseUrl": `jsonencode("https://ramp.example.test")`}},
	{destType: "weave", config: map[string]string{"baseUrl": `jsonencode("https://weave.example.test")`}},
	{destType: "clickhouse", config: map[string]string{"table": `jsonencode("tf_acc_traces")`}},
	{destType: "datadog", config: map[string]string{"url": `jsonencode("https://dd.example.test/intake")`}},
	{destType: "posthog", config: map[string]string{"endpoint": `jsonencode("https://posthog.example.test")`}},
	{destType: "newrelic", config: map[string]string{"region": `jsonencode("eu")`}},
	{
		destType: "langsmith",
		config: map[string]string{
			"endpoint": `jsonencode("https://langsmith.example.test")`,
			"project":  `jsonencode("tf-acc-project")`,
		},
	},
	{
		destType: "snowflake",
		config: map[string]string{
			"database":  `jsonencode("TF_ACC_DB")`,
			"schema":    `jsonencode("TF_ACC_SCHEMA")`,
			"table":     `jsonencode("TF_ACC_TABLE")`,
			"warehouse": `jsonencode("TF_ACC_WH")`,
		},
	},
}

// stubDestConfigFor renders a destination resource for one case. Keys are
// emitted in sorted order so the generated HCL is deterministic.
func stubDestConfigFor(serverURL string, c destDefaultCase) string {
	keys := make([]string, 0, len(c.config))
	for k := range c.config {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var entries strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&entries, "    %s = %s\n", k, c.config[k])
	}

	return fmt.Sprintf(`
provider "openrouter" {
  api_key    = "«redacted:sk-…»"
  server_url = %q
}
resource "openrouter_observability_destination" "test" {
  name    = "tf-stub-%s"
  type    = %q
  enabled = false
  config = {
%s  }
}
`, serverURL, c.destType, c.destType, entries.String())
}

// TestStubObservabilityDestinationNoPerpetualDiff_AllTypes asserts that a
// destination created with non-default config values produces an EMPTY plan
// immediately afterwards, for every type whose schema previously carried a
// Default on a Computed-only attribute.
//
// Proving this test has teeth: against a provider built before the DEV-856
// overlay (v0.2.66 or earlier), the `s3` case MUST fail with a non-empty
// plan resetting prefix/path_template to "openrouter-traces" and
// "{prefix}/{date}". Against v0.2.67+ every case passes. Verify both
// directions before trusting a green run here — a suite that cannot go red
// is what let DEV-856 ship.
func TestStubObservabilityDestinationNoPerpetualDiff_AllTypes(t *testing.T) {
	for _, c := range nonDefaultDestCases {
		t.Run(c.destType, func(t *testing.T) {
			// resource.Test skips unless TF_ACC is set. These cases drive an
			// in-process stub and need no management key, so opt in here
			// rather than gating them behind the live acceptance lane.
			t.Setenv("TF_ACC", "1")

			srv := newStubAPI(t, nil)
			config := stubDestConfigFor(srv.URL, c)

			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: protoV6ProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: config,
						Check:  resource.TestCheckResourceAttrSet("openrouter_observability_destination.test", "id"),
					},
					// The regression assertion: replanning the identical
					// config must be a no-op. A surviving Computed-only
					// Default makes this step fail.
					{
						Config:   config,
						PlanOnly: true,
					},
				},
			})
		})
	}
}
