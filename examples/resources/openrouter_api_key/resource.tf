resource "openrouter_api_key" "my_apikey" {
  creator_user_id       = "user_2dHFtVWx2n56w6HkM0000000000"
  disabled              = false
  expires_at            = "2027-12-31T23:59:59Z"
  external_api_key      = "...my_external_api_key..."
  external_user         = "...my_external_user..."
  include_byok_in_limit = true
  limit                 = 50
  limit_reset           = "monthly"
  name                  = "My New API Key"
  workspace_id          = "0df9e665-d932-5740-b2c7-b52af166bc11"
}