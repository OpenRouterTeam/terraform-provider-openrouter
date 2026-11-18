package provider

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
)

// Each fixture holds an independent input and the original hand mapper's output.
// Keeping values, not a second mapper, removes the duplicated implementation from tests too.
type guardrailConfigFixture[Input, Output any] struct {
	name  string
	input Input
	want  Output
}

func guardrailConfigFixturePtr[T any](value T) *T {
	return &value
}

// convert is a wrapper pair's own shape: one value in, one value plus the diagnostics
// the generated method reported. Asserting the diagnostics are empty is what makes a
// generation that starts reporting one fail here, rather than being carried into state.
func checkGuardrailConfigFixtures[Input, Output any](t *testing.T, cases []guardrailConfigFixture[Input, Output], convert func(Input) (Output, diag.Diagnostics)) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, diags := convert(tc.input)
			if diags.HasError() {
				t.Fatalf("config conversion reported diagnostics: %v", diags)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("config differs from legacy mapping: %s", guardrailConfigDifference("Config", reflect.ValueOf(got), reflect.ValueOf(tc.want)))
			}
		})
	}
}

// Report the first differing field without collapsing nil and empty collections.
func guardrailConfigDifference(path string, got, want reflect.Value) string {
	if got.Kind() == reflect.Pointer && !got.IsNil() && !want.IsNil() {
		return guardrailConfigDifference(path, got.Elem(), want.Elem())
	}
	if got.Kind() == reflect.Struct {
		for i := 0; i < got.NumField(); i++ {
			if got.Type().Field(i).PkgPath != "" {
				break // Framework scalar internals are unexported; print the value instead.
			}
			if !reflect.DeepEqual(got.Field(i).Interface(), want.Field(i).Interface()) {
				return guardrailConfigDifference(path+"."+got.Type().Field(i).Name, got.Field(i), want.Field(i))
			}
		}
	}
	if got.Kind() == reflect.Slice && !got.IsNil() && !want.IsNil() && got.Len() == want.Len() {
		for i := 0; i < got.Len(); i++ {
			if !reflect.DeepEqual(got.Index(i).Interface(), want.Index(i).Interface()) {
				return guardrailConfigDifference(fmt.Sprintf("%s[%d]", path, i), got.Index(i), want.Index(i))
			}
		}
	}
	return fmt.Sprintf("%s: got %#v; want %#v", path, got.Interface(), want.Interface())
}
