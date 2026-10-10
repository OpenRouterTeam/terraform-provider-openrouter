variable "langfuse_public_key" {
  type      = string
  sensitive = true
  ephemeral = true
}

variable "langfuse_secret_key" {
  type      = string
  sensitive = true
  ephemeral = true
}

resource "openrouter_observability_destination" "my_observabilitydestination" {
  api_key_hashes = [
    "..."
  ]
  broadcast_generation_cost            = false
  broadcast_generation_identity        = false
  broadcast_generation_request_context = false
  # Public settings stay in `config`. Credentials go in `config_secrets_wo`, which
  # keeps them out of Terraform state and plans (Terraform 1.11 or later); supply
  # them from ephemeral variables or ephemeral resources. To rotate them, change
  # the values and increment `config_secrets_wo_version`.
  config = {
    baseUrl = jsonencode("https://cloud.langfuse.com")
  }
  config_secrets_wo = {
    publicKey = jsonencode(var.langfuse_public_key)
    secretKey = jsonencode(var.langfuse_secret_key)
  }
  config_secrets_wo_version = 1
  enabled                   = true
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
  name         = "Production Langfuse"
  privacy_mode = false
  regions = [
    "global",
  ]
  sampling_rate = 1
  type          = "langfuse"
  workspace_id  = "550e8400-e29b-41d4-a716-446655440000"
}