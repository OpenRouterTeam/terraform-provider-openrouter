resource "openrouter_byok_key" "my_byokkey" {
  allowed_api_key_hashes = [
    "f01d52606dc8f0a8303a7b5cc3fa07109c2e346cec7c0a16b40de462992ce943",
  ]
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