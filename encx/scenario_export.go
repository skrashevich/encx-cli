package encx

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/skrashevich/encx-cli/encx/enapi"
	"github.com/skrashevich/encx-cli/encx/scenario"
)

// GetGameScenario reads the author's scenario export of a game.
//
// The two engines publish it differently — the legacy one renders
// GameScenario.aspx as HTML, the new one answers with a structured document —
// so this method returns the parsed model rather than a page. Callers that need
// the legacy HTML itself keep using GetGameScenarioHTML.
func (c *Client) GetGameScenario(ctx context.Context, gameId int) (*scenario.Document, error) {
	return c.engine(ctx).GetGameScenario(ctx, gameId)
}

// GetGameScenarioHTML reads the GameScenario.aspx export page.
//
// It exists only on the legacy engine; the new backend publishes no HTML
// scenario, so prefer GetGameScenario, which works on both.
func (c *Client) GetGameScenarioHTML(ctx context.Context, gameId int) (string, error) {
	return c.engine(ctx).GetGameScenarioHTML(ctx, gameId)
}

func (e *legacyEngine) GetGameScenarioHTML(ctx context.Context, gameId int) (string, error) {
	return e.c.legacyGetGameScenarioHTML(ctx, gameId)
}

func (e *legacyEngine) GetGameScenario(ctx context.Context, gameId int) (*scenario.Document, error) {
	body, err := e.c.legacyGetGameScenarioHTML(ctx, gameId)
	if err != nil {
		return nil, err
	}
	return scenario.ParseString(body, fmt.Sprintf("GameScenario.aspx?gid=%d", gameId))
}

func (e *newEngine) GetGameScenarioHTML(ctx context.Context, gameId int) (string, error) {
	return "", fmt.Errorf(
		"encx: the new engine publishes no HTML scenario export; use GetGameScenario")
}

func (e *newEngine) GetGameScenario(ctx context.Context, gameId int) (*scenario.Document, error) {
	var export enapi.GameScenario
	path := fmt.Sprintf("/games/%d/scenario", gameId)
	q := url.Values{"lang": {e.c.lang}}
	if err := e.c.api().GetJSON(ctx, path, q, &export); err != nil {
		return nil, err
	}
	if export.Error != nil && strings.TrimSpace(export.Error.Message) != "" {
		return nil, fmt.Errorf("encx: game scenario %d: %s", gameId, export.Error.Message)
	}
	return scenarioDocumentFromAPI(gameId, &export), nil
}

// scenarioDocumentFromAPI maps the structured export onto the document the
// legacy HTML parser produces, so both engines feed the same importer.
func scenarioDocumentFromAPI(gameID int, export *enapi.GameScenario) *scenario.Document {
	doc := &scenario.Document{
		SourcePath: fmt.Sprintf("api:/games/%d/scenario", gameID),
		GameID:     gameID,
	}
	if export.Game != nil {
		if export.Game.ID > 0 {
			doc.GameID = export.Game.ID
		}
		doc.GameNum = export.Game.GameNum
		doc.GameTitle = export.Game.Title
	}

	doc.Levels = make([]scenario.Level, 0, len(export.Levels))
	for _, level := range export.Levels {
		doc.Levels = append(doc.Levels, scenarioLevelFromAPI(level))
	}
	return doc
}

func scenarioLevelFromAPI(level enapi.LevelScenario) scenario.Level {
	out := scenario.Level{
		Number:  level.LevelNumber,
		Name:    firstNonEmpty(level.LevelName, level.Title),
		Comment: level.Comment,
	}
	out.AutopassSecond, out.AutopassPenaltySecond = scenarioAutopassFromText(level.AutopassText)
	out.RequiredSectorsCount = scenarioRequiredSectors(level.SectorsCompletionRule)

	for _, task := range level.Tasks {
		if text := strings.TrimSpace(task.TaskText); text != "" {
			out.Tasks = append(out.Tasks, task.TaskText)
		}
	}
	for _, help := range level.Helps {
		out.Hints = append(out.Hints, scenario.Hint{
			Title:        help.Title,
			Text:         help.HelpText,
			DelaySeconds: help.Timeout,
		})
	}
	for _, help := range level.PenaltyHelps {
		out.PenaltyHints = append(out.PenaltyHints, scenario.PenaltyHint{
			Title:          help.Title,
			Text:           help.HelpText,
			DelaySeconds:   help.Timeout,
			PenaltySeconds: help.PenaltyTime,
			RequestConfirm: help.RequestPenaltyConfirm,
			Comment:        help.PenaltyComment,
		})
	}

	// SectorAnswers mirrors Sectors position by position, which is the shape
	// the importer compares against.
	//
	// A level with no sectors publishes its answers in the flat list instead —
	// that is the single implicit sector — so they are collected there rather
	// than dropped.
	if len(level.Sectors) == 0 && len(level.Answers) > 0 {
		answers := scenarioAnswerTexts(level.Answers)
		if len(answers) > 0 {
			out.Sectors = append(out.Sectors, scenario.Sector{Answers: answers})
			out.SectorAnswers = append(out.SectorAnswers, answers)
		}
	}
	for _, sector := range level.Sectors {
		name := firstNonEmpty(sector.DisplayName, sector.SectorName)
		answers := scenarioAnswerTexts(sector.Answers)
		out.Sectors = append(out.Sectors, scenario.Sector{Name: name, Answers: answers})
		out.SectorAnswers = append(out.SectorAnswers, answers)
	}

	for _, bonus := range level.Bonuses {
		out.Bonuses = append(out.Bonuses, scenario.Bonus{
			Number:       len(out.Bonuses) + 1,
			Name:         firstNonEmpty(bonus.BonusName, bonus.Title),
			Task:         bonus.Task,
			Hint:         bonus.BonusHelp,
			Answers:      append([]string(nil), bonus.Answers...),
			AwardSeconds: bonus.BonusTime,
		})
	}
	return out
}

// scenarioAnswerTexts keeps the answers that carry text.
func scenarioAnswerTexts(answers []enapi.LevelAnswerScenario) []string {
	out := make([]string, 0, len(answers))
	for _, answer := range answers {
		if strings.TrimSpace(answer.AnswerText) != "" {
			out = append(out, answer.AnswerText)
		}
	}
	return out
}

// scenarioAutopassFromText reads the autopass line the export renders for
// humans ("через 30 минут, 5 минут штрафа"), which is the same wording the
// legacy page carried.
func scenarioAutopassFromText(text string) (autopass, penalty int) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return 0, 0
	}
	parts := strings.SplitN(trimmed, ",", 2)
	autopass = scenario.ParseRuDuration(parts[0])
	if len(parts) == 2 {
		penalty = scenario.ParseRuDuration(parts[1])
	}
	return autopass, penalty
}

// scenarioRequiredSectors reads the completion rule; "все" means every sector,
// which the document records as zero.
func scenarioRequiredSectors(rule string) int {
	lower := strings.ToLower(strings.TrimSpace(rule))
	if lower == "" || strings.Contains(lower, "все") || strings.Contains(lower, "all") {
		return 0
	}
	digits := strings.Builder{}
	for _, r := range lower {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		} else if digits.Len() > 0 {
			break
		}
	}
	return atoiOrZero(digits.String())
}
