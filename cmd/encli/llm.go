package main

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/skrashevich/encx-cli/encx"
)

const (
	defaultLLMBaseURL  = "https://openrouter.ai/api/v1"
	defaultLLMModel    = "openai/gpt-oss-120b:free"
	maxAgentTurns      = 200
	maxToolItemsForLLM = 200
	maxToolTextForLLM  = 240
	maxFixSteps        = 8
)

// agentMode controls fatal behavior for nested tool execution: when true,
// fatal panics instead of os.Exit so executeToolCallSafe can recover and
// return a structured tool error back to the LLM.
var agentMode bool

// agentFatalError is the panic value used by fatal in agent mode.
type agentFatalError struct {
	Message string
}

// llmTool is the persisted CLI catalog definition adapted into a PicoClaw tool.
type llmTool struct {
	Type     string      `json:"type"`
	Function llmFunction `json:"function"`
}

type llmFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type llmMessage struct {
	Role       string        `json:"role"`
	Content    string        `json:"content,omitempty"`
	ToolCalls  []llmToolCall `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
}

type llmToolCall struct {
	ID       string              `json:"id"`
	Type     string              `json:"type"`
	Function llmToolCallFunction `json:"function"`
}

type llmToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type llmSession struct {
	securityMode           AgentSecurityMode
	reviewApprovalMode     bool
	applyingApprovedFix    bool
	preferRussian          bool
	pendingFixes           []pendingAdminFix
	loadedLevelContent     map[int]struct{} // levels read via admin_level_content in this chat
	enumeratedLevelNumbers []int            // from latest admin_levels in this agent run
	levelCompletionNudges  int              // auto-nudges when model answers before loading all levels
}

func cmdLLM(ctx context.Context, cfg *config, client *encx.Client, prompt string) {
	baseURL := cmp.Or(os.Getenv("LLM_BASE_URL"), os.Getenv("OPENROUTER_BASE_URL"), defaultLLMBaseURL)

	apiKey := cmp.Or(os.Getenv("LLM_API_KEY"), os.Getenv("OPENROUTER_API_KEY"))
	if apiKey == "" && !strings.Contains(baseURL, "127.0.0.1") && !strings.Contains(baseURL, "localhost") {
		fatal("LLM_API_KEY (or OPENROUTER_API_KEY) environment variable is required for --llm mode")
	}

	model := cmp.Or(os.Getenv("LLM_MODEL"), os.Getenv("OPENROUTER_MODEL"), defaultLLMModel)

	// Force JSON output in LLM mode for structured results.
	// Keep fatal exit behavior at the top level; only nested tool calls should
	// panic and be recovered back into the agent loop.
	cfg.jsonOutput = true
	jsonMode = true
	session := newLLMSessionForPrompt(prompt)
	if cfg.agentReadonly {
		session.securityMode = SecurityModeReadonly
	}

	systemPrompt := buildSystemPrompt(cfg, session)

	messages := []llmMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: prompt},
	}

	tools := getToolsForSession(session)
	debugf("picoclaw mode initialized: base_url=%s model=%s review_mode=%v tools=%d prompt=%q", baseURL, model, session.reviewApprovalMode, len(tools), summarizeDebugText(prompt, 0))

	loopIn := AgentRunInput{
		Cfg:      cfg,
		Client:   client,
		Session:  session,
		Messages: messages,
		Tools:    tools,
	}
	ac := AgentConfig{
		APIKey:  apiKey,
		Model:   model,
		BaseURL: baseURL,
	}

	_, err := runAgentLoop(ctx, ac, &loopIn, AgentCallbacks{
		OnEvent: func(ev AgentEvent) {
			switch ev.Type {
			case agentEventAssistantText:
				if ev.Text != "" {
					fmt.Println(formatMarkdownForTerminal(ev.Text))
				}
			case agentEventToolStart:
				fmt.Fprintf(os.Stderr, "%s\n", formatToolCallForDisplay(session, ev.ToolName, ev.ToolArgs))
			case agentEventReport:
				fmt.Fprint(os.Stderr, ev.Report)
			case agentEventWarning:
				fmt.Fprintln(os.Stderr, ev.Message)
			}
		},
		Stderrf: func(format string, args ...any) {
			fmt.Fprintf(os.Stderr, format, args...)
		},
	})
	if err != nil {
		fatal("%v", err)
	}
}
