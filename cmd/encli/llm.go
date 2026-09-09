package main

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/skrashevich/encx-cli/encx"
)

const (
	defaultLLMBaseURL  = "https://openrouter.ai/api/v1"
	defaultLLMModel    = "openrouter/free"
	maxAgentTurns      = 200
	maxToolItemsForLLM = 200
	maxToolTextForLLM  = 240

	// maxToolContentForLLM budgets the tools that exist to read a document into
	// the conversation. maxToolTextForLLM is a summary width and would gut them.
	//
	// It matches read_local_file's own default max_bytes on purpose. A smaller
	// budget does not save context, it costs more of it: the model finds the
	// document cut short and asks again in another form, and every one of those
	// attempts is a turn. Identical re-reads are refused rather than re-sent, so
	// each distinct slice of a document is paid for exactly once per run.
	maxToolContentForLLM = defaultLocalReadMaxBytes
	maxFixSteps          = 8
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
	applyingApprovedFix    bool
	preferRussian          bool
	pendingFixes           []pendingAdminFix
	loadedLevelContent     map[int]struct{} // levels read via admin_level_content in this chat
	enumeratedLevelNumbers []int            // from latest admin_levels in this agent run
	levelCompletionNudges  int              // auto-nudges when model answers before loading all levels
}

func cmdLLM(ctx context.Context, cfg *config, client *encx.Client, prompt string) {
	ac, err := resolveAgentConfig(cfg)
	if err != nil {
		fatal("%v", err)
	}

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
	debugf("picoclaw mode initialized: auth=%s base_url=%s model=%s tools=%d prompt=%q",
		cmp.Or(ac.AuthMethod, authMethodAPIKey), ac.BaseURL, ac.Model, len(tools), summarizeDebugText(prompt, 0))

	loopIn := AgentRunInput{
		Cfg:      cfg,
		Client:   client,
		Session:  session,
		Messages: messages,
		Tools:    tools,
	}

	_, err = runAgentLoop(ctx, ac, &loopIn, AgentCallbacks{
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
