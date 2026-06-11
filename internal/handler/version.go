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

// ToolVersionDTO is the client-facing representation of a manifest tool entry.
//
// SECURITY: this DTO deliberately OMITS install_scripts. The remote manifest's
// install_scripts contain shell commands; if the frontend ever rendered them as
// copy-paste / one-click actions, a MITM'd or compromised manifest host could
// turn them into operator-run RCE. The backend may use install_scripts
// internally (they are never auto-executed in Go), but clients must never
// receive them. Always convert manifest data through toolVersionToDTO before
// returning it to a client so this guarantee cannot be lost by accident.
type ToolVersionDTO struct {
	Recommended string `json:"recommended"`
	Minimum     string `json:"minimum"`
}

// toolVersionToDTO maps the internal manifest model to the client-facing DTO,
// stripping install_scripts (the only runnable-command field).
func toolVersionToDTO(tv *versioncheck.ToolVersion) ToolVersionDTO {
	return ToolVersionDTO{
		Recommended: tv.Recommended,
		Minimum:     tv.Minimum,
	}
}

// Check returns the cached version comparison results.
// GET /api/version-check
//
// CheckResult itself carries no install_scripts; this endpoint is safe by
// construction. Any future endpoint that surfaces manifest tool details must
// route through ToolVersionDTO / toolVersionToDTO above.
func (h *VersionHandler) Check(c *gin.Context) {
	results := h.checker.GetResults()
	c.JSON(http.StatusOK, gin.H{"checks": results})
}
