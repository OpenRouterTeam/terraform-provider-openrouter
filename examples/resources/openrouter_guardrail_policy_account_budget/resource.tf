resource "openrouter_guardrail_policy_account_budget" "my_account_budget" {
  config = {
    include_byok_in_budgets = false
    limit_usd               = 500
    reset_interval          = "monthly"
  }
  is_default = false
  name       = "Engineering monthly budget"
  workspace_ids = [
    "0df9e665-d932-5740-b2c7-b52af166bc11",
  ]
}
