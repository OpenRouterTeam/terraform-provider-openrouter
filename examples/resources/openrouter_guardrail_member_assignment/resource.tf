resource "openrouter_guardrail" "engineering_budget" {
  name           = "Engineering monthly budget"
  workspace_id   = "3f7c2a4e-9b1d-4c6e-8a2f-5d0e1b7c9a31"
  limit_usd      = 100
  reset_interval = "monthly"
}

resource "openrouter_guardrail_member_assignment" "example" {
  workspace_id = openrouter_guardrail.engineering_budget.workspace_id
  guardrail_id = openrouter_guardrail.engineering_budget.id
  user_id      = "user_2abcDEFghiJKLmnoPQRstu"
}
