package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/operations"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/shared"
)

func TestRetainedPrivateEndpointDraftID(t *testing.T) {
	retained := &shared.CreatePrivateEndpointValidationFailedResponse{}
	retained.Data.Endpoint.ID = "ep_retained"
	res := &operations.CreatePrivateEndpointResponse{
		StatusCode: 422,
		FourHundredAndTwentyTwoApplicationJSONOneOf: &operations.CreatePrivateEndpointUnprocessableEntityResponseBody{
			CreatePrivateEndpointValidationFailedResponse: retained,
		},
	}
	if got := retainedPrivateEndpointDraftID(res); got != "ep_retained" {
		t.Fatalf("retained draft id = %q, want ep_retained", got)
	}

	plainError := &operations.CreatePrivateEndpointResponse{
		StatusCode: 422,
		FourHundredAndTwentyTwoApplicationJSONOneOf: &operations.CreatePrivateEndpointUnprocessableEntityResponseBody{
			UnprocessableEntityResponse: &shared.UnprocessableEntityResponse{},
		},
	}
	if got := retainedPrivateEndpointDraftID(plainError); got != "" {
		t.Fatalf("retained draft id for a pre-create error = %q, want empty", got)
	}
}

func TestDeleteRetainedPrivateEndpointDraftReportsFailure(t *testing.T) {
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":{"code":409,"message":"endpoint is active"}}`))
	}))
	defer server.Close()

	apiKey := "test"
	client := sdk.New(sdk.WithServerURL(server.URL), sdk.WithSecurity(shared.Security{APIKey: &apiKey}))
	diags := deleteRetainedPrivateEndpointDraft(context.Background(), client, "ep_retained")

	if gotQuery != "draft_only=true" {
		t.Fatalf("delete query = %q, want draft_only=true", gotQuery)
	}
	if !diags.HasError() || !strings.Contains(diags.Errors()[0].Detail(), "ep_retained") {
		t.Fatalf("diagnostics = %v, want an error naming the stranded draft", diags)
	}
}
