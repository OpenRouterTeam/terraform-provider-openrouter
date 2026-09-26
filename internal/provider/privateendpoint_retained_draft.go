package provider

import (
	"context"
	"fmt"

	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/operations"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/shared"
	"github.com/hashicorp/terraform-plugin-framework/diag"
)

// retainedPrivateEndpointDraftID returns the ID of the draft the API kept
// when a one-shot create-and-activate failed after the draft was persisted.
func retainedPrivateEndpointDraftID(res *operations.CreatePrivateEndpointResponse) string {
	candidates := []*shared.CreatePrivateEndpointValidationFailedResponse{}
	if body := res.FourHundredAndFourApplicationJSONOneOf; body != nil {
		candidates = append(candidates, body.CreatePrivateEndpointValidationFailedResponse)
	}
	if body := res.FourHundredAndNineApplicationJSONOneOf; body != nil {
		candidates = append(candidates, body.CreatePrivateEndpointValidationFailedResponse)
	}
	if body := res.FourHundredAndTwentyTwoApplicationJSONOneOf; body != nil {
		candidates = append(candidates, body.CreatePrivateEndpointValidationFailedResponse)
	}
	if body := res.FiveHundredApplicationJSONOneOf; body != nil {
		candidates = append(candidates, body.CreatePrivateEndpointValidationFailedResponse)
	}
	if body := res.FiveHundredAndTwoApplicationJSONOneOf; body != nil {
		candidates = append(candidates, body.CreatePrivateEndpointValidationFailedResponse)
	}
	for _, candidate := range candidates {
		if candidate != nil && candidate.Data.Endpoint.ID != "" {
			return candidate.Data.Endpoint.ID
		}
	}
	return ""
}

// deleteRetainedPrivateEndpointDraft removes a draft left behind by a failed
// create so the next apply does not add another one. Terraform never tracked
// the draft, so leaving it would strand it outside state.
func deleteRetainedPrivateEndpointDraft(ctx context.Context, client *sdk.OpenRouter, draftID string) diag.Diagnostics {
	var diags diag.Diagnostics
	draftOnly := operations.DraftOnlyTrue
	res, err := client.PrivateEndpoints.Delete(ctx, operations.DeletePrivateEndpointRequest{ID: draftID, DraftOnly: &draftOnly})
	if err == nil && res != nil && res.StatusCode == 200 {
		return diags
	}
	detail := fmt.Sprintf("Activation failed after the API created draft private endpoint %q, and deleting that draft also failed", draftID)
	if err != nil {
		detail += ": " + err.Error()
	} else if res != nil {
		detail += fmt.Sprintf(" with status %d", res.StatusCode)
	}
	diags.AddError(
		"Failed to clean up draft private endpoint",
		detail+". Delete it with the API or import it with `terraform import`.",
	)
	return diags
}
