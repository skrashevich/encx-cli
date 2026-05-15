package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func webChatsDir() string {
	return filepath.Join(sessionDir(), "web", "chats")
}

type persistedChat struct {
	ID         string        `json:"id"`
	Title      string        `json:"title"`
	Domain     string        `json:"domain"`
	GameID     int           `json:"game_id"`
	CreatedAt  time.Time     `json:"created_at"`
	UpdatedAt  time.Time     `json:"updated_at"`
	Messages   []llmMessage  `json:"messages"`
	UIMessages []UIMessage   `json:"ui_messages"`
	Session    persistedSess `json:"session"`
}

type persistedSess struct {
	SecurityMode       AgentSecurityMode `json:"security_mode,omitempty"`
	ReviewApprovalMode bool              `json:"review_approval_mode"`
	PreferRussian      bool              `json:"prefer_russian"`
	PendingFixes       []pendingAdminFix `json:"pending_fixes,omitempty"`
	LoadedLevelContent []int             `json:"loaded_level_content,omitempty"`
}

func (s *ChatStore) LoadFromDisk() error {
	dir := webChatsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var pc persistedChat
		if err := json.Unmarshal(data, &pc); err != nil || pc.ID == "" {
			continue
		}
		t := &ChatThread{
			ID:         pc.ID,
			Title:      pc.Title,
			Domain:     pc.Domain,
			GameID:     pc.GameID,
			CreatedAt:  pc.CreatedAt,
			UpdatedAt:  pc.UpdatedAt,
			messages:   pc.Messages,
			uiMessages: pc.UIMessages,
			session: &llmSession{
				securityMode:       pc.Session.SecurityMode.effective(),
				reviewApprovalMode: pc.Session.ReviewApprovalMode,
				preferRussian:      pc.Session.PreferRussian,
				pendingFixes:       pc.Session.PendingFixes,
			},
		}
		if t.session == nil {
			t.session = &llmSession{}
		}
		if len(pc.Session.LoadedLevelContent) > 0 {
			applyLoadedLevels(t.session, pc.Session.LoadedLevelContent)
		} else {
			t.session.loadedLevelContent = rebuildLoadedLevelsFromUI(t.uiMessages)
		}
		s.chats[t.ID] = t
	}
	return nil
}

func (s *ChatStore) Persist(id string) {
	s.mu.Lock()
	t, ok := s.chats[id]
	if !ok {
		s.mu.Unlock()
		return
	}
	pc := persistedChat{
		ID:         t.ID,
		Title:      t.Title,
		Domain:     t.Domain,
		GameID:     t.GameID,
		CreatedAt:  t.CreatedAt,
		UpdatedAt:  t.UpdatedAt,
		Messages:   append([]llmMessage(nil), t.messages...),
		UIMessages: append([]UIMessage(nil), t.uiMessages...),
	}
	if t.session != nil {
		pc.Session = persistedSess{
			SecurityMode:       t.session.securityMode.effective(),
			ReviewApprovalMode: t.session.reviewApprovalMode,
			PreferRussian:      t.session.preferRussian,
			PendingFixes:       append([]pendingAdminFix(nil), t.session.pendingFixes...),
			LoadedLevelContent: loadedLevelsSlice(t.session.loadedLevelContent),
		}
	}
	s.mu.Unlock()

	dir := webChatsDir()
	_ = os.MkdirAll(dir, 0o700)
	path := filepath.Join(dir, id+".json")
	data, err := json.MarshalIndent(pc, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o600)
}

func (s *ChatStore) RemovePersisted(id string) {
	_ = os.Remove(filepath.Join(webChatsDir(), id+".json"))
}

func autoTitleFromMessage(title, domain, content string) string {
	if content == "" {
		return title
	}
	// Only auto-rename generic titles.
	generic := title == "" || title == "chat" || title == domain
	if !generic && !strings.HasPrefix(title, domain) {
		return title
	}
	line := strings.TrimSpace(strings.Split(content, "\n")[0])
	const max = 48
	runes := []rune(line)
	if len(runes) > max {
		line = string(runes[:max]) + "…"
	}
	if line == "" {
		return title
	}
	return line
}
