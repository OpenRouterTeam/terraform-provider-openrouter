resource "openrouter_guardrail_policy_account_model_availability" "my_account_baseline" {
  config = {
    allowed_models = [
      "anthropic/claude-sonnet-4.5",
      "openai/gpt-4o",
    ]
    enforce_zdr = true
  }
  is_default = true
  name       = "Account baseline model access"
  opted_out_workspace_ids = [
    "0df9e665-d932-5740-b2c7-b52af166bc11",
  ]
}
