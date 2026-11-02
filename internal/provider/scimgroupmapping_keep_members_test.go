package provider

import (
	"context"
	"testing"

	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/operations"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// The delete request must always carry keep_members: the Management API has no
// default for it and answers 400 when it is absent, which made destroy fail.
func TestScimGroupMappingDeleteKeepsMembers(t *testing.T) {
	t.Run("unset defaults to keeping members", func(t *testing.T) {
		req := deleteRequestFor(t, types.BoolNull(), types.StringNull())
		if req.KeepMembers.Type != operations.KeepMembersTypeBoolean {
			t.Fatalf("union type = %q, want %q", req.KeepMembers.Type, operations.KeepMembersTypeBoolean)
		}
		if req.KeepMembers.Boolean == nil || !*req.KeepMembers.Boolean {
			t.Fatalf("keep_members = %v, want true", req.KeepMembers.Boolean)
		}
	})

	t.Run("boolean attribute is used", func(t *testing.T) {
		req := deleteRequestFor(t, types.BoolValue(false), types.StringNull())
		if req.KeepMembers.Type != operations.KeepMembersTypeBoolean {
			t.Fatalf("union type = %q, want %q", req.KeepMembers.Type, operations.KeepMembersTypeBoolean)
		}
		if req.KeepMembers.Boolean == nil || *req.KeepMembers.Boolean {
			t.Fatalf("keep_members = %v, want false", req.KeepMembers.Boolean)
		}
	})

	t.Run("enum attribute is used", func(t *testing.T) {
		req := deleteRequestFor(t, types.BoolNull(), types.StringValue("false"))
		if req.KeepMembers.Type != operations.KeepMembersTypeKeepMembersEnum {
			t.Fatalf("union type = %q, want %q", req.KeepMembers.Type, operations.KeepMembersTypeKeepMembersEnum)
		}
		if req.KeepMembers.KeepMembersEnum == nil || string(*req.KeepMembers.KeepMembersEnum) != "false" {
			t.Fatalf("keep_members = %v, want \"false\"", req.KeepMembers.KeepMembersEnum)
		}
	})

	// The schema makes this combination unreachable (the two attributes
	// ConflictWith each other); this only pins the helper's guard order.
	t.Run("boolean wins when both are set", func(t *testing.T) {
		req := deleteRequestFor(t, types.BoolValue(true), types.StringValue("false"))
		if req.KeepMembers.Type != operations.KeepMembersTypeBoolean {
			t.Fatalf("union type = %q, want %q", req.KeepMembers.Type, operations.KeepMembersTypeBoolean)
		}
		if req.KeepMembers.Boolean == nil || !*req.KeepMembers.Boolean {
			t.Fatalf("keep_members = %v, want true", req.KeepMembers.Boolean)
		}
	})
}

func deleteRequestFor(t *testing.T, boolean types.Bool, enumValue types.String) *operations.DeleteScimGroupMappingRequest {
	t.Helper()
	model := &ScimGroupMappingResourceModel{
		ID:              types.StringValue("sgm_1"),
		Boolean:         boolean,
		KeepMembersEnum: enumValue,
	}
	req, diags := model.ToOperationsDeleteScimGroupMappingRequest(context.Background())
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if req.ID != "sgm_1" {
		t.Fatalf("id = %q, want sgm_1", req.ID)
	}
	return req
}
