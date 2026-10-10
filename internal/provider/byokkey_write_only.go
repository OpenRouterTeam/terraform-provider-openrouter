package provider

import (
	"context"

	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/operations"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/shared"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// addByokKeyWriteOnlyAttributes adds the write-only alternative to the
// generated `key` attribute. `key` stays available, now optional, so existing
// configurations keep working; it is persisted in Terraform state.
func addByokKeyWriteOnlyAttributes(s *schema.Schema) {
	key := s.Attributes["key"].(schema.StringAttribute)
	key.Required = false
	key.Optional = true
	key.Description = "The raw provider API key or credential. This value is encrypted at rest and never returned in API responses. " +
		"It is stored in Terraform state; set `key_wo` instead to keep it out of state and plans. One of `key` and `key_wo` is required to create a key."
	s.Attributes["key"] = key

	s.Attributes["key_wo"] = schema.StringAttribute{
		Optional:  true,
		Sensitive: true,
		WriteOnly: true,
		Description: "Write-only alternative to `key`: the raw provider API key or credential, never stored in Terraform state or plans. " +
			"Requires Terraform 1.11 or later and `key_wo_version`. Cannot be combined with `key`. Must be at least 4 characters long, so it can be redacted from logs.",
		Validators: []validator.String{
			// Shorter values are not redacted from logs and error messages.
			stringvalidator.UTF8LengthAtLeast(minSensitiveValueLength),
			stringvalidator.ConflictsWith(path.MatchRoot("key")),
			stringvalidator.AlsoRequires(path.MatchRoot("key_wo_version")),
		},
	}
	s.Attributes["key_wo_version"] = schema.Int64Attribute{
		Optional: true,
		Description: "Rotation trigger for `key_wo`. The credential is sent on create, and again whenever this value changes. " +
			"Increment it to rotate the key; any positive integer works.",
		Validators: []validator.Int64{
			int64validator.AtLeast(1),
			int64validator.AlsoRequires(path.MatchRoot("key_wo")),
		},
	}
}

// applyByokWriteOnlyKeyOnCreate fills the create request's credential from
// `key_wo` when it is set. The generated request builder reads the plan, where
// write-only values are always null.
func applyByokWriteOnlyKeyOnCreate(ctx context.Context, cfg tfsdk.Config, request *shared.CreateBYOKKeyRequest) diag.Diagnostics {
	var diags diag.Diagnostics

	var keyWo types.String
	diags.Append(cfg.GetAttribute(ctx, path.Root("key_wo"), &keyWo)...)
	if diags.HasError() {
		return diags
	}

	switch {
	case !keyWo.IsNull() && !keyWo.IsUnknown():
		request.Key = keyWo.ValueString()
	case request.Key == "":
		diags.AddAttributeError(path.Root("key_wo"), "Missing credential", "One of `key` or `key_wo` must be set to create a BYOK key.")
	}

	return diags
}

// applyByokWriteOnlyKeyOnUpdate sends `key_wo` only when `key_wo_version`
// changed, so metadata-only updates and unchanged versions never resend (or
// need) the credential. A failed update leaves the prior version in state.
func applyByokWriteOnlyKeyOnUpdate(ctx context.Context, req resource.UpdateRequest, request *operations.UpdateBYOKKeyRequest) diag.Diagnostics {
	rotate, diags := writeOnlyVersionChanged(ctx, req.Plan, req.State, path.Root("key_wo_version"))
	if diags.HasError() || !rotate {
		return diags
	}

	var keyWo types.String
	diags.Append(req.Config.GetAttribute(ctx, path.Root("key_wo"), &keyWo)...)
	if diags.HasError() {
		return diags
	}
	if keyWo.IsNull() || keyWo.IsUnknown() {
		diags.AddAttributeError(path.Root("key_wo"), "Missing credential", "`key_wo` must be set when `key_wo_version` changes.")
		return diags
	}

	key := keyWo.ValueString()
	request.Body.Key = &key

	return diags
}
