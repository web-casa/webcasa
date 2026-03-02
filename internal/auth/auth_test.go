package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/web-casa/webcasa/internal/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const testSecret = "test-secret-key-for-unit-tests"

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// setupTestDB creates an in-memory SQLite database with the User table.
func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open in-memory sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatalf("failed to auto-migrate User: %v", err)
	}
	return db
}

// createTestUser inserts a user into the database and returns it.
func createTestUser(t *testing.T, db *gorm.DB, username, role string) model.User {
	t.Helper()
	hashed, err := HashPassword("password123")
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}
	user := model.User{
		Username: username,
		Password: hashed,
		Role:     role,
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("failed to create test user: %v", err)
	}
	return user
}

// ginEngine creates a minimal *gin.Engine with the given middlewares and a
// terminal handler that echoes user_id and username from context.
func ginEngine(middlewares ...gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	group := r.Group("/test")
	for _, m := range middlewares {
		group.Use(m)
	}
	group.GET("/protected", func(c *gin.Context) {
		uid, _ := c.Get("user_id")
		uname, _ := c.Get("username")
		c.JSON(http.StatusOK, gin.H{
			"user_id":  uid,
			"username": uname,
		})
	})
	return r
}

// jsonBody parses the JSON response body into a map.
func jsonBody(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to unmarshal response body: %v", err)
	}
	return body
}

// ---------------------------------------------------------------------------
// JWT Token Tests
// ---------------------------------------------------------------------------

func TestGenerateAndParseToken(t *testing.T) {
	tests := []struct {
		name     string
		userID   uint
		username string
	}{
		{"basic user", 1, "admin"},
		{"large ID", 99999, "somebody"},
		{"unicode username", 42, "用户名"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokenStr, err := GenerateToken(tt.userID, tt.username, testSecret)
			if err != nil {
				t.Fatalf("GenerateToken() error = %v", err)
			}
			if tokenStr == "" {
				t.Fatal("GenerateToken() returned empty string")
			}

			claims, err := ParseToken(tokenStr, testSecret)
			if err != nil {
				t.Fatalf("ParseToken() error = %v", err)
			}
			if claims.UserID != tt.userID {
				t.Errorf("UserID = %d, want %d", claims.UserID, tt.userID)
			}
			if claims.Username != tt.username {
				t.Errorf("Username = %q, want %q", claims.Username, tt.username)
			}
			if claims.Pending2FA {
				t.Error("Pending2FA should be false for normal tokens")
			}
			if claims.Issuer != "webcasa" {
				t.Errorf("Issuer = %q, want %q", claims.Issuer, "webcasa")
			}
		})
	}
}

func TestParseToken_Expired(t *testing.T) {
	// Create a token that expired 1 hour ago.
	claims := Claims{
		UserID:   1,
		Username: "admin",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
			Issuer:    "webcasa",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}

	_, err = ParseToken(tokenStr, testSecret)
	if err == nil {
		t.Fatal("ParseToken() should return error for expired token")
	}
}

func TestParseToken_InvalidSignature(t *testing.T) {
	tokenStr, err := GenerateToken(1, "admin", testSecret)
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	// Parse with a different secret.
	_, err = ParseToken(tokenStr, "wrong-secret-key")
	if err == nil {
		t.Fatal("ParseToken() should return error for invalid signature")
	}
}

func TestGenerateTempToken(t *testing.T) {
	tokenStr, err := GenerateTempToken(7, "twofactor_user", testSecret)
	if err != nil {
		t.Fatalf("GenerateTempToken() error = %v", err)
	}
	if tokenStr == "" {
		t.Fatal("GenerateTempToken() returned empty string")
	}

	claims, err := ParseToken(tokenStr, testSecret)
	if err != nil {
		t.Fatalf("ParseToken() error = %v", err)
	}

	if claims.UserID != 7 {
		t.Errorf("UserID = %d, want 7", claims.UserID)
	}
	if claims.Username != "twofactor_user" {
		t.Errorf("Username = %q, want %q", claims.Username, "twofactor_user")
	}
	if !claims.Pending2FA {
		t.Error("Pending2FA should be true for temp tokens")
	}

	// Expiry should be approximately 5 minutes from now (allow +/- 10 seconds).
	expiry := claims.ExpiresAt.Time
	expected := time.Now().Add(5 * time.Minute)
	diff := expiry.Sub(expected)
	if diff < -10*time.Second || diff > 10*time.Second {
		t.Errorf("expiry drift = %v, want within +/-10s of 5 minutes", diff)
	}
}

// ---------------------------------------------------------------------------
// Password Tests
// ---------------------------------------------------------------------------

func TestHashAndCheckPassword(t *testing.T) {
	passwords := []string{
		"simple",
		"P@$$w0rd!2026",
		"中文密码测试",
		"a",
		"very-long-password-that-keeps-going-and-going-and-going-1234567890",
	}

	for _, pw := range passwords {
		t.Run(pw, func(t *testing.T) {
			hashed, err := HashPassword(pw)
			if err != nil {
				t.Fatalf("HashPassword(%q) error = %v", pw, err)
			}
			if hashed == pw {
				t.Fatal("HashPassword() returned the plaintext password")
			}
			if !CheckPassword(hashed, pw) {
				t.Errorf("CheckPassword() returned false for correct password %q", pw)
			}
		})
	}
}

func TestCheckPassword_Wrong(t *testing.T) {
	hashed, err := HashPassword("correct-password")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	wrongPasswords := []string{
		"wrong-password",
		"correct-passwor",   // one char short
		"correct-password ", // trailing space
		"",
	}

	for _, wrong := range wrongPasswords {
		t.Run(wrong, func(t *testing.T) {
			if CheckPassword(hashed, wrong) {
				t.Errorf("CheckPassword() returned true for wrong password %q", wrong)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// WebSocket Detection Tests
// ---------------------------------------------------------------------------

func TestIsWebSocketUpgrade(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")

	if !isWebSocketUpgrade(req) {
		t.Error("isWebSocketUpgrade() should return true for proper WS headers")
	}
}

func TestIsWebSocketUpgrade_CaseInsensitive(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Header.Set("Upgrade", "WebSocket")
	req.Header.Set("Connection", "keep-alive, Upgrade")

	if !isWebSocketUpgrade(req) {
		t.Error("isWebSocketUpgrade() should handle case-insensitive and multi-value Connection header")
	}
}

func TestIsWebSocketUpgrade_NotWS(t *testing.T) {
	tests := []struct {
		name       string
		upgrade    string
		connection string
	}{
		{"no headers", "", ""},
		{"only upgrade header", "websocket", ""},
		{"only connection header", "", "Upgrade"},
		{"wrong upgrade value", "h2c", "Upgrade"},
		{"normal HTTP request", "", "keep-alive"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.upgrade != "" {
				req.Header.Set("Upgrade", tt.upgrade)
			}
			if tt.connection != "" {
				req.Header.Set("Connection", tt.connection)
			}

			if isWebSocketUpgrade(req) {
				t.Errorf("isWebSocketUpgrade() should return false for %s", tt.name)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Middleware Tests
// ---------------------------------------------------------------------------

func TestMiddleware_NoToken(t *testing.T) {
	engine := ginEngine(Middleware(testSecret))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test/protected", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	body := jsonBody(t, w)
	if body["error"] != "Authorization required" {
		t.Errorf("error = %v, want %q", body["error"], "Authorization required")
	}
}

func TestMiddleware_ValidJWT(t *testing.T) {
	engine := ginEngine(Middleware(testSecret))

	tokenStr, err := GenerateToken(42, "testuser", testSecret)
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test/protected", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	body := jsonBody(t, w)
	// gin serialises uint as float64 in JSON.
	if uid, ok := body["user_id"].(float64); !ok || uint(uid) != 42 {
		t.Errorf("user_id = %v, want 42", body["user_id"])
	}
	if body["username"] != "testuser" {
		t.Errorf("username = %v, want %q", body["username"], "testuser")
	}
}

func TestMiddleware_InvalidJWT(t *testing.T) {
	engine := ginEngine(Middleware(testSecret))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test/protected", nil)
	req.Header.Set("Authorization", "Bearer totally-not-a-jwt")
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	body := jsonBody(t, w)
	if body["error"] != "Invalid or expired token" {
		t.Errorf("error = %v, want %q", body["error"], "Invalid or expired token")
	}
}

func TestMiddleware_Pending2FA(t *testing.T) {
	engine := ginEngine(Middleware(testSecret))

	tokenStr, err := GenerateTempToken(10, "pending_user", testSecret)
	if err != nil {
		t.Fatalf("GenerateTempToken() error = %v", err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test/protected", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}

	body := jsonBody(t, w)
	if body["error_key"] != "error.2fa_required" {
		t.Errorf("error_key = %v, want %q", body["error_key"], "error.2fa_required")
	}
	if body["error"] != "2FA verification required" {
		t.Errorf("error = %v, want %q", body["error"], "2FA verification required")
	}
}

func TestMiddleware_WebSocketToken(t *testing.T) {
	engine := ginEngine(Middleware(testSecret))

	tokenStr, err := GenerateToken(99, "wsuser", testSecret)
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test/protected?token="+tokenStr, nil)
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	body := jsonBody(t, w)
	if uid, ok := body["user_id"].(float64); !ok || uint(uid) != 99 {
		t.Errorf("user_id = %v, want 99", body["user_id"])
	}
	if body["username"] != "wsuser" {
		t.Errorf("username = %v, want %q", body["username"], "wsuser")
	}
}

func TestMiddleware_QueryParamIgnoredForNonWS(t *testing.T) {
	// The ?token= query param should NOT be accepted for non-WebSocket requests.
	engine := ginEngine(Middleware(testSecret))

	tokenStr, err := GenerateToken(1, "admin", testSecret)
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test/protected?token="+tokenStr, nil)
	// No Upgrade/Connection headers — normal HTTP.
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d (query token should be ignored for non-WS)", w.Code, http.StatusUnauthorized)
	}
}

func TestMiddleware_BearerCaseInsensitive(t *testing.T) {
	// The middleware lowercases the scheme; verify "bearer" (lowercase) works.
	engine := ginEngine(Middleware(testSecret))

	tokenStr, err := GenerateToken(1, "admin", testSecret)
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test/protected", nil)
	req.Header.Set("Authorization", "bearer "+tokenStr) // lowercase
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

// ---------------------------------------------------------------------------
// RBAC Tests (RequireAdmin)
// ---------------------------------------------------------------------------

func TestRequireAdmin_AdminUser(t *testing.T) {
	db := setupTestDB(t)
	admin := createTestUser(t, db, "adminuser", "admin")

	engine := ginEngine(
		Middleware(testSecret),
		RequireAdmin(db),
	)

	tokenStr, err := GenerateToken(admin.ID, admin.Username, testSecret)
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test/protected", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestRequireAdmin_ViewerUser(t *testing.T) {
	db := setupTestDB(t)
	viewer := createTestUser(t, db, "vieweruser", "viewer")

	engine := ginEngine(
		Middleware(testSecret),
		RequireAdmin(db),
	)

	tokenStr, err := GenerateToken(viewer.ID, viewer.Username, testSecret)
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test/protected", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}

	body := jsonBody(t, w)
	if body["error"] != "Admin access required" {
		t.Errorf("error = %v, want %q", body["error"], "Admin access required")
	}
}

func TestRequireAdmin_APIToken(t *testing.T) {
	db := setupTestDB(t)

	// Simulate what the auth middleware does when it validates an API token:
	// it sets api_token=true in the context. We replicate that with a shim middleware.
	apiTokenShim := func(c *gin.Context) {
		c.Set("user_id", uint(1))
		c.Set("username", "api-token")
		c.Set("api_token", true)
		c.Next()
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	group := r.Group("/test")
	group.Use(apiTokenShim, RequireAdmin(db))
	group.GET("/protected", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test/protected", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (API token should pass RequireAdmin)", w.Code, http.StatusOK)
	}
}

func TestRequireAdmin_NoUserID(t *testing.T) {
	db := setupTestDB(t)

	// No auth middleware applied — user_id is never set in context.
	gin.SetMode(gin.TestMode)
	r := gin.New()
	group := r.Group("/test")
	group.Use(RequireAdmin(db))
	group.GET("/protected", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test/protected", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}

	body := jsonBody(t, w)
	if body["error"] != "Authorization required" {
		t.Errorf("error = %v, want %q", body["error"], "Authorization required")
	}
}

func TestRequireAdmin_UserNotFound(t *testing.T) {
	db := setupTestDB(t)
	// No users created — ID 999 does not exist.

	engine := ginEngine(
		Middleware(testSecret),
		RequireAdmin(db),
	)

	tokenStr, err := GenerateToken(999, "ghost", testSecret)
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test/protected", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}

	body := jsonBody(t, w)
	if body["error"] != "User not found" {
		t.Errorf("error = %v, want %q", body["error"], "User not found")
	}
}
