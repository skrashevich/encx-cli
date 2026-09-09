package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/sipeed/picoclaw/pkg/tools"
	toolshared "github.com/sipeed/picoclaw/pkg/tools/shared"
	"github.com/skrashevich/encx-cli/encx"
)

const (
	agentEventAssistantText = "assistant_text"
	agentEventToolStart     = "tool_start"
	agentEventToolDone      = "tool_done"
	agentEventApprovalNeed  = "approval_needed"
	agentEventReport        = "report"
	agentEventDone          = "done"
	agentEventError         = "error"
	agentEventWarning       = "warning"
)

// AgentEvent describes a milestone in runAgentLoop for UIs / harnesses (CLI, web, tests).
type AgentEvent struct {
	Type string

	Text    string // assistant_text, warning message
	Err     error
	Report  string // report (full stderr report block)
	Message string // human-readable (warning, auxiliary)

	ToolName   string // tool_start, tool_done
	ToolArgs   string
	ToolResult string // tool_done: payload passed to prepareToolResultForLLM output

	PendingFixes []pendingAdminFix // approval_needed
}

// AgentConfig holds the PicoClaw provider settings.
type AgentConfig struct {
	APIKey  string
	Model   string
	BaseURL string

	// AuthMethod selects the transport: empty or "apikey" uses APIKey against
	// BaseURL, "codex" uses the ChatGPT subscription stored by codex-login,
	// "gigachat" uses the GigaChat authorization key in GIGACHAT_CREDENTIALS.
	AuthMethod string

	// Provider overrides HTTP provider construction. It is used by tests and by
	// callers that already own a PicoClaw provider.
	Provider providers.LLMProvider
}

// AgentRunInput binds config, transport, mutable conversation state, and tool definitions.
type AgentRunInput struct {
	Cfg      *config
	Client   *encx.Client
	Session  *llmSession
	Messages []llmMessage // mutated in place; address of struct must be passed to runAgentLoop
	Tools    []llmTool
}

// AgentCallbacks observes the agent runner. Stderrf is used for retries and ancillary stderr lines.
//
// RunPendingApprovals overrides interactive approval handling when pending fixes exist (e.g. web UI).
// If nil, runAgentLoop invokes runPendingFixApprovals after emitting an approval_needed event.
//
// ApproveToolCall is invoked before each mutating tool when security mode is approve.
// Return false to skip the tool (user denied); error cancels the agent run.
type AgentCallbacks struct {
	OnEvent             func(AgentEvent)
	OnStatus            func(phase, message string)
	Stderrf             func(string, ...any)
	RunPendingApprovals func(context.Context, *config, *encx.Client, *llmSession)
	ApproveToolCall     func(context.Context, string, string) (bool, error)
}

func stderrAgentf(cb AgentCallbacks, format string, args ...any) {
	if cb.Stderrf != nil {
		cb.Stderrf(format, args...)
	}
}

func emitStatus(cb AgentCallbacks, phase, message string) {
	if cb.OnStatus != nil {
		cb.OnStatus(phase, message)
	}
	stderrAgentf(cb, "%s\n", message)
}

func emitAgent(cb AgentCallbacks, ev AgentEvent) {
	if cb.OnEvent != nil {
		cb.OnEvent(ev)
	}
}

func newLLMSessionForPrompt(prompt string) *llmSession {
	return &llmSession{
		preferRussian: looksLikeRussian(prompt),
	}
}

// systemPromptNow is the clock used when stamping the current date/time into
// the agent system prompt. It is a package var so tests can pin it.
var systemPromptNow = time.Now

func buildSystemPrompt(cfg *config, session *llmSession) string {
	now := systemPromptNow()
	return `You are an autonomous agent for the Encounter (en.cx) game engine CLI tool.
The user gives you a natural language request. Execute it step by step using the available tools.
The current domain is: ` + cfg.domain + `
The current date and time is: ` + now.Format(time.RFC3339) + ` (local time, RFC3339; ` + now.UTC().Format(time.RFC3339) + ` in UTC).
` + func() string {
		if cfg.gameId != 0 {
			return fmt.Sprintf("The current game ID is: %d\n", cfg.gameId)
		}
		return ""
	}() + `
Rules:
- TIME: Treat the "current date and time" above as authoritative. Never guess or infer today's date from memory. Resolve every relative time the user gives ("in a minute", "tonight", "tomorrow", "next Saturday") against that value, and echo the absolute date/time you computed so the user can check it.
- NEVER FABRICATE (strict): Do not invent, guess, or infer facts about game content, tool results, files, URLs, or anything else. If information is missing, call the appropriate tools to obtain it. If tools still cannot provide it, say clearly that the information is unavailable — do not fill gaps with assumptions, stereotypes, or plausible-sounding details.
- Execute multi-step tasks by calling tools one at a time. You will receive the result of each tool call.
- Use tool results to inform your next action (e.g., get level IDs before renaming levels).
- When all steps are complete, respond with a text summary of what was done.
- If a tool call fails, try to recover or report the error.
- For admin_copy_game: source is the first game mentioned, target is the second.
- Prefer admin_* tools for game management (viewing levels, creating content). Player tools (levels, status, bonuses) are for games IN PROGRESS.
- For reading level text, answers, hints, and other scenario content from the organizer side, prefer admin_level_content instead of player tools.
- TASK DECOMPOSITION: Enumeration tools (admin_levels, game lists, directory listings) return IDs, names, and metadata only — not full content. If the user needs scenario text, per-level details, or an audit/summary across items, call the read tool (admin_level_content, read_local_file, etc.) for every relevant item before your final answer. A complete-looking table or summary built only from names is wrong.
- IRREVERSIBLE ACTIONS: admin_delete_game destroys a whole game and admin_wipe_game empties one. Call either only when the user's latest message asks for that game to be deleted or emptied, and never as a step towards something else (for example, do not delete a game to "recreate it cleanly" unless asked). Deleting is not a way to fix a mistake in a game you just created.
- Starting/launching a game is NOT available via CLI — only through the web interface. Inform the user if they ask.
- When asked to CREATE a game/levels, make them INTERESTING and DIFFERENT: give unique names, add tasks with creative quest text, add sectors with answers, add hints. Don't just create empty shells.
- SCOPE: the user's LATEST message defines the task. Do what it asks and nothing more. If it asks for one action (for example "wipe the game"), perform that action and report the result — do not also resume an earlier request that was interrupted, cancelled, or replaced by this one. Resume previous work only when the user asks you to continue it.
- ALWAYS COMPLETE THE FULL TASK (within the scope above). If asked to create N levels, create ALL N levels with tasks, sectors (answers), and hints. Never stop partway through and offer to "continue if needed". You have up to 200 tool calls — use them. Do not summarize partial work as if it were complete.
- SELF-VERIFICATION: After creating or modifying levels, verify your own work by calling admin_level_content for each affected level. Check that: (1) all sector codes/answers are present and correct, (2) timings (autopass, answer block) are set to non-zero values if the level is timed, (3) hints are present if needed and have correct text/delays, (4) task text matches the intended answers. If you discover errors, fix them immediately before reporting success.
- LOCAL FILES: Use read_local_file, list_local_dir, and search_local_files to read scripts, notes, or scenario files on disk. Use read_pdf_file to extract text from a PDF (rulebook, uploaded document, scan) instead of read_local_file, which only handles text files. Paths are relative to LLM_FILES_ROOT (defaults to the current working directory). You cannot read files outside that root.
- WIKIPEDIA: Use wikipedia_search to find articles and wikipedia_article to read summaries when you need to verify facts, dates, places, or historical details for quest content.
- REVIEW/AUDIT REQUESTS: when the user asks to check, verify, audit, or review existing content WITHOUT explicitly asking for changes, do NOT call admin mutation tools directly. Call propose_admin_fix once per discovered issue (one proposal = one user approval decision), each with only the minimal admin mutation steps needed to resolve that one issue, then give a concise audit summary. Do not ask the user for confirmation in normal text; the interface handles approvals. When the user explicitly asks to create or modify content, use the admin mutation tools directly.
- Respond in the same language as the user's request.` + securityModeSystemPromptAddendum(session)
}

func formatAgentExecutionReport(session *llmSession, model string, pricing *llmPricing,
	totalElapsed, llmDur, toolDur time.Duration,
	turns, totalToolCalls, totalPromptTokens, totalCompletionTokens int,
) string {
	otherDur := totalElapsed - llmDur - toolDur
	if otherDur < 0 {
		otherDur = 0
	}

	var report strings.Builder
	if session.preferRussian {
		fmt.Fprintf(&report, "\n--- Отчёт о выполнении ---\n")
		fmt.Fprintf(&report, "Общее время:      %s\n", totalElapsed.Round(time.Millisecond))
		fmt.Fprintf(&report, "Модель:           %s\n", model)
		fmt.Fprintf(&report, "  Время LLM:      %s\n", llmDur.Round(time.Millisecond))
		fmt.Fprintf(&report, "  Время тулзов:   %s\n", toolDur.Round(time.Millisecond))
		fmt.Fprintf(&report, "  Накладные:      %s\n", otherDur.Round(time.Millisecond))
		fmt.Fprintf(&report, "Запросов к LLM:   %d\n", turns)
		fmt.Fprintf(&report, "Вызовов тулзов:   %d\n", totalToolCalls)
		if totalPromptTokens > 0 || totalCompletionTokens > 0 {
			fmt.Fprintf(&report, "Токены:           %d (вход: %d, выход: %d)\n",
				totalPromptTokens+totalCompletionTokens, totalPromptTokens, totalCompletionTokens)
			if pricing != nil && pricing.isSubscription {
				fmt.Fprintf(&report, "Стоимость:        $0 (подписка ChatGPT)\n")
			} else if pricing != nil && pricing.isLocal {
				fmt.Fprintf(&report, "Стоимость:        $0 (локальный прокси)\n")
			} else if pricing != nil {
				fmt.Fprintf(&report, "Стоимость:        $%.4f (OpenRouter pricing)\n", computeLLMCost(pricing, totalPromptTokens, totalCompletionTokens))
			}
		}
	} else {
		fmt.Fprintf(&report, "\n--- Execution Report ---\n")
		fmt.Fprintf(&report, "Total time:    %s\n", totalElapsed.Round(time.Millisecond))
		fmt.Fprintf(&report, "Model:         %s\n", model)
		fmt.Fprintf(&report, "  LLM time:    %s\n", llmDur.Round(time.Millisecond))
		fmt.Fprintf(&report, "  Tool time:   %s\n", toolDur.Round(time.Millisecond))
		fmt.Fprintf(&report, "  Overhead:    %s\n", otherDur.Round(time.Millisecond))
		fmt.Fprintf(&report, "LLM turns:     %d\n", turns)
		fmt.Fprintf(&report, "Tool calls:    %d\n", totalToolCalls)
		if totalPromptTokens > 0 || totalCompletionTokens > 0 {
			fmt.Fprintf(&report, "Tokens:        %d (in: %d, out: %d)\n",
				totalPromptTokens+totalCompletionTokens, totalPromptTokens, totalCompletionTokens)
			if pricing != nil && pricing.isSubscription {
				fmt.Fprintf(&report, "Cost:          $0 (ChatGPT subscription)\n")
			} else if pricing != nil && pricing.isLocal {
				fmt.Fprintf(&report, "Cost:          $0 (local proxy)\n")
			} else if pricing != nil {
				fmt.Fprintf(&report, "Cost:          $%.4f (OpenRouter pricing)\n", computeLLMCost(pricing, totalPromptTokens, totalCompletionTokens))
			}
		}
	}
	return report.String()
}

const levelReviewNudgeTool = "encx_continue_level_review"

var (
	disablePicoClawLogging sync.Once
	// executeToolCallSafe temporarily replaces process-wide stdout and toggles a
	// package-global fatal mode. PicoClaw executes a batch of tool calls in
	// parallel, so every legacy CLI tool must share one process-wide lock.
	legacyToolExecutionMu sync.Mutex
)

type agentRunStats struct {
	mu               sync.Mutex
	llmDuration      time.Duration
	toolDuration     time.Duration
	turns            int
	toolCalls        int
	promptTokens     int
	completionTokens int
}

func (s *agentRunStats) addLLM(duration time.Duration, usage *providers.UsageInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.llmDuration += duration
	s.turns++
	if usage != nil {
		s.promptTokens += usage.PromptTokens
		s.completionTokens += usage.CompletionTokens
	}
}

func (s *agentRunStats) addTool(duration time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.toolDuration += duration
	s.toolCalls++
}

func (s *agentRunStats) snapshot() (time.Duration, time.Duration, int, int, int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.llmDuration, s.toolDuration, s.turns, s.toolCalls, s.promptTokens, s.completionTokens
}

type observedPicoProvider struct {
	delegate providers.LLMProvider
	session  *llmSession
	cb       AgentCallbacks
	stats    *agentRunStats
	lastUser string

	// seen is the conversation handed to the model on the most recent turn.
	// RunToolLoop builds its history in a slice of its own and returns only the
	// final text, so this is the one place the tool calls and their results can
	// be recovered and written back into the caller's conversation.
	seen []providers.Message
}

func (p *observedPicoProvider) GetDefaultModel() string { return p.delegate.GetDefaultModel() }

func (p *observedPicoProvider) Chat(
	ctx context.Context,
	messages []providers.Message,
	toolDefs []providers.ToolDefinition,
	model string,
	options map[string]any,
) (*providers.LLMResponse, error) {
	p.seen = messages
	_, _, completedTurns, _, _, _ := p.stats.snapshot()
	turn := completedTurns + 1
	emitStatus(p.cb, "llm", p.session.reviewText(
		fmt.Sprintf("Step %d: waiting for model…", turn),
		fmt.Sprintf("Шаг %d: ожидание ответа модели…", turn),
	))
	debugf("picoclaw turn=%d request: messages=%d tools=%d", turn, len(messages), len(toolDefs))

	started := time.Now()
	var response *providers.LLMResponse
	var lastErr error
	for attempt := range 3 {
		if attempt > 0 {
			delay := time.Duration(attempt) * 5 * time.Second
			emitStatus(p.cb, "retry", p.session.reviewText(
				fmt.Sprintf("Retrying in %s…", delay),
				fmt.Sprintf("Повтор через %s…", delay),
			))
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}

		response, lastErr = p.chatWithWait(ctx, messages, toolDefs, model, options)
		if lastErr == nil {
			break
		}
		if !isRetryableLLMError(lastErr) {
			return nil, lastErr
		}
		stderrAgentf(p.cb, "LLM error (%d/3): %v\n", attempt+1, lastErr)
	}
	if lastErr != nil {
		return nil, fmt.Errorf("LLM API error after 3 attempts: %w", lastErr)
	}
	if response == nil {
		return nil, errors.New("LLM provider returned an empty response")
	}

	duration := time.Since(started)
	p.stats.addLLM(duration, response.Usage)
	debugf("picoclaw turn=%d response: finish_reason=%s tool_calls=%d content=%q duration=%s",
		turn, response.FinishReason, len(response.ToolCalls), summarizeDebugText(response.Content, 0), duration.Round(time.Millisecond))

	// Preserve the old completeness guard inside PicoClaw's own conversation.
	// A hidden tool turns the nudge into a tool result, so RunToolLoop retains all
	// preceding tool output while asking the model to continue.
	if len(response.ToolCalls) == 0 {
		if missing := missingLevelsForContentSummary(p.session, p.lastUser); len(missing) > 0 &&
			p.session.levelCompletionNudges < maxLevelCompletionNudges {
			p.session.levelCompletionNudges++
			emitStatus(p.cb, "plan", p.session.reviewText(
				"Loading remaining levels before answer…",
				"Дозагружаю уровни перед ответом…",
			))
			return &providers.LLMResponse{
				FinishReason: "tool_calls",
				Usage:        response.Usage,
				ToolCalls: []providers.ToolCall{{
					ID:   fmt.Sprintf("encx-level-review-%d", p.session.levelCompletionNudges),
					Name: levelReviewNudgeTool,
					Arguments: map[string]any{
						"message": buildLevelLoadNudge(p.session, missing),
					},
				}},
			}, nil
		}
	}
	return response, nil
}

func (p *observedPicoProvider) chatWithWait(
	ctx context.Context,
	messages []providers.Message,
	toolDefs []providers.ToolDefinition,
	model string,
	options map[string]any,
) (*providers.LLMResponse, error) {
	done := make(chan struct{})
	defer close(done)
	go func() {
		started := time.Now()
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				elapsed := time.Since(started).Round(time.Second)
				emitStatus(p.cb, "llm_wait", p.session.reviewText(
					fmt.Sprintf("Waiting for model… %s", elapsed),
					fmt.Sprintf("Ожидание ответа модели… %s", elapsed),
				))
			case <-done:
				return
			}
		}
	}()
	return p.delegate.Chat(ctx, messages, toolDefs, model, options)
}

type picoLegacyTool struct {
	definition llmFunction
	parameters map[string]any
	runtime    *picoLegacyToolRuntime
}

func (t *picoLegacyTool) Name() string               { return t.definition.Name }
func (t *picoLegacyTool) Description() string        { return t.definition.Description }
func (t *picoLegacyTool) Parameters() map[string]any { return t.parameters }

func (t *picoLegacyTool) Execute(ctx context.Context, args map[string]any) *toolshared.ToolResult {
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return toolshared.ErrorResult(fmt.Sprintf("encode %s arguments: %v", t.Name(), err)).WithError(err)
	}
	return t.runtime.execute(ctx, t.Name(), string(argsJSON))
}

type picoLegacyToolRuntime struct {
	input *AgentRunInput
	cb    AgentCallbacks
	stats *agentRunStats

	// delivered records the document-bearing calls already answered in this run,
	// keyed by tool name and arguments.
	//
	// An identical call returns identical bytes, and those bytes now stay in the
	// conversation for the rest of the run. A model that re-reads a long document
	// instead of paging through it would otherwise pay for the same prefix on
	// every turn: twenty such calls once pushed a 72-page PDF past a 261k context
	// limit. Guarded by legacyToolExecutionMu, which every execute call holds.
	delivered map[string]struct{}
}

// repeatedContentRead reports whether this exact content read was already
// answered, remembering it otherwise.
func (r *picoLegacyToolRuntime) repeatedContentRead(name, argsJSON string) bool {
	if _, isContentTool := contentToolFields[name]; !isContentTool {
		return false
	}
	key := name + "\x00" + argsJSON
	if _, seen := r.delivered[key]; seen {
		return true
	}
	if r.delivered == nil {
		r.delivered = map[string]struct{}{}
	}
	r.delivered[key] = struct{}{}
	return false
}

func (r *picoLegacyToolRuntime) execute(ctx context.Context, name, argsJSON string) *toolshared.ToolResult {
	legacyToolExecutionMu.Lock()
	defer legacyToolExecutionMu.Unlock()

	if securityRequiresApproval(r.input.Session, name) {
		if r.cb.ApproveToolCall == nil {
			message := r.input.Session.reviewText(
				"Tool approval required but no approval handler is configured",
				"Требуется согласование, но обработчик подтверждения не настроен",
			)
			return toolshared.ErrorResult(message)
		}
		emitStatus(r.cb, "approval", r.input.Session.reviewText(
			fmt.Sprintf("Waiting for approval: %s", name),
			fmt.Sprintf("Ожидание согласования: %s", name),
		))
		allowed, err := r.cb.ApproveToolCall(ctx, name, argsJSON)
		if err != nil {
			return toolshared.ErrorResult(err.Error()).WithError(err)
		}
		if !allowed {
			result := `{"skipped":true,"reason":"user denied tool execution"}`
			emitAgent(r.cb, AgentEvent{Type: agentEventToolDone, ToolName: name, ToolArgs: argsJSON, ToolResult: result})
			return toolshared.SilentResult(result)
		}
	}

	emitStatus(r.cb, "tool", r.input.Session.reviewText(
		fmt.Sprintf("Running tool: %s", name),
		fmt.Sprintf("Вызов инструмента: %s", name),
	))
	emitAgent(r.cb, AgentEvent{Type: agentEventToolStart, ToolName: name, ToolArgs: argsJSON})
	debugf("picoclaw tool call: name=%s args=%s", name, summarizeDebugArgs(argsJSON))

	if r.repeatedContentRead(name, argsJSON) {
		// Reported as an error, not a note: a note was ignored sixty times in a
		// row by a model that kept asking for the same document, burning a turn
		// each time. The loop and the model both treat an error as something to
		// act on rather than retry.
		result := `{"error":"Refused: these exact arguments were already read in this conversation and ` +
			`returned the same text. Re-read that earlier result instead. To see more of the document you ` +
			`MUST change the arguments — pass page=N for a PDF, or offset=N for a text file."}`
		emitAgent(r.cb, AgentEvent{Type: agentEventToolDone, ToolName: name, ToolArgs: argsJSON, ToolResult: result})
		debugf("picoclaw tool call: name=%s repeated with identical arguments, refused", name)
		refused := toolshared.SilentResult(result)
		refused.IsError = true
		return refused
	}

	started := time.Now()
	rawResult := executeToolCallSafe(ctx, r.input.Cfg, r.input.Client, r.input.Session, name, argsJSON)
	r.stats.addTool(time.Since(started))
	llmResult := prepareToolResultForLLM(name, rawResult)
	if name == "admin_level_content" && !toolResultLooksLikeError(llmResult) {
		markLevelContentLoaded(r.input.Session, name, argsJSON)
	}
	if name == "admin_levels" && !toolResultLooksLikeError(llmResult) {
		recordLevelEnumeration(r.input.Session, llmResult)
	}
	emitAgent(r.cb, AgentEvent{Type: agentEventToolDone, ToolName: name, ToolArgs: argsJSON, ToolResult: llmResult})
	debugf("picoclaw tool result: name=%s raw_bytes=%d llm_bytes=%d result=%q",
		name, len(rawResult), len(llmResult), summarizeDebugText(llmResult, 0))

	result := toolshared.SilentResult(llmResult)
	result.IsError = toolResultLooksLikeError(llmResult)
	return result
}

type picoLevelReviewNudgeTool struct{}

func (*picoLevelReviewNudgeTool) Name() string        { return levelReviewNudgeTool }
func (*picoLevelReviewNudgeTool) Description() string { return "Internal continuation guard." }
func (*picoLevelReviewNudgeTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"message": map[string]any{"type": "string"},
		},
		"required": []string{"message"},
	}
}
func (*picoLevelReviewNudgeTool) Execute(_ context.Context, args map[string]any) *toolshared.ToolResult {
	message, _ := args["message"].(string)
	return toolshared.SilentResult(message)
}

func newPicoProvider(agentCfg AgentConfig) (providers.LLMProvider, error) {
	if agentCfg.Provider != nil {
		return agentCfg.Provider, nil
	}
	switch agentCfg.AuthMethod {
	case authMethodCodex:
		return newCodexProvider()
	case authMethodGigaChat:
		gigachat, err := gigachatConfigFromEnv(agentCfg.Model)
		if err != nil {
			return nil, err
		}
		return newGigaChatProvider(gigachat)
	}
	provider := providers.NewHTTPProviderWithMaxTokensFieldAndRequestTimeout(
		agentCfg.APIKey,
		strings.TrimRight(agentCfg.BaseURL, "/"),
		"",
		"",
		"encli/"+version,
		600,
		nil,
		nil,
	)
	if strings.Contains(strings.ToLower(agentCfg.BaseURL), "openrouter.ai") {
		provider.SetProviderName("openrouter")
	}
	return provider, nil
}

// resolveAgentPricing skips the OpenRouter model catalog for subscription runs:
// a ChatGPT plan is not billed per token, and the catalog would not know the
// Codex model anyway.
//
// GigaChat is skipped too, for the opposite reason: it does bill per token, but
// in its own units against a prepaid balance rather than in the dollars the
// report would print, so no cost line is better than a wrong one.
func resolveAgentPricing(ctx context.Context, agentCfg AgentConfig) *llmPricing {
	switch agentCfg.AuthMethod {
	case authMethodCodex:
		return &llmPricing{isSubscription: true}
	case authMethodGigaChat:
		return nil
	}
	return fetchLLMPricing(ctx, agentCfg.BaseURL, agentCfg.APIKey, agentCfg.Model)
}

func newPicoRegistry(input *AgentRunInput, cb AgentCallbacks, stats *agentRunStats) (*tools.ToolRegistry, error) {
	registry := tools.NewToolRegistry()
	runtime := &picoLegacyToolRuntime{input: input, cb: cb, stats: stats, delivered: map[string]struct{}{}}
	for _, definition := range input.Tools {
		var parameters map[string]any
		if err := json.Unmarshal(definition.Function.Parameters, &parameters); err != nil {
			return nil, fmt.Errorf("decode schema for %s: %w", definition.Function.Name, err)
		}
		registry.Register(&picoLegacyTool{
			definition: definition.Function,
			parameters: parameters,
			runtime:    runtime,
		})
	}
	registry.RegisterHidden(&picoLevelReviewNudgeTool{})
	return registry, nil
}

func picoMessages(messages []llmMessage) []providers.Message {
	out := make([]providers.Message, 0, len(messages))
	for _, message := range messages {
		converted := providers.Message{
			Role:       message.Role,
			Content:    message.Content,
			ToolCallID: message.ToolCallID,
		}
		for _, call := range message.ToolCalls {
			var args map[string]any
			_ = json.Unmarshal([]byte(call.Function.Arguments), &args)
			converted.ToolCalls = append(converted.ToolCalls, providers.ToolCall{
				ID:        call.ID,
				Type:      call.Type,
				Name:      call.Function.Name,
				Arguments: args,
				Function: &providers.FunctionCall{
					Name:      call.Function.Name,
					Arguments: call.Function.Arguments,
				},
			})
		}
		out = append(out, converted)
	}
	return out
}

// llmMessagesFrom converts PicoClaw's conversation back into the persisted
// form, dropping the continuation guard.
//
// The guard is registered hidden, so it never appears in the tool catalog the
// model is shown. Keeping its synthetic call in the saved history would leave
// every later turn referring to a function the model has no definition for.
func llmMessagesFrom(messages []providers.Message) []llmMessage {
	dropped := map[string]struct{}{}
	out := make([]llmMessage, 0, len(messages))

	for _, message := range messages {
		if message.Role == "tool" {
			if _, isGuard := dropped[message.ToolCallID]; isGuard {
				continue
			}
			out = append(out, llmMessage{
				Role:       message.Role,
				Content:    message.Content,
				ToolCallID: message.ToolCallID,
			})
			continue
		}

		converted := llmMessage{Role: message.Role, Content: message.Content}
		for _, call := range message.ToolCalls {
			if call.Name == levelReviewNudgeTool {
				dropped[call.ID] = struct{}{}
				continue
			}
			converted.ToolCalls = append(converted.ToolCalls, llmToolCall{
				ID:       call.ID,
				Type:     cmp.Or(call.Type, "function"),
				Function: llmToolCallFunction{Name: call.Name, Arguments: toolCallArgumentsJSON(call)},
			})
		}
		if converted.Role == "assistant" && converted.Content == "" && len(converted.ToolCalls) == 0 {
			// An assistant turn that carried nothing but the guard.
			continue
		}
		out = append(out, converted)
	}
	return out
}

// interruptedRunNote records, in the conversation itself, that a run stopped
// before it finished.
//
// Without it the transcript ends on a tool result and reads like work still in
// progress, so the next user message is answered as a continuation: a run that
// was cancelled after creating four levels was silently resumed by the turn
// that followed, on top of the levels it had already made. A cancellation is
// the user saying stop, so it says so explicitly; any other failure may be
// worth retrying and is reported as a failure instead.
func interruptedRunNote(err error) string {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "[Выполнение прервано пользователем. Всё, что было сделано до этого момента, показано выше " +
			"и уже применено. Не продолжай прерванную задачу без новой явной просьбы.]"
	}
	return fmt.Sprintf("[Выполнение прервано ошибкой: %v. Всё, что было сделано до этого момента, показано "+
		"выше и уже применено. Прежде чем что-то менять, проверь текущее состояние игры.]", err)
}

func toolCallArgumentsJSON(call providers.ToolCall) string {
	if call.Function != nil && strings.TrimSpace(call.Function.Arguments) != "" {
		return call.Function.Arguments
	}
	if len(call.Arguments) == 0 {
		return "{}"
	}
	encoded, err := json.Marshal(call.Arguments)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func isRetryableLLMError(err error) bool {
	if err == nil {
		return false
	}
	var failover *providers.FailoverError
	if errors.As(err, &failover) {
		switch failover.Reason {
		case providers.FailoverRateLimit, providers.FailoverNetwork,
			providers.FailoverTimeout, providers.FailoverOverloaded:
			return true
		case providers.FailoverAuth, providers.FailoverBilling,
			providers.FailoverFormat, providers.FailoverContextOverflow:
			return false
		}
	}
	s := strings.ToLower(err.Error())
	for _, permanent := range []string{"unknown provider", "invalid model", "model not found", "unauthorized", "forbidden"} {
		if strings.Contains(s, permanent) {
			return false
		}
	}
	for _, transient := range []string{
		"context deadline exceeded", "http 429", "http 502", "http 503", "http 504", "connection reset", "eof",
		"rate limit", "rate_limit", "overloaded", "temporarily unavailable", "failover(network)", "failover(timeout)",
	} {
		if strings.Contains(s, transient) {
			return true
		}
	}
	return false
}

func runAgentLoop(ctx context.Context, agentCfg AgentConfig, input *AgentRunInput, cb AgentCallbacks) ([]llmMessage, error) {
	if input == nil {
		return nil, fmt.Errorf("runAgentLoop: nil AgentRunInput")
	}
	if input.Session == nil {
		input.Session = &llmSession{}
	}

	disablePicoClawLogging.Do(logger.DisableConsole)
	stats := &agentRunStats{}
	registry, err := newPicoRegistry(input, cb, stats)
	if err != nil {
		return input.Messages, err
	}
	delegate, err := newPicoProvider(agentCfg)
	if err != nil {
		emitAgent(cb, AgentEvent{Type: agentEventError, Err: err, Message: err.Error()})
		return input.Messages, err
	}
	pricing := resolveAgentPricing(ctx, agentCfg)
	totalStart := time.Now()
	resetLevelEnumeration(input.Session)

	provider := &observedPicoProvider{
		delegate: delegate,
		session:  input.Session,
		cb:       cb,
		stats:    stats,
		lastUser: lastUserMessageContent(input.Messages),
	}
	result, err := tools.RunToolLoop(ctx, tools.ToolLoopConfig{
		Provider:      provider,
		Model:         agentCfg.Model,
		Tools:         registry,
		MaxIterations: maxAgentTurns,
	}, picoMessages(input.Messages), "encli", "agent")

	// Carry the tool calls and their results back into the conversation.
	//
	// Only the final assistant text used to survive a run, so a document read by
	// a tool vanished the moment the turn ended: the next turn saw the model's
	// own paraphrase of it and nothing else. That is how a scenario read from an
	// attached PDF turned into an invented one on the following message.
	//
	// This runs before the error check on purpose. A cancelled or failed run has
	// already changed the game on the server, and dropping its history does not
	// undo any of that — it only hides it. A run stopped after creating four
	// levels left a conversation that still read as "the request was never
	// answered", so the next message was served as if nothing had happened and
	// the work was started over.
	if len(provider.seen) > len(input.Messages) {
		input.Messages = llmMessagesFrom(provider.seen)
	}

	if err != nil {
		input.Messages = append(input.Messages, llmMessage{
			Role:    "assistant",
			Content: interruptedRunNote(err),
		})
		emitAgent(cb, AgentEvent{Type: agentEventError, Err: err, Message: err.Error()})
		return input.Messages, err
	}

	content := strings.TrimSpace(result.Content)
	if content != "" {
		input.Messages = append(input.Messages, llmMessage{Role: "assistant", Content: content})
		emitAgent(cb, AgentEvent{Type: agentEventAssistantText, Text: content})
	} else if result.Iterations >= maxAgentTurns {
		emitAgent(cb, AgentEvent{Type: agentEventWarning, Message: "Warning: agent reached maximum iterations"})
	}

	if len(input.Session.pendingFixes) > 0 {
		fixes := append([]pendingAdminFix(nil), input.Session.pendingFixes...)
		emitAgent(cb, AgentEvent{Type: agentEventApprovalNeed, PendingFixes: fixes})
		if cb.RunPendingApprovals != nil {
			cb.RunPendingApprovals(ctx, input.Cfg, input.Client, input.Session)
		} else {
			runPendingFixApprovals(ctx, input.Cfg, input.Client, input.Session)
		}
	}

	llmDuration, toolDuration, turns, toolCalls, promptTokens, completionTokens := stats.snapshot()
	report := formatAgentExecutionReport(input.Session, agentCfg.Model, pricing,
		time.Since(totalStart), llmDuration, toolDuration,
		turns, toolCalls, promptTokens, completionTokens)
	emitAgent(cb, AgentEvent{Type: agentEventReport, Report: report})
	emitAgent(cb, AgentEvent{Type: agentEventDone})
	return input.Messages, nil
}
