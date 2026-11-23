resource "openrouter_guardrail_policy_account_moderation" "my_account_moderation" {
  config = {
    content_filter_builtins = [
      {
        action = "block"
        slug   = "secrets"
      }
    ]
  }
  is_default = false
  name       = "Block secrets in every covered workspace"
  workspace_ids = [
    "0df9e665-d932-5740-b2c7-b52af166bc11",
  ]
}
