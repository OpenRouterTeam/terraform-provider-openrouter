package provider

import (
	"net/http"
)

// federationAuthTransport sets a fresh federated access token on every HTTP attempt, so SDK retries refresh an expired token instead of resending it.
type federationAuthTransport struct {
	source *federatedTokenSource
	next   http.RoundTripper
}

func (t federationAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
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
