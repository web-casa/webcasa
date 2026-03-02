package mcpserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// InternalCaller makes HTTP requests to other plugin APIs on localhost.
type InternalCaller struct {
	baseURL string
	client  *http.Client
}

// NewInternalCaller creates a caller pointing at the panel's own HTTP server.
func NewInternalCaller(port string) *InternalCaller {
	return &InternalCaller{
		baseURL: "http://127.0.0.1:" + port,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Call makes an internal HTTP request and returns the response body.
func (c *InternalCaller) Call(method, path string, body interface{}, token string) (json.RawMessage, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(data))
	}

	return json.RawMessage(data), nil
}

// Get is a convenience method for GET requests.
func (c *InternalCaller) Get(path, token string) (json.RawMessage, error) {
	return c.Call(http.MethodGet, path, nil, token)
}

// Post is a convenience method for POST requests.
func (c *InternalCaller) Post(path string, body interface{}, token string) (json.RawMessage, error) {
	return c.Call(http.MethodPost, path, body, token)
}

// Delete is a convenience method for DELETE requests.
func (c *InternalCaller) Delete(path, token string) (json.RawMessage, error) {
	return c.Call(http.MethodDelete, path, nil, token)
}
