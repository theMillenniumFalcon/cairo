// Package db provides a simple persistent store for Cairo sessions and messages.
// It uses a single JSON file (~/.cairo/cairo.json) — no CGo, no external deps,
// works on Windows, macOS, and Linux out of the box.
package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ── public types ──────────────────────────────────────────────────────────────

// Session is a named conversation thread.
type Session struct {
	ID        int64
	Name      string
	Provider  string
	Model     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Message is a single turn stored in a session.
type Message struct {
	ID        int64
	SessionID int64
	Role      string
	Content   string
	CreatedAt time.Time
}

// ── internal file schema ──────────────────────────────────────────────────────

type fileSession struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	Provider  string     `json:"provider"`
	Model     string     `json:"model"`
	CreatedAt int64      `json:"created_at"`
	UpdatedAt int64      `json:"updated_at"`
	Messages  []*fileMsg `json:"messages"`
}

type fileMsg struct {
	ID        int64  `json:"id"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	CreatedAt int64  `json:"created_at"`
}

type fileStore struct {
	NextSessionID int64          `json:"next_session_id"`
	NextMsgID     int64          `json:"next_msg_id"`
	Sessions      []*fileSession `json:"sessions"`
}

// ── DB ────────────────────────────────────────────────────────────────────────

// DB is the persistent store.
type DB struct {
	mu   sync.Mutex
	path string
	data fileStore
}

// Open opens (or creates) the JSON store at path.
// Pass "" to use the default ~/.cairo/cairo.json.
func Open(path string) (*DB, error) {
	if path == "" {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, ".cairo", "cairo.json")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("db: mkdir: %w", err)
	}

	d := &DB{path: path}
	d.data.NextSessionID = 1
	d.data.NextMsgID = 1

	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("db: read: %w", err)
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &d.data); err != nil {
			return nil, fmt.Errorf("db: parse store: %w", err)
		}
	}
	return d, nil
}

// Close is a no-op (data is flushed on every write).
func (d *DB) Close() error { return nil }

// ── internal helpers ──────────────────────────────────────────────────────────

func (d *DB) save() error {
	raw, err := json.MarshalIndent(&d.data, "", "  ")
	if err != nil {
		return fmt.Errorf("db: marshal: %w", err)
	}
	tmp := d.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0600); err != nil {
		return fmt.Errorf("db: write tmp: %w", err)
	}
	return os.Rename(tmp, d.path)
}

func (d *DB) findByName(name string) *fileSession {
	for _, s := range d.data.Sessions {
		if s.Name == name {
			return s
		}
	}
	return nil
}

func (d *DB) findByID(id int64) *fileSession {
	for _, s := range d.data.Sessions {
		if s.ID == id {
			return s
		}
	}
	return nil
}

func toSession(fs *fileSession) *Session {
	return &Session{
		ID:        fs.ID,
		Name:      fs.Name,
		Provider:  fs.Provider,
		Model:     fs.Model,
		CreatedAt: time.Unix(fs.CreatedAt, 0),
		UpdatedAt: time.Unix(fs.UpdatedAt, 0),
	}
}

// ── Session operations ────────────────────────────────────────────────────────

// CreateSession inserts a new session and returns it.
func (d *DB) CreateSession(name, provider, model string) (*Session, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.findByName(name) != nil {
		return nil, fmt.Errorf("session %q already exists", name)
	}

	now := time.Now().Unix()
	fs := &fileSession{
		ID:        d.data.NextSessionID,
		Name:      name,
		Provider:  provider,
		Model:     model,
		CreatedAt: now,
		UpdatedAt: now,
	}
	d.data.NextSessionID++
	d.data.Sessions = append(d.data.Sessions, fs)

	if err := d.save(); err != nil {
		return nil, err
	}
	return toSession(fs), nil
}

// GetOrCreateSession loads a session by name, creating it if missing.
// Returns (session, isNew, error).
func (d *DB) GetOrCreateSession(name, provider, model string) (*Session, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if fs := d.findByName(name); fs != nil {
		return toSession(fs), false, nil
	}

	now := time.Now().Unix()
	fs := &fileSession{
		ID:        d.data.NextSessionID,
		Name:      name,
		Provider:  provider,
		Model:     model,
		CreatedAt: now,
		UpdatedAt: now,
	}
	d.data.NextSessionID++
	d.data.Sessions = append(d.data.Sessions, fs)

	if err := d.save(); err != nil {
		return nil, false, err
	}
	return toSession(fs), true, nil
}

// GetSessionByName looks up a session by name.
func (d *DB) GetSessionByName(name string) (*Session, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	fs := d.findByName(name)
	if fs == nil {
		return nil, sql.ErrNoRows
	}
	return toSession(fs), nil
}

// GetSessionByID looks up a session by ID.
func (d *DB) GetSessionByID(id int64) (*Session, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	fs := d.findByID(id)
	if fs == nil {
		return nil, sql.ErrNoRows
	}
	return toSession(fs), nil
}

// ListSessions returns all sessions ordered by most recently updated.
func (d *DB) ListSessions() ([]*Session, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Copy and sort descending by updated_at
	sorted := make([]*fileSession, len(d.data.Sessions))
	copy(sorted, d.data.Sessions)
	for i := 0; i < len(sorted)-1; i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].UpdatedAt > sorted[i].UpdatedAt {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	out := make([]*Session, len(sorted))
	for i, fs := range sorted {
		out[i] = toSession(fs)
	}
	return out, nil
}

// DeleteSession removes a session and all its messages.
func (d *DB) DeleteSession(name string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	idx := -1
	for i, s := range d.data.Sessions {
		if s.Name == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("session %q not found", name)
	}
	d.data.Sessions = append(d.data.Sessions[:idx], d.data.Sessions[idx+1:]...)
	return d.save()
}

// RenameSession changes the name of an existing session.
func (d *DB) RenameSession(oldName, newName string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	fs := d.findByName(oldName)
	if fs == nil {
		return fmt.Errorf("session %q not found", oldName)
	}
	if d.findByName(newName) != nil {
		return fmt.Errorf("session %q already exists", newName)
	}
	fs.Name = newName
	fs.UpdatedAt = time.Now().Unix()
	return d.save()
}

// ── Message operations ────────────────────────────────────────────────────────

// AddMessage appends a message to a session.
func (d *DB) AddMessage(sessionID int64, role, content string) (*Message, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	fs := d.findByID(sessionID)
	if fs == nil {
		return nil, fmt.Errorf("session id %d not found", sessionID)
	}

	now := time.Now().Unix()
	fm := &fileMsg{
		ID:        d.data.NextMsgID,
		Role:      role,
		Content:   content,
		CreatedAt: now,
	}
	d.data.NextMsgID++
	fs.Messages = append(fs.Messages, fm)
	fs.UpdatedAt = now

	if err := d.save(); err != nil {
		return nil, err
	}
	return &Message{
		ID:        fm.ID,
		SessionID: sessionID,
		Role:      fm.Role,
		Content:   fm.Content,
		CreatedAt: time.Unix(now, 0),
	}, nil
}

// GetMessages returns all messages for a session in order.
func (d *DB) GetMessages(sessionID int64) ([]*Message, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	fs := d.findByID(sessionID)
	if fs == nil {
		return nil, fmt.Errorf("session id %d not found", sessionID)
	}

	out := make([]*Message, len(fs.Messages))
	for i, fm := range fs.Messages {
		out[i] = &Message{
			ID:        fm.ID,
			SessionID: sessionID,
			Role:      fm.Role,
			Content:   fm.Content,
			CreatedAt: time.Unix(fm.CreatedAt, 0),
		}
	}
	return out, nil
}

// CountMessages returns the number of messages in a session.
func (d *DB) CountMessages(sessionID int64) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	fs := d.findByID(sessionID)
	if fs == nil {
		return 0, nil
	}
	return len(fs.Messages), nil
}

// ClearMessages deletes all messages in a session but keeps the session itself.
func (d *DB) ClearMessages(sessionID int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	fs := d.findByID(sessionID)
	if fs == nil {
		return fmt.Errorf("session id %d not found", sessionID)
	}
	fs.Messages = nil
	fs.UpdatedAt = time.Now().Unix()
	return d.save()
}
