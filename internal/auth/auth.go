package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// ctxKeyWSSubprotocol holds the Sec-WebSocket-Protocol value selected by the
// auth middleware, so the WebSocket handler can echo it back during Upgrade.
const ctxKeyWSSubprotocol = "webcasa.ws.subprotocol"

// WebSocketSelectedSubprotocol returns the subprotocol string the auth
// middleware parsed the token from, if any. Returns "" when the request was
// authenticated via Authorization header or ?token= query. Handlers should
// copy this into the response header they pass to gorilla's Upgrade so the
// browser accepts the 101 handshake.
func WebSocketSelectedSubprotocol(c *gin.Context) string {
	if v, ok := c.Get(ctxKeyWSSubprotocol); ok {
		if s, _ := v.(string); s != "" {
			return s
		}
	}
	return ""
}

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

// Middleware returns a Gin middleware that validates JWT tokens or API tokens.
// It checks the Authorization header first, then falls back to the "token"
// query parameter ONLY for WebSocket upgrade requests (browsers cannot set
// custom headers on WebSocket connections).
//
// API tokens (prefixed with "wc_") are validated via SHA-256 hash lookup
// against the api_tokens table. JWT tokens use the standard HMAC-SHA256 flow.
func Middleware(secret string, opts ...MiddlewareOption) gin.HandlerFunc {
	var cfg middlewareConfig
	for _, o := range opts {
		o(&cfg)
	}

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

		// 2. WebSocket auth. Browsers don't let the WebSocket API set arbitrary
		//    Authorization headers, so we fall back to two WS-only mechanisms,
		//    in preference order:
		//
		//    (a) Sec-WebSocket-Protocol "webcasa.token.<jwt>" — preferred.
		//        Subprotocols are not typically logged by reverse proxies or
		//        stored in browser history, which makes them safer than query
		//        strings. We echo the selected subprotocol back so the upgrade
		//        handshake completes.
		//    (b) ?token=… query parameter — legacy fallback for callsites that
		//        have not been migrated. Kept for backward compatibility; new
		//        code should use the subprotocol path.
		if tokenStr == "" && isWebSocketUpgrade(c.Request) {
			for _, raw := range strings.Split(c.GetHeader("Sec-WebSocket-Protocol"), ",") {
				p := strings.TrimSpace(raw)
				if rest, ok := strings.CutPrefix(p, "webcasa.token."); ok && rest != "" {
					tokenStr = rest
					// The Gorilla websocket Upgrader writes the handshake
					// response itself via the hijacked connection and does
					// not consult headers we set on the Gin writer. Store
					// the selected subprotocol on the gin.Context so the
					// handler can pass it into Upgrade via responseHeader
					// (see WebSocketSelectedSubprotocol below).
					c.Set(ctxKeyWSSubprotocol, p)
					break
				}
			}
			if tokenStr == "" {
				tokenStr = c.Query("token")
			}
		}

		if tokenStr == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization required"})
			c.Abort()
			return
		}

		// 3. API Token path: tokens starting with "wc_"
		if strings.HasPrefix(tokenStr, "wc_") && cfg.db != nil {
			if err := validateAPIToken(c, cfg.db, tokenStr); err != nil {
				c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
				c.Abort()
				return
			}
			c.Next()
			return
		}

		// 4. JWT path
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
			// Clear user info — not fully authenticated yet
			c.Set("user_id", uint(0))
			c.Set("username", "")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "2FA verification required", "error_key": "error.2fa_required"})
			c.Abort()
			return
		}

		c.Next()
	}
}

// middlewareConfig holds optional configuration for the auth middleware.
type middlewareConfig struct {
	db *gorm.DB
}

// MiddlewareOption configures the auth middleware.
type MiddlewareOption func(*middlewareConfig)

// WithDB enables API token authentication using the given database connection.
func WithDB(db *gorm.DB) MiddlewareOption {
	return func(cfg *middlewareConfig) { cfg.db = db }
}

// validateAPIToken validates a wc_ prefixed API token against the database.
func validateAPIToken(c *gin.Context, db *gorm.DB, plaintext string) error {
	if len(plaintext) < 11 {
		return errors.New("invalid API token format")
	}

	prefix := plaintext[:11]
	hash := sha256.Sum256([]byte(plaintext))
	tokenHash := hex.EncodeToString(hash[:])

	// Lookup candidates by prefix for fast filtering
	type tokenRow struct {
		ID         uint       `gorm:"primaryKey"`
		UserID     uint       `gorm:"column:user_id"`
		TokenHash  string     `gorm:"column:token_hash"`
		ExpiresAt  *time.Time `gorm:"column:expires_at"`
	}

	var candidates []tokenRow
	if err := db.Table("api_tokens").Where("prefix = ?", prefix).Find(&candidates).Error; err != nil {
		return errors.New("token validation failed")
	}

	for _, t := range candidates {
		if subtle.ConstantTimeCompare([]byte(t.TokenHash), []byte(tokenHash)) == 1 {
			// Check expiry
			if t.ExpiresAt != nil && t.ExpiresAt.Before(time.Now()) {
				return errors.New("API token expired")
			}
			// Update last_used_at
			db.Table("api_tokens").Where("id = ?", t.ID).Update("last_used_at", time.Now())
			// Set context values
			c.Set("user_id", t.UserID)
			c.Set("username", "api-token")
			c.Set("api_token", true)
			c.Set("api_token_id", t.ID)
			return nil
		}
	}

	return errors.New("invalid API token")
}

// isWebSocketUpgrade checks if the request is a WebSocket upgrade handshake.
func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") &&
		strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}
