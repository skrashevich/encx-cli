package main

import (
	"encoding/json"
	"github.com/skrashevich/encx-cli/encx"
)

func prepareToolResultForLLM(name, result string) string {
	if result == "" {
		return result
	}
	if name == "admin_level_content" {
		if len(result) <= 20000 {
			return result
		}
		if summarized, ok := summarizeGenericJSON(result); ok {
			return summarized
		}
		return summarizeDebugText(result, 20000)
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
