package auth

import (
	"encoding/base64"
	"encoding/json"
	"sync"
	"time"

	altcha "github.com/altcha-org/altcha-lib-go"
)

// altchaTTL is the challenge validity window; a verified solution is held in
// the single-use store for this duration to block replay within the window.
const altchaTTL = 120 * time.Second

// altchaUsedStore tracks already-redeemed ALTCHA solutions so a solved payload
// cannot be replayed until it expires. Keyed by the challenge's unique
// challenge string; values are the expiry time for lazy cleanup. Mutex-guarded
// for concurrent use across login/setup requests.
var altchaUsedStore = struct {
	sync.Mutex
	seen map[string]time.Time
}{seen: make(map[string]time.Time)}

// markAltchaUsed records a solution key as redeemed, returning false if it was
// already present (i.e. a replay). It sweeps expired entries on each call so
// the map cannot grow unbounded without an external dependency.
func markAltchaUsed(key string) bool {
	now := time.Now()
	altchaUsedStore.Lock()
	defer altchaUsedStore.Unlock()
	for k, exp := range altchaUsedStore.seen {
		if now.After(exp) {
			delete(altchaUsedStore.seen, k)
		}
	}
	if _, ok := altchaUsedStore.seen[key]; ok {
		return false
	}
	altchaUsedStore.seen[key] = now.Add(altchaTTL)
	return true
}

// GenerateAltchaChallenge creates a new ALTCHA PoW challenge
func GenerateAltchaChallenge(hmacKey string) (altcha.Challenge, error) {
	expires := time.Now().Add(120 * time.Second)
	return altcha.CreateChallenge(altcha.ChallengeOptions{
		HMACKey:   hmacKey,
		MaxNumber: 50000,
		Expires:   &expires,
	})
}

// VerifyAltchaSolution verifies a base64-encoded ALTCHA payload
func VerifyAltchaSolution(payload string, hmacKey string) (bool, error) {
	// Decode base64 payload from widget
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return false, err
	}

	// Parse JSON into map
	var data altcha.Payload
	if err := json.Unmarshal(decoded, &data); err != nil {
		return false, err
	}

	ok, err := altcha.VerifySolution(data, hmacKey, true)
	if err != nil || !ok {
		return ok, err
	}

	// Single-use enforcement: a valid solution may be redeemed only once within
	// the challenge validity window, blocking replay. Key on the challenge's
	// unique hash (derived from the per-request salt), falling back to the full
	// payload when absent.
	key := data.Challenge
	if key == "" {
		key = data.Salt + ":" + data.Signature
	}
	if !markAltchaUsed(key) {
		return false, nil
	}

	return true, nil
}
