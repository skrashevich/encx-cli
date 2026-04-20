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
	"time"

	"github.com/skrashevich/encx-cli/encx"
)

const (
	openRouterURL   = "https://openrouter.ai/api/v1/chat/completions"
	defaultLLMModel = "openai/gpt-4o-mini"
)

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
	Tools    []llmTool    `json:"tools"`
}

type llmMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type llmResponse struct {
	Choices []llmChoice `json:"choices"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type llmChoice struct {
	Message llmAssistantMessage `json:"message"`
}

type llmAssistantMessage struct {
	Content   string         `json:"content"`
	ToolCalls []llmToolCall  `json:"tool_calls"`
}

type llmToolCall struct {
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
			Description: "Show all levels with their pass/dismiss status",
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
			Description: "Enter a game (submit application to join)",
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
			Description: "List all levels with their IDs in the admin panel",
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

	// Force JSON output in LLM mode for structured results
	cfg.jsonOutput = true
	jsonMode = true

	systemPrompt := `You are an assistant for the Encounter (en.cx) game engine CLI tool.
The user gives you a natural language request. You must call the appropriate tool to fulfill it.
The current domain is: ` + cfg.domain + `
` + func() string {
		if cfg.gameId != 0 {
			return fmt.Sprintf("The current game ID is: %d\n", cfg.gameId)
		}
		return ""
	}() + `
Rules:
- Always use tool calls to execute commands. Never just describe what to do.
- If the user mentions a game ID, use it. If not and there's a current game ID set, use that.
- For admin_copy_game: the source is -game-id (or mentioned first), the target is the second game ID mentioned.
- Respond in the same language as the user's request.`

	req := llmRequest{
		Model: model,
		Messages: []llmMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: prompt},
		},
		Tools: getTools(),
	}

	body, err := json.Marshal(req)
	if err != nil {
		fatal("Failed to marshal LLM request: %v", err)
	}

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, "POST", openRouterURL, bytes.NewReader(body))
	if err != nil {
		fatal("Failed to create HTTP request: %v", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		fatal("LLM API request failed: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		fatal("Failed to read LLM response: %v", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		fatal("LLM API HTTP error %d: %s", resp.StatusCode, string(respBody))
	}

	var llmResp llmResponse
	if err := json.Unmarshal(respBody, &llmResp); err != nil {
		fatal("Failed to parse LLM response: %v", err)
	}

	if llmResp.Error != nil {
		fatal("LLM API error: %s", llmResp.Error.Message)
	}

	if len(llmResp.Choices) == 0 {
		fatal("LLM returned no choices")
	}

	choice := llmResp.Choices[0]

	if len(choice.Message.ToolCalls) == 0 {
		// No tool call — just print the text response
		if choice.Message.Content != "" {
			fmt.Println(choice.Message.Content)
		} else {
			fatal("LLM did not produce a tool call or text response")
		}
		return
	}

	// Execute the first tool call
	tc := choice.Message.ToolCalls[0]
	if len(choice.Message.ToolCalls) > 1 {
		fmt.Fprintf(os.Stderr, "Warning: LLM produced %d tool calls, executing only the first (%s)\n",
			len(choice.Message.ToolCalls), tc.Function.Name)
	}
	executeLLMToolCall(ctx, cfg, client, tc.Function.Name, tc.Function.Arguments)
}

func executeLLMToolCall(ctx context.Context, cfg *config, client *encx.Client, name, argsJSON string) {
	var args map[string]any
	if argsJSON != "" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			fatal("Failed to parse tool arguments: %v", err)
		}
	}

	// Helper to extract int from args
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

	// Apply game_id from args if present
	if gid := getInt("game_id"); gid != 0 {
		cfg.gameId = gid
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

