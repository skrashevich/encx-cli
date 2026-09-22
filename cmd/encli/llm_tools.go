package main

import "encoding/json"

func getTools() []llmTool {
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
			Description: "Send a code answer (level, sector, or bonus)",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"},"code":{"type":"string","description":"The code to submit"}},"required":["game_id","code"]}`),
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
			Description: "List all levels with their IDs (admin panel). Works for any game you author — use this to find level numbers/IDs, then use admin_game_scenario for the full scenario or admin_level_content for one level.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"}},"required":["game_id"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "admin_level_content",
			Description: "Read one level from the admin panel: task/scenario text, sector answers, bonuses, hints, comment, and settings. Use this when you need to verify that uploaded content matches the task, even if the game is not active and player APIs return no active level.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"},"level_number":{"type":"integer","description":"Level number from admin_levels"}},"required":["game_id","level_number"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "admin_game_scenario",
			Description: "Read the complete game scenario in one tool call: all levels, full task texts, answers, hints, penalty hints, bonuses and exported timings. Prefer this for showing, summarizing or auditing a whole scenario instead of calling admin_levels and admin_level_content for every level. Use admin_level_content only for an individual level or editable object IDs. Read-only; requires author access.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","minimum":1,"description":"Game ID"}},"required":["game_id"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "inspect_scenario_file",
			Description: "Parse an Encounter GameScenario HTML file locally and return exact counts and title without loading raw HTML into context. Use BEFORE creating a game from an attached HTML scenario. The source game ID belongs to the source domain; do not use it on the target domain.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "admin_import_scenario",
			Description: "Import all levels, names, tasks, hints, bonuses, sector answers, comments and timers directly from an Encounter GameScenario HTML file. Aligns existing levels by position, replacing mismatched content and creating missing levels; never wipes the game or deletes extra levels. Verifies against a fresh full export and reports success only on a match. Use after admin_create_game instead of hundreds of manual tool calls. Requires a target game_id on the current domain.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","minimum":1},"path":{"type":"string"}},"required":["game_id","path"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "admin_verify_scenario",
			Description: "Compare every imported level against the source GameScenario HTML file; read-only. Reports missing or mismatched content, settings and extra levels.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","minimum":1},"path":{"type":"string"}},"required":["game_id","path"]}`),
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
			Description: "Add a task (assignment text) to a level that has none. A level holds one task: if it already has one this fails — read the task id with admin_level_content and call admin_update_task to replace the text.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"},"level_number":{"type":"integer","description":"Level number"},"text":{"type":"string","description":"Task text"}},"required":["game_id","level_number","text"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "admin_update_task",
			Description: "Replace the text of an existing task. This is how a level's assignment is rewritten; admin_create_task only adds a task to a level that has none. Task ids come from admin_level_content. Works even while the game is running.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"},"level_number":{"type":"integer","description":"Level number"},"task_id":{"type":"integer","description":"Task ID from admin_level_content"},"text":{"type":"string","description":"New task text"}},"required":["game_id","level_number","task_id","text"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "admin_delete_task",
			Description: "Delete a task from a level by its ID (from admin_level_content). Use admin_update_task to rewrite a task; delete only when the level should have no task at all.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"},"level_number":{"type":"integer","description":"Level number"},"task_id":{"type":"integer","description":"Task ID from admin_level_content"}},"required":["game_id","level_number","task_id"]}`),
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
			Name:        "admin_create_game",
			Description: "Create a new game. Returns the new game's ID (use it as game_id for subsequent admin_* calls).",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"title":{"type":"string","description":"Game title"},"description":{"type":"string","description":"Game description (HTML)"},"start":{"type":"string","description":"Start datetime, RFC3339 (e.g. 2026-09-10T18:00:00+03:00)"},"finish":{"type":"string","description":"Finish datetime, RFC3339"},"game_type":{"type":"integer","enum":[0,1,2],"description":"0 Single, 1 Team, 2 Personal"},"zone_id":{"type":"integer","description":"Game zone, legacy engine only (0 Схватка, 1 Мозговой штурм, 2 Фотоэкстрим, 3 Мокрые войны, 4 Кэшинг, 5 Фотоохота, 7 Точки, 8 Конкурс, 9 Викторина). The new engine takes the zone from the domain and rejects any non-zero value."},"authors":{"type":"string","description":"Comma-separated author logins; defaults to the current user if omitted"},"request_last_date":{"type":"string","description":"Last date to request participation, RFC3339"},"moderated":{"type":"boolean","description":"Require moderation of answers"}},"required":["title","start","finish"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "admin_wipe_game",
			Description: "Completely reset a game: delete all bonuses, sectors, hints, levels, and corrections",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"}},"required":["game_id"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "inspect_game_scenario",
			Description: "Read a source game's scenario and return its title and exact content counts before copying. Supports another Encounter domain through source_domain, using its saved login. Does not change the current domain or game. Do this before creating the destination game.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"source_domain":{"type":"string","description":"Source domain, e.g. svk.en.cx; defaults to current domain"},"source_game_id":{"type":"integer"}},"required":["source_game_id"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "admin_copy_game",
			Description: "Copy the complete scenario from a source game to an existing target game on the CURRENT domain, then verify by reading back. Specify source_domain for cross-domain copying. If no target exists, inspect_game_scenario first and admin_create_game before copying. Never use the source ID as the target ID just because the user says here. Reports verified=true only when the scenario matches.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"source_domain":{"type":"string","description":"Source Encounter domain, e.g. svk.en.cx; defaults to current domain. Target always stays on current domain."},"source_game_id":{"type":"integer","description":"Source game ID to copy from"},"target_game_id":{"type":"integer","description":"Target game ID to copy to"}},"required":["source_game_id","target_game_id"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "admin_delete_game",
			Description: "Delete a game entirely, together with its levels, bonuses, hints and results. Irreversible: only call it when the user explicitly asked to delete that game. To empty a game but keep it, use admin_wipe_game instead.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"}},"required":["game_id"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "admin_game_info",
			Description: "Read game settings: title, authors, description, prize, dates",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"}},"required":["game_id"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "admin_update_game",
			Description: "Update game settings (title, description, authors, prize, start/finish dates, request deadline, request moderation). Only specified fields are changed; others are preserved.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"},"title":{"type":"string","description":"Game title"},"authors":{"type":"string","description":"Game authors"},"description":{"type":"string","description":"Game description (HTML)"},"prize":{"type":"string","description":"Prize value"},"start":{"type":"string","description":"Start datetime, RFC3339 (e.g. 2026-09-10T18:00:00+03:00). Cannot be changed once the game has started."},"finish":{"type":"string","description":"Finish datetime, RFC3339"},"request_last_date":{"type":"string","description":"Last date to request participation, RFC3339"},"accept_rate_from":{"type":"string","description":"Date the game starts accepting ratings, RFC3339. The engine refuses a start later than this, so moving start forward moves it along unless you set it here."},"moderated":{"type":"boolean","description":"Participation requests need the organizer's approval; false accepts them automatically"}},"required":["game_id"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "admin_not_deliver",
			Description: "Mark a game as not delivered (несостоявшаяся). This is irreversible.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"game_id":{"type":"integer","description":"Game ID"}},"required":["game_id"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "read_local_file",
			Description: "Read a text file from the local filesystem (relative to LLM_FILES_ROOT or current working directory). Use to inspect scripts, notes, or uploaded scenario files.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"File path (relative to LLM_FILES_ROOT or absolute within it)"},"max_bytes":{"type":"integer","description":"Max bytes to read (default 65536, max 524288)"},"offset":{"type":"integer","description":"Byte offset to start reading from"}},"required":["path"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "list_local_dir",
			Description: "List files and subdirectories in a local directory (relative to LLM_FILES_ROOT or cwd).",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Directory path (default: root)"},"recursive":{"type":"boolean","description":"List recursively up to depth 3 (max 500 entries)"}}}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "read_pdf_file",
			Description: "Extract plain text from a local PDF file (relative to LLM_FILES_ROOT or current working directory). Use to inspect uploaded scenario documents, rulebooks, or scans saved as PDF.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"File path (relative to LLM_FILES_ROOT or absolute within it)"},"page":{"type":"integer","description":"Extract only this page (1-based). Omit to read the whole document."},"max_bytes":{"type":"integer","description":"Max bytes of extracted text to return (default 65536, max 524288)"}},"required":["path"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "search_local_files",
			Description: "Search for a substring in local text files under a directory. Returns matching file paths, line numbers, and snippets.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Directory or file to search (default: LLM_FILES_ROOT)"},"pattern":{"type":"string","description":"Case-insensitive substring to find"},"glob":{"type":"string","description":"Optional filename glob, e.g. *.md"},"max_matches":{"type":"integer","description":"Max matches to return (default 50)"}},"required":["pattern"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "wikipedia_search",
			Description: "Search Wikipedia for articles matching a query. Use to find facts, dates, places, or verify names before writing game content.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"Search query"},"lang":{"type":"string","description":"Wikipedia language code (default: ru)"},"limit":{"type":"integer","description":"Max results (default 5, max 20)"}},"required":["query"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "wikipedia_article",
			Description: "Fetch a Wikipedia article summary (plain-text intro extract) by title. Use to verify facts from a known article title.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"title":{"type":"string","description":"Article title"},"lang":{"type":"string","description":"Wikipedia language code (default: ru)"}},"required":["title"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "fetch_url",
			Description: "Fetch an external web page or text document by URL and return it as plain text (HTML is converted to readable text). Use this whenever the user gives a link — question packs, rules, articles, any public page. Do not tell the user a link cannot be opened; call this tool.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"url":{"type":"string","description":"Absolute http or https URL"},"max_bytes":{"type":"integer","description":"Max bytes of text to return (default 65536, max 524288)"},"offset":{"type":"integer","description":"Byte offset into the extracted text; use to page through a long document"}},"required":["url"]}`),
		}},
		{Type: "function", Function: llmFunction{
			Name:        "propose_admin_fix",
			Description: "Queue exactly one proposed fix for user approval. Use this instead of direct admin mutation tools when the user asked to check/audit/review existing content rather than to change it. Each proposal must be minimal and target one problem only.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"title":{"type":"string","description":"Short human-readable title for this fix"},"summary":{"type":"string","description":"Why this fix is needed and what is wrong right now"},"level_number":{"type":"integer","description":"Affected level number when applicable"},"steps":{"type":"array","description":"Concrete admin mutation calls to execute if the user approves this fix","items":{"type":"object","properties":{"tool":{"type":"string","enum":["admin_set_autopass","admin_set_block","admin_create_bonus","admin_delete_bonus","admin_create_sector","admin_delete_sector","admin_create_hint","admin_delete_hint","admin_create_task","admin_set_comment"]},"arguments":{"type":"object","description":"Arguments for that admin tool call"}},"required":["tool","arguments"]}}},"required":["title","summary","steps"]}`),
		}},
	}
	return tools
}
