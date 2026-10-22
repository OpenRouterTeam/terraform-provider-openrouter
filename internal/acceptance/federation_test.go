package acceptance

// Workload identity federation through the whole provider: Terraform configures
// the provider, Configure builds the generated SDK client, and a data source
// read must reach the Management API with the exchanged access token, never the
// subject token or an API key. Runs offline against an httptest stub.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const (
	federationStubPolicyID     = "policy-stub"
	federationStubSubjectToken = "subject-jwt-stub"
	federationStubAccessToken  = "or-federated-access-stub"
)

type federationStub struct {
	mu        sync.Mutex
	exchanges int
	apiAuth   []string
	failures  []string
}

func (s *federationStub) fail(format string, args ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failures = append(s.failures, fmt.Sprintf(format, args...))
}

func newFederationStub(t *testing.T) (*httptest.Server, *federationStub) {
	t.Helper()
	stub := &federationStub{}
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			stub.fail("parse token exchange form: %v", err)
		}
		for field, want := range map[string]string{
			"grant_type":           "urn:ietf:params:oauth:grant-type:token-exchange",
			"subject_token":        federationStubSubjectToken,
			"subject_token_type":   "urn:ietf:params:oauth:token-type:jwt",
			"federation_policy_id": federationStubPolicyID,
			"scope":                "management",
		} {
			if got := r.PostForm.Get(field); got != want {
				stub.fail("token exchange %s = %q, want %q", field, got, want)
			}
		}
		if auth := r.Header.Get("Authorization"); auth != "" {
			stub.fail("token exchange carried Authorization %q, want none", auth)
		}
		stub.mu.Lock()
		stub.exchanges++
		stub.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":      federationStubAccessToken,
			"issued_token_type": "urn:ietf:params:oauth:token-type:access_token",
			"token_type":        "Bearer",
			"expires_in":        900,
			"scope":             "management",
		})
	})
	mux.HandleFunc("/credits", func(w http.ResponseWriter, r *http.Request) {
		stub.mu.Lock()
		stub.apiAuth = append(stub.apiAuth, r.Header.Get("Authorization"))
		stub.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"total_credits": 12.5, "total_usage": 2.5}})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		stub.fail("unexpected request %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, stub
}

// check verifies that the token was exchanged and every Management API
// request carried exactly the exchanged bearer token.
func (s *federationStub) check(*terraform.State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.failures) > 0 {
		return fmt.Errorf("stub saw invalid requests: %v", s.failures)
	}
	if s.exchanges == 0 {
		return fmt.Errorf("no token exchange happened")
	}
	if len(s.apiAuth) == 0 {
		return fmt.Errorf("no Management API request reached the stub")
	}
	for _, auth := range s.apiAuth {
		if auth != "Bearer "+federationStubAccessToken {
			return fmt.Errorf("Management API request carried Authorization %q, want the exchanged token", auth)
		}
	}
	return nil
}

func federationCreditsConfig(providerBody string) string {
	return fmt.Sprintf(`
provider "openrouter" {
%s
}

data "openrouter_credits" "test" {}
`, providerBody)
}

// isolateFederationEnv clears the credential environment variables the
// provider reads, so only the test's HCL (and managementKey) configures it.
func isolateFederationEnv(t *testing.T, managementKey string) {
	t.Helper()
	t.Setenv("OPENROUTER_MANAGEMENT_KEY", managementKey)
	t.Setenv("OPENROUTER_OIDC_TOKEN", "")
	t.Setenv("OPENROUTER_OIDC_TOKEN_FILE", "")
	t.Setenv("OPENROUTER_FEDERATION_POLICY_ID", "")
}

// UnitTest intentionally bypasses TF_ACC: every request goes to a local fixture.
func TestStubFederationAuthenticatesManagementRequests(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "oidc-token")
	if err := os.WriteFile(tokenFile, []byte(federationStubSubjectToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	for name, credential := range map[string]string{
		"oidc_token":      fmt.Sprintf("  oidc_token = %q", federationStubSubjectToken),
		"oidc_token_file": fmt.Sprintf("  oidc_token_file = %q", tokenFile),
	} {
		t.Run(name, func(t *testing.T) {
			isolateFederationEnv(t, "")
			srv, stub := newFederationStub(t)
			config := federationCreditsConfig(fmt.Sprintf("  server_url = %q\n  federation_policy_id = %q\n%s", srv.URL, federationStubPolicyID, credential))
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: protoV6ProviderFactories(),
				Steps: []resource.TestStep{{
					Config: config,
					Check: resource.ComposeAggregateTestCheckFunc(
						resource.TestCheckResourceAttr("data.openrouter_credits.test", "data.total_credits", "12.5"),
						stub.check,
					),
				}},
			})
		})
	}
}

func TestStubFederationRejectsAPIKeyAlongsideFederation(t *testing.T) {
	for name, tc := range map[string]struct {
		apiKeyEnv string
		apiKeyHCL string
	}{
		"api_key attribute":                 {apiKeyHCL: `  api_key = "sk-or-v1-static"`},
		"OPENROUTER_MANAGEMENT_KEY env var": {apiKeyEnv: "sk-or-v1-static"},
	} {
		t.Run(name, func(t *testing.T) {
			isolateFederationEnv(t, tc.apiKeyEnv)
			srv, stub := newFederationStub(t)
			config := federationCreditsConfig(fmt.Sprintf("  server_url = %q\n  federation_policy_id = %q\n  oidc_token = %q\n%s", srv.URL, federationStubPolicyID, federationStubSubjectToken, tc.apiKeyHCL))
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: protoV6ProviderFactories(),
				Steps: []resource.TestStep{{
					Config:      config,
					ExpectError: regexp.MustCompile(`Conflicting OpenRouter credentials`),
				}},
			})
			stub.mu.Lock()
			defer stub.mu.Unlock()
			if stub.exchanges != 0 || len(stub.apiAuth) != 0 {
				t.Fatalf("provider contacted the API despite conflicting credentials: %d exchanges, %d API requests", stub.exchanges, len(stub.apiAuth))
			}
		})
	}
}
