package acceptance

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/provider"
)

// TestGuardrailPolicyAccountImportRefusals calls the importer directly, because the acceptance
// framework cannot express either refusal: it reads an empty ImportStateId as "not set" and
// falls back to the id already in state, so a step never reaches the provider's check, and the
// slash case is asserted as a step but only through the message it produces.
//
// The importer is the one lifecycle method with no route behind it, so a bad id here is stored
// in state and only fails later, on a read that cannot resolve it.
func TestGuardrailPolicyAccountImportRefusals(t *testing.T) {
	r, ok := provider.NewGuardrailPolicyAccountBudgetResource().(resource.ResourceWithImportState)
	if !ok {
		t.Fatal("the account policy resource does not implement ResourceWithImportState")
	}

	for _, tc := range []struct {
		name string
		id   string
		want string
	}{
		{"empty", "", "expected a policy id, got an empty string"},
		{"workspace-qualified", "stub-workspace-a/stub-account-1", "expected a bare policy id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var resp resource.ImportStateResponse
			r.ImportState(context.Background(), resource.ImportStateRequest{ID: tc.id}, &resp)
			if !resp.Diagnostics.HasError() {
				t.Fatalf("import id %q was accepted", tc.id)
			}
			// A message alone is not the contract: an importer that set the id and then
			// errored would store a policy id that no read can resolve, so state has to stay
			// empty as well.
			if resp.State.Raw.Type() != nil && !resp.State.Raw.IsNull() {
				t.Errorf("a refused import still wrote state")
			}
			for _, d := range resp.Diagnostics {
				if strings.Contains(d.Detail(), tc.want) {
					return
				}
			}
			t.Errorf("no diagnostic carried %q; got %v", tc.want, resp.Diagnostics)
		})
	}
}
