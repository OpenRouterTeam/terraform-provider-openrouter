# Keeps the credential out of Terraform state and plans. Requires Terraform 1.11
# or later; supply the value from an ephemeral variable or ephemeral resource.
# To rotate the key, change the value and increment key_wo_version.
variable "openai_api_key" {
  type      = string
  sensitive = true
  ephemeral = true
}

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
  declared_region = "europe"
  declared_zdr    = true
  disabled        = false
  is_byok_only    = false
  is_fallback     = false
  is_required     = false
  # `key = "sk-proj-abc123..."` also works, but stores the key in Terraform state.
  key_wo         = var.openai_api_key
  key_wo_version = 1
  name           = "Production OpenAI Key"
  provider_slug  = "openai"
  workspace_id   = "550e8400-e29b-41d4-a716-446655440000"
}
