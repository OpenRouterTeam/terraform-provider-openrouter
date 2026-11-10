package provider

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/operations"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestPrivateEndpointCreateConflictDetail(t *testing.T) {
	response := func(body string) *operations.CreatePrivateEndpointResponse {
		return &operations.CreatePrivateEndpointResponse{
			StatusCode:  409,
			RawResponse: &http.Response{StatusCode: 409, Body: io.NopCloser(strings.NewReader(body))},
		}
	}

	got := privateEndpointCreateConflictDetail(response(`{"error":{"code":409,"message":"validation_stale"}}`))
	if !strings.HasSuffix(got, "API reason: validation_stale.") {
		t.Fatalf("detail = %q, want the API reason", got)
	}
	if strings.Contains(got, "terraform import") {
		t.Fatalf("detail = %q, must not suggest terraform import", got)
	}

	for _, body := range []string{`not json`, `{}`, ``} {
		if got := privateEndpointCreateConflictDetail(response(body)); strings.Contains(got, "API reason") {
			t.Fatalf("detail for %q = %q, want no API reason", body, got)
		}
	}
	if got := privateEndpointCreateConflictDetail(&operations.CreatePrivateEndpointResponse{StatusCode: 409}); got == "" {
		t.Fatal("detail without a raw response is empty")
	}
}

func TestPrivateEndpointValueChanged(t *testing.T) {
	pricingTypes := map[string]attr.Type{"prompt": types.StringType, "cache_read": types.StringType}
	pricing := func(prompt, cacheRead types.String) types.Object {
		return types.ObjectValueMust(pricingTypes, map[string]attr.Value{"prompt": prompt, "cache_read": cacheRead})
	}
	currentPricing := pricing(types.StringValue("0.000002"), types.StringValue("0.0000005"))
	inputTypes := map[string]attr.Type{"audio": types.BoolType, "image_urls": types.BoolType}
	settingsTypes := map[string]attr.Type{"inputs": types.ObjectType{AttrTypes: inputTypes}, "max_completion_tokens": types.Int64Type}
	settings := func(audio types.Bool, limit types.Int64) types.Object {
		return types.ObjectValueMust(settingsTypes, map[string]attr.Value{
			"inputs": types.ObjectValueMust(inputTypes, map[string]attr.Value{
				"audio": audio, "image_urls": types.BoolUnknown(),
			}),
			"max_completion_tokens": limit,
		})
	}
	currentSettings := types.ObjectValueMust(settingsTypes, map[string]attr.Value{
		"inputs": types.ObjectValueMust(inputTypes, map[string]attr.Value{
			"audio": types.BoolValue(false), "image_urls": types.BoolValue(true),
		}),
		"max_completion_tokens": types.Int64Value(4096),
	})
	parameters := types.ListValueMust(types.StringType, []attr.Value{types.StringValue("temperature")})
	for _, test := range []struct {
		name             string
		planned, current attr.Value
		changed          bool
	}{
		{"same pricing", currentPricing, currentPricing, false},
		{"unknown pricing", types.ObjectUnknown(pricingTypes), currentPricing, false},
		{"unknown computed rate", pricing(types.StringValue("0.000002"), types.StringUnknown()), currentPricing, false},
		{"changed prompt", pricing(types.StringValue("0.0000015"), types.StringUnknown()), currentPricing, true},
		{"changed cache rate", pricing(types.StringValue("0.000002"), types.StringValue("0.0000004")), currentPricing, true},
		{"new pricing", currentPricing, types.ObjectNull(pricingTypes), true},
		{"unknown settings", types.ObjectUnknown(settingsTypes), currentSettings, false},
		{"unknown computed setting", settings(types.BoolUnknown(), types.Int64Value(4096)), currentSettings, false},
		{"changed nested setting", settings(types.BoolValue(true), types.Int64Value(4096)), currentSettings, true},
		{"changed limit", settings(types.BoolValue(false), types.Int64Value(8192)), currentSettings, true},
		{"cleared limit", settings(types.BoolValue(false), types.Int64Null()), currentSettings, true},
		{"same list", parameters, parameters, false},
		{"unknown list", types.ListUnknown(types.StringType), parameters, false},
		{"unknown list entry", types.ListValueMust(types.StringType, []attr.Value{types.StringUnknown()}), parameters, false},
		{"changed list", types.ListValueMust(types.StringType, []attr.Value{types.StringValue("seed")}), parameters, true},
		{"cleared list", types.ListValueMust(types.StringType, []attr.Value{}), parameters, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := privateEndpointValueChanged(test.planned, test.current); got != test.changed {
				t.Fatalf("changed = %t, want %t", got, test.changed)
			}
		})
	}
}
