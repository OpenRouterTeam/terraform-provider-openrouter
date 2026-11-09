package types

import "github.com/hashicorp/terraform-plugin-framework/types"

type PrivateEndpointSettings struct {
	ContextLength types.Int64 `tfsdk:"context_length"`
}
