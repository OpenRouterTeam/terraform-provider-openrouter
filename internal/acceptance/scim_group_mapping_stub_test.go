package acceptance

// Offline regression coverage for the SCIM group mapping destroy path.
//
// The Management API requires the keep_members query parameter on
// DELETE /scim/group-mappings/{id} and returns 400 when it is absent, so a
// provider that does not send it can never destroy the resource. This test
// points the provider at an in-process stub that enforces the same contract,
// so it needs no management key and no network access.
//
// Run: go test ./internal/acceptance -run TestStubScimGroupMapping -v

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// scimGroupMappingStub answers like the live API: the mapping exists until a
// DELETE carries keep_members, and a DELETE without it is a 400.
type scimGroupMappingStub struct {
	mu            sync.Mutex
	mapping       map[string]any
	deleted       bool
	deleteQueries []url.Values
}

func (s *scimGroupMappingStub) snapshot() (map[string]any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]any{}
	for k, v := range s.mapping {
		out[k] = v
	}
	return out, s.deleted
}

// newScimGroupMappingStub returns a stub of the documented
// /scim/group-mappings surface.
func newScimGroupMappingStub(t *testing.T) (*httptest.Server, *scimGroupMappingStub) {
	t.Helper()
	s := &scimGroupMappingStub{mapping: map[string]any{
		"id":              "sgm_stub_1",
		"organization_id": "org_stub",
		"scim_group_id":   "group_stub_1",
		"workspace_id":    "ws_stub_1",
		"role":            "member",
		"created_at":      "2026-11-02T00:00:00Z",
		"updated_at":      "2026-11-02T00:00:00Z",
	}}

	mux := http.NewServeMux()
	mux.HandleFunc("/scim/group-mappings", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		s.mu.Lock()
		for _, k := range []string{"role", "scim_group_id", "workspace_id"} {
			if v, ok := req[k]; ok {
				s.mapping[k] = v
			}
		}
		out := map[string]any{}
		for k, v := range s.mapping {
			out[k] = v
		}
		s.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": out})
	})
	mux.HandleFunc("/scim/group-mappings/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			out, deleted := s.snapshot()
			if deleted {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":{"code":404,"message":"SCIM group mapping not found"}}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": out})

		case http.MethodDelete:
			query := r.URL.Query()
			s.mu.Lock()
			s.deleteQueries = append(s.deleteQueries, query)
			s.mu.Unlock()

			// The live API deliberately has no default for keep_members.
			if query.Get("keep_members") == "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":{"code":400,"message":"keep_members is required"}}`))
				return
			}

			s.mu.Lock()
			s.deleted = true
			s.mu.Unlock()

			// Mirrors DeleteScimGroupMappingResponseSchema: { deleted: true }.
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"deleted":true}`))

		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, s
}

func scimGroupMappingStubConfig(serverURL, extraAttrs string) string {
	return fmt.Sprintf(`
provider "openrouter" {
  api_key    = "sk-or-mgmt-stub"
  server_url = %q
}

resource "openrouter_scim_group_mapping" "test" {
  scim_group_id = "group_stub_1"
  workspace_id  = "ws_stub_1"
  role          = "member"
%s}
`, serverURL, extraAttrs)
}

// TestStubScimGroupMappingDestroySendsKeepMembers is the regression test for
// the destroy path: with no keep_members attribute configured, destroy must
// still send keep_members, and it must default to keeping membership.
func TestStubScimGroupMappingDestroySendsKeepMembers(t *testing.T) {
	// resource.Test skips unless TF_ACC is set. These cases drive a local
	// httptest fixture and need no credentials, so they opt in like the other
	// stub tests in this package and run in the default offline CI gate.
	t.Setenv("TF_ACC", "1")

	srv, stub := newScimGroupMappingStub(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: scimGroupMappingStubConfig(srv.URL, ""),
				Check:  resource.TestCheckResourceAttrSet("openrouter_scim_group_mapping.test", "id"),
			},
		},
	})

	stub.mu.Lock()
	defer stub.mu.Unlock()
	if len(stub.deleteQueries) == 0 {
		t.Fatal("destroy never issued a DELETE for the SCIM group mapping")
	}
	if !stub.deleted {
		t.Fatal("stub never recorded a successful delete; the mapping survived destroy")
	}
	for i, q := range stub.deleteQueries {
		if got := q.Get("keep_members"); got != "true" {
			t.Errorf("delete call %d sent keep_members=%q, want %q", i, got, "true")
		}
	}
}

// TestStubScimGroupMappingDestroyKeepMembersFalse covers the explicit
// destructive choice: keep_members_enum = "false" must reach the API so the
// mapping's granted membership is removed with it.
func TestStubScimGroupMappingDestroyKeepMembersFalse(t *testing.T) {
	t.Setenv("TF_ACC", "1")

	srv, stub := newScimGroupMappingStub(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: scimGroupMappingStubConfig(srv.URL, "  keep_members_enum = \"false\"\n"),
				Check:  resource.TestCheckResourceAttrSet("openrouter_scim_group_mapping.test", "id"),
			},
		},
	})

	stub.mu.Lock()
	defer stub.mu.Unlock()
	if len(stub.deleteQueries) == 0 {
		t.Fatal("destroy never issued a DELETE for the SCIM group mapping")
	}
	for i, q := range stub.deleteQueries {
		if got := q.Get("keep_members"); got != "false" {
			t.Errorf("delete call %d sent keep_members=%q, want %q", i, got, "false")
		}
	}
}
