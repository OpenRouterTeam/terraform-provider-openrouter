package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/operations"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/shared"
	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/mapvalidator"
	datasourceschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.ResourceWithValidateConfig = &ObservabilityDestinationResource{}

// Broadcast credentials travel in `config`, a free-form map. Write-only mode
// splits it: public settings stay in `config`, credentials go in
// `config_secrets_wo`, and the two are merged into one API request. The API
// echoes the destination's configuration back in typed, computed blocks
// (`datadog.config`, `webhook.config`, ...). In write-only mode those copies
// are filtered, so a credential cannot re-enter state through the response.
//
// The filter is an allowlist. A computed config field survives only if it is a
// known non-credential setting, or a URL the practitioner declared public by
// putting it in `config`. Fields the API adds later are dropped until they are
// classified here.

// observabilityPublicConfigFields are response config fields (Terraform names)
// that never carry a credential.
var observabilityPublicConfigFields = map[string]bool{
	"account":                           true,
	"bucket_name":                       true,
	"database":                          true,
	"entity":                            true,
	"instance_id":                       true,
	"ml_app":                            true,
	"method":                            true,
	"model_id":                          true,
	"path_template":                     true,
	"prefix":                            true,
	"project":                           true,
	"project_id":                        true,
	"project_name":                      true,
	"region":                            true,
	"schema":                            true,
	"should_include_cache_write_tokens": true,
	"table":                             true,
	"username":                          true,
	"warehouse":                         true,
	"workspace":                         true,
	"workspace_id":                      true,
}

// observabilityURLConfigFields can embed credentials in userinfo, a query
// string or a path. They are shown only when the practitioner declared them
// public by setting them in `config`.
var observabilityURLConfigFields = map[string]bool{
	"base_url":      true,
	"endpoint":      true,
	"host":          true,
	"otlp_endpoint": true,
	"url":           true,
}

// addObservabilityWriteOnlyAttributes adds the write-only credential map to the
// generated resource schema. `config` stays available, now optional, so
// existing configurations keep working; everything in it is stored in state.
func addObservabilityWriteOnlyAttributes(s *schema.Schema) {
	config := s.Attributes["config"].(schema.MapAttribute)
	config.Required = false
	config.Optional = true
	config.Description = "Provider-specific configuration. The shape depends on `type` and is validated server-side. " +
		"Everything in this map is stored in Terraform state; put credentials in `config_secrets_wo` instead to keep them out of state and plans. " +
		"At least one of `config` and `config_secrets_wo` is required to create a destination."
	s.Attributes["config"] = config

	s.Attributes["config_secrets_wo"] = schema.MapAttribute{
		Optional:  true,
		Sensitive: true,
		WriteOnly: true,
		// A plain string element type: jsontypes.NormalizedType and
		// validators.IsValidJSON echo an invalid value in their diagnostics.
		ElementType: types.StringType,
		Description: "Write-only credentials for the destination, as JSON-encoded values keyed like `config`. They are merged into `config` in the API request " +
			"and never stored in Terraform state or plans. Requires Terraform 1.11 or later and `config_secrets_wo_version`. " +
			"A key may appear in `config` or `config_secrets_wo`, not both, and keys the API returns in plain text (such as `username` or `region`) must go in `config`. " +
			"Each non-empty string in a value must be at least 4 characters long. While this is set, the computed `config` blocks in the response " +
			"omit credentials, headers, and URLs not declared in `config`.",
		Validators: []validator.Map{
			mapvalidator.ValueStringsAre(secretJSONValidator{}),
			mapvalidator.AlsoRequires(path.MatchRoot("config_secrets_wo_version")),
		},
	}
	s.Attributes["config_secrets_wo_version"] = schema.Int64Attribute{
		Optional: true,
		Description: "Rotation trigger for `config_secrets_wo`. The credentials are sent on create, and again whenever this value changes. " +
			"Increment it to rotate them; any positive integer works. Unchanged versions leave stored credentials as they are.",
		Validators: []validator.Int64{
			int64validator.AtLeast(1),
			int64validator.AlsoRequires(path.MatchRoot("config_secrets_wo")),
		},
	}
}

// addIncludeSensitiveConfigAttribute adds the data source option that drops
// credential-bearing fields from the computed config blocks. It defaults to
// the existing behavior of returning everything the API reports.
func addIncludeSensitiveConfigAttribute(s *datasourceschema.Schema) {
	s.Attributes["include_sensitive_config"] = datasourceschema.BoolAttribute{
		Optional: true,
		Description: "Whether to include credential-bearing fields in the `config` blocks. Set to `false` to omit credentials, headers, " +
			"and URLs, so they are not stored in state. Defaults to `true`.",
	}
}

// ValidateConfig rejects keys set in both `config` and `config_secrets_wo`, and
// public keys set in `config_secrets_wo`. It names keys only, never values.
func (r *ObservabilityDestinationResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var public, secrets types.Map
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("config"), &public)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("config_secrets_wo"), &secrets)...)
	if resp.Diagnostics.HasError() || secrets.IsNull() || secrets.IsUnknown() {
		return
	}

	keys := make([]string, 0, len(secrets.Elements()))
	for key := range secrets.Elements() {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		keyPath := path.Root("config_secrets_wo").AtMapKey(key)
		if _, ok := public.Elements()[key]; ok && !public.IsUnknown() {
			resp.Diagnostics.AddAttributeError(keyPath, "Overlapping configuration key",
				fmt.Sprintf("The key %q is set in both `config` and `config_secrets_wo`. Set each key in only one of them.", key))
			continue
		}
		if isObservabilityPublicConfigKey(key) {
			resp.Diagnostics.AddAttributeError(keyPath, publicKeyInSecretsSummary, publicKeyInSecretsDetail(key))
		}
	}
}

const publicKeyInSecretsSummary = "Public configuration key in config_secrets_wo"

func publicKeyInSecretsDetail(key string) string {
	return fmt.Sprintf("The key %q is returned by the API in plain text and stored in state, so it cannot be write-only. Set it in `config` instead.", key)
}

// isObservabilityPublicConfigKey reports whether a config key, in the API's
// camelCase or Terraform's snake_case, is an allowlisted public field. Those
// fields are kept in the computed config blocks, so a value supplied through
// `config_secrets_wo` would reach state.
func isObservabilityPublicConfigKey(key string) bool {
	return observabilityPublicConfigFields[key] || observabilityPublicConfigFields[camelToSnake(key)]
}

// mergeObservabilitySecrets reads `config_secrets_wo` from the operation
// configuration and merges it into into, the request's config map. It reports
// whether any credentials were present.
func mergeObservabilitySecrets(ctx context.Context, cfg tfsdk.Config, into map[string]any) (map[string]any, bool, diag.Diagnostics) {
	var secrets map[string]types.String
	diags := cfg.GetAttribute(ctx, path.Root("config_secrets_wo"), &secrets)
	if diags.HasError() || len(secrets) == 0 {
		return into, false, diags
	}

	merged, mergeDiags := mergeSecretValues(into, secrets)
	diags.Append(mergeDiags...)

	return merged, true, diags
}

// mergeSecretValues decodes each credential like the generated builder decodes
// `config` values (JSON) and adds it to into. Diagnostics name keys, never
// values.
func mergeSecretValues(into map[string]any, secrets map[string]types.String) (map[string]any, diag.Diagnostics) {
	var diags diag.Diagnostics

	if into == nil {
		into = make(map[string]any, len(secrets))
	}
	for key, value := range secrets {
		keyPath := path.Root("config_secrets_wo").AtMapKey(key)
		if _, ok := into[key]; ok {
			diags.AddAttributeError(keyPath, "Overlapping configuration key",
				fmt.Sprintf("The key %q is set in both `config` and `config_secrets_wo`. Set each key in only one of them.", key))
			continue
		}
		if isObservabilityPublicConfigKey(key) {
			diags.AddAttributeError(keyPath, publicKeyInSecretsSummary, publicKeyInSecretsDetail(key))
			continue
		}
		if value.IsNull() || value.IsUnknown() {
			diags.AddAttributeError(keyPath, "Unknown credential", fmt.Sprintf("The value for key %q is not known.", key))
			continue
		}
		var decoded any
		if err := json.Unmarshal([]byte(value.ValueString()), &decoded); err != nil {
			diags.AddAttributeError(keyPath, "Invalid JSON", fmt.Sprintf("The value for key %q is not valid JSON.", key))
			continue
		}
		into[key] = decoded
	}

	return into, diags
}

// applyObservabilityWriteOnlyConfigOnCreate merges `config_secrets_wo` into the
// create request. The generated builder reads the plan, where write-only
// values are always null.
func applyObservabilityWriteOnlyConfigOnCreate(ctx context.Context, cfg tfsdk.Config, request *shared.CreateObservabilityDestinationRequest) diag.Diagnostics {
	merged, _, diags := mergeObservabilitySecrets(ctx, cfg, request.Config)
	if diags.HasError() {
		return diags
	}
	request.Config = merged

	if len(request.Config) == 0 {
		diags.AddAttributeError(path.Root("config"), "Missing configuration", "At least one of `config` and `config_secrets_wo` must be set to create a destination.")
	}

	return diags
}

// applyObservabilityWriteOnlyConfigOnUpdate sends credentials only when
// `config_secrets_wo_version` changed. Updates retain configuration fields they
// omit, so public settings can change without resending credentials.
func applyObservabilityWriteOnlyConfigOnUpdate(ctx context.Context, req resource.UpdateRequest, request *operations.UpdateObservabilityDestinationRequest) diag.Diagnostics {
	rotate, diags := writeOnlyVersionChanged(ctx, req.Plan, req.State, path.Root("config_secrets_wo_version"))
	if diags.HasError() {
		return diags
	}

	if rotate {
		merged, sent, mergeDiags := mergeObservabilitySecrets(ctx, req.Config, request.Body.Config)
		diags.Append(mergeDiags...)
		if diags.HasError() {
			return diags
		}
		if !sent {
			diags.AddAttributeError(path.Root("config_secrets_wo"), "Missing credentials", "`config_secrets_wo` must be set when `config_secrets_wo_version` changes.")
			return diags
		}
		request.Body.Config = merged
	}

	if len(request.Body.Config) == 0 {
		request.Body.Config = nil
	}

	return diags
}

// scrubObservabilityResource filters the computed config blocks of the resource
// model when the practitioner uses write-only credentials, signalled by a
// config_secrets_wo_version in the plan or state. It also filters them when
// neither the version nor `config` is set, which is the state right after an
// import: the practitioner's mode is not known yet, so the response is treated
// as write-only. The next apply records the real mode. Call it after the
// response has been flattened into the model. The version and public config are
// passed in, rather than read from the model, so this file compiles against the
// pristine generated code that Speakeasy builds before merging custom edits.
func scrubObservabilityResource(version types.Int64, public map[string]jsontypes.Normalized, data any) {
	writeOnly := !version.IsNull() && !version.IsUnknown()
	imported := version.IsNull() && len(public) == 0
	if !writeOnly && !imported {
		return
	}
	scrubObservabilityConfigBlocks(reflect.ValueOf(data), public)
}

// scrubObservabilityDataSource filters the computed config blocks when
// include_sensitive_config is false.
func scrubObservabilityDataSource(include types.Bool, data any) {
	if include.IsNull() || include.IsUnknown() || include.ValueBool() {
		return
	}
	scrubObservabilityConfigBlocks(reflect.ValueOf(data), nil)
}

// scrubObservabilityConfigBlocks walks a model for struct fields tagged
// `tfsdk:"config"` that hold a pointer to a struct (the typed, computed copies
// of the API's configuration) and clears every field outside the allowlist.
// The top-level `config` map is the practitioner's input and is untouched.
func scrubObservabilityConfigBlocks(v reflect.Value, declared map[string]jsontypes.Normalized) {
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			scrubObservabilityConfigBlocks(v.Elem(), declared)
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			scrubObservabilityConfigBlocks(v.Index(i), declared)
		}
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < t.NumField(); i++ {
			name := tfsdkName(t.Field(i))
			if name == "" {
				continue
			}
			field := v.Field(i)
			if name == "config" && field.Kind() == reflect.Pointer && !field.IsNil() && field.Elem().Kind() == reflect.Struct {
				scrubObservabilityConfigStruct(field.Elem(), declared)
				continue
			}
			scrubObservabilityConfigBlocks(field, declared)
		}
	}
}

func scrubObservabilityConfigStruct(v reflect.Value, declared map[string]jsontypes.Normalized) {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		name := tfsdkName(t.Field(i))
		if name == "" || observabilityPublicConfigFields[name] {
			continue
		}
		if _, ok := declared[snakeToCamel(name)]; ok && observabilityURLConfigFields[name] {
			continue
		}
		field := v.Field(i)
		field.Set(reflect.Zero(field.Type()))
	}
}

func tfsdkName(f reflect.StructField) string {
	name, _, _ := strings.Cut(f.Tag.Get("tfsdk"), ",")
	return name
}

func camelToSnake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if 'A' <= r && r <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
}

func snakeToCamel(s string) string {
	parts := strings.Split(s, "_")
	for i := 1; i < len(parts); i++ {
		parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
	}
	return strings.Join(parts, "")
}
