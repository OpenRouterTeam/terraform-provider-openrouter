package provider

import (
	"context"
	"fmt"
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

const guardrailKeyAssignmentPageSize int64 = 100

var _ resource.Resource = &GuardrailKeyAssignmentResource{}
var _ resource.ResourceWithImportState = &GuardrailKeyAssignmentResource{}

func NewGuardrailKeyAssignmentResource() resource.Resource {
	return &GuardrailKeyAssignmentResource{}
}

// GuardrailKeyAssignmentResource manages the assignment of one guardrail to
// one API key.
type GuardrailKeyAssignmentResource struct {
	client *sdk.OpenRouter
}

type GuardrailKeyAssignmentResourceModel struct {
	GuardrailID types.String `tfsdk:"guardrail_id"`
	ID          types.String `tfsdk:"id"`
	KeyHash     types.String `tfsdk:"key_hash"`
}

func (r *GuardrailKeyAssignmentResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_guardrail_key_assignment"
}

func (r *GuardrailKeyAssignmentResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	identity := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Assigns one guardrail to one API key.\n\n" +
			"This resource manages an API key assignment only. It does not manage member assignments (see `openrouter_guardrail_member_assignment`) or the workspace default guardrail, " +
			"which applies to every key without an assignment and cannot be assigned directly. " +
			"For a guardrail that belongs to a workspace, the key must belong to the same workspace; the API rejects the assignment otherwise.\n\n" +
			"A key holds at most one guardrail. Assigning a key that already has a different guardrail moves the key to this guardrail, " +
			"so declare at most one `openrouter_guardrail_key_assignment` per key. " +
			"Changing any attribute replaces the resource. If the assignment is removed or the key is moved to another guardrail outside Terraform, the next plan recreates it. " +
			"If the guardrail or the key is deleted, the assignment is removed from state.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
				Description: "Import identifier of the assignment, `<guardrail_id>/<key_hash>`.",
			},
			"guardrail_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: identity,
				Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
				Description:   "ID of the guardrail to assign. Must not be a workspace default guardrail. Requires replacement if changed.",
			},
			"key_hash": schema.StringAttribute{
				Required:      true,
				PlanModifiers: identity,
				Validators:    []validator.String{stringvalidator.LengthAtLeast(1), notRawAPIKey{}},
				Description:   "Hash of the API key to assign the guardrail to, for example `openrouter_api_key.example.hash`. Not the secret key itself. Requires replacement if changed.",
			},
		},
	}
}

func (r *GuardrailKeyAssignmentResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *GuardrailKeyAssignmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data GuardrailKeyAssignmentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	guardrailID := data.GuardrailID.ValueString()
	keyHash := data.KeyHash.ValueString()

	// The API itself rejects a default guardrail and a key outside the
	// guardrail's workspace, so no pre-checks are needed here.
	res, err := r.client.Guardrails.BulkAssignKeys(ctx, operations.BulkAssignKeysToGuardrailRequest{
		ID:   guardrailID,
		Body: shared.BulkAssignKeysRequest{KeyHashes: []string{keyHash}},
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
		resp.Diagnostics.AddAttributeError(path.Root("guardrail_id"), "guardrail not found", fmt.Sprintf("Guardrail %q does not exist.", guardrailID))
		return
	}
	if res.StatusCode != 200 || res.BulkAssignKeysResponse == nil {
		message := apiErrorMessage(res.RawResponse, res.BadRequestResponse, res.NotFoundResponse, res.UnauthorizedResponse, res.InternalServerResponse)
		if res.ForbiddenResponse != nil && res.ForbiddenResponse.Error.Message != "" {
			message = res.ForbiddenResponse.Error.Message
		}
		resp.Diagnostics.AddError(
			fmt.Sprintf("failed to assign guardrail %q to API key %q", guardrailID, keyHash),
			fmt.Sprintf("API returned status %d: %s", res.StatusCode, message),
		)
		return
	}
	// The API skips hashes that match no key and still returns 200.
	if res.BulkAssignKeysResponse.AssignedCount != 1 {
		resp.Diagnostics.AddAttributeError(path.Root("key_hash"), "API key not found",
			fmt.Sprintf("No API key with hash %q exists in this organization, so guardrail %q was not assigned.", keyHash, guardrailID))
		return
	}

	data.ID = types.StringValue(guardrailKeyAssignmentID(guardrailID, keyHash))
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *GuardrailKeyAssignmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data GuardrailKeyAssignmentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	guardrailID := data.GuardrailID.ValueString()
	keyHash := data.KeyHash.ValueString()

	found, diags := r.keyAssigned(ctx, guardrailID, keyHash)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	data.ID = types.StringValue(guardrailKeyAssignmentID(guardrailID, keyHash))
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update is only reachable without attribute changes because every
// configurable attribute requires replacement.
func (r *GuardrailKeyAssignmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data GuardrailKeyAssignmentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *GuardrailKeyAssignmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data GuardrailKeyAssignmentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	guardrailID := data.GuardrailID.ValueString()
	keyHash := data.KeyHash.ValueString()

	// The API removes the key only from this guardrail, so a key that was moved
	// to another guardrail keeps that assignment. A key that is gone or no
	// longer assigned here returns 200 with a count of zero.
	res, err := r.client.Guardrails.BulkUnassignKeysFromGuardrail(ctx, operations.BulkUnassignKeysFromGuardrailRequest{
		ID:   guardrailID,
		Body: shared.BulkUnassignKeysRequest{KeyHashes: []string{keyHash}},
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
	if res.StatusCode != 200 || res.BulkUnassignKeysResponse == nil {
		resp.Diagnostics.AddError(
			fmt.Sprintf("failed to unassign guardrail %q from API key %q", guardrailID, keyHash),
			fmt.Sprintf("API returned status %d: %s", res.StatusCode, apiErrorMessage(res.RawResponse, res.BadRequestResponse, res.NotFoundResponse, res.UnauthorizedResponse, res.InternalServerResponse)),
		)
	}
}

func (r *GuardrailKeyAssignmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	guardrailID, keyHash, err := parseGuardrailKeyAssignmentID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("invalid import ID", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("guardrail_id"), guardrailID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("key_hash"), keyHash)...)
}

// keyAssigned pages through the guardrail's key assignments. A guardrail that
// does not exist reports no assignment.
func (r *GuardrailKeyAssignmentResource) keyAssigned(ctx context.Context, guardrailID, keyHash string) (bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	limit := guardrailKeyAssignmentPageSize
	for offset := int64(0); ; {
		pageOffset := offset
		res, err := r.client.Guardrails.ListKeyAssignments(ctx, operations.ListGuardrailKeyAssignmentsRequest{
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
		if res.StatusCode != 200 || res.ListKeyAssignmentsResponse == nil {
			diags.AddError(fmt.Sprintf("failed to list key assignments of guardrail %q", guardrailID), fmt.Sprintf("API returned status %d: %s", res.StatusCode, apiErrorMessage(res.RawResponse, nil, res.NotFoundResponse, res.UnauthorizedResponse, res.InternalServerResponse)))
			return false, diags
		}

		for _, assignment := range res.ListKeyAssignmentsResponse.Data {
			if assignment.KeyHash == keyHash && assignment.GuardrailID == guardrailID {
				return true, diags
			}
		}
		// The API drops rows whose key no longer exists after paging, so a page
		// can be shorter than limit before the end. Step by the page size,
		// which is what the server consumed, and stop at total_count.
		offset += limit
		if offset >= res.ListKeyAssignmentsResponse.TotalCount {
			return false, diags
		}
	}
}

func guardrailKeyAssignmentID(guardrailID, keyHash string) string {
	return guardrailID + "/" + keyHash
}

// parseGuardrailKeyAssignmentID splits on the last "/", since a key hash never
// contains one.
func parseGuardrailKeyAssignmentID(id string) (guardrailID, keyHash string, err error) {
	i := strings.LastIndex(id, "/")
	if i <= 0 || i == len(id)-1 {
		return "", "", fmt.Errorf("expected import ID in the format <guardrail_id>/<key_hash>, got %q", id)
	}
	return id[:i], id[i+1:], nil
}

// notRawAPIKey rejects a secret API key given where its hash belongs, so the
// secret never reaches the plan, the state, or the API request.
type notRawAPIKey struct{}

func (v notRawAPIKey) Description(ctx context.Context) string {
	return "value must be an API key hash, not the secret key"
}

func (v notRawAPIKey) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v notRawAPIKey) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if strings.HasPrefix(req.ConfigValue.ValueString(), "sk-or-") {
		resp.Diagnostics.AddAttributeError(req.Path, "secret API key given instead of its hash",
			"key_hash must be the API key's hash (for example openrouter_api_key.example.hash), not the secret sk-or-... key.")
	}
}
