package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestParseGuardrailKeyAssignmentID(t *testing.T) {
	for id, want := range map[string][2]string{
		"gr-1/c56454edb818":      {"gr-1", "c56454edb818"},
		"ws-1/gr-1/c56454edb818": {"ws-1/gr-1", "c56454edb818"},
	} {
		guardrailID, keyHash, err := parseGuardrailKeyAssignmentID(id)
		if err != nil {
			t.Fatalf("parseGuardrailKeyAssignmentID(%q) unexpected error: %v", id, err)
		}
		if guardrailID != want[0] || keyHash != want[1] {
			t.Fatalf("parseGuardrailKeyAssignmentID(%q) = (%q, %q), want (%q, %q)", id, guardrailID, keyHash, want[0], want[1])
		}
		if got := guardrailKeyAssignmentID(guardrailID, keyHash); got != id {
			t.Fatalf("round trip = %q, want %q", got, id)
		}
	}

	for _, id := range []string{"", "gr-1", "/", "gr-1/", "/c56454edb818", "ws-1/gr-1/"} {
		if _, _, err := parseGuardrailKeyAssignmentID(id); err == nil {
			t.Errorf("parseGuardrailKeyAssignmentID(%q) returned no error", id)
		}
	}
}

func TestNotRawAPIKey(t *testing.T) {
	for value, wantErr := range map[string]bool{
		"c56454edb818d6b14bc0d61c46025f1450b0f4012d12304ab40aacb519fcbc93": false,
		"sk-or-v1-0123456789abcdef":                                        true,
	} {
		resp := &validator.StringResponse{}
		notRawAPIKey{}.ValidateString(context.Background(), validator.StringRequest{
			Path:        path.Root("key_hash"),
			ConfigValue: types.StringValue(value),
		}, resp)
		if resp.Diagnostics.HasError() != wantErr {
			t.Errorf("ValidateString(%q) error = %v, want %v", value, resp.Diagnostics.HasError(), wantErr)
		}
	}

	resp := &validator.StringResponse{}
	notRawAPIKey{}.ValidateString(context.Background(), validator.StringRequest{ConfigValue: types.StringUnknown()}, resp)
	if resp.Diagnostics.HasError() {
		t.Error("unknown value should pass")
	}
}
