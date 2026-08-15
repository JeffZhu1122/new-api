package operation_setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAutomaticRetryKeywordsFromString(t *testing.T) {
	orig := AutomaticRetryKeywords
	t.Cleanup(func() { AutomaticRetryKeywords = orig })

	AutomaticRetryKeywordsFromString("  Content Policy Violation \n\ninvalid_prompt\r\n")
	require.Equal(t, []string{"content policy violation", "invalid_prompt"}, AutomaticRetryKeywords)

	AutomaticRetryKeywordsFromString("")
	require.Empty(t, AutomaticRetryKeywords)
}

func TestAutomaticRetryKeywordsRoundTrip(t *testing.T) {
	orig := AutomaticRetryKeywords
	t.Cleanup(func() { AutomaticRetryKeywords = orig })

	AutomaticRetryKeywordsFromString("foo\nBar")
	require.Equal(t, "foo\nbar", AutomaticRetryKeywordsToString())
}

func TestRetryAvoidFailedChannelsHTTPStatusCodeFallsBackOnInvalidValue(t *testing.T) {
	orig := RetryAvoidFailedChannelsStatusCode
	t.Cleanup(func() { RetryAvoidFailedChannelsStatusCode = orig })

	RetryAvoidFailedChannelsStatusCode = 503
	require.Equal(t, 503, RetryAvoidFailedChannelsHTTPStatusCode())

	RetryAvoidFailedChannelsStatusCode = 0
	require.Equal(t, 429, RetryAvoidFailedChannelsHTTPStatusCode())

	RetryAvoidFailedChannelsStatusCode = 1000
	require.Equal(t, 429, RetryAvoidFailedChannelsHTTPStatusCode())
}

func TestRetryAvoidFailedChannelsMessageReplacesPlaceholderAndFallsBack(t *testing.T) {
	orig := RetryAvoidFailedChannelsErrorMessage
	t.Cleanup(func() { RetryAvoidFailedChannelsErrorMessage = orig })

	RetryAvoidFailedChannelsErrorMessage = "no channel for {model}, try later"
	require.Equal(t, "no channel for gpt-4o, try later", RetryAvoidFailedChannelsMessage("gpt-4o"))

	RetryAvoidFailedChannelsErrorMessage = "  "
	require.Equal(t,
		"all available channels for model gpt-4o have failed in this request, no channels left to retry",
		RetryAvoidFailedChannelsMessage("gpt-4o"))
}
