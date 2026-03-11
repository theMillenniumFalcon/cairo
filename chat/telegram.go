package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/themillenniumfalcon/cairo/agent"
	"github.com/themillenniumfalcon/cairo/db"
	"github.com/themillenniumfalcon/cairo/llm"
	"github.com/themillenniumfalcon/cairo/tools"
)

const (
	tgBaseURL   = "https://api.telegram.org/bot"
	pollTimeout = 30 // seconds for long-poll
	tgMaxMsgLen = 4096
)

// ── Telegram API types (only what we need) ────────────────────────────────────

type tgUpdate struct {
	UpdateID int        `json:"update_id"`
	Message  *tgMessage `json:"message"`
}

type tgMessage struct {
	MessageID int    `json:"message_id"`
	Chat      tgChat `json:"chat"`
	From      tgUser `json:"from"`
	Text      string `json:"text"`
}

type tgChat struct {
	ID int64 `json:"id"`
}

type tgUser struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	Username  string `json:"username"`
}

// ── Bot ───────────────────────────────────────────────────────────────────────

// Bot is a Telegram bot that runs the ReAct agent for each user.
type Bot struct {
	token    string
	provider llm.Provider
	registry *tools.Registry
	store    *db.DB
	client   *http.Client

	// sessions keyed by Telegram chat ID
	sessions map[int64]*agent.Session
}

// NewBot creates a bot. Call Run() to start polling.
func NewBot(token string, provider llm.Provider, registry *tools.Registry, store *db.DB) *Bot {
	return &Bot{
		token:    token,
		provider: provider,
		registry: registry,
		store:    store,
		client:   &http.Client{Timeout: time.Duration(pollTimeout+5) * time.Second},
		sessions: make(map[int64]*agent.Session),
	}
}

// Run starts long-polling and blocks until ctx is cancelled.
func (b *Bot) Run(ctx context.Context) error {
	me, err := b.getMe()
	if err != nil {
		return fmt.Errorf("telegram: getMe failed — is your token correct? %w", err)
	}
	log.Printf("telegram: bot @%s is online, polling for messages…", me)

	offset := 0
	for {
		select {
		case <-ctx.Done():
			log.Println("telegram: shutting down")
			return nil
		default:
		}

		updates, err := b.getUpdates(offset)
		if err != nil {
			log.Printf("telegram: getUpdates error: %v — retrying in 5s", err)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(5 * time.Second):
			}
			continue
		}

		for _, u := range updates {
			offset = u.UpdateID + 1
			if u.Message == nil || strings.TrimSpace(u.Message.Text) == "" {
				continue
			}
			go b.handleMessage(ctx, u.Message)
		}
	}
}

// handleMessage processes one incoming Telegram message.
func (b *Bot) handleMessage(ctx context.Context, msg *tgMessage) {
	chatID := msg.Chat.ID
	text := strings.TrimSpace(msg.Text)

	// Built-in bot commands
	switch {
	case text == "/start":
		b.send(chatID, "👋 Hi! I'm Cairo, your personal AI agent.\n\nJust send me a message and I'll help you out. I can run shell commands, read/write files, and fetch URLs.")
		return
	case text == "/clear":
		if sess, ok := b.sessions[chatID]; ok {
			if err := sess.ClearHistory(); err != nil {
				b.send(chatID, "⚠️ Could not clear history: "+err.Error())
				return
			}
		}
		b.send(chatID, "🗑️ History cleared.")
		return
	case text == "/info":
		sess := b.getOrCreateSession(chatID)
		b.send(chatID, fmt.Sprintf(
			"📋 Session: %s\nProvider: %s / %s\nMessages: %d",
			sess.Record.Name,
			sess.Record.Provider,
			sess.Record.Model,
			sess.MessageCount(),
		))
		return
	case text == "/help":
		b.send(chatID, "Commands:\n/start — greeting\n/clear — wipe session history\n/info  — session info\n/help  — this message\n\nOr just send any message to chat.")
		return
	}

	// Get or create the session for this chat
	sess := b.getOrCreateSession(chatID)

	// Save user message
	if err := sess.Add(llm.RoleUser, text); err != nil {
		log.Printf("telegram: save user msg: %v", err)
	}

	// Send a "typing…" indicator
	b.sendTyping(chatID)

	// Run the ReAct loop
	reply, err := agent.RunReAct(
		ctx,
		b.provider,
		b.registry,
		sess.History,
		func(step agent.Step) {
			// Show tool use progress to the user
			notice := fmt.Sprintf("⚙️ `%s` ← %s", step.Action, truncateStr(step.ActionInput, 80))
			b.send(chatID, notice)
			b.sendTyping(chatID)
		},
	)
	if err != nil {
		log.Printf("telegram: react error for chat %d: %v", chatID, err)
		// Roll back the user message on error
		sess.History = sess.History[:len(sess.History)-1]
		b.send(chatID, "⚠️ "+err.Error())
		return
	}

	// Save assistant reply
	if err := sess.Add(llm.RoleAssistant, reply); err != nil {
		log.Printf("telegram: save reply: %v", err)
	}

	b.sendSplit(chatID, reply)
}

// getOrCreateSession returns the in-memory session for a chat ID, loading
// from DB if needed. Session name is "tg-<chatID>".
func (b *Bot) getOrCreateSession(chatID int64) *agent.Session {
	if sess, ok := b.sessions[chatID]; ok {
		return sess
	}
	name := "tg-" + strconv.FormatInt(chatID, 10)
	sess, _, err := agent.LoadOrCreate(b.store, b.registry, name, b.provider.Name(), b.provider.Model())
	if err != nil {
		log.Printf("telegram: could not load session for chat %d: %v", chatID, err)
		// Return an ephemeral session on DB error
		sess = agent.NewEphemeral(b.provider.Name(), b.provider.Model(), b.registry)
	}
	b.sessions[chatID] = sess
	return sess
}

// ── Telegram HTTP calls ───────────────────────────────────────────────────────

func (b *Bot) apiURL(method string) string {
	return tgBaseURL + b.token + "/" + method
}

func (b *Bot) getMe() (string, error) {
	resp, err := b.client.Get(b.apiURL("getMe"))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		OK     bool `json:"ok"`
		Result struct {
			Username string `json:"username"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if !result.OK {
		return "", fmt.Errorf("getMe returned ok=false")
	}
	return result.Result.Username, nil
}

func (b *Bot) getUpdates(offset int) ([]tgUpdate, error) {
	url := fmt.Sprintf("%s?timeout=%d&offset=%d&allowed_updates=[\"message\"]",
		b.apiURL("getUpdates"), pollTimeout, offset)

	resp, err := b.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		OK     bool       `json:"ok"`
		Result []tgUpdate `json:"result"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse updates: %w", err)
	}
	if !result.OK {
		return nil, fmt.Errorf("getUpdates returned ok=false: %s", body)
	}
	return result.Result, nil
}

// send posts a text message to a chat.
func (b *Bot) send(chatID int64, text string) {
	if text == "" {
		return
	}
	payload := map[string]any{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "Markdown",
	}
	body, _ := json.Marshal(payload)
	resp, err := b.client.Post(b.apiURL("sendMessage"), "application/json", bytes.NewReader(body))
	if err != nil {
		log.Printf("telegram: send error: %v", err)
		return
	}
	defer resp.Body.Close()
	// Retry without Markdown if parse error
	var result struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil || result.OK {
		return
	}
	if strings.Contains(result.Description, "parse") {
		plain := map[string]any{"chat_id": chatID, "text": text}
		body, _ = json.Marshal(plain)
		resp2, err := b.client.Post(b.apiURL("sendMessage"), "application/json", bytes.NewReader(body))
		if err == nil {
			resp2.Body.Close()
		}
	}
}

// sendSplit splits long messages into chunks ≤ 4096 chars and sends each.
func (b *Bot) sendSplit(chatID int64, text string) {
	if len(text) <= tgMaxMsgLen {
		b.send(chatID, text)
		return
	}
	for len(text) > 0 {
		chunk := text
		if len(chunk) > tgMaxMsgLen {
			// Split at last newline within limit
			cut := tgMaxMsgLen
			if idx := strings.LastIndex(text[:cut], "\n"); idx > 0 {
				cut = idx
			}
			chunk = text[:cut]
			text = text[cut:]
		} else {
			text = ""
		}
		b.send(chatID, chunk)
	}
}

// sendTyping sends a "typing…" chat action.
func (b *Bot) sendTyping(chatID int64) {
	payload := map[string]any{"chat_id": chatID, "action": "typing"}
	body, _ := json.Marshal(payload)
	resp, err := b.client.Post(b.apiURL("sendChatAction"), "application/json", bytes.NewReader(body))
	if err == nil {
		resp.Body.Close()
	}
}

func truncateStr(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
