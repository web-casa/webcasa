package auth

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// Claims defines JWT token claims
type Claims struct {
	UserID     uint   `json:"user_id"`
	Username   string `json:"username"`
	Pending2FA bool   `json:"pending_2fa,omitempty"`
	jwt.RegisteredClaims
}

// HashPassword hashes a plaintext password with bcrypt
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

// CheckPassword compares a bcrypt hashed password with a plaintext password
func CheckPassword(hashedPassword, password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
	return err == nil
}

// GenerateToken creates a JWT token for a given user
func GenerateToken(userID uint, username, secret string) (string, error) {
	claims := Claims{
		UserID:   userID,
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "webcasa",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// GenerateTempToken creates a short-lived JWT (5 min) with pending_2fa flag
func GenerateTempToken(userID uint, username, secret string) (string, error) {
	claims := Claims{
		UserID:     userID,
		Username:   username,
		Pending2FA: true,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(5 * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "webcasa",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// ParseToken validates and parses a JWT token
func ParseToken(tokenString, secret string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	return nil, jwt.ErrSignatureInvalid
}

// Middleware returns a Gin middleware that validates JWT tokens.
// It checks the Authorization header first, then falls back to the "token"
// query parameter ONLY for WebSocket upgrade requests (browsers cannot set
// custom headers on WebSocket connections).
func Middleware(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var tokenStr string

		// 1. Try Authorization header first.
		authHeader := c.GetHeader("Authorization")
		if authHeader != "" {
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) == 2 && strings.ToLower(parts[0]) == "bearer" {
				tokenStr = parts[1]
			}
		}

		// 2. Fall back to query parameter ONLY for WebSocket upgrades.
		if tokenStr == "" && isWebSocketUpgrade(c.Request) {
			tokenStr = c.Query("token")
		}

		if tokenStr == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization required"})
			c.Abort()
			return
		}

		claims, err := ParseToken(tokenStr, secret)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired token"})
			c.Abort()
			return
		}

		// Store user info in context
		c.Set("user_id", claims.UserID)
		c.Set("username", claims.Username)

		// Reject pending_2fa tokens from accessing protected routes
		if claims.Pending2FA {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "2FA verification required", "error_key": "error.2fa_required"})
			c.Abort()
			return
		}

		c.Next()
	}
}

// isWebSocketUpgrade checks if the request is a WebSocket upgrade handshake.
func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") &&
		strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}
