package auth

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/web-casa/webcasa/internal/model"
	"gorm.io/gorm"
)

// Role hierarchy: owner > admin > operator > viewer
// Each level includes all permissions of lower levels.
const (
	RoleOwner    = "owner"
	RoleAdmin    = "admin"
	RoleOperator = "operator"
	RoleViewer   = "viewer"
)

// ValidRoles is the set of valid role values.
var ValidRoles = map[string]bool{
	RoleOwner: true, RoleAdmin: true, RoleOperator: true, RoleViewer: true,
}

// roleLevel returns the privilege level for a role (higher = more access).
func roleLevel(role string) int {
	switch role {
	case RoleOwner:
		return 4
	case RoleAdmin:
		return 3
	case RoleOperator:
		return 2
	case RoleViewer:
		return 1
	default:
		return 0
	}
}

// requireRole creates a middleware that requires at least the given role level.
func requireRole(db *gorm.DB, minRole string, errorMsg string) gin.HandlerFunc {
	minLevel := roleLevel(minRole)
	return func(c *gin.Context) {
		userID, exists := c.Get("user_id")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization required"})
			c.Abort()
			return
		}

		var user model.User
		if err := db.Select("id, role").First(&user, userID).Error; err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "User not found"})
			c.Abort()
			return
		}

		// Backward compatibility: treat legacy "admin" without owner as owner-equivalent.
		userLevel := roleLevel(user.Role)
		if userLevel < minLevel {
			c.JSON(http.StatusForbidden, gin.H{"error": errorMsg})
			c.Abort()
			return
		}

		c.Set("user_role", user.Role)
		c.Next()
	}
}

// RequireAdmin restricts access to admin-role users (admin + owner).
// Use for: configuration changes, user management, certificate management.
func RequireAdmin(db *gorm.DB) gin.HandlerFunc {
	return requireRole(db, RoleAdmin, "Admin access required")
}

// RequireOperator restricts access to operator-level users (operator + admin + owner).
// Use for: start/stop/restart services, trigger builds, manage deployments.
func RequireOperator(db *gorm.DB) gin.HandlerFunc {
	return requireRole(db, RoleOperator, "Operator access required")
}

// RequireOwner restricts access to the owner only.
// Use for: deleting the panel, managing owner-level users.
func RequireOwner(db *gorm.DB) gin.HandlerFunc {
	return requireRole(db, RoleOwner, "Owner access required")
}
