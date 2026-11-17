package provider

import (
	"context"
	"fmt"

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

	speakeasy_stringplanmodifier "github.com/OpenRouterTeam/terraform-provider-openrouter/internal/planmodifiers/stringplanmodifier"
)

var _ resource.Resource = &GuardrailPolicyMemberAssignmentResource{}
var _ resource.ResourceWithImportState = &GuardrailPolicyMemberAssignmentResource{}

func NewGuardrailPolicyMemberAssignmentResource() resource.Resource {
	return &GuardrailPolicyMemberAssignmentResource{}
}

// GuardrailPolicyMemberAssignmentResource manages the assignment of one typed
// guardrail policy to one organization member.
type GuardrailPolicyMemberAssignmentResource struct {
	client *sdk.OpenRouter
}

type GuardrailPolicyMemberAssignmentResourceModel struct {
	ID          types.String `tfsdk:"id"`
	MemberID    types.String `tfsdk:"member_id"`
	PolicyID    types.String `tfsdk:"policy_id"`
	PolicyType  types.String `tfsdk:"policy_type"`
	WorkspaceID types.String `tfsdk:"workspace_id"`
}

func (r *GuardrailPolicyMemberAssignmentResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_guardrail_policy_member_assignment"
}

func (r *GuardrailPolicyMemberAssignmentResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	identity := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Assigns one typed guardrail policy to one organization member, and removes the assignment when the resource is destroyed.\n\n" +
			"A policy does nothing until it is assigned. `openrouter_guardrail_policy_budget`, `openrouter_guardrail_policy_moderation` and `openrouter_guardrail_policy_model_availability` create the policies; this resource attaches one of them to a member. A member holds at most one policy of each type: the route sets the assignment for a policy type rather than adding to it, so assigning a second policy of the same type to the same member displaces the first. Different types are independent, so a member can hold one of each and that is one resource each; two resources of one type on one member would displace each other on every apply.\n\n" +
			"A policy a member inherits from the workspace default is not an assignment and is not managed here. The read returns the directly assigned policies and the inherited ones separately, and only the directly assigned list is compared, so a member that inherits a policy without holding it does not read as drift.\n\n" +
			"The API changes assignments one policy at a time and requires the state the caller last read, answering 409 once that state has moved. The provider reads before every write and reads again after a conflict, so an assignment attached or removed elsewhere between two applies is reconciled rather than fought over, and a conflict that survives the retry is reported instead of retried indefinitely. A destroy of an assignment that is already gone is a no-op rather than a failure. Creating a resource for an assignment that is already in place adopts it without writing, and destroying it removes the assignment whoever put it there, so an assignment is not owned by the configuration that declares it.\n\n" +
			"Changing `member_id`, `policy_id` or `policy_type` replaces the resource: the destroy removes the old assignment and the create makes the new one. A policy cannot be moved between workspaces; where state records a workspace_id, changing it plans a replacement instead, and removing `workspace_id` from configuration is refused at plan time because the read that follows needs that scope. `workspace_id` is required for an organization management key and may be omitted for a workspace-scoped key, which defaults to its own workspace.\n\n" +
			"Import takes `<member_id>/<policy_type>/<policy_id>`, or `<workspace_id>/<member_id>/<policy_type>/<policy_id>` for an organization management key. The stored `id` is the bare form, so importing it under an organization management key means adding the workspace prefix; the API never returns the workspace, and a read that reaches the route without one is refused rather than scoped to a default.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
				Description: "Import identifier of the assignment, `<member_id>/<policy_type>/<policy_id>`.",
			},
			"member_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: identity,
				Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
				Description:   "Organization member user ID to assign the policy to. Requires replacement if changed.",
			},
			"policy_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: identity,
				Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
				Description:   "ID of the policy to assign, for example `openrouter_guardrail_policy_moderation.example.id`. Requires replacement if changed.",
			},
			"policy_type": schema.StringAttribute{
				Required:      true,
				PlanModifiers: identity,
				Validators:    []validator.String{guardrailPolicyAssignmentTypeValidator()},
				Description:   "Type of the policy being assigned: `budget`, `model_availability`, `moderation` or `rate_limit`. Must match the policy's own type. Requires replacement if changed.",
			},
			"workspace_id": schema.StringAttribute{
				Optional:    true,
				Description: "Workspace the policy belongs to. Required for an organization management key; a workspace-scoped key defaults to its own workspace and gets 403 for any other.",
				PlanModifiers: []planmodifier.String{
					speakeasy_stringplanmodifier.GuardrailPolicyWorkspaceImmutable(),
				},
			},
		},
	}
}

func (r *GuardrailPolicyMemberAssignmentResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *GuardrailPolicyMemberAssignmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data GuardrailPolicyMemberAssignmentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	workspaceID := data.WorkspaceID.ValueString()
	assignment := guardrailPolicyAssignmentFromMemberModel(data)

	resp.Diagnostics.Append(reconcileGuardrailPolicyAssignment(ctx, guardrailPolicyMemberAssignmentAPI(r.client), assignment, workspaceID, true)...)
	if resp.Diagnostics.HasError() {
		return
	}

	data.ID = types.StringValue(guardrailPolicyAssignmentID(assignment))
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *GuardrailPolicyMemberAssignmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data GuardrailPolicyMemberAssignmentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	workspaceID := data.WorkspaceID.ValueString()
	assignment := guardrailPolicyAssignmentFromMemberModel(data)

	got, diags := guardrailPolicyMemberAssignmentAPI(r.client).read(ctx, assignment, workspaceID)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if got.NotFound || !assignmentHolds(*got.State, assignment.PolicyID) {
		// The subject, the workspace scope or the assignment itself is gone, so
		// the resource is absent and the next plan re-creates it.
		resp.State.RemoveResource(ctx)
		return
	}

	data.ID = types.StringValue(guardrailPolicyAssignmentID(assignment))
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update is only reachable without attribute changes: every configurable
// attribute either requires replacement or is refused at plan time when it is
// removed, and a workspace change plans a replacement.
func (r *GuardrailPolicyMemberAssignmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data GuardrailPolicyMemberAssignmentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *GuardrailPolicyMemberAssignmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data GuardrailPolicyMemberAssignmentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(reconcileGuardrailPolicyAssignment(
		ctx,
		guardrailPolicyMemberAssignmentAPI(r.client),
		guardrailPolicyAssignmentFromMemberModel(data),
		data.WorkspaceID.ValueString(),
		false,
	)...)
}

func (r *GuardrailPolicyMemberAssignmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	workspaceID, assignment, err := parseGuardrailPolicyAssignmentID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("invalid import ID", err.Error())
		return
	}

	// The stored identifier is the tuple, not the import string: the four-part
	// form carries the workspace as well, and the workspace is an attribute.
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), guardrailPolicyAssignmentID(assignment))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("member_id"), assignment.SubjectID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("policy_type"), assignment.PolicyType)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("policy_id"), assignment.PolicyID)...)
	if workspaceID != "" {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace_id"), workspaceID)...)
	}
}

func guardrailPolicyAssignmentFromMemberModel(data GuardrailPolicyMemberAssignmentResourceModel) guardrailPolicyAssignment {
	return guardrailPolicyAssignment{
		SubjectID:  data.MemberID.ValueString(),
		PolicyType: data.PolicyType.ValueString(),
		PolicyID:   data.PolicyID.ValueString(),
	}
}

// guardrailPolicyMemberAssignmentAPI binds the shared read-modify-write to the
// member routes.
func guardrailPolicyMemberAssignmentAPI(client *sdk.OpenRouter) guardrailPolicyAssignmentAPI {
	return guardrailPolicyAssignmentAPI{
		subjectPath: path.Root("member_id"),
		read: func(ctx context.Context, a guardrailPolicyAssignment, workspaceID string) (guardrailPolicyAssignmentRead, diag.Diagnostics) {
			var diags diag.Diagnostics

			res, err := client.GuardrailPolicies.GetMemberPolicyAssignment(ctx, operations.GetMemberPolicyAssignmentRequest{
				MemberID:    a.SubjectID,
				PolicyType:  operations.GetMemberPolicyAssignmentPolicyType(a.PolicyType),
				WorkspaceID: guardrailPolicyAssignmentOptionalString(workspaceID),
			})
			if err != nil {
				diags.AddError("failure to invoke API", err.Error())
				return guardrailPolicyAssignmentRead{}, diags
			}
			if res == nil {
				diags.AddError("unexpected response from API", "nil response")
				return guardrailPolicyAssignmentRead{}, diags
			}
			if res.StatusCode == 404 || (res.StatusCode == 400 && guardrailPolicyAssignmentSubjectGone(res.BadRequestResponse)) {
				// The subject or the workspace scope is gone, so there is no
				// assignment left to read and the resource is absent. The write
				// route also answers 404, for a policy that is not in the
				// workspace, which is a different case and not handled here.
				return guardrailPolicyAssignmentRead{NotFound: true}, diags
			}
			if res.StatusCode != 200 || res.GuardrailPolicyAssignmentResponse == nil {
				diags.AddError(
					fmt.Sprintf("failed to read the %s assignment of member %q", a.PolicyType, a.SubjectID),
					fmt.Sprintf("API returned status %d: %s", res.StatusCode, apiErrorMessage(res.RawResponse, res.BadRequestResponse, res.NotFoundResponse, res.UnauthorizedResponse, res.InternalServerResponse)),
				)
				return guardrailPolicyAssignmentRead{}, diags
			}

			state := res.GuardrailPolicyAssignmentResponse.Data
			return guardrailPolicyAssignmentRead{State: &state}, diags
		},
		change: func(ctx context.Context, a guardrailPolicyAssignment, workspaceID string, assigned bool, expected shared.GuardrailPolicyAssignmentState) (bool, string, diag.Diagnostics) {
			var diags diag.Diagnostics

			res, err := client.GuardrailPolicies.ChangeMemberPolicyAssignment(ctx, operations.ChangeMemberPolicyAssignmentRequest{
				MemberID: a.SubjectID,
				Body: shared.ChangeGuardrailPolicyAssignmentRequest{
					Assigned:           assigned,
					ExpectedAssignment: expected,
					PolicyID:           a.PolicyID,
					PolicyType:         shared.ChangeGuardrailPolicyAssignmentRequestPolicyType(a.PolicyType),
					WorkspaceID:        guardrailPolicyAssignmentOptionalString(workspaceID),
				},
			})
			if err != nil {
				diags.AddError("failure to invoke API", err.Error())
				return false, "", diags
			}
			if res == nil {
				diags.AddError("unexpected response from API", "nil response")
				return false, "", diags
			}
			if res.StatusCode == 409 {
				// Both a stale expected_assignment and a removal that was
				// already done arrive here, so the caller re-reads rather than
				// deciding from the status.
				return true, guardrailPolicyAssignmentConflictMessage(res.ConflictResponse), diags
			}
			if res.StatusCode == 404 {
				// Measured: a 404 here means the policy is not in this workspace,
				// not that the subject is missing, which is a 400. Assigning such
				// a policy cannot succeed; removing one leaves nothing behind,
				// because an assignment to a policy that does not exist is gone
				// whether or not the caller still has it in state.
				if assigned {
					diags.AddAttributeError(
						path.Root("policy_id"),
						"policy not found in this workspace",
						fmt.Sprintf("The API answered 404: policy %q is not in this workspace, so it cannot be assigned. Nothing was assigned.", a.PolicyID),
					)
				}
				return false, "", diags
			}
			if res.StatusCode != 200 || res.ChangeGuardrailPolicyAssignmentResponse == nil {
				diags.AddError(
					fmt.Sprintf("failed to change the %s assignment of member %q", a.PolicyType, a.SubjectID),
					fmt.Sprintf("API returned status %d: %s", res.StatusCode, apiErrorMessage(res.RawResponse, res.BadRequestResponse, res.NotFoundResponse, res.UnauthorizedResponse, res.InternalServerResponse)),
				)
				return false, "", diags
			}

			return false, "", diags
		},
	}
}
