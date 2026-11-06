package provider

import (
	"context"

	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/explicitnull"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var byokNullableRestrictionFields = [...]string{"allowed_models", "allowed_api_key_hashes"}

func configureByokNullableRestrictions(s *schema.Schema) {
	for _, name := range byokNullableRestrictionFields {
		attribute := s.Attributes[name].(schema.ListAttribute)
		attribute.Computed = false
		attribute.Description += " This restriction is authoritative: omitting it or setting it to null removes any existing restriction, including on imported credentials."
		s.Attributes[name] = attribute
	}
}

func withByokNullableRestrictions(ctx context.Context, plan types.Object) context.Context {
	attributes := plan.Attributes()
	var fields []string
	for _, name := range byokNullableRestrictionFields {
		if attributes[name].IsNull() {
			fields = append(fields, name)
		}
	}
	return explicitnull.WithFields(ctx, fields...)
}
