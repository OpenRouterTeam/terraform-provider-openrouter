resource "openrouter_private_provider" "my_privateprovider" {
  base_url = "https://inference.example.com/v1"
  data_policy = {
    prompt_retention_days = 4
    retains_prompts       = false
    training              = false
  }
  datacenters = [
    "US",
  ]
  display_name       = "WCIE"
  headquarters       = "US"
  name               = "WCIE"
  privacy_policy_url = "https://example.com/privacy"
}