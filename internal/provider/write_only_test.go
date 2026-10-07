package provider

import (
	"context"
	"reflect"
	"strings"
	"testing"

	tfTypes "github.com/OpenRouterTeam/terraform-provider-openrouter/internal/provider/types"
	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

const writeOnlyMarker = "tf-synthetic-secret-marker"

// fillObservabilityConfigBlocks sets every field of every typed `config` block
// reachable from v to the marker, allocating the blocks as needed.
func fillObservabilityConfigBlocks(v reflect.Value) {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return
		}
		fillObservabilityConfigBlocks(v.Elem())
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			fillObservabilityConfigBlocks(v.Index(i))
		}
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < t.NumField(); i++ {
			name := tfsdkName(t.Field(i))
			field := v.Field(i)
			if name != "" && field.Kind() == reflect.Slice {
				fillObservabilityConfigBlocks(field)
				continue
			}
			if name == "" || field.Kind() != reflect.Pointer || field.Type().Elem().Kind() != reflect.Struct {
				continue
			}
			if name != "config" && !strings.Contains(field.Type().Elem().Name(), "Destination") {
				continue
			}
			field.Set(reflect.New(field.Type().Elem()))
			if name == "config" {
				fillConfigStruct(field.Elem())
			} else {
				fillObservabilityConfigBlocks(field)
			}
		}
	}
}

func fillConfigStruct(v reflect.Value) {
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		switch field.Type() {
		case reflect.TypeOf(types.String{}):
			field.Set(reflect.ValueOf(types.StringValue(writeOnlyMarker)))
		case reflect.TypeOf(types.Bool{}):
			field.Set(reflect.ValueOf(types.BoolValue(true)))
		case reflect.TypeOf(map[string]types.String{}):
			field.Set(reflect.ValueOf(map[string]types.String{"Authorization": types.StringValue(writeOnlyMarker)}))
		}
	}
}

// configBlocks returns every typed `config` struct reachable from v.
func configBlocks(v reflect.Value) []reflect.Value {
	var out []reflect.Value
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			out = append(out, configBlocks(v.Elem())...)
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			out = append(out, configBlocks(v.Index(i))...)
		}
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < t.NumField(); i++ {
			name := tfsdkName(t.Field(i))
			field := v.Field(i)
			if name == "config" && field.Kind() == reflect.Pointer && !field.IsNil() && field.Elem().Kind() == reflect.Struct {
				out = append(out, field.Elem())
			} else if name != "" {
				out = append(out, configBlocks(field)...)
			}
		}
	}
	return out
}

// isUnset reports whether a config field is null, whatever its Go type.
func isUnset(v reflect.Value) bool {
	switch x := v.Interface().(type) {
	case types.String:
		return x.IsNull()
	case types.Bool:
		return x.IsNull()
	case map[string]types.String:
		return x == nil
	}
	panic("unexpected config field type " + v.Type().String())
}

func TestScrubObservabilityConfigBlocks(t *testing.T) {
	declared := map[string]jsontypes.Normalized{"baseUrl": jsontypes.NewNormalizedValue(`"https://example.test"`)}

	models := map[string]any{
		"resource":    &ObservabilityDestinationResourceModel{},
		"data source": &ObservabilityDestinationDataSourceModel{},
		"data list":   &ObservabilityDestinationsDataSourceModel{Data: make([]tfTypes.ObservabilityDestination, 2)},
	}
	for name, model := range models {
		t.Run(name, func(t *testing.T) {
			fillObservabilityConfigBlocks(reflect.ValueOf(model))
			blocks := configBlocks(reflect.ValueOf(model))
			if len(blocks) < 17 {
				t.Fatalf("expected a config block for each of the 17 destination types, found %d", len(blocks))
			}

			scrubObservabilityConfigBlocks(reflect.ValueOf(model), declared)

			for _, block := range blocks {
				for i := 0; i < block.NumField(); i++ {
					field := tfsdkName(block.Type().Field(i))
					kept := observabilityPublicConfigFields[field] || (observabilityURLConfigFields[field] && field == "base_url")
					if kept == isUnset(block.Field(i)) {
						t.Errorf("%s.%s: kept=%v but unset=%v", block.Type().Name(), field, !isUnset(block.Field(i)), isUnset(block.Field(i)))
					}
				}
			}
		})
	}
}

// Every allowlisted name must exist in a generated config block, so a typo, or a
// field the API renamed, cannot silently turn into "dropped" or "kept".
func TestObservabilityConfigAllowlistsMatchGeneratedFields(t *testing.T) {
	model := &ObservabilityDestinationResourceModel{}
	fillObservabilityConfigBlocks(reflect.ValueOf(model))

	present := map[string]bool{}
	for _, block := range configBlocks(reflect.ValueOf(model)) {
		for i := 0; i < block.NumField(); i++ {
			present[tfsdkName(block.Type().Field(i))] = true
		}
	}
	for _, list := range []map[string]bool{observabilityPublicConfigFields, observabilityURLConfigFields} {
		for name := range list {
			if !present[name] {
				t.Errorf("allowlisted config field %q does not exist in any generated config block", name)
			}
		}
	}
}

func TestScrubObservabilityResourceOnlyInWriteOnlyMode(t *testing.T) {
	legacy := &ObservabilityDestinationResourceModel{}
	fillObservabilityConfigBlocks(reflect.ValueOf(legacy))
	scrubObservabilityResource(legacy.ConfigSecretsWoVersion, legacy.Config, legacy)
	if legacy.Datadog.Config.APIKey.IsNull() {
		t.Fatal("legacy mode (no config_secrets_wo_version) must keep today's behavior")
	}

	writeOnly := &ObservabilityDestinationResourceModel{ConfigSecretsWoVersion: types.Int64Value(1)}
	fillObservabilityConfigBlocks(reflect.ValueOf(writeOnly))
	scrubObservabilityResource(writeOnly.ConfigSecretsWoVersion, writeOnly.Config, writeOnly)
	if !writeOnly.Datadog.Config.APIKey.IsNull() || writeOnly.Webhook.Config.Headers != nil {
		t.Fatal("write-only mode must drop credentials and headers from computed config")
	}
	if writeOnly.Datadog.Config.MlApp.IsNull() {
		t.Fatal("write-only mode must keep non-credential settings")
	}
}

func TestScrubObservabilityDataSource(t *testing.T) {
	for name, tc := range map[string]struct {
		include   types.Bool
		wantKept  bool
		wantLabel string
	}{
		"unset keeps":  {types.BoolNull(), true, "existing behavior"},
		"true keeps":   {types.BoolValue(true), true, "explicit true"},
		"false scrubs": {types.BoolValue(false), false, "explicit false"},
	} {
		t.Run(name, func(t *testing.T) {
			data := &ObservabilityDestinationDataSourceModel{IncludeSensitiveConfig: tc.include}
			fillObservabilityConfigBlocks(reflect.ValueOf(data))
			scrubObservabilityDataSource(data.IncludeSensitiveConfig, data)
			if kept := !data.Datadog.Config.APIKey.IsNull(); kept != tc.wantKept {
				t.Fatalf("%s: credential kept = %v, want %v", tc.wantLabel, kept, tc.wantKept)
			}
		})
	}
}

func TestMergeSecretValues(t *testing.T) {
	json := jsontypes.NewNormalizedValue

	t.Run("merges and decodes like config", func(t *testing.T) {
		got, diags := mergeSecretValues(map[string]any{"mlApp": "app"}, map[string]jsontypes.Normalized{
			"apiKey":  json(`"` + writeOnlyMarker + `"`),
			"headers": json(`{"Authorization":"Bearer ` + writeOnlyMarker + `"}`),
		})
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", diags)
		}
		if got["mlApp"] != "app" || got["apiKey"] != writeOnlyMarker {
			t.Fatalf("unexpected merge result: %v", got)
		}
		if _, ok := got["headers"].(map[string]any); !ok {
			t.Fatalf("headers should decode to an object, got %T", got["headers"])
		}
	})

	t.Run("overlap names the key, not the value", func(t *testing.T) {
		_, diags := mergeSecretValues(map[string]any{"apiKey": "public"}, map[string]jsontypes.Normalized{"apiKey": json(`"` + writeOnlyMarker + `"`)})
		assertDiagsHideMarker(t, diags, `"apiKey"`)
	})

	t.Run("invalid JSON never echoes the value", func(t *testing.T) {
		_, diags := mergeSecretValues(nil, map[string]jsontypes.Normalized{"apiKey": json(writeOnlyMarker + " not json")})
		assertDiagsHideMarker(t, diags, `"apiKey"`)
	})

	t.Run("unknown credential is an error", func(t *testing.T) {
		_, diags := mergeSecretValues(nil, map[string]jsontypes.Normalized{"apiKey": jsontypes.NewNormalizedUnknown()})
		if !diags.HasError() {
			t.Fatal("expected an error for an unknown credential")
		}
	})
}

func assertDiagsHideMarker(t *testing.T, diags diag.Diagnostics, wantKey string) {
	t.Helper()
	if !diags.HasError() {
		t.Fatal("expected an error")
	}
	var named bool
	for _, d := range diags {
		text := d.Summary() + " " + d.Detail()
		if strings.Contains(text, writeOnlyMarker) {
			t.Errorf("diagnostic leaks the credential value: %q", text)
		}
		named = named || strings.Contains(text, wantKey)
	}
	if !named {
		t.Errorf("no diagnostic names the key %s", wantKey)
	}
}

func testVersionSchema() resourceschema.Schema {
	return resourceschema.Schema{Attributes: map[string]resourceschema.Attribute{"v": resourceschema.Int64Attribute{Optional: true}}}
}

func TestWriteOnlyVersionChanged(t *testing.T) {
	ctx := context.Background()
	versionPath := path.Root("v")
	testSchema := testVersionSchema()
	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{"v": tftypes.Number}}

	val := func(v *int64) tftypes.Value {
		if v == nil {
			return tftypes.NewValue(objType, map[string]tftypes.Value{"v": tftypes.NewValue(tftypes.Number, nil)})
		}
		return tftypes.NewValue(objType, map[string]tftypes.Value{"v": tftypes.NewValue(tftypes.Number, *v)})
	}
	n := func(i int64) *int64 { return &i }

	for name, tc := range map[string]struct {
		plan, state *int64
		want        bool
	}{
		"not used":         {nil, nil, false},
		"dropped":          {nil, n(2), false},
		"first use":        {n(1), nil, true},
		"unchanged":        {n(2), n(2), false},
		"bumped":           {n(3), n(2), true},
		"lowered also set": {n(1), n(2), true},
	} {
		t.Run(name, func(t *testing.T) {
			got, diags := writeOnlyVersionChanged(ctx,
				tfsdk.Plan{Schema: testSchema, Raw: val(tc.plan)},
				tfsdk.State{Schema: testSchema, Raw: val(tc.state)},
				versionPath)
			if diags.HasError() {
				t.Fatalf("unexpected diagnostics: %v", diags)
			}
			if got != tc.want {
				t.Fatalf("changed = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestWriteOnlySchemas(t *testing.T) {
	ctx := context.Background()

	byok := &resource.SchemaResponse{}
	(&ByokKeyResource{}).Schema(ctx, resource.SchemaRequest{}, byok)
	if diags := byok.Schema.ValidateImplementation(ctx); diags.HasError() {
		t.Fatalf("invalid byok schema: %v", diags)
	}
	for name, wantWriteOnly := range map[string]bool{"key": false, "key_wo": true} {
		attr := byok.Schema.Attributes[name]
		if attr == nil || attr.IsWriteOnly() != wantWriteOnly || !attr.IsSensitive() || attr.IsRequired() || attr.IsComputed() {
			t.Errorf("byok %s: want optional, sensitive, write-only=%v, not computed", name, wantWriteOnly)
		}
	}
	if byok.Schema.Attributes["key_wo_version"] == nil {
		t.Error("byok key_wo_version missing")
	}

	dest := &resource.SchemaResponse{}
	(&ObservabilityDestinationResource{}).Schema(ctx, resource.SchemaRequest{}, dest)
	if diags := dest.Schema.ValidateImplementation(ctx); diags.HasError() {
		t.Fatalf("invalid observability schema: %v", diags)
	}
	if attr := dest.Schema.Attributes["config_secrets_wo"]; attr == nil || !attr.IsWriteOnly() || !attr.IsSensitive() {
		t.Error("observability config_secrets_wo must be write-only and sensitive")
	}
	if attr := dest.Schema.Attributes["config"]; attr == nil || attr.IsRequired() || !attr.IsOptional() || !attr.IsSensitive() {
		t.Error("observability config must stay sensitive but become optional")
	}

	for name, ds := range map[string]datasource.DataSource{
		"observability_destination":  &ObservabilityDestinationDataSource{},
		"observability_destinations": &ObservabilityDestinationsDataSource{},
	} {
		resp := &datasource.SchemaResponse{}
		ds.Schema(ctx, datasource.SchemaRequest{}, resp)
		if diags := resp.Schema.ValidateImplementation(ctx); diags.HasError() {
			t.Fatalf("invalid %s schema: %v", name, diags)
		}
		if attr := resp.Schema.Attributes["include_sensitive_config"]; attr == nil || !attr.IsOptional() || attr.IsComputed() {
			t.Errorf("%s: include_sensitive_config must be optional and not computed", name)
		}
	}
}

// configWith builds an operation config for a schema with every attribute null
// except the given overrides.
func configWith(t *testing.T, s resourceschema.Schema, overrides map[string]tftypes.Value) tfsdk.Config {
	t.Helper()
	ctx := context.Background()
	objType := s.Type().TerraformType(ctx).(tftypes.Object)
	values := make(map[string]tftypes.Value, len(objType.AttributeTypes))
	for name, typ := range objType.AttributeTypes {
		values[name] = tftypes.NewValue(typ, nil)
	}
	for name, v := range overrides {
		values[name] = v
	}
	return tfsdk.Config{Schema: s, Raw: tftypes.NewValue(objType, values)}
}

// A write-only credential must be redacted from debug dumps and error text,
// like the sensitive values the provider already tracks.
func TestWriteOnlyValuesAreRedacted(t *testing.T) {
	ctx := context.Background()

	byok := &resource.SchemaResponse{}
	(&ByokKeyResource{}).Schema(ctx, resource.SchemaRequest{}, byok)
	cfg := configWith(t, byok.Schema, map[string]tftypes.Value{
		"key_wo":         tftypes.NewValue(tftypes.String, writeOnlyMarker),
		"key_wo_version": tftypes.NewValue(tftypes.Number, 1),
	})

	redacted := redactSensitiveValues(withSensitiveValues(ctx, cfg), "upstream rejected key "+writeOnlyMarker)
	if strings.Contains(redacted, writeOnlyMarker) {
		t.Fatalf("write-only key leaked: %q", redacted)
	}

	dest := &resource.SchemaResponse{}
	(&ObservabilityDestinationResource{}).Schema(ctx, resource.SchemaRequest{}, dest)
	cfg = configWith(t, dest.Schema, map[string]tftypes.Value{
		"config_secrets_wo": tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, map[string]tftypes.Value{
			"apiKey": tftypes.NewValue(tftypes.String, `"`+writeOnlyMarker+`"`),
		}),
	})
	redacted = redactSensitiveValues(withSensitiveValues(ctx, cfg), "rejected "+writeOnlyMarker)
	if strings.Contains(redacted, writeOnlyMarker) {
		t.Fatalf("write-only config secret leaked: %q", redacted)
	}
}

func TestObservabilityValidateConfigRejectsOverlap(t *testing.T) {
	ctx := context.Background()
	dest := &resource.SchemaResponse{}
	(&ObservabilityDestinationResource{}).Schema(ctx, resource.SchemaRequest{}, dest)

	mapOf := func(keys ...string) tftypes.Value {
		m := map[string]tftypes.Value{}
		for _, k := range keys {
			m[k] = tftypes.NewValue(tftypes.String, `"`+writeOnlyMarker+`"`)
		}
		return tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, m)
	}

	for name, tc := range map[string]struct {
		public, secrets tftypes.Value
		wantError       bool
	}{
		"disjoint":   {mapOf("url"), mapOf("headers"), false},
		"overlap":    {mapOf("url", "headers"), mapOf("headers"), true},
		"no secrets": {mapOf("url"), tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, nil), false},
	} {
		t.Run(name, func(t *testing.T) {
			resp := &resource.ValidateConfigResponse{}
			(&ObservabilityDestinationResource{}).ValidateConfig(ctx, resource.ValidateConfigRequest{
				Config: configWith(t, dest.Schema, map[string]tftypes.Value{"config": tc.public, "config_secrets_wo": tc.secrets}),
			}, resp)
			if resp.Diagnostics.HasError() != tc.wantError {
				t.Fatalf("error = %v, want %v: %v", resp.Diagnostics.HasError(), tc.wantError, resp.Diagnostics)
			}
			if tc.wantError {
				assertDiagsHideMarker(t, resp.Diagnostics, `"headers"`)
			}
		})
	}
}
