package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	tfTypes "github.com/OpenRouterTeam/terraform-provider-openrouter/internal/provider/types"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/shared"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestPrivateEndpointSettingsPatch(t *testing.T) {
	for _, test := range []struct {
		name        string
		next, prior *tfTypes.PrivateEndpointSettings
		want        string
	}{
		{name: "omitted"},
		{name: "removed", prior: &tfTypes.PrivateEndpointSettings{ContextLength: types.Int64Value(1000000)}},
		{name: "unchanged", next: &tfTypes.PrivateEndpointSettings{ContextLength: types.Int64Value(1000000)}, prior: &tfTypes.PrivateEndpointSettings{ContextLength: types.Int64Value(1000000)}},
		{name: "unknown", next: &tfTypes.PrivateEndpointSettings{ContextLength: types.Int64Unknown()}},
		{name: "context-only", next: &tfTypes.PrivateEndpointSettings{ContextLength: types.Int64Value(1000000)}, prior: &tfTypes.PrivateEndpointSettings{ContextLength: types.Int64Value(262144), MaxPromptTokens: types.Int64Value(250000)}, want: `{"settings":{"context_length":1000000}}`},
		{name: "context-and-prompt", next: &tfTypes.PrivateEndpointSettings{ContextLength: types.Int64Value(1000000), MaxPromptTokens: types.Int64Value(999968)}, want: `{"settings":{"context_length":1000000,"max_prompt_tokens":999968}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				calls++
				if req.Method != "PATCH" || req.URL.Path != "/private-endpoints/ep_existing" {
					t.Errorf("unexpected request: %s %s", req.Method, req.URL.Path)
				}
				var body map[string]any
				if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				raw, _ := json.Marshal(body)
				if string(raw) != test.want {
					t.Errorf("PATCH = %s, want %s", raw, test.want)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"data":{"id":"ep_existing","status":"active","model_permaslug":"openai/gpt-4o-2024-08-06","model_slug":null,"model_name":null,"provider_name":"OpenAI","provider_slug":"openai","base_url":"https://api.openai.com/v1","upstream_model_id":"gpt-4o-2024-08-06","declared_zdr":null,"declared_region":null,"pricing":{"prompt":"0","completion":"0"},"settings":{"context_length":1000000,"max_prompt_tokens":999968}}}`))
			}))
			defer server.Close()
			key := "test"
			r := PrivateEndpointResource{client: sdk.New(sdk.WithServerURL(server.URL), sdk.WithSecurity(shared.Security{APIKey: &key}))}
			diags := r.patchPrivateEndpointSettings(context.Background(), "ep_existing", privateEndpointSettingsDelta(test.next, test.prior))
			if diags.HasError() {
				t.Fatal(diags)
			}
			expectedCalls := 0
			if test.want != "" {
				expectedCalls = 1
			}
			if calls != expectedCalls {
				t.Fatalf("calls = %d, want %d", calls, expectedCalls)
			}
		})
	}
}

func TestPrivateEndpointSettingsReadback(t *testing.T) {
	var endpoint shared.PrivateEndpoint
	err := json.Unmarshal([]byte(`{"settings":{"context_length":1000000,"max_prompt_tokens":250000,"max_completion_tokens":32768}}`), &endpoint)
	if err != nil {
		t.Fatal(err)
	}
	var resource PrivateEndpointResourceModel
	if diags := resource.RefreshFromSharedPrivateEndpoint(context.Background(), &endpoint); diags.HasError() {
		t.Fatal(diags)
	}
	if resource.Settings.ContextLength.ValueInt64() != 1000000 || resource.Settings.MaxPromptTokens.ValueInt64() != 250000 {
		t.Fatalf("limits not read back: %+v", resource.Settings)
	}
	resource.RefreshFromSharedPrivateEndpoint(context.Background(), &shared.PrivateEndpoint{})
	if resource.Settings != nil {
		t.Fatal("missing API settings must clear stale observed state")
	}
}

func TestPrivateEndpointSettingsRejectError(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404, 409, 422} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":{"code":400,"message":"rejected"}}`))
		}))
		key := "test"
		r := PrivateEndpointResource{client: sdk.New(sdk.WithServerURL(server.URL), sdk.WithSecurity(shared.Security{APIKey: &key}))}
		limit := int64(1000000)
		if diags := r.patchPrivateEndpointSettings(context.Background(), "ep_existing", &shared.PrivateEndpointSettings{ContextLength: &limit}); !diags.HasError() {
			t.Errorf("HTTP %d must fail", status)
		}
		server.Close()
	}
}

func TestPrivateEndpointSettingsRejectUnacknowledgedLimits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"ep_existing","status":"active","settings":{"context_length":262144}}}`))
	}))
	defer server.Close()
	key := "test"
	r := PrivateEndpointResource{client: sdk.New(sdk.WithServerURL(server.URL), sdk.WithSecurity(shared.Security{APIKey: &key}))}
	limit := int64(1000000)
	if diags := r.patchPrivateEndpointSettings(context.Background(), "ep_existing", &shared.PrivateEndpointSettings{ContextLength: &limit}); !diags.HasError() {
		t.Fatal("HTTP 200 with the old limit must fail")
	}
}

func TestPrivateEndpointSettingsUpdatePreservesObservedPromptCap(t *testing.T) {
	methods := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"ep_existing","status":"active","model_permaslug":"openai/gpt-4o-2024-08-06","model_slug":null,"model_name":null,"provider_name":"OpenAI","provider_slug":"openai","base_url":"https://api.openai.com/v1","upstream_model_id":"gpt-4o-2024-08-06","declared_zdr":null,"declared_region":null,"pricing":{"prompt":"0","completion":"0"},"settings":{"context_length":1000000,"max_prompt_tokens":250000}}}`))
	}))
	defer server.Close()
	key := "test"
	r := PrivateEndpointResource{client: sdk.New(sdk.WithServerURL(server.URL), sdk.WithSecurity(shared.Security{APIKey: &key}))}
	ctx := context.Background()
	var schemaResponse resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResponse)
	previous := &PrivateEndpointResourceModel{
		ID:              types.StringValue("ep_existing"),
		ModelPermaslug:  types.StringValue("openai/gpt-4o-2024-08-06"),
		ProviderSlug:    types.StringValue("openai"),
		UpstreamModelID: types.StringValue("gpt-4o-2024-08-06"),
		BaseURL:         types.StringValue("https://api.openai.com/v1"),
		Status:          types.StringValue("active"),
		Pricing:         &tfTypes.PrivateEndpointPricing{Prompt: types.StringValue("0"), Completion: types.StringValue("0")},
		Settings:        &tfTypes.PrivateEndpointSettings{ContextLength: types.Int64Value(262144), MaxPromptTokens: types.Int64Value(250000)},
	}
	prior := tfsdk.State{Schema: schemaResponse.Schema}
	if diags := prior.Set(ctx, previous); diags.HasError() {
		t.Fatal(diags)
	}
	planned := *previous
	planned.Settings = &tfTypes.PrivateEndpointSettings{ContextLength: types.Int64Value(1000000), MaxPromptTokens: types.Int64Unknown()}
	next := tfsdk.State{Schema: schemaResponse.Schema}
	if diags := next.Set(ctx, &planned); diags.HasError() {
		t.Fatal(diags)
	}
	response := resource.UpdateResponse{State: prior}
	r.Update(ctx, resource.UpdateRequest{State: prior, Plan: tfsdk.Plan{Schema: schemaResponse.Schema, Raw: next.Raw}}, &response)
	if response.Diagnostics.HasError() {
		t.Fatal(response.Diagnostics)
	}
	var observed *PrivateEndpointResourceModel
	if diags := response.State.Get(ctx, &observed); diags.HasError() {
		t.Fatal(diags)
	}
	if observed.Settings.ContextLength.ValueInt64() != 1000000 || observed.Settings.MaxPromptTokens.IsUnknown() || observed.Settings.MaxPromptTokens.ValueInt64() != 250000 {
		t.Fatalf("lost API readback: %+v", observed.Settings)
	}
	if len(methods) != 2 || methods[0] != "PATCH /private-endpoints/ep_existing" || methods[1] != "GET /private-endpoints/ep_existing" {
		t.Fatalf("unexpected requests: %v", methods)
	}
}
