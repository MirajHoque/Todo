// Package memory manages conversation history for each AI agent.
//
// WHY MEMORY MATTERS IN AGENTIC APPS
// ====================================
// Claude has no memory between API calls. Every call is stateless.
// If you don't send the conversation history, the agent "forgets" what
// it already decided, what tools it already called, and what the user said.
//
// This is the most common mistake in agentic development:
//
//	BAD:  agent.Call(ctx, "create a todo called Buy milk")
//	      agent.Call(ctx, "mark it done")   ← agent has no idea what "it" is
//
//	GOOD: memory.Add(userMsg)
//	      memory.Add(agentReply)
//	      agent.Call(ctx, memory.Messages(), "mark it done")  ← works
//
// MEMORY DESIGN
// =============
// We keep ONE memory store per (userID, agentName) pair.
// Each agent has its own history — the auth agent doesn't need to know
// about todo CRUD operations and vice versa.
//
// The orchestrator has its OWN memory so it can maintain session context
// (what has the user been doing this session? what's their last action?).
//
// MEMORY LIMITS
// =============
// LLMs have a context window limit (tokens). Sending 10,000 past messages
// is expensive and slow. We keep the last N turns and optionally summarise
// older turns into a single "session summary" message.
//
// For this app we use a simple sliding window of MaxTurns turns.
// A "turn" = one user message + one assistant message = 2 Message objects.
package memory

import (
	"fmt"
	"sync"
	"time"
)

// Role mirrors the Anthropic API role values.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message is a single entry in the conversation history.
// It maps directly to what the Anthropic API expects in the messages array.
type Message struct {
	Role      Role      `json:"role"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

// Store holds the conversation history for one (user, agent) pair.
type Store struct {
	mu       sync.RWMutex
	messages []Message
	maxTurns int // keep this many user+assistant pairs
}

// NewStore creates a new memory store with a sliding window of maxTurns turns.
// A turn = 1 user message + 1 assistant message.
// For a simple todo app, 10 turns (20 messages) is plenty.
func NewStore(maxTurns int) *Store {
	if maxTurns <= 0 {
		maxTurns = 10
	}
	return &Store{
		messages: make([]Message, 0, maxTurns*2),
		maxTurns: maxTurns,
	}
}

// Add appends a message to the store and trims old messages if over the limit.
func (s *Store) Add(role Role, content string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.messages = append(s.messages, Message{
		Role:      role,
		Content:   content,
		Timestamp: time.Now().UTC(),
	})

	// Trim to maxTurns. Each turn = 2 messages (user + assistant).
	// We always trim from the front (oldest first), but we preserve the
	// first message if it's a system context injection (not implemented here,
	// system prompts go in the API call directly, not in the messages array).
	maxMessages := s.maxTurns * 2
	if len(s.messages) > maxMessages {
		s.messages = s.messages[len(s.messages)-maxMessages:]
	}
}

// Messages returns a copy of the current conversation history.
// The copy protects against mutation while the caller is reading it.
func (s *Store) Messages() []Message {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Message, len(s.messages))
	copy(out, s.messages)
	return out
}

// Len returns the number of messages currently stored.
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.messages)
}

// Clear wipes all history. Call this on logout so the next login starts fresh.
func (s *Store) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = s.messages[:0]
}

// LastAssistantMessage returns the most recent assistant reply, or "" if none.
// Useful for the orchestrator to check what the last agent said.
func (s *Store) LastAssistantMessage() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for i := len(s.messages) - 1; i >= 0; i-- {
		if s.messages[i].Role == RoleAssistant {
			return s.messages[i].Content
		}
	}
	return ""
}

// ---- Manager ----------------------------------------------------------------

// Manager holds memory stores for all active (user, agent) combinations.
// It's safe for concurrent use — multiple HTTP requests can access it at once.
//
// In a production system you'd persist this to Redis or PostgreSQL so memory
// survives a server restart. For now we keep it in-process RAM, which is fine
// for a single-node deployment.
type Manager struct {
	mu       sync.RWMutex
	stores   map[string]*Store // key: "userID:agentName"
	maxTurns int
}

// NewManager creates a Manager. maxTurns controls the sliding window size
// for every store it creates.
func NewManager(maxTurns int) *Manager {
	return &Manager{
		stores:   make(map[string]*Store),
		maxTurns: maxTurns,
	}
}

// Get returns the memory store for (userID, agentName), creating it if needed.
func (m *Manager) Get(userID, agentName string) *Store {
	key := storeKey(userID, agentName)

	// Fast path: store already exists.
	m.mu.RLock()
	if s, ok := m.stores[key]; ok {
		m.mu.RUnlock()
		return s
	}
	m.mu.RUnlock()

	// Slow path: create new store.
	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock (another goroutine may have created it).
	if s, ok := m.stores[key]; ok {
		return s
	}

	s := NewStore(m.maxTurns)
	m.stores[key] = s
	return s
}

// ClearUser deletes all memory stores for a user (call on logout).
func (m *Manager) ClearUser(userID string) {
	prefix := userID + ":"

	m.mu.Lock()
	defer m.mu.Unlock()

	for key := range m.stores {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			delete(m.stores, key)
		}
	}
}

func storeKey(userID, agentName string) string {
	return fmt.Sprintf("%s:%s", userID, agentName)
}
