package handler

import (
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/caddypanel/caddypanel/internal/config"
	"github.com/caddypanel/caddypanel/internal/service"
	"github.com/gin-gonic/gin"
)

// CertHandler handles SSL certificate operations
type CertHandler struct {
	svc *service.HostService
	cfg *config.Config
}

// NewCertHandler creates a new CertHandler
func NewCertHandler(svc *service.HostService, cfg *config.Config) *CertHandler {
	return &CertHandler{svc: svc, cfg: cfg}
}

// Upload handles uploading custom SSL cert + key for a host
func (h *CertHandler) Upload(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid host id"})
		return
	}

	host, err := h.svc.Get(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "host not found"})
		return
	}

	certFile, err := c.FormFile("cert")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cert file is required"})
		return
	}

	keyFile, err := c.FormFile("key")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "key file is required"})
		return
	}

	// Create cert directory for this domain
	certDir := filepath.Join(h.cfg.DataDir, "certs", host.Domain)
	if err := os.MkdirAll(certDir, 0700); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to create cert directory: %v", err)})
		return
	}

	certPath := filepath.Join(certDir, "cert.pem")
	keyPath := filepath.Join(certDir, "key.pem")

	// Save cert file
	if err := saveUploadedFile(certFile, certPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to save cert: %v", err)})
		return
	}

	// Save key file
	if err := saveUploadedFile(keyFile, keyPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to save key: %v", err)})
		return
	}

	// Set restrictive permissions on key file
	os.Chmod(keyPath, 0600)

	// Update host with cert paths
	if err := h.svc.UpdateCertPaths(uint(id), certPath, keyPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to update cert paths: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":   "Certificate uploaded successfully",
		"cert_path": certPath,
		"key_path":  keyPath,
	})
}

// Delete removes custom SSL cert + key for a host
func (h *CertHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid host id"})
		return
	}

	host, err := h.svc.Get(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "host not found"})
		return
	}

	// Remove cert directory
	certDir := filepath.Join(h.cfg.DataDir, "certs", host.Domain)
	os.RemoveAll(certDir)

	// Clear cert paths
	if err := h.svc.UpdateCertPaths(uint(id), "", ""); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to clear cert paths: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Certificate removed, reverting to automatic HTTPS"})
}

func saveUploadedFile(header *multipart.FileHeader, dst string) error {
	src, err := header.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, src)
	return err
}
