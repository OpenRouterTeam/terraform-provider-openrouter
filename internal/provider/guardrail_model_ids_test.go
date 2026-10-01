package provider

import (
	"testing"

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
			strs("anthropic/claude-sonnet-5.5", "deepseek/deepseek-v4-flash-0731", "anthropic/claude-sonnet-5.5:batch"),
			strs("anthropic/claude-sonnet-5.5-20260928", "deepseek/deepseek-v4-flash-20260731"), true, true},
		{"variant stored with its suffix", strs("apodex/apodex-1.1-mini:free"), strs("apodex/apodex-1.1-mini-20261001:free"), true, true},
		{"variant stored without its suffix", strs("apodex/apodex-1.1-mini:free"), strs("apodex/apodex-1.1-mini-20261001"), true, true},
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
