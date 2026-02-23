package auth

import (
	"crypto/rand"
	"encoding/hex"
	"math"
	"sync"
	"time"
)

// ============ Rate Limiter ============

// RateLimiter tracks login attempts per IP
type RateLimiter struct {
	mu       sync.Mutex
	attempts map[string]*attemptInfo
	maxAttempts int
	windowSecs  int
}

type attemptInfo struct {
	count    int
	firstAt  time.Time
	lastFail time.Time
}

// NewRateLimiter creates a rate limiter (e.g. 5 attempts per 900 seconds)
func NewRateLimiter(maxAttempts, windowSecs int) *RateLimiter {
	rl := &RateLimiter{
		attempts:    make(map[string]*attemptInfo),
		maxAttempts: maxAttempts,
		windowSecs:  windowSecs,
	}
	// Cleanup goroutine
	go func() {
		for {
			time.Sleep(5 * time.Minute)
			rl.cleanup()
		}
	}()
	return rl
}

// Check returns (allowed bool, waitSeconds int)
func (rl *RateLimiter) Check(ip string) (bool, int) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	info, exists := rl.attempts[ip]
	if !exists {
		return true, 0
	}

	// Window expired → reset
	if time.Since(info.firstAt) > time.Duration(rl.windowSecs)*time.Second {
		delete(rl.attempts, ip)
		return true, 0
	}

	if info.count >= rl.maxAttempts {
		remaining := time.Duration(rl.windowSecs)*time.Second - time.Since(info.firstAt)
		return false, int(remaining.Seconds())
	}

	// Exponential backoff: after each fail, wait 2^(n-1) seconds
	if info.count > 0 {
		backoff := time.Duration(math.Pow(2, float64(info.count-1))) * time.Second
		if time.Since(info.lastFail) < backoff {
			wait := backoff - time.Since(info.lastFail)
			return false, int(wait.Seconds()) + 1
		}
	}

	return true, 0
}

// RecordFail records a failed login attempt
func (rl *RateLimiter) RecordFail(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	info, exists := rl.attempts[ip]
	if !exists {
		rl.attempts[ip] = &attemptInfo{count: 1, firstAt: time.Now(), lastFail: time.Now()}
		return
	}
	info.count++
	info.lastFail = time.Now()
}

// RecordSuccess clears attempts for an IP
func (rl *RateLimiter) RecordSuccess(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.attempts, ip)
}

func (rl *RateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	cutoff := time.Now().Add(-time.Duration(rl.windowSecs) * time.Second)
	for ip, info := range rl.attempts {
		if info.firstAt.Before(cutoff) {
			delete(rl.attempts, ip)
		}
	}
}

// ============ Challenge Store ============

// ChallengeStore manages server-verified slider challenges
type ChallengeStore struct {
	mu         sync.Mutex
	challenges map[string]*Challenge
}

// Challenge represents a slider verification challenge
type Challenge struct {
	Token    string    `json:"token"`
	Target   int       `json:"target"`   // 0-100
	CreateAt time.Time `json:"-"`
}

// NewChallengeStore creates a new challenge store
func NewChallengeStore() *ChallengeStore {
	cs := &ChallengeStore{challenges: make(map[string]*Challenge)}
	go func() {
		for {
			time.Sleep(time.Minute)
			cs.cleanup()
		}
	}()
	return cs
}

// Generate creates a new challenge
func (cs *ChallengeStore) Generate() *Challenge {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	token := generateToken()
	// Random target between 20-80 (avoid edges)
	b := make([]byte, 1)
	rand.Read(b)
	target := 20 + int(b[0])%61

	ch := &Challenge{Token: token, Target: target, CreateAt: time.Now()}
	cs.challenges[token] = ch
	return ch
}

// Verify checks a challenge (one-time use, 60s expiry, ±5 tolerance)
func (cs *ChallengeStore) Verify(token string, value int) bool {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	ch, ok := cs.challenges[token]
	if !ok {
		return false
	}

	// Always delete (one-time use)
	delete(cs.challenges, token)

	// Check expiry (60 seconds)
	if time.Since(ch.CreateAt) > 60*time.Second {
		return false
	}

	// Check value within ±5 tolerance
	diff := ch.Target - value
	if diff < 0 {
		diff = -diff
	}
	return diff <= 5
}

func (cs *ChallengeStore) cleanup() {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cutoff := time.Now().Add(-60 * time.Second)
	for token, ch := range cs.challenges {
		if ch.CreateAt.Before(cutoff) {
			delete(cs.challenges, token)
		}
	}
}

func generateToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
