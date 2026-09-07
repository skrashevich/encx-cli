package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// newTestChatModel builds a model backed by a temporary HOME so the chat store
// persists into an isolated directory. No TTY is involved.
func newTestChatModel(t *testing.T, agentErr error) *chatModel {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	cfg := &config{domain: "tech.en.cx", gameId: 7, agentSecurity: SecurityModeApprove}
	m := newChatModel(context.Background(), cfg, NewChatStore(), NewAuthRegistry(),
		AgentConfig{Model: "test/model"}, agentErr)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return m
}

func (m *chatModel) lastRow(t *testing.T) UIMessage {
	t.Helper()
	if len(m.transcript) == 0 {
		t.Fatal("transcript is empty")
	}
	return m.transcript[len(m.transcript)-1]
}

func TestChatModelStartsWithOneChat(t *testing.T) {
	m := newTestChatModel(t, nil)
	if m.chatID == "" {
		t.Fatal("expected an active chat")
	}
	if len(m.chats) != 1 {
		t.Fatalf("expected 1 chat, got %d", len(m.chats))
	}
	if m.activeDomain != "tech.en.cx" || m.activeGameID != 7 {
		t.Fatalf("unexpected active chat: %s/%d", m.activeDomain, m.activeGameID)
	}
	if m.activeMode != SecurityModeApprove {
		t.Fatalf("unexpected security mode %q", m.activeMode)
	}
}

func TestChatModelSlashCommands(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		quits    bool
		wantRow  string
		validate func(t *testing.T, m *chatModel)
	}{
		{
			name:    "help lists commands",
			line:    "/help",
			wantRow: "/export",
		},
		{
			name:  "quit returns tea.Quit",
			line:  "/quit",
			quits: true,
		},
		{
			name: "chats toggles the sidebar",
			line: "/chats",
			validate: func(t *testing.T, m *chatModel) {
				if m.sidebar {
					t.Fatal("expected sidebar hidden")
				}
			},
		},
		{
			name:    "mode updates the security mode",
			line:    "/mode readonly",
			wantRow: "readonly",
			validate: func(t *testing.T, m *chatModel) {
				if m.activeMode != SecurityModeReadonly {
					t.Fatalf("mode = %q", m.activeMode)
				}
			},
		},
		{
			name:    "mode rejects unknown values",
			line:    "/mode nonsense",
			wantRow: "Неизвестный режим",
			validate: func(t *testing.T, m *chatModel) {
				if m.activeMode != SecurityModeApprove {
					t.Fatalf("mode changed to %q", m.activeMode)
				}
			},
		},
		{
			name: "domain patches the chat",
			line: "/domain demo.en.cx",
			validate: func(t *testing.T, m *chatModel) {
				if m.activeDomain != "demo.en.cx" {
					t.Fatalf("domain = %q", m.activeDomain)
				}
			},
		},
		{
			name: "game patches the chat",
			line: "/game 42",
			validate: func(t *testing.T, m *chatModel) {
				if m.activeGameID != 42 {
					t.Fatalf("game = %d", m.activeGameID)
				}
			},
		},
		{
			name:    "game rejects non-numeric ids",
			line:    "/game abc",
			wantRow: "Некорректный game id",
			validate: func(t *testing.T, m *chatModel) {
				if m.activeGameID != 7 {
					t.Fatalf("game = %d", m.activeGameID)
				}
			},
		},
		{
			name: "new creates and selects a chat",
			line: "/new demo.en.cx 11",
			validate: func(t *testing.T, m *chatModel) {
				if len(m.chats) != 2 {
					t.Fatalf("chats = %d", len(m.chats))
				}
				if m.activeDomain != "demo.en.cx" || m.activeGameID != 11 {
					t.Fatalf("unexpected new chat %s/%d", m.activeDomain, m.activeGameID)
				}
			},
		},
		{
			name:    "delete replaces the only chat",
			line:    "/delete",
			wantRow: "удалён",
			validate: func(t *testing.T, m *chatModel) {
				if len(m.chats) != 1 {
					t.Fatalf("chats = %d", len(m.chats))
				}
			},
		},
		{
			name:    "auth reports empty session list",
			line:    "/auth",
			wantRow: "Сохранённых сессий нет",
		},
		{
			name:    "unknown command is reported",
			line:    "/nope",
			wantRow: "Неизвестная команда",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestChatModel(t, nil)
			cmd := m.submit(tt.line)
			if tt.quits {
				if cmd == nil {
					t.Fatal("expected a command")
				}
				if _, ok := cmd().(tea.QuitMsg); !ok {
					t.Fatal("expected tea.QuitMsg")
				}
				return
			}
			if tt.wantRow != "" {
				got := m.lastRow(t).Content
				if !strings.Contains(got, tt.wantRow) {
					t.Fatalf("row %q does not contain %q", got, tt.wantRow)
				}
			}
			if tt.validate != nil {
				tt.validate(t, m)
			}
		})
	}
}

func TestChatModelExportWritesFile(t *testing.T) {
	m := newTestChatModel(t, nil)
	t.Chdir(t.TempDir())
	m.submit("/export md")
	row := m.lastRow(t).Content
	if !strings.Contains(row, "chat-"+m.chatID+".md") {
		t.Fatalf("unexpected export row: %q", row)
	}
	m.submit("/export xml")
	if got := m.lastRow(t).Content; !strings.Contains(got, "md или json") {
		t.Fatalf("unexpected export row: %q", got)
	}
}

func TestChatModelAgentEventsFillTranscript(t *testing.T) {
	m := newTestChatModel(t, nil)
	before := len(m.transcript)

	m.Update(agentEvtMsg{ev: AgentEvent{Type: agentEventAssistantText, Text: "готово"}})
	m.Update(agentEvtMsg{
		ev:      AgentEvent{Type: agentEventToolStart, ToolName: "admin_levels", ToolArgs: `{"game_id":1}`},
		display: "вызов admin_levels",
	})
	m.Update(agentEvtMsg{ev: AgentEvent{Type: agentEventToolDone, ToolName: "admin_levels", ToolResult: `{"ok":true}`}})
	m.Update(agentEvtMsg{ev: AgentEvent{Type: agentEventReport, Report: "--- Отчёт ---"}})
	m.Update(agentEvtMsg{ev: AgentEvent{Type: agentEventError, Err: errors.New("boom")}})
	// Empty payloads must not add rows.
	m.Update(agentEvtMsg{ev: AgentEvent{Type: agentEventAssistantText}})

	rows := m.transcript[before:]
	if len(rows) != 5 {
		t.Fatalf("expected 5 rows, got %d", len(rows))
	}
	if rows[0].Role != UIMessageRoleAssistant || rows[0].Content != "готово" {
		t.Fatalf("assistant row: %+v", rows[0])
	}
	if rows[1].Role != UIMessageRoleTool || rows[1].Content != "вызов admin_levels" {
		t.Fatalf("tool_start row: %+v", rows[1])
	}
	if !strings.HasPrefix(rows[2].Content, "→ ") || rows[2].ToolName != "admin_levels" {
		t.Fatalf("tool_done row: %+v", rows[2])
	}
	if rows[3].Role != UIMessageRoleSystem {
		t.Fatalf("report row: %+v", rows[3])
	}
	if !strings.Contains(rows[4].Content, "boom") {
		t.Fatalf("error row: %+v", rows[4])
	}
}

func TestChatModelApprovalPlumbing(t *testing.T) {
	tests := []struct {
		key  string
		want approvalDecision
	}{
		{"y", approveYes},
		{"д", approveYes},
		{"n", approveNo},
		{"esc", approveNo},
		{"q", approveQuit},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			m := newTestChatModel(t, nil)
			reply := make(chan approvalDecision, 1)
			m.Update(approvalNeededMsg{kind: "tool", title: "удалить уровень", details: []string{"Игра #1"}, reply: reply})
			if m.approval == nil {
				t.Fatal("approval prompt was not stored")
			}
			m.Update(keyMsgFor(tt.key))
			if m.approval != nil {
				t.Fatal("approval prompt was not cleared")
			}
			select {
			case got := <-reply:
				if got != tt.want {
					t.Fatalf("decision = %q, want %q", got, tt.want)
				}
			default:
				t.Fatal("no decision was sent")
			}
		})
	}
}

func TestChatModelApprovalIgnoresOtherKeys(t *testing.T) {
	m := newTestChatModel(t, nil)
	reply := make(chan approvalDecision, 1)
	m.Update(approvalNeededMsg{kind: "fix", title: "исправить сектор", reply: reply})
	m.Update(keyMsgFor("a"))
	if m.approval == nil {
		t.Fatal("approval prompt should stay open")
	}
	if len(reply) != 0 {
		t.Fatal("unexpected decision sent")
	}
}

func TestChatModelSendBlockedWithoutAPIKey(t *testing.T) {
	m := newTestChatModel(t, errors.New("LLM_API_KEY is required"))
	if cmd := m.submit("привет"); cmd != nil {
		t.Fatal("expected no agent turn")
	}
	if m.running {
		t.Fatal("agent must not start without config")
	}
	if !strings.Contains(m.status, "LLM_API_KEY") {
		t.Fatalf("status = %q", m.status)
	}
	// Slash commands keep working.
	m.submit("/help")
	if !strings.Contains(m.lastRow(t).Content, "/login") {
		t.Fatal("slash commands must still work")
	}
}

func TestChatModelTurnDoneClearsRunning(t *testing.T) {
	m := newTestChatModel(t, nil)
	m.running = true
	m.Update(turnDoneMsg{err: errors.New("upstream failed")})
	if m.running {
		t.Fatal("running flag was not cleared")
	}
	if !strings.Contains(m.lastRow(t).Content, "upstream failed") {
		t.Fatalf("last row = %q", m.lastRow(t).Content)
	}
}

func TestChatModelKeyBindings(t *testing.T) {
	m := newTestChatModel(t, nil)

	m.Update(keyMsgFor("ctrl+b"))
	if m.sidebar {
		t.Fatal("ctrl+b should hide the sidebar")
	}
	m.Update(keyMsgFor("tab"))
	if m.focus != focusInput {
		t.Fatal("tab must not focus a hidden sidebar")
	}
	m.Update(keyMsgFor("ctrl+b"))
	m.Update(keyMsgFor("tab"))
	if m.focus != focusSidebar {
		t.Fatal("tab should focus the sidebar")
	}

	_, cmd := m.Update(keyMsgFor("ctrl+c"))
	if cmd == nil {
		t.Fatal("ctrl+c should quit when idle")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("expected tea.QuitMsg")
	}

	cancelled := false
	m.running = true
	m.cancelRun = func() { cancelled = true }
	if _, cmd := m.Update(keyMsgFor("ctrl+c")); cmd != nil {
		t.Fatal("ctrl+c should cancel the run, not quit")
	}
	if !cancelled {
		t.Fatal("run was not cancelled")
	}
}

func TestChatModelViewRenders(t *testing.T) {
	m := newTestChatModel(t, nil)
	m.appendRow(UIMessageRoleUser, "привет", "")
	out := m.View()
	if !strings.Contains(out, "encli chat") {
		t.Fatalf("header missing:\n%s", out)
	}
	if !strings.Contains(out, "tech.en.cx") {
		t.Fatalf("domain missing:\n%s", out)
	}
	if lines := strings.Count(out, "\n") + 1; lines != 30 {
		t.Fatalf("view height = %d lines, want 30", lines)
	}
}

func TestChatModelRendersAssistantMarkdown(t *testing.T) {
	m := newTestChatModel(t, nil)
	m.appendRow(UIMessageRoleAssistant, "# Заголовок\n\n**жирный** текст\n\n- пункт один\n- пункт два", "")

	raw := m.renderTranscript()
	if !strings.Contains(raw, "\x1b[") {
		t.Fatalf("expected ANSI styling from the markdown renderer:\n%q", raw)
	}
	plain := ansi.Strip(raw)

	// Markdown syntax must be consumed by the renderer.
	for _, bad := range []string{"**", "# Заголовок", "- пункт один"} {
		if strings.Contains(plain, bad) {
			t.Fatalf("markdown syntax %q left unrendered:\n%s", bad, plain)
		}
	}
	// The words themselves survive.
	for _, want := range []string{"Заголовок", "жирный", "пункт один", "пункт два"} {
		if !strings.Contains(strings.Join(strings.Fields(plain), " "), want) {
			t.Fatalf("text %q lost during rendering:\n%s", want, plain)
		}
	}
}

func TestChatModelDoesNotRenderMarkdownForNonAssistant(t *testing.T) {
	m := newTestChatModel(t, nil)
	m.appendRow(UIMessageRoleSystem, "сырой **текст**", "")
	if !strings.Contains(ansi.Strip(m.renderTranscript()), "сырой **текст**") {
		t.Fatalf("non-assistant rows must stay verbatim:\n%s", m.renderTranscript())
	}
}

func TestChatModelMarkdownRendererRebuildsOnWidth(t *testing.T) {
	m := newTestChatModel(t, nil)
	r1 := m.markdownRenderer(60)
	if r1 == nil || m.markdownRenderer(60) != r1 {
		t.Fatal("renderer should be cached for the same width")
	}
	if r2 := m.markdownRenderer(90); r2 == r1 {
		t.Fatal("renderer should be rebuilt for a new width")
	}
}

func TestFixApprovalDetails(t *testing.T) {
	fix := pendingAdminFix{
		Title:       "Fix sector",
		Summary:     "answer typo",
		LevelNumber: 3,
		Steps: []pendingFixStep{{
			Tool:      "admin_update_sector",
			Arguments: map[string]any{"sector_id": 5, "level_number": 3},
		}},
	}
	details := fixApprovalDetails(&llmSession{}, fix)
	if len(details) != 3 {
		t.Fatalf("details = %#v", details)
	}
	if details[0] != "Level 3" || !strings.Contains(details[1], "answer typo") {
		t.Fatalf("details = %#v", details)
	}
	if !strings.Contains(details[2], "update sector 5") {
		t.Fatalf("step detail = %q", details[2])
	}
}

func TestApprovalLogSinkDefaultsToNil(t *testing.T) {
	if approvalLog != nil {
		t.Fatal("approvalLog must default to nil so CLI output is unchanged")
	}
	var got []string
	approvalLog = func(s string) { got = append(got, s) }
	defer func() { approvalLog = nil }()

	printApprovalMessage(&llmSession{}, "applying")
	printApprovalSummary(&llmSession{}, []proposalOutcome{{Title: "a", Applied: true}})
	if len(got) != 2 {
		t.Fatalf("sink received %#v", got)
	}
	if got[0] != "applying" || !strings.Contains(got[1], "Applied: 1") {
		t.Fatalf("sink received %#v", got)
	}
}

func keyMsgFor(s string) tea.KeyMsg {
	switch s {
	case "ctrl+b":
		return tea.KeyMsg{Type: tea.KeyCtrlB}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}
