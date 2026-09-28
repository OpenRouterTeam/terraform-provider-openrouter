package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/operations"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/shared"
)

func TestRetainedPrivateEndpointDraftID(t *testing.T) {
	response := func(status int, body string) *operations.CreatePrivateEndpointResponse {
		return &operations.CreatePrivateEndpointResponse{
			StatusCode:  status,
			RawResponse: &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))},
		}
	}
	for _, status := range []int{400, 404, 409, 422, 500, 502} {
		res := response(status, `{"error":{"code":1,"message":"failed"},"data":{"endpoint":{"id":"ep_retained"},"validation":null}}`)
		if got := retainedPrivateEndpointDraftID(res); got != "ep_retained" {
			t.Fatalf("status %d: retained draft id = %q, want ep_retained", status, got)
		}
		// The body stays readable for the error diagnostic.
		if body, _ := io.ReadAll(res.RawResponse.Body); !strings.Contains(string(body), "ep_retained") {
			t.Fatalf("status %d: body after parsing = %q, want it intact", status, body)
		}
	}

	for _, body := range []string{`{"error":{"code":422,"message":"invalid"}}`, `not json`, ``} {
		if got := retainedPrivateEndpointDraftID(response(422, body)); got != "" {
			t.Fatalf("retained draft id for %q = %q, want empty", body, got)
		}
	}
	if got := retainedPrivateEndpointDraftID(&operations.CreatePrivateEndpointResponse{StatusCode: 408}); got != "" {
		t.Fatalf("retained draft id without a raw response = %q, want empty", got)
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
