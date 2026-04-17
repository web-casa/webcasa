package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/web-casa/webcasa/internal/auth"
	"github.com/web-casa/webcasa/internal/config"
	"github.com/web-casa/webcasa/internal/model"
	"github.com/web-casa/webcasa/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupAuthLimiterTest(t *testing.T) (*gin.Engine, *AuthHandler) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	cfg := &config.Config{JWTSecret: "test-secret-do-not-use"}
	limiters := auth.NewLimiters()
	totpSvc := service.NewTOTPService(db, cfg)
	h := NewAuthHandler(db, cfg, limiters, totpSvc)

	r := gin.New()
	r.POST("/login", h.Login)
	return r, h
}

// TestLogin_TempTokenPath_NotBlockedByLoginLimiter is a regression test for
// the Codex-flagged MEDIUM finding: the temp-token (2FA second step) flow
// must NOT be gated by limiters.Login. Previously, an exhausted Login
// budget on the IP would 429 even on legitimate 2FA retries that were
// supposed to be governed by limiters.TOTP only.
func TestLogin_TempTokenPath_NotBlockedByLoginLimiter(t *testing.T) {
	r, h := setupAuthLimiterTest(t)

	// Exhaust the Login bucket for the test IP.
	for i := 0; i < 6; i++ {
		h.limiters.Login.RecordFail("192.0.2.1")
	}
	if allowed, _ := h.limiters.Login.Check("192.0.2.1"); allowed {
		t.Fatal("setup failure: Login bucket should be exhausted")
	}

	// Send a temp-token login request from the same IP. Even though the
	// Login bucket is exhausted, the request must reach handleTempTokenLogin
	// and be evaluated against the TOTP bucket — not summarily 429'd.
	body, _ := json.Marshal(map[string]string{
		"username":   "ignored",
		"password":   "ignored",
		"temp_token": "fake-but-non-empty",
		"totp_code":  "123456",
	})
	req := httptest.NewRequest("POST", "/login", bytes.NewReader(body))
	req.RemoteAddr = "192.0.2.1:1111"
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Expect 401 (invalid temp token) — NOT 429 (rate limited). 401 means
	// the request was actually evaluated; the temp token is rejected on
	// merits, not turned away at the gate.
	if w.Code == http.StatusTooManyRequests {
		t.Fatalf("temp-token request was 429'd by Login limiter; should have been routed to TOTP path. body=%s", w.Body.String())
	}
	if w.Code != http.StatusUnauthorized && w.Code != http.StatusBadRequest {
		t.Fatalf("expected 401/400 (token rejected on merits), got %d: %s", w.Code, w.Body.String())
	}
}

// TestLogin_PrimaryPath_StillBlockedByLoginLimiter verifies the inverse:
// primary credential requests are still gated by the Login bucket.
func TestLogin_PrimaryPath_StillBlockedByLoginLimiter(t *testing.T) {
	r, h := setupAuthLimiterTest(t)

	for i := 0; i < 6; i++ {
		h.limiters.Login.RecordFail("198.51.100.5")
	}

	body, _ := json.Marshal(map[string]string{
		"username": "anyone",
		"password": "whatever",
	})
	req := httptest.NewRequest("POST", "/login", bytes.NewReader(body))
	req.RemoteAddr = "198.51.100.5:2222"
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("primary login should be 429 when Login bucket exhausted, got %d: %s", w.Code, w.Body.String())
	}
}
