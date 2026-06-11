package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/web-casa/webcasa/internal/model"
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

// WSUpgradeResponseHeader returns the http.Header a WebSocket handler should
// pass as the responseHeader argument to gorilla/websocket's Upgrade. When
// the request authenticated via the Sec-WebSocket-Protocol subprotocol path
// this echoes the selected value so the browser accepts the 101 handshake.
// When the request used ?token= (legacy) or Authorization header, returns
// nil — gorilla then emits a stock 101 with no subprotocol echo, which is
// what clients in those modes expect.
//
// Callers: every wsUpgrader.Upgrade(c.Writer, c.Request, *) site. Prefer
// this over constructing the header inline so the "middleware selected a
// subprotocol but the handler forgot to echo" regression can never recur.
func WSUpgradeResponseHeader(c *gin.Context) http.Header {
	if sub := WebSocketSelectedSubprotocol(c); sub != "" {
		return http.Header{"Sec-WebSocket-Protocol": []string{sub}}
	}
	return nil
}

// Claims defines JWT token claims
type Claims struct {
	UserID       uint   `json:"user_id"`
	Username     string `json:"username"`
	Pending2FA   bool   `json:"pending_2fa,omitempty"`
	TokenVersion int    `json:"tv,omitempty"`
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

// GenerateToken creates a JWT token for a given user. The optional
// tokenVersion embeds the user's current TokenVersion (claim "tv") so the
// middleware can revoke outstanding tokens on password/role change; omitting
// it yields tv=0 (legacy callers / tests).
func GenerateToken(userID uint, username, secret string, tokenVersion ...int) (string, error) {
	tv := 0
	if len(tokenVersion) > 0 {
		tv = tokenVersion[0]
	}
	claims := Claims{
		UserID:       userID,
		Username:     username,
		TokenVersion: tv,
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
func GenerateTempToken(userID uint, username, secret string, tokenVersion ...int) (string, error) {
	tv := 0
	if len(tokenVersion) > 0 {
		tv = tokenVersion[0]
	}
	claims := Claims{
		UserID:       userID,
		Username:     username,
		Pending2FA:   true,
		TokenVersion: tv,
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
		// Pin the signing method: reject any token not using HMAC, so a forged
		// "alg: none" or asymmetric token can never be validated with our secret.
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(secret), nil
	}, jwt.WithValidMethods([]string{"HS256"}))
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

		// Token-version revocation: a JWT is invalidated once the user's
		// TokenVersion is bumped (password change, role change, logout-all).
		// Missing claim (legacy token) = 0 and default user = 0, so existing
		// tokens stay valid until the first bump. One indexed lookup by id.
		if cfg.db != nil {
			var tv int
			if err := cfg.db.Model(&model.User{}).Select("token_version").Where("id = ?", claims.UserID).Scan(&tv).Error; err != nil {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired token"})
				c.Abort()
				return
			}
			if claims.TokenVersion != tv {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Session expired, please log in again", "error_key": "error.session_revoked"})
				c.Abort()
				return
			}
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
		ID          uint       `gorm:"primaryKey"`
		UserID      uint       `gorm:"column:user_id"`
		TokenHash   string     `gorm:"column:token_hash"`
		Permissions string     `gorm:"column:permissions"` // JSON array, e.g. ["hosts:write","*"]
		ExpiresAt   *time.Time `gorm:"column:expires_at"`
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
			// Store the token's scopes so RequireTokenScope can enforce them.
			// Permissions is a JSON array of scope strings (e.g. ["hosts:write"]
			// or ["*"]); empty/missing means NO access (fail closed).
			c.Set("api_token_permissions", parseTokenScopes(t.Permissions))
			return nil
		}
	}

	return errors.New("invalid API token")
}

// parseTokenScopes parses the JSON-encoded permissions array stored on an API
// token into a slice of scope strings. A malformed or empty value yields nil,
// which the scope checks treat as NO access (fail closed).
func parseTokenScopes(permissions string) []string {
	if permissions == "" || permissions == "[]" {
		return nil
	}
	var scopes []string
	if err := json.Unmarshal([]byte(permissions), &scopes); err != nil {
		return nil
	}
	return scopes
}

// scopeMatches reports whether the granted scope slice satisfies the required
// scope. A granted "*" (or "<category>:*"/"*:<action>") wildcard matches using
// the same category:action vocabulary as the MCP layer
// (plugins/mcpserver/tools.go checkPermission). An empty granted slice never
// matches — tokens with no permissions get NO access.
func scopeMatches(granted []string, required string) bool {
	rCat, rAct := splitScope(required)
	for _, g := range granted {
		if g == "*" {
			return true
		}
		gCat, gAct := splitScope(g)
		if (gCat == "*" || gCat == rCat) && (gAct == "*" || gAct == rAct) {
			return true
		}
	}
	return false
}

// splitScope splits a "category:action" scope. A bare scope (no colon) is
// treated as "<scope>:*".
func splitScope(s string) (category, action string) {
	if i := strings.IndexByte(s, ':'); i >= 0 {
		return s[:i], s[i+1:]
	}
	return s, "*"
}

// RequireTokenScope gates API-token-authenticated requests by scope. It is a
// no-op for normal JWT/session auth (those are governed by RBAC role checks).
//
// For requests authenticated via a "wc_" API token (api_token == true) it
// requires the token's permissions to grant the given scope, "<category>:*",
// "*:<action>", or "*". Tokens with empty/missing permissions are rejected:
// access fails closed, never open. On insufficient scope it aborts 403.
//
// Scope strings use the same "category:action" vocabulary as the MCP layer
// (e.g. "hosts:write", "system:write"); see plugins/mcpserver/tools.go.
func RequireTokenScope(scope string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if isAPIToken, _ := c.Get("api_token"); isAPIToken == true {
			granted, _ := c.Get("api_token_permissions")
			scopes, _ := granted.([]string)
			if !scopeMatches(scopes, scope) {
				c.JSON(http.StatusForbidden, gin.H{"error": "API token lacks required scope: " + scope})
				c.Abort()
				return
			}
		}
		c.Next()
	}
}

// RequireTokenScopeForMutations gates API-token requests on any state-changing
// method (POST/PUT/PATCH/DELETE). It is a no-op for safe methods (GET/HEAD) and
// for normal JWT/session auth.
//
// This is a SAFE-DEFAULT gate: a scoped API token may only perform mutations it
// has been explicitly granted via the given scope (or a "*" wildcard). A token
// with no/empty permissions can read (subject to RBAC) but can never mutate —
// the previous behavior, where any "wc_" token inherited the owner's full role
// on REST routes, is closed. Read access still flows through normal RBAC.
func RequireTokenScopeForMutations(scope string) gin.HandlerFunc {
	gate := RequireTokenScope(scope)
	return func(c *gin.Context) {
		switch c.Request.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			gate(c)
		default:
			c.Next()
		}
	}
}

// RequireFullScopeForMutations is the SAFE-DEFAULT gate wired onto the REST
// route groups. On any state-changing method (POST/PUT/PATCH/DELETE) it permits
// an API-token request only when the token's permissions include "*" (full
// access). Scoped tokens (anything other than ["*"]) are rejected on mutations
// because the REST router does not (yet) carry per-route category:action scope
// metadata; granting fine-grained scopes there would require per-route wiring.
// Read (GET/HEAD) requests are unaffected and remain governed by RBAC.
//
// This closes the prior bypass where any "wc_" token inherited the owner's full
// role on mutating REST routes. Fine-grained scopes remain enforced on the MCP
// surface via plugins/mcpserver/tools.go checkPermission. It is a no-op for
// normal JWT/session auth.
func RequireFullScopeForMutations() gin.HandlerFunc {
	return RequireTokenScopeForMutations("*")
}

// isWebSocketUpgrade checks if the request is a WebSocket upgrade handshake.
func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") &&
		strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}
