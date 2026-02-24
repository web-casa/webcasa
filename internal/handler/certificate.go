package handler

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/web-casa/webcasa/internal/config"
	"github.com/web-casa/webcasa/internal/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// CertificateHandler handles certificate management
type CertificateHandler struct {
	db  *gorm.DB
	cfg *config.Config
}

// NewCertificateHandler creates a new CertificateHandler
func NewCertificateHandler(db *gorm.DB, cfg *config.Config) *CertificateHandler {
	return &CertificateHandler{db: db, cfg: cfg}
}

// List returns all certificates
func (h *CertificateHandler) List(c *gin.Context) {
	var certs []model.Certificate
	h.db.Order("created_at desc").Find(&certs)

	// Count hosts per cert
	type CertWithCount struct {
		model.Certificate
		HostCount int64 `json:"host_count"`
	}
	var result []CertWithCount
	for _, cert := range certs {
		var count int64
		h.db.Model(&model.Host{}).Where("certificate_id = ?", cert.ID).Count(&count)
		result = append(result, CertWithCount{Certificate: cert, HostCount: count})
	}

	c.JSON(http.StatusOK, gin.H{"certificates": result})
}

// Upload handles uploading a new certificate (cert + key files)
func (h *CertificateHandler) Upload(c *gin.Context) {
	name := c.PostForm("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
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

	// Read cert to parse domains and expiry
	certData, err := readMultipartFile(certFile)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read cert file"})
		return
	}

	domains, expiresAt := parseCertInfo(certData)

	// Save files
	certDir := filepath.Join(h.cfg.DataDir, "certs", "_managed", fmt.Sprintf("%d", time.Now().UnixMilli()))
	if err := os.MkdirAll(certDir, 0700); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create cert directory"})
		return
	}

	certPath := filepath.Join(certDir, "cert.pem")
	keyPath := filepath.Join(certDir, "key.pem")

	if err := os.WriteFile(certPath, certData, 0644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save cert"})
		return
	}

	keyData, err := readMultipartFile(keyFile)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read key file"})
		return
	}
	if err := os.WriteFile(keyPath, keyData, 0600); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save key"})
		return
	}

	cert := model.Certificate{
		Name:      name,
		Domains:   domains,
		CertPath:  certPath,
		KeyPath:   keyPath,
		ExpiresAt: expiresAt,
	}
	if err := h.db.Create(&cert).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save certificate"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "Certificate uploaded", "certificate": cert})
}

// Delete removes a certificate
func (h *CertificateHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	var cert model.Certificate
	if err := h.db.First(&cert, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "certificate not found"})
		return
	}

	// Check if any hosts use this cert
	var count int64
	h.db.Model(&model.Host{}).Where("certificate_id = ?", id).Count(&count)
	if count > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("certificate is used by %d host(s), remove references first", count)})
		return
	}

	// Remove files
	if cert.CertPath != "" {
		os.RemoveAll(filepath.Dir(cert.CertPath))
	}

	h.db.Delete(&cert)
	c.JSON(http.StatusOK, gin.H{"message": "Certificate deleted"})
}

// parseCertInfo extracts domains and expiry from PEM certificate data
func parseCertInfo(certData []byte) (string, *time.Time) {
	block, _ := pem.Decode(certData)
	if block == nil {
		return "", nil
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", nil
	}

	var domains []string
	if cert.Subject.CommonName != "" {
		domains = append(domains, cert.Subject.CommonName)
	}
	for _, san := range cert.DNSNames {
		if san != cert.Subject.CommonName {
			domains = append(domains, san)
		}
	}

	expires := cert.NotAfter
	return strings.Join(domains, ", "), &expires
}

func readMultipartFile(header *multipart.FileHeader) ([]byte, error) {
	src, err := header.Open()
	if err != nil {
		return nil, err
	}
	defer src.Close()
	return io.ReadAll(src)
}
