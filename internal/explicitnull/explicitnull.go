// Package explicitnull lets hand-written resources send explicit JSON nulls
// through the generated SDK, whose request models drop nil fields (omitzero).
package explicitnull

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type contextKey struct{}

// WithFields returns a context that makes Apply set the given top-level JSON
// body fields to null on the request issued with it.
func WithFields(ctx context.Context, fields ...string) context.Context {
	if len(fields) == 0 {
		return ctx
	}
	return context.WithValue(ctx, contextKey{}, fields)
}

// Apply rewrites req's JSON object body so that every field registered with
// WithFields on req's context is present with a null value. Requests without
// registered fields are returned unchanged.
func Apply(req *http.Request) (*http.Request, error) {
	fields, _ := req.Context().Value(contextKey{}).([]string)
	if len(fields) == 0 || req.Body == nil || req.Body == http.NoBody {
		return req, nil
	}

	raw, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, fmt.Errorf("explicitnull: read request body: %w", err)
	}
	_ = req.Body.Close()

	body := map[string]json.RawMessage{}
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &body); err != nil {
			return nil, fmt.Errorf("explicitnull: request body is not a JSON object: %w", err)
		}
	}
	for _, field := range fields {
		body[field] = json.RawMessage("null")
	}
	out, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("explicitnull: encode request body: %w", err)
	}

	req.Body = io.NopCloser(bytes.NewReader(out))
	req.ContentLength = int64(len(out))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(out)), nil
	}
	return req, nil
}
