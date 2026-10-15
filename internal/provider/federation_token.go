package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/shared"
)

const (
	tokenExchangeGrantType   = "urn:ietf:params:oauth:grant-type:token-exchange"
	tokenExchangeSubjectType = "urn:ietf:params:oauth:token-type:jwt"
	federatedManagementScope = "management"

	// Re-exchange this long before expiry so a request never carries a token
	// that lapses in flight.
	federatedTokenRefreshSkew = time.Minute

	maxTokenExchangeResponseBytes = 1 << 20
)

// federatedTokenSource exchanges a workload's OIDC token for a short-lived
// management-scoped OpenRouter access token (Workload Identity Federation) and
// re-exchanges it shortly before it expires.
type federatedTokenSource struct {
	tokenURL           string
	federationPolicyID string
	subjectToken       func() (string, error)
	httpClient         *http.Client
	now                func() time.Time

	mu          sync.Mutex
	accessToken string
	expiresAt   time.Time
}

type federatedTokenSourceConfig struct {
	serverURL          string
	federationPolicyID string
	oidcToken          string
	oidcTokenFile      string
	httpClient         *http.Client
}

type tokenExchangeResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
	Scope       string `json:"scope"`
}

type tokenExchangeError struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

func newFederatedTokenSource(cfg federatedTokenSourceConfig) (*federatedTokenSource, error) {
	if cfg.federationPolicyID == "" {
		return nil, errors.New("federation_policy_id is required for workload identity federation")
	}
	if (cfg.oidcToken == "") == (cfg.oidcTokenFile == "") {
		return nil, errors.New("set exactly one of oidc_token or oidc_token_file")
	}

	subjectToken := func() (string, error) { return cfg.oidcToken, nil }
	if cfg.oidcTokenFile != "" {
		path := cfg.oidcTokenFile
		subjectToken = func() (string, error) { return readSubjectTokenFile(path) }
	}

	return &federatedTokenSource{
		tokenURL:           strings.TrimRight(cfg.serverURL, "/") + "/oauth/token",
		federationPolicyID: cfg.federationPolicyID,
		subjectToken:       subjectToken,
		httpClient:         cfg.httpClient,
		now:                time.Now,
	}, nil
}

// Security returns the SDK credentials for the next request, exchanging a new
// access token when the cached one is missing or about to expire.
func (s *federatedTokenSource) Security(ctx context.Context) (shared.Security, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.accessToken != "" && s.now().Add(federatedTokenRefreshSkew).Before(s.expiresAt) {
		token := s.accessToken
		return shared.Security{APIKey: &token}, nil
	}

	exchanged, err := s.exchange(ctx)
	if err != nil {
		return shared.Security{}, err
	}
	s.accessToken = exchanged.AccessToken
	s.expiresAt = s.now().Add(time.Duration(exchanged.ExpiresIn) * time.Second)

	token := s.accessToken
	return shared.Security{APIKey: &token}, nil
}

func (s *federatedTokenSource) exchange(ctx context.Context) (tokenExchangeResponse, error) {
	subjectToken, err := s.subjectToken()
	if err != nil {
		return tokenExchangeResponse{}, err
	}
	registerSensitiveValues(subjectToken)

	form := url.Values{
		"grant_type":           {tokenExchangeGrantType},
		"subject_token":        {subjectToken},
		"subject_token_type":   {tokenExchangeSubjectType},
		"federation_policy_id": {s.federationPolicyID},
		"scope":                {federatedManagementScope},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return tokenExchangeResponse{}, fmt.Errorf("building token exchange request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	res, err := s.httpClient.Do(req)
	if err != nil {
		return tokenExchangeResponse{}, fmt.Errorf("token exchange request failed: %w", err)
	}
	defer res.Body.Close()

	body, err := io.ReadAll(io.LimitReader(res.Body, maxTokenExchangeResponseBytes))
	if err != nil {
		return tokenExchangeResponse{}, fmt.Errorf("reading token exchange response: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		return tokenExchangeResponse{}, tokenExchangeFailure(res.StatusCode, body)
	}

	return parseTokenExchangeResponse(body)
}

func parseTokenExchangeResponse(body []byte) (tokenExchangeResponse, error) {
	var parsed tokenExchangeResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return tokenExchangeResponse{}, fmt.Errorf("decoding token exchange response: %w", err)
	}
	if parsed.AccessToken == "" || parsed.ExpiresIn <= 0 {
		return tokenExchangeResponse{}, errors.New("token exchange response is missing access_token or expires_in")
	}
	if parsed.Scope != federatedManagementScope {
		return tokenExchangeResponse{}, fmt.Errorf("token exchange granted scope %q, want %q", parsed.Scope, federatedManagementScope)
	}
	registerSensitiveValues(parsed.AccessToken)
	return parsed, nil
}

// tokenExchangeFailure reports only the OAuth error fields, never the raw body.
func tokenExchangeFailure(status int, body []byte) error {
	var parsed tokenExchangeError
	if json.Unmarshal(body, &parsed) != nil || parsed.Error == "" {
		return fmt.Errorf("token exchange failed with HTTP %d", status)
	}
	if parsed.ErrorDescription == "" {
		return fmt.Errorf("token exchange failed with HTTP %d: %s", status, parsed.Error)
	}
	return fmt.Errorf("token exchange failed with HTTP %d: %s: %s", status, parsed.Error, parsed.ErrorDescription)
}

// readSubjectTokenFile rereads the file on every exchange so a token the
// platform rotates on disk (e.g. a projected service-account token) is picked up.
func readSubjectTokenFile(path string) (string, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading oidc_token_file: %w", err)
	}
	token := strings.TrimSpace(string(contents))
	if token == "" {
		return "", errors.New("oidc_token_file is empty")
	}
	return token, nil
}
