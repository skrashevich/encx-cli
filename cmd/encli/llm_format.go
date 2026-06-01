package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func formatToolCallForDisplay(session *llmSession, name, argsJSON string) string {
	var args map[string]any
	json.Unmarshal([]byte(argsJSON), &args)

	lvl := getAnyInt(args["level_number"])
	if lvl == 0 {
		lvl = getAnyInt(args["level_id"])
	}
	gameID := getAnyInt(args["game_id"])
	itemName := getAnyString(args["name"])
	rt := session.reviewText

	ts := time.Now().Format("15:04:05")

	// Build context prefix: "game#123 ур.2" or "game#123" or ""
	var ctx string
	if gameID > 0 && lvl > 0 {
		ctx = fmt.Sprintf("game#%d ур.%d", gameID, lvl)
	} else if gameID > 0 {
		ctx = fmt.Sprintf("game#%d", gameID)
	} else if lvl > 0 {
		ctx = fmt.Sprintf("ур.%d", lvl)
	}

	format := func(action string) string {
		if ctx != "" {
			return fmt.Sprintf("%s  %s — %s", ts, ctx, action)
		}
		return fmt.Sprintf("%s  %s", ts, action)
	}

	switch name {
	case "admin_levels":
		return format(rt("Fetching level list", "Получаю список уровней"))
	case "admin_level_content":
		return format(rt("Reading level content", "Читаю содержимое уровня"))
	case "admin_games":
		return format(rt("Fetching authored games", "Получаю список авторских игр"))
	case "admin_teams":
		return format(rt("Fetching teams", "Получаю список команд"))
	case "admin_corrections":
		return format(rt("Fetching corrections", "Получаю коррекции"))
	case "status":
		return format(rt("Checking game status", "Проверяю статус игры"))
	case "levels":
		return format(rt("Fetching levels", "Получаю уровни"))
	case "bonuses":
		return format(rt("Fetching bonuses", "Получаю бонусы"))
	case "enter":
		return format(rt("Entering game", "Вхожу в игру"))
	case "games":
		return format(rt("Fetching game list", "Получаю список игр"))
	case "game_list":
		return format(rt("Fetching game list (JSON)", "Получаю список игр (JSON)"))
	case "hints":
		return format(rt("Fetching hints", "Получаю подсказки"))
	case "sectors":
		return format(rt("Fetching sectors", "Получаю секторы"))
	case "messages":
		return format(rt("Fetching messages", "Получаю сообщения"))
	case "send_code":
		code := getAnyString(args["code"])
		return format(rt("Sending code: ", "Отправляю код: ") + code)
	case "hint":
		return format(rt("Requesting penalty hint", "Запрашиваю штрафную подсказку"))
	case "game_stats":
		return format(rt("Fetching game stats", "Получаю статистику"))
	case "profile":
		return format(rt("Fetching profile", "Получаю профиль"))
	case "admin_create_levels":
		cnt := getAnyInt(args["count"])
		return format(fmt.Sprintf(rt("Creating %d levels", "Создаю %d уровней"), cnt))
	case "admin_delete_level":
		return format(rt("Deleting level", "Удаляю уровень"))
	case "admin_rename_level":
		newName := getAnyString(args["name"])
		if newName != "" {
			return format(rt("Renaming level → ", "Переименовываю → ") + newName)
		}
		return format(rt("Renaming level", "Переименовываю уровень"))
	case "admin_set_autopass":
		t := getAnyString(args["time"])
		return format(rt("Setting autopass: ", "Автопереход: ") + t)
	case "admin_set_block":
		return format(rt("Configuring answer block", "Настраиваю блокировку ответов"))
	case "admin_set_comment":
		n := getAnyString(args["name"])
		if n != "" {
			return format(rt("Setting name: ", "Название: ") + n)
		}
		return format(rt("Setting name/comment", "Устанавливаю название"))
	case "admin_create_bonus":
		if itemName != "" {
			return format(rt("Creating bonus: ", "Создаю бонус: ") + itemName)
		}
		return format(rt("Creating bonus", "Создаю бонус"))
	case "admin_delete_bonus":
		return format(rt("Deleting bonus", "Удаляю бонус"))
	case "admin_create_sector":
		if itemName != "" {
			return format(rt("Creating sector: ", "Создаю сектор: ") + itemName)
		}
		return format(rt("Creating sector", "Создаю сектор"))
	case "admin_delete_sector":
		return format(rt("Deleting sector", "Удаляю сектор"))
	case "admin_update_sector":
		return format(rt("Updating sector", "Обновляю сектор"))
	case "admin_create_hint":
		delay := getAnyString(args["delay"])
		if delay != "" {
			return format(rt("Creating hint (delay ", "Создаю подсказку (через ") + delay + ")")
		}
		return format(rt("Creating hint", "Создаю подсказку"))
	case "admin_delete_hint":
		return format(rt("Deleting hint", "Удаляю подсказку"))
	case "admin_create_task":
		return format(rt("Creating task", "Создаю задание"))
	case "admin_add_correction":
		return format(rt("Adding correction", "Добавляю коррекцию"))
	case "admin_delete_correction":
		return format(rt("Deleting correction", "Удаляю коррекцию"))
	case "admin_wipe_game":
		return format(rt("Wiping game (full reset)", "Очищаю игру (полный сброс)"))
	case "admin_copy_game":
		dst := getAnyInt(args["target_game_id"])
		if dst > 0 {
			return format(fmt.Sprintf(rt("Copying to game#%d", "Копирую в game#%d"), dst))
		}
		return format(rt("Copying game", "Копирую игру"))
	case "admin_game_info":
		return format(rt("Reading game settings", "Читаю настройки игры"))
	case "admin_update_game":
		// Show which fields are being updated
		fields := []string{}
		for _, f := range []string{"title", "description", "prize"} {
			if v := getAnyString(args[f]); v != "" {
				fields = append(fields, f)
			}
		}
		if len(fields) > 0 {
			return format(rt("Updating: ", "Обновляю: ") + strings.Join(fields, ", "))
		}
		return format(rt("Updating game settings", "Обновляю настройки игры"))
	case "admin_not_deliver":
		return format(rt("Marking as not delivered", "Отмечаю как несостоявшуюся"))
	case "propose_admin_fix":
		title := getAnyString(args["title"])
		return format(title)
	case "read_local_file":
		p := getAnyString(args["path"])
		if p != "" {
			return format(rt("Reading file: ", "Читаю файл: ") + p)
		}
		return format(rt("Reading local file", "Читаю локальный файл"))
	case "list_local_dir":
		p := getAnyString(args["path"])
		if p != "" {
			return format(rt("Listing directory: ", "Содержимое каталога: ") + p)
		}
		return format(rt("Listing local directory", "Содержимое локального каталога"))
	case "search_local_files":
		pat := getAnyString(args["pattern"])
		return format(rt("Searching files for: ", "Ищу в файлах: ") + pat)
	case "wikipedia_search":
		q := getAnyString(args["query"])
		return format(rt("Wikipedia search: ", "Поиск в Википедии: ") + q)
	case "wikipedia_article":
		t := getAnyString(args["title"])
		return format(rt("Wikipedia article: ", "Статья Википедии: ") + t)
	default:
		return format(fmt.Sprintf("[%s] %s", name, argsJSON))
	}
}

// formatToolApprovalAction is a short headline for approval UI (no timestamp).
func formatToolApprovalAction(session *llmSession, name, argsJSON string) string {
	line := formatToolCallForDisplay(session, name, argsJSON)
	if idx := strings.Index(line, " — "); idx >= 0 {
		return strings.TrimSpace(line[idx+3:])
	}
	if idx := strings.Index(line, " - "); idx >= 0 {
		return strings.TrimSpace(line[idx+3:])
	}
	return strings.TrimSpace(line)
}

// formatToolApprovalDetails returns human-readable bullets of what a tool call will do.
func formatToolApprovalDetails(session *llmSession, name, argsJSON string) []string {
	var args map[string]any
	_ = json.Unmarshal([]byte(argsJSON), &args)
	ru := session != nil && session.preferRussian
	var lines []string
	add := func(en, ruText string) {
		if ru {
			lines = append(lines, ruText)
		} else {
			lines = append(lines, en)
		}
	}

	gid := getAnyInt(args["game_id"])
	if gid == 0 {
		gid = getAnyInt(args["source_game_id"])
	}
	if gid > 0 {
		add(fmt.Sprintf("Game #%d", gid), fmt.Sprintf("Игра #%d", gid))
	}
	lvl := getAnyInt(args["level_number"])
	if lvl == 0 {
		lvl = getAnyInt(args["level_id"])
	}
	if lvl > 0 {
		add(fmt.Sprintf("Level %d", lvl), fmt.Sprintf("Уровень %d", lvl))
	}

	switch name {
	case "admin_create_levels":
		cnt := getAnyInt(args["count"])
		if cnt <= 0 {
			cnt = 1
		}
		add(fmt.Sprintf("Create %d new level(s)", cnt), fmt.Sprintf("Создать новых уровней: %d", cnt))
	case "admin_delete_level":
		add("Delete this level permanently", "Удалить уровень без восстановления")
	case "admin_rename_level":
		if n := getAnyString(args["name"]); n != "" {
			add("New name: "+n, "Новое название: "+n)
		}
	case "admin_create_task":
		if txt := truncateDisplay(stripHTML(getAnyString(args["text"])), 220); txt != "" {
			add("Task text: "+txt, "Текст задания: "+txt)
		}
	case "admin_create_sector", "admin_create_bonus":
		if n := getAnyString(args["name"]); n != "" {
			add("Name: "+n, "Название: "+n)
		}
		if ans := getAnyStringSlice(args["answers"]); len(ans) > 0 {
			add("Answers: "+strings.Join(ans, ", "), "Ответы: "+strings.Join(ans, ", "))
		}
	case "admin_update_sector":
		if n := getAnyString(args["name"]); n != "" {
			add("Update name: "+n, "Новое имя: "+n)
		}
		if ans := getAnyStringSlice(args["answers"]); len(ans) > 0 {
			add("Update answers: "+strings.Join(ans, ", "), "Новые ответы: "+strings.Join(ans, ", "))
		}
	case "admin_delete_sector", "admin_delete_bonus", "admin_delete_hint":
		idKey := "sector_id"
		label := "sector"
		if strings.Contains(name, "bonus") {
			idKey, label = "bonus_id", "bonus"
		}
		if strings.Contains(name, "hint") {
			idKey, label = "hint_id", "hint"
		}
		if id := getAnyInt(args[idKey]); id > 0 {
			add(fmt.Sprintf("Delete %s #%d", label, id), fmt.Sprintf("Удалить %s #%d", label, id))
		}
	case "admin_create_hint":
		if d := getAnyString(args["delay"]); d != "" {
			add("Opens after: "+d, "Откроется через: "+d)
		}
		if txt := truncateDisplay(stripHTML(getAnyString(args["text"])), 180); txt != "" {
			add("Hint: "+txt, "Подсказка: "+txt)
		}
	case "admin_set_autopass":
		if t := getAnyString(args["time"]); t != "" {
			add("Autopass: "+t, "Автопереход: "+t)
		}
	case "admin_set_block":
		att := getAnyInt(args["attempts"])
		per := getAnyString(args["period"])
		add(fmt.Sprintf("Block: %d attempts / %s", att, per),
			fmt.Sprintf("Блокировка: %d попыток / %s", att, per))
	case "admin_set_comment":
		if n := getAnyString(args["name"]); n != "" {
			add("Level name: "+n, "Название уровня: "+n)
		}
		if c := truncateDisplay(getAnyString(args["comment"]), 120); c != "" {
			add("Comment: "+c, "Комментарий: "+c)
		}
	case "admin_wipe_game":
		add("Full game reset (irreversible)", "Полная очистка игры (необратимо)")
	case "admin_copy_game":
		if dst := getAnyInt(args["target_game_id"]); dst > 0 {
			add(fmt.Sprintf("Copy all content to game #%d", dst), fmt.Sprintf("Скопировать всё в игру #%d", dst))
		}
	case "admin_update_game":
		for _, pair := range []struct{ key, labelEn, labelRu string }{
			{"title", "Title", "Название"},
			{"authors", "Authors", "Авторы"},
			{"description", "Description", "Описание"},
			{"prize", "Prize", "Приз"},
			{"finish", "Finish date", "Дата финиша"},
		} {
			if v := truncateDisplay(stripHTML(getAnyString(args[pair.key])), 100); v != "" {
				add(pair.labelEn+": "+v, pair.labelRu+": "+v)
			}
		}
	case "admin_not_deliver":
		add("Mark game as not delivered", "Отметить игру как несостоявшуюся")
	case "admin_add_correction":
		add(fmt.Sprintf("Team %s: %s %s", getAnyString(args["team"]), getAnyString(args["type"]), getAnyString(args["time"])),
			fmt.Sprintf("Команда %s: %s %s", getAnyString(args["team"]), getAnyString(args["type"]), getAnyString(args["time"])))
	case "admin_delete_correction":
		if id := getAnyString(args["correction_id"]); id != "" {
			add("Correction ID: "+id, "Коррекция ID: "+id)
		}
	case "send_code":
		if c := getAnyString(args["code"]); c != "" {
			add("Code: "+c, "Код: "+c)
		}
	case "enter":
		add("Submit application to join the game", "Подать заявку на участие в игре")
	case "login":
		if l := getAnyString(args["login"]); l != "" {
			add("Login as: "+l, "Войти как: "+l)
		}
	case "logout":
		add("Clear saved session", "Очистить сохранённую сессию")
	case "hint":
		if id := getAnyInt(args["hint_id"]); id > 0 {
			add(fmt.Sprintf("Request penalty hint #%d", id), fmt.Sprintf("Запросить штрафную подсказку #%d", id))
		}
	}

	if len(lines) == 0 {
		if argsJSON != "" && argsJSON != "{}" {
			add("Arguments: "+truncateDisplay(argsJSON, 200), "Аргументы: "+truncateDisplay(argsJSON, 200))
		}
	}
	return lines
}

func getAnyStringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok && s != "" {
			out = append(out, decodeBareUnicodeEscapes(s))
		}
	}
	return out
}

func truncateDisplay(s string, max int) string {
	s = strings.TrimSpace(s)
	if s == "" || max <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}
