package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	pluginpkg "github.com/web-casa/webcasa/internal/plugin"
)

// Tool defines a single tool that the AI can invoke.
type Tool struct {
	Name              string                 `json:"name"`
	Description       string                 `json:"description"`
	Parameters        map[string]interface{} `json:"parameters"` // JSON Schema object
	ReadOnly          bool                   `json:"-"`           // true = no side effects
	NeedsConfirmation bool                   `json:"-"`           // true = requires user approval before execution
	AdminOnly         bool                   `json:"-"`           // true = only admin users can execute this tool
	Handler           ToolHandler            `json:"-"`
}

// ToolHandler executes a tool and returns a result (serialisable to JSON).
type ToolHandler func(ctx context.Context, args json.RawMessage) (interface{}, error)

// ToolCall represents a tool invocation from the LLM.
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // raw JSON string
}

// ToolResult is the outcome of executing a tool call.
type ToolResult struct {
	ToolCallID string      `json:"tool_call_id"`
	Name       string      `json:"name"`
	Content    interface{} `json:"content"`
	Error      string      `json:"error,omitempty"`
}

// ToolRegistry manages available tools.
type ToolRegistry struct {
	mu      sync.RWMutex
	tools   map[string]*Tool
	coreAPI pluginpkg.CoreAPI
	logger  *slog.Logger
	svc     *Service // back-reference for tools that need AI generation (e.g. generate_dockerfile)
}

// NewToolRegistry creates a new tool registry.
func NewToolRegistry(coreAPI pluginpkg.CoreAPI, logger *slog.Logger) *ToolRegistry {
	return &ToolRegistry{
		tools:   make(map[string]*Tool),
		coreAPI: coreAPI,
		logger:  logger,
	}
}

// Register adds a tool to the registry.
func (r *ToolRegistry) Register(tool *Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[tool.Name] = tool
	r.logger.Debug("tool registered", "name", tool.Name)
}

// Get returns a tool by name, or nil if not found.
func (r *ToolRegistry) Get(name string) *Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tools[name]
}

// All returns all registered tools.
func (r *ToolRegistry) All() []*Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*Tool, 0, len(r.tools))
	for _, t := range r.tools {
		result = append(result, t)
	}
	return result
}

// Execute runs a tool by name with the given JSON arguments.
func (r *ToolRegistry) Execute(ctx context.Context, name string, args json.RawMessage) (interface{}, error) {
	tool := r.Get(name)
	if tool == nil {
		return nil, fmt.Errorf("unknown tool: %s", name)
	}

	r.logger.Info("executing tool", "name", name)
	result, err := tool.Handler(ctx, args)
	if err != nil {
		r.logger.Warn("tool execution failed", "name", name, "err", err)
		return nil, err
	}
	return result, nil
}

// OpenAIToolSchema returns tools in OpenAI function calling format.
func (r *ToolRegistry) OpenAIToolSchema() []map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]map[string]interface{}, 0, len(r.tools))
	for _, t := range r.tools {
		params := t.Parameters
		if params == nil {
			params = map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			}
		}
		result = append(result, map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  params,
			},
		})
	}
	return result
}

// AnthropicToolSchema returns tools in Anthropic tool use format.
func (r *ToolRegistry) AnthropicToolSchema() []map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]map[string]interface{}, 0, len(r.tools))
	for _, t := range r.tools {
		schema := t.Parameters
		if schema == nil {
			schema = map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			}
		}
		result = append(result, map[string]interface{}{
			"name":         t.Name,
			"description":  t.Description,
			"input_schema": schema,
		})
	}
	return result
}
