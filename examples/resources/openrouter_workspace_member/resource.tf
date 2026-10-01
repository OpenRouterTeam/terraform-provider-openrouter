resource "openrouter_workspace" "engineering" {
  name = "Engineering"
  slug = "engineering"
}

resource "openrouter_workspace_member" "example" {
  workspace_id = openrouter_workspace.engineering.id
  user_id      = "user_2abcDEFghiJKLmnoPQRstu"
}
