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
	if session != nil && session.antiSpamResult != "" {
		var blocked map[string]any
		_ = json.Unmarshal([]byte(session.antiSpamResult), &blocked)
		blocked["skipped"] = true
		payload, _ := json.Marshal(blocked)
		return string(payload)
	}
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
					if fe.AntiSpamURL != "" {
						payload, _ := json.Marshal(map[string]any{
							"error":            fe.Message,
							"code":             "antispam_required",
							"verification_url": fe.AntiSpamURL,
							"action":           "Stop requests. Ask the user to complete browser verification, then continue in a new turn. Do not log in again or invent credentials. Preserve completed work; verify server state before retrying writes.",
						})
						result = string(payload)
						if session != nil {
							session.antiSpamResult = result
						}
					}
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
	if securityBlocksMutation(session, name) {
		fatal("Tool %q is blocked in read-only mode", name)
	}
	switch name {
	case "propose_admin_fix":
		if session == nil {
			fatal("propose_admin_fix requires an active agent session")
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
		login, password := getString("login"), getString("password")
		if login == "" || password == "" {
			fatal("LLM tool 'login' requires both login and password parameters")
		}
		if strings.EqualFold(strings.TrimSpace(login), "__ASK_USER__") || strings.EqualFold(strings.TrimSpace(password), "__ASK_USER__") {
			fatal("Login requires real user-provided credentials; ask the user to sign in through settings instead of sending __ASK_USER__")
		}
		cfg.login, cfg.password = login, password
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
		requireAdminAuth(ctx, cfg, client)
		cmdAdminGames(ctx, cfg, client)

	case "admin_levels":
		requireAdminAuth(ctx, cfg, client)
		cmdAdminLevels(ctx, cfg, client)

	case "admin_level_content":
		requireAdminAuth(ctx, cfg, client)
		cmdAdminLevelContent(ctx, cfg, client, []string{strconv.Itoa(getInt("level_number"))})

	case "admin_game_scenario":
		if getInt("game_id") <= 0 {
			fatal("game_id must be positive")
		}
		requireAdminAuth(ctx, cfg, client)
		doc, err := client.GetAdminGameScenario(ctx, cfg.gameId)
		if err != nil {
			fatalEncx("Read game scenario", err)
		}
		outputJSON(doc)

	case "inspect_scenario_file":
		toolInspectScenario(getString("path"))
	case "admin_import_scenario":
		requireAdminAuth(ctx, cfg, client)
		toolImportScenario(ctx, cfg, client, getString("path"))
	case "admin_verify_scenario":
		requireAdminAuth(ctx, cfg, client)
		toolVerifyScenario(ctx, cfg, client, getString("path"))
	case "admin_create_levels":
		requireAdminAuth(ctx, cfg, client)
		cmdAdminCreateLevels(ctx, cfg, client, []string{strconv.Itoa(getInt("count"))})

	case "admin_delete_level":
		requireAdminAuth(ctx, cfg, client)
		cmdAdminDeleteLevel(ctx, cfg, client, []string{strconv.Itoa(getInt("level_number"))})

	case "admin_rename_level":
		requireAdminAuth(ctx, cfg, client)
		cmdAdminRenameLevel(ctx, cfg, client, []string{strconv.Itoa(getInt("level_id")), getString("name")})

	case "admin_set_autopass":
		requireAdminAuth(ctx, cfg, client)
		positional := []string{strconv.Itoa(getInt("level_number")), getString("time")}
		if pt := getString("penalty_time"); pt != "" {
			positional = append(positional, pt)
		}
		cmdAdminUpdateAutopass(ctx, cfg, client, positional)

	case "admin_set_block":
		requireAdminAuth(ctx, cfg, client)
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
		requireAdminAuth(ctx, cfg, client)
		positional := []string{
			strconv.Itoa(getInt("level_number")),
			strconv.Itoa(getInt("level_id")),
			getString("name"),
		}
		positional = append(positional, getStringSlice("answers")...)
		cmdAdminCreateBonus(ctx, cfg, client, positional)

	case "admin_delete_bonus":
		requireAdminAuth(ctx, cfg, client)
		cmdAdminDeleteBonus(ctx, cfg, client, []string{
			strconv.Itoa(getInt("level_number")),
			strconv.Itoa(getInt("bonus_id")),
		})

	case "admin_create_sector":
		requireAdminAuth(ctx, cfg, client)
		positional := []string{
			strconv.Itoa(getInt("level_number")),
			getString("name"),
		}
		positional = append(positional, getStringSlice("answers")...)
		cmdAdminCreateSector(ctx, cfg, client, positional)

	case "admin_delete_sector":
		requireAdminAuth(ctx, cfg, client)
		cmdAdminDeleteSector(ctx, cfg, client, []string{
			strconv.Itoa(getInt("level_number")),
			strconv.Itoa(getInt("sector_id")),
		})

	case "admin_update_sector":
		requireAdminAuth(ctx, cfg, client)
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
		requireAdminAuth(ctx, cfg, client)
		cmdAdminCreateHint(ctx, cfg, client, []string{
			strconv.Itoa(getInt("level_number")),
			getString("delay"),
			getString("text"),
		})

	case "admin_delete_hint":
		requireAdminAuth(ctx, cfg, client)
		cmdAdminDeleteHint(ctx, cfg, client, []string{
			strconv.Itoa(getInt("level_number")),
			strconv.Itoa(getInt("hint_id")),
		})

	case "admin_create_task":
		requireAdminAuth(ctx, cfg, client)
		cmdAdminCreateTask(ctx, cfg, client, []string{
			strconv.Itoa(getInt("level_number")),
			getString("text"),
		})

	case "admin_update_task":
		requireAdminAuth(ctx, cfg, client)
		cmdAdminUpdateTask(ctx, cfg, client, []string{
			strconv.Itoa(getInt("level_number")),
			strconv.Itoa(getInt("task_id")),
			getString("text"),
		})

	case "admin_delete_task":
		requireAdminAuth(ctx, cfg, client)
		cmdAdminDeleteTask(ctx, cfg, client, []string{
			strconv.Itoa(getInt("level_number")),
			strconv.Itoa(getInt("task_id")),
		})

	case "admin_set_comment":
		requireAdminAuth(ctx, cfg, client)
		positional := []string{strconv.Itoa(getInt("level_number")), getString("name")}
		if c := getString("comment"); c != "" {
			positional = append(positional, c)
		}
		cmdAdminSetComment(ctx, cfg, client, positional)

	case "admin_teams":
		requireAdminAuth(ctx, cfg, client)
		cmdAdminTeams(ctx, cfg, client)

	case "admin_corrections":
		requireAdminAuth(ctx, cfg, client)
		cmdAdminCorrections(ctx, cfg, client)

	case "admin_add_correction":
		requireAdminAuth(ctx, cfg, client)
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
		requireAdminAuth(ctx, cfg, client)
		cmdAdminDeleteCorrection(ctx, cfg, client, []string{getString("correction_id")})

	case "admin_create_game":
		requireAdminAuth(ctx, cfg, client)
		var positional []string
		if t := getString("title"); t != "" {
			positional = append(positional, "title="+t)
		}
		if d := getString("description"); d != "" {
			positional = append(positional, "description="+d)
		}
		if s := getString("start"); s != "" {
			positional = append(positional, "start="+s)
		}
		if f := getString("finish"); f != "" {
			positional = append(positional, "finish="+f)
		}
		if _, ok := args["game_type"]; ok {
			positional = append(positional, "game_type="+strconv.Itoa(getInt("game_type")))
		}
		if _, ok := args["zone_id"]; ok {
			positional = append(positional, "zone_id="+strconv.Itoa(getInt("zone_id")))
		}
		if a := getString("authors"); a != "" {
			positional = append(positional, "authors="+a)
		}
		if r := getString("request_last_date"); r != "" {
			positional = append(positional, "request_last_date="+r)
		}
		if v, ok := args["moderated"]; ok {
			if b, ok := v.(bool); ok && b {
				positional = append(positional, "moderated=true")
			}
		}
		cmdAdminCreateGame(ctx, cfg, client, positional)

	case "admin_wipe_game":
		requireAdminAuth(ctx, cfg, client)
		cmdAdminWipeGame(ctx, cfg, client)

	case "inspect_game_scenario":
		if getInt("source_game_id") <= 0 {
			fatal("source_game_id must be positive")
		}
		sourceCfg, source := agentScenarioSource(ctx, cfg, client, getString("source_domain"))
		doc := readRemoteAgentScenario(ctx, source, getInt("source_game_id"))
		result := scenarioSummary(doc)
		result["source_domain"] = sourceCfg.domain
		outputJSON(result)

	case "admin_copy_game":
		sourceID, targetID := getInt("source_game_id"), getInt("target_game_id")
		if sourceID <= 0 || targetID <= 0 {
			fatal("source_game_id and target_game_id must be positive")
		}
		sourceDomain, err := scenarioSourceDomain(getString("source_domain"), cfg.domain)
		if err != nil {
			fatal("%v", err)
		}
		if sourceDomain == cfg.domain && sourceID == targetID {
			fatal("Source and target must be different games")
		}
		_, source := agentScenarioSource(ctx, cfg, client, sourceDomain)
		// Read the entire source before authenticating or writing to the target.
		doc := readRemoteAgentScenario(ctx, source, sourceID)
		requireAdminAuth(ctx, cfg, client)
		targetCfg := *cfg
		targetCfg.gameId = targetID
		importAgentScenario(ctx, &targetCfg, client, doc)

	case "admin_delete_game":
		requireAdminAuth(ctx, cfg, client)
		// The confirmation the CLI asks a human to type is the game id itself,
		// which the tool call already carries.
		cmdAdminDeleteGame(ctx, cfg, client, []string{strconv.Itoa(cfg.gameId)})

	case "admin_game_info":
		requireAdminAuth(ctx, cfg, client)
		cmdAdminGameInfo(ctx, cfg, client)

	case "admin_update_game":
		requireAdminAuth(ctx, cfg, client)
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
		if s := getString("start"); s != "" {
			positional = append(positional, "start="+s)
		}
		if f := getString("finish"); f != "" {
			positional = append(positional, "finish="+f)
		}
		if r := getString("request_last_date"); r != "" {
			positional = append(positional, "request_last_date="+r)
		}
		if a := getString("accept_rate_from"); a != "" {
			positional = append(positional, "accept_rate_from="+a)
		}
		// An absent moderated flag leaves the game's current setting alone;
		// cmdAdminUpdateGame reads the game before writing it back.
		if moderated, ok := moderatedArg(args["moderated"]); ok {
			positional = append(positional, "moderated="+strconv.FormatBool(moderated))
		}
		cmdAdminUpdateGame(ctx, cfg, client, positional)

	case "admin_not_deliver":
		requireAdminAuth(ctx, cfg, client)
		cmdAdminNotDeliver(ctx, cfg, client)

	case "read_local_file":
		toolReadLocalFile(getString("path"), getInt("max_bytes"), getInt("offset"))

	case "list_local_dir":
		recursive := false
		if v, ok := args["recursive"]; ok {
			if b, ok := v.(bool); ok {
				recursive = b
			}
		}
		path := getString("path")
		if path == "" {
			path = "."
		}
		toolListLocalDir(path, recursive)

	case "search_local_files":
		root := getString("path")
		if root == "" {
			root = "."
		}
		toolSearchLocalFiles(root, getString("pattern"), getString("glob"), getInt("max_matches"))

	case "read_pdf_file":
		toolReadPdfFile(getString("path"), getInt("page"), getInt("max_bytes"))

	case "wikipedia_search":
		toolWikipediaSearch(ctx, getString("query"), getString("lang"), getInt("limit"))

	case "wikipedia_article":
		toolWikipediaArticle(ctx, getString("title"), getString("lang"))

	case "fetch_url":
		toolFetchURL(ctx, getString("url"), getInt("max_bytes"), getInt("offset"))

	default:
		fatal("Unknown tool call: %s", name)
	}
}
