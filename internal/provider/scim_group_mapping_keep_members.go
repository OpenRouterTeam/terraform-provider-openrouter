package provider

// Hand-written, so a Speakeasy regeneration leaves it alone. The call site is a
// single line in the generated scimgroupmapping_resource_sdk.go, which is the
// shape CONTRIBUTING.md asks for when generated code has to change: keep the
// custom code to a few lines and put the logic in its own file.

import (
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/operations"
)

// keepMembersForDelete maps the keep_members union onto the delete request.
//
// DELETE /scim/group-mappings/{id} requires the keep_members query parameter and
// returns 400 without it, deliberately, so the destructive path cannot be
// reached by omission (services/cfw-public-api/src/routes/scim/schemas.ts). The
// generated delete request builder only set the id, leaving the required
// KeepMembers union zero-valued, so the request carried no query string and the
// resource could never be destroyed.
//
// The union reaches Terraform as two optional, mutually exclusive attributes.
// Both serialise to the same query parameter, so the order below only decides
// what happens if a state file somehow holds both.
//
// Unset means keep_members=true: a destroy, or a replacement of scim_group_id or
// workspace_id, must not revoke workspace membership the configuration never
// asked to revoke. Removing members is the irreversible direction; keeping them
// is not, because re-creating the mapping grants them again.
func (r *ScimGroupMappingResourceModel) keepMembersForDelete() operations.KeepMembers {
	if !r.Boolean.IsNull() && !r.Boolean.IsUnknown() {
		return operations.CreateKeepMembersBoolean(r.Boolean.ValueBool())
	}
	if !r.KeepMembersEnum.IsNull() && !r.KeepMembersEnum.IsUnknown() {
		return operations.CreateKeepMembersKeepMembersEnum(
			operations.KeepMembersEnum(r.KeepMembersEnum.ValueString()),
		)
	}
	return operations.CreateKeepMembersBoolean(true)
}
