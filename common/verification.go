package common

import (
	"crypto/subtle"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type verificationValue struct {
	code     string
	time     time.Time
	failures int
}

const (
	EmailVerificationPurpose = "v"
	PasswordResetPurpose     = "r"
)

var verificationMutex sync.Mutex
var verificationMap map[string]verificationValue
var verificationMapMaxSize = 10
var VerificationValidMinutes = 10

// verificationMaxFailures is how many wrong guesses a code survives. The
// registration code is six hex characters, so without a cap the per-address
// request limit is the only thing between a guesser and the code.
const verificationMaxFailures = 5

func GenerateVerificationCode(length int) string {
	code := uuid.New().String()
	code = strings.Replace(code, "-", "", -1)
	if length == 0 {
		return code
	}
	return code[:length]
}

func RegisterVerificationCodeWithKey(key string, code string, purpose string) {
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	verificationMap[purpose+key] = verificationValue{
		code: code,
		time: time.Now(),
	}
	if len(verificationMap) > verificationMapMaxSize {
		removeExpiredPairs()
	}
}

// VerifyCodeWithKey reports whether code is the live code for key. A match
// leaves the code in place for the caller to delete once the operation it
// guards has gone through; a wrong guess counts against the code, and the
// guess that reaches verificationMaxFailures removes it.
func VerifyCodeWithKey(key string, code string, purpose string) bool {
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	value, okay := verificationMap[purpose+key]
	if !okay {
		return false
	}
	if int(time.Since(value.time).Seconds()) >= VerificationValidMinutes*60 {
		delete(verificationMap, purpose+key)
		return false
	}
	if subtle.ConstantTimeCompare([]byte(code), []byte(value.code)) == 1 {
		return true
	}
	value.failures++
	if value.failures >= verificationMaxFailures {
		delete(verificationMap, purpose+key)
		return false
	}
	verificationMap[purpose+key] = value
	return false
}

func DeleteKey(key string, purpose string) {
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	delete(verificationMap, purpose+key)
}

// no lock inside, so the caller must lock the verificationMap before calling!
func removeExpiredPairs() {
	now := time.Now()
	for key := range verificationMap {
		if int(now.Sub(verificationMap[key].time).Seconds()) >= VerificationValidMinutes*60 {
			delete(verificationMap, key)
		}
	}
}

func init() {
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	verificationMap = make(map[string]verificationValue)
}
