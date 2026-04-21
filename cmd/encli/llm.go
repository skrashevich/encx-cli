package main

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

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

// llmTool defines an OpenAI-compatible function tool.
type llmTool struct {
	Type     string      `json:"type"`
	Function llmFunction `json:"function"`
}

type llmFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type llmRequest struct {
	Model    string       `json:"model"`
	Stream   bool         `json:"stream"`
	Messages []llmMessage `json:"messages"`
	Tools    []llmTool    `json:"tools,omitempty"`
}

type llmMessage struct {
	Role       string        `json:"role"`
	Content    string        `json:"content,omitempty"`
	ToolCalls  []llmToolCall `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
}

type llmUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type llmResponse struct {
	Choices []llmChoice `json:"choices"`
	Usage   *llmUsage   `json:"usage,omitempty"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type llmErrorEnvelope struct {
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type llmChoice struct {
	Message      llmAssistantMessage `json:"message"`
	FinishReason string              `json:"finish_reason"`
}

type llmAssistantMessage struct {
	Content   string        `json:"content"`
	ToolCalls []llmToolCall `json:"tool_calls"`
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
	reviewApprovalMode  bool
	applyingApprovedFix bool
	preferRussian       bool
	pendingFixes        []pendingAdminFix
}

func cmdLLM(ctx context.Context, cfg *config, client *encx.Client, prompt string) {
	baseURL := cmp.Or(os.Getenv("LLM_BASE_URL"), os.Getenv("OPENROUTER_BASE_URL"), defaultLLMBaseURL)
	apiURL := strings.TrimRight(baseURL, "/") + "/chat/completions"

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
	session := &llmSession{
		reviewApprovalMode: isReviewApprovalPrompt(prompt),
		preferRussian:      looksLikeRussian(prompt),
	}

	systemPrompt := `You are an autonomous agent for the Encounter (en.cx) game engine CLI tool.
The user gives you a natural language request. Execute it step by step using the available tools.
The current domain is: ` + cfg.domain + `
` + func() string {
		if cfg.gameId != 0 {
			return fmt.Sprintf("The current game ID is: %d\n", cfg.gameId)
		}
		return ""
	}() + `
Rules:
- Execute multi-step tasks by calling tools one at a time. You will receive the result of each tool call.
- Use tool results to inform your next action (e.g., get level IDs before renaming levels).
- When all steps are complete, respond with a text summary of what was done.
- If a tool call fails, try to recover or report the error.
- For admin_copy_game: source is the first game mentioned, target is the second.
- Prefer admin_* tools for game management (viewing levels, creating content). Player tools (levels, status, bonuses) are for games IN PROGRESS.
- For reading level text, answers, hints, and other scenario content from the organizer side, prefer admin_level_content instead of player tools.
- Starting/launching a game is NOT available via CLI — only through the web interface. Inform the user if they ask.
- When asked to CREATE a game/levels, make them INTERESTING and DIFFERENT: give unique names, add tasks with creative quest text, add sectors with answers, add hints. Don't just create empty shells.
- ALWAYS COMPLETE THE FULL TASK. If asked to create N levels, create ALL N levels with tasks, sectors (answers), and hints. Never stop partway through and offer to "continue if needed". You have up to 200 tool calls — use them. Do not summarize partial work as if it were complete.
- SELF-VERIFICATION: After creating or modifying levels, verify your own work by calling admin_level_content for each affected level. Check that: (1) all sector codes/answers are present and correct, (2) timings (autopass, answer block) are set to non-zero values if the level is timed, (3) hints are present if needed and have correct text/delays, (4) task text matches the intended answers. If you discover errors, fix them immediately before reporting success.
- Respond in the same language as the user's request.` + func() string {
		if !session.reviewApprovalMode {
			return ""
		}
		return `
- This request is in review/approval mode. You may inspect and analyze existing content, but you must NOT directly modify the game.
- If you find an issue that should be fixed, call propose_admin_fix once per issue. One proposal = one user approval decision.
- Each proposed fix must contain only the minimal admin mutation steps needed to resolve that one issue.
- After proposing fixes, give a concise audit summary. Do not ask the user for confirmation in normal text; the CLI will handle approval interactively.`
	}()

	messages := []llmMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: prompt},
	}

	tools := getTools(session.reviewApprovalMode)
	debugf("llm mode initialized: url=%s model=%s review_mode=%v tools=%d prompt=%q", apiURL, model, session.reviewApprovalMode, len(tools), summarizeDebugText(prompt, 0))

	// Fetch pricing info (OpenRouter → from API, localhost → free).
	pricing := fetchLLMPricing(ctx, baseURL, apiKey, model)

	// Timing and usage tracking.
	totalStart := time.Now()
	var llmDuration time.Duration
	var toolDuration time.Duration
	var totalToolCalls int
	var totalPromptTokens, totalCompletionTokens int
	turns := 0

	printReport := func() {
		totalElapsed := time.Since(totalStart)
		otherDuration := totalElapsed - llmDuration - toolDuration
		if otherDuration < 0 {
			otherDuration = 0
		}

		var report strings.Builder
		if session.preferRussian {
			fmt.Fprintf(&report, "\n--- Отчёт о выполнении ---\n")
			fmt.Fprintf(&report, "Общее время:      %s\n", totalElapsed.Round(time.Millisecond))
			fmt.Fprintf(&report, "Модель:           %s\n", model)
			fmt.Fprintf(&report, "  Время LLM:      %s\n", llmDuration.Round(time.Millisecond))
			fmt.Fprintf(&report, "  Время тулзов:   %s\n", toolDuration.Round(time.Millisecond))
			fmt.Fprintf(&report, "  Накладные:      %s\n", otherDuration.Round(time.Millisecond))
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
			fmt.Fprintf(&report, "  LLM time:    %s\n", llmDuration.Round(time.Millisecond))
			fmt.Fprintf(&report, "  Tool time:   %s\n", toolDuration.Round(time.Millisecond))
			fmt.Fprintf(&report, "  Overhead:    %s\n", otherDuration.Round(time.Millisecond))
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
		fmt.Fprint(os.Stderr, report.String())
	}

	for turn := 0; turn < maxAgentTurns; turn++ {
		debugf("llm turn=%d request: messages=%d", turn+1, len(messages))
		turnStart := time.Now()
		var resp *llmResponse
		var lastErr error
		for attempt := range 3 {
			if attempt > 0 {
				delay := time.Duration(attempt) * 5 * time.Second
				fmt.Fprintf(os.Stderr, "%s %s...\n",
					session.reviewText("Retrying in", "Повтор через"), delay)
				time.Sleep(delay)
			}
			resp, lastErr = callLLM(ctx, apiURL, apiKey, model, messages, tools)
			if lastErr == nil {
				break
			}
			if !isRetryableLLMError(lastErr) {
				fatal("LLM API error: %v", lastErr)
			}
			fmt.Fprintf(os.Stderr, "LLM error (%d/3): %v\n", attempt+1, lastErr)
		}
		if lastErr != nil {
			fatal("LLM API error after 3 attempts: %v", lastErr)
		}
		llmDuration += time.Since(turnStart)
		turns++

		if resp.Usage != nil {
			totalPromptTokens += resp.Usage.PromptTokens
			totalCompletionTokens += resp.Usage.CompletionTokens
		}

		if len(resp.Choices) == 0 {
			fatal("LLM returned no choices")
		}

		choice := resp.Choices[0]
		debugf("llm turn=%d response: finish_reason=%s tool_calls=%d content=%q",
			turn+1, choice.FinishReason, len(choice.Message.ToolCalls), summarizeDebugText(choice.Message.Content, 0))
		debugf("llm turn=%d completed in %s", turn+1, time.Since(turnStart).Round(time.Millisecond))

		// No tool calls — LLM is done, print final response
		if len(choice.Message.ToolCalls) == 0 {
			if choice.Message.Content != "" {
				fmt.Println(choice.Message.Content)
			}
			if len(session.pendingFixes) > 0 {
				runPendingFixApprovals(ctx, cfg, client, session)
			}
			printReport()
			return
		}

		// Append assistant message with tool calls to conversation
		messages = append(messages, llmMessage{
			Role:      "assistant",
			ToolCalls: choice.Message.ToolCalls,
		})

		// Execute each tool call and append results
		for _, tc := range choice.Message.ToolCalls {
			fmt.Fprintf(os.Stderr, "%s\n", formatToolCallForDisplay(session, tc.Function.Name, tc.Function.Arguments))
			debugf("llm tool call: id=%s name=%s args=%s", tc.ID, tc.Function.Name, summarizeDebugArgs(tc.Function.Arguments))

			toolStart := time.Now()
			result := executeToolCallSafe(ctx, cfg, client, session, tc.Function.Name, tc.Function.Arguments)
			toolDuration += time.Since(toolStart)
			totalToolCalls++

			llmResult := prepareToolResultForLLM(tc.Function.Name, result)
			debugf("llm tool result: id=%s name=%s raw_bytes=%d llm_bytes=%d result=%q",
				tc.ID, tc.Function.Name, len(result), len(llmResult), summarizeDebugText(llmResult, 0))

			messages = append(messages, llmMessage{
				Role:       "tool",
				Content:    llmResult,
				ToolCallID: tc.ID,
			})
		}
		debugf("llm turn=%d tools complete; waiting for next model step", turn+1)
	}

	fmt.Fprintln(os.Stderr, "Warning: agent reached maximum iterations")
	printReport()
}
