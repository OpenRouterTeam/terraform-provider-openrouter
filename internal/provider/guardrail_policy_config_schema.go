package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
)

// generatedGuardrailPolicyConfigSchemaAttribute returns a generated workspace-scoped guardrail
// policy resource's `config` attribute, so the hand-written account resource for the same policy
// type reuses it instead of declaring a second copy.
//
// The account family is hand-written because the route publishes no get-by-id, so the spec cannot
// express it as a resource, while the workspace-scoped sibling is generated. Both describe the same
// config object, and each account resource used to carry a hand copy of the generated declaration
// (501 lines of it in the moderation case). Taking the attribute from the generated resource keeps
// one declaration, removes the copies, and needs no edit inside generated code, which the
// repository's CONTRIBUTING.md tells contributors to keep to a few lines: pointing the generated
// resource at a shared helper instead would mean deleting hundreds of generated lines as a
// persistent edit, which a regeneration can drop.
//
// A regeneration that changes the entity therefore reaches the account resource's schema too, which
// is the intended direction: the two surfaces describe the same object and should not be able to
// drift.
func generatedGuardrailPolicyConfigSchemaAttribute(ctx context.Context, generated resource.Resource) schema.Attribute {
	var resp resource.SchemaResponse
	generated.Schema(ctx, resource.SchemaRequest{}, &resp)
	return resp.Schema.Attributes["config"]
}
