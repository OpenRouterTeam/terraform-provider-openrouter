// Hand-written resource. Not produced by Speakeasy; registered through
// terraform.additionalResources in .speakeasy/gen.yaml so regeneration keeps it.

package provider

import (
	"context"
	"fmt"

	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/operations"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

var _ resource.Resource = &WorkspaceDefaultGuardrailResource{}
var _ resource.ResourceWithImportState = &WorkspaceDefaultGuardrailResource{}

func NewWorkspaceDefaultGuardrailResource() resource.Resource {
	return &WorkspaceDefaultGuardrailResource{}
}

// WorkspaceDefaultGuardrailResource manages the guardrail that every
// workspace applies to keys and members without an explicit assignment.
//
// The default guardrail has a deterministic id, exposed as
// openrouter_workspace.default_guardrail_id, but it is lazily materialized:
// GET /guardrails/{id} returns 404 until its configuration is first written
// with PATCH /guardrails/{id}, which both materializes and configures it.
//
// Existence oracle: the workspace. GET /workspaces/{workspace_id} returning
// 404 means the default guardrail is gone too; a 404 from GET /guardrails/{id}
// while the workspace still resolves means "exists, unconfigured".
type WorkspaceDefaultGuardrailResource struct {
	client *sdk.OpenRouter
}

// The data model is shared with openrouter_guardrail so the generated
// request/response conversions can be reused unchanged.
type WorkspaceDefaultGuardrailResourceModel = GuardrailResourceModel

func (r *WorkspaceDefaultGuardrailResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace_default_guardrail"
}

// Schema derives from the generated openrouter_guardrail schema so the
// configurable surface stays in lockstep with the API spec, then adjusts the
// three attributes whose semantics differ for the workspace default.
func (r *WorkspaceDefaultGuardrailResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	var base resource.SchemaResponse
	NewGuardrailResource().Schema(ctx, req, &base)
	resp.Diagnostics.Append(base.Diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}

	attrs := base.Schema.Attributes

	attrs["workspace_id"] = schema.StringAttribute{
		Required: true,
		PlanModifiers: []planmodifier.String{
			stringplanmodifier.RequiresReplace(),
		},
		Description: `The workspace whose default guardrail is managed. The default guardrail applies to every API key and member in the workspace that has no explicit guardrail assignment. Exactly one of these resources should exist per workspace. Requires replacement if changed.`,
	}

	attrs["id"] = schema.StringAttribute{
		Computed: true,
		PlanModifiers: []planmodifier.String{
			stringplanmodifier.UseStateForUnknown(),
		},
		Description: `The guardrail id. Equals the workspace's ` + "`" + `default_guardrail_id` + "`" + ` and is resolved from the workspace, never chosen by the caller.`,
	}

	attrs["name"] = schema.StringAttribute{
		Computed:    true,
		Optional:    true,
		Description: `Name of the default guardrail. Assigned by the platform (` + "`" + `Workspace <id> Default` + "`" + `) when omitted.`,
		Validators: []validator.String{
			stringvalidator.UTF8LengthBetween(1, 200),
		},
	}

	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages the default guardrail of a workspace. The default guardrail is enforced for every API key and member of the workspace that has no explicit guardrail assignment, so this resource is the place to define workspace-wide protections. " +
			"The guardrail is created by the platform together with the workspace and materialized on first write, so this resource never issues `POST /guardrails`; create and update both `PATCH /guardrails/{default_guardrail_id}`. " +
			"Destroying the resource only removes it from state, the guardrail itself cannot be deleted. Import with the workspace id.\n\n" +
			"Until the first write, `GET /guardrails/{default_guardrail_id}` returns 404 even though the guardrail is in force. Read and import treat that 404 as an existing, unconfigured guardrail and use the workspace as the existence oracle: `GET /workspaces/{workspace_id}` still returning `default_guardrail_id` means the guardrail exists, while a 404 from the workspace lookup means the workspace and its default guardrail are gone and the resource is removed from state.",
		Attributes: attrs,
	}
}

func (r *WorkspaceDefaultGuardrailResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*sdk.OpenRouter)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *sdk.OpenRouter, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	r.client = client
}

// resolveDefaultGuardrailID looks up the workspace and returns its
// default_guardrail_id. found is false when the workspace does not exist.
func (r *WorkspaceDefaultGuardrailResource) resolveDefaultGuardrailID(ctx context.Context, workspaceID string) (id string, found bool, diags diag.Diagnostics) {
	res, err := r.client.Workspaces.Get(ctx, operations.GetWorkspaceRequest{ID: workspaceID})
	if err != nil {
		diags.AddError("failure to invoke API", err.Error())
		if res != nil && res.RawResponse != nil {
			diags.AddError("unexpected http request/response", debugResponse(res.RawResponse))
		}
		return "", false, diags
	}
	if res == nil {
		diags.AddError("unexpected response from API", fmt.Sprintf("%v", res))
		return "", false, diags
	}
	if res.StatusCode == 404 {
		return "", false, diags
	}
	if res.StatusCode != 200 {
		diags.AddError(fmt.Sprintf("unexpected response from API. Got an unexpected response code %v", res.StatusCode), debugResponse(res.RawResponse))
		return "", false, diags
	}
	if res.GetWorkspaceResponse == nil || res.GetWorkspaceResponse.Data.DefaultGuardrailID == "" {
		diags.AddError("unexpected response from API. Workspace has no default_guardrail_id", debugResponse(res.RawResponse))
		return "", false, diags
	}
	return res.GetWorkspaceResponse.Data.DefaultGuardrailID, true, diags
}

// patch writes data's configuration with PATCH /guardrails/{data.ID}. This is
// the only write the platform accepts for the default guardrail and it
// succeeds whether or not the guardrail is already materialized.
func (r *WorkspaceDefaultGuardrailResource) patch(ctx context.Context, data *WorkspaceDefaultGuardrailResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	request, requestDiags := data.ToOperationsUpdateGuardrailRequest(ctx)
	diags.Append(requestDiags...)
	if diags.HasError() {
		return diags
	}
	res, err := r.client.Guardrails.Update(ctx, *request)
	if err != nil {
		diags.AddError("failure to invoke API", err.Error())
		if res != nil && res.RawResponse != nil {
			diags.AddError("unexpected http request/response", debugResponse(res.RawResponse))
		}
		return diags
	}
	if res == nil {
		diags.AddError("unexpected response from API", fmt.Sprintf("%v", res))
		return diags
	}
	if res.StatusCode != 200 {
		diags.AddError(fmt.Sprintf("unexpected response from API. Got an unexpected response code %v", res.StatusCode), debugResponse(res.RawResponse))
		return diags
	}
	if res.UpdateGuardrailResponse == nil {
		diags.AddError("unexpected response from API. Got an unexpected response body", debugResponse(res.RawResponse))
		return diags
	}
	diags.Append(data.RefreshFromSharedUpdateGuardrailResponse(ctx, res.UpdateGuardrailResponse)...)
	return diags
}

func workspaceNotFound(workspaceID string) diag.Diagnostic {
	return diag.NewErrorDiagnostic("workspace not found", fmt.Sprintf("Workspace %q does not exist, so its default guardrail cannot be configured.", workspaceID))
}

func (r *WorkspaceDefaultGuardrailResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data *WorkspaceDefaultGuardrailResourceModel
	var plan types.Object

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(plan.As(ctx, &data, basetypes.ObjectAsOptions{
		UnhandledNullAsEmpty:    true,
		UnhandledUnknownAsEmpty: true,
	})...)
	if resp.Diagnostics.HasError() {
		return
	}

	workspaceID := data.WorkspaceID.ValueString()
	id, found, diags := r.resolveDefaultGuardrailID(ctx, workspaceID)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		resp.Diagnostics.Append(workspaceNotFound(workspaceID))
		return
	}
	data.ID = types.StringValue(id)

	resp.Diagnostics.Append(r.patch(ctx, data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	data.ID = types.StringValue(id)
	data.WorkspaceID = types.StringValue(workspaceID)

	resp.Diagnostics.Append(refreshPlan(ctx, plan, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *WorkspaceDefaultGuardrailResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data *WorkspaceDefaultGuardrailResourceModel
	var item types.Object

	resp.Diagnostics.Append(req.State.Get(ctx, &item)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(item.As(ctx, &data, basetypes.ObjectAsOptions{
		UnhandledNullAsEmpty:    true,
		UnhandledUnknownAsEmpty: true,
	})...)
	if resp.Diagnostics.HasError() {
		return
	}

	workspaceID := data.WorkspaceID.ValueString()
	id, found, diags := r.resolveDefaultGuardrailID(ctx, workspaceID)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	res, err := r.client.Guardrails.GetGuardrail(ctx, operations.GetGuardrailRequest{ID: id})
	if err != nil {
		resp.Diagnostics.AddError("failure to invoke API", err.Error())
		if res != nil && res.RawResponse != nil {
			resp.Diagnostics.AddError("unexpected http request/response", debugResponse(res.RawResponse))
		}
		return
	}
	if res == nil {
		resp.Diagnostics.AddError("unexpected response from API", fmt.Sprintf("%v", res))
		return
	}
	if res.StatusCode == 404 {
		// The workspace exists, so its default guardrail exists too; it has
		// simply never been written. Report it as present and unconfigured.
		data = &WorkspaceDefaultGuardrailResourceModel{
			ID:          types.StringValue(id),
			WorkspaceID: types.StringValue(workspaceID),
		}
		resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
		return
	}
	if res.StatusCode != 200 {
		resp.Diagnostics.AddError(fmt.Sprintf("unexpected response from API. Got an unexpected response code %v", res.StatusCode), debugResponse(res.RawResponse))
		return
	}
	if res.GetGuardrailResponse == nil {
		resp.Diagnostics.AddError("unexpected response from API. Got an unexpected response body", debugResponse(res.RawResponse))
		return
	}
	resp.Diagnostics.Append(data.RefreshFromSharedGetGuardrailResponse(ctx, res.GetGuardrailResponse)...)
	if resp.Diagnostics.HasError() {
		return
	}
	data.ID = types.StringValue(id)
	data.WorkspaceID = types.StringValue(workspaceID)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *WorkspaceDefaultGuardrailResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data *WorkspaceDefaultGuardrailResourceModel
	var plan types.Object

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	merge(ctx, req, resp, &data)
	if resp.Diagnostics.HasError() {
		return
	}

	workspaceID := data.WorkspaceID.ValueString()
	id, found, diags := r.resolveDefaultGuardrailID(ctx, workspaceID)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		resp.Diagnostics.Append(workspaceNotFound(workspaceID))
		return
	}
	data.ID = types.StringValue(id)

	resp.Diagnostics.Append(r.patch(ctx, data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	data.ID = types.StringValue(id)
	data.WorkspaceID = types.StringValue(workspaceID)

	resp.Diagnostics.Append(refreshPlan(ctx, plan, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Delete only forgets the resource. The default guardrail is intrinsic to
// its workspace and has no delete endpoint; its current configuration stays
// in effect until the workspace is deleted or the guardrail is managed again.
func (r *WorkspaceDefaultGuardrailResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
}

// ImportState accepts the workspace id (UUID or slug). Read resolves the
// guardrail id from the workspace and tolerates an unmaterialized default.
func (r *WorkspaceDefaultGuardrailResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace_id"), req.ID)...)
}
