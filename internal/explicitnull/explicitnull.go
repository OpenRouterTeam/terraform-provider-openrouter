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
	"strings"
)

type contextKey struct{}

// WithFields returns a context that makes Apply set the given JSON body fields
// to null on the request issued with it. A field is either top-level ("name")
// or one level into an object the body already sends ("parent.name").
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
		if err := setNull(body, field); err != nil {
			return nil, err
		}
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

// setNull sets field to null in body. A nested field is only set when its
// parent object is present, so a request that leaves the parent out keeps it.
func setNull(body map[string]json.RawMessage, field string) error {
	parent, child, isNested := strings.Cut(field, ".")
	if !isNested {
		body[field] = json.RawMessage("null")
		return nil
	}
	raw, ok := body[parent]
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	object := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &object); err != nil {
		return fmt.Errorf("explicitnull: %s is not a JSON object: %w", parent, err)
	}
	object[child] = json.RawMessage("null")
	encoded, err := json.Marshal(object)
	if err != nil {
		return fmt.Errorf("explicitnull: encode %s: %w", parent, err)
	}
	body[parent] = encoded
	return nil
}
