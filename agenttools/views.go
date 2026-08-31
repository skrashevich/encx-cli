package agenttools

import "github.com/skrashevich/encx-cli/encx"

// The engine returns large payloads with many fields an agent never reasons
// about (styling flags, duplicated wrapped text, raw HTML wrappers). The views
// below keep the parts that matter for play so a turn stays affordable in the
// model's context window.

type gameStateView struct {
	GameID        int                `json:"game_id"`
	GameNumber    int                `json:"game_number"`
	Title         string             `json:"title"`
	LevelSequence int                `json:"level_sequence"`
	StartsAt      string             `json:"starts_at,omitempty"`
	Login         string             `json:"login,omitempty"`
	TeamID        int                `json:"team_id,omitempty"`
	TeamName      string             `json:"team_name,omitempty"`
	IsCaptain     bool               `json:"is_captain"`
	Levels        []levelSummaryView `json:"levels,omitempty"`
	CurrentLevel  *levelView         `json:"current_level,omitempty"`
}

type levelSummaryView struct {
	LevelID     int    `json:"level_id"`
	LevelNumber int    `json:"level_number"`
	LevelName   string `json:"level_name,omitempty"`
	IsPassed    bool   `json:"is_passed"`
	Dismissed   bool   `json:"dismissed"`
}

type levelView struct {
	LevelID              int             `json:"level_id"`
	Number               int             `json:"number"`
	Name                 string          `json:"name,omitempty"`
	IsPassed             bool            `json:"is_passed"`
	Dismissed            bool            `json:"dismissed"`
	CanSubmitAnswer      bool            `json:"can_submit_answer"`
	TimeoutSecondsRemain int             `json:"timeout_seconds_remain,omitempty"`
	BlockDuration        int             `json:"block_duration,omitempty"`
	RequiredSectorsCount int             `json:"required_sectors_count"`
	PassedSectorsCount   int             `json:"passed_sectors_count"`
	SectorsLeftToClose   int             `json:"sectors_left_to_close"`
	Tasks                []string        `json:"tasks,omitempty"`
	Sectors              []sectorView    `json:"sectors,omitempty"`
	Bonuses              []bonusView     `json:"bonuses,omitempty"`
	Hints                []hintView      `json:"hints,omitempty"`
	PenaltyHints         []hintView      `json:"penalty_hints,omitempty"`
	Messages             []messageView   `json:"messages,omitempty"`
	Actions              []codeEntryView `json:"actions,omitempty"`
	// Images referenced by the level's HTML. On many levels the picture is the
	// task, so the agent has to know it exists before it can ask to see it.
	Images []levelImage `json:"images,omitempty"`
}

type sectorView struct {
	SectorID   int    `json:"sector_id"`
	Order      int    `json:"order"`
	Name       string `json:"name,omitempty"`
	IsAnswered bool   `json:"is_answered"`
	Answer     string `json:"answer,omitempty"`
}

type bonusView struct {
	BonusID        int    `json:"bonus_id"`
	Number         int    `json:"number"`
	Name           string `json:"name,omitempty"`
	Task           string `json:"task,omitempty"`
	Help           string `json:"help,omitempty"`
	IsAnswered     bool   `json:"is_answered"`
	Answer         string `json:"answer,omitempty"`
	Expired        bool   `json:"expired"`
	SecondsToStart int    `json:"seconds_to_start,omitempty"`
	SecondsLeft    int    `json:"seconds_left,omitempty"`
	AwardSeconds   int    `json:"award_seconds,omitempty"`
	Negative       bool   `json:"negative"`
}

type hintView struct {
	HelpID        int    `json:"help_id"`
	Number        int    `json:"number"`
	Text          string `json:"text,omitempty"`
	IsPenalty     bool   `json:"is_penalty"`
	PenaltySecs   int    `json:"penalty_seconds,omitempty"`
	State         int    `json:"state,omitempty"`
	RemainSeconds int    `json:"remain_seconds,omitempty"`
}

type messageView struct {
	MessageID  int    `json:"message_id"`
	OwnerLogin string `json:"owner_login,omitempty"`
	Text       string `json:"text,omitempty"`
}

type codeEntryView struct {
	ActionID    int    `json:"action_id"`
	LevelNumber int    `json:"level_number"`
	Kind        int    `json:"kind"`
	Login       string `json:"login,omitempty"`
	Answer      string `json:"answer"`
	IsCorrect   bool   `json:"is_correct"`
	EnteredAt   string `json:"entered_at,omitempty"`
	PenaltySecs int    `json:"penalty_seconds,omitempty"`
}

type gameSummaryView struct {
	GameID     int    `json:"game_id"`
	GameNumber int    `json:"game_number,omitempty"`
	Title      string `json:"title"`
	GameTypeID int    `json:"game_type_id,omitempty"`
	ZoneID     int    `json:"zone_id,omitempty"`
	StartsAt   int64  `json:"starts_at,omitempty"`
	FinishesAt int64  `json:"finishes_at,omitempty"`
	MaxPlayers int    `json:"max_players,omitempty"`
}

type actionResultView struct {
	LevelNumber   int        `json:"level_number"`
	Answer        string     `json:"answer,omitempty"`
	IsCorrect     *bool      `json:"is_correct,omitempty"`
	CurrentLevel  *levelView `json:"current_level,omitempty"`
	LevelAdvanced bool       `json:"level_advanced"`
}

func newGameStateView(model *encx.GameModel) *gameStateView {
	if model == nil {
		return nil
	}
	view := &gameStateView{
		GameID:        model.GameId,
		GameNumber:    model.GameNumber,
		Title:         model.GameTitle,
		LevelSequence: model.LevelSequence,
		StartsAt:      model.GameDateTimeStart,
		Login:         model.Login,
		TeamID:        model.TeamId,
		TeamName:      model.TeamName,
		IsCaptain:     model.IsCaptain,
		CurrentLevel:  newLevelView(model.Level),
	}
	for _, level := range model.Levels {
		view.Levels = append(view.Levels, levelSummaryView{
			LevelID:     level.LevelId,
			LevelNumber: level.LevelNumber,
			LevelName:   level.LevelName,
			IsPassed:    level.IsPassed,
			Dismissed:   level.Dismissed,
		})
	}
	return view
}

func newLevelView(level *encx.Level) *levelView {
	if level == nil {
		return nil
	}
	view := &levelView{
		LevelID:              level.LevelId,
		Number:               level.Number,
		Name:                 level.Name,
		IsPassed:             level.IsPassed,
		Dismissed:            level.Dismissed,
		CanSubmitAnswer:      level.CanSubmitLevelAnswer(),
		TimeoutSecondsRemain: level.TimeoutSecondsRemain,
		BlockDuration:        level.BlockDuration,
		RequiredSectorsCount: level.RequiredSectorsCount,
		PassedSectorsCount:   level.PassedSectorsCount,
		SectorsLeftToClose:   level.SectorsLeftToClose,
	}
	for _, task := range level.Tasks {
		if task.TaskText != "" {
			view.Tasks = append(view.Tasks, task.TaskText)
		}
	}
	if len(view.Tasks) == 0 && level.Task != nil && level.Task.TaskText != "" {
		view.Tasks = append(view.Tasks, level.Task.TaskText)
	}
	for _, sector := range level.Sectors {
		view.Sectors = append(view.Sectors, sectorView{
			SectorID:   sector.SectorId,
			Order:      sector.Order,
			Name:       sector.Name,
			IsAnswered: sector.IsAnswered,
			Answer:     sector.Answer.String(),
		})
	}
	for _, bonus := range level.Bonuses {
		view.Bonuses = append(view.Bonuses, bonusView{
			BonusID:        bonus.BonusId,
			Number:         bonus.Number,
			Name:           bonus.Name,
			Task:           bonus.Task,
			Help:           bonus.Help,
			IsAnswered:     bonus.IsAnswered,
			Answer:         bonus.Answer.String(),
			Expired:        bonus.Expired,
			SecondsToStart: bonus.SecondsToStart,
			SecondsLeft:    bonus.SecondsLeft,
			AwardSeconds:   bonus.AwardTime,
			Negative:       bonus.Negative,
		})
	}
	view.Hints = newHintViews(level.Helps)
	view.PenaltyHints = newHintViews(level.PenaltyHelps)
	for _, message := range level.Messages {
		view.Messages = append(view.Messages, messageView{
			MessageID:  message.MessageId,
			OwnerLogin: message.OwnerLogin,
			Text:       message.MessageText,
		})
	}
	view.Actions = newCodeEntryViews(level.MixedActions)
	view.Images = collectLevelImages(level)
	return view
}

func newHintViews(helps []encx.Help) []hintView {
	var views []hintView
	for _, help := range helps {
		view := hintView{
			HelpID:        help.HelpId,
			Number:        help.Number,
			IsPenalty:     help.IsPenalty,
			PenaltySecs:   help.Penalty,
			State:         help.PenaltyHelpState,
			RemainSeconds: help.RemainSeconds,
		}
		if help.HelpText != nil {
			view.Text = *help.HelpText
		}
		views = append(views, view)
	}
	return views
}

func newCodeEntryViews(actions []encx.CodeAction) []codeEntryView {
	var views []codeEntryView
	for _, action := range actions {
		views = append(views, codeEntryView{
			ActionID:    action.ActionId,
			LevelNumber: action.LevelNumber,
			Kind:        action.Kind,
			Login:       action.Login,
			Answer:      action.Answer,
			IsCorrect:   action.IsCorrect,
			EnteredAt:   action.LocDateTime,
			PenaltySecs: action.Penalty,
		})
	}
	return views
}

func newGameSummaryViews(games []encx.GameInfo) []gameSummaryView {
	views := make([]gameSummaryView, 0, len(games))
	for _, game := range games {
		view := gameSummaryView{
			GameID:     game.GameID,
			GameNumber: game.GameNum,
			Title:      game.Title,
			GameTypeID: game.GameTypeID,
			ZoneID:     game.ZoneId,
			MaxPlayers: game.MaxPlayers,
		}
		if game.StartDateTime != nil {
			view.StartsAt = game.StartDateTime.Timestamp
		}
		if game.FinishDateTime != nil {
			view.FinishesAt = game.FinishDateTime.Timestamp
		}
		views = append(views, view)
	}
	return views
}

// newActionResultView summarizes the engine reply to a submitted answer.
// bonus selects which of the two action slots the engine filled in.
func newActionResultView(model *encx.GameModel, submittedLevel int, bonus bool) *actionResultView {
	if model == nil {
		return nil
	}
	view := &actionResultView{CurrentLevel: newLevelView(model.Level)}
	if action := model.EngineAction; action != nil {
		view.LevelNumber = action.LevelNumber
		result := action.LevelAction
		if bonus {
			result = action.BonusAction
		}
		if result != nil {
			if result.Answer != nil {
				view.Answer = *result.Answer
			}
			view.IsCorrect = result.IsCorrectAnswer
		}
	}
	if model.Level != nil {
		view.LevelAdvanced = submittedLevel > 0 && model.Level.Number != submittedLevel
	}
	return view
}
