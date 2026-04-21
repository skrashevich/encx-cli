package main

import "encoding/json"

func getTools(reviewMode bool) []llmTool {
	tools := []llmTool{
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
			Description: "List all levels with their IDs (admin panel). Works for any game you author — use this to find level numbers/IDs, then use admin_level_content to inspect the actual task text, answers, bonuses, and hints.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"}},"required":["game_id"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "admin_level_content",
			Description: "Read one level from the admin panel: task/scenario text, sector answers, bonuses, hints, comment, and settings. Use this when you need to verify that uploaded content matches the task, even if the game is not active and player APIs return no active level.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"},"level_number":{"type":"integer","description":"Level number from admin_levels"}},"required":["game_id","level_number"]}`),
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
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"},"level_id":{"type":"integer","description":"Level ID (from admin-levels)"},"level_number":{"type":"integer","description":"Level number in the game (for display)"},"name":{"type":"string","description":"New level name"}},"required":["game_id","level_id","name"]}`),
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
			Name:        "admin_update_sector",
			Description: "Update a sector by its ID. Only specified fields are changed; others are preserved.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"},"level_number":{"type":"integer","description":"Level number"},"sector_id":{"type":"integer","description":"Sector ID"},"name":{"type":"string","description":"Sector name"},"answers":{"type":"array","items":{"type":"string"},"description":"Accepted answers"}},"required":["game_id","level_number","sector_id"]}`),
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
		{Type: "function", Function: llmFunction{
			Name:        "admin_game_info",
			Description: "Read game settings: title, authors, description, prize, dates",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"}},"required":["game_id"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "admin_update_game",
			Description: "Update game settings (title, description, authors, prize, finish date). Only specified fields are changed; others are preserved.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"},"title":{"type":"string","description":"Game title"},"authors":{"type":"string","description":"Game authors"},"description":{"type":"string","description":"Game description (HTML)"},"prize":{"type":"string","description":"Prize value"},"finish":{"type":"string","description":"Finish datetime DD.MM.YYYY HH:MM:SS"}},"required":["game_id"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "admin_not_deliver",
			Description: "Mark a game as not delivered (несостоявшаяся). This is irreversible.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"}},"required":["game_id"]}`),
		}},
	}
	if reviewMode {
		tools = append(tools, llmTool{Type: "function", Function: llmFunction{
			Name:        "propose_admin_fix",
			Description: "Queue exactly one proposed fix for user approval during review mode. Use this instead of direct admin mutation tools when you detect a concrete issue. Each proposal must be minimal and target one problem only.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"title":{"type":"string","description":"Short human-readable title for this fix"},"summary":{"type":"string","description":"Why this fix is needed and what is wrong right now"},"level_number":{"type":"integer","description":"Affected level number when applicable"},"steps":{"type":"array","description":"Concrete admin mutation calls to execute if the user approves this fix","items":{"type":"object","properties":{"tool":{"type":"string","enum":["admin_set_autopass","admin_set_block","admin_create_bonus","admin_delete_bonus","admin_create_sector","admin_delete_sector","admin_create_hint","admin_delete_hint","admin_create_task","admin_set_comment"]},"arguments":{"type":"object","description":"Arguments for that admin tool call"}},"required":["tool","arguments"]}}},"required":["title","summary","steps"]}`),
		}})
		filtered := make([]llmTool, 0, len(tools))
		for _, tool := range tools {
			if shouldExposeToolInReview(tool.Function.Name) {
				filtered = append(filtered, tool)
			}
		}
		return filtered
	}
	return tools
}
