package main

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/skrashevich/encx-cli/encx"
)

func resolveAgentConfig() (AgentConfig, error) {
	baseURL := cmp.Or(os.Getenv("LLM_BASE_URL"), os.Getenv("OPENROUTER_BASE_URL"), defaultLLMBaseURL)
	apiURL := strings.TrimRight(baseURL, "/") + "/chat/completions"
	apiKey := cmp.Or(os.Getenv("LLM_API_KEY"), os.Getenv("OPENROUTER_API_KEY"))
	if apiKey == "" && !strings.Contains(baseURL, "127.0.0.1") && !strings.Contains(baseURL, "localhost") {
		return AgentConfig{}, fmt.Errorf("LLM_API_KEY (or OPENROUTER_API_KEY) is required for web agent mode")
	}
	model := cmp.Or(os.Getenv("LLM_MODEL"), os.Getenv("OPENROUTER_MODEL"), defaultLLMModel)
	return AgentConfig{
		APIURL:  apiURL,
		APIKey:  apiKey,
		Model:   model,
		BaseURL: baseURL,
	}, nil
}

func chatConfigFromThread(base *config, t *ChatThread) *config {
	cfg := *base
	cfg.domain = t.Domain
	cfg.gameId = t.GameID
	cfg.jsonOutput = true
	return &cfg
}

func ensureChatSystemPrompt(t *ChatThread, cfg *config) {
	if t.session == nil {
		t.session = &llmSession{}
	}
	prompt := buildSystemPrompt(cfg, t.session) + sessionLoadedLevelsBlock(t.session)
	if len(t.messages) == 0 || t.messages[0].Role != "system" {
		t.messages = append([]llmMessage{{Role: "system", Content: prompt}}, t.messages...)
		return
	}
	t.messages[0].Content = prompt
}

func lastUserMessageContent(messages []llmMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return messages[i].Content
		}
	}
	return ""
}

func runWebChatTurn(ctx context.Context, hub *webHub, chatID string) {
	agentCfg, err := resolveAgentConfig()
	if err != nil {
		hub.publishSSE(chatID, agentEventError, map[string]any{"message": err.Error()})
		hub.publishSSE(chatID, agentEventDone, map[string]any{"chat_id": chatID})
		return
	}

	t, unlock, ok := hub.store.LockThread(chatID)
	if !ok {
		hub.publishSSE(chatID, agentEventError, map[string]any{"message": "chat not found"})
		hub.publishSSE(chatID, agentEventDone, map[string]any{"chat_id": chatID})
		return
	}
	defer unlock()

	chatCfg := chatConfigFromThread(hub.cfg, t)
	if t.session == nil {
		t.session = &llmSession{}
	}
	if last := lastUserMessageContent(t.messages); last != "" {
		if !t.session.reviewApprovalMode {
			t.session.preferRussian = looksLikeRussian(last)
		}
		if !t.session.reviewApprovalMode && isReviewApprovalPrompt(last) {
			t.session.reviewApprovalMode = true
		}
	}
	ensureChatSystemPrompt(t, chatCfg)

	client := hub.registry.Get(t.Domain, encOptsFromConfig(hub.cfg))
	tools := getToolsForSession(t.session)

	loopIn := AgentRunInput{
		Cfg:      chatCfg,
		Client:   client,
		Session:  t.session,
		Messages: t.messages,
		Tools:    tools,
	}

	hub.publishSSE(chatID, "status", map[string]any{
		"phase":   "start",
		"message": "Агент запущен…",
	})

	var runErr error
	hub.registry.WithDomainLock(t.Domain, func() {
		_, runErr = runAgentLoop(ctx, agentCfg, &loopIn, AgentCallbacks{
		OnEvent: func(ev AgentEvent) {
			hub.handleAgentEvent(chatID, t, ev)
		},
		OnStatus: func(phase, message string) {
			hub.publishSSE(chatID, "status", map[string]any{
				"phase":   phase,
				"message": strings.TrimSpace(message),
			})
		},
		Stderrf: func(format string, args ...any) {
			line := strings.TrimSpace(fmt.Sprintf(format, args...))
			if line == "" {
				return
			}
			hub.publishSSE(chatID, "stderr", map[string]any{"line": line})
			hub.publishSSE(chatID, "status", map[string]any{
				"phase":   "log",
				"message": line,
			})
		},
		RunPendingApprovals: func(ctx context.Context, cfg *config, client *encx.Client, session *llmSession) {
			runWebPendingFixApprovals(ctx, hub, chatID, cfg, client, session)
		},
		ApproveToolCall: func(ctx context.Context, toolName, argsJSON string) (bool, error) {
			return runWebToolApproval(ctx, hub, chatID, toolName, argsJSON)
		},
		})
	})

	t.messages = loopIn.Messages
	t.UpdatedAt = time.Now().UTC()
	hub.store.Persist(chatID)

	if runErr != nil {
		hub.publishSSE(chatID, agentEventError, map[string]any{"message": runErr.Error()})
	}
}

func (h *webHub) handleAgentEvent(chatID string, t *ChatThread, ev AgentEvent) {
	switch ev.Type {
	case agentEventAssistantText:
		if ev.Text == "" {
			return
		}
		h.publishSSE(chatID, "status", map[string]any{
			"phase":   "stream",
			"message": "Модель сформировала ответ",
		})
		t.appendUIMessage(UIMessageRoleAssistant, ev.Text, "")
		h.publishSSE(chatID, agentEventAssistantText, map[string]any{"text": ev.Text})
	case agentEventToolStart:
		t.appendUIMessage(UIMessageRoleTool, formatToolCallForDisplay(t.session, ev.ToolName, ev.ToolArgs), ev.ToolName)
		h.publishSSE(chatID, agentEventToolStart, map[string]any{
			"name":    ev.ToolName,
			"args":    ev.ToolArgs,
			"action":  formatToolApprovalAction(t.session, ev.ToolName, ev.ToolArgs),
			"details": formatToolApprovalDetails(t.session, ev.ToolName, ev.ToolArgs),
		})
	case agentEventToolDone:
		h.publishSSE(chatID, agentEventToolDone, map[string]any{
			"name":   ev.ToolName,
			"result": ev.ToolResult,
		})
	case agentEventReport:
		if ev.Report != "" {
			h.publishSSE(chatID, "report", map[string]any{"text": ev.Report})
		}
	case agentEventWarning:
		if ev.Message != "" {
			h.publishSSE(chatID, agentEventWarning, map[string]any{"message": ev.Message})
		}
	case agentEventError:
		msg := ev.Message
		if msg == "" && ev.Err != nil {
			msg = ev.Err.Error()
		}
		h.publishSSE(chatID, agentEventError, map[string]any{"message": msg})
	case agentEventDone:
		h.publishSSE(chatID, agentEventDone, map[string]any{"chat_id": chatID})
	case agentEventApprovalNeed:
		h.publishSSE(chatID, agentEventApprovalNeed, map[string]any{
			"count": len(ev.PendingFixes),
		})
	}
	h.store.Persist(chatID)
}

func (t *ChatThread) appendUIMessage(role UIMessageRole, content, toolName string) {
	msg := UIMessage{
		ID:        newUIMessageID(),
		Role:      role,
		Content:   content,
		ToolName:  toolName,
		CreatedAt: time.Now().UTC(),
	}
	t.uiMessages = append(t.uiMessages, msg)
	t.UpdatedAt = msg.CreatedAt
}

func newUIMessageID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err == nil {
		return hex.EncodeToString(b[:])
	}
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
