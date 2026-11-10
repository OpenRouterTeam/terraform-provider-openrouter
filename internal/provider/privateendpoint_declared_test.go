package provider

import (
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestDeclaredParametersWithProven(t *testing.T) {
	t.Parallel()

	str := func(values ...string) []types.String {
		out := make([]types.String, 0, len(values))
		for _, v := range values {
			out = append(out, types.StringValue(v))
		}
		return out
	}

	cases := []struct {
		name     string
		prior    []types.String
		declared []types.String
		tested   []types.String
		want     []string
	}{
		{
			name:     "name promoted to tested is restored",
			prior:    str("temperature"),
			declared: nil,
			tested:   str("temperature"),
			want:     []string{"temperature"},
		},
		{
			name:     "name absent from tested is not restored",
			prior:    str("temperature"),
			declared: nil,
			tested:   str("tools"),
			want:     nil,
		},
		{
			name:     "configured order is kept while duplicates collapse",
			prior:    str("seed", "stop", "seed"),
			declared: str("stop"),
			tested:   str("seed", "stop"),
			want:     []string{"seed", "stop"},
		},
		{
			name:     "a proven name keeps its configured position",
			prior:    str("tools", "temperature"),
			declared: str("temperature"),
			tested:   str("tools"),
			want:     []string{"tools", "temperature"},
		},
		{
			name:     "a name the API returned but the intended value omits follows it",
			prior:    str("temperature"),
			declared: str("temperature", "include_reasoning"),
			tested:   nil,
			want:     []string{"temperature", "include_reasoning"},
		},
		{
			name:     "null and unknown entries are ignored",
			prior:    []types.String{types.StringNull(), types.StringUnknown(), types.StringValue("seed")},
			declared: []types.String{types.StringNull()},
			tested:   []types.String{types.StringNull(), types.StringValue("seed")},
			want:     []string{"seed"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := declaredParametersWithProven(tc.prior, tc.declared, tc.tested)
			out := make([]string, 0, len(got))
			for _, value := range got {
				out = append(out, value.ValueString())
			}
			if len(out) != len(tc.want) {
				t.Fatalf("got %v, want %v", out, tc.want)
			}
			for i := range out {
				if out[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", out, tc.want)
				}
			}
			for _, value := range got {
				if value.IsNull() || value.IsUnknown() {
					t.Fatalf("result carries a null or unknown entry: %v", got)
				}
			}
			if !reflect.DeepEqual(parametersSet(out), parametersSet(tc.want)) {
				t.Fatalf("membership mismatch: got %v, want %v", out, tc.want)
			}
		})
	}
}

func parametersSet(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		out[value] = struct{}{}
	}
	return out
}
