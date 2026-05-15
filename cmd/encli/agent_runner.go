package main

import (
	"context"
	"fmt"
	"strings"
	"time"

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

// AgentConfig holds provider settings for chat/completions.
type AgentConfig struct {
	APIURL  string
	APIKey  string
	Model   string
	BaseURL string
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
		reviewApprovalMode: isReviewApprovalPrompt(prompt),
		preferRussian:      looksLikeRussian(prompt),
	}
}

func buildSystemPrompt(cfg *config, session *llmSession) string {
	return `You are an autonomous agent for the Encounter (en.cx) game engine CLI tool.
The user gives you a natural language request. Execute it step by step using the available tools.
The current domain is: ` + cfg.domain + `
` + func() string {
		if cfg.gameId != 0 {
			return fmt.Sprintf("The current game ID is: %d\n", cfg.gameId)
		}
		return ""
	}() + `
Rules:
- NEVER FABRICATE (strict): Do not invent, guess, or infer facts about game content, tool results, files, URLs, or anything else. If information is missing, call the appropriate tools to obtain it. If tools still cannot provide it, say clearly that the information is unavailable — do not fill gaps with assumptions, stereotypes, or plausible-sounding details.
- Execute multi-step tasks by calling tools one at a time. You will receive the result of each tool call.
- Use tool results to inform your next action (e.g., get level IDs before renaming levels).
- When all steps are complete, respond with a text summary of what was done.
- If a tool call fails, try to recover or report the error.
- For admin_copy_game: source is the first game mentioned, target is the second.
- Prefer admin_* tools for game management (viewing levels, creating content). Player tools (levels, status, bonuses) are for games IN PROGRESS.
- For reading level text, answers, hints, and other scenario content from the organizer side, prefer admin_level_content instead of player tools.
- TASK DECOMPOSITION: Enumeration tools (admin_levels, game lists, directory listings) return IDs, names, and metadata only — not full content. If the user needs scenario text, per-level details, or an audit/summary across items, call the read tool (admin_level_content, read_local_file, etc.) for every relevant item before your final answer. A complete-looking table or summary built only from names is wrong.
- Starting/launching a game is NOT available via CLI — only through the web interface. Inform the user if they ask.
- When asked to CREATE a game/levels, make them INTERESTING and DIFFERENT: give unique names, add tasks with creative quest text, add sectors with answers, add hints. Don't just create empty shells.
- ALWAYS COMPLETE THE FULL TASK. If asked to create N levels, create ALL N levels with tasks, sectors (answers), and hints. Never stop partway through and offer to "continue if needed". You have up to 200 tool calls — use them. Do not summarize partial work as if it were complete.
- SELF-VERIFICATION: After creating or modifying levels, verify your own work by calling admin_level_content for each affected level. Check that: (1) all sector codes/answers are present and correct, (2) timings (autopass, answer block) are set to non-zero values if the level is timed, (3) hints are present if needed and have correct text/delays, (4) task text matches the intended answers. If you discover errors, fix them immediately before reporting success.
- LOCAL FILES: Use read_local_file, list_local_dir, and search_local_files to read scripts, notes, or scenario files on disk. Paths are relative to LLM_FILES_ROOT (defaults to the current working directory). You cannot read files outside that root.
- WIKIPEDIA: Use wikipedia_search to find articles and wikipedia_article to read summaries when you need to verify facts, dates, places, or historical details for quest content.
- Respond in the same language as the user's request.` + securityModeSystemPromptAddendum(session) + func() string {
		if session == nil || !session.reviewApprovalMode {
			return ""
		}
		return `
- This request is in review/approval mode. You may inspect and analyze existing content, but you must NOT directly modify the game.
- If you find an issue that should be fixed, call propose_admin_fix once per issue. One proposal = one user approval decision.
- Each proposed fix must contain only the minimal admin mutation steps needed to resolve that one issue.
- After proposing fixes, give a concise audit summary. Do not ask the user for confirmation in normal text; the CLI will handle approval interactively.`
	}()
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
			if pricing != nil && pricing.isLocal {
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
			if pricing != nil && pricing.isLocal {
				fmt.Fprintf(&report, "Cost:          $0 (local proxy)\n")
			} else if pricing != nil {
				fmt.Fprintf(&report, "Cost:          $%.4f (OpenRouter pricing)\n", computeLLMCost(pricing, totalPromptTokens, totalCompletionTokens))
			}
		}
	}
	return report.String()
}

func runAgentLoop(ctx context.Context, agentCfg AgentConfig, input *AgentRunInput, cb AgentCallbacks) ([]llmMessage, error) {
	if input == nil {
		return nil, fmt.Errorf("runAgentLoop: nil AgentRunInput")
	}

	pricing := fetchLLMPricing(ctx, agentCfg.BaseURL, agentCfg.APIKey, agentCfg.Model)
	totalStart := time.Now()
	var llmDuration time.Duration
	var toolDuration time.Duration
	var totalToolCalls int
	var totalPromptTokens, totalCompletionTokens int
	turns := 0

	emitReport := func() {
		totalElapsed := time.Since(totalStart)
		txt := formatAgentExecutionReport(input.Session, agentCfg.Model, pricing,
			totalElapsed, llmDuration, toolDuration,
			turns, totalToolCalls, totalPromptTokens, totalCompletionTokens)
		emitAgent(cb, AgentEvent{Type: agentEventReport, Report: txt})
	}

	messages := &input.Messages
	resetLevelEnumeration(input.Session)
	for turn := 0; turn < maxAgentTurns; turn++ {
		debugf("llm turn=%d request: messages=%d", turn+1, len(*messages))
		turnStart := time.Now()
		var resp *llmResponse
		var lastErr error
		emitStatus(cb, "llm", input.Session.reviewText(
			fmt.Sprintf("Step %d: waiting for model…", turn+1),
			fmt.Sprintf("Шаг %d: ожидание ответа модели…", turn+1),
		))
		for attempt := range 3 {
			if attempt > 0 {
				delay := time.Duration(attempt) * 5 * time.Second
				emitStatus(cb, "retry", input.Session.reviewText(
					fmt.Sprintf("Retrying in %s…", delay),
					fmt.Sprintf("Повтор через %s…", delay),
				))
				time.Sleep(delay)
			}
			resp, lastErr = callLLM(ctx, agentCfg.APIURL, agentCfg.APIKey, agentCfg.Model, *messages, input.Tools, func(elapsed time.Duration) {
				emitStatus(cb, "llm_wait", input.Session.reviewText(
					fmt.Sprintf("Waiting for model… %s", elapsed),
					fmt.Sprintf("Ожидание ответа модели… %s", elapsed),
				))
			})
			if lastErr == nil {
				break
			}
			if !isRetryableLLMError(lastErr) {
				emitAgent(cb, AgentEvent{Type: agentEventError, Err: lastErr, Message: lastErr.Error()})
				return *messages, fmt.Errorf("LLM API error: %w", lastErr)
			}
			stderrAgentf(cb, "LLM error (%d/3): %v\n", attempt+1, lastErr)
		}
		if lastErr != nil {
			emitAgent(cb, AgentEvent{Type: agentEventError, Err: lastErr, Message: lastErr.Error()})
			return *messages, fmt.Errorf("LLM API error after 3 attempts: %w", lastErr)
		}
		llmDuration += time.Since(turnStart)
		turns++

		if resp.Usage != nil {
			totalPromptTokens += resp.Usage.PromptTokens
			totalCompletionTokens += resp.Usage.CompletionTokens
		}

		if len(resp.Choices) == 0 {
			err := fmt.Errorf("LLM returned no choices")
			emitAgent(cb, AgentEvent{Type: agentEventError, Err: err, Message: err.Error()})
			return *messages, err
		}

		choice := resp.Choices[0]
		debugf("llm turn=%d response: finish_reason=%s tool_calls=%d content=%q",
			turn+1, choice.FinishReason, len(choice.Message.ToolCalls), summarizeDebugText(choice.Message.Content, 0))
		debugf("llm turn=%d completed in %s", turn+1, time.Since(turnStart).Round(time.Millisecond))

		if len(choice.Message.ToolCalls) == 0 {
			lastUser := lastUserMessageContent(*messages)
			if missing := missingLevelsForContentSummary(input.Session, lastUser); len(missing) > 0 &&
				input.Session.levelCompletionNudges < maxLevelCompletionNudges {
				input.Session.levelCompletionNudges++
				nudge := buildLevelLoadNudge(input.Session, missing)
				emitStatus(cb, "plan", input.Session.reviewText(
					"Loading remaining levels before answer…",
					"Дозагружаю уровни перед ответом…",
				))
				*messages = append(*messages, llmMessage{Role: "user", Content: nudge})
				continue
			}
			if choice.Message.Content != "" {
				emitAgent(cb, AgentEvent{Type: agentEventAssistantText, Text: choice.Message.Content})
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
			emitReport()
			emitAgent(cb, AgentEvent{Type: agentEventDone})
			return *messages, nil
		}

		*messages = append(*messages, llmMessage{
			Role:      "assistant",
			ToolCalls: choice.Message.ToolCalls,
		})

		for _, tc := range choice.Message.ToolCalls {
			if securityRequiresApproval(input.Session, tc.Function.Name) {
				if cb.ApproveToolCall == nil {
					errMsg := input.Session.reviewText(
						"Tool approval required but no approval handler is configured",
						"Требуется согласование, но обработчик подтверждения не настроен",
					)
					emitAgent(cb, AgentEvent{Type: agentEventError, Message: errMsg})
					return *messages, fmt.Errorf("%s", errMsg)
				}
				emitStatus(cb, "approval", input.Session.reviewText(
					fmt.Sprintf("Waiting for approval: %s", tc.Function.Name),
					fmt.Sprintf("Ожидание согласования: %s", tc.Function.Name),
				))
				allowed, apprErr := cb.ApproveToolCall(ctx, tc.Function.Name, tc.Function.Arguments)
				if apprErr != nil {
					emitAgent(cb, AgentEvent{Type: agentEventError, Err: apprErr, Message: apprErr.Error()})
					return *messages, apprErr
				}
				if !allowed {
					skipResult := `{"skipped": true, "reason": "user denied tool execution"}`
					emitAgent(cb, AgentEvent{
						Type:       agentEventToolDone,
						ToolName:   tc.Function.Name,
						ToolArgs:   tc.Function.Arguments,
						ToolResult: skipResult,
					})
					*messages = append(*messages, llmMessage{
						Role:       "tool",
						Content:    skipResult,
						ToolCallID: tc.ID,
					})
					continue
				}
			}

			emitStatus(cb, "tool", input.Session.reviewText(
				fmt.Sprintf("Running tool: %s", tc.Function.Name),
				fmt.Sprintf("Вызов инструмента: %s", tc.Function.Name),
			))
			emitAgent(cb, AgentEvent{Type: agentEventToolStart, ToolName: tc.Function.Name, ToolArgs: tc.Function.Arguments})
			debugf("llm tool call: id=%s name=%s args=%s", tc.ID, tc.Function.Name, summarizeDebugArgs(tc.Function.Arguments))

			toolStart := time.Now()
			result := executeToolCallSafe(ctx, input.Cfg, input.Client, input.Session, tc.Function.Name, tc.Function.Arguments)
			toolDuration += time.Since(toolStart)
			totalToolCalls++

			llmResult := prepareToolResultForLLM(tc.Function.Name, result)
			if tc.Function.Name == "admin_level_content" && !toolResultLooksLikeError(llmResult) {
				markLevelContentLoaded(input.Session, tc.Function.Name, tc.Function.Arguments)
			}
			if tc.Function.Name == "admin_levels" && !toolResultLooksLikeError(llmResult) {
				recordLevelEnumeration(input.Session, llmResult)
			}
			emitAgent(cb, AgentEvent{
				Type:       agentEventToolDone,
				ToolName:   tc.Function.Name,
				ToolArgs:   tc.Function.Arguments,
				ToolResult: llmResult,
			})
			debugf("llm tool result: id=%s name=%s raw_bytes=%d llm_bytes=%d result=%q",
				tc.ID, tc.Function.Name, len(result), len(llmResult), summarizeDebugText(llmResult, 0))

			*messages = append(*messages, llmMessage{
				Role:       "tool",
				Content:    llmResult,
				ToolCallID: tc.ID,
			})
		}
		debugf("llm turn=%d tools complete; waiting for next model step", turn+1)
	}

	warn := "Warning: agent reached maximum iterations"
	emitAgent(cb, AgentEvent{Type: agentEventWarning, Message: warn})
	emitReport()
	emitAgent(cb, AgentEvent{Type: agentEventDone})
	return *messages, nil
}
