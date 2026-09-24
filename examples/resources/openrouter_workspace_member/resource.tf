resource "openrouter_workspace_member" "my_workspacemember" {
  id = "production"
  user_ids = [
    "user_abc123",
    "user_def456",
  ]
}