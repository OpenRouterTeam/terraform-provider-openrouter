package provider

import "github.com/hashicorp/terraform-plugin-framework/types"

// reconcileModelIDs keeps the configured model identifiers when the API
// returns the same number of models. The API accepts a slug or a
// canonical_slug but always responds with canonical_slugs, so copying the
// response verbatim into state causes a permanent diff (issue #347).
func reconcileModelIDs(prior []types.String, resp []string) []types.String {
	if resp == nil {
		return nil
	}
	if prior != nil && len(prior) == len(resp) {
		return prior
	}
	out := make([]types.String, 0, len(resp))
	for _, v := range resp {
		out = append(out, types.StringValue(v))
	}
	return out
}
