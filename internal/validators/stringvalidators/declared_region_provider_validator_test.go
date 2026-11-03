package stringvalidators

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestDeclaredRegionProviderValidator(t *testing.T) {
	t.Parallel()

	sch := schema.Schema{Attributes: map[string]schema.Attribute{
		"declared_region": schema.StringAttribute{Optional: true},
		"provider_slug":   schema.StringAttribute{Required: true},
	}}
	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"declared_region": tftypes.String,
		"provider_slug":   tftypes.String,
	}}

	tests := map[string]struct {
		providerSlug tftypes.Value
		region       tftypes.Value
		wantError    bool
	}{
		"anthropic europe":       {tftypes.NewValue(tftypes.String, "anthropic"), tftypes.NewValue(tftypes.String, "europe"), true},
		"anthropic us":           {tftypes.NewValue(tftypes.String, "anthropic"), tftypes.NewValue(tftypes.String, "us"), false},
		"azure europe":           {tftypes.NewValue(tftypes.String, "azure"), tftypes.NewValue(tftypes.String, "europe"), false},
		"azure us":               {tftypes.NewValue(tftypes.String, "azure"), tftypes.NewValue(tftypes.String, "us"), false},
		"amazon-bedrock us":      {tftypes.NewValue(tftypes.String, "amazon-bedrock"), tftypes.NewValue(tftypes.String, "us"), true},
		"amazon-bedrock europe":  {tftypes.NewValue(tftypes.String, "amazon-bedrock"), tftypes.NewValue(tftypes.String, "europe"), true},
		"amazon-bedrock global":  {tftypes.NewValue(tftypes.String, "amazon-bedrock"), tftypes.NewValue(tftypes.String, "global"), false},
		"amazon-bedrock null":    {tftypes.NewValue(tftypes.String, "amazon-bedrock"), tftypes.NewValue(tftypes.String, nil), false},
		"openai europe deferred": {tftypes.NewValue(tftypes.String, "openai"), tftypes.NewValue(tftypes.String, "europe"), false},
		"fireworks us deferred":  {tftypes.NewValue(tftypes.String, "fireworks"), tftypes.NewValue(tftypes.String, "us"), false},
		"region unknown":         {tftypes.NewValue(tftypes.String, "anthropic"), tftypes.NewValue(tftypes.String, tftypes.UnknownValue), false},
		"provider unknown":       {tftypes.NewValue(tftypes.String, tftypes.UnknownValue), tftypes.NewValue(tftypes.String, "europe"), false},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var configValue types.String
			switch {
			case tc.region.IsNull():
				configValue = types.StringNull()
			case !tc.region.IsKnown():
				configValue = types.StringUnknown()
			default:
				var region string
				if err := tc.region.As(&region); err != nil {
					t.Fatal(err)
				}
				configValue = types.StringValue(region)
			}

			req := validator.StringRequest{
				Path:        path.Root("declared_region"),
				ConfigValue: configValue,
				Config: tfsdk.Config{
					Schema: sch,
					Raw: tftypes.NewValue(objType, map[string]tftypes.Value{
						"declared_region": tc.region,
						"provider_slug":   tc.providerSlug,
					}),
				},
			}
			resp := &validator.StringResponse{}
			DeclaredRegionProviderValidator().ValidateString(context.Background(), req, resp)

			if got := resp.Diagnostics.HasError(); got != tc.wantError {
				t.Fatalf("HasError() = %v, want %v: %v", got, tc.wantError, resp.Diagnostics)
			}
		})
	}
}
