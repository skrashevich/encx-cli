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
	case "send_bonus":
		code := getAnyString(args["code"])
		return format(rt("Sending bonus: ", "Отправляю бонус: ") + code)
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
	default:
		return format(fmt.Sprintf("[%s] %s", name, argsJSON))
	}
}
