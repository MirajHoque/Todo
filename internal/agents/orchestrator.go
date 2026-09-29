// orchestrator.go — the entry point for all agent conversations.
//
// WHY AN ORCHESTRATOR?
// ====================
// Instead of the handler calling agents directly, all messages go through
// the orchestrator first. The orchestrator decides which agent handles it.
//
// Right now we only have one agent (backend). As the app grows:
//   - Auth questions  → auth agent
//   - Todo operations → backend agent
//   - General chat    → orchestrator handles directly
//
// The orchestrator also maintains its own memory — session context like
// "what has the user been doing this session" that helps it route correctly.
package agents

import (
	"context"
	"fmt"
	"log/slog"

	agentmemory "github.com/MirajHoque/todo-app/internal/agents/memory"
	"github.com/MirajHoque/todo-app/internal/db"
	"github.com/MirajHoque/todo-app/internal/logger"
)

const orchestratorAgentName = "orchestrator"

// Orchestrator routes user messages to the appropriate agent.
type Orchestrator struct {
	client       *Client
	memory       *agentmemory.Manager
	backendAgent *BackendAgent
}

// NewOrchestrator creates the orchestrator with all sub-agents wired in.
func NewOrchestrator(
	client *Client,
	memory *agentmemory.Manager,
	todos *db.TodoRepository,
) *Orchestrator {
	return &Orchestrator{
		client:       client,
		memory:       memory,
		backendAgent: NewBackendAgent(client, todos, memory),
	}
}

// Handle receives a user message and routes it to the right agent.
// This is the single entry point called by the HTTP handler.
func (o *Orchestrator) Handle(ctx context.Context, userID, message string) (string, error) {
	log := logger.FromContext(ctx).With(
		slog.String(logger.FieldAgent, orchestratorAgentName),
		slog.String(logger.FieldUserID, userID),
	)

	log.Info("orchestrator received message",
		slog.String(logger.FieldAction, "orchestrator_route"),
		slog.Int("message_len", len(message)),
	)

	// For now all messages go to the backend agent.
	// In a larger app you would classify the message first:
	//
	//   intent := classifyIntent(message)
	//   switch intent {
	//   case "auth":    return o.authAgent.Run(ctx, userID, message)
	//   case "todos":   return o.backendAgent.Run(ctx, userID, message)
	//   default:        return o.handleDirectly(ctx, userID, message)
	//   }
	//
	// We keep it simple here — the backend agent handles everything
	// and its system prompt guides it to respond appropriately.

	reply, err := o.backendAgent.Run(ctx, userID, message)
	if err != nil {
		log.Error("agent failed",
			slog.String(logger.FieldError, err.Error()),
			slog.String(logger.FieldAction, "orchestrator_fail"),
		)
		return "", fmt.Errorf("orchestrator: %w", err)
	}

	log.Info("orchestrator completed",
		slog.String(logger.FieldAction, "orchestrator_done"),
	)

	return reply, nil
}

// ClearMemory wipes all agent memory for a user.
// Called on logout so the next session starts fresh.
func (o *Orchestrator) ClearMemory(userID string) {
	o.memory.ClearUser(userID)
}
