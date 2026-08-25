resource "openrouter_workspace_budget" "my_workspacebudget" {
  id                      = "production"
  include_byok_in_budgets = true
  interval                = "monthly"
  limit_usd               = 100
}