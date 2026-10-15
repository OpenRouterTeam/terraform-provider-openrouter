package provider

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestFederationAuthTransportRefreshesTokenBetweenRetryAttempts(t *testing.T) {
	exchange := newExchangeServer(t, func(w http.ResponseWriter, _ *http.Request, call int32) {
		grantToken(w, "access-"+string(rune('0'+call)), 120)
	})
	source := newTestSource(t, exchange.URL, federatedTokenSourceConfig{oidcToken: "subject-jwt"})
	now := time.Unix(1_700_000_000, 0)
	source.now = func() time.Time { return now }

	var seen []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Values("Authorization")...)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(api.Close)

	transport, err := newFederationAuthTransport(source, api.URL+"/api/v1", http.DefaultTransport)
	if err != nil {
		t.Fatalf("newFederationAuthTransport: %v", err)
	}
	client := &http.Client{Transport: transport}
	req, err := http.NewRequest(http.MethodGet, api.URL+"/api/v1/workspaces", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer stale")
	for range 2 {
		res, err := client.Do(req)
		if err != nil {
			t.Fatalf("Do: %v", err)
		}
		_ = res.Body.Close()
		now = now.Add(5 * time.Minute)
	}

	want := []string{"Bearer access-1", "Bearer access-2"}
	if len(seen) != len(want) || seen[0] != want[0] || seen[1] != want[1] {
		t.Fatalf("Authorization headers = %q, want %q", seen, want)
	}
}

func TestFederationAuthTransportDoesNotAuthorizeRedirectsToAnotherHost(t *testing.T) {
	exchange := newExchangeServer(t, func(w http.ResponseWriter, _ *http.Request, _ int32) {
		grantToken(w, "access-1", 900)
	})
	source := newTestSource(t, exchange.URL, federatedTokenSourceConfig{oidcToken: "subject-jwt"})

	var redirectedAuthorization []string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectedAuthorization = r.Header.Values("Authorization")
	}))
	t.Cleanup(target.Close)
	var originAuthorization string
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originAuthorization = r.Header.Get("Authorization")
		http.Redirect(w, r, target.URL+"/elsewhere", http.StatusFound)
	}))
	t.Cleanup(origin.Close)

	transport, err := newFederationAuthTransport(source, origin.URL+"/api/v1", http.DefaultTransport)
	if err != nil {
		t.Fatalf("newFederationAuthTransport: %v", err)
	}
	res, err := (&http.Client{Transport: transport}).Get(origin.URL + "/api/v1/workspaces")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	_ = res.Body.Close()

	if originAuthorization != "Bearer access-1" {
		t.Fatalf("origin Authorization = %q, want the federated token", originAuthorization)
	}
	if len(redirectedAuthorization) != 0 {
		t.Fatalf("redirect target received Authorization %q", redirectedAuthorization)
	}
}

func TestOriginOfTreatsDefaultPortsAsTheSameOrigin(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		same bool
	}{
		{"https://api.example.com:443/api/v1", "https://API.example.com/api/v1/workspaces", true},
		{"http://localhost:80/api/v1", "http://localhost/api/v1", true},
		{"https://api.example.com/api/v1", "https://api.example.com:8443/api/v1", false},
		{"https://api.example.com/api/v1", "http://api.example.com/api/v1", false},
		{"https://api.example.com/api/v1", "https://evil.example.com/api/v1", false},
	} {
		a, _ := url.Parse(tc.a)
		b, _ := url.Parse(tc.b)
		if got := originOf(a) == originOf(b); got != tc.same {
			t.Errorf("same origin(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.same)
		}
	}
}

func TestFederationConfigRejectsUnknownValuesInsteadOfUsingEnvironment(t *testing.T) {
	t.Setenv(oidcTokenEnvVar, "env-subject-jwt")
	t.Setenv(federationPolicyIDEnvVar, "env-policy")

	for name, data := range map[string]OpenrouterProviderModel{
		"federation_policy_id": {FederationPolicyID: types.StringUnknown(), OIDCToken: types.StringValue("subject-jwt"), OIDCTokenFile: types.StringNull()},
		"oidc_token":           {FederationPolicyID: types.StringValue("policy-123"), OIDCToken: types.StringUnknown(), OIDCTokenFile: types.StringNull()},
		"oidc_token_file":      {FederationPolicyID: types.StringValue("policy-123"), OIDCToken: types.StringNull(), OIDCTokenFile: types.StringUnknown()},
	} {
		t.Run(name, func(t *testing.T) {
			source, diags := federatedTokenSourceFromConfig(data, "https://openrouter.ai/api/v1", nil)
			if source != nil || !diags.HasError() {
				t.Fatalf("expected an unknown-value diagnostic and no token source, got source=%v diags=%v", source != nil, diags)
			}
		})
	}
}
