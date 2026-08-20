package constant

// ClaudeCountTokensPath is the inbound relay path of the Anthropic token
// counting endpoint. Requests on this path may only be served by Anthropic
// channels that explicitly enable count_tokens in their channel settings.
const ClaudeCountTokensPath = "/v1/messages/count_tokens"

// IsClaudeCountTokensPath reports whether a request path targets the Anthropic
// token counting endpoint.
func IsClaudeCountTokensPath(path string) bool {
	return path == ClaudeCountTokensPath
}
