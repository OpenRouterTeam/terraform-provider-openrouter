package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func strs(v ...string) []types.String {
	out := make([]types.String, len(v))
	for i, s := range v {
		out[i] = types.StringValue(s)
	}
	return out
}

func TestKeepEquivalentModelIDs(t *testing.T) {
	canonical := map[string]string{
		"deepseek/deepseek-v4-flash-0731":   "deepseek/deepseek-v4-flash-20260731",
		"anthropic/claude-sonnet-5.5":       "anthropic/claude-sonnet-5.5-20260928",
		"anthropic/claude-sonnet-5.5:batch": "anthropic/claude-sonnet-5.5-20260928",
		"apodex/apodex-1.1-mini:free":       "apodex/apodex-1.1-mini-20261001",
	}
	tests := []struct {
		name       string
		prior, got []types.String
		wantPrior  bool
		wantLookup bool
	}{
		{"identical lists skip the lookup", strs("a/b"), strs("a/b"), false, false},
		{"slug resolved to canonical", strs("deepseek/deepseek-v4-flash-0731"), strs("deepseek/deepseek-v4-flash-20260731"), true, true},
		{"mixed slug and canonical keep order",
			strs("anthropic/claude-sonnet-5.5-20260928", "deepseek/deepseek-v4-flash-0731"),
			strs("anthropic/claude-sonnet-5.5-20260928", "deepseek/deepseek-v4-flash-20260731"), true, true},
		{"duplicates collapsed by the API",
			strs("anthropic/claude-sonnet-5.5", "deepseek/deepseek-v4-flash-0731", "anthropic/claude-sonnet-5.5-20260928"),
			strs("anthropic/claude-sonnet-5.5-20260928", "deepseek/deepseek-v4-flash-20260731"), true, true},
		{"variant stored with its suffix", strs("apodex/apodex-1.1-mini:free"), strs("apodex/apodex-1.1-mini-20261001:free"), true, true},
		{"variant without its suffix is drift", strs("apodex/apodex-1.1-mini:free"), strs("apodex/apodex-1.1-mini-20261001"), false, true},
		{"model and its variant are distinct",
			strs("anthropic/claude-sonnet-5.5", "anthropic/claude-sonnet-5.5:batch"),
			strs("anthropic/claude-sonnet-5.5-20260928", "anthropic/claude-sonnet-5.5-20260928:batch"), true, true},
		{"variant removed remotely is drift",
			strs("anthropic/claude-sonnet-5.5", "anthropic/claude-sonnet-5.5:batch"),
			strs("anthropic/claude-sonnet-5.5-20260928"), false, true},
		{"unknown id the API keeps as is", strs("old/delisted", "deepseek/deepseek-v4-flash-0731"), strs("old/delisted", "deepseek/deepseek-v4-flash-20260731"), true, true},
		{"reordered is drift",
			strs("deepseek/deepseek-v4-flash-0731", "anthropic/claude-sonnet-5.5"),
			strs("anthropic/claude-sonnet-5.5-20260928", "deepseek/deepseek-v4-flash-20260731"), false, true},
		{"different model is drift", strs("deepseek/deepseek-v4-flash-0731"), strs("deepseek/deepseek-v4-pro-20260813"), false, true},
		{"extra remote entry is drift", strs("deepseek/deepseek-v4-flash-0731"), strs("deepseek/deepseek-v4-flash-20260731", "x/y"), false, true},
		{"missing remote entry is drift", strs("deepseek/deepseek-v4-flash-0731", "anthropic/claude-sonnet-5.5"), strs("deepseek/deepseek-v4-flash-20260731"), false, true},
		{"no prior (import) keeps the response", nil, strs("deepseek/deepseek-v4-flash-20260731"), false, false},
		{"removed remotely keeps the response", strs("deepseek/deepseek-v4-flash-0731"), nil, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			looked := false
			out := keepEquivalentModelIDs(tt.prior, tt.got, func() map[string]string {
				looked = true
				return canonical
			})
			want := tt.got
			if tt.wantPrior {
				want = tt.prior
			}
			if !equalStringLists(out, want) || (out == nil) != (want == nil) {
				t.Fatalf("got %v, want %v", out, want)
			}
			if looked != tt.wantLookup {
				t.Fatalf("lookup called = %t, want %t", looked, tt.wantLookup)
			}
		})
	}
}

// Without the models list the API's values are stored, as before the fix.
func TestKeepEquivalentModelIDsWithoutLookup(t *testing.T) {
	got := strs("deepseek/deepseek-v4-flash-20260731")
	out := keepEquivalentModelIDs(strs("deepseek/deepseek-v4-flash-0731"), got, func() map[string]string { return map[string]string{} })
	if !equalStringLists(out, got) {
		t.Fatalf("got %v, want %v", out, got)
	}
}

// A failed models lookup is not cached: the next Read fetches again, and a
// successful fetch is reused.
func TestCanonicalModelSlugsRetriesAfterError(t *testing.T) {
	raw, err := os.ReadFile("../acceptance/testdata/guardrail_models.json")
	if err != nil {
		t.Fatal(err)
	}
	var models []json.RawMessage
	if err := json.Unmarshal(raw, &models); err != nil {
		t.Fatal(err)
	}
	page, _ := json.Marshal(map[string]any{"data": models, "total_count": len(models), "links": map[string]any{}})
	gets := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gets++
		w.Header().Set("Content-Type", "application/json")
		if gets == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":400,"message":"bad request"}}`))
			return
		}
		_, _ = w.Write(page)
	}))
	defer srv.Close()
	client := sdk.New(sdk.WithServerURL(srv.URL))

	if _, err := canonicalModelSlugs(context.Background(), client); err == nil {
		t.Fatal("first lookup succeeded, want the stub's error")
	}
	canonical, err := canonicalModelSlugs(context.Background(), client)
	if err != nil {
		t.Fatal(err)
	}
	if got := canonical["anthropic/claude-sonnet-5.5"]; got != "anthropic/claude-sonnet-5.5-20260928" {
		t.Fatalf("canonical slug = %q", got)
	}
	n := gets
	if _, err := canonicalModelSlugs(context.Background(), client); err != nil || gets != n {
		t.Fatalf("third lookup: err %v, %d new requests; want the cached map", err, gets-n)
	}
}

// keepConfiguredModelIDs is what Read calls after refreshing from the API: it
// restores both configured lists when the response is their canonical form,
// and keeps the API's values when the models lookup fails.
func TestKeepConfiguredModelIDs(t *testing.T) {
	raw, err := os.ReadFile("../acceptance/testdata/guardrail_models.json")
	if err != nil {
		t.Fatal(err)
	}
	var models []json.RawMessage
	if err := json.Unmarshal(raw, &models); err != nil {
		t.Fatal(err)
	}
	page, _ := json.Marshal(map[string]any{"data": models, "total_count": len(models), "links": map[string]any{}})
	fail := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if fail {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":400,"message":"bad request"}}`))
			return
		}
		_, _ = w.Write(page)
	}))
	defer srv.Close()
	r := &GuardrailResource{client: sdk.New(sdk.WithServerURL(srv.URL))}

	prior := GuardrailResourceModel{
		AllowedModels: strs("deepseek/deepseek-v4-flash-0731"),
		IgnoredModels: strs("anthropic/claude-sonnet-5.5"),
	}
	refreshed := func() *GuardrailResourceModel {
		return &GuardrailResourceModel{
			AllowedModels: strs("deepseek/deepseek-v4-flash-20260731"),
			IgnoredModels: strs("anthropic/claude-sonnet-5.5-20260928"),
		}
	}

	data := refreshed()
	if diags := r.keepConfiguredModelIDs(context.Background(), &prior, data); diags.HasError() {
		t.Fatal(diags)
	}
	if want := refreshed(); !equalStringLists(data.AllowedModels, want.AllowedModels) || !equalStringLists(data.IgnoredModels, want.IgnoredModels) {
		t.Fatalf("failed lookup: got %v / %v, want the API's values", data.AllowedModels, data.IgnoredModels)
	}

	fail = false
	data = refreshed()
	if diags := r.keepConfiguredModelIDs(context.Background(), &prior, data); diags.HasError() {
		t.Fatal(diags)
	}
	if !equalStringLists(data.AllowedModels, prior.AllowedModels) || !equalStringLists(data.IgnoredModels, prior.IgnoredModels) {
		t.Fatalf("got %v / %v, want the configured ids", data.AllowedModels, data.IgnoredModels)
	}
}
