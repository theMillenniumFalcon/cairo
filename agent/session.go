package agent

import (
	"fmt"

	"github.com/themillenniumfalcon/cairo/db"
	"github.com/themillenniumfalcon/cairo/llm"
	"github.com/themillenniumfalcon/cairo/tools"
)

const systemPrompt = `You are Cairo, a personal AI agent. You are helpful, concise, and direct.`

// Session holds an active conversation: its DB record + in-memory message history.
type Session struct {
	Record  *db.Session
	History []llm.Message
	store   *db.DB
}

// LoadOrCreate loads a named session from the DB (creating it if new)
// and reconstructs the in-memory message history.
// The registry is used to build the system prompt with the tool block.
func LoadOrCreate(store *db.DB, registry *tools.Registry, name, provider, model string) (*Session, bool, error) {
	record, isNew, err := store.GetOrCreateSession(name, provider, model)
	if err != nil {
		return nil, false, fmt.Errorf("session: %w", err)
	}

	s := &Session{
		Record: record,
		store:  store,
		History: []llm.Message{
			{Role: llm.RoleSystem, Content: BuildSystemPrompt(registry)},
		},
	}

	if !isNew {
		msgs, err := store.GetMessages(record.ID)
		if err != nil {
			return nil, false, fmt.Errorf("session: load messages: %w", err)
		}
		for _, m := range msgs {
			s.History = append(s.History, llm.Message{
				Role:    m.Role,
				Content: m.Content,
			})
		}
	}

	return s, isNew, nil
}

// Add appends a message to both the in-memory history and the database.
func (s *Session) Add(role, content string) error {
	s.History = append(s.History, llm.Message{Role: role, Content: content})
	_, err := s.store.AddMessage(s.Record.ID, role, content)
	return err
}

// MessageCount returns the number of non-system messages in history.
func (s *Session) MessageCount() int {
	n := len(s.History)
	if n > 0 && s.History[0].Role == llm.RoleSystem {
		n--
	}
	return n
}

// ClearHistory wipes all messages from DB and resets in-memory history to
// just the system prompt.
func (s *Session) ClearHistory() error {
	if err := s.store.ClearMessages(s.Record.ID); err != nil {
		return err
	}
	if len(s.History) > 0 && s.History[0].Role == llm.RoleSystem {
		s.History = s.History[:1]
	} else {
		s.History = nil
	}
	return nil
}
