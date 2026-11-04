package provider

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/operations"
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
