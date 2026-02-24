package handler

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/web-casa/webcasa/internal/model"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// UserHandler handles user management
type UserHandler struct {
	db *gorm.DB
}

func NewUserHandler(db *gorm.DB) *UserHandler {
	return &UserHandler{db: db}
}

// List returns all users (without passwords)
func (h *UserHandler) List(c *gin.Context) {
	var users []model.User
	h.db.Order("id ASC").Find(&users)
	c.JSON(http.StatusOK, gin.H{"users": users, "total": len(users)})
}

// Create creates a new user
func (h *UserHandler) Create(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required,min=6"`
		Role     string `json:"role"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	role := req.Role
	if role == "" {
		role = "viewer"
	}
	if role != "admin" && role != "viewer" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "role must be 'admin' or 'viewer'"})
		return
	}

	var count int64
	h.db.Model(&model.User{}).Where("username = ?", req.Username).Count(&count)
	if count > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "username already exists"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to hash password"})
		return
	}

	user := model.User{
		Username: req.Username,
		Password: string(hash),
		Role:     role,
	}
	if err := h.db.Create(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Audit
	if uid, ok := c.Get("user_id"); ok {
		uname, _ := c.Get("username")
		WriteAuditLog(h.db, uid.(uint), fmt.Sprint(uname), "CREATE", "user", fmt.Sprint(user.ID),
			fmt.Sprintf("Created user '%s' with role '%s'", user.Username, user.Role), c.ClientIP())
	}

	c.JSON(http.StatusCreated, user)
}

// Update modifies a user (password and/or role)
func (h *UserHandler) Update(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var user model.User
	if err := h.db.First(&user, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	var req struct {
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Role != "" {
		if req.Role != "admin" && req.Role != "viewer" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "role must be 'admin' or 'viewer'"})
			return
		}
		user.Role = req.Role
	}

	if req.Password != "" {
		if len(req.Password) < 6 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "password must be at least 6 characters"})
			return
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to hash password"})
			return
		}
		user.Password = string(hash)
	}

	h.db.Save(&user)

	if uid, ok := c.Get("user_id"); ok {
		uname, _ := c.Get("username")
		WriteAuditLog(h.db, uid.(uint), fmt.Sprint(uname), "UPDATE", "user", fmt.Sprint(user.ID),
			fmt.Sprintf("Updated user '%s'", user.Username), c.ClientIP())
	}

	c.JSON(http.StatusOK, user)
}

// Delete removes a user (cannot delete self)
func (h *UserHandler) Delete(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	// Cannot delete self
	currentID, _ := c.Get("user_id")
	if currentID.(uint) == uint(id) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot delete yourself"})
		return
	}

	var user model.User
	if err := h.db.First(&user, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	h.db.Delete(&user)

	if uid, ok := c.Get("user_id"); ok {
		uname, _ := c.Get("username")
		WriteAuditLog(h.db, uid.(uint), fmt.Sprint(uname), "DELETE", "user", fmt.Sprint(id),
			fmt.Sprintf("Deleted user '%s'", user.Username), c.ClientIP())
	}

	c.JSON(http.StatusOK, gin.H{"message": "user deleted"})
}
