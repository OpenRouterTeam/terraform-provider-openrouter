package explicitnull

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func applyBody(t *testing.T, ctx context.Context, body string) string {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, "https://example.com", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	out, err := Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	raw, err := io.ReadAll(out.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestApply(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		fields           []string
	}{
		{name: "no fields leaves the body alone", body: `{"a":1}`, want: `{"a":1}`},
		{name: "top-level field", body: `{"a":1}`, fields: []string{"b"}, want: `{"a":1,"b":null}`},
		{name: "nested field in a sent object", body: `{"p":{"x":true}}`, fields: []string{"p.y"}, want: `{"p":{"x":true,"y":null}}`},
		{name: "nested field overrides a sent value", body: `{"p":{"y":3}}`, fields: []string{"p.y"}, want: `{"p":{"y":null}}`},
		{name: "nested field without its parent is skipped", body: `{"a":1}`, fields: []string{"p.y"}, want: `{"a":1}`},
		{name: "nested field under a null parent is skipped", body: `{"p":null}`, fields: []string{"p.y"}, want: `{"p":null}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := applyBody(t, WithFields(context.Background(), tc.fields...), tc.body); got != tc.want {
				t.Errorf("body = %s, want %s", got, tc.want)
			}
		})
	}
}
