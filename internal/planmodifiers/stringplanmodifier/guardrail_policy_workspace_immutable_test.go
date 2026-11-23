package stringplanmodifier

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestGuardrailPolicyWorkspaceRemoved(t *testing.T) {
	for name, tc := range map[string]struct {
		state types.String
		plan  types.String
		want  bool
	}{
		"configured field removed": {state: types.StringValue("ws-a"), plan: types.StringNull(), want: true},
		"same value":               {state: types.StringValue("ws-a"), plan: types.StringValue("ws-a"), want: false},
		"moved workspace":          {state: types.StringValue("ws-a"), plan: types.StringValue("ws-b"), want: false},
		"added where none stored":  {state: types.StringNull(), plan: types.StringValue("ws-a"), want: false},
		"nothing stored or set":    {state: types.StringNull(), plan: types.StringNull(), want: false},
		"unknown state":            {state: types.StringUnknown(), plan: types.StringNull(), want: false},
		"unknown plan":             {state: types.StringValue("ws-a"), plan: types.StringUnknown(), want: false},
	} {
		if got := guardrailPolicyWorkspaceRemoved(tc.state, tc.plan); got != tc.want {
			t.Errorf("%s: guardrailPolicyWorkspaceRemoved = %v, want %v", name, got, tc.want)
		}
	}
}

func TestGuardrailPolicyWorkspaceRequiresReplace(t *testing.T) {
	for name, tc := range map[string]struct {
		state types.String
		plan  types.String
		want  bool
	}{
		"moved workspace":          {state: types.StringValue("ws-a"), plan: types.StringValue("ws-b"), want: true},
		"slug rewritten as UUID":   {state: types.StringValue("team-a"), plan: types.StringValue("9f1e8b2c"), want: true},
		"same value":               {state: types.StringValue("ws-a"), plan: types.StringValue("ws-a"), want: false},
		"configured field removed": {state: types.StringValue("ws-a"), plan: types.StringNull(), want: false},
		"added where none stored":  {state: types.StringNull(), plan: types.StringValue("ws-a"), want: false},
		"nothing stored or set":    {state: types.StringNull(), plan: types.StringNull(), want: false},
		"unknown state":            {state: types.StringUnknown(), plan: types.StringValue("ws-b"), want: false},
		"unknown plan":             {state: types.StringValue("ws-a"), plan: types.StringUnknown(), want: false},
	} {
		if got := guardrailPolicyWorkspaceRequiresReplace(tc.state, tc.plan); got != tc.want {
			t.Errorf("%s: guardrailPolicyWorkspaceRequiresReplace = %v, want %v", name, got, tc.want)
		}
	}
}

func TestGuardrailPolicyWorkspaceImmutablePlanModifyString(t *testing.T) {
	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{"workspace_id": tftypes.String}}
	raw := func(v any) tftypes.Value {
		return tftypes.NewValue(objType, map[string]tftypes.Value{
			"workspace_id": tftypes.NewValue(tftypes.String, v),
		})
	}

	for name, tc := range map[string]struct {
		modifier planmodifier.String
		wantDesc string
		wantWord string
		other    string
	}{
		"policy": {
			modifier: GuardrailPolicyWorkspaceImmutable(),
			wantDesc: "Replaces the policy when the workspace changes or is added, and refuses a plan that removes the workspace.",
			wantWord: "A guardrail policy stays in the workspace it was created in",
			other:    "An assignment is addressed with a workspace scope",
		},
		"assignment": {
			modifier: GuardrailPolicyAssignmentWorkspaceImmutable(),
			wantDesc: "Replaces the assignment when the workspace changes or is added, and refuses a plan that removes the workspace.",
			wantWord: "An assignment is addressed with a workspace scope",
			other:    "A guardrail policy stays in the workspace it was created in",
		},
	} {
		req := planmodifier.StringRequest{
			State:      tfsdk.State{Raw: raw("ws-a")},
			Plan:       tfsdk.Plan{Raw: raw(nil)},
			StateValue: types.StringValue("ws-a"),
			PlanValue:  types.StringNull(),
			Path:       path.Root("workspace_id"),
		}

		// Creating and destroying have nothing stored to compare against. A create
		// carries a null state value and a resolved plan value; a destroy carries a
		// stored value and no plan at all.
		for caseName, empty := range map[string]planmodifier.StringRequest{
			"create": {
				State:      tfsdk.State{},
				Plan:       tfsdk.Plan{Raw: raw("ws-b")},
				StateValue: types.StringNull(),
				PlanValue:  types.StringValue("ws-b"),
				Path:       path.Root("workspace_id"),
			},
			"destroy": {
				State:      tfsdk.State{Raw: raw("ws-a")},
				Plan:       tfsdk.Plan{},
				StateValue: types.StringValue("ws-a"),
				PlanValue:  types.StringNull(),
				Path:       path.Root("workspace_id"),
			},
		} {
			var resp planmodifier.StringResponse
			tc.modifier.PlanModifyString(context.Background(), empty, &resp)
			if resp.Diagnostics.HasError() || resp.RequiresReplace {
				t.Errorf("%s/%s: a create or a destroy must pass through: errors=%v replace=%v", name, caseName, resp.Diagnostics.Errors(), resp.RequiresReplace)
			}
		}

		// A plan that removes the recorded scope is refused, in this subject's words.
		var resp planmodifier.StringResponse
		tc.modifier.PlanModifyString(context.Background(), req, &resp)
		if !resp.Diagnostics.HasError() {
			t.Fatalf("%s: removing the scope must be refused", name)
		}
		got := fmt.Sprintf("%v", resp.Diagnostics.Errors())
		if !strings.Contains(got, tc.wantWord) {
			t.Errorf("%s: diagnostic = %s, want it to name %q", name, got, tc.wantWord)
		}
		if strings.Contains(got, tc.other) {
			t.Errorf("%s: diagnostic used the other subject's wording: %s", name, got)
		}

		// A moved scope is still a replacement, and not an error.
		moved := req
		moved.Plan = tfsdk.Plan{Raw: raw("ws-b")}
		moved.PlanValue = types.StringValue("ws-b")
		var movedResp planmodifier.StringResponse
		tc.modifier.PlanModifyString(context.Background(), moved, &movedResp)
		if movedResp.Diagnostics.HasError() {
			t.Errorf("%s: a moved scope must not be an error: %v", name, movedResp.Diagnostics.Errors())
		}
		if !movedResp.RequiresReplace {
			t.Errorf("%s: a moved scope must require replacement", name)
		}

		if d := tc.modifier.Description(context.Background()); d != tc.wantDesc {
			t.Errorf("%s: Description() = %q, want %q", name, d, tc.wantDesc)
		}
		if m := tc.modifier.MarkdownDescription(context.Background()); m != tc.wantDesc {
			t.Errorf("%s: MarkdownDescription() = %q, want %q", name, m, tc.wantDesc)
		}
	}
}
