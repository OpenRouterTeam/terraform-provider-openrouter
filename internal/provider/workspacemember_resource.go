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

const workspaceMemberPageSize int64 = 100

var _ resource.Resource = &WorkspaceMemberResource{}
var _ resource.ResourceWithImportState = &WorkspaceMemberResource{}

func NewWorkspaceMemberResource() resource.Resource {
	return &WorkspaceMemberResource{}
}

// WorkspaceMemberResource manages one organization member's membership in
// one non-default workspace.
type WorkspaceMemberResource struct {
	client *sdk.OpenRouter
}

type WorkspaceMemberResourceModel struct {
	CreatedAt   types.String `tfsdk:"created_at"`
	ID          types.String `tfsdk:"id"`
	Role        types.String `tfsdk:"role"`
	UserID      types.String `tfsdk:"user_id"`
	WorkspaceID types.String `tfsdk:"workspace_id"`
}

func (r *WorkspaceMemberResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace_member"
}

func (r *WorkspaceMemberResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	identity := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	notEmpty := []validator.String{stringvalidator.LengthAtLeast(1)}
	computed := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Adds one organization member to one workspace.\n\n" +
			"The member must already belong to the organization, and joins the workspace with the role they hold in the organization (`admin` or `member`). " +
			"The default workspace cannot be managed with this resource: every organization member belongs to it implicitly. " +
			"This resource manages only the membership it declares, so members added in the dashboard or through SCIM group mappings are left alone. " +
			"Declare at most one `openrouter_workspace_member` per member and workspace.\n\n" +
			"Changing any attribute replaces the resource. If the member is removed outside Terraform, the next plan re-adds them. If the workspace is deleted, the membership is removed from state. " +
			"Destroying the resource fails while the member still owns active API keys in the workspace, and for SCIM-managed members, whose membership must be changed in the identity provider.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: computed,
				Description:   "Import identifier of the membership, `<workspace_id>/<user_id>`.",
			},
			"workspace_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: identity,
				Validators:    notEmpty,
				Description:   "ID (UUID) of the workspace. Slugs are rejected. Requires replacement if changed.",
			},
			"user_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: identity,
				Validators:    notEmpty,
				Description:   "User ID of the organization member to add to the workspace. Requires replacement if changed.",
			},
			"role": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: computed,
				Description:   "Role of the member in the workspace, derived from their organization role. One of `admin` or `member`.",
			},
			"created_at": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: computed,
				Description:   "ISO 8601 timestamp of when the membership was created.",
			},
		},
	}
}

func (r *WorkspaceMemberResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *WorkspaceMemberResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data WorkspaceMemberResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	workspaceID := data.WorkspaceID.ValueString()
	userID := data.UserID.ValueString()

	resp.Diagnostics.Append(r.checkWorkspaceID(ctx, workspaceID)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.client.Workspaces.AddMembers(ctx, operations.BulkAddWorkspaceMembersRequest{
		ID:   workspaceID,
		Body: shared.BulkAddWorkspaceMembersRequest{UserIds: []string{userID}},
	})
	if err != nil {
		resp.Diagnostics.AddError("failure to invoke API", err.Error())
		return
	}
	if res == nil {
		resp.Diagnostics.AddError("unexpected response from API", "nil response")
		return
	}
	if res.StatusCode != 200 || res.BulkAddWorkspaceMembersResponse == nil {
		resp.Diagnostics.AddError(
			fmt.Sprintf("failed to add member %q to workspace %q", userID, workspaceID),
			fmt.Sprintf("API returned status %d: %s", res.StatusCode, workspaceMemberAPIErrorMessage(res.RawResponse, res.BadRequestResponse, res.ForbiddenResponse, res.NotFoundResponse, res.UnauthorizedResponse, res.InternalServerResponse)),
		)
		return
	}

	member := findWorkspaceMember(res.BulkAddWorkspaceMembersResponse.Data, userID)
	if member == nil {
		found, diags := r.findMember(ctx, workspaceID, userID)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		if found == nil {
			resp.Diagnostics.AddError(
				fmt.Sprintf("member %q not found in workspace %q after adding", userID, workspaceID),
				"The API accepted the request but the membership is not listed.",
			)
			return
		}
		member = found
	}

	setWorkspaceMemberState(&data, member)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *WorkspaceMemberResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data WorkspaceMemberResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	member, diags := r.findMember(ctx, data.WorkspaceID.ValueString(), data.UserID.ValueString())
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if member == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	setWorkspaceMemberState(&data, member)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update is only reachable without attribute changes because every
// configurable attribute requires replacement.
func (r *WorkspaceMemberResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data WorkspaceMemberResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *WorkspaceMemberResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data WorkspaceMemberResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	workspaceID := data.WorkspaceID.ValueString()
	userID := data.UserID.ValueString()

	res, err := r.client.Workspaces.BulkRemoveMembers(ctx, operations.BulkRemoveWorkspaceMembersRequest{
		ID:   workspaceID,
		Body: shared.BulkRemoveWorkspaceMembersRequest{UserIds: []string{userID}},
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
	if res.StatusCode != 200 || res.BulkRemoveWorkspaceMembersResponse == nil {
		resp.Diagnostics.AddError(
			fmt.Sprintf("failed to remove member %q from workspace %q", userID, workspaceID),
			fmt.Sprintf("API returned status %d: %s", res.StatusCode, workspaceMemberAPIErrorMessage(res.RawResponse, res.BadRequestResponse, res.ForbiddenResponse, res.NotFoundResponse, res.UnauthorizedResponse, res.InternalServerResponse)),
		)
		return
	}
}

func (r *WorkspaceMemberResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	workspaceID, userID, err := parseWorkspaceMemberID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("invalid import ID", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace_id"), workspaceID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("user_id"), userID)...)
}

// checkWorkspaceID verifies that workspaceID names an existing workspace by ID,
// so state and import IDs never hold a slug.
func (r *WorkspaceMemberResource) checkWorkspaceID(ctx context.Context, workspaceID string) diag.Diagnostics {
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
	if resolved := res.GetWorkspaceResponse.Data.ID; resolved != workspaceID {
		diags.AddAttributeError(path.Root("workspace_id"), "workspace_id must be a workspace ID",
			fmt.Sprintf("%q resolved to workspace %q. Set workspace_id to the workspace ID, not its slug.", workspaceID, resolved))
	}
	return diags
}

// findMember pages through the workspace's members. It returns nil without
// diagnostics when the workspace or the member does not exist.
func (r *WorkspaceMemberResource) findMember(ctx context.Context, workspaceID, userID string) (*shared.WorkspaceMember, diag.Diagnostics) {
	var diags diag.Diagnostics

	limit := workspaceMemberPageSize
	for offset := int64(0); ; {
		pageOffset := offset
		res, err := r.client.Workspaces.ListMembers(ctx, operations.ListWorkspaceMembersRequest{
			ID:     workspaceID,
			Offset: &pageOffset,
			Limit:  &limit,
		})
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
		if res.StatusCode != 200 || res.ListWorkspaceMembersResponse == nil {
			diags.AddError(fmt.Sprintf("failed to list members of workspace %q", workspaceID), fmt.Sprintf("API returned status %d: %s", res.StatusCode, workspaceMemberAPIErrorMessage(res.RawResponse, nil, res.ForbiddenResponse, res.NotFoundResponse, res.UnauthorizedResponse, res.InternalServerResponse)))
			return nil, diags
		}

		page := res.ListWorkspaceMembersResponse.Data
		if member := findWorkspaceMember(page, userID); member != nil {
			return member, diags
		}
		offset += int64(len(page))
		if len(page) == 0 || offset >= res.ListWorkspaceMembersResponse.TotalCount {
			return nil, diags
		}
	}
}

func findWorkspaceMember(members []shared.WorkspaceMember, userID string) *shared.WorkspaceMember {
	for i := range members {
		if members[i].UserID == userID {
			return &members[i]
		}
	}
	return nil
}

func setWorkspaceMemberState(data *WorkspaceMemberResourceModel, member *shared.WorkspaceMember) {
	data.ID = types.StringValue(workspaceMemberID(data.WorkspaceID.ValueString(), data.UserID.ValueString()))
	data.Role = types.StringValue(string(member.Role))
	data.CreatedAt = types.StringValue(member.CreatedAt)
}

func workspaceMemberID(workspaceID, userID string) string {
	return workspaceID + "/" + userID
}

func parseWorkspaceMemberID(id string) (workspaceID, userID string, err error) {
	parts := strings.Split(id, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("expected import ID in the format <workspace_id>/<user_id>, got %q", id)
	}
	return parts[0], parts[1], nil
}

func workspaceMemberAPIErrorMessage(raw *http.Response, badRequest *shared.BadRequestResponse, forbidden *shared.ForbiddenResponse, notFound *shared.NotFoundResponse, unauthorized *shared.UnauthorizedResponse, internalServer *shared.InternalServerResponse) string {
	if forbidden != nil && forbidden.Error.Message != "" {
		return forbidden.Error.Message
	}
	return apiErrorMessage(raw, badRequest, notFound, unauthorized, internalServer)
}
