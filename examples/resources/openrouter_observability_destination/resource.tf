resource "openrouter_observability_destination" "my_observabilitydestination" {
  api_key_hashes = [
    "..."
  ]
  config = {
    key = jsonencode("value")
  }
  enabled = true
  filter_rules = {
    enabled = true
    groups = [
      {
        logic = "and"
        rules = [
          {
            field    = "session_id"
            operator = "gt"
            value = {
              str = "...my_str..."
            }
          }
        ]
      }
    ]
  }
  name          = "Production Langfuse"
  privacy_mode  = false
  sampling_rate = 1
  type          = "langfuse"
  workspace_id  = "550e8400-e29b-41d4-a716-446655440000"
}