package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestParseGuardrailKeyAssignmentID(t *testing.T) {
	guardrailID, keyHash, err := parseGuardrailKeyAssignmentID("gr-1/c56454edb818")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if guardrailID != "gr-1" || keyHash != "c56454edb818" {
		t.Fatalf("got (%q, %q)", guardrailID, keyHash)
	}
	if got := guardrailKeyAssignmentID(guardrailID, keyHash); got != "gr-1/c56454edb818" {
		t.Fatalf("round trip = %q", got)
	}

	for _, id := range []string{"", "gr-1", "gr-1/", "/c56454edb818", "ws-1/gr-1/c56454edb818"} {
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
