package handler

import (
	"net/http"

	"github.com/caddypanel/caddypanel/internal/auth"
	"github.com/caddypanel/caddypanel/internal/config"
	"github.com/caddypanel/caddypanel/internal/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// AuthHandler manages authentication endpoints
type AuthHandler struct {
	db        *gorm.DB
	cfg       *config.Config
	limiter   *auth.RateLimiter
	challenge *auth.ChallengeStore
}

// NewAuthHandler creates a new AuthHandler
func NewAuthHandler(db *gorm.DB, cfg *config.Config, limiter *auth.RateLimiter, challenge *auth.ChallengeStore) *AuthHandler {
	return &AuthHandler{db: db, cfg: cfg, limiter: limiter, challenge: challenge}
}

type loginRequest struct {
	Username     string `json:"username" binding:"required"`
	Password     string `json:"password" binding:"required"`
	ChallengeToken string `json:"challenge_token"`
	SliderValue  int    `json:"slider_value"`
}

type setupRequest struct {
	Username string `json:"username" binding:"required,min=3"`
	Password string `json:"password" binding:"required,min=6"`
}

// Challenge generates a new slider challenge
func (h *AuthHandler) Challenge(c *gin.Context) {
	ch := h.challenge.Generate()
	c.JSON(http.StatusOK, gin.H{
		"token":  ch.Token,
		"target": ch.Target,
	})
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
			"error":        "Too many login attempts",
			"retry_after":  waitSec,
		})
		return
	}

	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Verify slider challenge
	if !h.challenge.Verify(req.ChallengeToken, req.SliderValue) {
		h.limiter.RecordFail(ip)
		c.JSON(http.StatusBadRequest, gin.H{"error": "验证失败，请重新滑动验证"})
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
