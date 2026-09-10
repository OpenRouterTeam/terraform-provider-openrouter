resource "openrouter_workspace_budget" "my_workspacebudget" {
  include_byok_in_budgets = true
  interval                = "monthly"
  limit_usd               = 100
  workspace_ref           = "production"
}