package provider

import (
	"net/http"
	"os"

	"github.com/hashicorp/go-cleanhttp"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	oidcTokenEnvVar          = "OPENROUTER_OIDC_TOKEN"
	oidcTokenFileEnvVar      = "OPENROUTER_OIDC_TOKEN_FILE"
	federationPolicyIDEnvVar = "OPENROUTER_FEDERATION_POLICY_ID"
)

func federationOIDCTokenAttribute() schema.StringAttribute {
	return schema.StringAttribute{
		MarkdownDescription: "OIDC token issued to this workload (for example a GitLab `id_tokens` JWT), exchanged for a short-lived management token through Workload Identity Federation. Use with `federation_policy_id` instead of `api_key`. Configurable via environment variable `" + oidcTokenEnvVar + "`.",
		Optional:            true,
		Sensitive:           true,
	}
}

func federationOIDCTokenFileAttribute() schema.StringAttribute {
	return schema.StringAttribute{
		MarkdownDescription: "Path to a file holding the workload's OIDC token. The file is reread on every exchange, so a rotated token is picked up. Alternative to `oidc_token`. Configurable via environment variable `" + oidcTokenFileEnvVar + "`.",
		Optional:            true,
	}
}

func federationPolicyIDAttribute() schema.StringAttribute {
	return schema.StringAttribute{
		MarkdownDescription: "ID of the organization's Workload Identity Federation policy that acts as a management key. Required with `oidc_token` or `oidc_token_file`. Configurable via environment variable `" + federationPolicyIDEnvVar + "`.",
		Optional:            true,
	}
}

// federatedTokenSourceFromConfig returns nil when no federation setting is
// present, so the provider keeps authenticating with `api_key`.
func federatedTokenSourceFromConfig(data OpenrouterProviderModel, serverURL string, apiKey *string) (*federatedTokenSource, diag.Diagnostics) {
	var diags diag.Diagnostics

	for _, attr := range []struct {
		name  string
		value types.String
	}{
		{"oidc_token", data.OIDCToken},
		{"oidc_token_file", data.OIDCTokenFile},
		{"federation_policy_id", data.FederationPolicyID},
	} {
		if attr.value.IsUnknown() {
			diags.AddAttributeError(
				path.Root(attr.name),
				"Unknown workload identity federation setting",
				"The provider cannot authenticate until "+attr.name+" is known. Set it to a value known at plan time, or leave it unset to use its environment variable.",
			)
		} else if !attr.value.IsNull() && attr.value.ValueString() == "" {
			diags.AddAttributeError(
				path.Root(attr.name),
				"Empty workload identity federation setting",
				attr.name+" is set to an empty string. Set it to a value, or leave it unset to use its environment variable.",
			)
		}
	}
	if diags.HasError() {
		return nil, diags
	}

	oidcToken := stringOrEnv(data.OIDCToken, oidcTokenEnvVar)
	oidcTokenFile := stringOrEnv(data.OIDCTokenFile, oidcTokenFileEnvVar)
	policyID := stringOrEnv(data.FederationPolicyID, federationPolicyIDEnvVar)
	if oidcToken == "" && oidcTokenFile == "" && policyID == "" {
		return nil, diags
	}

	if apiKey != nil && *apiKey != "" {
		diags.AddError(
			"Conflicting OpenRouter credentials",
			"Configure either api_key (or OPENROUTER_MANAGEMENT_KEY) or workload identity federation (oidc_token or oidc_token_file with federation_policy_id), not both.",
		)
		return nil, diags
	}

	registerSensitiveValues(oidcToken)
	source, err := newFederatedTokenSource(federatedTokenSourceConfig{
		serverURL:          serverURL,
		federationPolicyID: policyID,
		oidcToken:          oidcToken,
		oidcTokenFile:      oidcTokenFile,
		httpClient:         newFederationHTTPClient(),
	})
	if err != nil {
		diags.AddError("Invalid workload identity federation configuration", err.Error())
		return nil, diags
	}
	return source, diags
}

func stringOrEnv(value types.String, envVar string) string {
	if !value.IsNull() {
		return value.ValueString()
	}
	return os.Getenv(envVar)
}

// newFederationHTTPClient never follows redirects, so the subject token is only sent to the configured server.
func newFederationHTTPClient() *http.Client {
	return &http.Client{
		Transport: cleanhttp.DefaultPooledTransport(),
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}
