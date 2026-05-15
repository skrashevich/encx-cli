package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

// UIMessageRole is stored for UI-facing chat history.
type UIMessageRole string

const (
	UIMessageRoleUser      UIMessageRole = "user"
	UIMessageRoleAssistant UIMessageRole = "assistant"
	UIMessageRoleTool      UIMessageRole = "tool"
	UIMessageRoleSystem    UIMessageRole = "system"
)

// UIMessage is a single row in the chat transcript shown to the API.
type UIMessage struct {
	ID        string        `json:"id"`
	Role      UIMessageRole `json:"role"`
	Content   string        `json:"content"`
	ToolName  string        `json:"tool_name,omitempty"`
	CreatedAt time.Time     `json:"created_at"`
}

// ChatThread holds agent state for one conversation.
type ChatThread struct {
	mu sync.Mutex

	ID         string
	Title      string
	Domain     string
	GameID     int
	CreatedAt  time.Time
	UpdatedAt  time.Time
	messages   []llmMessage
	session    *llmSession
	uiMessages []UIMessage
	running    bool
	cancel     func()
}

// ChatSnapshot is the JSON API projection of ChatThread (mutex-safe copy).
type ChatSnapshot struct {
	ID           string            `json:"id"`
	Title        string            `json:"title"`
	Domain       string            `json:"domain"`
	GameID       int               `json:"game_id"`
	SecurityMode AgentSecurityMode `json:"security_mode"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
	Messages     []UIMessage       `json:"messages"`
	Running      bool              `json:"running"`
	LLMMsgs      []llmMessage      `json:"llm_messages,omitempty"`
}

// ChatStore is an in-memory chat registry protected by a mutex.
type ChatStore struct {
	mu          sync.Mutex
	chats       map[string]*ChatThread
	fallbackSeq atomic.Uint64 // used if crypto/rand fails
}

// NewChatStore constructs an empty ChatStore.
func NewChatStore() *ChatStore {
	return &ChatStore{chats: make(map[string]*ChatThread)}
}

func (s *ChatStore) newID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err == nil {
		return hex.EncodeToString(b[:])
	}
	n := s.fallbackSeq.Add(1)
	return hex.EncodeToString([]byte{
		byte(n >> 56), byte(n >> 48), byte(n >> 40), byte(n >> 32),
		byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n),
	})
}

// List returns snapshots for all chats (sorted by UpdatedAt desc).
func (s *ChatStore) List() []ChatSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ChatSnapshot, 0, len(s.chats))
	for _, t := range s.chats {
		out = append(out, s.snapshotLocked(t))
	}
	sortSnapshotsByUpdated(out)
	return out
}

// Get returns a snapshot copy for id, or ok false.
func (s *ChatStore) Get(id string) (snap ChatSnapshot, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.chats[id]
	if !ok {
		return ChatSnapshot{}, false
	}
	return s.snapshotLocked(t), true
}

// Create inserts a new chat thread for domain and gameID.
func (s *ChatStore) Create(domain string, gameID int, defaultSecurity AgentSecurityMode) ChatSnapshot {
	now := time.Now().UTC()
	if defaultSecurity == "" {
		defaultSecurity = SecurityModeFull
	}
	t := &ChatThread{
		ID:         s.newID(),
		Domain:     domain,
		GameID:     gameID,
		CreatedAt:  now,
		UpdatedAt:  now,
		session:    &llmSession{securityMode: defaultSecurity},
		messages:   nil,
		uiMessages: nil,
	}
	if domain != "" {
		t.Title = domain
	} else {
		t.Title = "chat"
	}

	s.mu.Lock()
	s.chats[t.ID] = t
	snap := s.snapshotLocked(t)
	s.mu.Unlock()
	// caller may Persist; Create path persists via HTTP handler
	return snap
}

// Update applies JSON-decodable partial fields to an existing thread.
type chatPatch struct {
	Title        *string            `json:"title"`
	Domain       *string            `json:"domain"`
	GameID       *int               `json:"game_id"`
	SecurityMode *AgentSecurityMode `json:"security_mode"`
}

// Update parses patch JSON and updates the thread; returns ok false if missing.
func (s *ChatStore) Update(id string, patchJSON []byte) (snap ChatSnapshot, ok bool) {
	var p chatPatch
	if err := json.Unmarshal(patchJSON, &p); err != nil {
		return ChatSnapshot{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.chats[id]
	if !ok {
		return ChatSnapshot{}, false
	}
	if p.Title != nil {
		t.Title = *p.Title
	}
	if p.Domain != nil {
		t.Domain = *p.Domain
	}
	if p.GameID != nil {
		t.GameID = *p.GameID
	}
	if p.SecurityMode != nil {
		if mode, valid := parseAgentSecurityMode(string(*p.SecurityMode)); valid {
			t.mu.Lock()
			if t.session == nil {
				t.session = &llmSession{}
			}
			t.session.securityMode = mode
			t.mu.Unlock()
		}
	}
	t.UpdatedAt = time.Now().UTC()
	return s.snapshotLocked(t), true
}

// Delete removes a chat by id.
func (s *ChatStore) Delete(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.chats[id]
	if !ok {
		return false
	}
	if t.cancel != nil {
		t.cancel()
	}
	delete(s.chats, id)
	return true
}

// AppendUserMessageUnlessRunning appends a user message if no agent run is active.
// Returns ok false when chatID is unknown; busy true when an agent run is already in progress (no message appended).
func (s *ChatStore) AppendUserMessageUnlessRunning(chatID, content string) (msg UIMessage, ok bool, busy bool) {
	now := time.Now().UTC()
	msg = UIMessage{
		ID:        s.newID(),
		Role:      UIMessageRoleUser,
		Content:   content,
		CreatedAt: now}
	s.mu.Lock()
	defer s.mu.Unlock()
	t, exists := s.chats[chatID]
	if !exists {
		return UIMessage{}, false, false
	}
	if t.running {
		return UIMessage{}, true, true
	}
	t.uiMessages = append(t.uiMessages, msg)
	t.messages = append(t.messages, llmMessage{Role: "user", Content: content})
	t.Title = autoTitleFromMessage(t.Title, t.Domain, content)
	t.UpdatedAt = now
	return msg, true, false
}

// PendingFixes returns a copy of pending admin fixes for a chat, if any.
func (s *ChatStore) PendingFixes(id string) ([]pendingAdminFix, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.chats[id]
	if !ok || t.session == nil || len(t.session.pendingFixes) == 0 {
		return nil, false
	}
	return append([]pendingAdminFix(nil), t.session.pendingFixes...), true
}

// AppendUserMessage adds a user-visible message and optional LLM mirror.
func (s *ChatStore) AppendUserMessage(chatID, content string) (UIMessage, bool) {
	now := time.Now().UTC()
	msg := UIMessage{
		ID:        s.newID(),
		Role:      UIMessageRoleUser,
		Content:   content,
		CreatedAt: now,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.chats[chatID]
	if !ok {
		return UIMessage{}, false
	}
	t.uiMessages = append(t.uiMessages, msg)
	t.messages = append(t.messages, llmMessage{Role: "user", Content: content})
	t.UpdatedAt = now
	return msg, true
}

// WithRunning sets running state and cancel func (must not hold locks across user callbacks).
func (s *ChatStore) WithRunning(id string, running bool, cancel func()) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.chats[id]
	if !ok {
		return false
	}
	t.running = running
	t.cancel = cancel
	t.UpdatedAt = time.Now().UTC()
	return true
}

func (s *ChatStore) snapshotLocked(t *ChatThread) ChatSnapshot {
	uiCopy := make([]UIMessage, len(t.uiMessages))
	copy(uiCopy, t.uiMessages)
	mode := SecurityModeFull
	if t.session != nil && t.session.securityMode != "" {
		mode = t.session.securityMode.effective()
	}
	return ChatSnapshot{
		ID:           t.ID,
		Title:        t.Title,
		Domain:       t.Domain,
		GameID:       t.GameID,
		SecurityMode: mode,
		CreatedAt:    t.CreatedAt,
		UpdatedAt:    t.UpdatedAt,
		Messages:     uiCopy,
		Running:      t.running,
	}
}

// LockThread locks the chat thread for agent execution. Caller must call unlock.
func (s *ChatStore) LockThread(id string) (t *ChatThread, unlock func(), ok bool) {
	s.mu.Lock()
	t, ok = s.chats[id]
	if !ok {
		s.mu.Unlock()
		return nil, nil, false
	}
	s.mu.Unlock()
	t.mu.Lock()
	return t, func() { t.mu.Unlock() }, true
}

func sortSnapshotsByUpdated(s []ChatSnapshot) {
	slices.SortFunc(s, func(a, b ChatSnapshot) int {
		if a.UpdatedAt.After(b.UpdatedAt) {
			return -1
		}
		if a.UpdatedAt.Before(b.UpdatedAt) {
			return 1
		}
		return 0
	})
}
