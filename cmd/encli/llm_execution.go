package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/skrashevich/encx-cli/encx"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

// executeToolCallSafe runs a tool call, capturing stdout and recovering from fatal panics.
func executeToolCallSafe(ctx context.Context, cfg *config, client *encx.Client, session *llmSession, name, argsJSON string) string {
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
		executeLLMToolCall(ctx, cfg, client, session, name, argsJSON)
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
		name, time.Since(start).Round(time.Millisecond), summarizeDebugText(result, 0))
	return result
}

func executeLLMToolCall(ctx context.Context, cfg *config, client *encx.Client, session *llmSession, name, argsJSON string) {
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
		return decodeBareUnicodeEscapes(s)
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
				result = append(result, decodeBareUnicodeEscapes(s))
			}
		}
		return result
	}

	if gid := getInt("game_id"); gid != 0 {
		cfg.gameId = gid
		debugf("tool execution context: set cfg.gameId=%d from tool args", gid)
	}
	if session != nil && session.reviewApprovalMode && !session.applyingApprovedFix && isAdminMutationTool(name) {
		fatal("Direct admin mutations are disabled during review. Use propose_admin_fix instead.")
	}

	switch name {
	case "propose_admin_fix":
		if session == nil || !session.reviewApprovalMode {
			fatal("propose_admin_fix is only available in review mode")
		}
		proposal, err := parsePendingAdminFix(args, cfg.gameId)
		if err != nil {
			fatal("Invalid fix proposal: %v", err)
		}
		session.pendingFixes = append(session.pendingFixes, proposal)
		outputJSON(map[string]any{
			"queued": true,
			"index":  len(session.pendingFixes),
			"title":  proposal.Title,
		})

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

	case "admin_level_content":
		requireAuth(ctx, cfg, client)
		cmdAdminLevelContent(ctx, cfg, client, []string{strconv.Itoa(getInt("level_number"))})

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

	case "admin_update_sector":
		requireAuth(ctx, cfg, client)
		positional := []string{
			strconv.Itoa(getInt("level_number")),
			strconv.Itoa(getInt("sector_id")),
		}
		if n := getString("name"); n != "" {
			positional = append(positional, "name="+n)
		}
		if answers := getStringSlice("answers"); len(answers) > 0 {
			positional = append(positional, "answers="+strings.Join(answers, ","))
		}
		cmdAdminUpdateSector(ctx, cfg, client, positional)

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

	case "admin_game_info":
		requireAuth(ctx, cfg, client)
		cmdAdminGameInfo(ctx, cfg, client)

	case "admin_update_game":
		requireAuth(ctx, cfg, client)
		var positional []string
		if t := getString("title"); t != "" {
			positional = append(positional, "title="+t)
		}
		if a := getString("authors"); a != "" {
			positional = append(positional, "authors="+a)
		}
		if d := getString("description"); d != "" {
			positional = append(positional, "description="+d)
		}
		if p := getString("prize"); p != "" {
			positional = append(positional, "prize="+p)
		}
		if f := getString("finish"); f != "" {
			positional = append(positional, "finish="+f)
		}
		cmdAdminUpdateGame(ctx, cfg, client, positional)

	case "admin_not_deliver":
		requireAuth(ctx, cfg, client)
		cmdAdminNotDeliver(ctx, cfg, client)

	default:
		fatal("Unknown tool call: %s", name)
	}
}
