package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/skrashevich/encx-cli/encx"
)

// cmdChat runs the full-screen TUI chat. It shares the agent engine, the chat
// store and the auth registry with `encli -web`.
func cmdChat(ctx context.Context, cfg *config) error {
	// executeToolCallSafe swaps the process-wide os.Stdout during every tool
	// call, so Bubble Tea must render to the handle captured here.
	origStdout := os.Stdout

	registry := NewAuthRegistry()
	store := NewChatStore()
	if err := store.LoadFromDisk(); err != nil {
		debugf("load chats from disk: %v", err)
	}
	agentCfg, agentErr := resolveAgentConfig()

	m := newChatModel(ctx, cfg, store, registry, agentCfg, agentErr)

	// Debug output goes to stderr, which would corrupt the alternate screen.
	if cfg.debug {
		if restore, path, err := redirectStderrToFile(); err == nil {
			m.debugNote = "debug → " + path
			defer restore()
		} else {
			debugMode = false
			m.debugNote = "debug выключён для TUI: " + err.Error()
		}
	}

	program := tea.NewProgram(m,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
		tea.WithOutput(origStdout),
	)
	m.program = program

	prevApprovalLog := approvalLog
	approvalLog = func(s string) {
		if line := strings.TrimSpace(s); line != "" {
			program.Send(agentLogMsg{line: line})
		}
	}
	defer func() { approvalLog = prevApprovalLog }()

	_, err := program.Run()
	if m.cancelRun != nil {
		m.cancelRun()
	}
	if cfg.harRecording {
		exportRegistryHAR(registry, cfg)
	}
	if errors.Is(err, tea.ErrProgramKilled) {
		return nil
	}
	return err
}

// redirectStderrToFile points os.Stderr (used by debugf) at a log file for the
// lifetime of the TUI and returns a restore func.
func redirectStderrToFile() (func(), string, error) {
	path := filepath.Join(sessionDir(), "chat-debug.log")
	if err := os.MkdirAll(sessionDir(), 0o700); err != nil {
		return nil, "", err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, "", err
	}
	prev := os.Stderr
	os.Stderr = f
	return func() {
		os.Stderr = prev
		_ = f.Close()
	}, path, nil
}

// chatBridge runs one agent turn on a goroutine and reports back through
// program.Send. It never writes to the real stdio.
type chatBridge struct {
	program  *tea.Program
	store    *ChatStore
	registry *AuthRegistry
	cfg      *config
	agentCfg AgentConfig
	chatID   string
}

func (b *chatBridge) send(msg tea.Msg) {
	if b.program != nil {
		b.program.Send(msg)
	}
}

// startTurn appends the user message and launches the agent bridge.
func (m *chatModel) startTurn(userText string) tea.Cmd {
	msg, ok, busy := m.store.AppendUserMessageUnlessRunning(m.chatID, userText)
	if !ok {
		m.status = "чат не найден"
		return nil
	}
	if busy {
		m.status = "агент уже выполняет запрос"
		return nil
	}
	m.store.Persist(m.chatID)
	m.transcript = append(m.transcript, msg)
	m.refreshViewport()

	runCtx, cancel := context.WithCancel(m.ctx)
	m.cancelRun = cancel
	m.running = true
	m.status = "запуск агента…"
	m.store.WithRunning(m.chatID, true, cancel)

	bridge := &chatBridge{
		program:  m.program,
		store:    m.store,
		registry: m.registry,
		cfg:      m.cfg,
		agentCfg: m.agentCfg,
		chatID:   m.chatID,
	}
	go bridge.run(runCtx)
	m.refreshChats()
	return nil
}

func (b *chatBridge) run(ctx context.Context) {
	var runErr error
	defer func() {
		if rec := recover(); rec != nil {
			runErr = fmt.Errorf("agent panic: %v", rec)
		}
		b.send(turnDoneMsg{err: runErr})
	}()

	t, unlock, ok := b.store.LockThread(b.chatID)
	if !ok {
		runErr = errors.New("chat not found")
		return
	}
	defer unlock()

	chatCfg := chatConfigFromThread(b.cfg, t)
	if t.session == nil {
		t.session = &llmSession{}
	}
	if last := lastUserMessageContent(t.messages); last != "" {
		t.session.preferRussian = looksLikeRussian(last)
	}
	ensureChatSystemPrompt(t, chatCfg)

	client := b.registry.Get(t.Domain, encOptsFromConfig(b.cfg))
	loopIn := AgentRunInput{
		Cfg:      chatCfg,
		Client:   client,
		Session:  t.session,
		Messages: t.messages,
		Tools:    getToolsForSession(t.session),
	}

	b.registry.WithDomainLock(t.Domain, func() {
		_, runErr = runAgentLoop(ctx, b.agentCfg, &loopIn, b.callbacks(t))
	})

	t.messages = loopIn.Messages
	t.UpdatedAt = time.Now().UTC()
	b.store.Persist(b.chatID)
}

func (b *chatBridge) callbacks(t *ChatThread) AgentCallbacks {
	return AgentCallbacks{
		OnEvent: func(ev AgentEvent) {
			display := ""
			switch ev.Type {
			case agentEventAssistantText:
				if ev.Text == "" {
					return
				}
				t.appendUIMessage(UIMessageRoleAssistant, ev.Text, "")
			case agentEventToolStart:
				display = formatToolCallForDisplay(t.session, ev.ToolName, ev.ToolArgs)
				t.appendUIMessage(UIMessageRoleTool, display, ev.ToolName)
			}
			b.store.Persist(b.chatID)
			b.send(agentEvtMsg{ev: ev, display: display})
		},
		OnStatus: func(phase, message string) {
			b.send(agentStatusMsg{phase: phase, message: strings.TrimSpace(message)})
		},
		Stderrf: func(format string, args ...any) {
			line := strings.TrimSpace(fmt.Sprintf(format, args...))
			if line == "" {
				return
			}
			b.send(agentStatusMsg{phase: "log", message: line})
		},
		ApproveToolCall: func(ctx context.Context, toolName, argsJSON string) (bool, error) {
			decision, err := b.askApproval(ctx, "tool",
				formatToolApprovalAction(t.session, toolName, argsJSON),
				formatToolApprovalDetails(t.session, toolName, argsJSON))
			if err != nil {
				return false, err
			}
			switch decision {
			case approveYes:
				return true, nil
			case approveQuit:
				return false, errors.New("tool approval stopped by user")
			default:
				return false, nil
			}
		},
		RunPendingApprovals: func(ctx context.Context, cfg *config, client *encx.Client, session *llmSession) {
			b.runPendingApprovals(ctx, cfg, client, session)
		},
	}
}

// askApproval hands a prompt to the Bubble Tea loop and waits for the answer.
func (b *chatBridge) askApproval(ctx context.Context, kind, title string, details []string) (approvalDecision, error) {
	if b.program == nil {
		return approveNo, nil
	}
	reply := make(chan approvalDecision, 1)
	b.send(approvalNeededMsg{kind: kind, title: title, details: details, reply: reply})
	select {
	case decision := <-reply:
		return decision, nil
	case <-ctx.Done():
		return approveNo, ctx.Err()
	}
}

// runPendingApprovals is the TUI replacement for runPendingFixApprovals, which
// reads os.Stdin and writes os.Stderr directly.
func (b *chatBridge) runPendingApprovals(ctx context.Context, cfg *config, client *encx.Client, session *llmSession) {
	if session == nil || len(session.pendingFixes) == 0 {
		return
	}
	outcomes := make([]proposalOutcome, 0, len(session.pendingFixes))
	total := len(session.pendingFixes)
	idx := 0
	for len(session.pendingFixes) > 0 {
		idx++
		fix := session.pendingFixes[0]
		title := fmt.Sprintf("[%d/%d] %s", idx, total, fix.Title)
		decision, err := b.askApproval(ctx, "fix", title, fixApprovalDetails(session, fix))
		if err != nil {
			return
		}
		switch decision {
		case approveYes:
			outcomes = append(outcomes, applyPendingAdminFix(ctx, cfg, client, session, fix))
			session.pendingFixes = session.pendingFixes[1:]
		case approveQuit:
			outcomes = append(outcomes, proposalOutcome{Title: fix.Title, Stopped: true})
			session.pendingFixes = nil
			printApprovalSummary(session, outcomes)
			b.store.Persist(b.chatID)
			return
		default:
			outcomes = append(outcomes, proposalOutcome{Title: fix.Title, Skipped: true})
			session.pendingFixes = session.pendingFixes[1:]
		}
	}
	printApprovalSummary(session, outcomes)
	session.pendingFixes = nil
	b.store.Persist(b.chatID)
}

// fixApprovalDetails describes one proposed fix as approval bullets.
func fixApprovalDetails(session *llmSession, fix pendingAdminFix) []string {
	var details []string
	if fix.LevelNumber > 0 {
		details = append(details, session.reviewText(
			fmt.Sprintf("Level %d", fix.LevelNumber),
			fmt.Sprintf("Уровень %d", fix.LevelNumber),
		))
	}
	if fix.Summary != "" {
		details = append(details, session.reviewText("Why: "+fix.Summary, "Почему: "+fix.Summary))
	}
	for _, step := range fix.Steps {
		details = append(details, "- "+describeProposalStep(step))
	}
	return details
}
