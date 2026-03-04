package deploy

import (
	"fmt"
	"net/http"
	"time"
)

// HealthChecker verifies that a deployed service is healthy by polling an HTTP endpoint.
type HealthChecker struct{}

// NewHealthChecker creates a new HealthChecker.
func NewHealthChecker() *HealthChecker {
	return &HealthChecker{}
}

// WaitHealthy polls http://localhost:{port}{path} until it returns a 2xx status code,
// retrying up to `retries` times with exponential backoff.
// Returns nil if healthy, or an error if all retries are exhausted.
func (h *HealthChecker) WaitHealthy(port int, path string, retries int, timeout time.Duration) error {
	if port <= 0 {
		return nil // no port, skip health check
	}
	if path == "" {
		path = "/"
	}
	if retries <= 0 {
		retries = 3
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	url := fmt.Sprintf("http://127.0.0.1:%d%s", port, path)

	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	interval := 2 * time.Second
	deadline := time.Now().Add(timeout)

	for attempt := 1; attempt <= retries; attempt++ {
		if time.Now().After(deadline) {
			return fmt.Errorf("health check timed out after %s", timeout)
		}

		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 400 {
				return nil // healthy
			}
		}

		// Wait before next retry (exponential backoff, capped at 10s)
		if attempt < retries {
			time.Sleep(interval)
			interval = interval * 3 / 2 // 2s → 3s → 4.5s → ...
			if interval > 10*time.Second {
				interval = 10 * time.Second
			}
		}
	}

	return fmt.Errorf("health check failed after %d retries on %s", retries, url)
}
