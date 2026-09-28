package stringplanmodifier

import (
	"context"
	"net/url"
	"strings"

	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/planmodifiers/utils"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
)

var _ planmodifier.String = StringPrivateEndpointBaseURLRequiresReplacePlanModifier{}

// StringPrivateEndpointBaseURLRequiresReplacePlanModifier replaces a private
// endpoint only when its base_url changes after the API's normalization, and
// keeps the stored value when base_url is not configured.
type StringPrivateEndpointBaseURLRequiresReplacePlanModifier struct{}

// Description describes the plan modification in plain text formatting.
func (v StringPrivateEndpointBaseURLRequiresReplacePlanModifier) Description(_ context.Context) string {
	return "Requires replacement when the normalized base URL changes."
}

// MarkdownDescription describes the plan modification in Markdown formatting.
func (v StringPrivateEndpointBaseURLRequiresReplacePlanModifier) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

// PlanModifyString performs the plan modification.
func (v StringPrivateEndpointBaseURLRequiresReplacePlanModifier) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	// Creating or destroying the resource: nothing to replace.
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	// Not configured (providers that derive the URL): keep the stored value.
	if req.ConfigValue.IsNull() {
		if req.PlanValue.IsUnknown() && !utils.IsAllStateUnknown(ctx, req.State) {
			resp.PlanValue = req.StateValue
		}
		return
	}
	if req.PlanValue.Equal(req.StateValue) {
		return
	}
	if !req.PlanValue.IsUnknown() && !req.StateValue.IsNull() && !req.StateValue.IsUnknown() &&
		NormalizePrivateEndpointBaseURL(req.PlanValue.ValueString()) == NormalizePrivateEndpointBaseURL(req.StateValue.ValueString()) {
		return
	}
	resp.RequiresReplace = true
}

func PrivateEndpointBaseURLRequiresReplace() planmodifier.String {
	return StringPrivateEndpointBaseURLRequiresReplacePlanModifier{}
}

// NormalizePrivateEndpointBaseURL mirrors how the API stores a private
// endpoint base URL: WHATWG URL serialization (lowercase scheme and host, no
// default port), then one trailing "/" and a trailing "/chat/completions"
// stripped. Input it cannot parse is returned unchanged.
func NormalizePrivateEndpointBaseURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return raw
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	if parsed.Scheme == "https" {
		parsed.Host = strings.TrimSuffix(parsed.Host, ":443")
	}
	normalized := strings.TrimSuffix(parsed.String(), "/")
	return strings.TrimSuffix(normalized, "/chat/completions")
}
