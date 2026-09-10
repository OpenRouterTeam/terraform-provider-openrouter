package acceptance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const (
	budgetWorkspaceID = "550e8400-e29b-41d4-a716-446655440000"
	budgetMonthlyID   = "770e8400-e29b-41d4-a716-446655440000"
	budgetDailyID     = "880e8400-e29b-41d4-a716-446655440000"
)

type stubWorkspaceBudget struct {
	ID            string  `json:"id"`
	WorkspaceID   string  `json:"workspace_id"`
	LimitUSD      float64 `json:"limit_usd"`
	ResetInterval string  `json:"reset_interval"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
}

// BYOK belongs to the workspace, not the individual interval. All handlers and
// out-of-band changes share the same lock, including concurrent Terraform reads.
type stubWorkspaceBudgets struct {
	mu          sync.Mutex
	includeBYOK bool
	budgets     map[string]stubWorkspaceBudget
}

func newStubWorkspaceBudgets(t *testing.T, includeBYOK bool) (*httptest.Server, *stubWorkspaceBudgets) {
	t.Helper()
	api := &stubWorkspaceBudgets{
		includeBYOK: includeBYOK,
		budgets:     make(map[string]stubWorkspaceBudget),
	}
	srv := httptest.NewServer(http.HandlerFunc(api.serveHTTP))
	t.Cleanup(srv.Close)
	return srv, api
}

func (s *stubWorkspaceBudgets) serveHTTP(w http.ResponseWriter, r *http.Request) {
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
	if len(parts) < 3 || len(parts) > 4 || parts[0] != "workspaces" ||
		(parts[1] != "production" && parts[1] != budgetWorkspaceID) || parts[2] != "budgets" {
		writeError(http.StatusNotFound, "Workspace not found")
		return
	}
	if len(parts) == 3 {
		if r.Method != http.MethodGet {
			writeError(http.StatusMethodNotAllowed, "Method not allowed")
			return
		}
		budgets := make([]stubWorkspaceBudget, 0, len(s.budgets))
		for _, interval := range []string{"daily", "monthly"} {
			if budget, ok := s.budgets[interval]; ok {
				budgets = append(budgets, budget)
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": budgets, "include_byok_in_budgets": s.includeBYOK,
		})
		return
	}
	interval := parts[3]
	budgetID := budgetMonthlyID
	switch interval {
	case "daily":
		budgetID = budgetDailyID
	case "monthly":
	default:
		writeError(http.StatusBadRequest, "Unsupported interval")
		return
	}
	switch r.Method {
	case http.MethodPut:
		var body struct {
			LimitUSD    float64 `json:"limit_usd"`
			IncludeBYOK *bool   `json:"include_byok_in_budgets"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.LimitUSD <= 0 {
			writeError(http.StatusBadRequest, "Invalid budget")
			return
		}
		if body.IncludeBYOK != nil {
			s.includeBYOK = *body.IncludeBYOK
		}
		s.budgets[interval] = stubWorkspaceBudget{
			ID: budgetID, WorkspaceID: budgetWorkspaceID,
			LimitUSD: body.LimitUSD, ResetInterval: interval,
			CreatedAt: "2026-09-10T12:00:00Z", UpdatedAt: "2026-09-10T12:00:00Z",
		}
	case http.MethodGet:
		if _, ok := s.budgets[interval]; !ok {
			writeError(http.StatusNotFound, "Budget not found")
			return
		}
	case http.MethodDelete:
		delete(s.budgets, interval)
		_ = json.NewEncoder(w).Encode(map[string]bool{"deleted": true})
		return
	default:
		writeError(http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": s.budgets[interval], "include_byok_in_budgets": s.includeBYOK,
	})
}

func (s *stubWorkspaceBudgets) setBYOK(value bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.includeBYOK = value
}

func (s *stubWorkspaceBudgets) checkBYOK(want bool) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.includeBYOK != want {
			return fmt.Errorf("server BYOK = %t, want %t", s.includeBYOK, want)
		}
		return nil
	}
}

func stubBudgetProviderConfig(serverURL string) string {
	return fmt.Sprintf(`
provider "openrouter" {
  api_key    = "local-test-only"
  server_url = %q
}
`, serverURL)
}

// UnitTest intentionally bypasses TF_ACC: every request goes to a local fixture.
// The initial true value catches defaults/unknown create state; the remote false
// refresh catches a Read mapper that preserves old state instead of server state.
func TestStubWorkspaceBudgetOmittedBYOKAndImport(t *testing.T) {
	srv, api := newStubWorkspaceBudgets(t, true)
	config := stubBudgetProviderConfig(srv.URL) + fmt.Sprintf(`
resource "openrouter_workspace_budget" "monthly" {
  workspace_ref = "production"
  interval      = "monthly"
  limit_usd     = 100
}
resource "openrouter_workspace_budget" "daily" {
  workspace_ref = %q
  interval      = "daily"
  limit_usd     = 10
}
data "openrouter_workspace_budget" "monthly" {
  workspace_ref = openrouter_workspace_budget.monthly.workspace_ref
  interval      = openrouter_workspace_budget.monthly.interval
  depends_on    = [openrouter_workspace_budget.monthly, openrouter_workspace_budget.daily]
}
data "openrouter_workspace_budgets" "all" {
  workspace_ref = openrouter_workspace_budget.daily.workspace_ref
  depends_on    = [openrouter_workspace_budget.monthly, openrouter_workspace_budget.daily]
}
`, budgetWorkspaceID)
	check := func(byok string) resource.TestCheckFunc {
		return resource.ComposeAggregateTestCheckFunc(
			resource.TestCheckResourceAttr("openrouter_workspace_budget.monthly", "workspace_ref", "production"),
			resource.TestCheckResourceAttr("openrouter_workspace_budget.monthly", "id", budgetMonthlyID),
			resource.TestCheckResourceAttr("openrouter_workspace_budget.monthly", "workspace_id", budgetWorkspaceID),
			resource.TestCheckResourceAttr("openrouter_workspace_budget.monthly", "include_byok_in_budgets", byok),
			resource.TestCheckResourceAttr("openrouter_workspace_budget.daily", "workspace_ref", budgetWorkspaceID),
			resource.TestCheckResourceAttr("openrouter_workspace_budget.daily", "id", budgetDailyID),
			resource.TestCheckResourceAttr("openrouter_workspace_budget.daily", "workspace_id", budgetWorkspaceID),
			resource.TestCheckResourceAttr("openrouter_workspace_budget.daily", "include_byok_in_budgets", byok),
			resource.TestCheckResourceAttr("data.openrouter_workspace_budget.monthly", "id", budgetMonthlyID),
			resource.TestCheckResourceAttr("data.openrouter_workspace_budget.monthly", "workspace_id", budgetWorkspaceID),
			resource.TestCheckResourceAttr("data.openrouter_workspace_budget.monthly", "include_byok_in_budgets", byok),
			resource.TestCheckResourceAttr("data.openrouter_workspace_budgets.all", "include_byok_in_budgets", byok),
			resource.TestCheckResourceAttr("data.openrouter_workspace_budgets.all", "data.#", "2"),
			resource.TestCheckResourceAttr("data.openrouter_workspace_budgets.all", "data.0.id", budgetDailyID),
			resource.TestCheckResourceAttr("data.openrouter_workspace_budgets.all", "data.1.id", budgetMonthlyID),
		)
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{Config: config, Check: check("true")},
			{
				ResourceName:      "openrouter_workspace_budget.monthly",
				ImportState:       true,
				ImportStateId:     `{"workspace_ref":"production","interval":"monthly"}`,
				ImportStateVerify: true,
			},
			{
				PreConfig:    func() { api.setBYOK(false) },
				RefreshState: true,
				Check:        check("false"),
			},
			{
				ResourceName:      "openrouter_workspace_budget.daily",
				ImportState:       true,
				ImportStateId:     fmt.Sprintf(`{"workspace_ref":%q,"interval":"daily"}`, budgetWorkspaceID),
				ImportStateVerify: true,
			},
		},
	})
}

func TestStubWorkspaceBudgetConfiguredBYOKDrift(t *testing.T) {
	srv, api := newStubWorkspaceBudgets(t, true)
	config := func(byok bool) string {
		return stubBudgetProviderConfig(srv.URL) + fmt.Sprintf(`
resource "openrouter_workspace_budget" "test" {
  workspace_ref           = "production"
  interval                = "monthly"
  limit_usd               = 100
  include_byok_in_budgets = %t
}
`, byok)
	}
	check := func(byok bool) resource.TestCheckFunc {
		return resource.ComposeAggregateTestCheckFunc(
			resource.TestCheckResourceAttr("openrouter_workspace_budget.test", "include_byok_in_budgets", fmt.Sprint(byok)),
			api.checkBYOK(byok),
		)
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{Config: config(false), Check: check(false)},
			{Config: config(true), Check: check(true)},
			{Config: config(false), Check: check(false)},
			{
				PreConfig: func() { api.setBYOK(true) },
				Config:    config(false),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("openrouter_workspace_budget.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: check(false),
			},
			{Config: config(false), PlanOnly: true},
		},
	})
}
