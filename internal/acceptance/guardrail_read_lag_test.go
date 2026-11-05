package acceptance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// The API answers 404 for a guardrail written milliseconds earlier, and keeps
// answering 200 for a guardrail deleted milliseconds earlier. This stub holds
// the row back from reads for a fixed number of calls in both directions, so
// the provider's tolerance of that lag is exercised end to end.
type stubGuardrailLag struct {
	mu sync.Mutex

	id string

	getMissesBeforeVisible int
	getMissesAfterDelete   int

	created    bool
	deleted    bool
	gets       int
	deleteGets int
}

func newStubGuardrailLag(t *testing.T) (*httptest.Server, *stubGuardrailLag) {
	t.Helper()
	api := &stubGuardrailLag{
		id:                     "c6a1f4c8-0f1e-4b6a-9d2f-5f0f3e7a1b22",
		getMissesBeforeVisible: 2,
		getMissesAfterDelete:   2,
	}
	srv := httptest.NewServer(http.HandlerFunc(api.serveHTTP))
	t.Cleanup(srv.Close)
	return srv, api
}

func (s *stubGuardrailLag) serveHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	write := func(status int, body any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}
	row := map[string]any{"id": s.id, "name": "lag", "include_byok_in_budgets": false, "created_at": "2026-11-05T00:00:00Z"}

	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/guardrails":
		s.created = true
		write(http.StatusCreated, map[string]any{"data": row})
	case r.URL.Path == "/guardrails/"+s.id && r.Method == http.MethodGet:
		s.gets++
		switch {
		case !s.created:
			write(http.StatusNotFound, map[string]any{"error": map[string]any{"code": 404, "message": "Guardrail not found"}})
		case s.gets <= s.getMissesBeforeVisible:
			write(http.StatusNotFound, map[string]any{"error": map[string]any{"code": 404, "message": "Guardrail not found"}})
		case s.deleted && s.gets-s.deleteGets > s.getMissesAfterDelete:
			write(http.StatusNotFound, map[string]any{"error": map[string]any{"code": 404, "message": "Guardrail not found"}})
		default:
			write(http.StatusOK, map[string]any{"data": row})
		}
	case r.URL.Path == "/guardrails/"+s.id && r.Method == http.MethodDelete:
		s.deleted = true
		s.deleteGets = s.gets
		write(http.StatusOK, map[string]any{"deleted": true})
	default:
		write(http.StatusNotFound, map[string]any{"error": map[string]any{"code": 404, "message": "not found"}})
	}
}

func TestStubGuardrailReadAfterWriteLag(t *testing.T) {
	srv, api := newStubGuardrailLag(t)
	const name = "openrouter_guardrail.test"
	config := stubBudgetProviderConfig(srv.URL) + fmt.Sprintf(`
resource "openrouter_guardrail" "test" {
  name = "lag"
}
`)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "id", api.id),
					resource.TestCheckResourceAttr(name, "name", "lag"),
				),
			},
			// The refresh at the start of this step is the read that used to
			// lose the guardrail and force a replacement.
			{Config: config, PlanOnly: true},
		},
	})

	api.mu.Lock()
	defer api.mu.Unlock()
	if api.gets < 3 {
		t.Fatalf("expected the provider to retry past the lagging reads, saw %d GETs", api.gets)
	}
	if !api.deleted {
		t.Fatal("expected the guardrail to be deleted at the end of the test")
	}
}
