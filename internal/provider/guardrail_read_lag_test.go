package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/operations"
)

// guardrailReadLagStub answers every guardrail read with a fixed status and
// reports each one, so a test can act while the helper sits in its retry delay.
// A nil served channel simply skips the notification.
func guardrailReadLagStub(t *testing.T, status int, served chan<- struct{}) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var gets atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gets.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"code": status, "message": "Guardrail not found"},
		})
		if served != nil {
			select {
			case served <- struct{}{}:
			default:
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &gets
}

func guardrailReadLagRequest() *operations.GetGuardrailRequest {
	return &operations.GetGuardrailRequest{ID: "c6a1f4c8-0f1e-4b6a-9d2f-5f0f3e7a1b22"}
}

// A canceled read must not be presented as a confirmed absence. Returning the
// last 404 with no error let the resource Read drop the guardrail from state
// (guardrail_resource.go, res.StatusCode == 404 -> State.RemoveResource) and the
// data source fail with an unexpected 404, for an operation Terraform had
// canceled rather than one confirmed missing.
func TestGuardrailReadLagCancellationPropagates(t *testing.T) {
	served := make(chan struct{}, 1)
	srv, gets := guardrailReadLagStub(t, http.StatusNotFound, served)
	client := sdk.New(sdk.WithServerURL(srv.URL))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type result struct {
		res *operations.GetGuardrailResponse
		err error
	}
	done := make(chan result, 1)
	go func() {
		res, err := getGuardrailTolerantOfReadLag(ctx, client, guardrailReadLagRequest())
		done <- result{res: res, err: err}
	}()

	select {
	case <-served:
	case <-time.After(5 * time.Second):
		t.Fatal("the stub never answered the first read")
	}
	// Cancel well inside the retry delay, not while the request is in flight,
	// so this exercises the ctx.Done arm rather than the transport error arm.
	time.Sleep(50 * time.Millisecond)
	cancel()

	var got result
	select {
	case got = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the helper did not return after cancellation")
	}

	if got.err == nil {
		t.Fatalf("err = nil with res.StatusCode = %v: an interrupted retry was reported as a definitive absence", got.res.StatusCode)
	}
	if !errors.Is(got.err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", got.err)
	}
	if got.res != nil {
		t.Fatalf("res = %v, want nil so no caller can read a 404 as absence", got.res)
	}
	if n := gets.Load(); n != 1 {
		t.Fatalf("reads = %d, want 1: the helper should return during the retry delay, not issue another read", n)
	}
}

// The tolerance the fix exists for must survive: a 404 that lasts the whole
// window is still reported as an absence with no error, which is what lets a
// create-then-immediately-read sequence converge instead of planning a
// replacement.
func TestGuardrailReadLagExhaustsWindowAsAbsence(t *testing.T) {
	srv, gets := guardrailReadLagStub(t, http.StatusNotFound, nil)
	client := sdk.New(sdk.WithServerURL(srv.URL))

	res, err := getGuardrailTolerantOfReadLag(context.Background(), client, guardrailReadLagRequest())
	if err != nil {
		t.Fatalf("err = %v, want nil for an absence that outlasts the window", err)
	}
	if res == nil {
		t.Fatal("res = nil, want the 404 response")
	}
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("StatusCode = %d, want 404", res.StatusCode)
	}
	if n := gets.Load(); n != int64(guardrailLagAttempts) {
		t.Fatalf("reads = %d, want %d", n, guardrailLagAttempts)
	}
}
