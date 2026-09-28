//lint:file-ignore U1000 Called only from persistent edits to privateendpoint_resource.go, which the generator lints before applying them.

package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/operations"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/shared"
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

// deleteUnactivatedPrivateEndpoint rejects a 201 create that asked for
// activation but returned a draft. A create retried with the same
// Idempotency-Key after a failed activation replays the kept draft as 201, and
// activate is create-only, so saving it would leave a non-routable endpoint
// that no later apply activates.
func deleteUnactivatedPrivateEndpoint(ctx context.Context, client *sdk.OpenRouter, data *PrivateEndpointResourceModel, res *shared.ManagedPrivateEndpointResponse) diag.Diagnostics {
	var diags diag.Diagnostics
	if data.Activate == nil || res.Data.Status != shared.PrivateEndpointStatusDraft {
		return diags
	}
	diags.Append(deleteRetainedPrivateEndpointDraft(ctx, client, res.Data.ID)...)
	diags.AddError(
		"Private endpoint activation did not complete",
		fmt.Sprintf("The API returned private endpoint %q as a draft although activate was set, so it was not saved to state. Fix the cause of the failed activation and apply again.", res.Data.ID),
	)
	return diags
}
