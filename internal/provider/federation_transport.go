package provider

import (
	"fmt"
	"net/http"
	"net/url"
)

// federationAuthTransport sets a fresh federated access token on every HTTP attempt, so SDK retries refresh an expired token instead of resending it.
// It only authorizes requests to the configured server's origin, so a redirect to another host never receives the token.
type federationAuthTransport struct {
	source *federatedTokenSource
	origin *url.URL
	next   http.RoundTripper
}

func newFederationAuthTransport(source *federatedTokenSource, serverURL string, next http.RoundTripper) (federationAuthTransport, error) {
	origin, err := url.Parse(serverURL)
	if err != nil || origin.Scheme == "" || origin.Host == "" {
		return federationAuthTransport{}, fmt.Errorf("server_url must be an absolute URL")
	}
	return federationAuthTransport{source: source, origin: origin, next: next}, nil
}

func (t federationAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme != t.origin.Scheme || req.URL.Host != t.origin.Host {
		unauthorized := req.Clone(req.Context())
		unauthorized.Header.Del("Authorization")
		return t.next.RoundTrip(unauthorized)
	}
	security, err := t.source.Security(req.Context())
	if err != nil {
		if req.Body != nil {
			_ = req.Body.Close()
		}
		return nil, err
	}
	authorized := req.Clone(req.Context())
	authorized.Header.Set("Authorization", "Bearer "+*security.APIKey)
	return t.next.RoundTrip(authorized)
}
