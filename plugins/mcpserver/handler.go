package mcpserver

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// Handler provides REST endpoints for API token management.
type Handler struct {
	tokenSvc *TokenService
}

// NewHandler creates a new Handler.
func NewHandler(tokenSvc *TokenService) *Handler {
	return &Handler{tokenSvc: tokenSvc}
}

// ListTokens returns all API tokens for the current user.
func (h *Handler) ListTokens(c *gin.Context) {
	userID := c.GetUint("user_id")
	tokens, err := h.tokenSvc.ListTokens(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, tokens)
}

// CreateToken generates a new API token.
func (h *Handler) CreateToken(c *gin.Context) {
	userID := c.GetUint("user_id")

	var req struct {
		Name        string `json:"name" binding:"required"`
		Permissions string `json:"permissions"` // JSON array
		ExpiresIn   int    `json:"expires_in"`  // days, 0 = never
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}

	var expiresAt *time.Time
	if req.ExpiresIn > 0 {
		t := time.Now().AddDate(0, 0, req.ExpiresIn)
		expiresAt = &t
	}

	token, plaintext, err := h.tokenSvc.CreateToken(userID, req.Name, req.Permissions, expiresAt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token":     plaintext, // shown only once
		"id":        token.ID,
		"name":      token.Name,
		"prefix":    token.Prefix,
		"expires_at": token.ExpiresAt,
		"created_at": token.CreatedAt,
	})
}

// DeleteToken revokes an API token.
func (h *Handler) DeleteToken(c *gin.Context) {
	userID := c.GetUint("user_id")
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid token ID"})
		return
	}

	if err := h.tokenSvc.DeleteToken(uint(id), userID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Token revoked"})
}
