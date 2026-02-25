package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/web-casa/webcasa/internal/auth"
	"github.com/web-casa/webcasa/internal/config"
	"github.com/web-casa/webcasa/internal/model"
	"github.com/gin-gonic/gin"
	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const testJWTSecret = "test-jwt-secret-for-totp-testing"

// setupTOTPTestDB creates an isolated in-memory SQLite database for TOTP testing
func setupTOTPTestDB(t *testing.T) (*TOTPService, *config.Config) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { sqlDB.Close() })
	db.AutoMigrate(
		&model.User{},
		&model.Host{},
		&model.Upstream{},
		&model.Route{},
		&model.CustomHeader{},
		&model.AccessRule{},
		&model.BasicAuth{},
		&model.AuditLog{},
		&model.Setting{},
		&model.Group{},
		&model.Tag{},
		&model.HostTag{},
	)

	cfg := &config.Config{
		JWTSecret: testJWTSecret,
	}
	svc := NewTOTPService(db, cfg)
	return svc, cfg
}

// createTestUser creates a test user in the database
func createTestUser(t *testing.T, svc *TOTPService, username, password string) *model.User {
	t.Helper()
	hashed, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}
	user := &model.User{
		Username: username,
		Password: hashed,
		Role:     "admin",
	}
	if err := svc.db.Create(user).Error; err != nil {
		t.Fatalf("failed to create test user: %v", err)
	}
	return user
}

// decryptTestSecret is a helper to decrypt the stored TOTP secret for test verification
func decryptTestSecret(t *testing.T, encryptedSecret string) string {
	t.Helper()
	aesKey := deriveAESKey(testJWTSecret)
	plaintext, err := decryptAESGCM(encryptedSecret, aesKey)
	if err != nil {
		t.Fatalf("failed to decrypt TOTP secret: %v", err)
	}
	return string(plaintext)
}

// Feature: phase6-enhancements, Property 4: TOTP 生成与验证 round-trip — generated TOTP code
// should pass verification.
// **Validates: Requirements 4.1, 4.3**
func TestProperty4_TOTPGenerateVerifyRoundTrip(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	properties.Property("TOTP generate and verify round-trip", prop.ForAll(
		func(suffix int) bool {
			svc, _ := setupTOTPTestDB(t)
			username := fmt.Sprintf("user-%d", suffix)
			user := createTestUser(t, svc, username, "password123")

			// Generate secret
			uri, err := svc.GenerateSecret(user.ID)
			if err != nil {
				t.Logf("GenerateSecret failed: %v", err)
				return false
			}
			if uri == "" || !strings.HasPrefix(uri, "otpauth://") {
				t.Logf("Invalid otpauth URI: %s", uri)
				return false
			}

			// Retrieve the stored encrypted secret and decrypt it
			var updatedUser model.User
			svc.db.First(&updatedUser, user.ID)
			secret := decryptTestSecret(t, updatedUser.TOTPSecret)

			// Generate a valid TOTP code using the secret
			code, err := totp.GenerateCode(secret, time.Now())
			if err != nil {
				t.Logf("GenerateCode failed: %v", err)
				return false
			}

			// Verify and enable 2FA with the valid code
			codes, err := svc.VerifyAndEnable(user.ID, code)
			if err != nil {
				t.Logf("VerifyAndEnable failed: %v", err)
				return false
			}

			return len(codes) == 8
		},
		gen.IntRange(1, 99999),
	))

	properties.TestingRun(t)
}

// Feature: phase6-enhancements, Property 5: 恢复码生成格式与数量 — exactly 8 codes, each 8
// alphanumeric chars, bcrypt hashes match.
// **Validates: Requirements 4.4**
func TestProperty5_RecoveryCodeFormatAndCount(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	alphanumRegex := regexp.MustCompile(`^[a-z0-9]{8}$`)

	properties.Property("recovery codes: 8 codes, 8 alphanumeric chars, bcrypt match", prop.ForAll(
		func(suffix int) bool {
			svc, _ := setupTOTPTestDB(t)
			username := fmt.Sprintf("user-%d", suffix)
			user := createTestUser(t, svc, username, "password123")

			// Generate and enable 2FA
			svc.GenerateSecret(user.ID)
			var updatedUser model.User
			svc.db.First(&updatedUser, user.ID)
			secret := decryptTestSecret(t, updatedUser.TOTPSecret)
			code, _ := totp.GenerateCode(secret, time.Now())
			plainCodes, err := svc.VerifyAndEnable(user.ID, code)
			if err != nil {
				t.Logf("VerifyAndEnable failed: %v", err)
				return false
			}

			// Check exactly 8 codes
			if len(plainCodes) != 8 {
				t.Logf("Expected 8 codes, got %d", len(plainCodes))
				return false
			}

			// Check each code is 8 alphanumeric chars
			for _, pc := range plainCodes {
				if !alphanumRegex.MatchString(pc) {
					t.Logf("Code %q doesn't match pattern", pc)
					return false
				}
			}

			// Verify bcrypt hashes match
			svc.db.First(&updatedUser, user.ID)
			var entries []RecoveryCodeEntry
			if err := json.Unmarshal([]byte(updatedUser.RecoveryCodes), &entries); err != nil {
				t.Logf("Failed to unmarshal recovery codes: %v", err)
				return false
			}
			if len(entries) != 8 {
				return false
			}
			for i, entry := range entries {
				if entry.Used {
					return false
				}
				if bcrypt.CompareHashAndPassword([]byte(entry.Hash), []byte(plainCodes[i])) != nil {
					t.Logf("bcrypt hash mismatch for code %d", i)
					return false
				}
			}

			return true
		},
		gen.IntRange(1, 99999),
	))

	properties.TestingRun(t)
}

// setupTestRouter creates a Gin router with auth endpoints for integration testing
func setupTestRouter(t *testing.T, svc *TOTPService, cfg *config.Config) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()

	// Simulate login endpoint that checks 2FA state
	r.POST("/api/auth/login", func(c *gin.Context) {
		var req struct {
			Username  string `json:"username"`
			Password  string `json:"password"`
			TOTPCode  string `json:"totp_code"`
			TempToken string `json:"temp_token"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Handle temp_token flow
		if req.TempToken != "" {
			claims, err := auth.ParseToken(req.TempToken, cfg.JWTSecret)
			if err != nil || !claims.Pending2FA {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid temp token"})
				return
			}
			if req.TOTPCode == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "TOTP code required"})
				return
			}
			valid, err := svc.ValidateLogin(claims.UserID, req.TOTPCode)
			if err != nil || !valid {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid TOTP code", "error_key": "error.invalid_totp"})
				return
			}
			token, _ := auth.GenerateToken(claims.UserID, claims.Username, cfg.JWTSecret)
			c.JSON(http.StatusOK, gin.H{"token": token})
			return
		}

		var user model.User
		if err := svc.db.Where("username = ?", req.Username).First(&user).Error; err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
			return
		}
		if !auth.CheckPassword(user.Password, req.Password) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
			return
		}

		// Check 2FA
		if user.TOTPEnabled != nil && *user.TOTPEnabled {
			if req.TOTPCode == "" {
				tempToken, _ := auth.GenerateTempToken(user.ID, user.Username, cfg.JWTSecret)
				c.JSON(http.StatusOK, gin.H{"requires_2fa": true, "temp_token": tempToken})
				return
			}
			valid, err := svc.ValidateLogin(user.ID, req.TOTPCode)
			if err != nil || !valid {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid TOTP code", "error_key": "error.invalid_totp"})
				return
			}
		}

		token, _ := auth.GenerateToken(user.ID, user.Username, cfg.JWTSecret)
		c.JSON(http.StatusOK, gin.H{"token": token})
	})

	return r
}

// Feature: phase6-enhancements, Property 6: 登录流程依赖 2FA 状态 — with 2FA enabled,
// password-only login returns requires_2fa; without 2FA, password login gets JWT.
// **Validates: Requirements 4.5, 4.10**
func TestProperty6_LoginFlowDependsOn2FAState(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	properties.Property("login flow depends on 2FA state", prop.ForAll(
		func(suffix int) bool {
			svc, cfg := setupTOTPTestDB(t)
			username := fmt.Sprintf("user-%d", suffix)
			password := "password123"
			user := createTestUser(t, svc, username, password)
			router := setupTestRouter(t, svc, cfg)

			// Without 2FA: password login should get JWT directly
			body := fmt.Sprintf(`{"username":"%s","password":"%s"}`, username, password)
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("POST", "/api/auth/login", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Logf("Expected 200, got %d: %s", w.Code, w.Body.String())
				return false
			}
			var resp map[string]interface{}
			json.Unmarshal(w.Body.Bytes(), &resp)
			if _, hasToken := resp["token"]; !hasToken {
				t.Logf("Expected token in response without 2FA")
				return false
			}
			if _, has2FA := resp["requires_2fa"]; has2FA {
				t.Logf("Should not have requires_2fa without 2FA enabled")
				return false
			}

			// Enable 2FA
			svc.GenerateSecret(user.ID)
			var updatedUser model.User
			svc.db.First(&updatedUser, user.ID)
			secret := decryptTestSecret(t, updatedUser.TOTPSecret)
			code, _ := totp.GenerateCode(secret, time.Now())
			svc.VerifyAndEnable(user.ID, code)

			// With 2FA: password-only login should return requires_2fa
			w = httptest.NewRecorder()
			req, _ = http.NewRequest("POST", "/api/auth/login", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Logf("Expected 200, got %d: %s", w.Code, w.Body.String())
				return false
			}
			json.Unmarshal(w.Body.Bytes(), &resp)
			requires2FA, ok := resp["requires_2fa"]
			if !ok || requires2FA != true {
				t.Logf("Expected requires_2fa=true, got %v", resp)
				return false
			}
			if _, hasTempToken := resp["temp_token"]; !hasTempToken {
				t.Logf("Expected temp_token in 2FA response")
				return false
			}

			return true
		},
		gen.IntRange(1, 99999),
	))

	properties.TestingRun(t)
}

// enableTestUser2FA is a helper that enables 2FA for a user and returns the plaintext recovery codes and TOTP secret
func enableTestUser2FA(t *testing.T, svc *TOTPService, userID uint) ([]string, string) {
	t.Helper()
	svc.GenerateSecret(userID)
	var user model.User
	svc.db.First(&user, userID)
	secret := decryptTestSecret(t, user.TOTPSecret)
	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("GenerateCode failed: %v", err)
	}
	codes, err := svc.VerifyAndEnable(userID, code)
	if err != nil {
		t.Fatalf("VerifyAndEnable failed: %v", err)
	}
	return codes, secret
}

// Feature: phase6-enhancements, Property 7: 恢复码一次性使用 — first use succeeds, second use
// of same code fails.
// **Validates: Requirements 4.6**
func TestProperty7_RecoveryCodeOneTimeUse(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	properties.Property("recovery code one-time use", prop.ForAll(
		func(suffix int, codeIndex int) bool {
			svc, _ := setupTOTPTestDB(t)
			username := fmt.Sprintf("user-%d", suffix)
			user := createTestUser(t, svc, username, "password123")
			codes, _ := enableTestUser2FA(t, svc, user.ID)

			// Pick a recovery code
			idx := codeIndex % len(codes)
			recoveryCode := codes[idx]

			// First use should succeed
			valid, err := svc.ValidateLogin(user.ID, recoveryCode)
			if err != nil || !valid {
				t.Logf("First use of recovery code failed: err=%v, valid=%v", err, valid)
				return false
			}

			// Second use of same code should fail
			valid, err = svc.ValidateLogin(user.ID, recoveryCode)
			if err != nil {
				t.Logf("Unexpected error on second use: %v", err)
				return false
			}
			if valid {
				t.Logf("Recovery code should not be valid on second use")
				return false
			}

			return true
		},
		gen.IntRange(1, 99999),
		gen.IntRange(0, 7),
	))

	properties.TestingRun(t)
}

// Feature: phase6-enhancements, Property 8: 无效验证码拒绝登录 — random invalid strings
// rejected in 2FA verification.
// **Validates: Requirements 4.7**
func TestProperty8_InvalidCodeRejected(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	properties.Property("invalid codes rejected", prop.ForAll(
		func(suffix int, invalidCode string) bool {
			svc, _ := setupTOTPTestDB(t)
			username := fmt.Sprintf("user-%d", suffix)
			user := createTestUser(t, svc, username, "password123")
			enableTestUser2FA(t, svc, user.ID)

			// Try with random invalid string
			valid, err := svc.ValidateLogin(user.ID, invalidCode)
			if err != nil {
				// Errors are acceptable (e.g., parse errors)
				return true
			}
			// Should not be valid
			return !valid
		},
		gen.IntRange(1, 99999),
		gen.RegexMatch(`[a-zA-Z0-9!@#$%]{3,12}`),
	))

	properties.TestingRun(t)
}

// Feature: phase6-enhancements, Property 9: 2FA 禁用 round-trip — after disabling 2FA with
// valid TOTP, login no longer requires 2FA.
// **Validates: Requirements 4.8**
func TestProperty9_Disable2FARoundTrip(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	properties.Property("2FA disable round-trip", prop.ForAll(
		func(suffix int) bool {
			svc, cfg := setupTOTPTestDB(t)
			username := fmt.Sprintf("user-%d", suffix)
			password := "password123"
			user := createTestUser(t, svc, username, password)
			_, secret := enableTestUser2FA(t, svc, user.ID)
			router := setupTestRouter(t, svc, cfg)

			// Verify 2FA is required
			body := fmt.Sprintf(`{"username":"%s","password":"%s"}`, username, password)
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("POST", "/api/auth/login", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(w, req)

			var resp map[string]interface{}
			json.Unmarshal(w.Body.Bytes(), &resp)
			if resp["requires_2fa"] != true {
				t.Logf("Expected requires_2fa before disable, got: %v", resp)
				return false
			}

			// Disable 2FA with valid TOTP code
			disableCode, _ := totp.GenerateCode(secret, time.Now())
			err := svc.Disable(user.ID, disableCode)
			if err != nil {
				t.Logf("Disable failed: %v", err)
				return false
			}

			// Verify DB state after disable
			var dbUser model.User
			svc.db.First(&dbUser, user.ID)
			if dbUser.TOTPEnabled != nil && *dbUser.TOTPEnabled {
				t.Logf("DB still shows 2FA enabled after Disable call")
				return false
			}

			// Login should no longer require 2FA
			w = httptest.NewRecorder()
			req, _ = http.NewRequest("POST", "/api/auth/login", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Logf("Expected 200 after disable, got %d", w.Code)
				return false
			}
			resp = make(map[string]interface{})
			json.Unmarshal(w.Body.Bytes(), &resp)
			if _, has2FA := resp["requires_2fa"]; has2FA {
				t.Logf("Should not require 2FA after disable, resp=%v", resp)
				return false
			}
			if _, hasToken := resp["token"]; !hasToken {
				t.Logf("Expected token after 2FA disabled, resp=%v", resp)
				return false
			}

			return true
		},
		gen.IntRange(1, 99999),
	))

	properties.TestingRun(t)
}
