package stringplanmodifier

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
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
