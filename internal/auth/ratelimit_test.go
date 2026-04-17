package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestNewLimiters_DistinctBuckets(t *testing.T) {
	l := NewLimiters()
	if l.Login == nil || l.TOTP == nil || l.APIRead == nil || l.APIWrite == nil || l.Default == nil {
		t.Fatal("all limiter fields must be non-nil")
	}

	// Burn the Login budget; TOTP and API buckets must remain untouched.
	for i := 0; i < 5; i++ {
		l.Login.RecordFail("1.1.1.1")
	}
	if allowed, _ := l.Login.Check("1.1.1.1"); allowed {
		t.Error("Login should be blocked after 5 fails (window=15min)")
	}
	if allowed, _ := l.TOTP.Check("1.1.1.1"); !allowed {
		t.Error("TOTP must not inherit Login failures — separate buckets")
	}
	if allowed, _ := l.APIRead.Check("1.1.1.1"); !allowed {
		t.Error("APIRead must not inherit Login failures")
	}
	if allowed, _ := l.APIWrite.Check("1.1.1.1"); !allowed {
		t.Error("APIWrite must not inherit Login failures")
	}
}

func TestLimiters_TuningMatchesPlan(t *testing.T) {
	l := NewLimiters()
	cases := []struct {
		name   string
		rl     *RateLimiter
		window int
		max    int
	}{
		{"Login 5/15min", l.Login, 900, 5},
		{"TOTP 10/5min", l.TOTP, 300, 10},
		{"APIRead 300/min", l.APIRead, 60, 300},
		{"APIWrite 60/min", l.APIWrite, 60, 60},
		{"Default 600/min", l.Default, 60, 600},
	}
	for _, tc := range cases {
		if tc.rl.windowSecs != tc.window {
			t.Errorf("%s: windowSecs = %d, want %d", tc.name, tc.rl.windowSecs, tc.window)
		}
		if tc.rl.maxAttempts != tc.max {
			t.Errorf("%s: maxAttempts = %d, want %d", tc.name, tc.rl.maxAttempts, tc.max)
		}
	}
}

func TestMiddleware_AllowsUnderLimit(t *testing.T) {
	rl := NewRateLimiter(10, 60)
	r := gin.New()
	r.Use(rl.Middleware())
	r.GET("/ping", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest("GET", "/ping", nil)
	req.RemoteAddr = "192.0.2.1:12345"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("first request: status = %d, want 200", w.Code)
	}
}

func TestMiddleware_Blocks429WhenExceeded(t *testing.T) {
	rl := NewRateLimiter(2, 60)
	r := gin.New()
	r.Use(rl.Middleware())
	r.POST("/login", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	ip := "198.51.100.7:99"

	// Prime: record 2 fails to exhaust the budget.
	rl.RecordFail("198.51.100.7")
	rl.RecordFail("198.51.100.7")

	req := httptest.NewRequest("POST", "/login", nil)
	req.RemoteAddr = ip
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("exhausted budget: status = %d, want 429", w.Code)
	}
	if retryAfter := w.Header().Get("Retry-After"); retryAfter == "" {
		t.Error("Retry-After header must be present on 429")
	}
}

func TestMiddleware_DoesNotCallRecordFail(t *testing.T) {
	rl := NewRateLimiter(3, 60)
	r := gin.New()
	r.Use(rl.Middleware())
	r.GET("/", func(c *gin.Context) { c.Status(http.StatusUnauthorized) })

	ip := "203.0.113.1"
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = ip + ":" + "80"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("iter %d: middleware should not block — handler returned 401, got %d", i, w.Code)
		}
	}
	// The middleware itself must not have recorded failures.
	if allowed, _ := rl.Check(ip); !allowed {
		t.Error("middleware should not call RecordFail; handler must decide")
	}
}
