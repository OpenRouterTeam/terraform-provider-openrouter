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

// privateProviderRetentionDaysHook sends data_policy.prompt_retention_days as
// an explicit null when the provider plans it null. The generated models omit
// a nil value, but the API requires the key on create and, on update, rejects
// retains_prompts = false over stored days unless the days are cleared with an
// explicit null. Terraform always sends the whole declared policy, so a
// missing key always means null.
type privateProviderRetentionDaysHook struct{}

func (privateProviderRetentionDaysHook) BeforeRequest(_ BeforeRequestContext, req *http.Request) (*http.Request, error) {
	if req.Body == nil || req.Body == http.NoBody || !isPrivateProviderWrite(req) {
		return req, nil
	}
	raw, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, fmt.Errorf("private provider retention days: read request body: %w", err)
	}
	_ = req.Body.Close()

	out, err := withNullRetentionDays(raw)
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

func isPrivateProviderWrite(req *http.Request) bool {
	match := privateProviderPath.FindStringSubmatch(req.URL.Path)
	if match == nil {
		return false
	}
	isCollection := match[1] == ""
	return (isCollection && req.Method == http.MethodPost) || (!isCollection && req.Method == http.MethodPatch)
}

func withNullRetentionDays(raw []byte) ([]byte, error) {
	body := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("private provider retention days: request body is not a JSON object: %w", err)
	}
	policyRaw, ok := body["data_policy"]
	if !ok {
		return raw, nil
	}
	policy := map[string]json.RawMessage{}
	if err := json.Unmarshal(policyRaw, &policy); err != nil {
		return nil, fmt.Errorf("private provider retention days: data_policy is not a JSON object: %w", err)
	}
	if _, ok := policy["prompt_retention_days"]; ok {
		return raw, nil
	}
	policy["prompt_retention_days"] = json.RawMessage("null")
	encodedPolicy, err := json.Marshal(policy)
	if err != nil {
		return nil, fmt.Errorf("private provider retention days: encode data_policy: %w", err)
	}
	body["data_policy"] = encodedPolicy
	out, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("private provider retention days: encode request body: %w", err)
	}
	return out, nil
}
