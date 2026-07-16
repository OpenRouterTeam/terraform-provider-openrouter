resource "openrouter_byok_key" "my_byokkey" {
  allowed_models = [
    "..."
  ]
  allowed_user_ids = [
    "..."
  ]
  disabled      = false
  is_fallback   = false
  key           = "sk-proj-abc123..."
  name          = "Production OpenAI Key"
  provider_slug = "openai"
  workspace_id  = "550e8400-e29b-41d4-a716-446655440000"
}