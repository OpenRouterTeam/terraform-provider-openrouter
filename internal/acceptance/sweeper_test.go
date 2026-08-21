package acceptance

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// TestMain sweeps orphaned tf-acc-* resources before the suite runs, so a
// crashed or cancelled previous run can never accumulate garbage (or exhaust
// account caps like the 5-destinations-per-type limit).
func TestMain(m *testing.M) {
	if os.Getenv("TF_ACC") != "" && os.Getenv("OPENROUTER_MANAGEMENT_KEY") != "" {
		if err := sweep(); err != nil {
			fmt.Fprintf(os.Stderr, "sweeper: %v (continuing)\n", err)
		}
	}
	os.Exit(m.Run())
}

func apiBase() string {
	return testAccAPIBase()
}

func sweep() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	var errs []string
	// API keys: identified by name, deleted by hash.
	if err := sweepCollection(ctx, "/keys", "hash", "name"); err != nil {
		errs = append(errs, "keys: "+err.Error())
	}
	// Guardrails, workspaces, destinations: identified by name, deleted by id.
	for _, path := range []string{"/guardrails", "/workspaces", "/observability/destinations"} {
		if err := sweepCollection(ctx, path, "id", "name"); err != nil {
			errs = append(errs, path+": "+err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

// sweepCollection lists a management collection and deletes every entry whose
// name carries the tf-acc prefix.
func sweepCollection(ctx context.Context, path, idField, nameField string) error {
	body, err := apiRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	var payload struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf("decode list: %w", err)
	}
	for _, item := range payload.Data {
		name, _ := item[nameField].(string)
		if !strings.HasPrefix(name, runPrefix+"-") {
			continue
		}
		id, _ := item[idField].(string)
		if id == "" {
			continue
		}
		if _, err := apiRequest(ctx, http.MethodDelete, path+"/"+id, nil); err != nil {
			fmt.Fprintf(os.Stderr, "sweeper: failed deleting %s %q (%s): %v\n", path, name, id, err)
			continue
		}
		fmt.Fprintf(os.Stderr, "sweeper: deleted orphaned %s %q\n", path, name)
	}
	return nil
}

func apiRequest(ctx context.Context, method, path string, body io.Reader) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, apiBase()+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+os.Getenv("OPENROUTER_MANAGEMENT_KEY"))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// The response body is intentionally excluded from this error: it
		// may echo sensitive configuration (e.g. destination credentials)
		// back from the Management API, and this error can surface in CI
		// logs via the sweeper's stderr diagnostics.
		return nil, fmt.Errorf("%s %s: HTTP %d", method, path, resp.StatusCode)
	}
	return data, nil
}
