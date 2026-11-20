package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/operations"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/shared"
)

// The account (workspace-wide) policy family has three resources whose five lifecycle
// operations are the same operation against the same route with a different body and a
// different row in the union. This file carries that one implementation.
//
// `guardrailpolicyaccount.go` carries the route contract they all read; the three
// `guardrailpolicyaccount*_resource.go` files carry only what differs: the schema's
// description and config attribute, the generated types, and the four mappings in the
// descriptor below. A rule stated here is stated once, which matters because every round
// of review on this family so far has found a rule applied to one member of three.

// guardrailPolicyAccountOps is the per-family half of the lifecycle.
//
// The descriptor is parameterized on the family's model type, so a family that wires a
// mapping to the wrong type fails to compile. Every closure here names a generated type
// that only the family's own file can see.
type guardrailPolicyAccountOps[M any] struct {
	// policyType is the value the routes resolve a policy id by. A row found under another
	// type is a mismatched import rather than this resource's policy, and
	// guardrailPolicyAccountLookup reports it.
	policyType string

	// id reads the policy id state holds, which is the identifier every operation passes
	// to the route.
	id func(*M) string

	// setID writes the id a write response carried onto the model.
	setID func(*M, string)

	// isDefault reads the flag the destroy refusal and the write body are both conditional
	// on.
	isDefault func(*M) bool

	// refreshFromRow hydrates the model from the union row the list returns. A nil model
	// comes back as an error naming the mismatched type rather than as a panic.
	refreshFromRow func(ctx context.Context, model *M, policy shared.AccountGuardrailPolicy) diag.Diagnostics

	// writeBody renders the body both write routes take.
	writeBody func(ctx context.Context, model *M) (shared.AccountGuardrailPolicyRequest, diag.Diagnostics)
}

// guardrailPolicyAccountSchema builds the schema all three resources publish. Only the
// description and the `config` attribute differ per family, and the attribute is built from
// each family's own generated workspace-scoped resource, so it is passed in.
func guardrailPolicyAccountSchema(configAttribute schema.Attribute, markdownDescription string) schema.Schema {
	return schema.Schema{
		MarkdownDescription: markdownDescription,
		Attributes: map[string]schema.Attribute{
			"config": configAttribute,
			"id": schema.StringAttribute{
				Computed:    true,
				Description: `Policy ID`,
			},
			"is_default": schema.BoolAttribute{
				Computed:    true,
				Optional:    true,
				Description: `Cover every workspace in the account except ` + "`" + `opted_out_workspace_ids` + "`" + `. Set it to ` + "`" + `false` + "`" + ` to unset that status. Only one policy of a type can be the account default: declaring it on a second policy of the same type unsets it on the first and clears its coverage, so two policies that both declare it change on every run. Destroying a policy that holds this status is refused: the route deletes the account default with no complaint and cannot restore it, so a destroy fails and names the two ways forward.`,
			},
			"is_synced": schema.BoolAttribute{
				Computed:    true,
				Description: `Kept in sync with a v1 guardrail; saving changes creates an account copy`,
			},
			"is_workspace_default_synced": schema.BoolAttribute{
				Computed:    true,
				Description: `Synced from a v1 workspace default guardrail`,
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: `Policy name`,
				Validators: []validator.String{
					stringvalidator.UTF8LengthBetween(1, 255),
				},
			},
			"opted_out_workspace_ids": schema.ListAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.List{
					guardrailPolicyAccountOptOutsModifier{},
				},
				Optional:    true,
				ElementType: types.StringType,
				Description: `Workspaces left out of a default policy. Only the account default accepts these, and the API answers a policy that is not the default with 400 when it carries them, so flipping ` + "`" + `is_default` + "`" + ` off drops them: these require an effective ` + "`" + `is_default = true` + "`" + `, and a write that carries them while the flag is false or unset is refused.`,
			},
			"updated_at": schema.StringAttribute{
				Computed: true,
			},
			"workspace_ids": schema.ListAttribute{
				Computed:    true,
				Optional:    true,
				ElementType: types.StringType,
				PlanModifiers: []planmodifier.List{
					guardrailPolicyAccountWorkspaceIDsModifier{},
				},
				Description: `Workspaces a policy covers when it is not the default. For the account default this attribute reads back the workspaces currently resolving to it, which is more than what was configured, and the provider does not send it while ` + "`" + `is_default` + "`" + ` is true.`,
			},
		},
	}
}

// guardrailPolicyAccountUnexpectedRow is the diagnostic for a union row that carries
// another family's type. The read already refuses that case by type, so this is a guard
// for the write paths rather than a user-facing branch.
func guardrailPolicyAccountUnexpectedRow() diag.Diagnostics {
	var diags diag.Diagnostics
	diags.AddError("unexpected response from API", "the account policy list returned a policy of a type this resource cannot read")
	return diags
}

func guardrailPolicyAccountCreate[M any](ctx context.Context, client *sdk.OpenRouter, ops guardrailPolicyAccountOps[M], req resource.CreateRequest, resp *resource.CreateResponse) {
	var data *M
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

	body, bodyDiags := ops.writeBody(ctx, data)
	resp.Diagnostics.Append(bodyDiags...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := client.GuardrailPolicies.CreateAccountPolicy(ctx, body)
	if err != nil {
		resp.Diagnostics.AddError("failure to invoke API", redactSensitiveValues(ctx, err.Error()))
		if res != nil && res.RawResponse != nil {
			resp.Diagnostics.AddError("unexpected http request/response", debugResponse(res.RawResponse))
		}
		return
	}
	if res == nil {
		resp.Diagnostics.AddError("unexpected response from API", fmt.Sprintf("%v", res))
		return
	}
	if res.StatusCode == 409 {
		resp.Diagnostics.AddError(
			"Resource Already Exists",
			"When creating this resource, the API indicated that this resource already exists. You can bring the existing resource under management using Terraform import functionality or retry with a unique configuration.",
		)
		return
	}
	if res.StatusCode != 201 {
		resp.Diagnostics.AddError(fmt.Sprintf("unexpected response from API. Got an unexpected response code %v", res.StatusCode),
			guardrailPolicyAccountResponseMessage(res.BadRequestResponse, res.ConflictResponse, res.NotFoundResponse, res.RawResponse))
		return
	}
	if res.AccountGuardrailPolicyResponse == nil {
		resp.Diagnostics.AddError("unexpected response from API. Got an unexpected response body", debugResponse(res.RawResponse))
		return
	}

	// The write response carries the whole row, not only the id. Measured on the live route
	// 2026-11-20: the 201 answer's field set is identical to the same policy's row in the
	// list, and its config came back byte-equal to what was sent. So state comes from the
	// response and no list read follows a write, which removes the window in which a read
	// issued milliseconds after a write could miss the row just created.
	ops.setID(data, guardrailPolicyAccountID(res.AccountGuardrailPolicyResponse.Data))
	if ops.id(data) == "" {
		resp.Diagnostics.AddError("unexpected response from API", "the created policy carried no id")
		return
	}

	resp.Diagnostics.Append(ops.refreshFromRow(ctx, data, res.AccountGuardrailPolicyResponse.Data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(refreshPlan(ctx, plan, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func guardrailPolicyAccountRead[M any](ctx context.Context, client *sdk.OpenRouter, ops guardrailPolicyAccountOps[M], req resource.ReadRequest, resp *resource.ReadResponse) {
	var data *M
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

	policy, found, lookupDiags := guardrailPolicyAccountLookup(ctx, client, ops.policyType, ops.id(data))
	resp.Diagnostics.Append(lookupDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		// An account policy list that returns in full without the id is the only
		// absence signal these routes give: there is no get-by-id to answer 404 for
		// a single policy. A route that is unavailable answers non-200 above and
		// reaches this as an error rather than as absence.
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(ops.refreshFromRow(ctx, data, *policy)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func guardrailPolicyAccountUpdate[M any](ctx context.Context, client *sdk.OpenRouter, ops guardrailPolicyAccountOps[M], req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data *M
	var plan types.Object

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	merge(ctx, req, resp, &data)
	if resp.Diagnostics.HasError() {
		return
	}

	body, bodyDiags := ops.writeBody(ctx, data)
	resp.Diagnostics.Append(bodyDiags...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := client.GuardrailPolicies.UpdateAccountPolicy(ctx, operations.UpdateAccountPolicyRequest{
		ID:   ops.id(data),
		Body: body,
	})
	if err != nil {
		resp.Diagnostics.AddError("failure to invoke API", redactSensitiveValues(ctx, err.Error()))
		if res != nil && res.RawResponse != nil {
			resp.Diagnostics.AddError("unexpected http request/response", debugResponse(res.RawResponse))
		}
		return
	}
	if res == nil {
		resp.Diagnostics.AddError("unexpected response from API", fmt.Sprintf("%v", res))
		return
	}
	if res.StatusCode != 200 {
		resp.Diagnostics.AddError(fmt.Sprintf("unexpected response from API. Got an unexpected response code %v", res.StatusCode),
			guardrailPolicyAccountResponseMessage(res.BadRequestResponse, res.ConflictResponse, res.NotFoundResponse, res.RawResponse))
		return
	}
	if res.AccountGuardrailPolicyResponse == nil {
		resp.Diagnostics.AddError("unexpected response from API. Got an unexpected response body", debugResponse(res.RawResponse))
		return
	}

	// See Create: the write response carries the whole row, so an update takes its state
	// from it rather than re-reading the list. Measured on the live route 2026-11-20, the
	// PUT's 200 carries the same field set the list does, and its updated_at is the
	// post-update value that the list would return. The id is already the one state holds.
	resp.Diagnostics.Append(ops.refreshFromRow(ctx, data, res.AccountGuardrailPolicyResponse.Data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(refreshPlan(ctx, plan, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func guardrailPolicyAccountDelete[M any](ctx context.Context, client *sdk.OpenRouter, ops guardrailPolicyAccountOps[M], req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data *M
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

	// The destroy of the account default is refused, not sent and not silently skipped.
	// Measured 2026-11-18: DELETE answers 200 for a policy that holds the default flag,
	// with no refusal, and there is no undelete, so sending it would release the baseline
	// for every workspace that resolves to the policy with no way back. Skipping it is
	// the other failure: a destroy that reports success while the policy still enforces
	// hands back a resource nobody manages, and writing the configuration again creates a
	// second policy rather than adopting that one. An error keeps the resource in state
	// and lets the operator choose, and the two paths are named in the message.
	if ops.isDefault(data) {
		resp.Diagnostics.AddAttributeError(
			path.Root("is_default"),
			"refusing to destroy the account default",
			"This policy is the account default for its type. The API deletes it with no complaint and cannot restore it, and every workspace that resolves to it would lose this policy, so the provider refuses. To remove it: set `is_default = false`, apply, then destroy. To leave it enforcing and stop managing it here: `terraform state rm` on this resource address.",
		)
		return
	}

	res, err := client.GuardrailPolicies.DeleteAccountPolicy(ctx, operations.DeleteAccountPolicyRequest{
		ID: ops.id(data),
	})
	if err != nil {
		resp.Diagnostics.AddError("failure to invoke API", redactSensitiveValues(ctx, err.Error()))
		if res != nil && res.RawResponse != nil {
			resp.Diagnostics.AddError("unexpected http request/response", debugResponse(res.RawResponse))
		}
		return
	}
	if res == nil {
		resp.Diagnostics.AddError("unexpected response from API", fmt.Sprintf("%v", res))
		return
	}
	// A 404 is success, the way it is in the three generated workspace-scoped siblings (the
	// same `case 200, 404` appears in each): a delete of a policy that is already gone is
	// satisfied, and re-running the destroy after an out-of-band removal cannot fail. An
	// absence becomes state removal in the read, which is where it is reported.
	switch res.StatusCode {
	case 200, 404:
		break
	default:
		resp.Diagnostics.AddError(fmt.Sprintf("unexpected response from API. Got an unexpected response code %v", res.StatusCode),
			guardrailPolicyAccountResponseMessage(res.BadRequestResponse, res.ConflictResponse, res.NotFoundResponse, res.RawResponse))
		return
	}
}

// guardrailPolicyAccountImportState takes the bare policy id. The list read is
// account-wide, so unlike the workspace-scoped policy resources this family needs no
// workspace prefix, and the resource's own type supplies the `policy_type` the route
// resolves the id by.
func guardrailPolicyAccountImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID == "" {
		resp.Diagnostics.AddError("invalid import ID", "expected a policy id, got an empty string")
		return
	}
	if strings.Contains(req.ID, "/") {
		resp.Diagnostics.AddError(
			"invalid import ID",
			fmt.Sprintf("expected a bare policy id, got %q. An account policy is not scoped to a workspace, so this resource's import identifier carries no workspace prefix.", req.ID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
