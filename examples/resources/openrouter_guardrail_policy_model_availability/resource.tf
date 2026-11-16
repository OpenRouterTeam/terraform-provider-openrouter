resource "openrouter_guardrail_policy_model_availability" "my_guardrailpolicymodelavailability" {
  api_key_hashes = [
    "c56454edb818d6b14bc0d61c46025f1450b0f4012d12304ab40aacb519fcbc93",
  ]
  config = {
    allow_safety_retention_google = true
    allow_safety_retention_other  = false
    allowed_data_regions = [
      "..."
    ]
    allowed_models = [
      "..."
    ]
    allowed_providers = [
      "..."
    ]
    enable_free_model_publication      = false
    enable_free_model_training         = false
    enable_paid_model_training         = true
    enforce_first_party_harness_models = false
    enforce_zdr                        = false
    enforce_zdr_anthropic              = false
    enforce_zdr_google                 = true
    enforce_zdr_openai                 = true
    enforce_zdr_other                  = false
    enforce_zdr_xai                    = false
    ignored_models = [
      "..."
    ]
    ignored_providers = [
      "..."
    ]
    platform_banned_authors = [
      "..."
    ]
    platform_banned_models = [
      "..."
    ]
    platform_banned_providers = [
      "..."
    ]
    require_eu_data_region = true
  }
  is_workspace_default = false
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
  policy_type  = "model_availability"
  workspace_id = "0df9e665-d932-5740-b2c7-b52af166bc11"
}