package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

type byokKeyFixture struct {
	mu      sync.Mutex
	stored  map[string]any
	patches []map[string]any
	server  *httptest.Server
}

func newByokKeyFixture(t *testing.T, failClosed bool) *byokKeyFixture {
	t.Helper()
	fixture := &byokKeyFixture{}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /byok", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode BYOK create: %v", err)
			w.WriteHeader(400)
			return
		}
		fixture.mu.Lock()
		defer fixture.mu.Unlock()
		disabled, present := body["disabled"]
		if !present {
			disabled = false
		}
		fixture.stored = map[string]any{
			"id": "11111111-1111-1111-1111-111111111111", "provider": "openai",
			"workspace_id": "ws-local", "label": "local", "name": "nullable",
			"disabled": disabled, "is_fallback": false, "is_required": false, "is_byok_only": false,
			"declared_zdr": nil, "declared_region": nil, "allowed_user_ids": nil,
			"sort_order": 0, "created_at": "2026-01-01T00:00:00Z",
			"allowed_models": body["allowed_models"], "allowed_api_key_hashes": body["allowed_api_key_hashes"],
		}
		writeJSON(w, http.StatusCreated, map[string]any{"data": fixture.stored})
	})
	mux.HandleFunc("/byok/11111111-1111-1111-1111-111111111111", func(w http.ResponseWriter, r *http.Request) {
		fixture.mu.Lock()
		defer fixture.mu.Unlock()
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, 200, map[string]any{"data": fixture.stored})
		case http.MethodPatch:
			var patch map[string]any
			if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
				t.Errorf("decode BYOK update: %v", err)
				w.WriteHeader(400)
				return
			}
			preview := make(map[string]any, len(fixture.stored))
			for key, value := range fixture.stored {
				preview[key] = value
			}
			// Omitted PATCH fields preserve the API's stored restrictions.
			for key, value := range patch {
				if key != "key" {
					preview[key] = value
				}
			}
			if failClosed {
				hashes, _ := preview["allowed_api_key_hashes"].([]any)
				if len(hashes) == 0 {
					if preview["disabled"] != true {
						t.Errorf("unsafe BYOK PATCH %v: unrestricted credential would remain enabled", patch)
						http.Error(w, "unrestricted BYOK credential must be disabled", http.StatusBadRequest)
						return
					}
					previousHashes, _ := fixture.stored["allowed_api_key_hashes"].([]any)
					if len(previousHashes) > 0 {
						disabled, hasDisabled := patch["disabled"]
						hashes, hasHashes := patch["allowed_api_key_hashes"]
						if !hasDisabled || disabled != true || !hasHashes || hashes != nil {
							t.Errorf("unsafe BYOK PATCH %v: clearing hashes requires explicit disabled=true and allowed_api_key_hashes=null together", patch)
							http.Error(w, "BYOK hash clearing must atomically disable the credential", http.StatusBadRequest)
							return
						}
					}
				}
			}
			fixture.stored = preview
			fixture.patches = append(fixture.patches, patch)
			writeJSON(w, 200, map[string]any{"data": fixture.stored})
		case http.MethodDelete:
			writeJSON(w, 200, map[string]any{"deleted": true})
		default:
			t.Errorf("unexpected BYOK method %s", r.Method)
			w.WriteHeader(405)
		}
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected BYOK request %s %s", r.Method, r.URL.Path)
		w.WriteHeader(500)
	})
	fixture.server = httptest.NewServer(mux)
	t.Cleanup(fixture.server.Close)
	t.Cleanup(func() {
		fixture.mu.Lock()
		defer fixture.mu.Unlock()
		t.Logf("persisted BYOK restrictions after Terraform: allowed_models=%v allowed_api_key_hashes=%v PATCH count=%d", fixture.stored["allowed_models"], fixture.stored["allowed_api_key_hashes"], len(fixture.patches))
	})
	return fixture
}

func TestByokKey_ClearsNullableRestrictions(t *testing.T) {
	for _, field := range []string{"allowed_models", "allowed_api_key_hashes"} {
		t.Run(field, func(t *testing.T) {
			fixture := newByokKeyFixture(t, false)
			config := func(clear bool) string {
				models, hashes := `["openai/gpt-4o"]`, `["local-key-hash"]`
				if clear {
					if field == "allowed_models" {
						models = "null"
					} else {
						hashes = "null"
					}
				}
				return fmt.Sprintf(`
provider "openrouter" {
  api_key = "local-dummy-token"
  server_url = %q
}
resource "openrouter_byok_key" "this" {
  provider_slug = "openai"
  key = "local-dummy-credential"
  name = "nullable"
  allowed_models = %s
  allowed_api_key_hashes = %s
}
`, fixture.server.URL, models, hashes)
			}
			checkStored := func(clear bool) resource.TestCheckFunc {
				return func(*terraform.State) error {
					fixture.mu.Lock()
					defer fixture.mu.Unlock()
					want := map[string]any{
						"allowed_models":         []any{"openai/gpt-4o"},
						"allowed_api_key_hashes": []any{"local-key-hash"},
					}
					if clear {
						want[field] = nil
						sentNull := false
						for _, patch := range fixture.patches {
							if value, sent := patch[field]; sent && value == nil {
								sentNull = true
							}
						}
						if !sentNull {
							return fmt.Errorf("BYOK clear %s: want explicit null in a successful PATCH", field)
						}
					}
					for key, value := range want {
						if !reflect.DeepEqual(fixture.stored[key], value) {
							return fmt.Errorf("persisted BYOK %s = %v, want %v", key, fixture.stored[key], value)
						}
					}
					return nil
				}
			}
			const addr = "openrouter_byok_key.this"
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: testProviderFactories(),
				Steps: []resource.TestStep{
					{Config: config(false), Check: checkStored(false)},
					{
						Config: config(true),
						ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
							plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate),
						}},
						Check: resource.ComposeAggregateTestCheckFunc(checkStored(true), resource.TestCheckNoResourceAttr(addr, field+".#")),
					},
					{RefreshState: true, Check: checkStored(true)},
					{Config: config(true), PlanOnly: true, ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}},
				},
			})
		})
	}
}

func TestByokKey_FailClosedDisabledLifecycle(t *testing.T) {
	fixture := newByokKeyFixture(t, true)
	const addr = "openrouter_byok_key.this"
	createDisabled := fmt.Sprintf(`
provider "openrouter" {
  api_key = "local-dummy-token"
  server_url = %q
}
resource "openrouter_byok_key" "this" {
  provider_slug = "openai"
  key = "local-dummy-credential"
  name = "nullable"
  disabled = true
  allowed_api_key_hashes = null
}
`, fixture.server.URL)
	enableRestricted := fmt.Sprintf(`
provider "openrouter" {
  api_key = "local-dummy-token"
  server_url = %q
}
resource "openrouter_byok_key" "this" {
  provider_slug = "openai"
  key = "local-dummy-credential"
  name = "nullable"
  disabled = false
  allowed_api_key_hashes = ["local-key-hash"]
}
`, fixture.server.URL)
	disableAndClear := createDisabled
	checkStored := func(disabled bool, hashes any) resource.TestCheckFunc {
		return func(*terraform.State) error {
			fixture.mu.Lock()
			defer fixture.mu.Unlock()
			if fixture.stored["disabled"] != disabled {
				return fmt.Errorf("persisted BYOK disabled = %v, want %v", fixture.stored["disabled"], disabled)
			}
			if !reflect.DeepEqual(fixture.stored["allowed_api_key_hashes"], hashes) {
				return fmt.Errorf("persisted BYOK allowed_api_key_hashes = %v, want %v", fixture.stored["allowed_api_key_hashes"], hashes)
			}
			return nil
		}
	}
	checkAtomicClear := func(*terraform.State) error {
		fixture.mu.Lock()
		defer fixture.mu.Unlock()
		for _, patch := range fixture.patches {
			disabled, hasDisabled := patch["disabled"]
			hashes, hasHashes := patch["allowed_api_key_hashes"]
			if hasDisabled && disabled == true && hasHashes && hashes == nil {
				return nil
			}
		}
		return fmt.Errorf("BYOK clear: want a successful PATCH containing explicit disabled=true and allowed_api_key_hashes=null")
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{Config: createDisabled, Check: checkStored(true, nil)},
			{RefreshState: true, Check: checkStored(true, nil)},
			{Config: createDisabled, PlanOnly: true, ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}},
			{
				Config: enableRestricted,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate),
				}},
				Check: checkStored(false, []any{"local-key-hash"}),
			},
			{RefreshState: true, Check: checkStored(false, []any{"local-key-hash"})},
			{Config: enableRestricted, PlanOnly: true, ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}},
			{
				Config: disableAndClear,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(checkStored(true, nil), checkAtomicClear, resource.TestCheckNoResourceAttr(addr, "allowed_api_key_hashes.#")),
			},
			{RefreshState: true, Check: resource.ComposeAggregateTestCheckFunc(checkStored(true, nil), checkAtomicClear)},
			{Config: disableAndClear, PlanOnly: true, ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}},
		},
	})
}
