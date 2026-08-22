resource "openrouter_guardrail" "my_guardrail" {
  allowed_models = [
    "openai/gpt-5.2",
    "anthropic/claude-4.5-opus-20251124",
    "deepseek/deepseek-r1-0528:free",
  ]
  allowed_providers = [
    "openai",
    "anthropic",
    "deepseek",
  ]
  content_filter_builtins = [
    {
      action     = "block"
      label      = "[EMAIL]"
      scan_scope = "user_only"
      slug       = "regex-prompt-injection"
    }
  ]
  content_filters = [
    {
      action  = "block"
      label   = "[API_KEY]"
      pattern = "\\b(sk-[a-zA-Z0-9]{48})\\b"
    }
  ]
  description                   = "A guardrail for limiting API usage"
  enable_free_model_publication = false
  enable_free_model_training    = true
  enable_paid_model_training    = true
  enforce_zdr                   = false
  enforce_zdr_anthropic         = false
  enforce_zdr_google            = false
  enforce_zdr_openai            = false
  enforce_zdr_other             = false
  enforce_zdr_xai               = false
  ignored_models = [
    "openai/gpt-4o-mini",
  ]
  ignored_providers = [
    "azure",
  ]
  include_byok_in_budgets = false
  limit_usd               = 50
  name                    = "My New Guardrail"
  reset_interval          = "monthly"
  workspace_id            = "0df9e665-d932-5740-b2c7-b52af166bc11"
}