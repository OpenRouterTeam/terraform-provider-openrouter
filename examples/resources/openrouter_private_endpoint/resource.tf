resource "openrouter_private_endpoint" "my_privateendpoint" {
  activate = {
    workspace_id = "550e8400-e29b-41d4-a716-446655440000"
  }
  base_url = "https://api.openai.com/v1"
  declared_parameters = [
    "temperature",
    "seed",
  ]
  declared_region = "us"
  declared_zdr    = true
  idempotency_key = "wayfair-gpt-4o-eastus-2026-09"
  model_permaslug = "openai/gpt-4o-2024-08-06"
  pricing = {
    cache_read  = "0.00000025"
    cache_write = "0.000003125"
    completion  = "0.00001"
    prompt      = "0.0000025"
  }
  provider_slug = "openai"
  settings = {
    completions = {
      chat = false
      text = false
    }
    context_length = 128000
    excluded_parameters = [
      "logit_bias",
    ]
    inputs = {
      audio         = true
      base64_files  = true
      base64_images = true
      file_urls     = true
      image_urls    = true
    }
    max_completion_tokens = 16384
    max_prompt_tokens     = 9
    quantization          = "bf16"
    reasoning = {
      return_mechanism = "reasoning"
      supported        = true
    }
    tool_choice = {
      auto     = true
      function = true
      none     = true
      required = false
    }
  }
  upstream_model_id = "gpt-4o-2024-08-06"
}