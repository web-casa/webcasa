package handler

import (
	"fmt"
	"io"
	"net/http"

	"github.com/caddypanel/caddypanel/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// TemplateHandler manages template CRUD and import/export endpoints.
type TemplateHandler struct {
	svc *service.TemplateService
	db  *gorm.DB
}

// NewTemplateHandler creates a new TemplateHandler.
func NewTemplateHandler(svc *service.TemplateService, db *gorm.DB) *TemplateHandler {
	return &TemplateHandler{svc: svc, db: db}
}

func (h *TemplateHandler) audit(c *gin.Context, action, targetID, detail string) {
	if uid, ok := c.Get("user_id"); ok {
		uname, _ := c.Get("username")
		WriteAuditLog(h.db, uid.(uint), fmt.Sprint(uname), action, "template", targetID, detail, c.ClientIP())
	}
}

// List returns all templates.
func (h *TemplateHandler) List(c *gin.Context) {
	templates, err := h.svc.List()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "error_key": "error.template_list_failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"templates": templates, "total": len(templates)})
}

// Create adds a new custom template.
func (h *TemplateHandler) Create(c *gin.Context) {
	var req struct {
		Name        string `json:"name" binding:"required"`
		Description string `json:"description"`
		Config      string `json:"config" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "error_key": "error.invalid_request"})
		return
	}

	tpl, err := h.svc.Create(req.Name, req.Description, req.Config)
	if err != nil {
		errMsg := err.Error()
		switch errMsg {
		case "error.invalid_template_json":
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid template config JSON", "error_key": errMsg})
		case "error.template_missing_fields":
			c.JSON(http.StatusBadRequest, gin.H{"error": "Template config missing required fields (host_type)", "error_key": errMsg})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": errMsg, "error_key": "error.template_create_failed"})
		}
		return
	}

	h.audit(c, "CREATE", fmt.Sprint(tpl.ID), fmt.Sprintf("Created template '%s'", tpl.Name))
	c.JSON(http.StatusCreated, tpl)
}

// Update modifies an existing custom template.
func (h *TemplateHandler) Update(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID", "error_key": "error.invalid_id"})
		return
	}

	var req struct {
		Name        string `json:"name" binding:"required"`
		Description string `json:"description"`
		Config      string `json:"config"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "error_key": "error.invalid_request"})
		return
	}

	tpl, err := h.svc.Update(id, req.Name, req.Description, req.Config)
	if err != nil {
		errMsg := err.Error()
		switch errMsg {
		case "error.template_not_found":
			c.JSON(http.StatusNotFound, gin.H{"error": "Template not found", "error_key": errMsg})
		case "error.preset_immutable":
			c.JSON(http.StatusForbidden, gin.H{"error": "Preset templates cannot be modified", "error_key": errMsg})
		case "error.invalid_template_json":
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid template config JSON", "error_key": errMsg})
		case "error.template_missing_fields":
			c.JSON(http.StatusBadRequest, gin.H{"error": "Template config missing required fields", "error_key": errMsg})
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": errMsg, "error_key": "error.template_update_failed"})
		}
		return
	}

	h.audit(c, "UPDATE", fmt.Sprint(tpl.ID), fmt.Sprintf("Updated template '%s'", tpl.Name))
	c.JSON(http.StatusOK, tpl)
}

// Delete removes a custom template.
func (h *TemplateHandler) Delete(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID", "error_key": "error.invalid_id"})
		return
	}

	if err := h.svc.Delete(id); err != nil {
		errMsg := err.Error()
		switch errMsg {
		case "error.template_not_found":
			c.JSON(http.StatusNotFound, gin.H{"error": "Template not found", "error_key": errMsg})
		case "error.preset_immutable":
			c.JSON(http.StatusForbidden, gin.H{"error": "Preset templates cannot be deleted", "error_key": errMsg})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": errMsg, "error_key": "error.template_delete_failed"})
		}
		return
	}

	h.audit(c, "DELETE", fmt.Sprint(id), "Deleted template")
	c.JSON(http.StatusOK, gin.H{"message": "Template deleted successfully"})
}

// Import accepts a JSON file upload and creates a template from it.
func (h *TemplateHandler) Import(c *gin.Context) {
	file, _, err := c.Request.FormFile("file")
	if err != nil {
		// Fallback: try reading raw JSON body
		body, readErr := io.ReadAll(c.Request.Body)
		if readErr != nil || len(body) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "No file uploaded", "error_key": "error.invalid_request"})
			return
		}
		tpl, importErr := h.svc.Import(body)
		if importErr != nil {
			h.handleImportError(c, importErr)
			return
		}
		h.audit(c, "IMPORT", fmt.Sprint(tpl.ID), fmt.Sprintf("Imported template '%s'", tpl.Name))
		c.JSON(http.StatusCreated, tpl)
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read file", "error_key": "error.invalid_request"})
		return
	}

	tpl, err := h.svc.Import(data)
	if err != nil {
		h.handleImportError(c, err)
		return
	}

	h.audit(c, "IMPORT", fmt.Sprint(tpl.ID), fmt.Sprintf("Imported template '%s'", tpl.Name))
	c.JSON(http.StatusCreated, tpl)
}

func (h *TemplateHandler) handleImportError(c *gin.Context, err error) {
	errMsg := err.Error()
	switch errMsg {
	case "error.invalid_template_json":
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid template JSON format", "error_key": errMsg})
	case "error.template_missing_fields":
		c.JSON(http.StatusBadRequest, gin.H{"error": "Template JSON missing required fields", "error_key": errMsg})
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": errMsg, "error_key": "error.template_import_failed"})
	}
}

// Export returns a template as a JSON file download.
func (h *TemplateHandler) Export(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID", "error_key": "error.invalid_id"})
		return
	}

	data, err := h.svc.Export(id)
	if err != nil {
		if err.Error() == "error.template_not_found" {
			c.JSON(http.StatusNotFound, gin.H{"error": "Template not found", "error_key": "error.template_not_found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "error_key": "error.template_export_failed"})
		return
	}

	h.audit(c, "EXPORT", fmt.Sprint(id), "Exported template")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=template_%d.json", id))
	c.Data(http.StatusOK, "application/json", data)
}

// CreateHost creates a new host from a template.
func (h *TemplateHandler) CreateHost(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID", "error_key": "error.invalid_id"})
		return
	}

	var req struct {
		Domain string `json:"domain" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "error_key": "error.invalid_request"})
		return
	}

	host, err := h.svc.CreateFromTemplate(id, req.Domain)
	if err != nil {
		errMsg := err.Error()
		switch errMsg {
		case "error.template_not_found":
			c.JSON(http.StatusNotFound, gin.H{"error": "Template not found", "error_key": errMsg})
		case "error.domain_exists":
			c.JSON(http.StatusBadRequest, gin.H{
				"error":     fmt.Sprintf("domain '%s' already exists", req.Domain),
				"error_key": errMsg,
			})
		case "error.invalid_template_json":
			c.JSON(http.StatusBadRequest, gin.H{"error": "Template config is invalid", "error_key": errMsg})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": errMsg, "error_key": "error.template_create_host_failed"})
		}
		return
	}

	h.audit(c, "CREATE_FROM_TEMPLATE", fmt.Sprint(host.ID),
		fmt.Sprintf("Created host '%s' from template #%d", req.Domain, id))
	c.JSON(http.StatusCreated, host)
}

// SaveAsTemplate creates a template from an existing host (called from host context).
func (h *TemplateHandler) SaveAsTemplate(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID", "error_key": "error.invalid_id"})
		return
	}

	var req struct {
		Name        string `json:"name" binding:"required"`
		Description string `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "error_key": "error.invalid_request"})
		return
	}

	tpl, err := h.svc.SaveAsTemplate(id, req.Name, req.Description)
	if err != nil {
		if err.Error() == "error.host_not_found" {
			c.JSON(http.StatusNotFound, gin.H{"error": "Host not found", "error_key": "error.host_not_found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "error_key": "error.template_save_failed"})
		return
	}

	h.audit(c, "SAVE_AS_TEMPLATE", fmt.Sprint(tpl.ID),
		fmt.Sprintf("Saved host #%d as template '%s'", id, tpl.Name))
	c.JSON(http.StatusCreated, tpl)
}
