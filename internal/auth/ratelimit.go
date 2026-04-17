package auth

import (
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// ============ Rate Limiter ============

// RateLimiter tracks request counts per client (IP) within a sliding window.
// Exponential backoff applies: after each failure, the next attempt must wait
// 2^(n-1) seconds. Callers explicitly record failures via RecordFail so that
// unauthenticated/anonymous endpoints can rate-limit without penalising
// legitimate successful requests.
type RateLimiter struct {
	mu          sync.Mutex
	attempts    map[string]*attemptInfo
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

// Middleware returns a gin.HandlerFunc that rate-limits requests by client IP.
// On exceed, responds 429 with a Retry-After header. Does NOT call RecordFail;
// that is the handler's responsibility (a failed login is different from a
// rate-limited anonymous GET).
func (rl *RateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		allowed, waitSec := rl.Check(ip)
		if !allowed {
			c.Header("Retry-After", strconv.Itoa(waitSec))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":       "Too many requests",
				"retry_after": waitSec,
			})
			return
		}
		c.Next()
	}
}

// ============ Scenario Limiters ============

// Limiters bundles the scenario-specific rate limiters instantiated at startup.
// Each limiter is tuned for its call site:
//   - Login guards the credential-check path (brute-force resistance).
//   - TOTP guards the 2FA verification path (slower iteration).
//   - APIRead is a generous ceiling for GET endpoints.
//   - APIWrite is stricter for mutating endpoints.
//   - Default is an umbrella fallback for any route that does not opt into a
//     specific bucket.
type Limiters struct {
	Login    *RateLimiter
	TOTP     *RateLimiter
	APIRead  *RateLimiter
	APIWrite *RateLimiter
	Default  *RateLimiter
}

// NewLimiters constructs the standard set with production-tuned thresholds:
//
//	Login:    5 per 15 min   (matches pre-v0.11 behaviour — brute-force resistant)
//	TOTP:     10 per 5 min   (allows legitimate typo recovery, blocks guessing)
//	APIRead:  300 per min    (dashboard polling headroom)
//	APIWrite: 60 per min     (mutation ceiling per IP)
//	Default:  600 per min    (umbrella for unlabelled routes)
func NewLimiters() *Limiters {
	return &Limiters{
		Login:    NewRateLimiter(5, 900),
		TOTP:     NewRateLimiter(10, 300),
		APIRead:  NewRateLimiter(300, 60),
		APIWrite: NewRateLimiter(60, 60),
		Default:  NewRateLimiter(600, 60),
	}
}
