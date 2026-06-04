package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/skrashevich/encx-cli/encx"
	"github.com/skrashevich/encx-cli/encx/scenario"
)

func resolveScenarioPath(flagPath string) string {
	if p := strings.TrimSpace(flagPath); p != "" {
		return p
	}
	return strings.TrimSpace(os.Getenv("ENCX_MOCK_SCENARIO"))
}

func loadScenario(path string) (*scenario.Document, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil
	}
	doc, err := scenario.ParseFile(path)
	if err != nil {
		return nil, fmt.Errorf("scenario %q: %w", path, err)
	}
	if len(doc.Levels) == 0 {
		return nil, fmt.Errorf("scenario %q: no levels found", path)
	}
	return doc, nil
}

func (s *server) levelCount() int {
	if s.scenario != nil {
		return len(s.scenario.Levels)
	}
	return mockLevelCount
}

func (s *server) gameTitle() string {
	if s.scenario != nil && strings.TrimSpace(s.scenario.GameTitle) != "" {
		return s.scenario.GameTitle
	}
	return "Mock Game 2026"
}

func (s *server) levelName(idx int) string {
	if s.scenario != nil && idx >= 0 && idx < len(s.scenario.Levels) {
		return s.scenario.Levels[idx].Name
	}
	return fmt.Sprintf("Mock level %d", idx+1)
}

func (s *server) newSessionSectorState() [][]bool {
	n := s.levelCount()
	sectors := make([][]bool, n)
	for i := 0; i < n; i++ {
		sectors[i] = make([]bool, s.sectorCount(i))
	}
	return sectors
}

func (s *server) newSessionSectorAnswerState() [][]string {
	n := s.levelCount()
	answers := make([][]string, n)
	for i := 0; i < n; i++ {
		answers[i] = make([]string, s.sectorCount(i))
	}
	return answers
}

func (s *server) sectorCount(levelIdx int) int {
	if s.scenario != nil && levelIdx >= 0 && levelIdx < len(s.scenario.Levels) {
		return s.scenario.Levels[levelIdx].SectorCount()
	}
	return mockSectorsPerLevel
}

func (s *server) newSessionPassedState() []bool {
	return make([]bool, s.levelCount())
}

func (s *server) buildGameModelResponse(st *sessionState, now time.Time) (map[string]any, error) {
	if s.scenario != nil {
		return s.buildGameModelFromScenario(st, now)
	}
	return s.buildGameModelFromFixtures(st, now)
}

func (s *server) buildGameModelFromScenario(st *sessionState, now time.Time) (map[string]any, error) {
	doc := s.scenario
	idx := st.CurrentIdx
	if idx >= len(doc.Levels) {
		idx = len(doc.Levels) - 1
	}
	lvl := doc.Levels[idx]
	levelID := mockLevelBaseID + idx + 1

	levelSummaries := make([]map[string]any, len(doc.Levels))
	for i, summary := range doc.Levels {
		levelSummaries[i] = map[string]any{
			"LevelId":     mockLevelBaseID + i + 1,
			"LevelNumber": i + 1,
			"LevelName":   summary.Name,
			"Dismissed":   false,
			"IsPassed":    i < len(st.Passed) && st.Passed[i],
			"Task":        nil,
			"LevelAction": nil,
		}
	}

	tasks := make([]map[string]any, 0, len(lvl.Tasks))
	for _, taskText := range lvl.Tasks {
		tasks = append(tasks, map[string]any{
			"TaskText":          taskText,
			"TaskTextFormatted": taskText,
			"ReplaceNlToBr":     !strings.Contains(taskText, "<"),
		})
	}
	var taskField any
	if len(tasks) > 0 {
		taskField = tasks[0]
	}

	helps := scenarioHelps(st, idx, lvl, now)

	sectorCount := lvl.SectorCount()
	passedSectors := 0
	if idx < len(st.SectorPassed) {
		passedSectors = countTrue(st.SectorPassed[idx])
	}
	sectors := make([]map[string]any, sectorCount)
	for i := 0; i < sectorCount; i++ {
		answered := idx < len(st.SectorPassed) && i < len(st.SectorPassed[idx]) && st.SectorPassed[idx][i]
		sector := map[string]any{
			"SectorId":   mockSectorBaseID + idx*100 + i + 1,
			"Order":      i + 1,
			"Name":       fmt.Sprintf("Сектор %d", i+1),
			"IsAnswered": answered,
			"Answer":     nil,
		}
		if answered {
			code := submittedSectorAnswer(st, idx, i, lvl)
			sector["Answer"] = sectorAnswerObject(st.Login, code, now)
		}
		sectors[i] = sector
	}

	levelName := lvl.Name
	if st.Completed {
		levelName = "Финиш — игра завершена"
	}

	started := time.Time{}
	if idx < len(st.LevelStartedAt) {
		started = st.LevelStartedAt[idx]
	}
	if started.IsZero() {
		started = now
	}
	timeout, timeoutRemain, timeoutAward := scenarioLevelTimeout(lvl, started, now)

	levelObj := map[string]any{
		"LevelId":              levelID,
		"Number":               idx + 1,
		"Name":                 levelName,
		"Timeout":              timeout,
		"TimeoutSecondsRemain": timeoutRemain,
		"TimeoutAward":         timeoutAward,
		"IsPassed":             idx < len(st.Passed) && st.Passed[idx],
		"Dismissed":            false,
		"StartTime":            dt(started),
		"HasAnswerBlockRule":   false,
		"BlockDuration":        0,
		"BlockTargetId":        0,
		"AttemtsNumber":        0,
		"AttemtsPeriod":        0,
		"RequiredSectorsCount": sectorCount,
		"PassedSectorsCount":   passedSectors,
		"PassedBonusesCount":   passedBonusesCount(st, idx, lvl),
		"SectorsLeftToClose":   sectorCount - passedSectors,
		"Tasks":                tasks,
		"Task":                 taskField,
		"Messages":             []any{},
		"Sectors":              sectors,
		"Helps":                helps,
		"Bonuses":              scenarioBonuses(st, idx, lvl, now),
		"PenaltyHelps":         []any{},
		"MixedActions":         codeActionsToMaps(st.Actions, now),
	}

	model := map[string]any{
		"Level":             levelObj,
		"Levels":            levelSummaries,
		"GameId":            mockGameID,
		"GameTypeId":        1,
		"GameZoneId":        0,
		"GameNumber":        doc.GameNum,
		"GameTitle":         s.gameTitle(),
		"GameDateTimeStart": now.Format("2006-01-02 15:04:05"),
		"LevelSequence":     0,
		"UserId":            101,
		"Login":             st.Login,
		"TeamId":            mockTeamID,
		"TeamName":          "MockTeam",
		"IsCaptain":         true,
		"MixedActions":      nil,
	}
	if st.Completed {
		model["Event"] = 100
	} else {
		model["Event"] = 0
	}
	if st.LastAction != nil {
		model["EngineAction"] = engineActionToMap(st.LastAction, mockGameID)
	} else {
		model["EngineAction"] = idleEngineAction(mockGameID)
	}
	return model, nil
}

func scenarioBonusID(levelIdx, bonusNumber int) int {
	return mockBonusBaseID + levelIdx*1000 + bonusNumber
}

func passedBonusesCount(st *sessionState, levelIdx int, lvl scenario.Level) int {
	count := 0
	for _, bonus := range lvl.Bonuses {
		if st.AnsweredBonuses[scenarioBonusID(levelIdx, bonus.Number)] {
			count++
		}
	}
	return count
}

func scenarioBonuses(st *sessionState, levelIdx int, lvl scenario.Level, now time.Time) []any {
	bonuses := make([]any, 0, len(lvl.Bonuses))
	for _, bonus := range lvl.Bonuses {
		bonusID := scenarioBonusID(levelIdx, bonus.Number)
		answered := st.AnsweredBonuses[bonusID]
		item := map[string]any{
			"BonusId":        bonusID,
			"Name":           bonus.Name,
			"Number":         bonus.Number,
			"Task":           bonus.Task,
			"Help":           nil,
			"IsAnswered":     answered,
			"Expired":        false,
			"SecondsToStart": 0,
			"SecondsLeft":    0,
			"AwardTime":      bonus.AwardSeconds,
			"Negative":       false,
			"Answer":         nil,
		}
		if answered {
			if answer, ok := st.BonusAnswers[bonusID]; ok {
				item["Answer"] = sectorAnswerObject(st.Login, answer, now)
			}
		}
		bonuses = append(bonuses, item)
	}
	return bonuses
}

func submittedSectorAnswer(st *sessionState, levelIdx, sectorIdx int, lvl scenario.Level) string {
	if levelIdx < len(st.SectorPassed) && sectorIdx < len(st.SectorPassed[levelIdx]) && st.SectorPassed[levelIdx][sectorIdx] {
		if levelIdx < len(st.SectorAnswers) && sectorIdx < len(st.SectorAnswers[levelIdx]) && st.SectorAnswers[levelIdx][sectorIdx] != "" {
			return st.SectorAnswers[levelIdx][sectorIdx]
		}
	}
	if sectorIdx < len(lvl.SectorAnswers) && len(lvl.SectorAnswers[sectorIdx]) > 0 {
		return lvl.SectorAnswers[sectorIdx][0]
	}
	return fmt.Sprintf("%d", sectorIdx+1)
}

func (s *server) processAnswer(st *sessionState, answer string) bool {
	if s.scenario != nil {
		return s.processScenarioAnswer(st, answer)
	}
	return s.processFixtureAnswer(st, answer)
}

func (s *server) processScenarioAnswer(st *sessionState, answer string) bool {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return false
	}

	idx := st.CurrentIdx
	if idx >= len(s.scenario.Levels) {
		return false
	}
	lvl := s.scenario.Levels[idx]

	for sectorIdx := range st.SectorPassed[idx] {
		if st.SectorPassed[idx][sectorIdx] {
			continue
		}
		var accepted []string
		if sectorIdx < len(lvl.SectorAnswers) {
			accepted = lvl.SectorAnswers[sectorIdx]
		}
		if scenario.MatchAnswer(answer, accepted) {
			return s.markSectorAnswer(st, idx, sectorIdx, answer, 1)
		}
	}

	for _, bonus := range lvl.Bonuses {
		bonusID := scenarioBonusID(idx, bonus.Number)
		if st.AnsweredBonuses[bonusID] {
			continue
		}
		if scenario.MatchAnswer(answer, bonus.Answers) {
			return s.markBonusAnswer(st, idx, bonusID, answer)
		}
	}

	st.Actions = append(st.Actions, encx.CodeAction{
		ActionId:    len(st.Actions) + 1,
		LevelId:     mockLevelBaseID + idx + 1,
		LevelNumber: idx + 1,
		UserId:      101,
		Kind:        1,
		Login:       st.Login,
		Answer:      answer,
		LocDateTime: time.Now().Format("02.01 15:04:05"),
		IsCorrect:   false,
	})
	st.LastAction = newEngineAction(st, idx, answer, false, 1)
	return false
}
