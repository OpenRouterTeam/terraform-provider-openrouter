package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
)

// The API fills in defaults for `settings`, `pricing`, `tested_parameters` and
// the effective `max_completion_tokens` that config never spells out, and it
// rewrites them whenever those groups change. Keeping the prior state in the
// plan unconditionally would make Terraform reject the new server value as an
// inconsistent result, so these modifiers only keep state while the group they
// mirror is unchanged.

const privateEndpointSettingsGroup = "settings"

func privateEndpointGroupUnchanged(ctx context.Context, plan tfsdk.Plan, state tfsdk.State, group string) bool {
	if plan.Raw.IsNull() || state.Raw.IsNull() || !plan.Raw.IsKnown() {
		return false
	}
	var planned, current attr.Value
	if diags := plan.GetAttribute(ctx, path.Root(group), &planned); diags.HasError() {
		return false
	}
	if diags := state.GetAttribute(ctx, path.Root(group), &current); diags.HasError() {
		return false
	}
	if planned.IsUnknown() || planned.IsNull() {
		return false
	}
	return !privateEndpointValueChanged(planned, current)
}

type privateEndpointObjectKeepUnlessChanged struct {
	group string
}

func (m privateEndpointObjectKeepUnlessChanged) Description(context.Context) string { return "" }

func (m privateEndpointObjectKeepUnlessChanged) MarkdownDescription(context.Context) string {
	return ""
}

func (m privateEndpointObjectKeepUnlessChanged) PlanModifyObject(ctx context.Context, req planmodifier.ObjectRequest, resp *planmodifier.ObjectResponse) {
	if privateEndpointGroupUnchanged(ctx, req.Plan, req.State, m.group) {
		resp.PlanValue = req.StateValue
	}
}

type privateEndpointListKeepUnlessChanged struct {
	group string
}

func (m privateEndpointListKeepUnlessChanged) Description(context.Context) string { return "" }

func (m privateEndpointListKeepUnlessChanged) MarkdownDescription(context.Context) string { return "" }

func (m privateEndpointListKeepUnlessChanged) PlanModifyList(ctx context.Context, req planmodifier.ListRequest, resp *planmodifier.ListResponse) {
	if privateEndpointGroupUnchanged(ctx, req.Plan, req.State, m.group) {
		resp.PlanValue = req.StateValue
	}
}

type privateEndpointInt64KeepUnlessChanged struct {
	group string
}

func (m privateEndpointInt64KeepUnlessChanged) Description(context.Context) string { return "" }

func (m privateEndpointInt64KeepUnlessChanged) MarkdownDescription(context.Context) string { return "" }

func (m privateEndpointInt64KeepUnlessChanged) PlanModifyInt64(ctx context.Context, req planmodifier.Int64Request, resp *planmodifier.Int64Response) {
	if privateEndpointGroupUnchanged(ctx, req.Plan, req.State, m.group) {
		resp.PlanValue = req.StateValue
	}
}

type privateEndpointStringKeepUnlessChanged struct {
	group string
}

func (m privateEndpointStringKeepUnlessChanged) Description(context.Context) string { return "" }

func (m privateEndpointStringKeepUnlessChanged) MarkdownDescription(context.Context) string {
	return ""
}

func (m privateEndpointStringKeepUnlessChanged) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if privateEndpointGroupUnchanged(ctx, req.Plan, req.State, m.group) {
		resp.PlanValue = req.StateValue
	}
}
