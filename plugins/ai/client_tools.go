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
)

// StreamEvent represents an event from the LLM stream during tool use.
type StreamEvent struct {
	Type     string    // "delta", "tool_call", "tool_result", "confirm_required", "done"
	Content  string    // text content for delta events
	ToolCall *ToolCall // for tool_call events (complete, after accumulation)
}

// PendingConfirmation holds info about a tool call awaiting user approval.
type PendingConfirmation struct {
	PendingID string                 `json:"pending_id"`
	ToolName  string                 `json:"tool_name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// StreamEventCallback receives events from a tool-use stream.
type StreamEventCallback func(event StreamEvent) error

// ToolUseMessage is a flexible message type for multi-turn tool use conversations.
type ToolUseMessage struct {
	Role       string     // "system", "user", "assistant", "tool"
	Content    string     // text content
	ToolCalls  []ToolCall // assistant's tool calls (when Role == "assistant")
	ToolCallID string     // tool result's call ID (when Role == "tool")
	ToolName   string     // tool name (when Role == "tool")
}

// ChatStreamWithTools sends a streaming request with tool definitions.
// The callback receives StreamEvents: "delta" for text, "tool_call" for tool invocations, "done" when finished.
func (c *LLMClient) ChatStreamWithTools(
	ctx context.Context,
	messages []ToolUseMessage,
	tools []map[string]interface{},
	cb StreamEventCallback,
) error {
	switch c.apiFormat {
	case "anthropic-messages":
		return c.chatToolsAnthropic(ctx, messages, tools, cb)
	default: // openai-chat (also covers compatible providers)
		return c.chatToolsOpenAI(ctx, messages, tools, cb)
	}
}

// ──────────────────────────────────────────────────
// OpenAI Tool Use
// ──────────────────────────────────────────────────

type openAIToolRequest struct {
	Model       string                   `json:"model"`
	Messages    []json.RawMessage        `json:"messages"`
	Tools       []map[string]interface{} `json:"tools,omitempty"`
	Stream      bool                     `json:"stream"`
	Temperature float64                  `json:"temperature,omitempty"`
}

// openAIToolChunk extends the basic chunk to include tool_calls in delta.
type openAIToolChunk struct {
	Choices []struct {
		Delta struct {
			Content   string                 `json:"content"`
			ToolCalls []openAIToolCallDelta  `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

type openAIToolCallDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

func (c *LLMClient) chatToolsOpenAI(ctx context.Context, messages []ToolUseMessage, tools []map[string]interface{}, cb StreamEventCallback) error {
	// Convert messages to OpenAI format.
	apiMsgs := make([]json.RawMessage, 0, len(messages))
	for _, m := range messages {
		raw, err := c.toolMsgToOpenAI(m)
		if err != nil {
			return fmt.Errorf("convert message: %w", err)
		}
		apiMsgs = append(apiMsgs, raw)
	}

	body := openAIToolRequest{
		Model:       c.model,
		Messages:    apiMsgs,
		Tools:       tools,
		Stream:      true,
		Temperature: 0.7,
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	return c.parseOpenAIToolStream(resp.Body, cb)
}

func (c *LLMClient) toolMsgToOpenAI(m ToolUseMessage) (json.RawMessage, error) {
	switch {
	case m.Role == "tool":
		// Tool result message
		msg := map[string]interface{}{
			"role":         "tool",
			"tool_call_id": m.ToolCallID,
			"content":      m.Content,
		}
		return json.Marshal(msg)

	case m.Role == "assistant" && len(m.ToolCalls) > 0:
		// Assistant message with tool calls
		calls := make([]map[string]interface{}, len(m.ToolCalls))
		for i, tc := range m.ToolCalls {
			calls[i] = map[string]interface{}{
				"id":   tc.ID,
				"type": "function",
				"function": map[string]interface{}{
					"name":      tc.Name,
					"arguments": tc.Arguments,
				},
			}
		}
		msg := map[string]interface{}{
			"role":       "assistant",
			"content":    nil,
			"tool_calls": calls,
		}
		if m.Content != "" {
			msg["content"] = m.Content
		}
		return json.Marshal(msg)

	default:
		// Regular text message
		msg := map[string]interface{}{
			"role":    m.Role,
			"content": m.Content,
		}
		return json.Marshal(msg)
	}
}

func (c *LLMClient) parseOpenAIToolStream(r io.Reader, cb StreamEventCallback) error {
	scanner := bufio.NewScanner(r)
	// Increase scanner buffer for large responses
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	// Accumulate tool calls (index → ToolCall)
	toolCalls := make(map[int]*ToolCall)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk openAIToolChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		for _, choice := range chunk.Choices {
			// Text content delta
			if choice.Delta.Content != "" {
				if err := cb(StreamEvent{Type: "delta", Content: choice.Delta.Content}); err != nil {
					return err
				}
			}

			// Tool call deltas (accumulated)
			for _, tcd := range choice.Delta.ToolCalls {
				tc, ok := toolCalls[tcd.Index]
				if !ok {
					tc = &ToolCall{}
					toolCalls[tcd.Index] = tc
				}
				if tcd.ID != "" {
					tc.ID = tcd.ID
				}
				if tcd.Function.Name != "" {
					tc.Name = tcd.Function.Name
				}
				tc.Arguments += tcd.Function.Arguments
			}

			// When finish_reason is "tool_calls", emit accumulated tool calls
			if choice.FinishReason != nil && *choice.FinishReason == "tool_calls" {
				for i := 0; i < len(toolCalls); i++ {
					if tc, ok := toolCalls[i]; ok {
						if err := cb(StreamEvent{Type: "tool_call", ToolCall: tc}); err != nil {
							return err
						}
					}
				}
				return cb(StreamEvent{Type: "done"})
			}

			// Normal stop
			if choice.FinishReason != nil && *choice.FinishReason == "stop" {
				return cb(StreamEvent{Type: "done"})
			}
		}
	}

	// If we get here without a finish event, emit done anyway
	if len(toolCalls) > 0 {
		for i := 0; i < len(toolCalls); i++ {
			if tc, ok := toolCalls[i]; ok {
				if err := cb(StreamEvent{Type: "tool_call", ToolCall: tc}); err != nil {
					return err
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}
	return cb(StreamEvent{Type: "done"})
}

// ──────────────────────────────────────────────────
// Anthropic Tool Use
// ──────────────────────────────────────────────────

type anthropicToolRequest struct {
	Model     string                   `json:"model"`
	Messages  []json.RawMessage        `json:"messages"`
	MaxTokens int                      `json:"max_tokens"`
	Stream    bool                     `json:"stream"`
	System    string                   `json:"system,omitempty"`
	Tools     []map[string]interface{} `json:"tools,omitempty"`
}

// anthropicToolStreamEvent extends the basic event to include tool use blocks.
type anthropicToolStreamEvent struct {
	Type         string `json:"type"`
	Index        int    `json:"index"`
	ContentBlock *struct {
		Type  string `json:"type"`
		ID    string `json:"id,omitempty"`
		Name  string `json:"name,omitempty"`
		Text  string `json:"text,omitempty"`
	} `json:"content_block,omitempty"`
	Delta *struct {
		Type        string `json:"type"`
		Text        string `json:"text,omitempty"`
		PartialJSON string `json:"partial_json,omitempty"`
	} `json:"delta,omitempty"`
}

func (c *LLMClient) chatToolsAnthropic(ctx context.Context, messages []ToolUseMessage, tools []map[string]interface{}, cb StreamEventCallback) error {
	var systemPrompt string
	apiMsgs := make([]json.RawMessage, 0, len(messages))
	for _, m := range messages {
		if m.Role == "system" {
			systemPrompt = m.Content
			continue
		}
		raw, err := c.toolMsgToAnthropic(m)
		if err != nil {
			return fmt.Errorf("convert message: %w", err)
		}
		apiMsgs = append(apiMsgs, raw)
	}

	body := anthropicToolRequest{
		Model:     c.model,
		Messages:  apiMsgs,
		MaxTokens: 4096,
		Stream:    true,
		System:    systemPrompt,
		Tools:     tools,
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/messages", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	return c.parseAnthropicToolStream(resp.Body, cb)
}

func (c *LLMClient) toolMsgToAnthropic(m ToolUseMessage) (json.RawMessage, error) {
	switch {
	case m.Role == "tool":
		// Anthropic: tool results are wrapped in a user message with content blocks
		block := map[string]interface{}{
			"type":        "tool_result",
			"tool_use_id": m.ToolCallID,
			"content":     m.Content,
		}
		msg := map[string]interface{}{
			"role":    "user",
			"content": []interface{}{block},
		}
		return json.Marshal(msg)

	case m.Role == "assistant" && len(m.ToolCalls) > 0:
		// Assistant message with tool use blocks
		blocks := make([]interface{}, 0)
		if m.Content != "" {
			blocks = append(blocks, map[string]interface{}{
				"type": "text",
				"text": m.Content,
			})
		}
		for _, tc := range m.ToolCalls {
			var input interface{}
			if err := json.Unmarshal([]byte(tc.Arguments), &input); err != nil {
				input = map[string]interface{}{}
			}
			blocks = append(blocks, map[string]interface{}{
				"type":  "tool_use",
				"id":    tc.ID,
				"name":  tc.Name,
				"input": input,
			})
		}
		msg := map[string]interface{}{
			"role":    "assistant",
			"content": blocks,
		}
		return json.Marshal(msg)

	default:
		msg := map[string]interface{}{
			"role":    m.Role,
			"content": m.Content,
		}
		return json.Marshal(msg)
	}
}

func (c *LLMClient) parseAnthropicToolStream(r io.Reader, cb StreamEventCallback) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	// Track current content block (could be text or tool_use)
	type blockState struct {
		blockType string
		id        string
		name      string
		text      strings.Builder
		inputJSON strings.Builder
	}

	blocks := make(map[int]*blockState)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		var event anthropicToolStreamEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		switch event.Type {
		case "content_block_start":
			if event.ContentBlock != nil {
				blocks[event.Index] = &blockState{
					blockType: event.ContentBlock.Type,
					id:        event.ContentBlock.ID,
					name:      event.ContentBlock.Name,
				}
			}

		case "content_block_delta":
			bs := blocks[event.Index]
			if bs == nil || event.Delta == nil {
				continue
			}
			switch event.Delta.Type {
			case "text_delta":
				if event.Delta.Text != "" {
					bs.text.WriteString(event.Delta.Text)
					if err := cb(StreamEvent{Type: "delta", Content: event.Delta.Text}); err != nil {
						return err
					}
				}
			case "input_json_delta":
				bs.inputJSON.WriteString(event.Delta.PartialJSON)
			}

		case "content_block_stop":
			bs := blocks[event.Index]
			if bs == nil {
				continue
			}
			if bs.blockType == "tool_use" {
				tc := &ToolCall{
					ID:        bs.id,
					Name:      bs.name,
					Arguments: bs.inputJSON.String(),
				}
				if err := cb(StreamEvent{Type: "tool_call", ToolCall: tc}); err != nil {
					return err
				}
			}
			delete(blocks, event.Index)

		case "message_stop":
			return cb(StreamEvent{Type: "done"})

		case "error":
			return fmt.Errorf("anthropic stream error: %s", data)
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}
	return cb(StreamEvent{Type: "done"})
}
