package llm

import "context"

// Role constants for chat messages.
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

// Message is a single chat turn.
type Message struct {
	Role    string
	Content string
}

// Provider is the common interface all LLM backends must satisfy.
type Provider interface {
	// Chat sends the message history and returns the assistant reply.
	Chat(ctx context.Context, messages []Message) (string, error)
	// Name returns the provider identifier (e.g. "openai").
	Name() string
	// Model returns the model being used.
	Model() string
}
