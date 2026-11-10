import {
  to = openrouter_workspace_budget.my_openrouter_workspace_budget
  id = jsonencode({
    interval      = "monthly"
    workspace_ref = "production"
  })
}
