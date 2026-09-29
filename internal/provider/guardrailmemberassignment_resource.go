package provider

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/operations"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/shared"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const guardrailMemberAssignmentPageSize int64 = 100

var _ resource.Resource = &GuardrailMemberAssignmentResource{}
var _ resource.ResourceWithImportState = &GuardrailMemberAssignmentResource{}

func NewGuardrailMemberAssignmentResource() resource.Resource {
	return &GuardrailMemberAssignmentResource{}
}

// GuardrailMemberAssignmentResource manages one direct assignment of a
// guardrail to one organization member within one workspace.
type GuardrailMemberAssignmentResource struct {
	client *sdk.OpenRouter
}

type GuardrailMemberAssignmentResourceModel struct {
	GuardrailID types.String `tfsdk:"guardrail_id"`
	ID          types.String `tfsdk:"id"`
	UserID      types.String `tfsdk:"user_id"`
	WorkspaceID types.String `tfsdk:"workspace_id"`
}

func (r *GuardrailMemberAssignmentResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_guardrail_member_assignment"
}

func (r *GuardrailMemberAssignmentResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	identity := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	notEmpty := []validator.String{stringvalidator.LengthAtLeast(1)}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Assigns one guardrail directly to one organization member within one workspace.\n\n" +
			"This resource manages a direct member assignment only. It does not manage SCIM group mappings, API key assignments, or the workspace default guardrail, " +
			"which applies to every member without a direct assignment and cannot be assigned directly. " +
			"The guardrail must belong to `workspace_id`; a guardrail in another workspace, a legacy guardrail with no workspace, or the workspace default guardrail is rejected before any assignment is made. " +
			"For workspaces other than the default workspace, the API also rejects members who do not belong to the workspace.\n\n" +
			"A member has at most one direct guardrail per workspace. Assigning a member who already has a different guardrail in the workspace moves the member to this guardrail, " +
			"so declare at most one `openrouter_guardrail_member_assignment` per member and workspace. " +
			"Changing any attribute replaces the resource. If the assignment is removed or the member is moved to another guardrail outside Terraform, the next plan recreates it. " +
			"If the guardrail is deleted, the assignment is removed from state.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
				Description: "Import identifier of the assignment, `<workspace_id>/<guardrail_id>/<user_id>`.",
			},
			"workspace_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: identity,
				Validators:    notEmpty,
				Description:   "ID (UUID) of the workspace the guardrail belongs to. Slugs are rejected. Requires replacement if changed.",
			},
			"guardrail_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: identity,
				Validators:    notEmpty,
				Description:   "ID of the guardrail to assign. Must belong to `workspace_id` and must not be the workspace default guardrail. Requires replacement if changed.",
			},
			"user_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: identity,
				Validators:    notEmpty,
				Description:   "User ID of the organization member to assign the guardrail to. Requires replacement if changed.",
			},
		},
	}
}

func (r *GuardrailMemberAssignmentResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *GuardrailMemberAssignmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data GuardrailMemberAssignmentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	workspaceID := data.WorkspaceID.ValueString()
	guardrailID := data.GuardrailID.ValueString()
	userID := data.UserID.ValueString()

	resp.Diagnostics.Append(r.checkWorkspaceScope(ctx, workspaceID, guardrailID)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.client.Guardrails.BulkAssignMembers(ctx, operations.BulkAssignMembersToGuardrailRequest{
		ID:   guardrailID,
		Body: shared.BulkAssignMembersRequest{MemberUserIds: []string{userID}},
	})
	if err != nil {
		resp.Diagnostics.AddError("failure to invoke API", err.Error())
		return
	}
	if res == nil {
		resp.Diagnostics.AddError("unexpected response from API", "nil response")
		return
	}
	if res.StatusCode != 200 || res.BulkAssignMembersResponse == nil {
		resp.Diagnostics.AddError(
			fmt.Sprintf("failed to assign guardrail %q to member %q", guardrailID, userID),
			fmt.Sprintf("API returned status %d: %s", res.StatusCode, apiErrorMessage(res.RawResponse, res.BadRequestResponse, res.NotFoundResponse, res.UnauthorizedResponse, res.InternalServerResponse)),
		)
		return
	}

	data.ID = types.StringValue(guardrailMemberAssignmentID(workspaceID, guardrailID, userID))
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *GuardrailMemberAssignmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data GuardrailMemberAssignmentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	workspaceID := data.WorkspaceID.ValueString()
	guardrailID := data.GuardrailID.ValueString()
	userID := data.UserID.ValueString()

	guardrail, diags := r.getGuardrail(ctx, guardrailID)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if guardrail == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(checkGuardrailWorkspace(guardrail, workspaceID)...)
	if resp.Diagnostics.HasError() {
		return
	}

	found, diags := r.memberAssigned(ctx, guardrailID, userID)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	data.ID = types.StringValue(guardrailMemberAssignmentID(workspaceID, guardrailID, userID))
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update is only reachable without attribute changes because every
// configurable attribute requires replacement.
func (r *GuardrailMemberAssignmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data GuardrailMemberAssignmentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *GuardrailMemberAssignmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data GuardrailMemberAssignmentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	guardrailID := data.GuardrailID.ValueString()
	userID := data.UserID.ValueString()

	// The API removes the member only from this guardrail, so a member that
	// was moved to another guardrail keeps that assignment.
	res, err := r.client.Guardrails.BulkUnassignMembers(ctx, operations.BulkUnassignMembersFromGuardrailRequest{
		ID:   guardrailID,
		Body: shared.BulkUnassignMembersRequest{MemberUserIds: []string{userID}},
	})
	if err != nil {
		resp.Diagnostics.AddError("failure to invoke API", err.Error())
		return
	}
	if res == nil {
		resp.Diagnostics.AddError("unexpected response from API", "nil response")
		return
	}
	if res.StatusCode == 404 {
		return
	}
	if res.StatusCode != 200 || res.BulkUnassignMembersResponse == nil {
		resp.Diagnostics.AddError(
			fmt.Sprintf("failed to unassign guardrail %q from member %q", guardrailID, userID),
			fmt.Sprintf("API returned status %d: %s", res.StatusCode, apiErrorMessage(res.RawResponse, res.BadRequestResponse, res.NotFoundResponse, res.UnauthorizedResponse, res.InternalServerResponse)),
		)
	}
}

func (r *GuardrailMemberAssignmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	workspaceID, guardrailID, userID, err := parseGuardrailMemberAssignmentID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("invalid import ID", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace_id"), workspaceID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("guardrail_id"), guardrailID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("user_id"), userID)...)
}

// checkWorkspaceScope verifies that workspaceID names an existing workspace by
// ID and that guardrailID is a non-default guardrail inside it.
func (r *GuardrailMemberAssignmentResource) checkWorkspaceScope(ctx context.Context, workspaceID, guardrailID string) diag.Diagnostics {
	var diags diag.Diagnostics

	res, err := r.client.Workspaces.Get(ctx, operations.GetWorkspaceRequest{ID: workspaceID})
	if err != nil {
		diags.AddError("failure to invoke API", err.Error())
		return diags
	}
	if res == nil {
		diags.AddError("unexpected response from API", "nil response")
		return diags
	}
	if res.StatusCode == 404 {
		diags.AddAttributeError(path.Root("workspace_id"), "workspace not found", fmt.Sprintf("Workspace %q does not exist.", workspaceID))
		return diags
	}
	if res.StatusCode != 200 || res.GetWorkspaceResponse == nil {
		diags.AddError(fmt.Sprintf("failed to read workspace %q", workspaceID), fmt.Sprintf("API returned status %d: %s", res.StatusCode, apiErrorMessage(res.RawResponse, nil, res.NotFoundResponse, res.UnauthorizedResponse, res.InternalServerResponse)))
		return diags
	}
	workspace := res.GetWorkspaceResponse.Data
	if workspace.ID != workspaceID {
		diags.AddAttributeError(path.Root("workspace_id"), "workspace_id must be a workspace ID",
			fmt.Sprintf("%q resolved to workspace %q. Set workspace_id to the workspace ID, not its slug.", workspaceID, workspace.ID))
		return diags
	}
	if workspace.DefaultGuardrailID == guardrailID {
		diags.AddAttributeError(path.Root("guardrail_id"), "cannot assign the workspace default guardrail",
			fmt.Sprintf("Guardrail %q is the default guardrail of workspace %q. It applies to every member without a direct assignment and cannot be assigned to members.", guardrailID, workspaceID))
		return diags
	}

	guardrail, getDiags := r.getGuardrail(ctx, guardrailID)
	diags.Append(getDiags...)
	if diags.HasError() {
		return diags
	}
	if guardrail == nil {
		diags.AddAttributeError(path.Root("guardrail_id"), "guardrail not found", fmt.Sprintf("Guardrail %q does not exist.", guardrailID))
		return diags
	}
	diags.Append(checkGuardrailWorkspace(guardrail, workspaceID)...)
	return diags
}

// getGuardrail returns nil without diagnostics when the guardrail does not exist.
func (r *GuardrailMemberAssignmentResource) getGuardrail(ctx context.Context, guardrailID string) (*shared.GetGuardrailResponseData, diag.Diagnostics) {
	var diags diag.Diagnostics

	res, err := r.client.Guardrails.GetGuardrail(ctx, operations.GetGuardrailRequest{ID: guardrailID})
	if err != nil {
		diags.AddError("failure to invoke API", err.Error())
		return nil, diags
	}
	if res == nil {
		diags.AddError("unexpected response from API", "nil response")
		return nil, diags
	}
	if res.StatusCode == 404 {
		return nil, diags
	}
	if res.StatusCode != 200 || res.GetGuardrailResponse == nil {
		diags.AddError(fmt.Sprintf("failed to read guardrail %q", guardrailID), fmt.Sprintf("API returned status %d: %s", res.StatusCode, apiErrorMessage(res.RawResponse, nil, res.NotFoundResponse, res.UnauthorizedResponse, res.InternalServerResponse)))
		return nil, diags
	}
	return &res.GetGuardrailResponse.Data, diags
}

func checkGuardrailWorkspace(guardrail *shared.GetGuardrailResponseData, workspaceID string) diag.Diagnostics {
	var diags diag.Diagnostics
	if guardrail.WorkspaceID == nil || *guardrail.WorkspaceID == "" {
		diags.AddAttributeError(path.Root("guardrail_id"), "guardrail has no workspace",
			fmt.Sprintf("Guardrail %q is not scoped to a workspace, so it cannot be assigned to members.", guardrail.ID))
		return diags
	}
	if *guardrail.WorkspaceID != workspaceID {
		diags.AddAttributeError(path.Root("guardrail_id"), "guardrail belongs to a different workspace",
			fmt.Sprintf("Guardrail %q belongs to workspace %q, not %q.", guardrail.ID, *guardrail.WorkspaceID, workspaceID))
	}
	return diags
}

// memberAssigned pages through the guardrail's direct member assignments.
func (r *GuardrailMemberAssignmentResource) memberAssigned(ctx context.Context, guardrailID, userID string) (bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	limit := guardrailMemberAssignmentPageSize
	for offset := int64(0); ; {
		pageOffset := offset
		res, err := r.client.Guardrails.ListMemberAssignmentsByGuardrail(ctx, operations.ListGuardrailMemberAssignmentsRequest{
			ID:     guardrailID,
			Offset: &pageOffset,
			Limit:  &limit,
		})
		if err != nil {
			diags.AddError("failure to invoke API", err.Error())
			return false, diags
		}
		if res == nil {
			diags.AddError("unexpected response from API", "nil response")
			return false, diags
		}
		if res.StatusCode == 404 {
			return false, diags
		}
		if res.StatusCode != 200 || res.ListMemberAssignmentsResponse == nil {
			diags.AddError(fmt.Sprintf("failed to list member assignments of guardrail %q", guardrailID), fmt.Sprintf("API returned status %d: %s", res.StatusCode, apiErrorMessage(res.RawResponse, nil, res.NotFoundResponse, res.UnauthorizedResponse, res.InternalServerResponse)))
			return false, diags
		}

		page := res.ListMemberAssignmentsResponse.Data
		for _, assignment := range page {
			if assignment.UserID == userID && assignment.GuardrailID == guardrailID {
				return true, diags
			}
		}
		offset += int64(len(page))
		if len(page) == 0 || offset >= res.ListMemberAssignmentsResponse.TotalCount {
			return false, diags
		}
	}
}

func guardrailMemberAssignmentID(workspaceID, guardrailID, userID string) string {
	return workspaceID + "/" + guardrailID + "/" + userID
}

func parseGuardrailMemberAssignmentID(id string) (workspaceID, guardrailID, userID string, err error) {
	parts := strings.Split(id, "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", "", fmt.Errorf("expected import ID in the format <workspace_id>/<guardrail_id>/<user_id>, got %q", id)
	}
	return parts[0], parts[1], parts[2], nil
}

// apiErrorMessage returns the API error message from whichever typed error
// payload is set, falling back to a dump of the raw response.
func apiErrorMessage(raw *http.Response, badRequest *shared.BadRequestResponse, notFound *shared.NotFoundResponse, unauthorized *shared.UnauthorizedResponse, internalServer *shared.InternalServerResponse) string {
	switch {
	case badRequest != nil && badRequest.Error.Message != "":
		return badRequest.Error.Message
	case notFound != nil && notFound.Error.Message != "":
		return notFound.Error.Message
	case unauthorized != nil && unauthorized.Error.Message != "":
		return unauthorized.Error.Message
	case internalServer != nil && internalServer.Error.Message != "":
		return internalServer.Error.Message
	case raw != nil:
		return debugResponse(raw)
	default:
		return "no error message"
	}
}
