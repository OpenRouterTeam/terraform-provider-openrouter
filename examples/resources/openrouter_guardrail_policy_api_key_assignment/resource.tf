resource "openrouter_guardrail_policy_budget" "engineering" {
  name         = "Engineering monthly budget"
  policy_type  = "budget"
  workspace_id = "3f7c2a4e-9b1d-4c6e-8a2f-5d0e1b7c9a31"

  config = {
    include_byok_in_budgets = false
    limit_usd               = 100
    reset_interval          = "monthly"
  }
}

resource "openrouter_api_key" "ci" {
  name         = "CI pipeline"
  workspace_id = openrouter_guardrail_policy_budget.engineering.workspace_id
}

# The assignment is what makes the policy apply to the key: a policy that is not the workspace default
# constrains nothing on its own.
resource "openrouter_guardrail_policy_api_key_assignment" "example" {
  key_hash     = openrouter_api_key.ci.hash
  policy_id    = openrouter_guardrail_policy_budget.engineering.id
  policy_type  = openrouter_guardrail_policy_budget.engineering.policy_type
  workspace_id = openrouter_guardrail_policy_budget.engineering.workspace_id
}
