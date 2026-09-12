package constant

// ClaudeCountTokensPath is the inbound relay path of the Anthropic token
// counting endpoint. Requests on this path may only be served by Anthropic
// channels that explicitly enable count_tokens in their channel settings.
const ClaudeCountTokensPath = "/v1/messages/count_tokens"

// OpenAIInputTokensPath is the inbound relay path of the OpenAI Responses
// input token counting endpoint. Requests on this path may only be served by
// OpenAI channels that explicitly enable count_tokens in their channel settings.
const OpenAIInputTokensPath = "/v1/responses/input_tokens"

// IsClaudeCountTokensPath reports whether a request path targets the Anthropic
// token counting endpoint.
func IsClaudeCountTokensPath(path string) bool {
	return path == ClaudeCountTokensPath
}

// IsOpenAIInputTokensPath reports whether a request path targets the OpenAI
// Responses input token counting endpoint.
func IsOpenAIInputTokensPath(path string) bool {
	return path == OpenAIInputTokensPath
}

// IsCountTokensPath reports whether a request path targets any free token
// counting endpoint (Anthropic count_tokens or OpenAI input_tokens).
func IsCountTokensPath(path string) bool {
	return IsClaudeCountTokensPath(path) || IsOpenAIInputTokensPath(path)
}
