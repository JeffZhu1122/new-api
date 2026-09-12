package types

type RelayFormat string

const (
	RelayFormatOpenAI                     RelayFormat = "openai"
	RelayFormatClaude                                 = "claude"
	RelayFormatGemini                                 = "gemini"
	RelayFormatOpenAIResponses                        = "openai_responses"
	RelayFormatOpenAIResponsesCompaction              = "openai_responses_compaction"
	RelayFormatOpenAIResponsesInputTokens             = "openai_responses_input_tokens"
	RelayFormatOpenAIAlphaSearch                      = "openai_alpha_search"
	RelayFormatOpenAIAudio                            = "openai_audio"
	RelayFormatOpenAIImage                            = "openai_image"
	RelayFormatOpenAIRealtime                         = "openai_realtime"
	RelayFormatRerank                                 = "rerank"
	RelayFormatEmbedding                              = "embedding"

	RelayFormatTask    = "task"
	RelayFormatMjProxy = "mj_proxy"
)
