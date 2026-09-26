resource "openrouter_private_endpoint" "my_privateendpoint" {
  activate = {
    workspace_id = "550e8400-e29b-41d4-a716-446655440000"
  }
  base_url        = "https://contoso.openai.azure.com"
  declared_region = "us"
  declared_zdr    = true
  draft_only      = "false"
  model_permaslug = "openai/gpt-4o-2024-08-06"
  pricing = {
    completion = "0.00001"
    prompt     = "0.0000025"
  }
  provider_slug     = "azure"
  upstream_model_id = "gpt-4o-prod"
}