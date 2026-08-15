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
