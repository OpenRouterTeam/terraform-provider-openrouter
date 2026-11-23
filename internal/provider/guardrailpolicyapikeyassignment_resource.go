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

var _ resource.Resource = &GuardrailPolicyAPIKeyAssignmentResource{}
var _ resource.ResourceWithImportState = &GuardrailPolicyAPIKeyAssignmentResource{}

func NewGuardrailPolicyAPIKeyAssignmentResource() resource.Resource {
	return &GuardrailPolicyAPIKeyAssignmentResource{}
}

// GuardrailPolicyAPIKeyAssignmentResource manages the assignment of one typed
// guardrail policy to one API key.
type GuardrailPolicyAPIKeyAssignmentResource struct {
	client *sdk.OpenRouter
}

type GuardrailPolicyAPIKeyAssignmentResourceModel struct {
	ID          types.String `tfsdk:"id"`
	KeyHash     types.String `tfsdk:"key_hash"`
	PolicyID    types.String `tfsdk:"policy_id"`
	PolicyType  types.String `tfsdk:"policy_type"`
	WorkspaceID types.String `tfsdk:"workspace_id"`
}

func (r *GuardrailPolicyAPIKeyAssignmentResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_guardrail_policy_api_key_assignment"
}

func (r *GuardrailPolicyAPIKeyAssignmentResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	identity := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Assigns one typed guardrail policy to one API key, and removes the assignment when the resource is destroyed.\n\n" +
			"A policy that is not the workspace default does nothing until it is assigned. `openrouter_guardrail_policy_budget`, `openrouter_guardrail_policy_moderation` and `openrouter_guardrail_policy_model_availability` create the policies; this resource attaches one of them to a key. A key holds at most one policy of each type: the route sets the assignment for a policy type rather than adding to it, so assigning a second policy of the same type to the same key displaces the first. Different types are independent, so a key can hold one of each and that is one resource each; two resources of one type on one key would displace each other on every apply. A policy's own member_ids and api_key_hashes attributes name the same membership from the policy side, so declare one side or the other for a given subject and policy: competing declarations of one membership overwrite each other on alternate applies.\n\n" +
			"A policy a key inherits from the workspace default is not an assignment and is not managed here. The read returns the directly assigned policies and the inherited ones separately, and only the directly assigned list is compared, so a key that inherits a policy without holding it does not read as drift.\n\n" +
			"The API changes assignments one policy at a time and requires the state the caller last read, answering 409 once that state has moved. The provider reads before every write and reads again after the first conflict, so an assignment attached or removed elsewhere between two applies is reconciled rather than fought over, and a second conflict in a row is reported with the API's own message instead of retried indefinitely. A destroy of an assignment that is already gone is a no-op rather than a failure. Creating a resource for an assignment that is already in place adopts it without writing, and destroying it removes the assignment whoever put it there, so an assignment is not owned by the configuration that declares it.\n\n" +
			"Changing `key_hash`, `policy_id` or `policy_type` replaces the resource: the destroy removes the old assignment and the create makes the new one. A policy cannot be moved between workspaces; where state records a workspace_id, changing it plans a replacement instead, and removing `workspace_id` from configuration is refused at plan time because the read that follows needs that scope. `workspace_id` is required for an organization management key and may be omitted for a workspace-scoped key, which defaults to its own workspace. It is a UUID in every case: the routes that take a scope validate the field as one, so a workspace slug is refused with 400 `Invalid guardrail policy request`, the same answer an omitted scope gets, rather than being looked up. This resource's own update path never reaches the API, because a change to the scope plans a replacement.\n\n" +
			"Import takes `<key_hash>/<policy_type>/<policy_id>`, or `<workspace_id>/<key_hash>/<policy_type>/<policy_id>` for an organization management key. The stored `id` is the bare form, so importing it under an organization management key means adding the workspace prefix; the API never returns the workspace, and a read that reaches the route without one is refused rather than scoped to a default.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
				Description: "Import identifier of the assignment, `<key_hash>/<policy_type>/<policy_id>`. Importing under an organization management key needs the workspace prefix, which this stored identifier does not include.",
			},
			"key_hash": schema.StringAttribute{
				Required:      true,
				PlanModifiers: identity,
				Validators:    []validator.String{stringvalidator.LengthAtLeast(1), notRawAPIKey{}},
				Description:   "Hash of the API key to assign the policy to, for example `openrouter_api_key.example.hash`. Not the secret key itself. Requires replacement if changed.",
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
					speakeasy_stringplanmodifier.GuardrailPolicyAssignmentWorkspaceImmutable(),
					// The shared modifier replaces a changed workspace and refuses
					// a removal, but it returns early when state records no
					// workspace, on the assumption that the update reaches the API
					// and is answered with 400. This resource's Update never calls
					// the API, so without this second modifier a configuration
					// that added workspace_id to a resource whose state has none,
					// which a bare three-part import and a workspace-scoped
					// create both produce, would record the value unchecked.
					// Replacing instead routes it through create, where the API
					// refuses a bad scope, matching
					// guardrailmemberassignment_resource.go.
					stringplanmodifier.RequiresReplaceIfConfigured(),
				},
			},
		},
	}
}

func (r *GuardrailPolicyAPIKeyAssignmentResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *GuardrailPolicyAPIKeyAssignmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data GuardrailPolicyAPIKeyAssignmentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	workspaceID := data.WorkspaceID.ValueString()
	assignment := guardrailPolicyAssignmentFromAPIKeyModel(data)

	resp.Diagnostics.Append(reconcileGuardrailPolicyAssignment(ctx, guardrailPolicyAPIKeyAssignmentAPI(r.client), assignment, workspaceID, true)...)
	if resp.Diagnostics.HasError() {
		return
	}

	data.ID = types.StringValue(guardrailPolicyAssignmentID(assignment))
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *GuardrailPolicyAPIKeyAssignmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data GuardrailPolicyAPIKeyAssignmentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	workspaceID := data.WorkspaceID.ValueString()
	assignment := guardrailPolicyAssignmentFromAPIKeyModel(data)

	got, diags := guardrailPolicyAPIKeyAssignmentAPI(r.client).read(ctx, assignment, workspaceID)
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
func (r *GuardrailPolicyAPIKeyAssignmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data GuardrailPolicyAPIKeyAssignmentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *GuardrailPolicyAPIKeyAssignmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data GuardrailPolicyAPIKeyAssignmentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(reconcileGuardrailPolicyAssignment(
		ctx,
		guardrailPolicyAPIKeyAssignmentAPI(r.client),
		guardrailPolicyAssignmentFromAPIKeyModel(data),
		data.WorkspaceID.ValueString(),
		false,
	)...)
}

func (r *GuardrailPolicyAPIKeyAssignmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	workspaceID, assignment, err := parseGuardrailPolicyAssignmentID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("invalid import ID", err.Error())
		return
	}

	// The stored identifier is the tuple, not the import string: the four-part
	// form carries the workspace as well, and the workspace is an attribute.
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), guardrailPolicyAssignmentID(assignment))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("key_hash"), assignment.SubjectID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("policy_type"), assignment.PolicyType)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("policy_id"), assignment.PolicyID)...)
	if workspaceID != "" {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace_id"), workspaceID)...)
	}
}

func guardrailPolicyAssignmentFromAPIKeyModel(data GuardrailPolicyAPIKeyAssignmentResourceModel) guardrailPolicyAssignment {
	return guardrailPolicyAssignment{
		SubjectID:  data.KeyHash.ValueString(),
		PolicyType: data.PolicyType.ValueString(),
		PolicyID:   data.PolicyID.ValueString(),
	}
}

// guardrailPolicyAPIKeyAssignmentAPI binds the shared read-modify-write to the
// API key routes.
func guardrailPolicyAPIKeyAssignmentAPI(client *sdk.OpenRouter) guardrailPolicyAssignmentAPI {
	return guardrailPolicyAssignmentAPI{
		subjectPath: path.Root("key_hash"),
		read: func(ctx context.Context, a guardrailPolicyAssignment, workspaceID string) (guardrailPolicyAssignmentRead, diag.Diagnostics) {
			var diags diag.Diagnostics

			res, err := client.GuardrailPolicies.GetAPIKeyPolicyAssignment(ctx, operations.GetAPIKeyPolicyAssignmentRequest{
				Hash:        a.SubjectID,
				PolicyType:  operations.GetAPIKeyPolicyAssignmentPolicyType(a.PolicyType),
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
			if res.StatusCode == 400 && guardrailPolicyAssignmentSubjectGone(res.BadRequestResponse) {
				// The subject is gone, so there is no assignment left to read and
				// the resource is absent. This 400 is the only absence signal: a
				// subject holding nothing answers 200 with empty lists, and a 404
				// is deliberately left to the error path below because on these
				// routes it does not mean absence. The write route answers 404 for
				// a policy that is not in the workspace, and every route answers
				// 404 for an organization without the guardrails backend, so a
				// read 404 read as absence would clear state over a route that is
				// merely unavailable while the assignment still exists.
				return guardrailPolicyAssignmentRead{NotFound: true}, diags
			}
			if res.StatusCode != 200 || res.GuardrailPolicyAssignmentResponse == nil {
				message := apiErrorMessage(res.RawResponse, res.BadRequestResponse, res.NotFoundResponse, res.UnauthorizedResponse, res.InternalServerResponse)
				if res.ForbiddenResponse != nil && res.ForbiddenResponse.Error.Message != "" {
					// A key that cannot reach this workspace is answered with 403 and a
					// sentence naming that, which the raw exchange does not render as
					// well as the route's own words.
					message = res.ForbiddenResponse.Error.Message
				}
				diags.AddError(
					fmt.Sprintf("failed to read the %s assignment of API key %q", a.PolicyType, a.SubjectID),
					fmt.Sprintf("API returned status %d: %s", res.StatusCode, message),
				)
				return guardrailPolicyAssignmentRead{}, diags
			}

			state := res.GuardrailPolicyAssignmentResponse.Data
			return guardrailPolicyAssignmentRead{State: &state}, diags
		},
		change: func(ctx context.Context, a guardrailPolicyAssignment, workspaceID string, assigned bool, expected shared.GuardrailPolicyAssignmentState) (bool, string, diag.Diagnostics) {
			var diags diag.Diagnostics

			res, err := client.GuardrailPolicies.ChangeAPIKeyPolicyAssignment(ctx, operations.ChangeAPIKeyPolicyAssignmentRequest{
				Hash: a.SubjectID,
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
			if res.StatusCode == 400 && !assigned && guardrailPolicyAssignmentSubjectGone(res.BadRequestResponse) {
				// The subject went away between the pre-read and this write, so the
				// removal the pre-read confirmed is already complete. The same 400
				// on an assign stays an error below, because nothing was assigned.
				return false, "", diags
			}
			if res.StatusCode == 409 {
				// Both a stale expected_assignment and a removal that was
				// already done arrive here, so the caller re-reads rather than
				// deciding from the status.
				return true, guardrailPolicyAssignmentConflictMessage(res.ConflictResponse), diags
			}
			if res.StatusCode == 404 {
				// Measured: this status is about the policy, not the subject — a
				// policy that is not in this workspace answers 404, while a
				// subject that is gone answers 400. Only a body naming the policy
				// establishes anything, because every route also answers 404 for
				// an organization without the guardrails backend, and reading
				// that as a finished removal would drop an assignment that is
				// still there. Anything else keeps the error path below.
				if guardrailPolicyAssignmentPolicyAbsent(res.NotFoundResponse) {
					if assigned {
						diags.AddAttributeError(
							path.Root("policy_id"),
							"policy not found in this workspace",
							fmt.Sprintf("The API answered 404: policy %q is not in this workspace, so it cannot be assigned. Nothing was assigned.", a.PolicyID),
						)
						return false, "", diags
					}
					// The assignment names a policy that is not in this
					// workspace, so it went with the policy and the removal is
					// complete rather than failed.
					return false, "", diags
				}
			}
			if res.StatusCode != 200 || res.ChangeGuardrailPolicyAssignmentResponse == nil {
				message := apiErrorMessage(res.RawResponse, res.BadRequestResponse, res.NotFoundResponse, res.UnauthorizedResponse, res.InternalServerResponse)
				if res.ForbiddenResponse != nil && res.ForbiddenResponse.Error.Message != "" {
					// A key that cannot reach this workspace is answered with 403 and a
					// sentence naming that, which the raw exchange does not render as
					// well as the route's own words.
					message = res.ForbiddenResponse.Error.Message
				}
				diags.AddError(
					fmt.Sprintf("failed to change the %s assignment of API key %q", a.PolicyType, a.SubjectID),
					fmt.Sprintf("API returned status %d: %s", res.StatusCode, message),
				)
				return false, "", diags
			}

			return false, "", diags
		},
	}
}
