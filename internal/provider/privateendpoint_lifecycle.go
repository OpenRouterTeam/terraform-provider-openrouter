package provider

import (
	"context"
	"fmt"

	custom_stringplanmodifier "github.com/OpenRouterTeam/terraform-provider-openrouter/internal/planmodifiers/stringplanmodifier"
	"github.com/hashicorp/go-uuid"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// privateEndpointBaseURL keeps the configured spelling of base_url when the
// API returns the same URL in its normalized form (lowercase scheme and host,
// no trailing "/" or "/chat/completions"). Storing the server value instead
// would differ from config on every plan.
func privateEndpointBaseURL(prior types.String, server *string) types.String {
	if server != nil && !prior.IsNull() && !prior.IsUnknown() &&
		custom_stringplanmodifier.NormalizePrivateEndpointBaseURL(prior.ValueString()) == custom_stringplanmodifier.NormalizePrivateEndpointBaseURL(*server) {
		return prior
	}
	return types.StringPointerValue(server)
}

// newPrivateEndpointIdempotencyKey returns the Idempotency-Key for one
// resource create. The SDK retries reuse it, so a create that timed out or
// failed after the server committed it returns that endpoint instead of adding
// another. It must be fresh per create: the server keeps keys of deleted
// endpoints, so a key derived from config would replay a deleted endpoint.
func newPrivateEndpointIdempotencyKey() (*string, diag.Diagnostics) {
	var diags diag.Diagnostics
	key, err := uuid.GenerateUUID()
	if err != nil {
		diags.AddError("failure to generate an idempotency key", err.Error())
		return nil, diags
	}
	key = "terraform-" + key
	return &key, diags
}

// privateEndpointCreateTimeoutDetail explains a create that ran out of retries
// on 408: the API keeps working after it answers 408, so the endpoint may
// exist without being in state.
func privateEndpointCreateTimeoutDetail(statusCode int) string {
	if statusCode != 408 {
		return ""
	}
	return fmt.Sprintf("The API timed out (%d) on every attempt but keeps processing a create after it times out, so the private endpoint may still have been created. List private endpoints and import it with `terraform import` instead of applying again.", statusCode)
}

// privateEndpointPricingChanged reports whether an update asks for different
// rates than state holds. Every PUT .../pricing inserts a new pricing version,
// so updates that only touch other attributes (such as activate) must not send
// one. Omitted pricing keeps the current rates.
func privateEndpointPricingChanged(ctx context.Context, req resource.UpdateRequest) (bool, diag.Diagnostics) {
	var planned, current types.Object
	diags := req.Plan.GetAttribute(ctx, path.Root("pricing"), &planned)
	diags.Append(req.State.GetAttribute(ctx, path.Root("pricing"), &current)...)
	if diags.HasError() || planned.IsNull() || planned.IsUnknown() {
		return false, diags
	}
	return !planned.Equal(current), diags
}

// refreshPrivateEndpointWithoutPricingWrite finishes an update that changes no
// pricing by reading the endpoint instead of writing a pricing version.
func (r *PrivateEndpointResource) refreshPrivateEndpointWithoutPricingWrite(ctx context.Context, plan types.Object, data *PrivateEndpointResourceModel, resp *resource.UpdateResponse) {
	request, requestDiags := data.ToOperationsGetPrivateEndpointRequest(ctx)
	resp.Diagnostics.Append(requestDiags...)

	if resp.Diagnostics.HasError() {
		return
	}
	res, err := r.client.PrivateEndpoints.Get(ctx, *request)
	if err != nil {
		resp.Diagnostics.AddError("failure to invoke API", err.Error())
		if res != nil && res.RawResponse != nil {
			resp.Diagnostics.AddError("unexpected http request/response", debugResponse(res.RawResponse))
		}
		return
	}
	if res == nil {
		resp.Diagnostics.AddError("unexpected response from API", fmt.Sprintf("%v", res))
		return
	}
	if res.StatusCode != 200 {
		resp.Diagnostics.AddError(fmt.Sprintf("unexpected response from API. Got an unexpected response code %v", res.StatusCode), debugResponse(res.RawResponse))
		return
	}
	if !(res.PrivateEndpointResponse != nil) {
		resp.Diagnostics.AddError("unexpected response from API. Got an unexpected response body", debugResponse(res.RawResponse))
		return
	}
	resp.Diagnostics.Append(data.RefreshFromSharedPrivateEndpointResponse(ctx, res.PrivateEndpointResponse)...)

	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(refreshPlan(ctx, plan, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
