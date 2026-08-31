package main

import (
	"time"

	"github.com/skrashevich/encx-cli/encx"
	"github.com/skrashevich/encx-cli/encx/enapi"
)

// engineStateFromGameModel renders the mock's GameModel in the new engine's
// EngineState shape. It is the inverse of the mapping encx performs on the
// client side, so a round trip through both proves they agree.
func engineStateFromGameModel(model *encx.GameModel) *enapi.EngineState {
	if model == nil {
		return nil
	}
	state := &enapi.EngineState{
		GameID:            model.GameId,
		GameNumber:        model.GameNumber,
		GameTitle:         model.GameTitle,
		GameTypeID:        model.GameTypeId,
		GameZoneID:        model.GameZoneId,
		GameDateTimeStart: model.GameDateTimeStart,
		LevelSequence:     model.LevelSequence,
		CountLevels:       len(model.Levels),
		UserID:            model.UserId,
		TeamID:            model.TeamId,
		Login:             model.Login,
		TeamName:          model.TeamName,
		IsCaptain:         model.IsCaptain,
		ServerUnix:        timeNow().Unix(),
		Event:             engineEventFromModel(model),
	}

	for _, level := range model.Levels {
		state.Levels = append(state.Levels, enapi.EngineLevelBand{
			LevelID:     level.LevelId,
			LevelNumber: level.LevelNumber,
			LevelName:   level.LevelName,
			Dismissed:   level.Dismissed,
			IsPassed:    level.IsPassed,
		})
	}
	state.Level = engineLevelFromModel(model.Level)
	state.EngineAction = engineActionFromModel(model.EngineAction)
	return state
}

func engineEventFromModel(model *encx.GameModel) int {
	switch value := model.Event.(type) {
	case int:
		return value
	case float64:
		return int(value)
	default:
		return 0
	}
}

func engineLevelFromModel(level *encx.Level) *enapi.EngineCurrentLevel {
	if level == nil {
		return nil
	}
	out := &enapi.EngineCurrentLevel{
		LevelID:              level.LevelId,
		Number:               level.Number,
		Name:                 level.Name,
		Timeout:              level.Timeout,
		TimeoutAward:         level.TimeoutAward,
		TimeoutSecondsRemain: level.TimeoutSecondsRemain,
		IsPassed:             level.IsPassed,
		Dismissed:            level.Dismissed,
		HasAnswerBlockRule:   level.HasAnswerBlockRule,
		BlockDuration:        level.BlockDuration,
		BlockTargetID:        level.BlockTargetId,
		AttemptsNumber:       level.AttemtsNumber,
		AttemptsPeriod:       level.AttemtsPeriod,
		RequiredSectorsCount: level.RequiredSectorsCount,
		PassedSectorsCount:   level.PassedSectorsCount,
		PassedBonusesCount:   level.PassedBonusesCount,
		SectorsLeftToClose:   level.SectorsLeftToClose,
	}
	if level.StartTime != nil {
		out.LevelStartUnix = level.StartTime.Timestamp
	}

	for _, task := range level.Tasks {
		out.Tasks = append(out.Tasks, enapi.EngineTask{TaskTextFormatted: task.TaskText})
	}
	for _, message := range level.Messages {
		out.Messages = append(out.Messages, enapi.EngineLevelMessage{
			MessageID:   message.MessageId,
			OwnerID:     message.OwnerId,
			OwnerLogin:  message.OwnerLogin,
			WrappedText: firstNonEmptyText(message.WrappedText, message.MessageText),
		})
	}
	for _, sector := range level.Sectors {
		out.Sectors = append(out.Sectors, enapi.EngineSector{
			SectorID:   sector.SectorId,
			Order:      sector.Order,
			Name:       sector.Name,
			IsPassed:   sector.IsAnswered,
			AnswerText: sector.Answer.String(),
		})
	}
	out.Helps = engineHelpsFromModel(level.Helps)
	out.PenaltyHelps = engineHelpsFromModel(level.PenaltyHelps)
	for _, bonus := range level.Bonuses {
		out.Bonuses = append(out.Bonuses, enapi.EngineBonus{
			BonusID:        bonus.BonusId,
			Number:         bonus.Number,
			Name:           bonus.Name,
			Task:           bonus.Task,
			Help:           bonus.Help,
			IsAnswered:     bonus.IsAnswered,
			AnswerText:     bonus.Answer.String(),
			Expired:        bonus.Expired,
			SecondsToStart: bonus.SecondsToStart,
			SecondsLeft:    bonus.SecondsLeft,
			AwardTime:      bonus.AwardTime,
			Negative:       bonus.Negative,
		})
	}
	for _, action := range level.MixedActions {
		entry := enapi.EngineMixedAction{
			LevelID:     action.LevelId,
			LevelNumber: action.LevelNumber,
			UserID:      action.UserId,
			Login:       action.Login,
			Answer:      action.Answer,
			Kind:        action.Kind,
			IsCorrect:   action.IsCorrect,
			Negative:    action.Negative,
		}
		if action.EnterDateTime != nil {
			entry.EnterUnix = action.EnterDateTime.Timestamp
		}
		out.MixedActions = append(out.MixedActions, entry)
	}
	return out
}

func engineHelpsFromModel(helps []encx.Help) []enapi.EngineHelp {
	out := make([]enapi.EngineHelp, 0, len(helps))
	for _, help := range helps {
		entry := enapi.EngineHelp{
			HelpID:         help.HelpId,
			HelpNumber:     help.Number,
			IsPenalty:      help.IsPenalty,
			PenaltyTime:    help.Penalty,
			RequestConfirm: help.RequestConfirm,
			RemainSeconds:  help.RemainSeconds,
			State:          help.PenaltyHelpState,
		}
		if help.HelpText != nil {
			entry.HelpText = *help.HelpText
		}
		if help.PenaltyComment != nil {
			entry.PenaltyComment = *help.PenaltyComment
		}
		out = append(out, entry)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func engineActionFromModel(action *encx.EngineAction) *enapi.EngineActionSnapshot {
	if action == nil {
		return nil
	}
	snapshot := &enapi.EngineActionSnapshot{
		GameID:      action.GameId,
		LevelID:     action.LevelId,
		LevelNumber: action.LevelNumber,
	}
	if action.LevelAction != nil {
		if action.LevelAction.Answer != nil {
			snapshot.LevelAnswer = *action.LevelAction.Answer
		}
		// A nil verdict stays nil: the engine omits it when it never judged
		// the answer, and the client relies on that to tell "wrong" from
		// "not checked".
		snapshot.LevelOK = action.LevelAction.IsCorrectAnswer
	}
	if action.BonusAction != nil {
		if action.BonusAction.Answer != nil {
			snapshot.BonusAnswer = *action.BonusAction.Answer
		}
		snapshot.BonusOK = action.BonusAction.IsCorrectAnswer
	}
	snapshot.RejectReason = action.RejectReason
	return snapshot
}

func firstNonEmptyText(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// timeNow and timeUnix are seams so the mock's clock stays testable.
var (
	timeNow  = time.Now
	timeUnix = func(sec int64) time.Time { return time.Unix(sec, 0) }
)
