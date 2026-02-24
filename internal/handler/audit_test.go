package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

	"github.com/web-casa/webcasa/internal/caddy"
	"github.com/web-casa/webcasa/internal/config"
	"github.com/web-casa/webcasa/internal/model"
	"github.com/web-casa/webcasa/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupAuditTestDB(t *testing.T, name string) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", name)
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	err = db.AutoMigrate(
		&model.Host{}, &model.Upstream{}, &model.Route{},
		&model.CustomHeader{}, &model.AccessRule{}, &model.BasicAuth{},
		&model.AuditLog{}, &model.Setting{},
		&model.Group{}, &model.Tag{}, &model.HostTag{},
		&model.Template{},
	)
	if err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	db.Create(&model.Setting{Key: "auto_reload", Value: "false"})
	return db
}

func setupAuditTestServices(t *testing.T, db *gorm.DB) (*service.HostService, *service.GroupService, *service.TagService, *service.TemplateService) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "webcasa-audit-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	cfg := &config.Config{
		DataDir:       tmpDir,
		CaddyfilePath: tmpDir + "/Caddyfile",
		CaddyBin:      "echo",
		LogDir:        tmpDir + "/logs",
		AdminAPI:      "http://localhost:2019",
	}
	os.MkdirAll(cfg.LogDir, 0755)
	caddyMgr := caddy.NewManager(cfg)
	hostSvc := service.NewHostService(db, caddyMgr, cfg)
	groupSvc := service.NewGroupService(db, caddyMgr, cfg, hostSvc)
	tagSvc := service.NewTagService(db)
	tplSvc := service.NewTemplateService(db, hostSvc)
	return hostSvc, groupSvc, tagSvc, tplSvc
}

func setAuthContext(c *gin.Context) {
	c.Set("user_id", uint(1))
	c.Set("username", "admin")
}

func countAuditLogs(db *gorm.DB) int64 {
	var count int64
	db.Model(&model.AuditLog{}).Count(&count)
	return count
}

var auditTestCounter atomic.Int64

// Feature: phase6-enhancements, Property 20: 变更操作产生审计日志
// For any successful mutation operation (clone Host, Group/Tag CRUD, Template CRUD/import/export),
// the audit log table should contain a new corresponding record after the operation completes.
// **Validates: Requirements 1.6, 4.9, 5.11, 6.10**
func TestProperty20_MutationOperationsProduceAuditLogs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	// Sub-property: Group CRUD produces audit logs
	properties.Property("group create produces audit log", prop.ForAll(
		func(suffix int) bool {
			n := auditTestCounter.Add(1)
			dbName := fmt.Sprintf("audit_grp_c_%d", n)
			db := setupAuditTestDB(t, dbName)
			hostSvc, groupSvc, _, _ := setupAuditTestServices(t, db)
			_ = hostSvc
			groupHandler := NewGroupHandler(groupSvc, db)

			before := countAuditLogs(db)

			body, _ := json.Marshal(map[string]string{
				"name":  fmt.Sprintf("group-%d", suffix),
				"color": "#10b981",
			})
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("POST", "/api/groups", bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			setAuthContext(c)
			groupHandler.Create(c)

			if w.Code != http.StatusCreated {
				return false
			}
			after := countAuditLogs(db)
			return after == before+1
		},
		gen.IntRange(1, 10000),
	))

	// Sub-property: Group delete produces audit log
	properties.Property("group delete produces audit log", prop.ForAll(
		func(suffix int) bool {
			n := auditTestCounter.Add(1)
			dbName := fmt.Sprintf("audit_grp_d_%d", n)
			db := setupAuditTestDB(t, dbName)
			_, groupSvc, _, _ := setupAuditTestServices(t, db)
			groupHandler := NewGroupHandler(groupSvc, db)

			// Create a group first
			group, err := groupSvc.Create(fmt.Sprintf("grp-%d", suffix), "red")
			if err != nil {
				return false
			}

			before := countAuditLogs(db)

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("DELETE", fmt.Sprintf("/api/groups/%d", group.ID), nil)
			c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(group.ID)}}
			setAuthContext(c)
			groupHandler.Delete(c)

			if w.Code != http.StatusOK {
				return false
			}
			after := countAuditLogs(db)
			return after == before+1
		},
		gen.IntRange(1, 10000),
	))

	// Sub-property: Tag create produces audit log
	properties.Property("tag create produces audit log", prop.ForAll(
		func(suffix int) bool {
			n := auditTestCounter.Add(1)
			dbName := fmt.Sprintf("audit_tag_c_%d", n)
			db := setupAuditTestDB(t, dbName)
			_, _, tagSvc, _ := setupAuditTestServices(t, db)
			tagHandler := NewTagHandler(tagSvc, db)

			before := countAuditLogs(db)

			body, _ := json.Marshal(map[string]string{
				"name":  fmt.Sprintf("tag-%d", suffix),
				"color": "blue",
			})
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("POST", "/api/tags", bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			setAuthContext(c)
			tagHandler.Create(c)

			if w.Code != http.StatusCreated {
				return false
			}
			after := countAuditLogs(db)
			return after == before+1
		},
		gen.IntRange(1, 10000),
	))

	// Sub-property: Template create produces audit log
	properties.Property("template create produces audit log", prop.ForAll(
		func(suffix int) bool {
			n := auditTestCounter.Add(1)
			dbName := fmt.Sprintf("audit_tpl_c_%d", n)
			db := setupAuditTestDB(t, dbName)
			_, _, _, tplSvc := setupAuditTestServices(t, db)
			tplHandler := NewTemplateHandler(tplSvc, db)

			before := countAuditLogs(db)

			configJSON := `{"host_type":"proxy","tls_mode":"auto","upstreams":[{"address":"localhost:8080"}]}`
			body, _ := json.Marshal(map[string]string{
				"name":        fmt.Sprintf("tpl-%d", suffix),
				"description": "test template",
				"config":      configJSON,
			})
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("POST", "/api/templates", bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			setAuthContext(c)
			tplHandler.Create(c)

			if w.Code != http.StatusCreated {
				return false
			}
			after := countAuditLogs(db)
			return after == before+1
		},
		gen.IntRange(1, 10000),
	))

	properties.TestingRun(t)
}
