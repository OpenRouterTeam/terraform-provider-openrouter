package int64planmodifier

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ planmodifier.Int64 = Int64PrivateProviderRetentionDaysPlanModifier{}

// Int64PrivateProviderRetentionDaysPlanModifier plans an unconfigured
// prompt_retention_days as null when its sibling retains_prompts is false,
// matching the API, which clears the days when retention is turned off.
// Otherwise the stored days would be sent back with retains_prompts = false
// and the API would reject the update.
type Int64PrivateProviderRetentionDaysPlanModifier struct{}

// Description describes the plan modification in plain text formatting.
func (v Int64PrivateProviderRetentionDaysPlanModifier) Description(_ context.Context) string {
	return "Plans null retention days when retains_prompts is false and the days are not configured."
}

// MarkdownDescription describes the plan modification in Markdown formatting.
func (v Int64PrivateProviderRetentionDaysPlanModifier) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

// PlanModifyInt64 performs the plan modification.
func (v Int64PrivateProviderRetentionDaysPlanModifier) PlanModifyInt64(ctx context.Context, req planmodifier.Int64Request, resp *planmodifier.Int64Response) {
	if req.Plan.Raw.IsNull() || !req.ConfigValue.IsNull() {
		return
	}
	var retainsPrompts types.Bool
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, req.Path.ParentPath().AtName("retains_prompts"), &retainsPrompts)...)
	if resp.Diagnostics.HasError() || retainsPrompts.IsUnknown() || retainsPrompts.IsNull() {
		return
	}
	if !retainsPrompts.ValueBool() {
		resp.PlanValue = types.Int64Null()
	}
}

func PrivateProviderRetentionDays() planmodifier.Int64 {
	return Int64PrivateProviderRetentionDaysPlanModifier{}
}
