package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// LLMClient communicates with various LLM provider APIs.
type LLMClient struct {
	baseURL    string
	apiKey     string
	model      string
	apiFormat  string // openai-chat | anthropic-messages | google-generativeai
	httpClient *http.Client
}

// openAIChatURL returns the OpenAI-compatible chat completions endpoint,
// handling the case where baseURL already includes "/v1".
func (c *LLMClient) openAIChatURL() string {
	if strings.HasSuffix(c.baseURL, "/v1") {
		return c.baseURL + "/chat/completions"
	}
	return c.baseURL + "/v1/chat/completions"
}

// anthropicMessagesURL returns the Anthropic-compatible messages endpoint,
// handling the case where baseURL already includes "/v1".
func (c *LLMClient) anthropicMessagesURL() string {
	if strings.HasSuffix(c.baseURL, "/v1") {
		return c.baseURL + "/messages"
	}
	return c.baseURL + "/v1/messages"
}

// NewLLMClient creates a new LLM client.
func NewLLMClient(baseURL, apiKey, model, apiFormat string) *LLMClient {
	if apiFormat == "" {
		apiFormat = "openai-chat"
	}
	return &LLMClient{
		baseURL:   strings.TrimRight(baseURL, "/"),
		apiKey:    apiKey,
		model:     model,
		apiFormat: apiFormat,
		httpClient: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
}

// chatMessage is the internal message format.
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// StreamCallback receives each content delta from the SSE stream.
type StreamCallback func(content string) error

// ChatStream sends a chat request with streaming and calls cb for each delta.
func (c *LLMClient) ChatStream(ctx context.Context, messages []chatMessage, cb StreamCallback) error {
	req, err := c.buildRequest(ctx, messages, true)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	return c.parseSSEStream(resp.Body, cb)
}

// TestConnection verifies the API key by sending a minimal request.
func (c *LLMClient) TestConnection(ctx context.Context) error {
	messages := []chatMessage{
		{Role: "user", Content: "Hi"},
	}

	req, err := c.buildRequest(ctx, messages, false)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// ── Request building ──

func (c *LLMClient) buildRequest(ctx context.Context, messages []chatMessage, stream bool) (*http.Request, error) {
	switch c.apiFormat {
	case "anthropic-messages":
		return c.buildAnthropicRequest(ctx, messages, stream)
	case "google-generativeai":
		return c.buildGoogleRequest(ctx, messages, stream)
	default: // openai-chat
		return c.buildOpenAIRequest(ctx, messages, stream)
	}
}

// ── OpenAI Chat Completions ──

type openAIChatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Stream      bool          `json:"stream"`
	Temperature float64       `json:"temperature,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
}

type openAIStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

func (c *LLMClient) buildOpenAIRequest(ctx context.Context, messages []chatMessage, stream bool) (*http.Request, error) {
	body := openAIChatRequest{
		Model:       c.model,
		Messages:    messages,
		Stream:      stream,
		Temperature: 0.7,
	}
	if !stream {
		body.MaxTokens = 5
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.openAIChatURL(), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	return req, nil
}

// ── Anthropic Messages ──

type anthropicRequest struct {
	Model     string             `json:"model"`
	Messages  []anthropicMessage `json:"messages"`
	MaxTokens int                `json:"max_tokens"`
	Stream    bool               `json:"stream"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicStreamEvent struct {
	Type  string `json:"type"`
	Delta struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"delta,omitempty"`
}

func (c *LLMClient) buildAnthropicRequest(ctx context.Context, messages []chatMessage, stream bool) (*http.Request, error) {
	// Anthropic doesn't accept "system" as a message role; extract system prompt separately.
	var systemPrompt string
	var apiMsgs []anthropicMessage
	for _, m := range messages {
		if m.Role == "system" {
			systemPrompt = m.Content
			continue
		}
		apiMsgs = append(apiMsgs, anthropicMessage{Role: m.Role, Content: m.Content})
	}

	maxTokens := 4096
	if !stream {
		maxTokens = 16
	}

	type anthropicRequestWithSystem struct {
		Model     string             `json:"model"`
		Messages  []anthropicMessage `json:"messages"`
		MaxTokens int                `json:"max_tokens"`
		Stream    bool               `json:"stream"`
		System    string             `json:"system,omitempty"`
	}

	body := anthropicRequestWithSystem{
		Model:     c.model,
		Messages:  apiMsgs,
		MaxTokens: maxTokens,
		Stream:    stream,
		System:    systemPrompt,
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.anthropicMessagesURL(), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	return req, nil
}

// ── Google Generative AI (Gemini) ──

type googleRequest struct {
	Contents []googleContent `json:"contents"`
}

type googleContent struct {
	Role  string       `json:"role"`
	Parts []googlePart `json:"parts"`
}

type googlePart struct {
	Text string `json:"text"`
}

type googleStreamChunk struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

func (c *LLMClient) buildGoogleRequest(ctx context.Context, messages []chatMessage, stream bool) (*http.Request, error) {
	var contents []googleContent
	for _, m := range messages {
		role := m.Role
		// Google uses "user" and "model" roles
		if role == "assistant" {
			role = "model"
		}
		if role == "system" {
			// Prepend system prompt as a user message for Gemini
			role = "user"
		}
		contents = append(contents, googleContent{
			Role:  role,
			Parts: []googlePart{{Text: m.Content}},
		})
	}

	body := googleRequest{Contents: contents}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	method := "generateContent"
	if stream {
		method = "streamGenerateContent"
	}
	url := fmt.Sprintf("%s/v1beta/models/%s:%s?key=%s", c.baseURL, c.model, method, c.apiKey)
	if stream {
		url += "&alt=sse"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

// ── SSE stream parsing ──

func (c *LLMClient) parseSSEStream(r io.Reader, cb StreamCallback) error {
	switch c.apiFormat {
	case "anthropic-messages":
		return c.parseAnthropicStream(r, cb)
	case "google-generativeai":
		return c.parseGoogleStream(r, cb)
	default:
		return c.parseOpenAIStream(r, cb)
	}
}

func (c *LLMClient) parseOpenAIStream(r io.Reader, cb StreamCallback) error {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk openAIStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				if err := cb(choice.Delta.Content); err != nil {
					return err
				}
			}
		}
	}
	return scanner.Err()
}

func (c *LLMClient) parseAnthropicStream(r io.Reader, cb StreamCallback) error {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		var event anthropicStreamEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		switch event.Type {
		case "content_block_delta":
			if event.Delta.Text != "" {
				if err := cb(event.Delta.Text); err != nil {
					return err
				}
			}
		case "message_stop":
			return nil
		case "error":
			return fmt.Errorf("anthropic stream error: %s", data)
		}
	}
	return scanner.Err()
}

func (c *LLMClient) parseGoogleStream(r io.Reader, cb StreamCallback) error {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		var chunk googleStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		for _, candidate := range chunk.Candidates {
			for _, part := range candidate.Content.Parts {
				if part.Text != "" {
					if err := cb(part.Text); err != nil {
						return err
					}
				}
			}
		}
	}
	return scanner.Err()
}
