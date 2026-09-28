package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/operations"
	"github.com/hashicorp/terraform-plugin-framework/diag"
)

// retainedPrivateEndpointDraftID returns the ID of the draft the API kept
// when a one-shot create-and-activate failed after the draft was persisted.
// The kept draft can come back with any error status (a failed check can
// surface as 400, 404, 409, 422 or 5xx), so the raw body is read rather than
// the per-status models.
func retainedPrivateEndpointDraftID(res *operations.CreatePrivateEndpointResponse) string {
	if res == nil || res.RawResponse == nil || res.RawResponse.Body == nil {
		return ""
	}
	raw, err := io.ReadAll(res.RawResponse.Body)
	res.RawResponse.Body = io.NopCloser(bytes.NewReader(raw))
	if err != nil {
		return ""
	}
	var body struct {
		Data struct {
			Endpoint struct {
				ID string `json:"id"`
			} `json:"endpoint"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &body) != nil {
		return ""
	}
	return body.Data.Endpoint.ID
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
