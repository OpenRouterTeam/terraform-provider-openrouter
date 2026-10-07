package acceptance

import (
	"context"
	"testing"

	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/operations"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/shared"
)

// A partial SDK update outside the resource must keep every field it leaves
// out; only the resource asks for explicit nulls.
func TestStubPrivateProviderPartialSDKUpdateKeepsOmittedFields(t *testing.T) {
	srv, api := newStubPrivateProviders(t)
	api.providers["acme"] = map[string]any{
		"name":               "Acme",
		"slug":               "acme",
		"display_name":       "Acme",
		"base_url":           "https://inference.example.com/v1",
		"privacy_policy_url": "https://example.com/privacy",
		"headquarters":       "US",
		"datacenters":        []any{},
		"data_policy":        map[string]any{"training": false, "retains_prompts": true, "prompt_retention_days": float64(30)},
		"created_at":         "2026-10-07T00:00:00.000Z",
	}

	apiKey := "test-key"
	client := sdk.New(sdk.WithServerURL(srv.URL), sdk.WithSecurity(shared.Security{APIKey: &apiKey}))
	displayName, training := "Acme Renamed", true
	_, err := client.PrivateProviders.Update(context.Background(), operations.UpdatePrivateProviderRequest{
		Slug: "acme",
		Body: shared.UpdatePrivateProviderRequest{
			DisplayName: &displayName,
			DataPolicy:  &shared.UpdatePrivateProviderRequestDataPolicy{Training: &training},
		},
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	stored := api.providers["acme"]
	policy, _ := stored["data_policy"].(map[string]any)
	for field, got := range map[string]any{
		"display_name":                      stored["display_name"],
		"headquarters":                      stored["headquarters"],
		"privacy_policy_url":                stored["privacy_policy_url"],
		"data_policy.training":              policy["training"],
		"data_policy.prompt_retention_days": policy["prompt_retention_days"],
	} {
		want := map[string]any{
			"display_name":                      "Acme Renamed",
			"headquarters":                      "US",
			"privacy_policy_url":                "https://example.com/privacy",
			"data_policy.training":              true,
			"data_policy.prompt_retention_days": float64(30),
		}[field]
		if got != want {
			t.Errorf("stored %s = %v, want %v", field, got, want)
		}
	}
}
