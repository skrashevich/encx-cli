package enapi

// AdminGameLevelsResponse is models.AdminGameLevelsResponse — the level manager.
//
// Measured against demo.en.cx: can_manipulate_levels is true even for a started
// game — can_change_levels_sequence is the one that goes false there.
type AdminGameLevelsResponse struct {
	GameID                  int              `json:"game_id"`
	GameNum                 int              `json:"game_num"`
	GameTypeID              int              `json:"game_type_id"`
	ZoneID                  int              `json:"zone_id"`
	StatusID                int              `json:"status_id"`
	Title                   string           `json:"title"`
	Started                 bool             `json:"started"`
	LevelsSequenceID        int              `json:"levels_sequence_id"`
	CanManipulateLevels     bool             `json:"can_manipulate_levels"`
	CanChangeLevelsSequence bool             `json:"can_change_levels_sequence"`
	ShowPassingSequence     bool             `json:"show_passing_sequence"`
	Levels                  []AdminLevelItem `json:"levels"`
}

// AdminLevelItem is models.AdminLevelItem.
type AdminLevelItem struct {
	LevelID     int    `json:"level_id"`
	LevelNumber int    `json:"level_number"`
	LevelName   string `json:"level_name"`
	Comment     string `json:"comment"`
	Dismissed   bool   `json:"dismissed"`
	OwnerID     int    `json:"owner_id"`
}

// AdminLevelEditorResponse is models.AdminLevelEditorResponse: the whole level
// in one document, where the legacy engine needed a page per collection.
//
// Measured against demo.en.cx: timeout_time_award_sec is signed — negative for a
// penalty, positive for a bonus — and required_sectors_count keeps its previous
// value unless passing_condition_id is 1.
type AdminLevelEditorResponse struct {
	GameID               int                 `json:"game_id"`
	GameNum              int                 `json:"game_num"`
	GameTypeID           int                 `json:"game_type_id"`
	ZoneID               int                 `json:"zone_id"`
	Title                string              `json:"title"`
	Version              string              `json:"version"`
	Level                *AdminLevelItem     `json:"level"`
	Levels               []AdminLevelItem    `json:"levels"`
	Tasks                []AdminTaskDTO      `json:"tasks"`
	Helps                []AdminHelpDTO      `json:"helps"`
	PenaltyHelps         []AdminHelpDTO      `json:"penalty_helps"`
	Bonuses              []AdminBonusDTO     `json:"bonuses"`
	Sectors              []AdminSectorDTO    `json:"sectors"`
	Answers              []AdminAnswerDTO    `json:"answers"`
	Messages             []AdminMessageDTO   `json:"messages"`
	Members              []AdminMemberOption `json:"members"`
	TimeoutSec           int                 `json:"timeout_sec"`
	TimeoutTimeAwardSec  int                 `json:"timeout_time_award_sec"`
	TimeoutPointsAward   int                 `json:"timeout_points_award"`
	AttemptsNumber       int                 `json:"attempts_number"`
	AttemptsPeriodSec    int                 `json:"attempts_period_sec"`
	BlockTypeID          int                 `json:"block_type_id"`
	PassingConditionID   int                 `json:"passing_condition_id"`
	RequiredSectorsCount int                 `json:"required_sectors_count"`
	SupportsAutoPass     bool                `json:"supports_auto_pass"`
	SupportsBonuses      bool                `json:"supports_bonuses"`
	SupportsSectors      bool                `json:"supports_answers"`
	SupportsHelps        bool                `json:"supports_helps"`
	SupportsPenaltyHelps bool                `json:"supports_penalty_helps"`
	SupportsMessages     bool                `json:"supports_messages"`
	SupportsTasks        bool                `json:"supports_tasks"`
}

// AdminTaskDTO is models.AdminTaskDTO.
type AdminTaskDTO struct {
	TaskID        int     `json:"task_id"`
	LevelID       int     `json:"level_id"`
	TaskText      string  `json:"task_text"`
	ReplaceNlToBr bool    `json:"replace_nl_to_br"`
	ForUserID     int     `json:"for_user_id"`
	ForUserLogin  string  `json:"for_user_login"`
	ForTeamID     int     `json:"for_team_id"`
	ForTeamName   string  `json:"for_team_name"`
	Latitude      float64 `json:"latitude"`
	Longitude     float64 `json:"longitude"`
}

// AdminHelpDTO is models.AdminHelpDTO.
type AdminHelpDTO struct {
	HelpID                int    `json:"help_id"`
	LevelID               int    `json:"level_id"`
	HelpNumber            int    `json:"help_number"`
	HelpText              string `json:"help_text"`
	Timeout               int    `json:"timeout"`
	IsPenalty             bool   `json:"is_penalty"`
	PenaltyTime           int    `json:"penalty_time"`
	PenaltyScore          int    `json:"penalty_score"`
	PenaltyComment        string `json:"penalty_comment"`
	RequestPenaltyConfirm bool   `json:"request_penalty_confirm"`
	ForUserID             int    `json:"for_user_id"`
	ForUserLogin          string `json:"for_user_login"`
	ForTeamID             int    `json:"for_team_id"`
	ForTeamName           string `json:"for_team_name"`
}

// AdminBonusDTO is models.AdminBonusDTO.
type AdminBonusDTO struct {
	BonusID          int      `json:"bonus_id"`
	GameID           int      `json:"game_id"`
	BonusName        string   `json:"bonus_name"`
	Task             string   `json:"task"`
	BonusHelp        string   `json:"bonus_help"`
	Answers          []string `json:"answers"`
	BonusTime        int      `json:"bonus_time"`
	Negative         bool     `json:"negative"`
	AllLevels        bool     `json:"all_levels"`
	LevelIDs         []int    `json:"level_ids"`
	HasAbsoluteLimit bool     `json:"has_absolute_limit"`
	ValidFrom        string   `json:"valid_from"`
	ValidTo          string   `json:"valid_to"`
	HasDelay         bool     `json:"has_delay"`
	DelaySec         int      `json:"delay_sec"`
	HasRelativeLimit bool     `json:"has_relative_limit"`
	LifeTimeSec      int      `json:"life_time_sec"`
	UserID           int      `json:"user_id"`
	Login            string   `json:"login"`
	TeamID           int      `json:"team_id"`
	TeamName         string   `json:"team_name"`
}

// AdminSectorDTO is models.AdminSectorDTO.
type AdminSectorDTO struct {
	SectorID                int    `json:"sector_id"`
	LevelID                 int    `json:"level_id"`
	SectorName              string `json:"sector_name"`
	TaskID                  int    `json:"task_id"`
	AnswerTypeID            int    `json:"answer_type_id"`
	ValidAnswerScoreAward   int    `json:"valid_answer_score_award"`
	WrongAnswerScorePenalty int    `json:"wrong_answer_score_penalty"`
}

// AdminAnswerDTO is models.AdminAnswerDTO.
type AdminAnswerDTO struct {
	AnswerID   int    `json:"answer_id"`
	LevelID    int    `json:"level_id"`
	SectorID   int    `json:"sector_id"`
	AnswerText string `json:"answer_text"`
	ForUserID  int    `json:"for_user_id"`
	ForTeamID  int    `json:"for_team_id"`
	ScoreAward int    `json:"score_award"`
	TimeAward  int    `json:"time_award"`
}

// AdminMessageDTO is models.AdminMessageDTO.
type AdminMessageDTO struct {
	MessageID      int    `json:"message_id"`
	MessageText    string `json:"message_text"`
	ReplaceNlToBr  bool   `json:"replace_nl_to_br"`
	AllLevels      bool   `json:"all_levels"`
	LevelIDs       []int  `json:"level_ids"`
	RequiredPoints int    `json:"required_points"`
}

// AdminMemberOption is models.AdminMemberOption — the ForMember dropdown.
type AdminMemberOption struct {
	ID    int    `json:"id"`
	Label string `json:"label"`
}

// AdminCreateLevelRequest is models.AdminCreateLevelRequest.
type AdminCreateLevelRequest struct {
	LevelName   string `json:"level_name,omitempty"`
	Comment     string `json:"comment,omitempty"`
	AfterLevel  int    `json:"after_level,omitempty"`
	BeforeLevel int    `json:"before_level,omitempty"`
}

// AdminTaskRequest is models.AdminTaskRequest.
type AdminTaskRequest struct {
	TaskText      string  `json:"task_text"`
	ReplaceNlToBr bool    `json:"replace_nl_to_br"`
	ForMemberID   int     `json:"for_member_id"`
	Latitude      float64 `json:"latitude,omitempty"`
	Longitude     float64 `json:"longitude,omitempty"`
}

// AdminHelpRequest is models.AdminHelpRequest. Timeout and PenaltyTime are
// seconds. penalty_comment and request_penalty_confirm carry no omitempty so a
// caller can clear them.
type AdminHelpRequest struct {
	HelpText              string `json:"help_text"`
	Timeout               int    `json:"timeout"`
	ForMemberID           int    `json:"for_member_id"`
	IsPenalty             bool   `json:"is_penalty"`
	PenaltyTime           int    `json:"penalty_time"`
	PenaltyScore          int    `json:"penalty_score"`
	PenaltyComment        string `json:"penalty_comment"`
	RequestPenaltyConfirm bool   `json:"request_penalty_confirm"`
}

// AdminBonusRequest is models.AdminBonusRequest.
//
// The has_* switches carry no omitempty: they are the form's checkboxes, and an
// omitted false would leave a limit the caller cleared still in place.
type AdminBonusRequest struct {
	BonusName        string   `json:"bonus_name"`
	Task             string   `json:"task"`
	BonusHelp        string   `json:"bonus_help"`
	Answers          []string `json:"answers"`
	BonusTime        int      `json:"bonus_time"`
	Negative         bool     `json:"negative"`
	ForMemberID      int      `json:"for_member_id"`
	AllLevels        bool     `json:"all_levels"`
	LevelIDs         []int    `json:"level_ids,omitempty"`
	HasAbsoluteLimit bool     `json:"has_absolute_limit"`
	ValidFrom        string   `json:"valid_from,omitempty"`
	ValidTo          string   `json:"valid_to,omitempty"`
	HasDelay         bool     `json:"has_delay"`
	DelaySec         int      `json:"delay_sec,omitempty"`
	HasRelativeLimit bool     `json:"has_relative_limit"`
	LifeTimeSec      int      `json:"life_time_sec,omitempty"`
}

// AdminSectorRequest is models.AdminSectorRequest.
type AdminSectorRequest struct {
	SectorName              string `json:"sector_name"`
	TaskID                  int    `json:"task_id,omitempty"`
	ValidAnswerScoreAward   int    `json:"valid_answer_score_award,omitempty"`
	WrongAnswerScorePenalty int    `json:"wrong_answer_score_penalty,omitempty"`
}

// AdminAnswerRequest is models.AdminAnswerRequest.
type AdminAnswerRequest struct {
	AnswerText  string `json:"answer_text"`
	SectorID    int    `json:"sector_id,omitempty"`
	ForMemberID int    `json:"for_member_id"`
	ScoreAward  int    `json:"score_award,omitempty"`
	TimeAward   int    `json:"time_award,omitempty"`
}

// AdminAnswerBatchItem is models.AdminAnswerBatchItem.
type AdminAnswerBatchItem struct {
	AnswerText  string `json:"answer_text"`
	ForMemberID int    `json:"for_member_id"`
	ScoreAward  int    `json:"score_award,omitempty"`
	TimeAward   int    `json:"time_award,omitempty"`
}

// AdminAnswersBatchRequest is models.AdminAnswersBatchRequest.
type AdminAnswersBatchRequest struct {
	SectorID int                    `json:"sector_id,omitempty"`
	Answers  []AdminAnswerBatchItem `json:"answers"`
}

// AdminMessageRequest is models.AdminMessageRequest.
type AdminMessageRequest struct {
	MessageText    string `json:"message_text"`
	ReplaceNlToBr  bool   `json:"replace_nl_to_br"`
	AllLevels      bool   `json:"all_levels"`
	LevelIDs       []int  `json:"level_ids,omitempty"`
	RequiredPoints int    `json:"required_points,omitempty"`
}

// AdminLevelMetaRequest is models.AdminLevelMetaRequest.
type AdminLevelMetaRequest struct {
	LevelName    string `json:"level_name"`
	Comment      string `json:"comment"`
	AnswerTypeID int    `json:"answer_type_id,omitempty"`
}

// AdminLevelAutoPassRequest is models.AdminLevelAutoPassRequest.
type AdminLevelAutoPassRequest struct {
	TimeoutHours   int  `json:"timeout_hours"`
	TimeoutMinutes int  `json:"timeout_minutes"`
	TimeoutSeconds int  `json:"timeout_seconds"`
	PenaltyEnabled bool `json:"penalty_enabled"`
	PenaltyHours   int  `json:"penalty_hours"`
	PenaltyMinutes int  `json:"penalty_minutes"`
	PenaltySeconds int  `json:"penalty_seconds"`
	AwardIsPenalty bool `json:"award_is_penalty"`
	PointsAward    int  `json:"points_award,omitempty"`
}

// Sections accepted by AdminLevelSettingsRequest.Section: the endpoint edits
// one panel of the level settings at a time, mirroring the ASP editor.
const (
	AdminSettingsSectionBlocking = "blocking"
	AdminSettingsSectionSectors  = "sectors"
)

// AdminLevelSettingsRequest is models.AdminLevelSettingsRequest.
//
// The numeric fields carry no omitempty on purpose: zero is a meaningful value
// here — passing_condition_id 0 means "close every sector" and
// required_sectors_count 0 means "all of them" — so omitting them would leave
// the server unable to tell "all sectors" from "field not sent".
type AdminLevelSettingsRequest struct {
	Section               string `json:"section"`
	AttemptsNumber        int    `json:"attempts_number"`
	AttemptsCount         int    `json:"attempts_count"`
	AttemptsPeriodHours   int    `json:"attempts_period_hours"`
	AttemptsPeriodMinutes int    `json:"attempts_period_minutes"`
	AttemptsPeriodSeconds int    `json:"attempts_period_seconds"`
	BlockTypeID           int    `json:"block_type_id"`
	PassingConditionID    int    `json:"passing_condition_id"`
	RequiredSectorsCount  int    `json:"required_sectors_count"`
	Unrestricted          bool   `json:"unrestricted,omitempty"`
	RestrictionEnabled    bool   `json:"restriction_enabled,omitempty"`
}

// AdminExchangeLevelsRequest is models.AdminExchangeLevelsRequest.
type AdminExchangeLevelsRequest struct {
	Level1ID int `json:"level1_id"`
	Level2ID int `json:"level2_id"`
}

// AdminPutLevelRequest is models.AdminPutLevelRequest.
type AdminPutLevelRequest struct {
	LevelID      int `json:"level_id"`
	AfterLevelID int `json:"after_level_id"`
}

// AdminCopyLevelsRequest is models.AdminCopyLevelsRequest.
type AdminCopyLevelsRequest struct {
	FromLevelID int `json:"from_level_id"`
	Count       int `json:"count"`
}

// AdminBulkLevelsRequest is models.AdminBulkLevelsRequest.
type AdminBulkLevelsRequest struct {
	LevelIDs []int `json:"level_ids"`
}

// AdminGamesListResponse is the answer of GET /admin/games.
//
// It is not models.GamesResponse: the admin listing has its own row shape,
// keyed by game_id rather than id, as a live call to api.en.cx confirms.
type AdminGamesListResponse struct {
	Items      []AdminGameListItem `json:"items"`
	TotalCount int                 `json:"total_count"`
	TotalPages int                 `json:"total_pages"`
	Page       int                 `json:"page"`
	OnlyOwn    bool                `json:"only_own"`
}

// AdminGameListItem is one row of the admin game manager.
type AdminGameListItem struct {
	GameID         int    `json:"game_id"`
	GameNum        int    `json:"game_num"`
	Title          string `json:"title"`
	ZoneID         int    `json:"zone_id"`
	GameTypeID     int    `json:"game_type_id"`
	StatusID       int    `json:"status_id"`
	StartDateTime  string `json:"start_date_time"`
	FinishDateTime string `json:"finish_date_time"`
	FeeType        string `json:"fee_type"`
	OwnerID        int    `json:"owner_id"`
	OwnerLogin     string `json:"owner_login"`
	RefereeEnd     bool   `json:"referee_end"`
}

// AdminGameLifecycle is models.AdminGameLifecycleResponse — which management
// actions the current session may perform on a game.
type AdminGameLifecycle struct {
	GameID                int    `json:"game_id"`
	GameNum               int    `json:"game_num"`
	Title                 string `json:"title"`
	StatusID              int    `json:"status_id"`
	Started               bool   `json:"started"`
	Finished              bool   `json:"finished"`
	RateClosed            bool   `json:"rate_closed"`
	PointsCalculated      bool   `json:"points_calculated"`
	QualityRateCalculated bool   `json:"quality_rate_calculated"`
	CanDeliver            bool   `json:"can_deliver"`
	CanCancel             bool   `json:"can_cancel"`
	CanCalculatePoints    bool   `json:"can_calculate_points"`
	CanCancelPoints       bool   `json:"can_cancel_points"`
	CanCloseRate          bool   `json:"can_close_rate"`
	CanOpenRate           bool   `json:"can_open_rate"`
	CanCalculateQI        bool   `json:"can_calculate_qi"`
	CanCancelQI           bool   `json:"can_cancel_qi"`
	CanCorrectResults     bool   `json:"can_correct_results"`
}

// AdminGameStatusRequest is models.AdminGameStatusRequest.
type AdminGameStatusRequest struct {
	StatusID           int  `json:"status_id"`
	Force              bool `json:"force,omitempty"`
	ReturnFeeToPlayers bool `json:"return_fee_to_players,omitempty"`
}

// AdminGameEditorResponse is models.AdminGameEditorResponse.
type AdminGameEditorResponse struct {
	Game     *Game        `json:"game"`
	Authors  []GameAuthor `json:"authors"`
	Referees []GameAuthor `json:"referees"`
}

// GameCorrectionsResponse is models.GameCorrectionsResponse.
type GameCorrectionsResponse struct {
	GameID    int                      `json:"game_id"`
	GameNum   int                      `json:"game_num"`
	GameTitle string                   `json:"game_title"`
	CanAdd    bool                     `json:"can_add"`
	IsAdmin   bool                     `json:"is_admin"`
	Items     []GameCorrection         `json:"items"`
	Levels    []CorrectionLevelOption  `json:"levels"`
	Players   []CorrectionPlayerOption `json:"players"`
}

// GameCorrection is models.GameCorrection.
type GameCorrection struct {
	CorrectID       int    `json:"correct_id"`
	GameID          int    `json:"game_id"`
	CorrectDateTime string `json:"correct_date_time"`
	CorrectText     string `json:"correct_text"`
	Comment         string `json:"comment"`
	CorrectionType  int    `json:"correction_type"`
	CorrectionValue int    `json:"correction_value"`
	ValueText       string `json:"value_text"`
	LevelID         int    `json:"level_id"`
	LevelNum        int    `json:"level_num"`
	UserID          int    `json:"user_id"`
	Login           string `json:"login"`
	TeamID          int    `json:"team_id"`
	TeamName        string `json:"team_name"`
	GamePlayerID    int    `json:"game_player_id"`
	ByAdminID       int    `json:"by_admin_id"`
	ByAdminLogin    string `json:"by_admin_login"`
	CanEdit         bool   `json:"can_edit"`
}

// CorrectionLevelOption is models.CorrectionLevelOption.
type CorrectionLevelOption struct {
	LevelID  int    `json:"level_id"`
	LevelNum int    `json:"level_num"`
	Name     string `json:"name"`
}

// CorrectionPlayerOption is models.CorrectionPlayerOption.
type CorrectionPlayerOption struct {
	ID           int    `json:"id"`
	GamePlayerID int    `json:"game_player_id"`
	Label        string `json:"label"`
}

// GameCorrectionWriteRequest is models.GameCorrectionWriteRequest.
type GameCorrectionWriteRequest struct {
	PlayerID     int    `json:"player_id"`
	GamePlayerID int    `json:"game_player_id,omitempty"`
	LevelID      int    `json:"level_id"`
	IsBonus      bool   `json:"is_bonus"`
	Seconds      int    `json:"seconds"`
	Score        int    `json:"score,omitempty"`
	Comment      string `json:"comment"`
}

// GameMonitoringResponse is models.GameMonitoringResponse.
type GameMonitoringResponse struct {
	GameID     int                `json:"game_id"`
	GameNum    int                `json:"game_num"`
	GameTitle  string             `json:"game_title"`
	CanView    bool               `json:"can_view"`
	Mode       string             `json:"mode"`
	Page       int                `json:"page"`
	TotalPages int                `json:"total_pages"`
	TotalRows  int                `json:"total_rows"`
	Actions    []MonitoringAction `json:"actions"`
	Levels     []MonitoringLevel  `json:"levels"`
	Players    []MonitoringPlayer `json:"players"`
}

// MonitoringAction is models.MonitoringAction — one answer in the monitor.
type MonitoringAction struct {
	ActionID       int    `json:"action_id"`
	LevelID        int    `json:"level_id"`
	LevelNumber    int    `json:"level_number"`
	UserID         int    `json:"user_id"`
	UserLogin      string `json:"user_login"`
	TeamID         int    `json:"team_id"`
	TeamName       string `json:"team_name"`
	Answer         string `json:"answer"`
	AnswerDateTime string `json:"answer_date_time"`
	IsCorrect      bool   `json:"is_correct"`
	IsLevelPass    bool   `json:"is_level_pass"`
	SectorID       int    `json:"sector_id"`
	SectorsInfo    string `json:"sectors_info"`
	ScoresText     string `json:"scores_text"`
}

// MonitoringLevel is models.MonitoringLevelOption.
type MonitoringLevel struct {
	LevelID     int    `json:"level_id"`
	LevelNumber int    `json:"level_number"`
	Name        string `json:"name"`
}

// MonitoringPlayer is models.MonitoringPlayerOption.
type MonitoringPlayer struct {
	UserID   int    `json:"user_id"`
	Login    string `json:"login"`
	TeamID   int    `json:"team_id"`
	TeamName string `json:"team_name"`
}

// GameScenario is models.GameScenario — the structured scenario export that
// replaces the legacy GameScenario.aspx page.
type GameScenario struct {
	Game          *LocalizedGame     `json:"game"`
	IsClassicGame bool               `json:"is_classic_game"`
	Levels        []LevelScenario    `json:"levels"`
	LevelNumbers  []ScenarioLevelRef `json:"level_numbers"`
	Bonuses       []BonusScenario    `json:"whole_game_bonuses"`
	Error         *GameScenarioError `json:"error"`
}

// LocalizedGame is models.LocalizedGame, trimmed to what identifies the game.
type LocalizedGame struct {
	ID      int    `json:"id"`
	GameNum int    `json:"game_num"`
	Title   string `json:"title"`
}

// ScenarioLevelRef is models.LevelNumber.
type ScenarioLevelRef struct {
	Number int    `json:"number"`
	Name   string `json:"name"`
	Anchor string `json:"anchor"`
}

// GameScenarioError is models.GameScenarioError — why the export is unavailable.
type GameScenarioError struct {
	Type    string `json:"type"`
	Key     string `json:"key"`
	Message string `json:"message"`
}

// LevelScenario is models.LevelScenario.
type LevelScenario struct {
	LevelID               int                   `json:"level_id"`
	LevelNumber           int                   `json:"level_number"`
	LevelName             string                `json:"level_name"`
	Title                 string                `json:"title"`
	Comment               string                `json:"comment"`
	AutopassText          string                `json:"autopass_text"`
	SectorsCompletionRule string                `json:"sectors_completion_rule"`
	Tasks                 []LevelTaskScenario   `json:"tasks"`
	Sectors               []LevelSectorScenario `json:"sectors"`
	Answers               []LevelAnswerScenario `json:"answers"`
	Helps                 []LevelHelpScenario   `json:"helps"`
	PenaltyHelps          []LevelHelpScenario   `json:"penalty_helps"`
	Bonuses               []BonusScenario       `json:"bonuses"`
}

// LevelTaskScenario is models.LevelTaskScenario.
type LevelTaskScenario struct {
	TaskID   int    `json:"task_id"`
	TaskText string `json:"task_text"`
	TaskFor  string `json:"task_for"`
}

// LevelSectorScenario is models.LevelSectorScenario.
type LevelSectorScenario struct {
	SectorID    int                   `json:"sector_id"`
	SectorName  string                `json:"sector_name"`
	DisplayName string                `json:"display_name"`
	Answers     []LevelAnswerScenario `json:"answers"`
}

// LevelAnswerScenario is models.LevelAnswerScenario.
type LevelAnswerScenario struct {
	AnswerID   int    `json:"answer_id"`
	AnswerText string `json:"answer_text"`
	AnswerFor  string `json:"answer_for"`
}

// LevelHelpScenario is models.LevelHelpScenario.
type LevelHelpScenario struct {
	HelpID                int    `json:"help_id"`
	Title                 string `json:"title"`
	HelpText              string `json:"help_text"`
	Timeout               int    `json:"timeout"`
	IsPenalty             bool   `json:"is_penalty"`
	PenaltyTime           int    `json:"penalty_time"`
	PenaltyTimeText       string `json:"penalty_time_text"`
	PenaltyComment        string `json:"penalty_comment"`
	RequestPenaltyConfirm bool   `json:"request_penalty_confirm"`
}

// BonusScenario is models.BonusScenario.
type BonusScenario struct {
	BonusID       int      `json:"bonus_id"`
	BonusName     string   `json:"bonus_name"`
	Task          string   `json:"task"`
	BonusHelp     string   `json:"bonus_help"`
	Answers       []string `json:"answers"`
	BonusTime     int      `json:"bonus_time"`
	BonusTimeText string   `json:"bonus_time_text"`
	HasBonusTime  bool     `json:"has_bonus_time"`
	Delay         int      `json:"delay"`
	HasDelay      bool     `json:"has_delay"`
	LifeTime      int      `json:"life_time"`
	ValidFrom     string   `json:"valid_from"`
	ValidTo       string   `json:"valid_to"`
	Title         string   `json:"title"`
}
