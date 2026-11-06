package provider

import (
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestWorkspaceDefaultGuardrail_ClearsBudget(t *testing.T) {
	for _, mode := range []string{"explicit_null", "omitted"} {
		t.Run(mode, func(t *testing.T) {
			api := &fakeAPI{}
			srv := httptest.NewServer(api.handler(t))
			defer srv.Close()
			defer func() {
				api.mu.Lock()
				defer api.mu.Unlock()
				t.Logf("persisted budget after Terraform: limit_usd=%v reset_interval=%v include_byok_in_budgets=%v PATCH count=%d", api.guardrail["limit_usd"], api.guardrail["reset_interval"], api.guardrail["include_byok_in_budgets"], api.patches)
			}()
			config := func(clear bool) string {
				budget := "limit_usd = 100\n reset_interval = \"monthly\""
				include := true
				if clear {
					include = false
					budget = "limit_usd = null\n reset_interval = null"
					if mode == "omitted" {
						budget = ""
					}
				}
				return fmt.Sprintf(`
provider "openrouter" {
 api_key = "local-dummy-token"
 server_url = %q
}
resource "openrouter_workspace_default_guardrail" "this" {
 workspace_id = %q
 %s
 include_byok_in_budgets = %t
}
`, srv.URL, fakeWorkspaceID, budget, include)
			}
			checkStored := func(clear bool) resource.TestCheckFunc {
				return func(*terraform.State) error {
					api.mu.Lock()
					defer api.mu.Unlock()
					if !clear {
						if api.guardrail["limit_usd"] != float64(100) || api.guardrail["reset_interval"] != "monthly" || api.guardrail["include_byok_in_budgets"] != true {
							return fmt.Errorf("initial stored budget = %v", api.guardrail)
						}
						return nil
					}
					for _, field := range []string{"limit_usd", "reset_interval"} {
						value, present := api.lastPatch[field]
						if !present || value != nil || api.guardrail[field] != nil {
							return fmt.Errorf("budget clear %s: PATCH value=%v present=%t stored=%v, want explicit null and cleared API state", field, value, present, api.guardrail[field])
						}
					}
					if api.guardrail["include_byok_in_budgets"] != false {
						return fmt.Errorf("stored include_byok_in_budgets=%v, want false", api.guardrail["include_byok_in_budgets"])
					}
					return nil
				}
			}
			const addr = "openrouter_workspace_default_guardrail.this"
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: testProviderFactories(),
				Steps: []resource.TestStep{
					{Config: config(false), Check: checkStored(false)},
					{Config: config(true), ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate)}}, Check: resource.ComposeAggregateTestCheckFunc(checkStored(true), resource.TestCheckNoResourceAttr(addr, "limit_usd"), resource.TestCheckNoResourceAttr(addr, "reset_interval"), resource.TestCheckResourceAttr(addr, "include_byok_in_budgets", "false"))},
					{RefreshState: true, Check: resource.ComposeAggregateTestCheckFunc(checkStored(true), resource.TestCheckNoResourceAttr(addr, "limit_usd"), resource.TestCheckNoResourceAttr(addr, "reset_interval"))},
					{Config: config(true), PlanOnly: true},
				},
			})
		})
	}
}
