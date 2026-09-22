# (a) Fresh workspace. Its default guardrail already exists (the workspace
# reports default_guardrail_id) but has never been written, so
# GET /guardrails/{default_guardrail_id} still returns 404 and the guardrail
# is absent from the openrouter_guardrails data source.
#
#   terraform plan   -> openrouter_workspace_default_guardrail.production will be created
#   terraform apply  -> PATCH /guardrails/{default_guardrail_id} materializes and configures it
#
# (b) Same workspace, later. GET /guardrails/{default_guardrail_id} now returns
# the configured guardrail and matches this configuration.
#
#   terraform plan   -> No changes. Your infrastructure matches the configuration.
resource "openrouter_workspace" "production" {
  name = "production"
}

resource "openrouter_workspace_default_guardrail" "production" {
  workspace_id = openrouter_workspace.production.id

  limit_usd             = 500
  reset_interval        = "monthly"
  enforce_zdr_openai    = true
  enforce_zdr_anthropic = true
  allowed_models        = ["openai/gpt-4o", "anthropic/claude-sonnet-4"]
}
