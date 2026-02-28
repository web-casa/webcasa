package filemanager

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const maxUploadSize = 100 << 20 // 100 MB

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true // Non-browser clients
		}
		host := r.Host
		// Allow same-origin connections
		return strings.HasSuffix(origin, "://"+host)
	},
}

// Handler exposes file manager REST and WebSocket endpoints.
type Handler struct {
	fileOps *FileOps
	termMgr *TerminalManager
}

// NewHandler creates a new file manager handler.
func NewHandler(fileOps *FileOps, termMgr *TerminalManager) *Handler {
	return &Handler{fileOps: fileOps, termMgr: termMgr}
}

// List returns directory entries.
func (h *Handler) List(c *gin.Context) {
	path := c.DefaultQuery("path", "/")
	entries, err := h.fileOps.List(path)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"files": entries, "path": path})
}

// Read returns file content as string.
func (h *Handler) Read(c *gin.Context) {
	path := c.Query("path")
	if path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path required"})
		return
	}
	content, err := h.fileOps.Read(path)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"content": content, "path": path})
}

// Write saves content to a file.
func (h *Handler) Write(c *gin.Context) {
	var req struct {
		Path    string `json:"path" binding:"required"`
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.fileOps.Write(req.Path, req.Content); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// Upload handles multipart file upload.
func (h *Handler) Upload(c *gin.Context) {
	// Limit request body size.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxUploadSize)

	path := c.PostForm("path")
	if path == "" {
		path = "/"
	}
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file required or file too large (max 100MB)"})
		return
	}
	defer file.Close()

	dest := path
	if strings.HasSuffix(dest, "/") {
		dest += header.Filename
	}

	// Stream directly to disk instead of buffering in memory.
	if err := h.fileOps.WriteFromReader(dest, file, maxUploadSize); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "path": dest})
}

// Download streams a file.
func (h *Handler) Download(c *gin.Context) {
	path := c.Query("path")
	if path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path required"})
		return
	}
	reader, name, size, err := h.fileOps.Download(path)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	defer reader.Close()

	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	c.Header("Content-Length", strconv.FormatInt(size, 10))
	c.Header("Content-Type", "application/octet-stream")
	if _, err := io.Copy(c.Writer, reader); err != nil {
		// Headers already sent; log but can't change HTTP status.
		_ = err
	}
}

// Mkdir creates a directory.
func (h *Handler) Mkdir(c *gin.Context) {
	var req struct {
		Path string `json:"path" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.fileOps.Mkdir(req.Path); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// Delete removes files or directories.
func (h *Handler) Delete(c *gin.Context) {
	var req struct {
		Paths []string `json:"paths"`
		Path  string   `json:"path"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	paths := req.Paths
	if len(paths) == 0 && req.Path != "" {
		paths = []string{req.Path}
	}
	for _, p := range paths {
		if err := h.fileOps.Delete(p); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// Rename moves/renames a file.
func (h *Handler) Rename(c *gin.Context) {
	var req struct {
		OldPath string `json:"old_path" binding:"required"`
		NewPath string `json:"new_path" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.fileOps.Rename(req.OldPath, req.NewPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// Chmod changes file permissions.
func (h *Handler) Chmod(c *gin.Context) {
	var req struct {
		Path string `json:"path" binding:"required"`
		Mode string `json:"mode" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	mode, err := strconv.ParseUint(req.Mode, 8, 32)
	if err != nil || mode > 0777 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid mode (use octal 0000-0777, e.g. 0755)"})
		return
	}
	if err := h.fileOps.Chmod(req.Path, os.FileMode(mode)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// Info returns file metadata.
func (h *Handler) Info(c *gin.Context) {
	path := c.Query("path")
	if path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path required"})
		return
	}
	fi, err := h.fileOps.Stat(path)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, fi)
}

// Compress creates an archive.
func (h *Handler) Compress(c *gin.Context) {
	var req struct {
		Paths  []string `json:"paths" binding:"required"`
		Dest   string   `json:"dest" binding:"required"`
		Format string   `json:"format" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.fileOps.Compress(req.Paths, req.Dest, req.Format); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// Extract decompresses an archive.
func (h *Handler) Extract(c *gin.Context) {
	var req struct {
		Path string `json:"path" binding:"required"`
		Dest string `json:"dest" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.fileOps.Extract(req.Path, req.Dest); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// TerminalWS handles WebSocket terminal connections.
func (h *Handler) TerminalWS(c *gin.Context) {
	cols, _ := strconv.ParseUint(c.DefaultQuery("cols", "80"), 10, 16)
	rows, _ := strconv.ParseUint(c.DefaultQuery("rows", "24"), 10, 16)

	conn, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	session, err := h.termMgr.Create(uint16(cols), uint16(rows))
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte("Error: "+err.Error()))
		return
	}
	defer h.termMgr.Close(session.ID)

	// PTY → WebSocket (server output).
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 4096)
		for {
			n, err := session.PTY.Read(buf)
			if err != nil {
				return
			}
			if err := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
				return
			}
		}
	}()

	// WebSocket → PTY (client input).
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}

		// Try to parse as JSON control message.
		var input TerminalInput
		if json.Unmarshal(msg, &input) == nil && input.Type != "" {
			switch input.Type {
			case "resize":
				h.termMgr.Resize(session.ID, input.Cols, input.Rows)
			case "data":
				session.PTY.Write([]byte(input.Data))
			}
			continue
		}

		// Raw keystrokes.
		session.PTY.Write(msg)
	}

	<-done
}
