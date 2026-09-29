// backend_agent.go — the agent that manages todos using tool calls.
//
// HOW THIS AGENT WORKS
// ====================
// 1. Receives a user message like "add buy milk with high priority"
// 2. Sends it to Claude with tool definitions (create_todo, list_todos etc)
// 3. Claude responds with a tool_use block — which function to call and args
// 4. We run the actual Go function (todoRepo.Create etc)
// 5. Send the result back to Claude
// 6. Claude replies with natural language confirmation
// 7. We save both sides to memory and return the reply
//
// The loop runs until Claude returns stop_reason: "end_turn"
// meaning it is done calling tools and has a final answer.
package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	agentmemory "github.com/MirajHoque/todo-app/internal/agents/memory"
	"github.com/MirajHoque/todo-app/internal/db"
	"github.com/MirajHoque/todo-app/internal/logger"
	"github.com/MirajHoque/todo-app/internal/models"
)

const backendAgentName = "backend"

// BackendAgent handles all todo-related conversations.
type BackendAgent struct {
	client *Client
	todos  *db.TodoRepository
	memory *agentmemory.Manager
}

// NewBackendAgent creates the backend agent.
func NewBackendAgent(client *Client, todos *db.TodoRepository, memory *agentmemory.Manager) *BackendAgent {
	return &BackendAgent{
		client: client,
		todos:  todos,
		memory: memory,
	}
}

// ── Tool definitions ──────────────────────────────────────────────────────────
// These are sent to Claude so it knows what functions exist.
// Claude reads the description to decide WHEN to call each one.
// Claude reads the input_schema to know WHAT arguments to pass.

func (a *BackendAgent) tools() []Tool {
	return []Tool{
		{
			Name:        "create_todo",
			Description: "Creates a new todo item for the user. Use this when the user wants to add, create, or remember something.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title": map[string]any{
						"type":        "string",
						"description": "The todo title — what needs to be done",
					},
					"priority": map[string]any{
						"type":        "string",
						"enum":        []string{"low", "medium", "high"},
						"description": "How urgent this todo is. Default to medium if not specified.",
					},
					"description": map[string]any{
						"type":        "string",
						"description": "Optional extra detail about the todo",
					},
				},
				"required": []string{"title"},
			},
		},
		{
			Name:        "list_todos",
			Description: "Lists all todos for the user. Use this when the user asks what they have to do, wants to see their list, or asks about pending tasks.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "complete_todo",
			Description: "Marks a todo as done. Use this when the user says they finished, completed, or did something.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"todo_id": map[string]any{
						"type":        "string",
						"description": "The ID of the todo to mark as done",
					},
				},
				"required": []string{"todo_id"},
			},
		},
		{
			Name:        "delete_todo",
			Description: "Permanently deletes a todo. Use this when the user wants to remove or delete something from their list.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"todo_id": map[string]any{
						"type":        "string",
						"description": "The ID of the todo to delete",
					},
				},
				"required": []string{"todo_id"},
			},
		},
	}
}

// ── System prompt ─────────────────────────────────────────────────────────────
// This tells Claude what role it plays and how to behave.
// It is sent on every API call as the "system" field.

func backendSystemPrompt(userID string) string {
	return fmt.Sprintf(`You are a helpful todo assistant. You help the user manage their todo list.

User ID: %s

Guidelines:
- Be concise and friendly. Confirm actions clearly.
- When the user mentions something they need to do, add it as a todo.
- When listing todos, format them clearly with their priority and status.
- If a todo is not found, explain it and offer to list existing todos.
- Always use the tools to actually perform actions — don't just say you did it.
- If the user says "it" or "that", refer to the most recent todo mentioned.`, userID)
}

// ── Run ───────────────────────────────────────────────────────────────────────

// Run processes a user message and returns the agent's reply.
// This is the main loop — it keeps calling Claude until stop_reason is "end_turn".
func (a *BackendAgent) Run(ctx context.Context, userID, userMessage string) (string, error) {
	log := logger.FromContext(ctx).With(
		slog.String(logger.FieldAgent, backendAgentName),
		slog.String(logger.FieldUserID, userID),
	)

	// Get this user's memory store for the backend agent
	// load the conversation from the memory
	store := a.memory.Get(userID, backendAgentName)

	// Add the new user message to memory
	store.Add(agentmemory.RoleUser, userMessage)

	// Build the messages array from memory
	// This is the conversation history Claude needs to understand context
	apiMessages := MessagesToAPI(store.Messages())

	// ── Tool call loop ────────────────────────────────────────────────────
	// We loop because Claude may call multiple tools before giving
	// the final answer. Each tool call → we run the function → send result back.
	for {
		// Call Claude API
		resp, err := a.client.Call(ctx, backendAgentName, APIRequest{
			MaxTokens: 1024,
			System:    backendSystemPrompt(userID),
			Messages:  apiMessages,
			Tools:     a.tools(),
		})
		if err != nil {
			return "", fmt.Errorf("backend agent: api call: %w", err)
		}

		// ── Claude is done — return final text ────────────────────────────
		if resp.StopReason == "end_turn" {
			reply := resp.TextContent()

			// Save assistant reply to memory so next message has context
			store.Add(agentmemory.RoleAssistant, reply)

			log.Info("backend agent completed",
				slog.String(logger.FieldAction, "agent_done"),
				slog.Int("memory_len", store.Len()),
			)

			return reply, nil
		}

		// ── Claude wants to call a tool ───────────────────────────────────
		if resp.StopReason == "tool_use" {
			toolBlock := resp.ToolUseBlock()
			if toolBlock == nil {
				return "", fmt.Errorf("backend agent: tool_use stop but no tool block")
			}

			log.Info("tool call requested",
				slog.String(logger.FieldAction, "tool_call"),
				slog.String("tool", toolBlock.Name),
			)

			// Run the Go function Claude requested
			toolResult, err := a.executeTool(ctx, userID, toolBlock)
			if err != nil {
				log.Error("tool execution failed",
					slog.String(logger.FieldError, err.Error()),
					slog.String("tool", toolBlock.Name),
				)
				toolResult = fmt.Sprintf("error: %s", err.Error())
			}

			// Add Claude's tool_use response + our tool_result to the messages
			// Claude needs both to continue the conversation
			apiMessages = append(apiMessages,
				// What Claude said (the tool_use request)
				APIMessage{
					Role:    "assistant",
					Content: resp.Content,
				},
				// What we got back from running the tool
				APIMessage{
					Role: "user",
					Content: []ContentBlock{
						{
							Type:      "tool_result",
							ToolUseID: toolBlock.ID,
							Content:   toolResult,
						},
					},
				},
			)

			// Loop — Claude will now read the tool result and either
			// call another tool or give the final text answer
			continue
		}

		// Unexpected stop reason
		return "", fmt.Errorf("backend agent: unexpected stop_reason: %s", resp.StopReason)
	}
}

// ── Tool execution ────────────────────────────────────────────────────────────
// This is where Claude's tool call request becomes a real Go function call.
// Claude decides WHAT to call. We decide HOW to call it.

func (a *BackendAgent) executeTool(ctx context.Context, userID string, block *ContentBlock) (string, error) {
	switch block.Name {

	case "create_todo":
		var args struct {
			Title       string `json:"title"`
			Priority    string `json:"priority"`
			Description string `json:"description"`
		}
		if err := json.Unmarshal(block.Input, &args); err != nil {
			return "", fmt.Errorf("parse create_todo args: %w", err)
		}
		if args.Priority == "" {
			args.Priority = "medium"
		}

		todo, err := a.todos.Create(ctx, userID, &models.CreateTodoRequest{
			Title:       args.Title,
			Description: args.Description,
			Priority:    models.Priority(args.Priority),
		})
		if err != nil {
			return "", fmt.Errorf("create todo: %w", err)
		}

		return fmt.Sprintf("Created todo: id=%s title=%q priority=%s", todo.ID, todo.Title, todo.Priority), nil

	case "list_todos":
		todos, err := a.todos.ListByUser(ctx, userID)
		if err != nil {
			return "", fmt.Errorf("list todos: %w", err)
		}

		if len(todos) == 0 {
			return "No todos found. The list is empty.", nil
		}

		result := fmt.Sprintf("Found %d todos:\n", len(todos))
		for _, t := range todos {
			status := "pending"
			if t.Done {
				status = "done"
			}
			result += fmt.Sprintf("- id=%s title=%q priority=%s status=%s\n",
				t.ID, t.Title, t.Priority, status)
		}
		return result, nil

	case "complete_todo":
		var args struct {
			TodoID string `json:"todo_id"`
		}
		if err := json.Unmarshal(block.Input, &args); err != nil {
			return "", fmt.Errorf("parse complete_todo args: %w", err)
		}

		done := true
		todo, err := a.todos.Update(ctx, args.TodoID, userID, &models.UpdateTodoRequest{
			Done: &done,
		})
		if err != nil {
			return "", fmt.Errorf("complete todo: %w", err)
		}

		return fmt.Sprintf("Marked todo %q as done", todo.Title), nil

	case "delete_todo":
		var args struct {
			TodoID string `json:"todo_id"`
		}
		if err := json.Unmarshal(block.Input, &args); err != nil {
			return "", fmt.Errorf("parse delete_todo args: %w", err)
		}

		if err := a.todos.Delete(ctx, args.TodoID, userID); err != nil {
			return "", fmt.Errorf("delete todo: %w", err)
		}

		return fmt.Sprintf("Deleted todo with id=%s", args.TodoID), nil

	default:
		return "", fmt.Errorf("unknown tool: %s", block.Name)
	}
}
