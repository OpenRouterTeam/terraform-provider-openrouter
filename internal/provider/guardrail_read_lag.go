package provider

import (
	"context"
	"time"

	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/operations"
)

// Guardrail reads can lag the write that created or deleted the row: a create
// returns 201 with the guardrail, yet a read issued milliseconds later answers
// 404, and a read right after a delete can still answer 200. Measured against
// the production API, visibility settled within 250 ms on most calls and once
// at 1.9 s. Terraform issues exactly those reads (a data source that depends on
// the resource, and the post-destroy check), so the provider tolerates the lag
// instead of reporting a guardrail that exists as missing, or a deleted one as
// present.
const (
	guardrailLagAttempts = 8
	guardrailLagInterval = 300 * time.Millisecond
)

// getGuardrailTolerantOfReadLag retries a 404 for a bounded window. Any other
// status, and any transport error, returns immediately.
func getGuardrailTolerantOfReadLag(ctx context.Context, client *sdk.OpenRouter, request *operations.GetGuardrailRequest) (*operations.GetGuardrailResponse, error) {
	var res *operations.GetGuardrailResponse
	var err error
	for attempt := 0; attempt < guardrailLagAttempts; attempt++ {
		res, err = client.Guardrails.GetGuardrail(ctx, *request)
		if err != nil {
			return res, err
		}
		if res != nil && res.StatusCode != 404 {
			return res, nil
		}
		if attempt == guardrailLagAttempts-1 {
			break
		}
		select {
		case <-ctx.Done():
			return res, nil
		case <-time.After(guardrailLagInterval):
		}
	}
	return res, nil
}

// awaitGuardrailDeleted polls until the guardrail reads as gone, so a destroy
// reports completion only once the API stops serving the row. A guardrail that
// is still visible when the window closes does not fail the destroy: the delete
// call already succeeded, and reporting an error there would strand state.
func awaitGuardrailDeleted(ctx context.Context, client *sdk.OpenRouter, data *GuardrailResourceModel) {
	request, diags := data.ToOperationsGetGuardrailRequest(ctx)
	if diags.HasError() {
		return
	}
	for attempt := 0; attempt < guardrailLagAttempts; attempt++ {
		res, err := client.Guardrails.GetGuardrail(ctx, *request)
		if err != nil || res == nil || res.StatusCode == 404 {
			return
		}
		if attempt == guardrailLagAttempts-1 {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(guardrailLagInterval):
		}
	}
}
