resource "openrouter_guardrail_policy_budget" "my_guardrailpolicybudget" {
  api_key_hashes = [
    "c56454edb818d6b14bc0d61c46025f1450b0f4012d12304ab40aacb519fcbc93",
  ]
  config = {
    include_byok_in_budgets = false
    limit_usd               = 50
    reset_interval          = "monthly"
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
  policy_type  = "budget"
  workspace_id = "0df9e665-d932-5740-b2c7-b52af166bc11"
}