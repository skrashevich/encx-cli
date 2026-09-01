package main

import (
	"fmt"
	"html"
	"strconv"
	"strings"
	"time"

	"github.com/skrashevich/encx-cli/encx"
)

const (
	mockLevelCount      = 3
	mockSectorsPerLevel = 4
	mockLevelBaseID     = 1400000
	mockSectorBaseID    = 3010000
	mockBonusBaseID     = 5000000
)

var legacyLevelCodes = []string{"CODE-1", "CODE-2", "CODE-3"}

func expectedSectorCode(levelIdx, sectorOrder int) string {
	return strconv.Itoa(levelIdx*mockSectorsPerLevel + sectorOrder)
}

func (s *server) buildGameModelFromFixtures(st *sessionState, now time.Time) (map[string]any, error) {
	model, err := cloneJSONObject(s.fixtures.gameModelTemplate)
	if err != nil {
		return nil, err
	}

	model["Login"] = st.Login
	model["UserId"] = 101
	model["TeamId"] = mockTeamID
	model["TeamName"] = "MockTeam"
	model["GameId"] = mockGameID
	model["GameNumber"] = 1
	model["GameTitle"] = "Mock Game 2026"
	model["GameDateTimeStart"] = now.Format("2006-01-02 15:04:05")
	if st.Completed {
		model["Event"] = encx.EventGameFinished
	} else {
		model["Event"] = encx.EventGameNormal
	}

	idx := st.CurrentIdx
	if idx >= s.levelCount() {
		idx = s.levelCount() - 1
	}
	levelID := mockLevelBaseID + idx + 1

	if s.fixtures.profile != nil {
		levels := make([]any, s.levelCount())
		for i := range levels {
			topology, _ := s.profileLevel(i)
			levels[i] = map[string]any{"LevelId": mockLevelBaseID + i + 1, "LevelNumber": i + 1, "LevelName": fmt.Sprintf("Level %d", i+1), "Dismissed": topology.Dismissed, "IsPassed": st.Passed[i], "Task": nil, "LevelAction": nil}
		}
		model["Levels"] = levels
	} else if levels, ok := model["Levels"].([]any); ok {
		for i, item := range levels {
			levelSummary, ok := item.(map[string]any)
			if !ok {
				continue
			}
			levelSummary["LevelId"] = mockLevelBaseID + i + 1
			levelSummary["LevelNumber"] = i + 1
			if i < len(st.Passed) {
				levelSummary["IsPassed"] = st.Passed[i]
			}
		}
	}

	level, ok := model["Level"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("game model template missing Level")
	}
	level["LevelId"] = levelID
	level["Number"] = idx + 1
	if st.Completed {
		level["Name"] = "Финиш — игра завершена"
	} else {
		level["Name"] = ""
	}

	passedSectors := 0
	if idx < len(st.SectorPassed) {
		passedSectors = countTrue(st.SectorPassed[idx])
	}
	required := s.requiredSectorCount(idx)
	level["RequiredSectorsCount"] = required
	level["PassedSectorsCount"] = passedSectors
	level["SectorsLeftToClose"] = required - passedSectors
	level["IsPassed"] = idx < len(st.Passed) && st.Passed[idx]

	if s.fixtures.profile != nil {
		sectors := make([]any, s.sectorCount(idx))
		for i := range sectors {
			sectors[i] = map[string]any{"SectorId": mockSectorBaseID + idx*100 + i + 1, "Order": i + 1, "Name": fmt.Sprintf("Sector %d", i+1), "IsAnswered": false, "Answer": nil}
		}
		level["Sectors"] = sectors
		bonuses := make([]any, 0)
		if topology, ok := s.profileLevel(idx); ok {
			for i := 0; i < topology.BonusCount; i++ {
				bonuses = append(bonuses, map[string]any{"BonusId": mockBonusBaseID + idx*1000 + i + 1, "Number": i + 1, "Name": "", "Task": nil, "Help": nil, "IsAnswered": false, "Answer": nil})
			}
		}
		level["Bonuses"] = bonuses
	}
	if sectors, ok := level["Sectors"].([]any); ok {
		for i, item := range sectors {
			sector, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if s.fixtures.profile != nil {
				sector["SectorId"] = mockSectorBaseID + idx*100 + i + 1
			} else {
				sector["SectorId"] = mockSectorBaseID + idx*mockSectorsPerLevel + i + 1
			}
			sector["Order"] = i + 1
			sector["Name"] = fmt.Sprintf("Сектор %d", i+1)
			answered := idx < len(st.SectorPassed) && i < len(st.SectorPassed[idx]) && st.SectorPassed[idx][i]
			sector["IsAnswered"] = answered
			if answered {
				code := expectedSectorCode(idx, i+1)
				if idx < len(st.SectorAnswers) && i < len(st.SectorAnswers[idx]) && st.SectorAnswers[idx][i] != "" {
					code = st.SectorAnswers[idx][i]
				}
				sector["Answer"] = sectorAnswerObject(st.Login, code, actionTime(st, idx, code, now))
			} else {
				sector["Answer"] = nil
			}
		}
	}

	if bonuses, ok := level["Bonuses"].([]any); ok {
		for _, item := range bonuses {
			bonus, ok := item.(map[string]any)
			if !ok {
				continue
			}
			id, ok := valueInt(bonus["BonusId"])
			if !ok {
				continue
			}
			if st.AnsweredBonuses[id] {
				bonus["IsAnswered"] = true
				if answer, ok := st.BonusAnswers[id]; ok {
					bonus["Answer"] = sectorAnswerObject(st.Login, answer, actionTime(st, idx, answer, now))
				}
			} else {
				bonus["IsAnswered"] = false
				bonus["Answer"] = nil
			}
		}
	}

	level["MixedActions"] = codeActionsToMaps(actionsForLevel(st.Actions, idx), now)

	if st.LastAction != nil {
		model["EngineAction"] = engineActionToMap(st.LastAction, mockGameID)
	} else {
		model["EngineAction"] = idleEngineAction(mockGameID)
	}

	return model, nil
}

func (s *server) processFixtureAnswer(st *sessionState, answer string) bool {
	return s.processFixtureLevelAnswer(st, answer) || s.processFixtureBonusAnswer(st, answer)
}

func (s *server) processFixtureLevelAnswer(st *sessionState, answer string) bool {
	normalized := strings.TrimSpace(answer)
	if normalized == "" {
		return false
	}

	idx := st.CurrentIdx
	if idx >= s.levelCount() {
		return false
	}
	if s.fixtures.profile != nil && strings.HasPrefix(strings.ToLower(normalized), "sector-") {
		n, err := strconv.Atoi(strings.TrimPrefix(strings.ToLower(normalized), "sector-"))
		if err == nil && n >= 0 && n < len(st.SectorPassed[idx]) && !st.SectorPassed[idx][n] {
			return s.markSectorAnswer(st, idx, n, answer, 1)
		}
	}

	for i, code := range legacyLevelCodes {
		if strings.EqualFold(normalized, code) && idx == i {
			return s.completeLevel(st, idx, answer, 1)
		}
	}

	if idx < len(st.SectorPassed) {
		for sectorIdx := range st.SectorPassed[idx] {
			if st.SectorPassed[idx][sectorIdx] {
				continue
			}
			if strings.EqualFold(normalized, expectedSectorCode(idx, sectorIdx+1)) {
				return s.markSectorAnswer(st, idx, sectorIdx, answer, 1)
			}
		}
	}

	return s.recordFixtureIncorrectAction(st, answer, 1)
}

func (s *server) processFixtureBonusAnswer(st *sessionState, answer string) bool {
	normalized := strings.TrimSpace(answer)
	if normalized == "" || st.CurrentIdx >= s.levelCount() {
		return false
	}
	idx := st.CurrentIdx
	if s.fixtures.profile != nil {
		if topology, ok := s.profileLevel(idx); ok {
			for i := 0; i < topology.BonusCount; i++ {
				id := mockBonusBaseID + idx*1000 + i + 1
				if !st.AnsweredBonuses[id] && strings.EqualFold(normalized, fmt.Sprintf("bonus-%d", i+1)) {
					return s.markBonusAnswer(st, idx, id, answer)
				}
			}
		}
	}
	if level, ok := s.fixtures.gameModelTemplate["Level"].(map[string]any); ok {
		if bonuses, ok := level["Bonuses"].([]any); ok {
			for _, item := range bonuses {
				bonus, ok := item.(map[string]any)
				if !ok {
					continue
				}
				id, ok := valueInt(bonus["BonusId"])
				if !ok {
					continue
				}
				task, _ := bonus["Task"].(string)
				if !st.AnsweredBonuses[id] && task != "" && strings.EqualFold(normalized, task) {
					return s.markBonusAnswer(st, idx, id, answer)
				}
			}
		}
	}
	return s.recordFixtureIncorrectAction(st, answer, 2)
}

func (s *server) recordFixtureIncorrectAction(st *sessionState, answer string, kind int) bool {
	idx := st.CurrentIdx
	st.Actions = append(st.Actions, encx.CodeAction{
		ActionId:      len(st.Actions) + 1,
		LevelId:       mockLevelBaseID + idx + 1,
		LevelNumber:   idx + 1,
		UserId:        101,
		Kind:          kind,
		Login:         st.Login,
		Answer:        answer,
		EnterDateTime: dt(time.Now()),
		LocDateTime:   time.Now().Format("02.01 15:04:05"),
		IsCorrect:     false,
	})
	st.LastAction = newEngineAction(st, idx, answer, false, kind)
	return false
}

func (s *server) markSectorAnswer(st *sessionState, levelIdx, sectorIdx int, answer string, kind int) bool {
	st.SectorPassed[levelIdx][sectorIdx] = true
	recordSectorAnswer(st, levelIdx, sectorIdx, answer)
	appendCodeAction(st, levelIdx, answer, true, kind)
	st.LastAction = newEngineAction(st, levelIdx, answer, true, kind)
	if countTrue(st.SectorPassed[levelIdx]) >= s.requiredSectorCount(levelIdx) {
		st.Passed[levelIdx] = true
		if levelIdx < s.levelCount()-1 {
			st.CurrentIdx = levelIdx + 1
			s.ensureLevelStarted(st, st.CurrentIdx, time.Now())
		} else {
			st.Completed = true
		}
	}
	return true
}

func recordSectorAnswer(st *sessionState, levelIdx, sectorIdx int, answer string) {
	for len(st.SectorAnswers) <= levelIdx {
		st.SectorAnswers = append(st.SectorAnswers, nil)
	}
	if len(st.SectorAnswers[levelIdx]) < len(st.SectorPassed[levelIdx]) {
		st.SectorAnswers[levelIdx] = make([]string, len(st.SectorPassed[levelIdx]))
	}
	st.SectorAnswers[levelIdx][sectorIdx] = answer
}

func (s *server) completeLevel(st *sessionState, levelIdx int, answer string, kind int) bool {
	for i := range st.SectorPassed[levelIdx] {
		st.SectorPassed[levelIdx][i] = true
	}
	st.Passed[levelIdx] = true
	appendCodeAction(st, levelIdx, answer, true, kind)
	st.LastAction = newEngineAction(st, levelIdx, answer, true, kind)
	if levelIdx < s.levelCount()-1 {
		st.CurrentIdx = levelIdx + 1
		s.ensureLevelStarted(st, st.CurrentIdx, time.Now())
	} else {
		st.Completed = true
	}
	return true
}

func (s *server) markBonusAnswer(st *sessionState, levelIdx, bonusID int, answer string) bool {
	st.AnsweredBonuses[bonusID] = true
	st.BonusAnswers[bonusID] = answer
	appendCodeAction(st, levelIdx, answer, true, 2)
	st.LastAction = newEngineAction(st, levelIdx, answer, true, 2)
	if s.scenario == nil && s.fixtures != nil && s.fixtures.profile != nil {
		st.LastAction.BonusAction.IsCorrectAnswer = nil
	}
	return true
}

func appendCodeAction(st *sessionState, levelIdx int, answer string, correct bool, kind int) {
	levelNumber := levelIdx + 1
	if kind == 2 {
		levelNumber = 0
	}
	st.Actions = append(st.Actions, encx.CodeAction{
		ActionId:      len(st.Actions) + 1,
		LevelId:       mockLevelBaseID + levelIdx + 1,
		LevelNumber:   levelNumber,
		UserId:        101,
		Kind:          kind,
		Login:         st.Login,
		Answer:        answer,
		EnterDateTime: dt(time.Now()),
		LocDateTime:   time.Now().Format("02.01 15:04:05"),
		IsCorrect:     correct,
	})
}

func newEngineAction(st *sessionState, levelIdx int, answer string, correct bool, kind int) *encx.EngineAction {
	levelNumber := levelIdx + 1
	levelID := mockLevelBaseID + levelIdx + 1
	action := &encx.EngineAction{
		LevelNumber: levelNumber,
		LevelAction: &encx.ActionResult{
			Answer:          new(answer),
			IsCorrectAnswer: new(correct),
		},
		BonusAction: &encx.ActionResult{
			Answer:          nil,
			IsCorrectAnswer: nil,
		},
		PenaltyAction: &encx.PenaltyActionResult{
			PenaltyId:  0,
			ActionType: 0,
		},
		GameId:  mockGameID,
		LevelId: levelID,
	}
	if kind == 2 {
		action.LevelNumber = 0
		action.LevelAction = &encx.ActionResult{Answer: nil, IsCorrectAnswer: nil}
		action.BonusAction = &encx.ActionResult{
			Answer:          new(answer),
			IsCorrectAnswer: new(correct),
		}
	}
	return action
}

func idleEngineAction(gameID int) map[string]any {
	return map[string]any{
		"LevelNumber": 0,
		"LevelAction": map[string]any{"Answer": nil, "IsCorrectAnswer": nil},
		"BonusAction": map[string]any{"Answer": nil, "IsCorrectAnswer": nil},
		"PenaltyAction": map[string]any{
			"PenaltyId":  0,
			"ActionType": 0,
		},
		"GameId":  gameID,
		"LevelId": 0,
	}
}

func engineActionToMap(action *encx.EngineAction, gameID int) map[string]any {
	if action == nil {
		return idleEngineAction(gameID)
	}
	levelAction := map[string]any{"Answer": nil, "IsCorrectAnswer": nil}
	bonusAction := map[string]any{"Answer": nil, "IsCorrectAnswer": nil}
	if action.LevelAction != nil {
		levelAction["Answer"] = action.LevelAction.Answer
		levelAction["IsCorrectAnswer"] = action.LevelAction.IsCorrectAnswer
	}
	if action.BonusAction != nil {
		bonusAction["Answer"] = action.BonusAction.Answer
		bonusAction["IsCorrectAnswer"] = action.BonusAction.IsCorrectAnswer
	}
	penaltyID := 0
	penaltyType := 0
	if action.PenaltyAction != nil {
		penaltyID = action.PenaltyAction.PenaltyId
		penaltyType = action.PenaltyAction.ActionType
	}
	return map[string]any{
		"LevelNumber":   action.LevelNumber,
		"LevelAction":   levelAction,
		"BonusAction":   bonusAction,
		"PenaltyAction": map[string]any{"PenaltyId": penaltyID, "ActionType": penaltyType},
		"GameId":        gameID,
		"LevelId":       action.LevelId,
	}
}

func codeActionsToMaps(actions []encx.CodeAction, now time.Time) []map[string]any {
	out := make([]map[string]any, len(actions))
	for i, action := range actions {
		out[i] = map[string]any{
			"ActionId":      action.ActionId,
			"LevelId":       action.LevelId,
			"LevelNumber":   action.LevelNumber,
			"UserId":        action.UserId,
			"Kind":          action.Kind,
			"Login":         action.Login,
			"Answer":        action.Answer,
			"AnswForm":      action.AnswForm,
			"EnterDateTime": dt(actionTimeFromCodeAction(action, now)),
			"LocDateTime":   action.LocDateTime,
			"IsCorrect":     action.IsCorrect,
			"Award":         action.Award,
			"LocAward":      action.LocAward,
			"Penalty":       action.Penalty,
			"Negative":      action.Negative,
		}
	}
	return out
}

func actionTimeFromCodeAction(action encx.CodeAction, fallback time.Time) time.Time {
	if action.EnterDateTime != nil && action.EnterDateTime.Timestamp != 0 {
		return time.Unix(action.EnterDateTime.Timestamp, 0)
	}
	if t, err := time.Parse("02.01 15:04:05", action.LocDateTime); err == nil {
		return time.Date(fallback.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, fallback.Location())
	}
	return fallback
}

func actionTime(st *sessionState, levelIdx int, answer string, fallback time.Time) time.Time {
	for _, action := range st.Actions {
		if action.LevelId == mockLevelBaseID+levelIdx+1 && action.Answer == answer {
			return actionTimeFromCodeAction(action, fallback)
		}
	}
	return fallback
}

func valueInt(value any) (int, bool) {
	switch value := value.(type) {
	case int:
		return value, true
	case float64:
		return int(value), true
	default:
		return 0, false
	}
}

func actionsForLevel(actions []encx.CodeAction, levelIdx int) []encx.CodeAction {
	id := mockLevelBaseID + levelIdx + 1
	out := make([]encx.CodeAction, 0, len(actions))
	for _, action := range actions {
		if action.LevelId == id {
			out = append(out, action)
		}
	}
	return out
}

func sectorAnswerObject(login, code string, now time.Time) map[string]any {
	return map[string]any{
		"Answer":         code,
		"AnswerDateTime": dt(now),
		"Login":          login,
		"UserId":         101,
		"LocDateTime":    nil,
	}
}

func countTrue(values []bool) int {
	n := 0
	for _, v := range values {
		if v {
			n++
		}
	}
	return n
}

func (s *server) buildGameInfoResponse(st *sessionState, now time.Time) (map[string]any, error) {
	info, err := cloneJSONObject(s.fixtures.gameInfoTemplate)
	if err != nil {
		return nil, err
	}
	info["GameID"] = mockGameID
	info["Title"] = s.gameTitle()
	info["LevelNumber"] = s.levelCount()
	info["Started"] = true
	info["Finished"] = st.Completed
	info["InProgress"] = !st.Completed
	info["TSRemain"] = durationMap(3 * time.Hour)
	info["StartDateTime"] = dt(now.Add(-1 * time.Hour))
	info["FinishDateTime"] = dt(now.Add(3 * time.Hour))
	info["CreateDateTime"] = dt(now.Add(-24 * time.Hour))
	return info, nil
}

func durationMap(d time.Duration) map[string]any {
	totalSeconds := d.Seconds()
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	sec := int(d.Seconds()) % 60
	return map[string]any{
		"Days":         h / 24,
		"Hours":        h % 24,
		"Minutes":      m,
		"Seconds":      sec,
		"TotalSeconds": totalSeconds,
	}
}

func (s *server) renderUserDetails(login string) string {
	return strings.ReplaceAll(s.fixtures.userDetailsHTML, "{{LOGIN}}", html.EscapeString(login))
}
