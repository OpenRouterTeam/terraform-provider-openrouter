resource "openrouter_guardrail_policy_budget" "engineering" {
  name         = "Engineering monthly budget"
  policy_type  = "budget"
  workspace_id = "3f7c2a4e-9b1d-4c6e-8a2f-5d0e1b7c9a31"

  config = {
    include_byok_in_budgets = false
    limit_usd               = 100
    reset_interval          = "monthly"
  }
}

data "openrouter_organization_members" "all" {}

locals {
  # Name the member the assignment is for. The member list is not ordered, so an
  # index would attach the policy to whoever happens to come first; matching the
  # address and taking one result fails when the member is absent or the address
  # matches more than one, rather than assigning the policy to the wrong person.
  engineering_lead = one([
    for member in data.openrouter_organization_members.all.data :
    member.id if member.email == "lead@example.com"
  ])
}

# The assignment is what makes the policy apply to the member: a policy that is not the workspace default
# constrains nothing on its own.
resource "openrouter_guardrail_policy_member_assignment" "example" {
  member_id    = local.engineering_lead
  policy_id    = openrouter_guardrail_policy_budget.engineering.id
  policy_type  = openrouter_guardrail_policy_budget.engineering.policy_type
  workspace_id = openrouter_guardrail_policy_budget.engineering.workspace_id
}
