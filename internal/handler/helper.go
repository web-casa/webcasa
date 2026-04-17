package handler

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

// gzipMinBytes is the minimum payload size for SuccessGzipped to compress.
// Below this threshold the gzip overhead outweighs the bandwidth win.
const gzipMinBytes = 1024

// gzipWriterPool reuses gzip.Writer instances across requests to avoid
// per-call allocation of compression state.
var gzipWriterPool = sync.Pool{
	New: func() interface{} {
		return gzip.NewWriter(nil)
	},
}

// SuccessGzipped writes data as JSON, compressed with gzip when the client
// advertises support and the payload exceeds gzipMinBytes. Responds 200 OK.
//
// Call this from handlers returning large JSON bodies (list endpoints,
// aggregate views). For small responses, keep using c.JSON directly.
//
// Behavior:
//   - No Accept-Encoding: gzip header -> falls back to uncompressed c.JSON.
//   - JSON marshal fails -> 500 with {"error": "..."}.
//   - Payload below threshold -> uncompressed c.JSON (avoids bookkeeping overhead).
//   - Otherwise -> Content-Encoding: gzip + compressed body.
func SuccessGzipped(c *gin.Context, data interface{}) {
	body, err := json.Marshal(data)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if len(body) < gzipMinBytes || !clientAcceptsGzip(c.Request.Header.Get("Accept-Encoding")) {
		c.Data(http.StatusOK, "application/json; charset=utf-8", body)
		return
	}

	var buf bytes.Buffer
	zw := gzipWriterPool.Get().(*gzip.Writer)
	zw.Reset(&buf)
	if _, err := zw.Write(body); err != nil {
		zw.Close()
		gzipWriterPool.Put(zw)
		c.Data(http.StatusOK, "application/json; charset=utf-8", body)
		return
	}
	if err := zw.Close(); err != nil {
		gzipWriterPool.Put(zw)
		c.Data(http.StatusOK, "application/json; charset=utf-8", body)
		return
	}
	gzipWriterPool.Put(zw)

	c.Header("Content-Encoding", "gzip")
	c.Header("Vary", "Accept-Encoding")
	c.Data(http.StatusOK, "application/json; charset=utf-8", buf.Bytes())
}

// clientAcceptsGzip returns true if the Accept-Encoding header advertises
// gzip with a non-zero q-value. RFC 7231 §5.3.1 allows multiple semicolon-
// separated parameters per encoding; earlier versions of this parser only
// read the first parameter blob and could mis-classify headers like
// `gzip;foo=bar;q=0` (extra param before q) or `gzip;q=0;x=y` (q followed
// by another param) as accepted. This implementation scans all parameters,
// honours any q=, and treats malformed q values conservatively as refused.
func clientAcceptsGzip(acceptEncoding string) bool {
	if acceptEncoding == "" {
		return false
	}
	for _, part := range strings.Split(acceptEncoding, ",") {
		part = strings.TrimSpace(part)
		name, paramStr, _ := strings.Cut(part, ";")
		if !strings.EqualFold(strings.TrimSpace(name), "gzip") {
			continue
		}

		q := 1.0 // default when no q param is supplied
		for _, p := range strings.Split(paramStr, ";") {
			p = strings.TrimSpace(p)
			if len(p) < 2 || !strings.EqualFold(p[:2], "q=") {
				continue
			}
			v, err := strconv.ParseFloat(strings.TrimSpace(p[2:]), 64)
			if err != nil {
				// Malformed q — refuse conservatively. Misbehaving clients
				// that cannot accept gzip would rather we served raw than
				// corrupt.
				return false
			}
			q = v
		}
		return q > 0
	}
	return false
}
