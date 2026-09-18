// Package agents contains the AI agent layer of the todo application.
//
// AGENT ARCHITECTURE
// ==================
// We have 3 agents:
//
//   Orchestrator  — receives the user's intent, decides which sub-agent handles it,
//                   maintains session context across the conversation
//
//   AuthAgent     — handles login / logout / register decisions
//                   (the actual HTTP handling is still in Go; the agent decides
//                    what to do and the handler executes it)
//
//   BackendAgent  — handles todo CRUD: create, list, complete, delete
//                   uses "tool calls" so Claude can invoke your Go functions
//
// HOW A TOOL CALL WORKS (important concept)
// ==========================================
// Instead of Claude returning plain text, you tell it "here are the functions
// you can call". Claude then returns a structured request to call one of them.
// Your Go code executes the function and sends the result back to Claude.
// Claude then produces a final natural-language reply.
//
//   User: "add buy milk to my todos"
//   ↓
//   BackendAgent → Claude API (with tool: create_todo)
//   ↓
//   Claude: { tool_call: "create_todo", args: { title: "Buy milk", priority: "medium" } }
//   ↓
//   Your Go code: db.CreateTodo(ctx, ...)  ← actual database write
//   ↓
//   Claude (with tool result): "Done! I've added 'Buy milk' to your todos."
//
// This is the key insight: Claude decides WHAT to do, Go executes it.

package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/MirajHoque/todo-app/internal/agents/memory"
	"github.com/MirajHoque/todo-app/internal/logger"
)

// AnthropicBaseURL is the endpoint for Claude messages.
const AnthropicBaseURL = "https://api.anthropic.com/v1/messages"

// AnthropicVersion is the API version header value required by Anthropic.
const AnthropicVersion = "2023-06-01"

// ---- API types (minimal — only what we need) --------------------------------

// APIMessage maps to the Anthropic API messages array entry.
type APIMessage struct {
	Role    string `json:"role"`    //sender role user or assistant
	Content any    `json:"content"` // (msg body) string or complex slice of content blocks|data with images etc
}

// ContentBlock is used when content is structured (tool use / tool result).
type ContentBlock struct {
	Type      string          `json:"type"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

// Tool describes a function Claude can call.
type Tool struct {
	Name        string         `json:"name"`        // function name the model uses to execute this tool.
	Description string         `json:"description"` // Explanatory text telling the model when and how to use the tool.
	InputSchema map[string]any `json:"input_schema"`
}

// APIRequest is the full body sent to the Anthropic messages endpoint.
type APIRequest struct {
	Model     string       `json:"model"` // claude-3-5-sonnet-20241022
	MaxTokens int          `json:"max_tokens"`
	System    string       `json:"system,omitempty"` // System instructions directing the model's tone or behavior.
	Messages  []APIMessage `json:"messages"`         // sequence of conversation messages.
	Tools     []Tool       `json:"tools,omitempty"`  // tools avaiable for the model to invoke
}

// APIResponse is the relevant part of the Anthropic response.
type APIResponse struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Role       string         `json:"role"`        // Role of the sender(assistant)
	Content    []ContentBlock `json:"content"`     // Slice of generated content blocks (text responses, tool requests, etc.).
	StopReason string         `json:"stop_reason"` // "end_turn" | "tool_use" | "max_tokens"
	Usage      Usage          `json:"usage"`
}

// Usage tracks token consumption — useful for cost monitoring in Grafana.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// TextContent extracts the plain text from all text blocks in the response.
func (r *APIResponse) TextContent() string {
	var buf bytes.Buffer
	for _, block := range r.Content {
		if block.Type == "text" {
			buf.WriteString(block.Content)
		}
	}
	return buf.String()
}

// ToolUseBlock returns the first tool_use block, or nil if none.
func (r *APIResponse) ToolUseBlock() *ContentBlock {
	for i := range r.Content {
		if r.Content[i].Type == "tool_use" {
			return &r.Content[i]
		}
	}
	return nil
}

// ---- Base client ------------------------------------------------------------

// Client is the shared HTTP client for all agents.
// All agents embed this so they share the same transport and logging.
type Client struct {
	apiKey     string
	model      string
	httpClient *http.Client
	log        *slog.Logger
}

// NewClient creates a reusable Anthropic API client.
func NewClient(apiKey, model string, log *slog.Logger) *Client {
	return &Client{
		apiKey: apiKey,
		model:  model,
		httpClient: &http.Client{
			Timeout: 60 * time.Second, // LLM calls can be slow
		},
		log: log,
	}
}

// Call sends a request to the Claude API and returns the response.
// This is the single place where all HTTP happens — every agent calls this.
//
// We log:
//   - the agent name, action, and token usage on every call (for Grafana)
//   - any errors with full context (for Loki alerting)
func (c *Client) Call(ctx context.Context, agentName string, req APIRequest) (*APIResponse, error) {
	// Fallback & Set Up
	req.Model = c.model
	if req.MaxTokens == 0 {
		req.MaxTokens = 1024
	}

	start := time.Now()
	log := logger.FromContext(ctx).With(
		slog.String(logger.FieldAgent, agentName),
	)

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("agent %s: marshal request: %w", agentName, err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, AnthropicBaseURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("agent %s: build request: %w", agentName, err)
	}

	// Attach 3 mandatory header required by Antrophic
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", AnthropicVersion)

	// Execution and Network Handling
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		log.Error("agent api call failed",
			slog.String(logger.FieldError, err.Error()),
		)
		return nil, fmt.Errorf("agent %s: http call: %w", agentName, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("agent %s: read response: %w", agentName, err)
	}

	// Error Status Check
	if resp.StatusCode != http.StatusOK {
		log.Error("agent api error response",
			slog.Int("http_status", resp.StatusCode),
			slog.String("body", string(respBody)),
		)
		return nil, fmt.Errorf("agent %s: api returned %d: %s", agentName, resp.StatusCode, string(respBody))
	}

	// Unmarshing & Logging Success
	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("agent %s: unmarshal response: %w", agentName, err)
	}

	log.Info("agent api call completed",
		slog.String("stop_reason", apiResp.StopReason),
		slog.Int("input_tokens", apiResp.Usage.InputTokens),
		slog.Int("output_tokens", apiResp.Usage.OutputTokens),
		slog.Int64(logger.FieldDuration, time.Since(start).Milliseconds()),
	)

	return &apiResp, nil
}

// ---- Memory → APIMessage conversion -----------------------------------------

// MessagesToAPI converts our internal memory.Message slice to the format
// the Anthropic API expects. Call this before every agent API call.
func MessagesToAPI(msgs []memory.Message) []APIMessage {
	out := make([]APIMessage, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, APIMessage{
			Role:    string(m.Role),
			Content: m.Content,
		})
	}
	return out
}
