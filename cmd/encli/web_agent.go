package main

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/skrashevich/encx-cli/encx"
)

// resolveAgentConfig is the single LLM transport resolution used by --llm,
// -chat and -web. The transport is an OpenAI-compatible API key, a ChatGPT
// subscription stored by `encli codex-login`, or GigaChat's OAuth key.
//
// Each field is resolved the same way: the command line, then the environment,
// then what the -web settings panel stored, then the built-in default.
func resolveAgentConfig(cfg *config) (AgentConfig, error) {
	var requested string
	if cfg != nil {
		requested = cfg.llmAuth
	}
	stored, err := loadLLMSettings()
	if err != nil {
		return AgentConfig{}, err
	}

	authMethod := strings.ToLower(resolveLLMField(requested, llmAuthEnvVars, stored.AuthMethod, "").Value)
	apiKey := resolveLLMField("", llmAPIKeyEnvVars, stored.APIKey, "").Value
	endpoint := resolveLLMField("", llmBaseURLEnvVars, stored.BaseURL, "").Value
	model := resolveLLMField("", llmModelEnvVars, stored.Model, "").Value

	// Without an explicit choice a stored ChatGPT sign-in or a GigaChat key is
	// used only when nothing else was configured. Both an API key and a base URL
	// are deliberate statements of intent — in particular a local proxy needs no
	// key, so a credential left over from an earlier `codex-login` must not
	// silently redirect that setup to chatgpt.com.
	//
	// When nothing at all is configured the agent runs on this machine. That is
	// the last resort rather than a preference: every cloud transport an operator
	// did set up outranks it, because a model that fits on a laptop is no match
	// for one that does not.
	if authMethod == "" && apiKey == "" && endpoint == "" {
		switch {
		case hasCodexCredential():
			authMethod = authMethodCodex
		case hasGigaChatCredentials():
			authMethod = authMethodGigaChat
		default:
			authMethod = authMethodLocal
		}
	}

	switch authMethod {
	case authMethodLocal:
		local := localConfigFrom(stored)
		return AgentConfig{
			AuthMethod: authMethodLocal,
			Model:      localModelDisplayName(local.modelRef),
			Local:      local,
		}, nil
	case authMethodCodex:
		if _, err := loadCodexCredential(); err != nil {
			return AgentConfig{}, err
		}
		return AgentConfig{
			AuthMethod: authMethodCodex,
			Model:      codexModel(model),
		}, nil
	case authMethodGigaChat:
		gigachat, err := gigachatConfigFromEnv(model)
		if err != nil {
			return AgentConfig{}, err
		}
		return AgentConfig{
			AuthMethod: authMethodGigaChat,
			Model:      gigachat.model,
			BaseURL:    gigachat.baseURL,
		}, nil
	case "", authMethodAPIKey:
	default:
		return AgentConfig{}, fmt.Errorf("unknown LLM auth method %q: use local, apikey, codex or gigachat", authMethod)
	}

	baseURL := cmp.Or(endpoint, defaultLLMBaseURL)
	if apiKey == "" && !strings.Contains(baseURL, "127.0.0.1") && !strings.Contains(baseURL, "localhost") {
		return AgentConfig{}, fmt.Errorf(
			"LLM_API_KEY (or OPENROUTER_API_KEY) is required for agent mode; " +
				"alternatively sign in to a ChatGPT subscription with 'encli codex-login', " +
				"run the agent on this machine with LLM_AUTH=local, " +
				"or configure a provider in the -web LLM settings panel")
	}
	return AgentConfig{
		APIKey:  apiKey,
		Model:   cmp.Or(model, defaultLLMModel),
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
	t, unlock, ok := hub.store.LockThread(chatID)
	if !ok {
		hub.publishSSE(chatID, agentEventError, map[string]any{"message": "chat not found"})
		hub.publishSSE(chatID, agentEventDone, map[string]any{"chat_id": chatID})
		return
	}
	defer unlock()
	agentCfg, err := resolveAgentConfig(hub.cfg)
	if err != nil {
		hub.handleAgentEvent(chatID, t, AgentEvent{Type: agentEventError, Message: err.Error()})
		return
	}

	chatCfg := chatConfigFromThread(hub.cfg, t)
	if t.session == nil {
		t.session = &llmSession{}
	}
	if last := lastUserMessageContent(t.messages); last != "" {
		t.session.preferRussian = looksLikeRussian(last)
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
				if ev.Type != agentEventError {
					hub.handleAgentEvent(chatID, t, ev)
				}
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
				return runWebToolApproval(ctx, hub, chatID, t.session, toolName, argsJSON)
			},
		})
	})

	t.messages = loopIn.Messages
	t.UpdatedAt = time.Now().UTC()
	hub.store.Persist(chatID)

	if runErr != nil {
		hub.handleAgentEvent(chatID, t, AgentEvent{Type: agentEventError, Message: runErr.Error()})
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
		t.appendUIMessage(UIMessageRoleSystem, "Ошибка: "+msg, "")
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
