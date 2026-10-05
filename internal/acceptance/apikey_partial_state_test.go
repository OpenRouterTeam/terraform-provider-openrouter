package acceptance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync/atomic"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestStubAPIKeyCreatePreservesStateWhenUpdateFails(t *testing.T) {
	t.Setenv("TF_ACC", "1")

	var deletes atomic.Int32
	keyData := map[string]any{
		"byok_usage": 0, "byok_usage_daily": 0, "byok_usage_monthly": 0, "byok_usage_weekly": 0,
		"created_at": "2026-08-25T00:00:00Z", "creator_user_id": nil, "disabled": false,
		"expires_at": nil, "external_user": nil, "hash": "hash_stub_1", "include_byok_in_limit": false,
		"label": "sk-or-v1-stub", "limit": 0, "limit_remaining": 0, "limit_reset": nil,
		"name": "tf-stub-key", "updated_at": nil, "usage": 0, "usage_daily": 0,
		"usage_monthly": 0, "usage_weekly": 0, "workspace_id": "ws_stub",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/keys":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": keyData, "key": "sk-or-v1-secret"})
		case r.Method == http.MethodPatch && r.URL.Path == "/keys/hash_stub_1":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":400,"message":"forced update failure"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/keys/hash_stub_1":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": keyData})
		case r.Method == http.MethodDelete && r.URL.Path == "/keys/hash_stub_1":
			deletes.Add(1)
			_, _ = w.Write([]byte(`{"deleted":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{{
			Config: fmt.Sprintf(`
provider "openrouter" {
  api_key    = "sk-or-mgmt-stub"
  server_url = %q
}
resource "openrouter_api_key" "test" {
  name     = "tf-stub-key"
  limit    = 0
  disabled = true
}
`, srv.URL),
			ExpectError: regexp.MustCompile(`unexpected response code 400`),
		}},
	})

	if got := deletes.Load(); got != 1 {
		t.Fatalf("expected Terraform to retain and destroy the partially created key, got %d deletes", got)
	}
}
