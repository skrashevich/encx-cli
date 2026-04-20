package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/skrashevich/encx-cli/encx"
)

const (
	openRouterURL      = "https://openrouter.ai/api/v1/chat/completions"
	defaultLLMModel    = "openai/gpt-4o-mini"
	maxAgentTurns      = 20
	maxToolItemsForLLM = 200
	maxToolTextForLLM  = 240
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
	Messages []llmMessage `json:"messages"`
	Tools    []llmTool    `json:"tools,omitempty"`
}

type llmMessage struct {
	Role       string        `json:"role"`
	Content    string        `json:"content,omitempty"`
	ToolCalls  []llmToolCall `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
}

type llmResponse struct {
	Choices []llmChoice `json:"choices"`
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

func getTools() []llmTool {
	return []llmTool{
		{Type: "function", Function: llmFunction{
			Name:        "login",
			Description: "Authenticate and save session. Use when user wants to log in.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"login":{"type":"string","description":"Username"},"password":{"type":"string","description":"Password"}},"required":["login","password"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "logout",
			Description: "Clear saved session",
			Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "games",
			Description: "List available games on the domain (HTML scraping)",
			Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "game_list",
			Description: "List games with full details via JSON API",
			Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "status",
			Description: "Show current game state: level, sectors, bonuses, hints, messages",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"}},"required":["game_id"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "level",
			Description: "Show current level task/assignment text",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"}},"required":["game_id"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "levels",
			Description: "Show all levels with pass/dismiss status for a game IN PROGRESS (player view). Use admin_levels for games not yet started or for management.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"}},"required":["game_id"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "bonuses",
			Description: "Show bonuses for the current level",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"}},"required":["game_id"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "hints",
			Description: "Show hints (regular and penalty) for the current level",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"}},"required":["game_id"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "sectors",
			Description: "Show sectors for the current level",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"}},"required":["game_id"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "log",
			Description: "Show recent code submissions (action log)",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"}},"required":["game_id"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "messages",
			Description: "Show messages from game organizers",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"}},"required":["game_id"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "enter",
			Description: "Submit application to join a game as a player (NOT start/launch a game — that can only be done via web UI)",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"}},"required":["game_id"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "send_code",
			Description: "Send a level code answer",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"},"code":{"type":"string","description":"The code to submit"}},"required":["game_id","code"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "send_bonus",
			Description: "Send a bonus code answer",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"},"code":{"type":"string","description":"The bonus code to submit"}},"required":["game_id","code"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "hint",
			Description: "Request a penalty hint by its ID",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"},"hint_id":{"type":"integer","description":"Penalty hint ID"}},"required":["game_id","hint_id"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "game_stats",
			Description: "Show game statistics (levels, teams, rankings)",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"}},"required":["game_id"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "profile",
			Description: "Show user profile (login, name, rank, team, points)",
			Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "admin_games",
			Description: "List games where the user is an author or has admin access",
			Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "admin_levels",
			Description: "List all levels with their IDs (admin panel). Works for any game you author — use this to see/manage levels even if game hasn't started.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"}},"required":["game_id"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "admin_create_levels",
			Description: "Create new levels in a game",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"},"count":{"type":"integer","description":"Number of levels to create"}},"required":["game_id","count"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "admin_delete_level",
			Description: "Delete a level by its number",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"},"level_number":{"type":"integer","description":"Level number to delete"}},"required":["game_id","level_number"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "admin_rename_level",
			Description: "Rename a level",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"},"level_id":{"type":"integer","description":"Level ID (from admin-levels)"},"name":{"type":"string","description":"New level name"}},"required":["game_id","level_id","name"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "admin_set_autopass",
			Description: "Set level autopass timer (auto-transition to next level after timeout)",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"},"level_number":{"type":"integer","description":"Level number"},"time":{"type":"string","description":"Autopass time in HH:MM:SS format"},"penalty_time":{"type":"string","description":"Optional penalty time in HH:MM:SS format"}},"required":["game_id","level_number","time"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "admin_set_block",
			Description: "Set level answer block settings (limit answer attempts)",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"},"level_number":{"type":"integer","description":"Level number"},"attempts":{"type":"integer","description":"Max attempts"},"period":{"type":"string","description":"Block period in HH:MM:SS format"},"per_player":{"type":"boolean","description":"Apply per player (not per team)"}},"required":["game_id","level_number","attempts","period"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "admin_create_bonus",
			Description: "Create a bonus on a level",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"},"level_number":{"type":"integer","description":"Level number"},"level_id":{"type":"integer","description":"Level ID"},"name":{"type":"string","description":"Bonus name"},"answers":{"type":"array","items":{"type":"string"},"description":"Accepted answers"}},"required":["game_id","level_number","level_id","name","answers"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "admin_delete_bonus",
			Description: "Delete a bonus by its ID",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"},"level_number":{"type":"integer","description":"Level number"},"bonus_id":{"type":"integer","description":"Bonus ID"}},"required":["game_id","level_number","bonus_id"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "admin_create_sector",
			Description: "Create a sector on a level",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"},"level_number":{"type":"integer","description":"Level number"},"name":{"type":"string","description":"Sector name"},"answers":{"type":"array","items":{"type":"string"},"description":"Accepted answers"}},"required":["game_id","level_number","name","answers"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "admin_delete_sector",
			Description: "Delete a sector by its ID",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"},"level_number":{"type":"integer","description":"Level number"},"sector_id":{"type":"integer","description":"Sector ID"}},"required":["game_id","level_number","sector_id"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "admin_create_hint",
			Description: "Create a hint on a level with a delay before it opens",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"},"level_number":{"type":"integer","description":"Level number"},"delay":{"type":"string","description":"Delay before hint opens in HH:MM:SS format"},"text":{"type":"string","description":"Hint text"}},"required":["game_id","level_number","delay","text"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "admin_delete_hint",
			Description: "Delete a hint by its ID",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"},"level_number":{"type":"integer","description":"Level number"},"hint_id":{"type":"integer","description":"Hint ID"}},"required":["game_id","level_number","hint_id"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "admin_create_task",
			Description: "Create a task (assignment text) on a level",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"},"level_number":{"type":"integer","description":"Level number"},"text":{"type":"string","description":"Task text"}},"required":["game_id","level_number","text"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "admin_set_comment",
			Description: "Set level name and optional comment",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"},"level_number":{"type":"integer","description":"Level number"},"name":{"type":"string","description":"Level name"},"comment":{"type":"string","description":"Optional comment (visible to organizers)"}},"required":["game_id","level_number","name"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "admin_teams",
			Description: "List teams registered in the game",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"}},"required":["game_id"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "admin_corrections",
			Description: "List bonus/penalty time corrections",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"}},"required":["game_id"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "admin_add_correction",
			Description: "Add a time correction (bonus or penalty) for a team",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"},"team":{"type":"string","description":"Team name"},"type":{"type":"string","enum":["bonus","penalty"],"description":"Correction type"},"time":{"type":"string","description":"Time in HH:MM:SS format"},"level":{"type":"string","description":"Level name or 0 for all levels"},"comment":{"type":"string","description":"Optional comment"}},"required":["game_id","team","type","time"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "admin_delete_correction",
			Description: "Delete a time correction by its ID",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"},"correction_id":{"type":"string","description":"Correction ID"}},"required":["game_id","correction_id"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "admin_wipe_game",
			Description: "Completely reset a game: delete all bonuses, sectors, hints, levels, and corrections",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"}},"required":["game_id"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "admin_copy_game",
			Description: "Copy entire game (levels, settings, bonuses, sectors, hints) from source to target game",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"source_game_id":{"type":"integer","description":"Source game ID to copy from"},"target_game_id":{"type":"integer","description":"Target game ID to copy to"}},"required":["source_game_id","target_game_id"]}`),
		}},
	}
}

func cmdLLM(ctx context.Context, cfg *config, client *encx.Client, prompt string) {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		fatal("OPENROUTER_API_KEY environment variable is required for --llm mode")
	}

	model := os.Getenv("OPENROUTER_MODEL")
	if model == "" {
		model = defaultLLMModel
	}

	// Force JSON output in LLM mode for structured results.
	// Keep fatal exit behavior at the top level; only nested tool calls should
	// panic and be recovered back into the agent loop.
	cfg.jsonOutput = true
	jsonMode = true

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
- Starting/launching a game is NOT available via CLI — only through the web interface. Inform the user if they ask.
- When asked to CREATE a game/levels, make them INTERESTING and DIFFERENT: give unique names, add tasks with creative quest text, add sectors with answers, add hints. Don't just create empty shells.
- Respond in the same language as the user's request.`

	messages := []llmMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: prompt},
	}

	tools := getTools()
	debugf("llm mode initialized: model=%s tools=%d prompt=%q", model, len(tools), summarizeDebugText(prompt, 160))

	for turn := 0; turn < maxAgentTurns; turn++ {
		debugf("llm turn=%d request: messages=%d", turn+1, len(messages))
		turnStart := time.Now()
		resp, err := callLLM(ctx, apiKey, model, messages, tools)
		if err != nil {
			fatal("LLM API error: %v", err)
		}

		if len(resp.Choices) == 0 {
			fatal("LLM returned no choices")
		}

		choice := resp.Choices[0]
		debugf("llm turn=%d response: finish_reason=%s tool_calls=%d content=%q",
			turn+1, choice.FinishReason, len(choice.Message.ToolCalls), summarizeDebugText(choice.Message.Content, 160))
		debugf("llm turn=%d completed in %s", turn+1, time.Since(turnStart).Round(time.Millisecond))

		// No tool calls — LLM is done, print final response
		if len(choice.Message.ToolCalls) == 0 {
			if choice.Message.Content != "" {
				fmt.Println(choice.Message.Content)
			}
			return
		}

		// Append assistant message with tool calls to conversation
		messages = append(messages, llmMessage{
			Role:      "assistant",
			ToolCalls: choice.Message.ToolCalls,
		})

		// Execute each tool call and append results
		for _, tc := range choice.Message.ToolCalls {
			fmt.Fprintf(os.Stderr, "[%s] %s\n", tc.Function.Name, tc.Function.Arguments)
			debugf("llm tool call: id=%s name=%s args=%s", tc.ID, tc.Function.Name, summarizeDebugArgs(tc.Function.Arguments))

			result := executeToolCallSafe(ctx, cfg, client, tc.Function.Name, tc.Function.Arguments)
			llmResult := prepareToolResultForLLM(tc.Function.Name, result)
			debugf("llm tool result: id=%s name=%s raw_bytes=%d llm_bytes=%d result=%q",
				tc.ID, tc.Function.Name, len(result), len(llmResult), summarizeDebugText(llmResult, 200))

			messages = append(messages, llmMessage{
				Role:       "tool",
				Content:    llmResult,
				ToolCallID: tc.ID,
			})
		}
		debugf("llm turn=%d tools complete; waiting for next model step", turn+1)
	}

	fmt.Fprintln(os.Stderr, "Warning: agent reached maximum iterations")
}

func callLLM(ctx context.Context, apiKey, model string, messages []llmMessage, tools []llmTool) (*llmResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	req := llmRequest{
		Model:    model,
		Messages: messages,
		Tools:    tools,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	debugf("llm http request: model=%s messages=%d tools=%d bytes=%d", model, len(messages), len(tools), len(body))

	httpReq, err := http.NewRequestWithContext(ctx, "POST", openRouterURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	debugf("llm http response: status=%d bytes=%d", resp.StatusCode, len(respBody))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, parseLLMErrorMessage(respBody))
	}

	var llmResp llmResponse
	if err := json.Unmarshal(respBody, &llmResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if llmResp.Error != nil {
		return nil, fmt.Errorf("%s", llmResp.Error.Message)
	}

	return &llmResp, nil
}

func parseLLMErrorMessage(respBody []byte) string {
	var apiErr llmErrorEnvelope
	if err := json.Unmarshal(respBody, &apiErr); err == nil && apiErr.Error != nil && apiErr.Error.Message != "" {
		return apiErr.Error.Message
	}
	if msg := strings.TrimSpace(string(respBody)); msg != "" {
		return msg
	}
	return "empty error response"
}

// executeToolCallSafe runs a tool call, capturing stdout and recovering from fatal panics.
func executeToolCallSafe(ctx context.Context, cfg *config, client *encx.Client, name, argsJSON string) string {
	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	oldAgentMode := agentMode
	agentMode = true
	start := time.Now()
	debugf("tool execution start: name=%s args=%s", name, summarizeDebugArgs(argsJSON))
	var captured bytes.Buffer
	copyDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(&captured, r)
		close(copyDone)
	}()

	var result string
	func() {
		defer func() {
			agentMode = oldAgentMode
			if rec := recover(); rec != nil {
				if fe, ok := rec.(agentFatalError); ok {
					result = fmt.Sprintf(`{"error": %q}`, fe.Message)
				} else {
					result = fmt.Sprintf(`{"error": "panic: %v"}`, rec)
				}
			}
		}()
		executeLLMToolCall(ctx, cfg, client, name, argsJSON)
	}()

	w.Close()
	os.Stdout = oldStdout
	<-copyDone
	r.Close()

	if result == "" {
		result = captured.String()
	}
	if result == "" {
		result = `{"success": true}`
	}
	debugf("tool execution finish: name=%s duration=%s result=%q",
		name, time.Since(start).Round(time.Millisecond), summarizeDebugText(result, 200))
	return result
}

func executeLLMToolCall(ctx context.Context, cfg *config, client *encx.Client, name, argsJSON string) {
	var args map[string]any
	if argsJSON != "" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			fatal("Failed to parse tool arguments: %v", err)
		}
	}

	getInt := func(key string) int {
		v, ok := args[key]
		if !ok {
			return 0
		}
		switch val := v.(type) {
		case float64:
			return int(val)
		case string:
			n, _ := strconv.Atoi(val)
			return n
		}
		return 0
	}

	getString := func(key string) string {
		v, ok := args[key]
		if !ok {
			return ""
		}
		s, _ := v.(string)
		return s
	}

	getStringSlice := func(key string) []string {
		v, ok := args[key]
		if !ok {
			return nil
		}
		arr, ok := v.([]any)
		if !ok {
			return nil
		}
		result := make([]string, 0, len(arr))
		for _, item := range arr {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}

	if gid := getInt("game_id"); gid != 0 {
		cfg.gameId = gid
		debugf("tool execution context: set cfg.gameId=%d from tool args", gid)
	}

	switch name {
	case "login":
		cfg.login = getString("login")
		cfg.password = getString("password")
		if cfg.login == "" || cfg.password == "" {
			fatal("LLM tool 'login' requires both login and password parameters")
		}
		cmdLogin(ctx, cfg, client)

	case "logout":
		cmdLogout(cfg)

	case "games":
		loadSession(cfg, client)
		cmdGames(ctx, cfg, client)

	case "game_list":
		loadSession(cfg, client)
		cmdGameList(ctx, cfg, client)

	case "status":
		requireAuth(ctx, cfg, client)
		cmdStatus(ctx, cfg, client)

	case "level":
		requireAuth(ctx, cfg, client)
		cmdLevel(ctx, cfg, client)

	case "levels":
		requireAuth(ctx, cfg, client)
		cmdLevels(ctx, cfg, client)

	case "bonuses":
		requireAuth(ctx, cfg, client)
		cmdBonuses(ctx, cfg, client)

	case "hints":
		requireAuth(ctx, cfg, client)
		cmdHints(ctx, cfg, client)

	case "sectors":
		requireAuth(ctx, cfg, client)
		cmdSectors(ctx, cfg, client)

	case "log":
		requireAuth(ctx, cfg, client)
		cmdLog(ctx, cfg, client)

	case "messages":
		requireAuth(ctx, cfg, client)
		cmdMessages(ctx, cfg, client)

	case "enter":
		requireAuth(ctx, cfg, client)
		cmdEnter(ctx, cfg, client)

	case "send_code":
		requireAuth(ctx, cfg, client)
		cmdSendCode(ctx, cfg, client, []string{getString("code")})

	case "send_bonus":
		requireAuth(ctx, cfg, client)
		cmdSendBonus(ctx, cfg, client, []string{getString("code")})

	case "hint":
		requireAuth(ctx, cfg, client)
		cmdHint(ctx, cfg, client, []string{strconv.Itoa(getInt("hint_id"))})

	case "game_stats":
		requireAuth(ctx, cfg, client)
		cmdGameStats(ctx, cfg, client)

	case "profile":
		requireAuth(ctx, cfg, client)
		cmdProfile(ctx, cfg, client)

	case "admin_games":
		requireAuth(ctx, cfg, client)
		cmdAdminGames(ctx, cfg, client)

	case "admin_levels":
		requireAuth(ctx, cfg, client)
		cmdAdminLevels(ctx, cfg, client)

	case "admin_create_levels":
		requireAuth(ctx, cfg, client)
		cmdAdminCreateLevels(ctx, cfg, client, []string{strconv.Itoa(getInt("count"))})

	case "admin_delete_level":
		requireAuth(ctx, cfg, client)
		cmdAdminDeleteLevel(ctx, cfg, client, []string{strconv.Itoa(getInt("level_number"))})

	case "admin_rename_level":
		requireAuth(ctx, cfg, client)
		cmdAdminRenameLevel(ctx, cfg, client, []string{strconv.Itoa(getInt("level_id")), getString("name")})

	case "admin_set_autopass":
		requireAuth(ctx, cfg, client)
		positional := []string{strconv.Itoa(getInt("level_number")), getString("time")}
		if pt := getString("penalty_time"); pt != "" {
			positional = append(positional, pt)
		}
		cmdAdminUpdateAutopass(ctx, cfg, client, positional)

	case "admin_set_block":
		requireAuth(ctx, cfg, client)
		positional := []string{
			strconv.Itoa(getInt("level_number")),
			strconv.Itoa(getInt("attempts")),
			getString("period"),
		}
		if v, ok := args["per_player"]; ok {
			if b, ok := v.(bool); ok && b {
				positional = append(positional, "player")
			}
		}
		cmdAdminUpdateAnswerBlock(ctx, cfg, client, positional)

	case "admin_create_bonus":
		requireAuth(ctx, cfg, client)
		positional := []string{
			strconv.Itoa(getInt("level_number")),
			strconv.Itoa(getInt("level_id")),
			getString("name"),
		}
		positional = append(positional, getStringSlice("answers")...)
		cmdAdminCreateBonus(ctx, cfg, client, positional)

	case "admin_delete_bonus":
		requireAuth(ctx, cfg, client)
		cmdAdminDeleteBonus(ctx, cfg, client, []string{
			strconv.Itoa(getInt("level_number")),
			strconv.Itoa(getInt("bonus_id")),
		})

	case "admin_create_sector":
		requireAuth(ctx, cfg, client)
		positional := []string{
			strconv.Itoa(getInt("level_number")),
			getString("name"),
		}
		positional = append(positional, getStringSlice("answers")...)
		cmdAdminCreateSector(ctx, cfg, client, positional)

	case "admin_delete_sector":
		requireAuth(ctx, cfg, client)
		cmdAdminDeleteSector(ctx, cfg, client, []string{
			strconv.Itoa(getInt("level_number")),
			strconv.Itoa(getInt("sector_id")),
		})

	case "admin_create_hint":
		requireAuth(ctx, cfg, client)
		cmdAdminCreateHint(ctx, cfg, client, []string{
			strconv.Itoa(getInt("level_number")),
			getString("delay"),
			getString("text"),
		})

	case "admin_delete_hint":
		requireAuth(ctx, cfg, client)
		cmdAdminDeleteHint(ctx, cfg, client, []string{
			strconv.Itoa(getInt("level_number")),
			strconv.Itoa(getInt("hint_id")),
		})

	case "admin_create_task":
		requireAuth(ctx, cfg, client)
		cmdAdminCreateTask(ctx, cfg, client, []string{
			strconv.Itoa(getInt("level_number")),
			getString("text"),
		})

	case "admin_set_comment":
		requireAuth(ctx, cfg, client)
		positional := []string{strconv.Itoa(getInt("level_number")), getString("name")}
		if c := getString("comment"); c != "" {
			positional = append(positional, c)
		}
		cmdAdminSetComment(ctx, cfg, client, positional)

	case "admin_teams":
		requireAuth(ctx, cfg, client)
		cmdAdminTeams(ctx, cfg, client)

	case "admin_corrections":
		requireAuth(ctx, cfg, client)
		cmdAdminCorrections(ctx, cfg, client)

	case "admin_add_correction":
		requireAuth(ctx, cfg, client)
		level := getString("level")
		if level == "" {
			level = "0"
		}
		positional := []string{
			getString("team"),
			getString("type"),
			getString("time"),
			level,
		}
		if c := getString("comment"); c != "" {
			positional = append(positional, c)
		}
		cmdAdminAddCorrection(ctx, cfg, client, positional)

	case "admin_delete_correction":
		requireAuth(ctx, cfg, client)
		cmdAdminDeleteCorrection(ctx, cfg, client, []string{getString("correction_id")})

	case "admin_wipe_game":
		requireAuth(ctx, cfg, client)
		cmdAdminWipeGame(ctx, cfg, client)

	case "admin_copy_game":
		sourceID := getInt("source_game_id")
		targetID := getInt("target_game_id")
		if sourceID != 0 {
			cfg.gameId = sourceID
		}
		requireAuth(ctx, cfg, client)
		cmdAdminCopyGame(ctx, cfg, client, []string{strconv.Itoa(targetID)})

	default:
		fatal("Unknown tool call: %s", name)
	}
}

func summarizeDebugArgs(argsJSON string) string {
	if argsJSON == "" {
		return "{}"
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return summarizeDebugText(argsJSON, 160)
	}
	if _, ok := args["password"]; ok {
		args["password"] = "<redacted>"
	}
	data, err := json.Marshal(args)
	if err != nil {
		return summarizeDebugText(argsJSON, 160)
	}
	return summarizeDebugText(string(data), 160)
}

func prepareToolResultForLLM(name, result string) string {
	if result == "" {
		return result
	}
	if summarized, ok := summarizeToolResult(name, result); ok {
		return summarized
	}
	if len(result) <= 8000 {
		return result
	}
	if summarized, ok := summarizeGenericJSON(result); ok {
		return summarized
	}
	return summarizeDebugText(result, 8000)
}

func summarizeToolResult(name, result string) (string, bool) {
	switch name {
	case "games":
		var games []encx.DomainGame
		if !decodeJSON(result, &games) {
			return "", false
		}
		return marshalToolSummary(map[string]any{
			"count": gamesCount(games),
			"games": limitDomainGames(games, maxToolItemsForLLM),
		}), true
	case "game_list":
		var list encx.GameListResponse
		if !decodeJSON(result, &list) {
			return "", false
		}
		return marshalToolSummary(map[string]any{
			"coming_count": len(list.ComingGames),
			"active_count": len(list.ActiveGames),
			"coming_games": summarizeGameInfos(list.ComingGames, maxToolItemsForLLM),
			"active_games": summarizeGameInfos(list.ActiveGames, maxToolItemsForLLM),
		}), true
	case "admin_games":
		var games []encx.AdminGame
		if !decodeJSON(result, &games) {
			return "", false
		}
		return marshalToolSummary(map[string]any{
			"count": len(games),
			"games": limitAdminGames(games, maxToolItemsForLLM),
		}), true
	case "admin_levels":
		var levels []encx.AdminLevel
		if !decodeJSON(result, &levels) {
			return "", false
		}
		return marshalToolSummary(map[string]any{
			"count":  len(levels),
			"levels": limitAdminLevels(levels, maxToolItemsForLLM),
		}), true
	case "levels":
		var levels []encx.LevelSummary
		if !decodeJSON(result, &levels) {
			return "", false
		}
		return marshalToolSummary(map[string]any{
			"count":  len(levels),
			"levels": limitLevelSummaries(levels, maxToolItemsForLLM),
		}), true
	case "status":
		var model encx.GameModel
		if !decodeJSON(result, &model) {
			return "", false
		}
		return marshalToolSummary(summarizeGameModel(model)), true
	case "level":
		var payload struct {
			Level int              `json:"level"`
			Name  string           `json:"name"`
			Tasks []encx.LevelTask `json:"tasks"`
			Task  *encx.LevelTask  `json:"task"`
		}
		if !decodeJSON(result, &payload) {
			return "", false
		}
		taskText := ""
		if len(payload.Tasks) > 0 {
			taskText = summarizeDebugText(stripHTML(payload.Tasks[0].TaskText), maxToolTextForLLM)
		} else if payload.Task != nil {
			taskText = summarizeDebugText(stripHTML(payload.Task.TaskText), maxToolTextForLLM)
		}
		return marshalToolSummary(map[string]any{
			"level": payload.Level,
			"name":  payload.Name,
			"task":  taskText,
		}), true
	case "bonuses":
		var bonuses []encx.Bonus
		if !decodeJSON(result, &bonuses) {
			return "", false
		}
		return marshalToolSummary(map[string]any{
			"count":   len(bonuses),
			"bonuses": limitBonuses(bonuses, maxToolItemsForLLM),
		}), true
	case "hints":
		var payload struct {
			Helps        []encx.Help `json:"helps"`
			PenaltyHelps []encx.Help `json:"penalty_helps"`
		}
		if !decodeJSON(result, &payload) {
			return "", false
		}
		return marshalToolSummary(map[string]any{
			"helps_count":         len(payload.Helps),
			"penalty_helps_count": len(payload.PenaltyHelps),
			"helps":               limitHelps(payload.Helps, maxToolItemsForLLM),
			"penalty_helps":       limitHelps(payload.PenaltyHelps, maxToolItemsForLLM),
		}), true
	case "sectors":
		var sectors []encx.Sector
		if !decodeJSON(result, &sectors) {
			return "", false
		}
		return marshalToolSummary(map[string]any{
			"count":   len(sectors),
			"sectors": limitSectors(sectors, maxToolItemsForLLM),
		}), true
	case "messages":
		var messages []encx.AdminMessage
		if !decodeJSON(result, &messages) {
			return "", false
		}
		return marshalToolSummary(map[string]any{
			"count":    len(messages),
			"messages": limitMessages(messages, maxToolItemsForLLM),
		}), true
	case "log":
		var actions []encx.CodeAction
		if !decodeJSON(result, &actions) {
			return "", false
		}
		return marshalToolSummary(map[string]any{
			"count":   len(actions),
			"actions": limitCodeActions(actions, 50),
		}), true
	case "profile":
		var profile encx.Profile
		if !decodeJSON(result, &profile) {
			return "", false
		}
		return marshalToolSummary(profile), true
	case "admin_teams":
		var teams []encx.AdminTeam
		if !decodeJSON(result, &teams) {
			return "", false
		}
		return marshalToolSummary(map[string]any{
			"count": len(teams),
			"teams": limitAdminTeams(teams, maxToolItemsForLLM),
		}), true
	case "admin_corrections":
		var corrections []encx.AdminCorrection
		if !decodeJSON(result, &corrections) {
			return "", false
		}
		return marshalToolSummary(map[string]any{
			"count":       len(corrections),
			"corrections": limitCorrections(corrections, maxToolItemsForLLM),
		}), true
	case "game_stats":
		var stats encx.GameStatisticsResponse
		if !decodeJSON(result, &stats) {
			return "", false
		}
		return marshalToolSummary(summarizeGameStatistics(stats)), true
	default:
		return "", false
	}
}

func decodeJSON(input string, dst any) bool {
	return json.Unmarshal([]byte(input), dst) == nil
}

func marshalToolSummary(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return `{"error":"failed to marshal summarized tool result"}`
	}
	return string(data)
}

func summarizeGameInfos(items []encx.GameInfo, limit int) []map[string]any {
	items = clampSlice(items, limit)
	out := make([]map[string]any, 0, len(items))
	for _, g := range items {
		item := map[string]any{
			"game_id":     g.GameID,
			"game_num":    g.GameNum,
			"title":       g.Title,
			"type":        gameTypeName(g.GameTypeID),
			"zone":        zoneName(g.ZoneId),
			"started":     g.Started,
			"in_progress": g.InProgress,
			"finished":    g.Finished,
		}
		if g.StartDateTime != nil && g.StartDateTime.Timestamp > 0 {
			item["start_ts"] = g.StartDateTime.Timestamp
		}
		out = append(out, item)
	}
	return out
}

func gamesCount(games []encx.DomainGame) int {
	return len(games)
}

func limitDomainGames(items []encx.DomainGame, limit int) []map[string]any {
	items = clampSlice(items, limit)
	out := make([]map[string]any, 0, len(items))
	for _, g := range items {
		out = append(out, map[string]any{
			"game_id": g.GameId,
			"title":   g.Title,
		})
	}
	return out
}

func limitAdminGames(items []encx.AdminGame, limit int) []map[string]any {
	items = clampSlice(items, limit)
	out := make([]map[string]any, 0, len(items))
	for _, g := range items {
		out = append(out, map[string]any{
			"id":     g.ID,
			"title":  g.Title,
			"status": g.Status,
			"number": g.Number,
		})
	}
	return out
}

func limitAdminLevels(items []encx.AdminLevel, limit int) []map[string]any {
	items = clampSlice(items, limit)
	out := make([]map[string]any, 0, len(items))
	for _, l := range items {
		out = append(out, map[string]any{
			"number": l.Number,
			"id":     l.ID,
			"name":   l.Name,
		})
	}
	return out
}

func limitLevelSummaries(items []encx.LevelSummary, limit int) []map[string]any {
	items = clampSlice(items, limit)
	out := make([]map[string]any, 0, len(items))
	for _, l := range items {
		out = append(out, map[string]any{
			"level_id":     l.LevelId,
			"level_number": l.LevelNumber,
			"level_name":   l.LevelName,
			"is_passed":    l.IsPassed,
			"dismissed":    l.Dismissed,
		})
	}
	return out
}

func summarizeGameModel(model encx.GameModel) map[string]any {
	out := map[string]any{
		"game_id":    model.GameId,
		"game_title": model.GameTitle,
		"event":      parseEventCode(model.Event),
		"user_id":    model.UserId,
		"login":      model.Login,
		"team_id":    model.TeamId,
		"team_name":  model.TeamName,
		"levels":     len(model.Levels),
	}
	if model.Level != nil {
		level := model.Level
		out["current_level"] = map[string]any{
			"level_id":              level.LevelId,
			"number":                level.Number,
			"name":                  level.Name,
			"is_passed":             level.IsPassed,
			"dismissed":             level.Dismissed,
			"required_sectors":      level.RequiredSectorsCount,
			"passed_sectors":        level.PassedSectorsCount,
			"sectors_left_to_close": level.SectorsLeftToClose,
			"passed_bonuses":        level.PassedBonusesCount,
			"task":                  summarizeLevelTask(level.Tasks, level.Task),
			"helps_count":           len(level.Helps),
			"penalty_helps_count":   len(level.PenaltyHelps),
			"bonuses_count":         len(level.Bonuses),
			"sectors_count":         len(level.Sectors),
			"messages_count":        len(level.Messages),
		}
	}
	return out
}

func summarizeLevelTask(tasks []encx.LevelTask, task *encx.LevelTask) string {
	switch {
	case len(tasks) > 0:
		return summarizeDebugText(stripHTML(tasks[0].TaskText), maxToolTextForLLM)
	case task != nil:
		return summarizeDebugText(stripHTML(task.TaskText), maxToolTextForLLM)
	default:
		return ""
	}
}

func limitBonuses(items []encx.Bonus, limit int) []map[string]any {
	items = clampSlice(items, limit)
	out := make([]map[string]any, 0, len(items))
	for _, b := range items {
		out = append(out, map[string]any{
			"bonus_id":         b.BonusId,
			"number":           b.Number,
			"name":             b.Name,
			"is_answered":      b.IsAnswered,
			"expired":          b.Expired,
			"seconds_to_start": b.SecondsToStart,
			"seconds_left":     b.SecondsLeft,
			"award_time":       b.AwardTime,
		})
	}
	return out
}

func limitHelps(items []encx.Help, limit int) []map[string]any {
	items = clampSlice(items, limit)
	out := make([]map[string]any, 0, len(items))
	for _, h := range items {
		text := ""
		if h.HelpText != nil {
			text = summarizeDebugText(stripHTML(*h.HelpText), maxToolTextForLLM)
		} else if h.PenaltyComment != nil {
			text = summarizeDebugText(stripHTML(*h.PenaltyComment), maxToolTextForLLM)
		}
		out = append(out, map[string]any{
			"help_id":              h.HelpId,
			"number":               h.Number,
			"text":                 text,
			"is_penalty":           h.IsPenalty,
			"penalty":              h.Penalty,
			"remain_seconds":       h.RemainSeconds,
			"penalty_help_state":   h.PenaltyHelpState,
			"request_confirmation": h.RequestConfirm,
		})
	}
	return out
}

func limitSectors(items []encx.Sector, limit int) []map[string]any {
	items = clampSlice(items, limit)
	out := make([]map[string]any, 0, len(items))
	for _, s := range items {
		out = append(out, map[string]any{
			"sector_id":   s.SectorId,
			"order":       s.Order,
			"name":        s.Name,
			"is_answered": s.IsAnswered,
			"answer":      s.Answer,
		})
	}
	return out
}

func limitMessages(items []encx.AdminMessage, limit int) []map[string]any {
	items = clampSlice(items, limit)
	out := make([]map[string]any, 0, len(items))
	for _, m := range items {
		out = append(out, map[string]any{
			"message_id":  m.MessageId,
			"owner_id":    m.OwnerId,
			"owner_login": m.OwnerLogin,
			"text":        summarizeDebugText(stripHTML(m.MessageText), maxToolTextForLLM),
		})
	}
	return out
}

func limitCodeActions(items []encx.CodeAction, limit int) []map[string]any {
	items = clampSlice(items, limit)
	out := make([]map[string]any, 0, len(items))
	for _, a := range items {
		out = append(out, map[string]any{
			"action_id":    a.ActionId,
			"level_number": a.LevelNumber,
			"kind":         a.Kind,
			"login":        a.Login,
			"answer":       a.Answer,
			"is_correct":   a.IsCorrect,
			"loc_datetime": a.LocDateTime,
		})
	}
	return out
}

func limitAdminTeams(items []encx.AdminTeam, limit int) []map[string]any {
	items = clampSlice(items, limit)
	out := make([]map[string]any, 0, len(items))
	for _, t := range items {
		out = append(out, map[string]any{
			"id":   t.ID,
			"name": t.Name,
		})
	}
	return out
}

func limitCorrections(items []encx.AdminCorrection, limit int) []map[string]any {
	items = clampSlice(items, limit)
	out := make([]map[string]any, 0, len(items))
	for _, c := range items {
		out = append(out, map[string]any{
			"id":       c.ID,
			"datetime": c.DateTime,
			"team":     c.Team,
			"level":    c.Level,
			"reason":   c.Reason,
			"time":     c.Time,
			"comment":  summarizeDebugText(c.Comment, maxToolTextForLLM),
		})
	}
	return out
}

func summarizeGameStatistics(stats encx.GameStatisticsResponse) map[string]any {
	out := map[string]any{
		"levels_count":        len(stats.Levels),
		"level_players_count": len(stats.LevelPlayers),
		"stat_groups_count":   len(stats.StatItems),
	}
	if stats.Game != nil {
		out["game"] = map[string]any{
			"game_id":     stats.Game.GameID,
			"title":       stats.Game.Title,
			"type":        gameTypeName(stats.Game.GameTypeID),
			"zone":        zoneName(stats.Game.ZoneId),
			"started":     stats.Game.Started,
			"in_progress": stats.Game.InProgress,
			"finished":    stats.Game.Finished,
		}
	}
	out["levels"] = summarizeStatLevels(stats.Levels, 50)
	out["top"] = summarizeStatTop(stats.StatItems, 20)
	return out
}

func summarizeStatLevels(items []encx.LevelStatInfo, limit int) []map[string]any {
	items = clampSlice(items, limit)
	out := make([]map[string]any, 0, len(items))
	for _, l := range items {
		out = append(out, map[string]any{
			"level_id":       l.LevelId,
			"level_number":   l.LevelNumber,
			"level_name":     l.LevelName,
			"passed_players": l.PassedPlayers,
			"dismissed":      l.Dismissed,
		})
	}
	return out
}

func summarizeStatTop(groups [][]encx.StatItem, limit int) []map[string]any {
	if len(groups) == 0 {
		return nil
	}
	items := clampSlice(groups[0], limit)
	out := make([]map[string]any, 0, len(items))
	for _, s := range items {
		out = append(out, map[string]any{
			"team_id":       s.TeamId,
			"team_name":     s.TeamName,
			"user_id":       s.UserId,
			"user_name":     s.UserName,
			"level_num":     s.LevelNum,
			"spent_seconds": s.SpentSeconds,
			"scores":        s.Scores,
		})
	}
	return out
}

func summarizeGenericJSON(result string) (string, bool) {
	var value any
	if !decodeJSON(result, &value) {
		return "", false
	}
	reduced := reduceJSONValue(value, 0)
	return marshalToolSummary(reduced), true
}

func reduceJSONValue(v any, depth int) any {
	if depth >= 4 {
		return "<omitted>"
	}
	switch val := v.(type) {
	case map[string]any:
		if errMsg, ok := val["error"].(string); ok {
			return map[string]any{"error": errMsg}
		}
		out := make(map[string]any, len(val))
		for k, item := range val {
			out[k] = reduceJSONValue(item, depth+1)
		}
		return out
	case []any:
		limit := 20
		out := make([]any, 0, min(len(val), limit)+1)
		for i, item := range val {
			if i >= limit {
				out = append(out, map[string]any{"omitted_items": len(val) - limit})
				break
			}
			out = append(out, reduceJSONValue(item, depth+1))
		}
		return out
	case string:
		return summarizeDebugText(val, maxToolTextForLLM)
	default:
		return v
	}
}

func clampSlice[T any](items []T, limit int) []T {
	if len(items) <= limit {
		return items
	}
	return items[:limit]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
