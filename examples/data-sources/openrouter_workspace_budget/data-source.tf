data "openrouter_workspace_budget" "my_workspacebudget" {
  interval      = "monthly"
  workspace_ref = "production"
}