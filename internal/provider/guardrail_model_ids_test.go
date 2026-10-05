package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestReconcileModelIDs(t *testing.T) {
	prior := []types.String{types.StringValue("openai/gpt-5.2")}

	if got := reconcileModelIDs(prior, []string{"openai/gpt-5.2-20251211"}); got[0].ValueString() != "openai/gpt-5.2" {
		t.Fatalf("canonicalized response should keep configured value, got %v", got)
	}
	if got := reconcileModelIDs(prior, []string{"a/x", "b/y"}); len(got) != 2 || got[1].ValueString() != "b/y" {
		t.Fatalf("length change should surface API values, got %v", got)
	}
	if got := reconcileModelIDs(nil, []string{"a/x"}); len(got) != 1 || got[0].ValueString() != "a/x" {
		t.Fatalf("import with no prior should use API values, got %v", got)
	}
	if got := reconcileModelIDs(prior, nil); got != nil {
		t.Fatalf("nil response should clear state, got %v", got)
	}
}
