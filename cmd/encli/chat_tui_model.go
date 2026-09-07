package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
)

type focusTarget int

const (
	focusInput focusTarget = iota
	focusSidebar
)

type approvalDecision string

const (
	approveYes  approvalDecision = "yes"
	approveNo   approvalDecision = "no"
	approveQuit approvalDecision = "quit"
)

// approvalPrompt is the approval currently shown in place of the input box.
type approvalPrompt struct {
	kind    string // "tool" or "fix"
	title   string
	details []string
	reply   chan approvalDecision
}

// Messages pushed into the Bubble Tea loop by the agent bridge goroutine.
type (
	// agentEvtMsg carries one AgentEvent plus the display string the bridge
	// rendered with the thread session (so the TUI matches the stored history).
	agentEvtMsg struct {
		ev      AgentEvent
		display string
	}
	agentStatusMsg    struct{ phase, message string }
	agentLogMsg       struct{ line string }
	turnDoneMsg       struct{ err error }
	chatInfoMsg       struct{ text string }
	approvalNeededMsg struct {
		kind    string
		title   string
		details []string
		reply   chan approvalDecision
	}
)

// chatModel is the Bubble Tea model behind `encli -chat`.
type chatModel struct {
	ctx      context.Context
	cfg      *config
	store    *ChatStore
	registry *AuthRegistry
	agentCfg AgentConfig
	agentErr error

	program *tea.Program

	chatID     string
	chats      []ChatSnapshot
	transcript []UIMessage

	// Cached fields of the active chat, refreshed by refreshChats.
	activeTitle  string
	activeDomain string
	activeGameID int
	activeMode   AgentSecurityMode

	vp    viewport.Model
	input textarea.Model

	// Markdown renderer for assistant messages, rebuilt when the wrap width changes.
	mdRenderer *glamour.TermRenderer
	mdWrap     int

	ready         bool
	width, height int
	sidebar       bool
	focus         focusTarget
	sidebarIdx    int

	running   bool
	cancelRun context.CancelFunc

	approval *approvalPrompt

	status    string
	debugNote string
}

func newChatModel(ctx context.Context, cfg *config, store *ChatStore, registry *AuthRegistry,
	agentCfg AgentConfig, agentErr error) *chatModel {
	ta := textarea.New()
	ta.Placeholder = "Сообщение агенту (/help — список команд)"
	ta.Prompt = "┃ "
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.SetHeight(chatInputHeight)
	ta.SetWidth(60)
	// Enter submits; the textarea must not swallow it as a newline.
	ta.KeyMap.InsertNewline.SetEnabled(false)
	ta.Focus()

	vp := viewport.New(60, 10)
	vp.MouseWheelEnabled = true

	m := &chatModel{
		ctx:      ctx,
		cfg:      cfg,
		store:    store,
		registry: registry,
		agentCfg: agentCfg,
		agentErr: agentErr,
		vp:       vp,
		input:    ta,
		sidebar:  true,
		focus:    focusInput,
	}
	m.ensureActiveChat()
	if agentErr != nil {
		m.status = "LLM: " + agentErr.Error()
	} else {
		m.status = "модель: " + agentCfg.Model
	}
	return m
}

// ensureActiveChat selects the most recent chat, creating one when the store is empty.
func (m *chatModel) ensureActiveChat() {
	m.chats = m.store.List()
	if len(m.chats) == 0 {
		snap := m.store.Create(m.cfg.domain, m.cfg.gameId, m.cfg.agentSecurity.effective())
		m.store.Persist(snap.ID)
		m.chatID = snap.ID
	} else if _, ok := m.store.Get(m.chatID); !ok {
		m.chatID = m.chats[0].ID
	}
	m.refreshChats()
	m.loadTranscript()
}

// refreshChats reloads the sidebar list and the cached header fields.
func (m *chatModel) refreshChats() {
	m.chats = m.store.List()
	for i, c := range m.chats {
		if c.ID != m.chatID {
			continue
		}
		m.sidebarIdx = i
		m.activeTitle = c.Title
		m.activeDomain = c.Domain
		m.activeGameID = c.GameID
		m.activeMode = c.SecurityMode
		return
	}
	if m.sidebarIdx >= len(m.chats) {
		m.sidebarIdx = max(0, len(m.chats)-1)
	}
}

func (m *chatModel) loadTranscript() {
	snap, ok := m.store.Get(m.chatID)
	if !ok {
		m.transcript = nil
		return
	}
	m.transcript = snap.Messages
	m.refreshViewport()
}

func (m *chatModel) appendRow(role UIMessageRole, content, toolName string) {
	if strings.TrimSpace(content) == "" {
		return
	}
	m.transcript = append(m.transcript, UIMessage{Role: role, Content: content, ToolName: toolName})
	m.refreshViewport()
}

func (m *chatModel) refreshViewport() {
	if m.vp.Width <= 0 {
		return
	}
	m.vp.SetContent(m.renderTranscript())
	m.vp.GotoBottom()
}

// applySize recomputes child component sizes after a resize or sidebar toggle.
func (m *chatModel) applySize() {
	m.vp.Height = max(1, m.height-chatChromeHeight)
	m.vp.Width = max(10, m.width-m.sidebarTotalWidth())
	m.input.SetWidth(max(10, m.width-4))
	m.input.SetHeight(chatInputHeight)
	m.refreshViewport()
}

func (m *chatModel) sidebarTotalWidth() int {
	if !m.sidebar {
		return 0
	}
	return chatSidebarWidth + 1 // + right border
}

func (m *chatModel) Init() tea.Cmd {
	return textarea.Blink
}

func (m *chatModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.ready = true
		m.applySize()
		return m, nil

	case tea.MouseMsg:
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		return m.handleKey(msg)

	case approvalNeededMsg:
		m.approval = &approvalPrompt{kind: msg.kind, title: msg.title, details: msg.details, reply: msg.reply}
		return m, nil

	case agentEvtMsg:
		m.applyAgentEvent(msg.ev, msg.display)
		return m, nil

	case agentStatusMsg:
		if msg.message != "" {
			m.status = msg.message
		}
		return m, nil

	case agentLogMsg:
		m.appendRow(UIMessageRoleSystem, msg.line, "")
		return m, nil

	case chatInfoMsg:
		m.appendRow(UIMessageRoleSystem, msg.text, "")
		return m, nil

	case turnDoneMsg:
		m.running = false
		m.cancelRun = nil
		m.store.WithRunning(m.chatID, false, nil)
		if msg.err != nil {
			m.appendRow(UIMessageRoleSystem, "Ошибка: "+msg.err.Error(), "")
			m.status = "ошибка: " + msg.err.Error()
		} else {
			m.status = "готово"
		}
		m.refreshChats()
		return m, nil
	}

	return m, nil
}

func (m *chatModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.approval != nil {
		return m, m.handleApprovalKey(msg)
	}

	switch msg.String() {
	case "ctrl+c":
		if m.running {
			m.cancelTurn()
			return m, nil
		}
		return m, tea.Quit
	case "esc":
		if m.running {
			m.cancelTurn()
		}
		return m, nil
	case "ctrl+b":
		m.sidebar = !m.sidebar
		if !m.sidebar && m.focus == focusSidebar {
			m.focus = focusInput
			m.input.Focus()
		}
		m.applySize()
		return m, nil
	case "tab":
		if !m.sidebar {
			return m, nil
		}
		if m.focus == focusInput {
			m.focus = focusSidebar
			m.input.Blur()
		} else {
			m.focus = focusInput
			m.input.Focus()
		}
		return m, nil
	case "pgup", "pgdown":
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	}

	if m.focus == focusSidebar {
		switch msg.String() {
		case "up", "k":
			if m.sidebarIdx > 0 {
				m.sidebarIdx--
			}
		case "down", "j":
			if m.sidebarIdx < len(m.chats)-1 {
				m.sidebarIdx++
			}
		case "enter":
			if m.sidebarIdx < len(m.chats) {
				m.switchChat(m.chats[m.sidebarIdx].ID)
			}
		}
		return m, nil
	}

	if msg.String() == "enter" {
		text := strings.TrimSpace(m.input.Value())
		if text == "" {
			return m, nil
		}
		m.input.Reset()
		return m, m.submit(text)
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *chatModel) handleApprovalKey(msg tea.KeyMsg) tea.Cmd {
	var decision approvalDecision
	switch strings.ToLower(msg.String()) {
	case "y", "д":
		decision = approveYes
	case "n", "н", "esc":
		decision = approveNo
	case "q", "в", "ctrl+c":
		decision = approveQuit
	default:
		return nil
	}
	select {
	case m.approval.reply <- decision:
	default:
	}
	m.appendRow(UIMessageRoleSystem, "Согласование: "+m.approval.title+" → "+string(decision), "")
	m.approval = nil
	return nil
}

func (m *chatModel) cancelTurn() {
	if m.cancelRun != nil {
		m.cancelRun()
		m.status = "отмена…"
	}
}

func (m *chatModel) switchChat(id string) {
	if id == "" || id == m.chatID {
		return
	}
	m.chatID = id
	m.refreshChats()
	m.loadTranscript()
}

func (m *chatModel) applyAgentEvent(ev AgentEvent, display string) {
	switch ev.Type {
	case agentEventAssistantText:
		m.appendRow(UIMessageRoleAssistant, ev.Text, "")
	case agentEventToolStart:
		if display == "" {
			display = formatToolCallForDisplay(nil, ev.ToolName, ev.ToolArgs)
		}
		m.appendRow(UIMessageRoleTool, display, ev.ToolName)
	case agentEventToolDone:
		m.appendRow(UIMessageRoleTool, "→ "+truncateRunes(summarizeDebugText(ev.ToolResult, 0), 200), ev.ToolName)
	case agentEventReport:
		m.appendRow(UIMessageRoleSystem, ev.Report, "")
	case agentEventWarning:
		m.appendRow(UIMessageRoleSystem, ev.Message, "")
	case agentEventError:
		msg := ev.Message
		if msg == "" && ev.Err != nil {
			msg = ev.Err.Error()
		}
		m.appendRow(UIMessageRoleSystem, "Ошибка: "+msg, "")
	}
}

func truncateRunes(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit]) + "…"
}

// submit routes a submitted line to a slash command or to the agent.
func (m *chatModel) submit(text string) tea.Cmd {
	if strings.HasPrefix(text, "/") {
		return m.runSlashCommand(text)
	}
	if m.agentErr != nil {
		m.status = "агент недоступен: " + m.agentErr.Error()
		return nil
	}
	if m.running {
		m.status = "агент уже выполняет запрос"
		return nil
	}
	return m.startTurn(text)
}

const chatHelpText = `Команды:
  /help                          эта справка
  /new [domain] [gameID]         создать чат
  /chats                         показать/скрыть список чатов
  /domain <d>                    сменить домен чата
  /game <id>                     сменить игру чата
  /games                         список доступных игр
  /mode readonly|approve|full    режим безопасности чата
  /login <domain> <login> <pass> вход в Encounter
  /logout <domain>               выход
  /auth                          статус сессий
  /delete                        удалить текущий чат
  /export [md|json]              выгрузить чат в файл
  /quit, /exit                   выход

Клавиши: Enter — отправить, Tab — фокус списка, Ctrl+B — список чатов,
Esc/Ctrl+C — отмена запроса или выход, PgUp/PgDn — прокрутка,
y/n/q — ответ на запрос согласования.`

func (m *chatModel) runSlashCommand(line string) tea.Cmd {
	fields := strings.Fields(line)
	cmd := strings.ToLower(fields[0])
	args := fields[1:]

	switch cmd {
	case "/help", "/?":
		m.appendRow(UIMessageRoleSystem, chatHelpText, "")
	case "/quit", "/exit":
		return tea.Quit
	case "/chats":
		m.sidebar = !m.sidebar
		if !m.sidebar && m.focus == focusSidebar {
			m.focus = focusInput
			m.input.Focus()
		}
		m.applySize()
	case "/new":
		domain := m.activeDomain
		if len(args) > 0 {
			domain = args[0]
		}
		gameID := 0
		if len(args) > 1 {
			gameID, _ = strconv.Atoi(args[1])
		}
		snap := m.store.Create(domain, gameID, m.cfg.agentSecurity.effective())
		m.store.Persist(snap.ID)
		m.chatID = snap.ID
		m.refreshChats()
		m.loadTranscript()
		m.appendRow(UIMessageRoleSystem, "Новый чат "+snap.ID, "")
	case "/domain":
		if len(args) < 1 {
			m.appendRow(UIMessageRoleSystem, "Использование: /domain <домен>", "")
			break
		}
		m.patchChat(map[string]any{"domain": args[0]})
	case "/game":
		if len(args) < 1 {
			m.appendRow(UIMessageRoleSystem, "Использование: /game <id>", "")
			break
		}
		id, err := strconv.Atoi(args[0])
		if err != nil {
			m.appendRow(UIMessageRoleSystem, "Некорректный game id: "+args[0], "")
			break
		}
		m.patchChat(map[string]any{"game_id": id})
	case "/mode":
		if len(args) < 1 {
			m.appendRow(UIMessageRoleSystem, "Использование: /mode readonly|approve|full", "")
			break
		}
		mode, ok := parseAgentSecurityMode(strings.ToLower(args[0]))
		if !ok {
			m.appendRow(UIMessageRoleSystem, "Неизвестный режим: "+args[0], "")
			break
		}
		m.patchChat(map[string]any{"security_mode": string(mode)})
	case "/games":
		domain := m.activeDomain
		if domain == "" {
			m.appendRow(UIMessageRoleSystem, "У чата не задан домен", "")
			break
		}
		m.appendRow(UIMessageRoleSystem, "Загружаю список игр для "+domain+"…", "")
		return m.fetchGamesCmd(domain)
	case "/login":
		if len(args) < 3 {
			m.appendRow(UIMessageRoleSystem, "Использование: /login <домен> <логин> <пароль>", "")
			break
		}
		m.appendRow(UIMessageRoleSystem, "Вход на "+args[0]+"…", "")
		return m.loginCmd(args[0], args[1], args[2])
	case "/logout":
		if len(args) < 1 {
			m.appendRow(UIMessageRoleSystem, "Использование: /logout <домен>", "")
			break
		}
		m.registry.Logout(args[0])
		m.appendRow(UIMessageRoleSystem, "Сессия "+args[0]+" удалена", "")
	case "/auth":
		m.appendRow(UIMessageRoleSystem, formatAuthStatus(m.registry.ListStatus()), "")
	case "/delete":
		m.deleteActiveChat()
	case "/export":
		format := "md"
		if len(args) > 0 {
			format = strings.ToLower(args[0])
		}
		m.appendRow(UIMessageRoleSystem, m.exportActiveChat(format), "")
	default:
		m.appendRow(UIMessageRoleSystem, "Неизвестная команда: "+cmd+" (/help)", "")
	}
	return nil
}

func (m *chatModel) patchChat(patch map[string]any) {
	data, err := json.Marshal(patch)
	if err != nil {
		m.appendRow(UIMessageRoleSystem, "Ошибка: "+err.Error(), "")
		return
	}
	if _, ok := m.store.Update(m.chatID, data); !ok {
		m.appendRow(UIMessageRoleSystem, "Чат не найден", "")
		return
	}
	m.store.Persist(m.chatID)
	m.refreshChats()
	m.appendRow(UIMessageRoleSystem, "Обновлено: "+string(data), "")
}

func (m *chatModel) deleteActiveChat() {
	id := m.chatID
	if !m.store.Delete(id) {
		m.appendRow(UIMessageRoleSystem, "Чат не найден", "")
		return
	}
	m.store.RemovePersisted(id)
	m.chatID = ""
	m.ensureActiveChat()
	m.appendRow(UIMessageRoleSystem, "Чат "+id+" удалён", "")
}

func (m *chatModel) exportActiveChat(format string) string {
	snap, ok := m.store.Get(m.chatID)
	if !ok {
		return "Чат не найден"
	}
	var (
		path string
		data []byte
	)
	switch format {
	case "json":
		path = "chat-" + snap.ID + ".json"
		encoded, err := json.MarshalIndent(snap, "", "  ")
		if err != nil {
			return "Ошибка экспорта: " + err.Error()
		}
		data = encoded
	case "md", "markdown":
		path = "chat-" + snap.ID + ".md"
		data = []byte(exportChatMarkdown(snap))
	default:
		return "Формат должен быть md или json"
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "Ошибка экспорта: " + err.Error()
	}
	return "Экспортировано в " + path
}

func formatAuthStatus(list []AuthDomainStatus) string {
	if len(list) == 0 {
		return "Сохранённых сессий нет"
	}
	var b strings.Builder
	b.WriteString("Сессии:")
	for _, st := range list {
		state := "нет"
		if st.HasSession {
			state = "есть"
		}
		fmt.Fprintf(&b, "\n  %s — сессия: %s", st.Domain, state)
		if st.Login != "" {
			b.WriteString(", логин: " + st.Login)
		}
	}
	return b.String()
}

func (m *chatModel) fetchGamesCmd(domain string) tea.Cmd {
	ctx, registry, cfg := m.ctx, m.registry, m.cfg
	return func() tea.Msg {
		games, err := fetchWebGames(ctx, registry, cfg, domain)
		if err != nil {
			return chatInfoMsg{text: "Не удалось получить игры: " + err.Error()}
		}
		if len(games) == 0 {
			return chatInfoMsg{text: "Игр не найдено на " + domain}
		}
		var b strings.Builder
		fmt.Fprintf(&b, "Игры на %s:", domain)
		for _, g := range games {
			fmt.Fprintf(&b, "\n  %d — %s (%s)", g.ID, g.Title, roleLabelRu(g.Role))
		}
		return chatInfoMsg{text: b.String()}
	}
}

func (m *chatModel) loginCmd(domain, login, password string) tea.Cmd {
	ctx, registry, cfg := m.ctx, m.registry, m.cfg
	return func() tea.Msg {
		if err := registry.Login(ctx, domain, login, password, encOptsFromConfig(cfg)); err != nil {
			return chatInfoMsg{text: "Вход не выполнен: " + err.Error()}
		}
		return chatInfoMsg{text: "Вход выполнен: " + login + "@" + domain}
	}
}
