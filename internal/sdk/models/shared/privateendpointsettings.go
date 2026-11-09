package shared

// PrivateEndpointSettings models the endpoint context-length setting.
// Other API settings are left unmanaged.
type PrivateEndpointSettings struct {
	// Maximum combined input and reserved output token count.
	ContextLength *int64 `json:"context_length,omitempty"`
}
