package auth

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/web-casa/webcasa/internal/model"
	"gorm.io/gorm"
)

// RequireAdmin is a Gin middleware that restricts access to admin-role users.
// It must be placed AFTER the auth Middleware so that "user_id" is set in context.
func RequireAdmin(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, exists := c.Get("user_id")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization required"})
			c.Abort()
			return
		}

		// API token users are treated as admin (tokens are admin-managed)
		if isAPI, _ := c.Get("api_token"); isAPI == true {
			c.Next()
			return
		}

		var user model.User
		if err := db.Select("id, role").First(&user, userID).Error; err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "User not found"})
			c.Abort()
			return
		}

		if user.Role != "admin" {
			c.JSON(http.StatusForbidden, gin.H{"error": "Admin access required"})
			c.Abort()
			return
		}

		c.Set("user_role", user.Role)
		c.Next()
	}
}
