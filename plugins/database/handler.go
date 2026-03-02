package database

import (
	"bufio"
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// Handler implements the REST API for the database plugin.
type Handler struct {
	svc    *Service
	sqlite *SQLiteBrowser
}

// NewHandler creates a database Handler.
func NewHandler(svc *Service, sqlite *SQLiteBrowser) *Handler {
	return &Handler{svc: svc, sqlite: sqlite}
}

// ── Engines ──

// ListEngines returns supported database engines.
func (h *Handler) ListEngines(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"engines": SupportedEngines})
}

// ── Instances ──

// ListInstances returns all instances.
func (h *Handler) ListInstances(c *gin.Context) {
	instances, err := h.svc.ListInstances()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"instances": instances})
}

// GetInstance returns a single instance.
func (h *Handler) GetInstance(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	inst, err := h.svc.GetInstance(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Instance not found"})
		return
	}
	c.JSON(http.StatusOK, inst)
}

// CreateInstance creates a new database instance.
func (h *Handler) CreateInstance(c *gin.Context) {
	var req CreateInstanceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	inst, err := h.svc.CreateInstance(&req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, inst)
}

// DeleteInstance deletes an instance.
func (h *Handler) DeleteInstance(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	if err := h.svc.DeleteInstance(id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Instance deleted"})
}

// StartInstance starts an instance.
func (h *Handler) StartInstance(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	if err := h.svc.StartInstance(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Instance started"})
}

// StopInstance stops an instance.
func (h *Handler) StopInstance(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	if err := h.svc.StopInstance(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Instance stopped"})
}

// RestartInstance restarts an instance.
func (h *Handler) RestartInstance(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	if err := h.svc.RestartInstance(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Instance restarted"})
}

// InstanceLogs returns recent logs.
func (h *Handler) InstanceLogs(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	tail := c.DefaultQuery("tail", "200")
	logs, err := h.svc.InstanceLogs(id, tail)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"logs": logs})
}

// GetConnectionInfo returns connection info for an instance.
func (h *Handler) GetConnectionInfo(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	info, err := h.svc.GetConnectionInfo(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, info)
}

// GetRootPassword returns the root password for an instance.
func (h *Handler) GetRootPassword(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	password, err := h.svc.GetRootPassword(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"password": password})
}

// ExecuteQuery executes a SQL query or Redis command against a running instance.
func (h *Handler) ExecuteQuery(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	var req ExecuteQueryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	result, err := h.svc.ExecuteQuery(id, req.Database, req.Query, req.Limit)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

// ── Database CRUD ──

// ListDatabases returns databases for an instance.
func (h *Handler) ListDatabases(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	dbs, err := h.svc.ListDatabases(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"databases": dbs})
}

// CreateDatabase creates a logical database.
func (h *Handler) CreateDatabase(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	var req CreateDatabaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	db, err := h.svc.CreateDatabase(id, &req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, db)
}

// DeleteDatabase drops a logical database.
func (h *Handler) DeleteDatabase(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	dbName := c.Param("dbname")
	if dbName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "database name required"})
		return
	}
	if err := h.svc.DeleteDatabase(id, dbName); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Database deleted"})
}

// ── User CRUD ──

// ListUsers returns database users for an instance.
func (h *Handler) ListUsers(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	users, err := h.svc.ListUsers(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"users": users})
}

// CreateUser creates a database user.
func (h *Handler) CreateUser(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	var req CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	user, err := h.svc.CreateUser(id, &req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, user)
}

// DeleteUser drops a database user.
func (h *Handler) DeleteUser(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	username := c.Param("username")
	if username == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username required"})
		return
	}
	if err := h.svc.DeleteUser(id, username); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "User deleted"})
}

// ── SQLite Browser ──

// SQLiteTables returns table names in a SQLite file.
func (h *Handler) SQLiteTables(c *gin.Context) {
	path := c.Query("path")
	if path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path parameter required"})
		return
	}
	tables, err := h.sqlite.ListTables(path)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"tables": tables})
}

// SQLiteSchema returns the schema for a table.
func (h *Handler) SQLiteSchema(c *gin.Context) {
	path := c.Query("path")
	table := c.Query("table")
	if path == "" || table == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path and table parameters required"})
		return
	}
	schema, err := h.sqlite.GetSchema(path, table)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"schema": schema})
}

// SQLiteQuery executes a read-only query.
func (h *Handler) SQLiteQuery(c *gin.Context) {
	var req SQLiteQueryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	result, err := h.sqlite.Query(req.Path, req.Query, req.Limit)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

// ── WebSocket Log Streaming ──

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		u, err := url.Parse(origin)
		if err != nil {
			return false
		}
		return u.Host == r.Host
	},
}

// InstanceLogsWS streams instance logs via WebSocket.
func (h *Handler) InstanceLogsWS(c *gin.Context) {
	conn, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	id, err := parseID(c)
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte("Error: invalid id"))
		return
	}
	tail := c.DefaultQuery("tail", "100")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				cancel()
				return
			}
		}
	}()

	reader, err := h.svc.InstanceLogsFollow(ctx, id, tail)
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte("Error: "+err.Error()))
		return
	}
	defer reader.Close()

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)
	for scanner.Scan() {
		if err := conn.WriteMessage(websocket.TextMessage, scanner.Bytes()); err != nil {
			return
		}
	}
}

// ── Helpers ──

func parseID(c *gin.Context) (uint, error) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return 0, err
	}
	return uint(id), nil
}
