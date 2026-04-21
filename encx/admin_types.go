package encx

// AdminLevel represents a level entry as seen in the admin level manager.
type AdminLevel struct {
	Number int    `json:"number"`
	Name   string `json:"name"`
	ID     int    `json:"id"`
}

// AdminLevelSettings holds the configuration of a level (autopass, answer block, attempts).
type AdminLevelSettings struct {
	// Autopass
	AutopassHours   int `json:"autopass_hours"`
	AutopassMinutes int `json:"autopass_minutes"`
	AutopassSeconds int `json:"autopass_seconds"`

	// Autopass penalty (timeout penalty)
	TimeoutPenalty bool `json:"timeout_penalty"`
	PenaltyHours   int  `json:"penalty_hours"`
	PenaltyMinutes int  `json:"penalty_minutes"`
	PenaltySeconds int  `json:"penalty_seconds"`

	// Answer block
	AttemptsNumber        int `json:"attempts_number"`
	AttemptsPeriodHours   int `json:"attempts_period_hours"`
	AttemptsPeriodMinutes int `json:"attempts_period_minutes"`
	AttemptsPeriodSeconds int `json:"attempts_period_seconds"`

	// Apply for: 0=team, 1=player
	ApplyForPlayer int `json:"apply_for_player"`
}

// AdminBonus holds the data for creating/editing a bonus in the admin panel.
type AdminBonus struct {
	Name     string   `json:"name"`
	Task     string   `json:"task"`
	Hint     string   `json:"hint"`
	LevelID  int      `json:"level_id"`
	Answers  []string `json:"answers"`
	BonusFor string   `json:"bonus_for"` // ddlBonusFor value

	// Award time
	AwardHours   int  `json:"award_hours"`
	AwardMinutes int  `json:"award_minutes"`
	AwardSeconds int  `json:"award_seconds"`
	Negative     bool `json:"negative"`

	// Absolute time limits
	ValidFrom string `json:"valid_from,omitempty"`
	ValidTo   string `json:"valid_to,omitempty"`

	// Delay before bonus becomes available
	DelayHours   int `json:"delay_hours,omitempty"`
	DelayMinutes int `json:"delay_minutes,omitempty"`
	DelaySeconds int `json:"delay_seconds,omitempty"`

	// Relative time limit (how long bonus is active)
	WorkHours   int `json:"work_hours,omitempty"`
	WorkMinutes int `json:"work_minutes,omitempty"`
	WorkSeconds int `json:"work_seconds,omitempty"`
}

// AdminSector holds the data for creating a sector in the admin panel.
type AdminSector struct {
	ID          int      `json:"id,omitempty"`
	Name        string   `json:"name"`
	Answers     []string `json:"answers"`
	ForMemberID string   `json:"for_member_id,omitempty"` // 0 = for all
}

// AdminHint holds the data for creating a hint in the admin panel.
type AdminHint struct {
	Text        string `json:"text"`
	Days        int    `json:"days"`
	Hours       int    `json:"hours"`
	Minutes     int    `json:"minutes"`
	Seconds     int    `json:"seconds"`
	ForMemberID string `json:"for_member_id,omitempty"`

	// Penalty hint fields
	IsPenalty      bool   `json:"is_penalty"`
	PenaltyHours   int    `json:"penalty_hours,omitempty"`
	PenaltyMinutes int    `json:"penalty_minutes,omitempty"`
	PenaltySeconds int    `json:"penalty_seconds,omitempty"`
	PenaltyComment string `json:"penalty_comment,omitempty"`
	RequestConfirm bool   `json:"request_confirm,omitempty"`
}

// AdminTask holds the data for creating a task in the admin panel.
type AdminTask struct {
	Text        string `json:"text"`
	ReplaceNl   bool   `json:"replace_nl"` // chkReplaceNlToBr
	ForMemberID string `json:"for_member_id,omitempty"`
}

// AdminTeam represents a team entry as seen in the admin panel.
type AdminTeam struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// AdminCorrection represents a bonus/penalty time correction entry.
type AdminCorrection struct {
	ID       string `json:"id"`
	DateTime string `json:"datetime"`
	Team     string `json:"team"`
	Level    string `json:"level"`
	Reason   string `json:"reason"`
	Time     string `json:"time"`
	Comment  string `json:"comment"`
}

// AdminGameInfo holds the data from the game editor page (GameEditor.aspx).
type AdminGameInfo struct {
	Title          string `json:"title"`
	Authors        string `json:"authors"`
	Description    string `json:"description"`
	Prize          string `json:"prize"`
	FinishDateTime string `json:"finish_datetime,omitempty"`
	RequestLastDate string `json:"request_last_date,omitempty"`
	IsModerated    bool   `json:"is_moderated"`

	// Visibility settings
	GameStatAvailability     string `json:"game_stat_availability,omitempty"`
	GameScenarioAvailability string `json:"game_scenario_availability,omitempty"`
	ShowFinishPlace          bool   `json:"show_finish_place"`

	// Limits
	MaxPlayers     string `json:"max_players,omitempty"`
	MaxTeamPlayers string `json:"max_team_players,omitempty"`

	// Rating/certificates
	ShowFee         string `json:"show_fee,omitempty"`
	CertificateMode string `json:"certificate_mode,omitempty"`
	FirstPlaces     string `json:"first_places,omitempty"`
	NotFirstPlaces  string `json:"not_first_places,omitempty"`
	AcceptRateMode  string `json:"accept_rate_mode,omitempty"`
	AcceptRateFrom  string `json:"accept_rate_from,omitempty"`
	AuthorComplexity string `json:"author_complexity,omitempty"`
}

// AdminCorrectionAdd holds the data for adding a new time correction.
type AdminCorrectionAdd struct {
	TeamName       string `json:"team_name"`
	LevelName      string `json:"level_name"` // "0" for all levels
	Comment        string `json:"comment"`
	CorrectionType string `json:"correction_type"` // "1" = bonus, "2" = penalty
	Days           string `json:"days"`
	Hours          string `json:"hours"`
	Minutes        string `json:"minutes"`
	Seconds        string `json:"seconds"`
}

// AdminActionMonitorEntry represents one row from the game action monitor.
type AdminActionMonitorEntry struct {
	Number      string `json:"number"`
	Participant string `json:"participant"`
	Direction   string `json:"direction,omitempty"`
	Answer      string `json:"answer"`
	DateTime    string `json:"datetime"`
	Sectors     string `json:"sectors,omitempty"`
}

// AdminGameMessage holds the data for creating/editing a game message in MessageEdit.aspx.
type AdminGameMessage struct {
	ID               int   `json:"id,omitempty"`
	Text             string `json:"text"`
	ReplaceNlToBr    bool   `json:"replace_nl_to_br"`
	ShowOnLevelsMode int    `json:"show_on_levels_mode,omitempty"` // 1=all, 2=chosen
	RequiredPoints   string `json:"required_points,omitempty"`
	LevelIDs         []int  `json:"level_ids,omitempty"`
}
