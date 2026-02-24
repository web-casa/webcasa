package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/caddypanel/caddypanel/internal/model"
	"github.com/caddypanel/caddypanel/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

var errTestCounter atomic.Int64

// Feature: phase6-enhancements, Property 21: 后端错误响应包含翻译键
// For any Phase 6 API error response, the response body should contain an `error_key` field
// whose value is a string usable for frontend i18n translation.
// **Validates: Requirements 7.5**
func TestProperty21_ErrorResponsesContainTranslationKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	// Sub-property: Group create with duplicate name returns error_key
	properties.Property("group duplicate name error has error_key", prop.ForAll(
		func(suffix int) bool {
			n := errTestCounter.Add(1)
			dbName := fmt.Sprintf("errkey_grp_%d", n)
			db := setupAuditTestDB(t, dbName)
			_, groupSvc, _, _ := setupAuditTestServices(t, db)
			groupHandler := NewGroupHandler(groupSvc, db)

			name := fmt.Sprintf("dup-group-%d", suffix)
			// Create first
			groupSvc.Create(name, "red")

			// Try to create duplicate
			body, _ := json.Marshal(map[string]string{"name": name, "color": "blue"})
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("POST", "/api/groups", bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			setAuthContext(c)
			groupHandler.Create(c)

			if w.Code != http.StatusBadRequest {
				return false
			}
			return responseHasErrorKey(w)
		},
		gen.IntRange(1, 10000),
	))

	// Sub-property: Tag create with duplicate name returns error_key
	properties.Property("tag duplicate name error has error_key", prop.ForAll(
		func(suffix int) bool {
			n := errTestCounter.Add(1)
			dbName := fmt.Sprintf("errkey_tag_%d", n)
			db := setupAuditTestDB(t, dbName)
			_, _, tagSvc, _ := setupAuditTestServices(t, db)
			tagHandler := NewTagHandler(tagSvc, db)

			name := fmt.Sprintf("dup-tag-%d", suffix)
			tagSvc.Create(name, "red")

			body, _ := json.Marshal(map[string]string{"name": name, "color": "blue"})
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("POST", "/api/tags", bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			setAuthContext(c)
			tagHandler.Create(c)

			if w.Code != http.StatusBadRequest {
				return false
			}
			return responseHasErrorKey(w)
		},
		gen.IntRange(1, 10000),
	))

	// Sub-property: Template delete preset returns error_key
	properties.Property("delete preset template error has error_key", prop.ForAll(
		func(suffix int) bool {
			n := errTestCounter.Add(1)
			dbName := fmt.Sprintf("errkey_tpl_%d", n)
			db := setupAuditTestDB(t, dbName)
			_, _, _, tplSvc := setupAuditTestServices(t, db)
			tplHandler := NewTemplateHandler(tplSvc, db)

			// Create a preset template directly in DB
			preset := model.Template{
				Name:   fmt.Sprintf("Preset-%d", suffix),
				Type:   "preset",
				Config: `{"host_type":"proxy"}`,
			}
			db.Create(&preset)

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("DELETE", fmt.Sprintf("/api/templates/%d", preset.ID), nil)
			c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(preset.ID)}}
			setAuthContext(c)
			tplHandler.Delete(c)

			if w.Code != http.StatusForbidden {
				return false
			}
			return responseHasErrorKey(w)
		},
		gen.IntRange(1, 10000),
	))

	// Sub-property: Clone with non-existent host returns error_key
	properties.Property("clone non-existent host error has error_key", prop.ForAll(
		func(fakeID int) bool {
			n := errTestCounter.Add(1)
			dbName := fmt.Sprintf("errkey_clone_%d", n)
			db := setupAuditTestDB(t, dbName)
			hostSvc, _, _, _ := setupAuditTestServices(t, db)
			hostHandler := NewHostHandler(hostSvc, db)

			body, _ := json.Marshal(map[string]string{"domain": "new.example.com"})
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("POST", fmt.Sprintf("/api/hosts/%d/clone", fakeID+99999), bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(fakeID + 99999)}}
			setAuthContext(c)
			hostHandler.Clone(c)

			if w.Code != http.StatusNotFound {
				return false
			}
			return responseHasErrorKey(w)
		},
		gen.IntRange(1, 10000),
	))

	// Sub-property: DNS check without domain returns error_key
	properties.Property("dns check missing domain error has error_key", prop.ForAll(
		func(idx int) bool {
			n := errTestCounter.Add(1)
			dbName := fmt.Sprintf("errkey_dns_%d", n)
			db := setupAuditTestDB(t, dbName)
			dnsSvc := service.NewDnsCheckService(db)
			dnsHandler := NewDnsCheckHandler(dnsSvc, db)

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/api/dns-check", nil)
			setAuthContext(c)
			dnsHandler.Check(c)

			if w.Code != http.StatusBadRequest {
				return false
			}
			return responseHasErrorKey(w)
		},
		gen.IntRange(1, 10000),
	))

	// Sub-property: Template import with invalid JSON returns error_key
	properties.Property("template import invalid json error has error_key", prop.ForAll(
		func(suffix int) bool {
			n := errTestCounter.Add(1)
			dbName := fmt.Sprintf("errkey_tpl_imp_%d", n)
			db := setupAuditTestDB(t, dbName)
			_, _, _, tplSvc := setupAuditTestServices(t, db)
			tplHandler := NewTemplateHandler(tplSvc, db)

			// Send invalid JSON as raw body
			invalidJSON := []byte(fmt.Sprintf("{invalid json %d", suffix))
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("POST", "/api/templates/import", bytes.NewReader(invalidJSON))
			c.Request.Header.Set("Content-Type", "application/json")
			setAuthContext(c)
			tplHandler.Import(c)

			if w.Code != http.StatusBadRequest {
				return false
			}
			return responseHasErrorKey(w)
		},
		gen.IntRange(1, 10000),
	))

	properties.TestingRun(t)
}

// responseHasErrorKey checks that the response body contains a non-empty "error_key" field
func responseHasErrorKey(w *httptest.ResponseRecorder) bool {
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		return false
	}
	errKey, ok := resp["error_key"]
	if !ok {
		return false
	}
	keyStr, ok := errKey.(string)
	if !ok || keyStr == "" {
		return false
	}
	// Verify it starts with "error." prefix (i18n convention)
	return len(keyStr) > 6 && keyStr[:6] == "error."
}
