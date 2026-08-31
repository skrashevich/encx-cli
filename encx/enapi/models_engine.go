package enapi

// Engine message types accepted by POST /games/{id}/engine and its
// ASP-compatible aliases. See docs/newengine/engine-websocket.md.
const (
	EngineMessageSubscribe = "subscribe"
	EngineMessageAnswer    = "answer"
	EngineMessageBonus     = "bonus"
	EngineMessagePenalty   = "penalty"
)

// Penalty hint actions carried by EngineClientMessage.PenaltyAct.
const (
	PenaltyActUnknown = 0
	PenaltyActApprove = 1
	PenaltyActConfirm = 2
)

// EngineClientMessage is a player action sent to the engine.
type EngineClientMessage struct {
	Type        string `json:"type,omitempty"`
	Answer      string `json:"answer,omitempty"`
	LevelID     int    `json:"level_id,omitempty"`
	LevelNumber int    `json:"level_number,omitempty"`
	PenaltyID   int    `json:"penalty_id,omitempty"`
	PenaltyAct  int    `json:"penalty_act,omitempty"`
	ReqID       string `json:"req_id,omitempty"`
	Compact     bool   `json:"compact,omitempty"`
}

// EngineState is the full engine state (models.EngineState).
type EngineState struct {
	Event             int                   `json:"event"`
	GameID            int                   `json:"game_id"`
	GameNumber        int                   `json:"game_number"`
	GameTitle         string                `json:"game_title"`
	GameTypeID        int                   `json:"game_type_id"`
	GameZoneID        int                   `json:"game_zone_id"`
	GameDateTimeStart string                `json:"game_date_time_start"`
	LevelSequence     int                   `json:"level_sequence"`
	CountLevels       int                   `json:"count_levels"`
	UserID            int                   `json:"user_id"`
	TeamID            int                   `json:"team_id"`
	Login             string                `json:"login"`
	TeamName          string                `json:"team_name"`
	IsCaptain         bool                  `json:"is_captain"`
	IsChatVisible     bool                  `json:"is_chat_visible"`
	IsGuestMode       bool                  `json:"is_guest_mode"`
	AllowedToAnswer   bool                  `json:"allowed_to_answer"`
	UserAnswerHit     bool                  `json:"user_answer_hit"`
	ServerUnix        int64                 `json:"server_unix"`
	Level             *EngineCurrentLevel   `json:"level"`
	Levels            []EngineLevelBand     `json:"levels"`
	EngineAction      *EngineActionSnapshot `json:"engine_action"`
}

// EngineLevelBand is a level entry in the level strip.
type EngineLevelBand struct {
	LevelID     int    `json:"level_id"`
	LevelNumber int    `json:"level_number"`
	LevelName   string `json:"level_name"`
	Dismissed   bool   `json:"dismissed"`
	IsPassed    bool   `json:"is_passed"`
}

// EngineCurrentLevel is the level the player is standing on.
type EngineCurrentLevel struct {
	LevelID              int                   `json:"level_id"`
	Number               int                   `json:"number"`
	Name                 string                `json:"name"`
	Timeout              int                   `json:"timeout"`
	TimeoutAward         int                   `json:"timeout_award"`
	TimeoutSecondsRemain int                   `json:"timeout_seconds_remain"`
	TimeoutExpiresUnix   int64                 `json:"timeout_expires_unix"`
	IsPassed             bool                  `json:"is_passed"`
	Dismissed            bool                  `json:"dismissed"`
	LevelStartUnix       int64                 `json:"level_start_unix"`
	LevelPassedUnix      int64                 `json:"level_passed_unix"`
	StartsAtUnix         int64                 `json:"starts_at_unix"`
	SecondsToStart       int                   `json:"seconds_to_start"`
	HasAnswerBlockRule   bool                  `json:"has_answer_block_rule"`
	BlockDuration        int                   `json:"block_duration"`
	BlockTargetID        int                   `json:"block_target_id"`
	AttemptsNumber       int                   `json:"attempts_number"`
	AttemptsPeriod       int                   `json:"attempts_period"`
	RequiredSectorsCount int                   `json:"required_sectors_count"`
	PassedSectorsCount   int                   `json:"passed_sectors_count"`
	PassedBonusesCount   int                   `json:"passed_bonuses_count"`
	SectorsLeftToClose   int                   `json:"sectors_left_to_close"`
	Tasks                []EngineTask          `json:"tasks"`
	Messages             []EngineLevelMessage  `json:"messages"`
	Sectors              []EngineSector        `json:"sectors"`
	Helps                []EngineHelp          `json:"helps"`
	PenaltyHelps         []EngineHelp          `json:"penalty_helps"`
	Bonuses              []EngineBonus         `json:"bonuses"`
	MixedActions         []EngineMixedAction   `json:"mixed_actions"`
	Answers              []EnginePreviewAnswer `json:"answers"`
}

// EngineTask is the level assignment text.
type EngineTask struct {
	TaskID            int    `json:"task_id"`
	TaskTextFormatted string `json:"task_text_formatted"`
	ForPlayerID       int    `json:"for_player_id"`
	ForPlayerLoc      string `json:"for_player_loc"`
}

// EngineLevelMessage is a message from the organizers.
type EngineLevelMessage struct {
	MessageID   int    `json:"message_id"`
	OwnerID     int    `json:"owner_id"`
	OwnerLogin  string `json:"owner_login"`
	WrappedText string `json:"wrapped_text"`
}

// EngineSector is a sector of the current level.
type EngineSector struct {
	SectorID     int                   `json:"sector_id"`
	Order        int                   `json:"order"`
	Name         string                `json:"name"`
	IsPassed     bool                  `json:"is_passed"`
	AnswerText   string                `json:"answer_text"`
	AnswerLogin  string                `json:"answer_login"`
	AnswerUserID int                   `json:"answer_user_id"`
	AnswerUnix   int64                 `json:"answer_unix"`
	Answers      []EnginePreviewAnswer `json:"answers"`
}

// EngineHelp is a hint, regular or penalty.
type EngineHelp struct {
	HelpID         int    `json:"help_id"`
	HelpNumber     int    `json:"help_number"`
	HelpText       string `json:"help_text"`
	IsPenalty      bool   `json:"is_penalty"`
	PenaltyTime    int    `json:"penalty_time"`
	PenaltyComment string `json:"penalty_comment"`
	RequestConfirm bool   `json:"request_confirm"`
	RemainSeconds  int    `json:"remain_seconds"`
	State          int    `json:"state"`
	Award          int    `json:"award"`
	Delay          int    `json:"delay"`
	OpensAtUnix    int64  `json:"opens_at_unix"`
	ForPlayerID    int    `json:"for_player_id"`
	ForPlayerLoc   string `json:"for_player_loc"`
}

// EngineBonus is a bonus task of the current level.
type EngineBonus struct {
	BonusID        int                   `json:"bonus_id"`
	Number         int                   `json:"number"`
	Name           string                `json:"name"`
	Task           string                `json:"task"`
	Help           string                `json:"help"`
	IsAnswered     bool                  `json:"is_answered"`
	AnswerText     string                `json:"answer_text"`
	AnswerLogin    string                `json:"answer_login"`
	AnswerUserID   int                   `json:"answer_user_id"`
	AnswerUnix     int64                 `json:"answer_unix"`
	Answers        []EnginePreviewAnswer `json:"answers"`
	Expired        bool                  `json:"expired"`
	SecondsToStart int                   `json:"seconds_to_start"`
	SecondsLeft    int                   `json:"seconds_left"`
	AwardTime      int                   `json:"award_time"`
	Negative       bool                  `json:"negative"`
	Delay          int                   `json:"delay"`
	LifeTime       int                   `json:"life_time"`
	AvailableFrom  string                `json:"available_from"`
	AvailableTo    string                `json:"available_to"`
	StartsAtUnix   int64                 `json:"starts_at_unix"`
	ExpiresAtUnix  int64                 `json:"expires_at_unix"`
	ForPlayerID    int                   `json:"for_player_id"`
	ForPlayerLoc   string                `json:"for_player_loc"`
}

// EngineMixedAction is an entry of the level answer log.
type EngineMixedAction struct {
	LevelID     int    `json:"level_id"`
	LevelNumber int    `json:"level_number"`
	UserID      int    `json:"user_id"`
	Login       string `json:"login"`
	Answer      string `json:"answer"`
	Kind        int    `json:"kind"`
	IsCorrect   bool   `json:"is_correct"`
	Negative    bool   `json:"negative"`
	EnterUnix   int64  `json:"enter_unix"`
}

// EnginePreviewAnswer is an author-visible answer shown in preview mode.
type EnginePreviewAnswer struct {
	Text         string `json:"text"`
	ForPlayerID  int    `json:"for_player_id"`
	ForPlayerLoc string `json:"for_player_loc"`
}

// EngineActionSnapshot reports the outcome of the last submitted action.
//
// LevelOK and BonusOK are pointers because the engine omits them when it never
// judged the answer — a submission refused by the answer-block rule comes back
// with reject_reason set and no verdict at all. Decoding that absence into a
// plain false would report the player's answer as wrong.
type EngineActionSnapshot struct {
	GameID             int    `json:"game_id"`
	LevelID            int    `json:"level_id"`
	LevelNumber        int    `json:"level_number"`
	LevelAnswer        string `json:"level_answer"`
	LevelOK            *bool  `json:"level_ok"`
	BonusAnswer        string `json:"bonus_answer"`
	BonusOK            *bool  `json:"bonus_ok"`
	RejectReason       string `json:"reject_reason"`
	RequestLevelID     int    `json:"request_level_id"`
	RequestLevelNumber int    `json:"request_level_number"`
}

// Reject reasons the engine reports in EngineActionSnapshot.RejectReason.
const (
	// RejectAnswerBlocked means the level's answer-block rule refused the
	// submission: the attempt was not spent and no verdict was made.
	RejectAnswerBlocked = "answer_blocked"
)
