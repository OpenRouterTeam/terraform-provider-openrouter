resource "openrouter_workspace" "my_workspace" {
  confirm_default_settings_deletion = "false"
  default_image_model               = "openai/dall-e-3"
  default_provider_sort             = "price"
  default_text_model                = "openai/gpt-4o"
  description                       = "Production environment workspace"
  io_logging_api_key_ids = [
    4
  ]
  io_logging_sampling_rate            = 1
  is_data_discount_logging_enabled    = true
  is_observability_broadcast_enabled  = false
  is_observability_io_logging_enabled = false
  name                                = "Production"
  slug                                = "production"
}