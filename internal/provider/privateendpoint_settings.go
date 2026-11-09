package provider

import (
	"context"
	"fmt"

	tfTypes "github.com/OpenRouterTeam/terraform-provider-openrouter/internal/provider/types"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/operations"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/shared"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func privateEndpointSettingsFromAPI(settings *shared.PrivateEndpointSettings) *tfTypes.PrivateEndpointSettings {
	if settings == nil {
		return nil
	}
	return &tfTypes.PrivateEndpointSettings{
		ContextLength:   types.Int64PointerValue(settings.ContextLength),
		MaxPromptTokens: types.Int64PointerValue(settings.MaxPromptTokens),
	}
}

func privateEndpointSettingsDelta(planned, prior *tfTypes.PrivateEndpointSettings) *shared.PrivateEndpointSettings {
	if planned == nil {
		return nil
	}
	if prior == nil {
		prior = &tfTypes.PrivateEndpointSettings{}
	}
	changed := func(next, old types.Int64) *int64 {
		if next.IsNull() || next.IsUnknown() || next.Equal(old) {
			return nil
		}
		value := next.ValueInt64()
		return &value
	}
	delta := &shared.PrivateEndpointSettings{
		ContextLength:   changed(planned.ContextLength, prior.ContextLength),
		MaxPromptTokens: changed(planned.MaxPromptTokens, prior.MaxPromptTokens),
	}
	if delta.ContextLength == nil && delta.MaxPromptTokens == nil {
		return nil
	}
	return delta
}

func (r *PrivateEndpointResource) patchPrivateEndpointSettings(ctx context.Context, id string, settings *shared.PrivateEndpointSettings) diag.Diagnostics {
	var diags diag.Diagnostics
	if settings == nil {
		return diags
	}
	res, err := r.client.PrivateEndpoints.Update(ctx, operations.UpdatePrivateEndpointRequest{
		ID: id, Body: shared.UpdatePrivateEndpointRequest{Settings: settings},
	})
	if err != nil {
		diags.AddError("failure to patch private endpoint settings", redactSensitiveValues(ctx, err.Error()))
		return diags
	}
	if res == nil || res.StatusCode != 200 || res.PrivateEndpointResponse == nil || res.PrivateEndpointResponse.Data.ID != id {
		diags.AddError("unexpected private endpoint settings response", fmt.Sprintf("Expected HTTP 200 and endpoint %s.", id))
		return diags
	}
	actual := res.PrivateEndpointResponse.Data.Settings
	if actual == nil || (settings.ContextLength != nil && (actual.ContextLength == nil || *actual.ContextLength != *settings.ContextLength)) || (settings.MaxPromptTokens != nil && (actual.MaxPromptTokens == nil || *actual.MaxPromptTokens != *settings.MaxPromptTokens)) {
		diags.AddError("private endpoint settings were not acknowledged", "The PATCH response does not contain the requested limits.")
	}
	return diags
}
