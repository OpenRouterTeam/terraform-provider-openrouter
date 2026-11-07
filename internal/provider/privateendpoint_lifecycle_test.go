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

func TestDeclaredParameterListChanged(t *testing.T) {
	list := func(values ...string) types.List {
		elements := make([]attr.Value, 0, len(values))
		for _, value := range values {
			elements = append(elements, types.StringValue(value))
		}
		return types.ListValueMust(types.StringType, elements)
	}
	cases := []struct {
		name    string
		planned types.List
		current types.List
		want    bool
	}{
		{"unset plan keeps the API value", types.ListNull(types.StringType), list("seed"), false},
		{"unknown plan keeps the API value", types.ListUnknown(types.StringType), list("seed"), false},
		{"same list", list("seed", "tools"), list("seed", "tools"), false},
		{"added parameter", list("seed", "tools"), list("seed"), true},
		{"cleared list", list(), list("seed"), true},
		{"reordered list", list("tools", "seed"), list("seed", "tools"), true},
	}
	for _, tc := range cases {
		if got := declaredParameterListChanged(tc.planned, tc.current); got != tc.want {
			t.Errorf("%s: changed = %v, want %v", tc.name, got, tc.want)
		}
	}
}
