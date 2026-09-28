package stringplanmodifier

import "testing"

// Expected values follow the API's normalizeBaseUrl: WHATWG URL serialization,
// then one trailing "/" and a trailing "/chat/completions" stripped.
func TestNormalizePrivateEndpointBaseURL(t *testing.T) {
	for raw, want := range map[string]string{
		"https://contoso.openai.azure.com":             "https://contoso.openai.azure.com",
		"https://contoso.openai.azure.com/":            "https://contoso.openai.azure.com",
		"HTTPS://Contoso.OpenAI.Azure.com/openai/v1":   "https://contoso.openai.azure.com/openai/v1",
		"https://contoso.openai.azure.com:443/v1/":     "https://contoso.openai.azure.com/v1",
		"https://contoso.openai.azure.com:8443/v1":     "https://contoso.openai.azure.com:8443/v1",
		"https://api.example.com/v1/chat/completions":  "https://api.example.com/v1",
		"https://api.example.com/v1/chat/completions/": "https://api.example.com/v1",
		"https://api.example.com/chat/completions":     "https://api.example.com",
		"https://api.example.com/v1//":                 "https://api.example.com/v1/",
		"https://api.example.com/V1":                   "https://api.example.com/V1",
		"not a url":                                    "not a url",
	} {
		if got := NormalizePrivateEndpointBaseURL(raw); got != want {
			t.Errorf("NormalizePrivateEndpointBaseURL(%q) = %q, want %q", raw, got, want)
		}
	}
}
