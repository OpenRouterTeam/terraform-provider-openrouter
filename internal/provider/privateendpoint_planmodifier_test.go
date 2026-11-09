package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// A resource created before this provider modelled `settings`, `pricing`,
// `declared_parameters`, `tested_parameters` or `max_completion_tokens` has
// null for those groups in state. Config still omits the group, so the
// framework plans the attribute as unknown. Keeping the prior state there is
// what stops the first plan after an upgrade from reporting
// `(known after apply)` on a resource the user never reconfigured.

func privateEndpointTestSchema(t *testing.T) schema.Schema {
	t.Helper()
	ctx := context.Background()
	var resp resource.SchemaResponse
	(&PrivateEndpointResource{}).Schema(ctx, resource.SchemaRequest{}, &resp)
	return resp.Schema
}

// privateEndpointNullModel returns a fully null object of the resource's shape,
// which is what a plan or state needs before SetAttribute can be called.
func privateEndpointNullModel(ctx context.Context, t *testing.T, s schema.Schema) tftypes.Value {
	t.Helper()
	objType, ok := s.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatalf("private endpoint schema is not an object type")
	}
	values := make(map[string]tftypes.Value, len(objType.AttributeTypes))
	for name, attrType := range objType.AttributeTypes {
		values[name] = tftypes.NewValue(attrType, nil)
	}
	return tftypes.NewValue(objType, values)
}

func TestPrivateEndpointGroupUnchangedKeepsPriorState(t *testing.T) {
	ctx := context.Background()
	s := privateEndpointTestSchema(t)

	state := tfsdk.State{Schema: s, Raw: privateEndpointNullModel(ctx, t, s)}
	if diags := state.SetAttribute(ctx, path.Root("max_completion_tokens"), types.Int64Value(4096)); diags.HasError() {
		t.Fatalf("setting state attribute: %v", diags)
	}
	if diags := state.SetAttribute(ctx, path.Root("tested_parameters"), types.ListValueMust(types.StringType, []attr.Value{types.StringValue("temperature")})); diags.HasError() {
		t.Fatalf("setting state attribute: %v", diags)
	}

	plan := tfsdk.Plan{Schema: s, Raw: privateEndpointNullModel(ctx, t, s)}
	if diags := plan.SetAttribute(ctx, path.Root("max_completion_tokens"), types.Int64Unknown()); diags.HasError() {
		t.Fatalf("setting plan attribute: %v", diags)
	}
	if diags := plan.SetAttribute(ctx, path.Root("tested_parameters"), types.ListUnknown(types.StringType)); diags.HasError() {
		t.Fatalf("setting plan attribute: %v", diags)
	}

	for _, group := range []string{"max_completion_tokens", "tested_parameters"} {
		if !privateEndpointGroupUnchanged(ctx, plan, state, group) {
			t.Errorf("%s: an unknown plan value over a populated prior state must keep state, or the first plan after an upgrade reports (known after apply)", group)
		}
	}
}

func TestPrivateEndpointGroupUnchangedComparesPlannedValues(t *testing.T) {
	ctx := context.Background()
	s := privateEndpointTestSchema(t)

	state := tfsdk.State{Schema: s, Raw: privateEndpointNullModel(ctx, t, s)}
	if diags := state.SetAttribute(ctx, path.Root("max_completion_tokens"), types.Int64Value(4096)); diags.HasError() {
		t.Fatalf("setting state attribute: %v", diags)
	}

	plan := tfsdk.Plan{Schema: s, Raw: privateEndpointNullModel(ctx, t, s)}
	if diags := plan.SetAttribute(ctx, path.Root("max_completion_tokens"), types.Int64Value(4096)); diags.HasError() {
		t.Fatalf("setting plan attribute: %v", diags)
	}
	if !privateEndpointGroupUnchanged(ctx, plan, state, "max_completion_tokens") {
		t.Error("an unchanged configured group must keep state")
	}

	if diags := plan.SetAttribute(ctx, path.Root("max_completion_tokens"), types.Int64Value(8192)); diags.HasError() {
		t.Fatalf("setting plan attribute: %v", diags)
	}
	if privateEndpointGroupUnchanged(ctx, plan, state, "max_completion_tokens") {
		t.Error("a changed configured group must not keep state, or the API never receives the new value")
	}
}

func TestPrivateEndpointGroupUnchangedWithoutPriorState(t *testing.T) {
	ctx := context.Background()
	s := privateEndpointTestSchema(t)

	state := tfsdk.State{Schema: s, Raw: privateEndpointNullModel(ctx, t, s)}

	plan := tfsdk.Plan{Schema: s, Raw: privateEndpointNullModel(ctx, t, s)}
	if diags := plan.SetAttribute(ctx, path.Root("max_completion_tokens"), types.Int64Unknown()); diags.HasError() {
		t.Fatalf("setting plan attribute: %v", diags)
	}

	if privateEndpointGroupUnchanged(ctx, plan, state, "max_completion_tokens") {
		t.Error("with no prior value there is no state to keep; the planned result must stay unknown")
	}
}
