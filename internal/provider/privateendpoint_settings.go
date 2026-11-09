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
		ContextLength: types.Int64PointerValue(settings.ContextLength),
	}
}

func privateEndpointSettingsDelta(planned, prior *tfTypes.PrivateEndpointSettings) *shared.PrivateEndpointSettings {
	if planned == nil || planned.ContextLength.IsNull() || planned.ContextLength.IsUnknown() {
		return nil
	}
	if prior != nil && planned.ContextLength.Equal(prior.ContextLength) {
		return nil
	}
	value := planned.ContextLength.ValueInt64()
	return &shared.PrivateEndpointSettings{ContextLength: &value}
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
	if actual == nil || (settings.ContextLength != nil && (actual.ContextLength == nil || *actual.ContextLength != *settings.ContextLength)) {
		diags.AddError("private endpoint settings were not acknowledged", "The PATCH response does not contain the requested limits.")
	}
	return diags
}
