package handler

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestClientAcceptsGzip(t *testing.T) {
	cases := []struct {
		header string
		want   bool
	}{
		{"", false},
		{"gzip", true},
		{"GZIP", true},
		{"gzip, deflate", true},
		{"deflate, gzip", true},
		{"gzip;q=0.8", true},
		{"gzip;q=0", false},
		{"gzip;q=0.0", false},
		{"gzip;q=0.001", true},
		{"deflate", false},
		{"br, deflate", false},
		{"gzip ; q = 0.5", true},
		{"identity;q=1, gzip;q=0.8", true},
		// Multi-param headers (RFC 7231 §5.3.1). Earlier parser only read
		// the first parameter blob and returned true for these.
		{"gzip;foo=bar;q=0", false},
		{"gzip;q=0;foo=bar", false},
		{"gzip;foo=bar", true},
		{"gzip;q=garbage", false}, // malformed q refused conservatively
		{"gzip;q=1.0;unexpected=x", true},
	}
	for _, tc := range cases {
		if got := clientAcceptsGzip(tc.header); got != tc.want {
			t.Errorf("clientAcceptsGzip(%q) = %v, want %v", tc.header, got, tc.want)
		}
	}
}

func TestSuccessGzipped_SmallPayloadSkipsCompression(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/", nil)
	c.Request.Header.Set("Accept-Encoding", "gzip")

	SuccessGzipped(c, map[string]string{"msg": "hi"})

	if got := w.Header().Get("Content-Encoding"); got != "" {
		t.Errorf("small payload should not be compressed, got Content-Encoding=%q", got)
	}
	var decoded map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded["msg"] != "hi" {
		t.Errorf("decoded msg = %q, want hi", decoded["msg"])
	}
}

func TestSuccessGzipped_LargePayloadCompresses(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/", nil)
	c.Request.Header.Set("Accept-Encoding", "gzip")

	// Build a payload well above gzipMinBytes.
	data := make([]map[string]string, 200)
	for i := range data {
		data[i] = map[string]string{"key": strings.Repeat("value", 20)}
	}

	SuccessGzipped(c, data)

	if got := w.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("want Content-Encoding=gzip, got %q", got)
	}
	if got := w.Header().Get("Vary"); got != "Accept-Encoding" {
		t.Errorf("want Vary=Accept-Encoding, got %q", got)
	}
	if got := w.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Errorf("want Content-Type=application/json, got %q", got)
	}
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	zr, err := gzip.NewReader(bytes.NewReader(w.Body.Bytes()))
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	decompressed, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("read decompressed: %v", err)
	}

	var decoded []map[string]string
	if err := json.Unmarshal(decompressed, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(decoded) != 200 {
		t.Errorf("decoded len = %d, want 200", len(decoded))
	}

	// Verify compression actually saved bytes.
	if len(w.Body.Bytes()) >= len(decompressed) {
		t.Errorf("compressed size %d not smaller than raw %d", len(w.Body.Bytes()), len(decompressed))
	}
}

func TestSuccessGzipped_NoGzipHeaderServesRaw(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/", nil)
	// No Accept-Encoding header.

	data := make([]map[string]string, 200)
	for i := range data {
		data[i] = map[string]string{"key": strings.Repeat("value", 20)}
	}

	SuccessGzipped(c, data)

	if got := w.Header().Get("Content-Encoding"); got != "" {
		t.Errorf("client without Accept-Encoding should get raw, got Content-Encoding=%q", got)
	}
	// Body is plain JSON.
	var decoded []map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(decoded) != 200 {
		t.Errorf("decoded len = %d, want 200", len(decoded))
	}
}
