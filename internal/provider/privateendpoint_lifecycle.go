//lint:file-ignore U1000 Called only from persistent edits to privateendpoint_resource.go, which the generator lints before applying them.

package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	custom_stringplanmodifier "github.com/OpenRouterTeam/terraform-provider-openrouter/internal/planmodifiers/stringplanmodifier"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/operations"
	"github.com/hashicorp/go-uuid"
	"github.com/hashicorp/terraform-plugin-framework/attr"
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
	if server == nil {
		return prior
	}
	normalized := custom_stringplanmodifier.NormalizePrivateEndpointBaseURL(*server)
	if prior.IsUnknown() || prior.IsNull() {
		return types.StringValue(normalized)
	}
	if custom_stringplanmodifier.NormalizePrivateEndpointBaseURL(prior.ValueString()) == normalized {
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

// privateEndpointCreateConflictDetail explains a 409 on create. The API
// answers 409 when the endpoint it activated is disabled, or when validation
// is stale or never passed, not when a matching endpoint already exists, so
// importing is not the fix.
func privateEndpointCreateConflictDetail(res *operations.CreatePrivateEndpointResponse) string {
	const detail = "The API could not activate the private endpoint: it is disabled, its validation is stale, or it never passed validation. This is not an existing endpoint to import. Check the validation workspace and its BYOK key, then apply again."
	reason := privateEndpointErrorMessage(res)
	if reason == "" {
		return detail
	}
	return fmt.Sprintf("%s API reason: %s.", detail, reason)
}

// privateEndpointErrorMessage returns error.message from a create response
// body, leaving the body readable.
func privateEndpointErrorMessage(res *operations.CreatePrivateEndpointResponse) string {
	if res == nil || res.RawResponse == nil || res.RawResponse.Body == nil {
		return ""
	}
	raw, err := io.ReadAll(res.RawResponse.Body)
	res.RawResponse.Body = io.NopCloser(bytes.NewReader(raw))
	if err != nil {
		return ""
	}
	var body struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &body) != nil {
		return ""
	}
	return body.Error.Message
}

type privateEndpointChanges struct {
	pricing, settings, declaredParameters bool
}

// privateEndpointChangesForUpdate compares the mutable groups with prior state.
// Null or unknown groups do not request a write. Unknown nested computed values
// keep their state value, so an unrelated change must not rewrite pricing.
func privateEndpointChangesForUpdate(ctx context.Context, req resource.UpdateRequest) (privateEndpointChanges, diag.Diagnostics) {
	var planned, current types.Object
	diags := req.Plan.Get(ctx, &planned)
	diags.Append(req.State.Get(ctx, &current)...)
	var changes privateEndpointChanges
	if diags.HasError() {
		return changes, diags
	}
	plannedAttributes, currentAttributes := planned.Attributes(), current.Attributes()
	for name, changed := range map[string]*bool{
		"pricing": &changes.pricing, "settings": &changes.settings, "declared_parameters": &changes.declaredParameters,
	} {
		value := plannedAttributes[name]
		*changed = !value.IsNull() && privateEndpointValueChanged(value, currentAttributes[name])
	}
	return changes, diags
}

func privateEndpointValueChanged(planned, current attr.Value) bool {
	if planned.IsUnknown() || planned.Equal(current) {
		return false
	}
	switch value := planned.(type) {
	case types.Object:
		prior, ok := current.(types.Object)
		if !ok || value.IsNull() || prior.IsNull() || prior.IsUnknown() {
			return true
		}
		priorAttributes := prior.Attributes()
		for name, attribute := range value.Attributes() {
			if privateEndpointValueChanged(attribute, priorAttributes[name]) {
				return true
			}
		}
		return false
	case types.List:
		prior, ok := current.(types.List)
		if !ok || value.IsNull() || prior.IsNull() || prior.IsUnknown() || len(value.Elements()) != len(prior.Elements()) {
			return true
		}
		priorElements := prior.Elements()
		for i, element := range value.Elements() {
			if privateEndpointValueChanged(element, priorElements[i]) {
				return true
			}
		}
		return false
	default:
		return true
	}
}

// declaredParametersWithProven restores declared parameters that a validation run
// has since proven. The detail response returns declared_parameters as stored minus
// tested_parameters, so a proven name disappears from the read and Terraform would
// propose adding it again on every plan.
func declaredParametersWithProven(prior, declared, tested []types.String) []types.String {
	proven := make(map[string]struct{}, len(tested))
	for _, name := range tested {
		if name.IsNull() || name.IsUnknown() {
			continue
		}
		proven[name.ValueString()] = struct{}{}
	}

	out := make([]types.String, 0, len(declared)+len(prior))
	seen := make(map[string]struct{}, len(declared)+len(prior))
	for _, value := range declared {
		if value.IsNull() || value.IsUnknown() {
			continue
		}
		name := value.ValueString()
		if _, ok := seen[name]; ok {
			continue
		}
		out = append(out, types.StringValue(name))
		seen[name] = struct{}{}
	}
	for _, value := range prior {
		if value.IsNull() || value.IsUnknown() {
			continue
		}
		name := value.ValueString()
		if _, ok := proven[name]; !ok {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		out = append(out, types.StringValue(name))
		seen[name] = struct{}{}
	}
	return out
}

// refreshPrivateEndpointWithoutPricingWrite settles state with a full GET,
// either when no mutable group changed or after a successful PATCH. A limited
// PATCH response must not erase computed attributes missing from that response.
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
	priorDeclaredParameters := data.DeclaredParameters
	priorBaseURL := data.BaseURL
	priorProviderSlug := data.ProviderSlug

	// Copy the plan's own attributes in first, then let the API's answer win. The
	// reverse order needed a snapshot of the server truth, but `PrivateEndpointPricing`
	// and `PrivateEndpointSettings` are pointers and the framework's reflect reuses a
	// non-nil pointer and mutates the struct in place, so a value copy of the model
	// aliased them and restored nothing.
	resp.Diagnostics.Append(refreshPlan(ctx, plan, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(data.RefreshFromSharedPrivateEndpointResponse(ctx, res.PrivateEndpointResponse)...)

	if resp.Diagnostics.HasError() {
		return
	}

	data.BaseURL = privateEndpointBaseURL(priorBaseURL, res.PrivateEndpointResponse.Data.BaseURL)
	// The provider catalogue can stop listing a provider we already recorded;
	// keep the configured slug instead of dropping into a replacement.
	if data.ProviderSlug.IsNull() && !priorProviderSlug.IsNull() && !priorProviderSlug.IsUnknown() {
		data.ProviderSlug = priorProviderSlug
	}
	data.DeclaredParameters = declaredParametersWithProven(priorDeclaredParameters, data.DeclaredParameters, data.TestedParameters)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// privateEndpointPricingChanged is the pre-PATCH guard the generated update body
// still calls. The custom dispatch returns before it runs, and it stays so the
// generated code compiles on the regeneration that records the custom path.
func privateEndpointPricingChanged(ctx context.Context, req resource.UpdateRequest) (bool, diag.Diagnostics) {
	var planned, current types.Object
	diags := req.Plan.GetAttribute(ctx, path.Root("pricing"), &planned)
	diags.Append(req.State.GetAttribute(ctx, path.Root("pricing"), &current)...)
	if diags.HasError() || planned.IsNull() || planned.IsUnknown() {
		return false, diags
	}
	return !planned.Equal(current), diags
}
