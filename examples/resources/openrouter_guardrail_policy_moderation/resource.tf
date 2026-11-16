resource "openrouter_guardrail_policy_moderation" "my_guardrailpolicymoderation" {
  api_key_hashes = [
    "c56454edb818d6b14bc0d61c46025f1450b0f4012d12304ab40aacb519fcbc93",
  ]
  config = {
    armor = {
      classifier_context = {
        additional_properties = {
          key = jsonencode("value")
        }
        auto_mode = {
          allow = [
            {
              str = "...my_str..."
            }
          ]
          environment = [
            {
              str = "...my_str..."
            }
          ]
          hard_deny = [
            {
              armor_rule = {
                id           = "...my_id..."
                instructions = "...my_instructions..."
                name         = "...my_name..."
                threshold    = 9.18
              }
            }
          ]
          soft_deny = [
            {
              str = "...my_str..."
            }
          ]
        }
        v = 2.96
      }
      mode                       = "enforce"
      on_error                   = "block"
      use_cc_style_default_rules = false
    }
    broadcast_model_armor_flagged_requests = false
    content_filter_blocks_requests         = true
    content_filter_builtins = [
      {
        action     = "flag"
        label      = "...my_label..."
        scan_scope = "user_only"
        slug       = "email"
      }
    ]
    content_filter_patterns = [
      "..."
    ]
    content_filters = [
      {
        action  = "redact"
        label   = "...my_label..."
        pattern = "...my_pattern..."
      }
    ]
    enable_model_armor_injection_protection = false
    lakera_injection_protection_mode        = "flag"
    model_armor_injection_confidence_level  = "high"
    model_armor_injection_protection_mode   = "flag"
    pii_filters = [
      {
        action          = "flag"
        entity_type     = "...my_entity_type..."
        score_threshold = 0.9
      }
    ]
    platform_image_moderated_providers = [
      "..."
    ]
    platform_moderated_providers = [
      "..."
    ]
  }
  is_workspace_default = true
  member_ids = [
    "user_2abcDEFghiJKLmnoPQRstu",
  ]
  name = "Engineering monthly budget"
  opted_out_api_key_hashes = [
    "c56454edb818d6b14bc0d61c46025f1450b0f4012d12304ab40aacb519fcbc93",
  ]
  opted_out_member_ids = [
    "user_2abcDEFghiJKLmnoPQRstu",
  ]
  policy_type  = "moderation"
  workspace_id = "0df9e665-d932-5740-b2c7-b52af166bc11"
}