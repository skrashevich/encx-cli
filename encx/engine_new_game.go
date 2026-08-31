package encx

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/skrashevich/encx-cli/encx/enapi"
)

// engineController is the ASP controller name kept by the new backend on its
// compatibility route; the legacy client uses the same one.
const engineController = "encounter"

func enginePlayPath(gameID int) string {
	return fmt.Sprintf("/gameengines/%s/play/%d", engineController, gameID)
}

// engineQuery builds the query the compatibility route requires: without
// json=1 the new backend answers 404.
func (e *newEngine) engineQuery(extra url.Values) url.Values {
	q := url.Values{}
	q.Set("json", "1")
	q.Set("lang", e.c.lang)
	for key, values := range extra {
		for _, value := range values {
			q.Add(key, value)
		}
	}
	return q
}

func (e *newEngine) GetGameModel(ctx context.Context, gameId int, formValues ...url.Values) (*GameModel, error) {
	if len(formValues) > 0 {
		msg, err := engineMessageFromForm(formValues)
		if err != nil {
			return nil, err
		}
		return e.postEngine(ctx, gameId, nil, msg)
	}
	return e.getEngine(ctx, gameId, nil)
}

func (e *newEngine) GetGameModelLevel(ctx context.Context, gameId, levelNumber int) (*GameModel, error) {
	extra := url.Values{}
	if levelNumber > 0 {
		extra.Set("level", strconv.Itoa(levelNumber))
	}
	return e.getEngine(ctx, gameId, extra)
}

func (e *newEngine) SendCode(ctx context.Context, gameId, levelId, levelNumber int, code string) (*GameModel, error) {
	return e.postEngine(ctx, gameId, nil, enapi.EngineClientMessage{
		Type:        enapi.EngineMessageAnswer,
		LevelID:     levelId,
		LevelNumber: levelNumber,
		Answer:      code,
	})
}

func (e *newEngine) SendBonusCode(ctx context.Context, gameId, levelId, levelNumber int, code string) (*GameModel, error) {
	return e.postEngine(ctx, gameId, nil, enapi.EngineClientMessage{
		Type:        enapi.EngineMessageBonus,
		LevelID:     levelId,
		LevelNumber: levelNumber,
		Answer:      code,
	})
}

func (e *newEngine) GetPenaltyHint(ctx context.Context, gameId, penaltyId int) (*GameModel, error) {
	return e.postEngine(ctx, gameId, nil, enapi.EngineClientMessage{
		Type:       enapi.EngineMessagePenalty,
		PenaltyID:  penaltyId,
		PenaltyAct: enapi.PenaltyActApprove,
	})
}

func (e *newEngine) getEngine(ctx context.Context, gameID int, extra url.Values) (*GameModel, error) {
	var state enapi.EngineState
	if err := e.c.api().GetJSON(ctx, enginePlayPath(gameID), e.engineQuery(extra), &state); err != nil {
		return nil, err
	}
	return gameModelFromEngineState(&state), nil
}

func (e *newEngine) postEngine(ctx context.Context, gameID int, extra url.Values, msg enapi.EngineClientMessage) (*GameModel, error) {
	var state enapi.EngineState
	err := e.c.api().Do(ctx, enapi.Request{
		Method: "POST",
		Path:   enginePlayPath(gameID),
		Query:  e.engineQuery(extra),
		Body:   msg,
		Out:    &state,
	})
	if err != nil {
		return nil, err
	}
	return gameModelFromEngineState(&state), nil
}

// engineMessageFromForm translates the legacy form-value contract of
// GetGameModel into an engine message. Callers that still pass raw form values
// keep working across both engines.
func engineMessageFromForm(formValues []url.Values) (enapi.EngineClientMessage, error) {
	merged := url.Values{}
	for _, fv := range formValues {
		for key, values := range fv {
			for _, value := range values {
				merged.Add(key, value)
			}
		}
	}

	msg := enapi.EngineClientMessage{Type: enapi.EngineMessageSubscribe}
	msg.LevelID = atoiOrZero(merged.Get("LevelId"))
	msg.LevelNumber = atoiOrZero(merged.Get("LevelNumber"))

	switch {
	case merged.Has("LevelAction.Answer"):
		msg.Type = enapi.EngineMessageAnswer
		msg.Answer = merged.Get("LevelAction.Answer")
	case merged.Has("BonusAction.Answer"):
		msg.Type = enapi.EngineMessageBonus
		msg.Answer = merged.Get("BonusAction.Answer")
	case merged.Has("pid"):
		msg.Type = enapi.EngineMessagePenalty
		msg.PenaltyID = atoiOrZero(merged.Get("pid"))
		msg.PenaltyAct = enapi.PenaltyActApprove
	}
	return msg, nil
}

func atoiOrZero(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

// gameModelFromEngineState maps the new engine's snake_case state onto the
// GameModel every consumer of this package already reads.
func gameModelFromEngineState(state *enapi.EngineState) *GameModel {
	if state == nil {
		return nil
	}
	model := &GameModel{
		Event:             state.Event,
		GameId:            state.GameID,
		GameNumber:        state.GameNumber,
		GameTitle:         state.GameTitle,
		GameTypeId:        state.GameTypeID,
		GameZoneId:        state.GameZoneID,
		LevelSequence:     state.LevelSequence,
		UserId:            state.UserID,
		TeamId:            state.TeamID,
		Login:             state.Login,
		TeamName:          state.TeamName,
		IsCaptain:         state.IsCaptain,
		GameDateTimeStart: gameStartFromEngine(state.GameDateTimeStart),
		Levels:            levelSummariesFromBands(state.Levels),
		Level:             levelFromEngineLevel(state.Level),
		EngineAction:      engineActionFromSnapshot(state.EngineAction),
	}
	return model
}

func levelSummariesFromBands(bands []enapi.EngineLevelBand) []LevelSummary {
	if len(bands) == 0 {
		return nil
	}
	out := make([]LevelSummary, 0, len(bands))
	for _, band := range bands {
		out = append(out, LevelSummary{
			LevelId:     band.LevelID,
			LevelNumber: band.LevelNumber,
			LevelName:   band.LevelName,
			Dismissed:   band.Dismissed,
			IsPassed:    band.IsPassed,
		})
	}
	return out
}

func levelFromEngineLevel(src *enapi.EngineCurrentLevel) *Level {
	if src == nil {
		return nil
	}
	level := &Level{
		LevelId:              src.LevelID,
		Number:               src.Number,
		Name:                 src.Name,
		Timeout:              src.Timeout,
		TimeoutSecondsRemain: src.TimeoutSecondsRemain,
		TimeoutAward:         src.TimeoutAward,
		IsPassed:             src.IsPassed,
		Dismissed:            src.Dismissed,
		StartTime:            dateTimeFromUnix(src.LevelStartUnix),
		HasAnswerBlockRule:   src.HasAnswerBlockRule,
		BlockDuration:        src.BlockDuration,
		BlockTargetId:        src.BlockTargetID,
		AttemtsNumber:        src.AttemptsNumber,
		AttemtsPeriod:        src.AttemptsPeriod,
		RequiredSectorsCount: src.RequiredSectorsCount,
		PassedSectorsCount:   src.PassedSectorsCount,
		PassedBonusesCount:   src.PassedBonusesCount,
		SectorsLeftToClose:   src.SectorsLeftToClose,
		Tasks:                levelTasksFromEngine(src.Tasks),
		Messages:             adminMessagesFromEngine(src.Messages),
		Sectors:              sectorsFromEngine(src.Sectors),
		Helps:                helpsFromEngine(src.Helps),
		PenaltyHelps:         helpsFromEngine(src.PenaltyHelps),
		Bonuses:              bonusesFromEngine(src.Bonuses),
		MixedActions:         codeActionsFromEngine(src.MixedActions),
	}
	return level
}

func levelTasksFromEngine(tasks []enapi.EngineTask) []LevelTask {
	if len(tasks) == 0 {
		return nil
	}
	out := make([]LevelTask, 0, len(tasks))
	for _, task := range tasks {
		// The new engine ships a single, already formatted text; the legacy
		// split between raw and formatted no longer exists upstream.
		out = append(out, LevelTask{
			TaskText:          task.TaskTextFormatted,
			TaskTextFormatted: task.TaskTextFormatted,
		})
	}
	return out
}

func adminMessagesFromEngine(messages []enapi.EngineLevelMessage) []AdminMessage {
	if len(messages) == 0 {
		return nil
	}
	out := make([]AdminMessage, 0, len(messages))
	for _, msg := range messages {
		out = append(out, AdminMessage{
			OwnerId:     msg.OwnerID,
			OwnerLogin:  msg.OwnerLogin,
			MessageId:   msg.MessageID,
			MessageText: msg.WrappedText,
			WrappedText: msg.WrappedText,
		})
	}
	return out
}

func sectorsFromEngine(sectors []enapi.EngineSector) []Sector {
	if len(sectors) == 0 {
		return nil
	}
	out := make([]Sector, 0, len(sectors))
	for _, sector := range sectors {
		out = append(out, Sector{
			SectorId:   sector.SectorID,
			Order:      sector.Order,
			Name:       sector.Name,
			IsAnswered: sector.IsPassed,
			Answer:     FlexString(sector.AnswerText),
		})
	}
	return out
}

func helpsFromEngine(helps []enapi.EngineHelp) []Help {
	if len(helps) == 0 {
		return nil
	}
	out := make([]Help, 0, len(helps))
	for _, help := range helps {
		item := Help{
			HelpId:           help.HelpID,
			Number:           help.HelpNumber,
			IsPenalty:        help.IsPenalty,
			Penalty:          help.PenaltyTime,
			RequestConfirm:   help.RequestConfirm,
			PenaltyHelpState: help.State,
			RemainSeconds:    help.RemainSeconds,
		}
		if help.HelpText != "" {
			text := help.HelpText
			item.HelpText = &text
		}
		if help.PenaltyComment != "" {
			comment := help.PenaltyComment
			item.PenaltyComment = &comment
		}
		out = append(out, item)
	}
	return out
}

func bonusesFromEngine(bonuses []enapi.EngineBonus) []Bonus {
	if len(bonuses) == 0 {
		return nil
	}
	out := make([]Bonus, 0, len(bonuses))
	for _, bonus := range bonuses {
		out = append(out, Bonus{
			BonusId:        bonus.BonusID,
			Name:           bonus.Name,
			Number:         bonus.Number,
			Task:           bonus.Task,
			Help:           bonus.Help,
			IsAnswered:     bonus.IsAnswered,
			Answer:         FlexString(bonus.AnswerText),
			Expired:        bonus.Expired,
			SecondsToStart: bonus.SecondsToStart,
			SecondsLeft:    bonus.SecondsLeft,
			AwardTime:      bonus.AwardTime,
			Negative:       bonus.Negative,
		})
	}
	return out
}

func codeActionsFromEngine(actions []enapi.EngineMixedAction) []CodeAction {
	if len(actions) == 0 {
		return nil
	}
	out := make([]CodeAction, 0, len(actions))
	for _, action := range actions {
		out = append(out, CodeAction{
			LevelId:       action.LevelID,
			LevelNumber:   action.LevelNumber,
			UserId:        action.UserID,
			Kind:          action.Kind,
			Login:         action.Login,
			Answer:        action.Answer,
			EnterDateTime: dateTimeFromUnix(action.EnterUnix),
			IsCorrect:     action.IsCorrect,
			Negative:      action.Negative,
		})
	}
	return out
}

func engineActionFromSnapshot(snapshot *enapi.EngineActionSnapshot) *EngineAction {
	if snapshot == nil {
		return nil
	}
	action := &EngineAction{
		GameId:       snapshot.GameID,
		LevelId:      snapshot.LevelID,
		LevelNumber:  snapshot.LevelNumber,
		RejectReason: snapshot.RejectReason,
	}
	// The verdict is reported only when the engine actually judged the answer.
	// A submission the answer-block rule refused comes back with the answer
	// echoed and no verdict: reporting it as an incorrect answer would tell the
	// player their code was wrong when it was never checked, and hide that the
	// attempt was not spent.
	if snapshot.LevelAnswer != "" {
		answer := snapshot.LevelAnswer
		action.LevelAction = &ActionResult{Answer: &answer, IsCorrectAnswer: copyBool(snapshot.LevelOK)}
	}
	if snapshot.BonusAnswer != "" {
		answer := snapshot.BonusAnswer
		action.BonusAction = &ActionResult{Answer: &answer, IsCorrectAnswer: copyBool(snapshot.BonusOK)}
	}
	return action
}

func copyBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

// gameStartFromEngine normalizes GameModel.GameDateTimeStart.
//
// The new engine sends Unix seconds as a string ("1501015980") where the legacy
// engine sent a formatted local time; callers display the field as-is, so the
// digits are converted rather than passed through.
func gameStartFromEngine(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	unix, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil || unix <= 0 {
		return trimmed
	}
	return time.Unix(unix, 0).UTC().Format(engineTimeLayout)
}

// engineTimeLayout is the format the legacy engine used for GameDateTimeStart.
const engineTimeLayout = "2006-01-02 15:04:05"

// dateTimeFromUnix mirrors how the legacy engine reports timestamps: Value and
// Timestamp both carry Unix seconds.
func dateTimeFromUnix(unix int64) *DateTime {
	if unix <= 0 {
		return nil
	}
	return &DateTime{Value: float64(unix), Timestamp: unix}
}
