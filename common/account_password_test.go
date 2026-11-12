package common

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

func TestDecoyPasswordsCostWhatStoredPasswordsCost(t *testing.T) {
	// The temporary bcrypt write mode must not turn the Argon2id decoy into
	// a second bcrypt one.
	t.Setenv("ACCOUNT_PASSWORD_HASH_ALGORITHM", "bcrypt")

	stored, err := hashArgon2AccountPassword("stored-password")
	require.NoError(t, err)
	parameters := stored[:strings.LastIndex(stored[:strings.LastIndex(stored, "$")], "$")+1]
	assert.True(t, strings.HasPrefix(decoyArgon2AccountPasswordHash(), parameters), "the Argon2id decoy uses the parameters of stored passwords")

	legacy, err := Password2Hash("stored-password")
	require.NoError(t, err)
	legacyCost, err := bcrypt.Cost([]byte(legacy))
	require.NoError(t, err)
	decoyCost, err := bcrypt.Cost([]byte(decoyBcryptAccountPasswordHash()))
	require.NoError(t, err)
	assert.Equal(t, legacyCost, decoyCost, "the bcrypt decoy uses the cost of stored passwords")
}

func TestSignInPasswordChecksCoverBothAlgorithms(t *testing.T) {
	argon2Hash, err := hashArgon2AccountPassword("stored-password")
	require.NoError(t, err)
	bcryptHash, err := Password2Hash("stored-password")
	require.NoError(t, err)

	for _, test := range []struct {
		name    string
		stored  []string
		checked AccountPasswordChecks
	}{
		{"Argon2id account", []string{argon2Hash}, AccountPasswordChecks{argon2: true}},
		{"legacy bcrypt account", []string{bcryptHash}, AccountPasswordChecks{bcrypt: true}},
		{"no account", nil, AccountPasswordChecks{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, password := range []string{"stored-password", "tried-password"} {
				var checks AccountPasswordChecks
				matched := false
				for _, hash := range test.stored {
					matched = checks.Validate(password, hash) || matched
				}
				assert.Equal(t, password == "stored-password" && len(test.stored) > 0, matched)
				assert.Equal(t, test.checked, checks, "the stored passwords account for their own algorithm")

				checks.Finish(password)
				assert.Equal(t, AccountPasswordChecks{argon2: true, bcrypt: true}, checks, "the decoys make up the other one")
			}
		})
	}
}
