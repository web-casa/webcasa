package deploy

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// HealthChecker verifies that a deployed service is healthy by polling an HTTP endpoint.
type HealthChecker struct{}

// NewHealthChecker creates a new HealthChecker.
func NewHealthChecker() *HealthChecker {
	return &HealthChecker{}
}

// HealthCheckConfig holds all parameters for a health check.
type HealthCheckConfig struct {
	Port        int
	Path        string
	Method      string        // GET, HEAD, POST (default: GET)
	ExpectCode  int           // expected HTTP status (0 = any 2xx)
	ExpectBody  string        // response body must contain this text
	Timeout     time.Duration // overall deadline
	Retries     int
	StartPeriod time.Duration // wait before first check
}

// WaitHealthy polls http://localhost:{port}{path} until it passes all checks.
// Supports custom method, expected status code, expected body text, and start period.
func (h *HealthChecker) WaitHealthy(port int, path string, retries int, timeout time.Duration) error {
	return h.WaitHealthyAdvanced(HealthCheckConfig{
		Port:    port,
		Path:    path,
		Retries: retries,
		Timeout: timeout,
	})
}

// WaitHealthyAdvanced runs health checks with full configuration.
func (h *HealthChecker) WaitHealthyAdvanced(cfg HealthCheckConfig) error {
	if cfg.Port <= 0 {
		return nil
	}
	if cfg.Path == "" {
		cfg.Path = "/"
	}
	if cfg.Method == "" {
		cfg.Method = "GET"
	}
	if cfg.Retries <= 0 {
		cfg.Retries = 3
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}

	// Validate method against allowlist.
	switch cfg.Method {
	case "GET", "HEAD", "POST":
		// OK
	default:
		cfg.Method = "GET"
	}

	// Validate expect code range.
	if cfg.ExpectCode < 0 || cfg.ExpectCode > 599 {
		cfg.ExpectCode = 0
	}

	// Compute deadline BEFORE start period so total time is bounded.
	deadline := time.Now().Add(cfg.Timeout)

	// Wait for start period before first check (cancellable via deadline).
	if cfg.StartPeriod > 0 {
		waitUntil := time.Now().Add(cfg.StartPeriod)
		if waitUntil.After(deadline) {
			waitUntil = deadline
		}
		time.Sleep(time.Until(waitUntil))
	}

	url := fmt.Sprintf("http://127.0.0.1:%d%s", cfg.Port, cfg.Path)
	interval := 2 * time.Second

	for attempt := 1; attempt <= cfg.Retries; attempt++ {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return fmt.Errorf("health check timed out after %s", cfg.Timeout)
		}

		// Per-attempt HTTP timeout capped by remaining deadline.
		attemptTimeout := 5 * time.Second
		if remaining < attemptTimeout {
			attemptTimeout = remaining
		}
		client := &http.Client{Timeout: attemptTimeout}

		req, err := http.NewRequest(cfg.Method, url, nil)
		if err != nil {
			return fmt.Errorf("create health check request: %w", err)
		}

		resp, err := client.Do(req)
		if err == nil {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
			resp.Body.Close()

			// Check status code.
			codeOK := false
			if cfg.ExpectCode > 0 {
				codeOK = resp.StatusCode == cfg.ExpectCode
			} else {
				codeOK = resp.StatusCode >= 200 && resp.StatusCode < 400
			}

			// Check body content.
			bodyOK := true
			if cfg.ExpectBody != "" {
				bodyOK = strings.Contains(string(body), cfg.ExpectBody)
			}

			if codeOK && bodyOK {
				return nil // healthy
			}
		}

		// Wait before next retry, capped by remaining deadline.
		if attempt < cfg.Retries {
			sleep := interval
			if rem := time.Until(deadline); rem < sleep {
				sleep = rem
			}
			if sleep > 0 {
				time.Sleep(sleep)
			}
			interval = interval * 3 / 2
			if interval > 10*time.Second {
				interval = 10 * time.Second
			}
		}
	}

	return fmt.Errorf("health check failed after %d retries on %s %s", cfg.Retries, cfg.Method, url)
}
