package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/glamour"
	glamouransi "github.com/charmbracelet/glamour/ansi"
	glamourstyles "github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
)

const (
	chatSidebarWidth = 26
	chatInputHeight  = 3
	// header + status + bordered bottom pane (input or approval)
	chatChromeHeight = 1 + 1 + chatInputHeight + 2
)

var (
	chatHeaderStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	chatHeaderMetaStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	chatStatusStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	chatErrorStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	chatSidebarStyle    = lipgloss.NewStyle().
				Width(chatSidebarWidth).
				Border(lipgloss.NormalBorder(), false, true, false, false).
				BorderForeground(lipgloss.Color("240"))
	chatSidebarItemStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	chatSidebarActiveStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Bold(true)
	chatUserStyle           = lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
	chatAssistantStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	chatToolStyle           = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	chatSystemStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	chatBoxStyle            = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("240"))
	chatApprovalBoxStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("11"))
	chatApprovalTitleStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
	chatApprovalPromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
)

func (m *chatModel) View() string {
	if !m.ready {
		return "encli chat — запуск…"
	}
	if m.width < 40 || m.height < 12 {
		return "Окно терминала слишком мало для encli -chat"
	}
	bottom := m.bottomView()
	bodyHeight := max(1, m.height-2-lipgloss.Height(bottom))
	return strings.Join([]string{
		m.headerView(),
		m.bodyView(bodyHeight),
		m.statusView(),
		bottom,
	}, "\n")
}

func (m *chatModel) headerView() string {
	title := m.activeTitle
	if title == "" {
		title = "чат"
	}
	var meta []string
	if m.activeDomain != "" && m.activeDomain != title {
		meta = append(meta, m.activeDomain)
	}
	if m.activeGameID != 0 {
		meta = append(meta, fmt.Sprintf("игра %d", m.activeGameID))
	}
	meta = append(meta, "режим "+string(m.activeMode.effective()))
	if m.agentCfg.Model != "" {
		meta = append(meta, m.agentCfg.Model)
	}
	line := chatHeaderStyle.Render("encli chat · "+title) + " " +
		chatHeaderMetaStyle.Render(strings.Join(meta, " · "))
	return truncateToWidth(line, m.width)
}

func (m *chatModel) statusView() string {
	parts := []string{}
	switch {
	case m.running:
		parts = append(parts, "● агент работает (Esc — отмена)")
	case m.agentErr != nil:
		parts = append(parts, "○ агент отключён")
	default:
		parts = append(parts, "○ готов")
	}
	if m.status != "" {
		parts = append(parts, m.status)
	}
	if m.debugNote != "" {
		parts = append(parts, m.debugNote)
	}
	line := strings.Join(parts, " · ")
	style := chatStatusStyle
	if m.agentErr != nil {
		style = chatErrorStyle
	}
	return truncateToWidth(style.Render(line), m.width)
}

func (m *chatModel) bodyView(height int) string {
	m.vp.Height = height
	transcript := m.vp.View()
	if !m.sidebar {
		return transcript
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, m.sidebarView(height), transcript)
}

func (m *chatModel) sidebarView(height int) string {
	var lines []string
	for i, c := range m.chats {
		title := c.Title
		if title == "" {
			title = c.ID
		}
		marker := "  "
		style := chatSidebarItemStyle
		if c.ID == m.chatID {
			marker = "▸ "
			style = chatSidebarActiveStyle
		}
		if m.focus == focusSidebar && i == m.sidebarIdx {
			marker = "> "
		}
		if c.Running {
			title = "● " + title
		}
		lines = append(lines, style.Render(truncateToWidth(marker+title, chatSidebarWidth)))
		if len(lines) >= height {
			break
		}
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return chatSidebarStyle.Height(height).Render(strings.Join(lines, "\n"))
}

func (m *chatModel) bottomView() string {
	width := max(10, m.width-2)
	if m.approval != nil {
		return chatApprovalBoxStyle.Width(width).Render(m.approvalContent(width))
	}
	return chatBoxStyle.Width(width).Render(m.input.View())
}

func (m *chatModel) approvalContent(width int) string {
	kind := "инструмент"
	if m.approval.kind == "fix" {
		kind = "исправление"
	}
	lines := []string{
		chatApprovalTitleStyle.Render(truncateToWidth("Подтвердить "+kind+": "+m.approval.title, width)),
	}
	for _, d := range m.approval.details {
		if len(lines) >= chatInputHeight {
			break
		}
		lines = append(lines, truncateToWidth("  "+d, width))
	}
	for len(lines) < chatInputHeight {
		lines = append(lines, "")
	}
	lines[chatInputHeight-1] = chatApprovalPromptStyle.Render("[y] применить  [n] пропустить  [q] прервать")
	return strings.Join(lines[:chatInputHeight], "\n")
}

// renderTranscript builds the viewport content for the active chat.
func (m *chatModel) renderTranscript() string {
	width := max(10, m.vp.Width-1)
	wrap := lipgloss.NewStyle().Width(width)
	var b strings.Builder
	for i, msg := range m.transcript {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(chatRoleLabel(msg))
		b.WriteString("\n")
		if msg.Role == UIMessageRoleAssistant {
			// glamour word-wraps and emits ANSI itself; do not re-wrap via lipgloss.
			b.WriteString(m.renderAssistantMarkdown(msg.Content, width))
		} else {
			b.WriteString(wrap.Render(msg.Content))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// renderAssistantMarkdown renders LLM markdown to ANSI for the transcript,
// falling back to the lightweight table formatter if glamour is unavailable.
func (m *chatModel) renderAssistantMarkdown(content string, wrap int) string {
	r := m.markdownRenderer(wrap)
	if r == nil {
		return lipgloss.NewStyle().Width(wrap).Render(formatMarkdownForTerminal(content))
	}
	out, err := r.Render(content)
	if err != nil {
		return lipgloss.NewStyle().Width(wrap).Render(formatMarkdownForTerminal(content))
	}
	return strings.Trim(out, "\n")
}

// markdownRenderer lazily builds a glamour renderer, rebuilding it when the
// wrap width changes (glamour fixes the wrap width at construction time).
func (m *chatModel) markdownRenderer(wrap int) *glamour.TermRenderer {
	if wrap < 20 {
		wrap = 20
	}
	if m.mdRenderer != nil && m.mdWrap == wrap {
		return m.mdRenderer
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(chatMarkdownStyle()),
		glamour.WithWordWrap(wrap),
		glamour.WithEmoji(),
	)
	if err != nil {
		m.mdRenderer = nil
		m.mdWrap = 0
		return nil
	}
	m.mdRenderer = r
	m.mdWrap = wrap
	return r
}

// chatMarkdownStyle is glamour's dark theme without the document margin and
// full-width background fill, so assistant messages align under the role label
// and do not overflow the viewport. The global DarkStyleConfig is left intact
// (only the top-level pointers are swapped, never the pointees).
func chatMarkdownStyle() glamouransi.StyleConfig {
	s := glamourstyles.DarkStyleConfig
	var zero uint
	s.Document.Margin = &zero
	s.Document.Color = nil
	s.Document.BlockPrefix = ""
	s.Document.BlockSuffix = ""
	return s
}

func chatRoleLabel(msg UIMessage) string {
	switch msg.Role {
	case UIMessageRoleUser:
		return chatUserStyle.Render("вы")
	case UIMessageRoleAssistant:
		return chatAssistantStyle.Render("агент")
	case UIMessageRoleTool:
		name := msg.ToolName
		if name == "" {
			name = "tool"
		}
		return chatToolStyle.Render("· " + name)
	default:
		return chatSystemStyle.Render("система")
	}
}

func truncateToWidth(s string, width int) string {
	if width <= 0 {
		return ""
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(s)
}
