package handler

import (
	"fmt"
	"net/http"

	"github.com/caddypanel/caddypanel/internal/auth"
	"github.com/caddypanel/caddypanel/internal/config"
	"github.com/caddypanel/caddypanel/internal/model"
	"github.com/caddypanel/caddypanel/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// AuthHandler manages authentication endpoints
type AuthHandler struct {
	db       *gorm.DB
	cfg      *config.Config
	limiter  *auth.RateLimiter
	totpSvc  *service.TOTPService
}

// NewAuthHandler creates a new AuthHandler
func NewAuthHandler(db *gorm.DB, cfg *config.Config, limiter *auth.RateLimiter, totpSvc *service.TOTPService) *AuthHandler {
	return &AuthHandler{db: db, cfg: cfg, limiter: limiter, totpSvc: totpSvc}
}

type loginRequest struct {
	Username  string `json:"username" binding:"required"`
	Password  string `json:"password" binding:"required"`
	Altcha    string `json:"altcha"`
	TOTPCode  string `json:"totp_code"`
	TempToken string `json:"temp_token"`
}

type setupRequest struct {
	Username string `json:"username" binding:"required,min=3"`
	Password string `json:"password" binding:"required,min=6"`
}

// AltchaChallenge generates a new ALTCHA PoW challenge
func (h *AuthHandler) AltchaChallenge(c *gin.Context) {
	ch, err := auth.GenerateAltchaChallenge(h.cfg.JWTSecret)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate challenge"})
		return
	}
	c.JSON(http.StatusOK, ch)
}

// Setup creates the initial admin user (only works when no users exist)
func (h *AuthHandler) Setup(c *gin.Context) {
	var count int64
	h.db.Model(&model.User{}).Count(&count)
	if count > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Admin user already exists"})
		return
	}

	var req setupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	hashed, err := auth.HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
		return
	}

	user := model.User{
		Username: req.Username,
		Password: hashed,
	}

	if err := h.db.Create(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user"})
		return
	}

	token, _ := auth.GenerateToken(user.ID, user.Username, h.cfg.JWTSecret)
	c.JSON(http.StatusOK, gin.H{
		"message": "Admin user created successfully",
		"token":   token,
		"user": gin.H{
			"id":       user.ID,
			"username": user.Username,
		},
	})
}

// Login authenticates a user and returns a JWT token
func (h *AuthHandler) Login(c *gin.Context) {
	ip := c.ClientIP()

	// Rate limit check
	allowed, waitSec := h.limiter.Check(ip)
	if !allowed {
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error":       "Too many login attempts",
			"retry_after": waitSec,
		})
		return
	}

	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Handle 2FA verification with temp_token
	if req.TempToken != "" {
		h.handleTempTokenLogin(c, ip, req)
		return
	}

	// Verify ALTCHA PoW challenge
	if req.Altcha == "" {
		h.limiter.RecordFail(ip)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Please complete security verification first"})
		return
	}
	ok, err := auth.VerifyAltchaSolution(req.Altcha, h.cfg.JWTSecret)
	if err != nil || !ok {
		h.limiter.RecordFail(ip)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Verification failed, please try again"})
		return
	}

	var user model.User
	if err := h.db.Where("username = ?", req.Username).First(&user).Error; err != nil {
		h.limiter.RecordFail(ip)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	if !auth.CheckPassword(user.Password, req.Password) {
		h.limiter.RecordFail(ip)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	// Check if 2FA is enabled
	if user.TOTPEnabled != nil && *user.TOTPEnabled {
		if req.TOTPCode == "" {
			// 2FA enabled but no code provided — return temp token
			tempToken, err := auth.GenerateTempToken(user.ID, user.Username, h.cfg.JWTSecret)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate temp token"})
				return
			}
			h.limiter.RecordSuccess(ip)
			c.JSON(http.StatusOK, gin.H{
				"requires_2fa": true,
				"temp_token":   tempToken,
			})
			return
		}

		// 2FA enabled and code provided — validate
		valid, err := h.totpSvc.ValidateLogin(user.ID, req.TOTPCode)
		if err != nil || !valid {
			h.limiter.RecordFail(ip)
			c.JSON(http.StatusUnauthorized, gin.H{
				"error":     "Invalid TOTP code",
				"error_key": "error.invalid_totp",
			})
			return
		}
	}

	token, err := auth.GenerateToken(user.ID, user.Username, h.cfg.JWTSecret)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
		return
	}

	h.limiter.RecordSuccess(ip)
	c.JSON(http.StatusOK, gin.H{
		"token": token,
		"user": gin.H{
			"id":       user.ID,
			"username": user.Username,
		},
	})
}

// handleTempTokenLogin handles the second step of 2FA login using a temp token
func (h *AuthHandler) handleTempTokenLogin(c *gin.Context, ip string, req loginRequest) {
	// Parse and validate the temp token
	claims, err := auth.ParseToken(req.TempToken, h.cfg.JWTSecret)
	if err != nil {
		h.limiter.RecordFail(ip)
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":     "Temp token expired or invalid",
			"error_key": "error.temp_token_expired",
		})
		return
	}

	if !claims.Pending2FA {
		h.limiter.RecordFail(ip)
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":     "Invalid token type",
			"error_key": "error.invalid_token",
		})
		return
	}

	if req.TOTPCode == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":     "TOTP code is required",
			"error_key": "error.totp_required",
		})
		return
	}

	// Validate the TOTP code or recovery code
	valid, err := h.totpSvc.ValidateLogin(claims.UserID, req.TOTPCode)
	if err != nil || !valid {
		h.limiter.RecordFail(ip)
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":     "Invalid TOTP code",
			"error_key": "error.invalid_totp",
		})
		return
	}

	// Issue full JWT
	token, err := auth.GenerateToken(claims.UserID, claims.Username, h.cfg.JWTSecret)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
		return
	}

	h.limiter.RecordSuccess(ip)
	c.JSON(http.StatusOK, gin.H{
		"token": token,
		"user": gin.H{
			"id":       claims.UserID,
			"username": claims.Username,
		},
	})
}

// Me returns the current authenticated user info
func (h *AuthHandler) Me(c *gin.Context) {
	userID, _ := c.Get("user_id")
	username, _ := c.Get("username")

	c.JSON(http.StatusOK, gin.H{
		"id":       userID,
		"username": username,
	})
}

// NeedSetup checks if initial setup is required
func (h *AuthHandler) NeedSetup(c *gin.Context) {
	var count int64
	h.db.Model(&model.User{}).Count(&count)
	c.JSON(http.StatusOK, gin.H{"need_setup": count == 0})
}

func (h *AuthHandler) audit(c *gin.Context, action, detail string) {
	if uid, ok := c.Get("user_id"); ok {
		uname, _ := c.Get("username")
		WriteAuditLog(h.db, uid.(uint), fmt.Sprint(uname), action, "user", fmt.Sprint(uid), detail, c.ClientIP())
	}
}

// Setup2FA generates a TOTP secret and returns the otpauth URI
func (h *AuthHandler) Setup2FA(c *gin.Context) {
	userID, _ := c.Get("user_id")

	uri, err := h.totpSvc.GenerateSecret(userID.(uint))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":     err.Error(),
			"error_key": "error.2fa_setup_failed",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"otpauth_uri": uri,
	})
}

// Verify2FA verifies a TOTP code and enables 2FA, returning recovery codes
func (h *AuthHandler) Verify2FA(c *gin.Context) {
	userID, _ := c.Get("user_id")

	var req struct {
		Code string `json:"code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "error_key": "error.invalid_request"})
		return
	}

	codes, err := h.totpSvc.VerifyAndEnable(userID.(uint), req.Code)
	if err != nil {
		errMsg := err.Error()
		errKey := "error.2fa_verify_failed"
		if errMsg == "error.invalid_totp" {
			errKey = "error.invalid_totp"
		}
		c.JSON(http.StatusBadRequest, gin.H{
			"error":     errMsg,
			"error_key": errKey,
		})
		return
	}

	h.audit(c, "ENABLE_2FA", "Enabled 2FA")
	c.JSON(http.StatusOK, gin.H{
		"message":        "2FA enabled successfully",
		"recovery_codes": codes,
	})
}

// Disable2FA verifies a TOTP code and disables 2FA
func (h *AuthHandler) Disable2FA(c *gin.Context) {
	userID, _ := c.Get("user_id")

	var req struct {
		Code string `json:"code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "error_key": "error.invalid_request"})
		return
	}

	if err := h.totpSvc.Disable(userID.(uint), req.Code); err != nil {
		errMsg := err.Error()
		errKey := "error.2fa_disable_failed"
		switch errMsg {
		case "error.invalid_totp":
			errKey = "error.invalid_totp"
		case "error.2fa_not_enabled":
			errKey = "error.2fa_not_enabled"
		}
		c.JSON(http.StatusBadRequest, gin.H{
			"error":     errMsg,
			"error_key": errKey,
		})
		return
	}

	h.audit(c, "DISABLE_2FA", "Disabled 2FA")
	c.JSON(http.StatusOK, gin.H{
		"message": "2FA disabled successfully",
	})
}
