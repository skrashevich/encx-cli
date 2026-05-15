package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const maxLevelCompletionNudges = 4

func resetLevelEnumeration(session *llmSession) {
	if session == nil {
		return
	}
	session.enumeratedLevelNumbers = nil
	session.levelCompletionNudges = 0
}

func markLevelContentLoaded(session *llmSession, toolName, argsJSON string) {
	if session == nil || toolName != "admin_level_content" {
		return
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return
	}
	lvl := getAnyInt(args["level_number"])
	if lvl == 0 {
		lvl = getAnyInt(args["level_id"])
	}
	if lvl <= 0 {
		return
	}
	if session.loadedLevelContent == nil {
		session.loadedLevelContent = make(map[int]struct{})
	}
	session.loadedLevelContent[lvl] = struct{}{}
}

func recordLevelEnumeration(session *llmSession, toolResult string) {
	if session == nil || toolResult == "" {
		return
	}
	var data struct {
		Count  int `json:"count"`
		Levels []struct {
			Number int `json:"number"`
		} `json:"levels"`
	}
	if err := json.Unmarshal([]byte(toolResult), &data); err != nil {
		return
	}
	nums := make([]int, 0, len(data.Levels))
	seen := make(map[int]struct{})
	for _, l := range data.Levels {
		if l.Number <= 0 {
			continue
		}
		if _, ok := seen[l.Number]; ok {
			continue
		}
		seen[l.Number] = struct{}{}
		nums = append(nums, l.Number)
	}
	if len(nums) == 0 && data.Count > 0 {
		for i := 1; i <= data.Count; i++ {
			nums = append(nums, i)
		}
	}
	sort.Ints(nums)
	session.enumeratedLevelNumbers = nums
}

func userWantsLevelContentDetail(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	keywords := []string{
		"сводк", "сценари", "содержан", "описан", "опиши", "расскаж",
		"обзор", "подробн", "детал", "что на уровн", "текст уровн",
		"задани", "квест",
		"summary", "scenario", "content", "describe", "overview", "detail",
		"audit", "review", "walkthrough",
	}
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

func missingLevelsForContentSummary(session *llmSession, lastUser string) []int {
	if session == nil || len(session.enumeratedLevelNumbers) == 0 {
		return nil
	}
	wantsDetail := userWantsLevelContentDetail(lastUser)
	startedLoading := len(session.loadedLevelContent) > 0
	if !wantsDetail && !startedLoading {
		return nil
	}
	var missing []int
	for _, num := range session.enumeratedLevelNumbers {
		if session.loadedLevelContent == nil {
			missing = append(missing, num)
			continue
		}
		if _, ok := session.loadedLevelContent[num]; !ok {
			missing = append(missing, num)
		}
	}
	return missing
}

func buildLevelLoadNudge(session *llmSession, missing []int) string {
	list := formatLevelNumberList(missing)
	if session != nil && session.preferRussian {
		return fmt.Sprintf(
			"Сначала загрузи содержимое каждого уровня через admin_level_content (ещё не загружены: %s), затем дай итоговый ответ. Не подставляй названия уровней вместо прочитанного текста заданий.",
			list,
		)
	}
	return fmt.Sprintf(
		"Load each level with admin_level_content before your final answer (still missing: %s). Do not substitute level names for unread task text.",
		list,
	)
}

func formatLevelNumberList(nums []int) string {
	parts := make([]string, len(nums))
	for i, n := range nums {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ", ")
}

func sessionLoadedLevelsBlock(session *llmSession) string {
	if session == nil {
		return ""
	}
	if len(session.enumeratedLevelNumbers) > 0 {
		missing := missingLevelsForContentSummary(session, "сводка")
		if len(missing) > 0 {
			return fmt.Sprintf(
				"\nLevel list loaded (%d levels). Content not yet loaded for levels: %s — call admin_level_content for each before answering about scenario text.\n",
				len(session.enumeratedLevelNumbers),
				formatLevelNumberList(missing),
			)
		}
	}
	if len(session.loadedLevelContent) == 0 {
		return ""
	}
	levels := loadedLevelsSlice(session.loadedLevelContent)
	parts := make([]string, len(levels))
	for i, n := range levels {
		parts[i] = strconv.Itoa(n)
	}
	return fmt.Sprintf(
		"\nLevel content loaded via admin_level_content: %s.\n",
		strings.Join(parts, ", "),
	)
}

func rebuildLoadedLevelsFromUI(msgs []UIMessage) map[int]struct{} {
	out := make(map[int]struct{})
	for _, m := range msgs {
		if m.Role != UIMessageRoleTool || m.ToolName != "admin_level_content" {
			continue
		}
		if lvl := parseLevelFromToolDisplay(m.Content); lvl > 0 {
			out[lvl] = struct{}{}
		}
	}
	return out
}

func parseLevelFromToolDisplay(content string) int {
	idx := strings.Index(content, "ур.")
	if idx < 0 {
		return 0
	}
	rest := content[idx+len("ур."):]
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0
	}
	n, _ := strconv.Atoi(rest[:end])
	return n
}

func loadedLevelsSlice(m map[int]struct{}) []int {
	if len(m) == 0 {
		return nil
	}
	out := make([]int, 0, len(m))
	for n := range m {
		out = append(out, n)
	}
	sort.Ints(out)
	return out
}

func toolResultLooksLikeError(result string) bool {
	result = strings.TrimSpace(result)
	if result == "" {
		return true
	}
	if strings.HasPrefix(result, "Error") || strings.HasPrefix(result, "error:") {
		return true
	}
	var obj map[string]any
	if json.Unmarshal([]byte(result), &obj) != nil {
		return false
	}
	if errMsg, ok := obj["error"].(string); ok && errMsg != "" {
		return true
	}
	return false
}

func applyLoadedLevels(session *llmSession, levels []int) {
	if session == nil || len(levels) == 0 {
		return
	}
	if session.loadedLevelContent == nil {
		session.loadedLevelContent = make(map[int]struct{}, len(levels))
	}
	for _, n := range levels {
		if n > 0 {
			session.loadedLevelContent[n] = struct{}{}
		}
	}
}
