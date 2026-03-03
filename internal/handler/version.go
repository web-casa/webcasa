package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/web-casa/webcasa/internal/versioncheck"
)

// VersionHandler serves version-check results.
type VersionHandler struct {
	checker *versioncheck.Checker
}

// NewVersionHandler creates a VersionHandler.
func NewVersionHandler(checker *versioncheck.Checker) *VersionHandler {
	return &VersionHandler{checker: checker}
}

// Check returns the cached version comparison results.
// GET /api/version-check
func (h *VersionHandler) Check(c *gin.Context) {
	results := h.checker.GetResults()
	c.JSON(http.StatusOK, gin.H{"checks": results})
}
