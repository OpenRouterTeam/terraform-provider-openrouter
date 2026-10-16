package provider

import (
	"context"
	"encoding/json"
	"errors"
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

func recordSleeps(source *federatedTokenSource) *[]time.Duration {
	var slept []time.Duration
	source.sleep = func(_ context.Context, d time.Duration) error {
		slept = append(slept, d)
		return nil
	}
	return &slept
}

func drainSubjects(s *exchangeServer) []string {
	var subjects []string
	for {
		select {
		case subject := <-s.subjects:
			subjects = append(subjects, subject)
		default:
			return subjects
		}
	}
}

func TestFederatedTokenSourceRetriesTransientFailuresWithTheSameSubjectToken(t *testing.T) {
	for name, status := range map[string]int{"429": http.StatusTooManyRequests, "500": http.StatusInternalServerError, "502": http.StatusBadGateway, "503": http.StatusServiceUnavailable} {
		t.Run(name, func(t *testing.T) {
			server := newExchangeServer(t, func(w http.ResponseWriter, _ *http.Request, call int32) {
				if call < maxTokenExchangeAttempts {
					w.WriteHeader(status)
					return
				}
				grantToken(w, "access-after-retry", 900)
			})
			source := newTestSource(t, server.URL, federatedTokenSourceConfig{oidcToken: "subject-jwt"})
			slept := recordSleeps(source)

			security, err := source.Security(context.Background())
			if err != nil {
				t.Fatalf("Security: %v", err)
			}
			if *security.APIKey != "access-after-retry" || server.calls.Load() != maxTokenExchangeAttempts {
				t.Fatalf("got %q after %d exchanges, want access-after-retry after %d", *security.APIKey, server.calls.Load(), maxTokenExchangeAttempts)
			}
			for _, subject := range drainSubjects(server) {
				if subject != "subject-jwt" {
					t.Fatalf("retry sent subject %q, want subject-jwt", subject)
				}
			}
			if len(*slept) != maxTokenExchangeAttempts-1 {
				t.Fatalf("slept %d times, want %d", len(*slept), maxTokenExchangeAttempts-1)
			}
		})
	}
}

func TestFederatedTokenSourceRetriesNetworkErrors(t *testing.T) {
	server := newExchangeServer(t, func(w http.ResponseWriter, _ *http.Request, call int32) {
		if call == 1 {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Errorf("hijack: %v", err)
				return
			}
			_ = conn.Close()
			return
		}
		grantToken(w, "access-after-reset", 900)
	})
	source := newTestSource(t, server.URL, federatedTokenSourceConfig{oidcToken: "subject-jwt"})
	recordSleeps(source)

	security, err := source.Security(context.Background())
	if err != nil {
		t.Fatalf("Security: %v", err)
	}
	if *security.APIKey != "access-after-reset" || server.calls.Load() != 2 {
		t.Fatalf("got %q after %d exchanges, want access-after-reset after 2", *security.APIKey, server.calls.Load())
	}
}

func TestFederatedTokenSourceGivesUpAfterMaxAttempts(t *testing.T) {
	server := newExchangeServer(t, func(w http.ResponseWriter, _ *http.Request, _ int32) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	source := newTestSource(t, server.URL, federatedTokenSourceConfig{oidcToken: "subject-jwt"})
	recordSleeps(source)

	if _, err := source.Security(context.Background()); err == nil || !strings.Contains(err.Error(), "HTTP 503") {
		t.Fatalf("err = %v, want the last 503", err)
	}
	if server.calls.Load() != maxTokenExchangeAttempts {
		t.Fatalf("exchanges = %d, want %d", server.calls.Load(), maxTokenExchangeAttempts)
	}
}

func TestFederatedTokenSourceNeverRetriesClientErrors(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusRequestEntityTooLarge} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := newExchangeServer(t, func(w http.ResponseWriter, _ *http.Request, _ int32) {
				w.WriteHeader(status)
			})
			source := newTestSource(t, server.URL, federatedTokenSourceConfig{oidcToken: "subject-jwt"})
			slept := recordSleeps(source)

			if _, err := source.Security(context.Background()); err == nil {
				t.Fatal("expected the exchange to fail")
			}
			if server.calls.Load() != 1 || len(*slept) != 0 {
				t.Fatalf("exchanges = %d, sleeps = %d; want 1 and 0", server.calls.Load(), len(*slept))
			}
		})
	}
}

func TestFederatedTokenSourceHonorsRetryAfter(t *testing.T) {
	server := newExchangeServer(t, func(w http.ResponseWriter, _ *http.Request, call int32) {
		if call == 1 {
			w.Header().Set("Retry-After", "5")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		grantToken(w, "access", 900)
	})
	source := newTestSource(t, server.URL, federatedTokenSourceConfig{oidcToken: "subject-jwt"})
	slept := recordSleeps(source)

	if _, err := source.Security(context.Background()); err != nil {
		t.Fatalf("Security: %v", err)
	}
	if len(*slept) != 1 || (*slept)[0] != 5*time.Second {
		t.Fatalf("slept %v, want [5s]", *slept)
	}
}

func TestFederatedTokenSourceStopsRetryingWhenContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := newExchangeServer(t, func(w http.ResponseWriter, _ *http.Request, _ int32) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	source := newTestSource(t, server.URL, federatedTokenSourceConfig{oidcToken: "subject-jwt"})
	source.sleep = func(ctx context.Context, d time.Duration) error {
		cancel()
		return sleepContext(ctx, d)
	}

	start := time.Now()
	_, err := source.Security(ctx)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if server.calls.Load() != 1 || time.Since(start) > 5*time.Second {
		t.Fatalf("exchanges = %d after %v; want 1 without waiting out Retry-After", server.calls.Load(), time.Since(start))
	}
}

func TestTokenExchangeRetryDelayGrowsExponentiallyWithJitter(t *testing.T) {
	for attempt := 1; attempt < maxTokenExchangeAttempts; attempt++ {
		ceiling := tokenExchangeBaseBackoff << (attempt - 1)
		for range 100 {
			if d := tokenExchangeRetryDelay(attempt, 0); d < ceiling/2 || d > ceiling {
				t.Fatalf("attempt %d delay %v outside [%v, %v]", attempt, d, ceiling/2, ceiling)
			}
		}
	}
	if d := tokenExchangeRetryDelay(1, time.Hour); d != maxTokenExchangeRetryDelay {
		t.Fatalf("delay with a huge Retry-After = %v, want the %v cap", d, maxTokenExchangeRetryDelay)
	}
}

func TestParseRetryAfterAcceptsSecondsAndHTTPDates(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	for value, want := range map[string]time.Duration{
		"5":                             5 * time.Second,
		" 60 ":                          time.Minute,
		"-1":                            0,
		"Fri, 16 Oct 2026 12:00:30 GMT": 30 * time.Second,
		"Fri, 16 Oct 2026 11:59:00 GMT": 0,
		"soon":                          0,
		"":                              0,
	} {
		if got := parseRetryAfter(value, now); got != want {
			t.Errorf("parseRetryAfter(%q) = %v, want %v", value, got, want)
		}
	}
}

func TestFederatedTokenSourceRereadsTokenFileBetweenRetries(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("expired-jwt"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := newExchangeServer(t, func(w http.ResponseWriter, r *http.Request, _ int32) {
		if r.PostForm.Get("subject_token") != "rotated-jwt" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		grantToken(w, "access", 900)
	})
	source := newTestSource(t, server.URL, federatedTokenSourceConfig{oidcTokenFile: tokenFile})
	source.sleep = func(context.Context, time.Duration) error {
		return os.WriteFile(tokenFile, []byte("rotated-jwt"), 0o600)
	}

	if _, err := source.Security(context.Background()); err != nil {
		t.Fatalf("Security: %v", err)
	}
	if subjects := drainSubjects(server); len(subjects) != 2 || subjects[0] != "expired-jwt" || subjects[1] != "rotated-jwt" {
		t.Fatalf("subjects = %v, want [expired-jwt rotated-jwt]", subjects)
	}
}

func TestFederatedTokenSourceServesCachedTokenWhileRefreshing(t *testing.T) {
	release := make(chan struct{})
	server := newExchangeServer(t, func(w http.ResponseWriter, _ *http.Request, _ int32) {
		<-release
		grantToken(w, "refreshed", 900)
	})
	source := newTestSource(t, server.URL, federatedTokenSourceConfig{oidcToken: "subject-jwt"})
	source.accessToken = "cached"
	source.expiresAt = time.Now().Add(federatedTokenRefreshSkew / 2)

	refreshed := make(chan string, 1)
	go func() {
		security, err := source.Security(context.Background())
		if err != nil {
			t.Errorf("refreshing Security: %v", err)
		}
		refreshed <- *security.APIKey
	}()
	for server.calls.Load() == 0 {
		time.Sleep(time.Millisecond)
	}

	security, err := source.Security(context.Background())
	if err != nil || *security.APIKey != "cached" {
		t.Fatalf("concurrent Security = %v, %v; want the cached token without waiting", security.APIKey, err)
	}
	close(release)
	if got := <-refreshed; got != "refreshed" {
		t.Fatalf("refresh returned %q, want refreshed", got)
	}
	if server.calls.Load() != 1 {
		t.Fatalf("exchanges = %d, want 1", server.calls.Load())
	}
}

func TestFederatedTokenSourceRunsOneExchangeForConcurrentCallers(t *testing.T) {
	release := make(chan struct{})
	server := newExchangeServer(t, func(w http.ResponseWriter, _ *http.Request, _ int32) {
		<-release
		grantToken(w, "access", 900)
	})
	source := newTestSource(t, server.URL, federatedTokenSourceConfig{oidcToken: "subject-jwt"})

	const callers = 5
	results := make(chan string, callers)
	for range callers {
		go func() {
			security, err := source.Security(context.Background())
			if err != nil {
				t.Errorf("Security: %v", err)
				results <- ""
				return
			}
			results <- *security.APIKey
		}()
	}
	for server.calls.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	close(release)
	for range callers {
		if got := <-results; got != "access" {
			t.Fatalf("caller got %q, want access", got)
		}
	}
	if server.calls.Load() != 1 {
		t.Fatalf("exchanges = %d, want 1", server.calls.Load())
	}
}

func TestFederatedTokenSourceKeepsValidTokenWhenRefreshFails(t *testing.T) {
	server := newExchangeServer(t, func(w http.ResponseWriter, _ *http.Request, _ int32) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	source := newTestSource(t, server.URL, federatedTokenSourceConfig{oidcToken: "subject-jwt"})
	recordSleeps(source)
	source.accessToken = "cached"
	source.expiresAt = time.Now().Add(federatedTokenRefreshSkew / 2)

	security, err := source.Security(context.Background())
	if err != nil || *security.APIKey != "cached" {
		t.Fatalf("Security = %v, %v; want the still-valid cached token", security.APIKey, err)
	}
}

func TestFederatedTokenSourceWaiterHonorsContext(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	server := newExchangeServer(t, func(w http.ResponseWriter, _ *http.Request, _ int32) {
		<-release
		grantToken(w, "access", 900)
	})
	source := newTestSource(t, server.URL, federatedTokenSourceConfig{oidcToken: "subject-jwt"})

	go func() { _, _ = source.Security(context.Background()) }()
	for server.calls.Load() == 0 {
		time.Sleep(time.Millisecond)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := source.Security(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
}

func TestFederatedTokenSourceWaitsForRefreshWhenCachedTokenIsAboutToExpire(t *testing.T) {
	release := make(chan struct{})
	server := newExchangeServer(t, func(w http.ResponseWriter, _ *http.Request, _ int32) {
		<-release
		grantToken(w, "refreshed", 900)
	})
	source := newTestSource(t, server.URL, federatedTokenSourceConfig{oidcToken: "subject-jwt"})
	source.accessToken = "almost-expired"
	source.expiresAt = time.Now().Add(federatedTokenMinRemaining / 2)

	go func() { _, _ = source.Security(context.Background()) }()
	for server.calls.Load() == 0 {
		time.Sleep(time.Millisecond)
	}

	waiter := make(chan string, 1)
	go func() {
		security, err := source.Security(context.Background())
		if err != nil {
			t.Errorf("Security: %v", err)
			waiter <- ""
			return
		}
		waiter <- *security.APIKey
	}()
	select {
	case got := <-waiter:
		t.Fatalf("got %q before the refresh finished, want the caller to wait", got)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if got := <-waiter; got != "refreshed" {
		t.Fatalf("got %q, want refreshed", got)
	}
}
