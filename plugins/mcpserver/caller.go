package mcpserver

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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

	data, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
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

// PostSSE makes a POST request to an SSE endpoint, parses the event stream,
// and returns the concatenated data payloads as clean text.
func (c *InternalCaller) PostSSE(path string, body interface{}, token string) (string, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return "", fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(http.MethodPost, c.baseURL+path, bodyReader)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	// Use a longer timeout for SSE streams.
	sseClient := &http.Client{Timeout: 120 * time.Second}
	resp, err := sseClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
		return "", fmt.Errorf("API error %d: %s", resp.StatusCode, string(data))
	}

	// Parse SSE: extract "data:" lines and reconstruct multi-line payloads.
	// Per SSE spec, multi-line data is split into consecutive "data:" lines
	// within the same event (terminated by a blank line). We must rejoin
	// consecutive data lines with "\n".
	var result strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	prevWasData := false
	for scanner.Scan() {
		line := scanner.Text()
		var payload string
		if strings.HasPrefix(line, "data: ") {
			payload = line[6:]
		} else if strings.HasPrefix(line, "data:") {
			payload = line[5:]
		} else {
			// Non-data line (blank line = event boundary, or event:/id: field).
			prevWasData = false
			continue
		}
		// If the previous line was also data, insert a newline to reconstruct
		// the original multi-line payload.
		if prevWasData {
			result.WriteByte('\n')
		}
		result.WriteString(payload)
		prevWasData = true
	}
	return result.String(), scanner.Err()
}
