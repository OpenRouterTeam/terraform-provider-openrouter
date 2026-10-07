package hooks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
)

var privateProviderPath = regexp.MustCompile(`/private-providers(/[^/]+)?$`)

// Nullable private provider fields the resource clears by leaving them out of
// configuration. Top-level fields only apply to PATCH; create defaults them.
var privateProviderNullableFields = []string{"privacy_policy_url", "headquarters"}

// privateProviderExplicitNullsHook sends the nulls the generated models omit on
// private provider writes. The API requires data_policy.prompt_retention_days
// on create, and PATCH keeps any field it doesn't receive, so a value removed
// from configuration would otherwise never clear. The resource is the only
// caller and always sends every configured field and the whole data policy,
// so a missing key always means the plan is null.
type privateProviderExplicitNullsHook struct{}

func (privateProviderExplicitNullsHook) BeforeRequest(_ BeforeRequestContext, req *http.Request) (*http.Request, error) {
	if req.Body == nil || req.Body == http.NoBody {
		return req, nil
	}
	match := privateProviderPath.FindStringSubmatch(req.URL.Path)
	if match == nil {
		return req, nil
	}
	isCreate := match[1] == "" && req.Method == http.MethodPost
	isUpdate := match[1] != "" && req.Method == http.MethodPatch
	if !isCreate && !isUpdate {
		return req, nil
	}

	raw, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, fmt.Errorf("private provider explicit nulls: read request body: %w", err)
	}
	_ = req.Body.Close()
	out, err := withPrivateProviderNulls(raw, isUpdate)
	if err != nil {
		return nil, err
	}
	req.Body = io.NopCloser(bytes.NewReader(out))
	req.ContentLength = int64(len(out))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(out)), nil
	}
	return req, nil
}

func withPrivateProviderNulls(raw []byte, isUpdate bool) ([]byte, error) {
	body := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("private provider explicit nulls: request body is not a JSON object: %w", err)
	}
	if isUpdate {
		setMissingToNull(body, privateProviderNullableFields...)
	}
	if policyRaw, ok := body["data_policy"]; ok {
		policy := map[string]json.RawMessage{}
		if err := json.Unmarshal(policyRaw, &policy); err != nil {
			return nil, fmt.Errorf("private provider explicit nulls: data_policy is not a JSON object: %w", err)
		}
		setMissingToNull(policy, "prompt_retention_days")
		encoded, err := json.Marshal(policy)
		if err != nil {
			return nil, fmt.Errorf("private provider explicit nulls: encode data_policy: %w", err)
		}
		body["data_policy"] = encoded
	}
	out, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("private provider explicit nulls: encode request body: %w", err)
	}
	return out, nil
}

func setMissingToNull(object map[string]json.RawMessage, fields ...string) {
	for _, field := range fields {
		if _, ok := object[field]; !ok {
			object[field] = json.RawMessage("null")
		}
	}
}
