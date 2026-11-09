package shared

// PrivateEndpointSettings contains only limits explicitly managed by the caller.
// Nil fields are omitted from PATCH and preserve their current values.
type PrivateEndpointSettings struct {
	ContextLength   *int64 `json:"context_length,omitempty"`
	MaxPromptTokens *int64 `json:"max_prompt_tokens,omitempty"`
}
