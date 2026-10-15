package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type exchangeServer struct {
	*httptest.Server
	calls    atomic.Int32
	subjects chan string
}

func newExchangeServer(t *testing.T, handle func(w http.ResponseWriter, r *http.Request, call int32)) *exchangeServer {
	t.Helper()
	s := &exchangeServer{subjects: make(chan string, 10)}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := s.calls.Add(1)
		if r.URL.Path != "/api/v1/oauth/token" || r.Method != http.MethodPost {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		s.subjects <- r.PostForm.Get("subject_token")
		handle(w, r, call)
	}))
	t.Cleanup(s.Close)
	return s
}

func grantToken(w http.ResponseWriter, token string, expiresIn int64) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token":      token,
		"issued_token_type": "urn:ietf:params:oauth:token-type:access_token",
		"token_type":        "Bearer",
		"expires_in":        expiresIn,
		"scope":             "management",
	})
}

func newTestSource(t *testing.T, serverURL string, cfg federatedTokenSourceConfig) *federatedTokenSource {
	t.Helper()
	cfg.serverURL = serverURL + "/api/v1"
	cfg.httpClient = http.DefaultClient
	if cfg.federationPolicyID == "" {
		cfg.federationPolicyID = "policy-123"
	}
	source, err := newFederatedTokenSource(cfg)
	if err != nil {
		t.Fatalf("newFederatedTokenSource: %v", err)
	}
	return source
}

func TestFederatedTokenSourceSendsTokenExchangeRequest(t *testing.T) {
	server := newExchangeServer(t, func(w http.ResponseWriter, r *http.Request, _ int32) {
		want := map[string]string{
			"grant_type":           tokenExchangeGrantType,
			"subject_token_type":   tokenExchangeSubjectType,
			"federation_policy_id": "policy-123",
			"scope":                "management",
		}
		for field, value := range want {
			if got := r.PostForm.Get(field); got != value {
				t.Errorf("%s = %q, want %q", field, got, value)
			}
		}
		grantToken(w, "access-1", 900)
	})
	source := newTestSource(t, server.URL, federatedTokenSourceConfig{oidcToken: "subject-jwt"})

	security, err := source.Security(context.Background())
	if err != nil {
		t.Fatalf("Security: %v", err)
	}
	if security.APIKey == nil || *security.APIKey != "access-1" {
		t.Fatalf("APIKey = %v, want access-1", security.APIKey)
	}
	if got := <-server.subjects; got != "subject-jwt" {
		t.Fatalf("subject_token = %q, want subject-jwt", got)
	}
}

func TestFederatedTokenSourceReusesTokenUntilNearExpiry(t *testing.T) {
	server := newExchangeServer(t, func(w http.ResponseWriter, _ *http.Request, call int32) {
		grantToken(w, "access-"+string(rune('0'+call)), 900)
	})
	source := newTestSource(t, server.URL, federatedTokenSourceConfig{oidcToken: "subject-jwt"})
	now := time.Unix(1_700_000_000, 0)
	source.now = func() time.Time { return now }

	first, err := source.Security(context.Background())
	if err != nil {
		t.Fatalf("first Security: %v", err)
	}
	now = now.Add(10 * time.Minute)
	cached, err := source.Security(context.Background())
	if err != nil {
		t.Fatalf("cached Security: %v", err)
	}
	if *cached.APIKey != *first.APIKey || server.calls.Load() != 1 {
		t.Fatalf("expected the cached token, got %q after %d exchanges", *cached.APIKey, server.calls.Load())
	}

	now = now.Add(4*time.Minute + 30*time.Second)
	refreshed, err := source.Security(context.Background())
	if err != nil {
		t.Fatalf("refreshed Security: %v", err)
	}
	if *refreshed.APIKey != "access-2" || server.calls.Load() != 2 {
		t.Fatalf("expected a re-exchange inside the refresh skew, got %q after %d exchanges", *refreshed.APIKey, server.calls.Load())
	}
}

func TestFederatedTokenSourceRereadsTokenFile(t *testing.T) {
	server := newExchangeServer(t, func(w http.ResponseWriter, _ *http.Request, _ int32) {
		grantToken(w, "access", 1)
	})
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("first-jwt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := newTestSource(t, server.URL, federatedTokenSourceConfig{oidcTokenFile: path})

	if _, err := source.Security(context.Background()); err != nil {
		t.Fatalf("first Security: %v", err)
	}
	if err := os.WriteFile(path, []byte("rotated-jwt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Security(context.Background()); err != nil {
		t.Fatalf("second Security: %v", err)
	}
	if first, second := <-server.subjects, <-server.subjects; first != "first-jwt" || second != "rotated-jwt" {
		t.Fatalf("subject tokens = %q, %q; want first-jwt, rotated-jwt", first, second)
	}
}

func TestFederatedTokenSourceReportsOAuthError(t *testing.T) {
	server := newExchangeServer(t, func(w http.ResponseWriter, _ *http.Request, _ int32) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_scope","error_description":"This federation policy grants only the inference scope."}`))
	})
	source := newTestSource(t, server.URL, federatedTokenSourceConfig{oidcToken: "subject-jwt"})

	_, err := source.Security(context.Background())
	if err == nil || !strings.Contains(err.Error(), "invalid_scope: This federation policy grants only the inference scope.") {
		t.Fatalf("err = %v, want the OAuth error and description", err)
	}
}

func TestFederatedTokenSourceRejectsNonManagementGrant(t *testing.T) {
	server := newExchangeServer(t, func(w http.ResponseWriter, _ *http.Request, _ int32) {
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "a", "expires_in": 900, "scope": "inference"})
	})
	source := newTestSource(t, server.URL, federatedTokenSourceConfig{oidcToken: "subject-jwt"})

	if _, err := source.Security(context.Background()); err == nil || !strings.Contains(err.Error(), `granted scope "inference"`) {
		t.Fatalf("err = %v, want a scope mismatch", err)
	}
}

func TestNewFederatedTokenSourceValidatesConfig(t *testing.T) {
	cases := map[string]federatedTokenSourceConfig{
		"missing policy":      {oidcToken: "jwt"},
		"no subject token":    {federationPolicyID: "p"},
		"both subject tokens": {federationPolicyID: "p", oidcToken: "jwt", oidcTokenFile: "/tmp/jwt"},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := newFederatedTokenSource(cfg); err == nil {
				t.Fatal("expected a configuration error")
			}
		})
	}
}

func TestFederationHTTPClientDoesNotFollowRedirects(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirected.Add(1)
	}))
	t.Cleanup(target.Close)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/api/v1/oauth/token", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(origin.Close)
	source, err := newFederatedTokenSource(federatedTokenSourceConfig{
		serverURL:          origin.URL + "/api/v1",
		federationPolicyID: "policy-123",
		oidcToken:          "subject-jwt",
		httpClient:         newFederationHTTPClient(),
	})
	if err != nil {
		t.Fatalf("newFederatedTokenSource: %v", err)
	}

	if _, err := source.Security(context.Background()); err == nil {
		t.Fatal("expected the redirect response to fail the exchange")
	}
	if redirected.Load() != 0 {
		t.Fatal("subject token was resent to the redirect target")
	}
}
