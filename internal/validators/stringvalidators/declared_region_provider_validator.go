package stringvalidators

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// declarableRegionsByProviderSlug mirrors the regions the management API lets
// each provider declare on a BYOK key, besides `global`. Providers not listed
// accept only `global` (or no declaration).
var declarableRegionsByProviderSlug = map[string][]string{
	"azure":     {"europe", "us"},
	"anthropic": {"us"},
}

// keyRegionProviderSlugs carry the region in the key material, so the valid
// declared_region depends on the (sensitive) key and is left to the API.
var keyRegionProviderSlugs = []string{"openai", "fireworks"}

var _ validator.String = StringDeclaredRegionProviderValidatorValidator{}

// StringDeclaredRegionProviderValidatorValidator rejects a declared_region the
// configured provider_slug cannot declare, so the error surfaces at plan time
// instead of as a 400 at apply.
type StringDeclaredRegionProviderValidatorValidator struct{}

// Description describes the validation in plain text formatting.
func (v StringDeclaredRegionProviderValidatorValidator) Description(_ context.Context) string {
	return "declared_region must be a region the provider supports: azure accepts europe or us, anthropic accepts us, openai and fireworks must match the key's region, and every other provider accepts only global"
}

// MarkdownDescription describes the validation in Markdown formatting.
func (v StringDeclaredRegionProviderValidatorValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

// ValidateString performs the validation.
func (v StringDeclaredRegionProviderValidatorValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	region := req.ConfigValue.ValueString()
	if region == "global" {
		return
	}

	var providerSlug types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("provider_slug"), &providerSlug)...)
	if resp.Diagnostics.HasError() || providerSlug.IsNull() || providerSlug.IsUnknown() {
		return
	}
	slug := providerSlug.ValueString()
	if slices.Contains(keyRegionProviderSlugs, slug) {
		return
	}

	allowed := declarableRegionsByProviderSlug[slug]
	if slices.Contains(allowed, region) {
		return
	}
	resp.Diagnostics.AddAttributeError(
		req.Path,
		"Unsupported declared_region for provider",
		fmt.Sprintf("declared_region %q is not supported for provider_slug %q; supported values: %s.", region, slug, strings.Join(append([]string{"global"}, allowed...), ", ")),
	)
}

// DeclaredRegionProviderValidator returns a validator that checks
// declared_region against the configured provider_slug.
func DeclaredRegionProviderValidator() validator.String {
	return StringDeclaredRegionProviderValidatorValidator{}
}
