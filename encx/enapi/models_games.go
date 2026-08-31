package enapi

import "encoding/json"

// Game is models.Game — the catalog entry the new engine returns for every
// game listing. Fields the client does not map are omitted deliberately.
type Game struct {
	ID                       int             `json:"id"`
	GameNum                  int             `json:"game_num"`
	SiteID                   int             `json:"site_id"`
	LangID                   int             `json:"lang_id"`
	OwnerID                  int             `json:"owner_id"`
	CompetitionID            int             `json:"competition_id"`
	LevelNumber              int             `json:"level_number"`
	Title                    string          `json:"title"`
	Descr                    string          `json:"descr"`
	GameTypeID               int             `json:"game_type_id"`
	ZoneID                   int             `json:"zone_id"`
	StatusID                 int             `json:"status_id"`
	LevelsSequenceID         int             `json:"levels_sequence_id"`
	ScenarioAvailability     int             `json:"scenario_availability"`
	MaxPlayers               int             `json:"max_players"`
	MaxTeamMembers           int             `json:"max_team_members"`
	TopicID                  int             `json:"topic_id"`
	CreateDateTime           string          `json:"create_date_time"`
	StartDateTime            string          `json:"start_date_time"`
	FinishDateTime           string          `json:"finish_date_time"`
	RequestLastDate          string          `json:"request_last_date"`
	AcceptRateFromDateTime   string          `json:"accept_rate_from_date_time"`
	Fee                      int             `json:"fee"`
	Prize                    int             `json:"prize"`
	Price                    int             `json:"price"`
	FeeName                  string          `json:"fee_name"`
	FeeTypeID                int             `json:"fee_type_id"`
	FeeCurrencyID            int             `json:"fee_currency_id"`
	PrizeType                int             `json:"prize_type"`
	ShowFee                  int             `json:"show_fee"`
	Started                  bool            `json:"started"`
	Finished                 bool            `json:"finished"`
	IsModerated              bool            `json:"is_moderated"`
	IsAvailableAfterFinished bool            `json:"is_available_after_finished"`
	StatAvailabilityTypeID   int             `json:"stat_availability_type_id"`
	QualityRate              json.Number     `json:"quality_rate"`
	QualityRateCalculated    bool            `json:"quality_rate_calculated"`
	AuthorIndexCalculated    bool            `json:"author_index_calculated"`
	AFC                      float64         `json:"afc"`
	ShowInCalendar           bool            `json:"show_in_calendar"`
	ShowFinishPlace          bool            `json:"show_finish_place"`
	HideLevelsNames          bool            `json:"hide_levels_names"`
	HidePlayersList          bool            `json:"hide_players_list"`
	HideGameDescr            bool            `json:"hide_game_descr"`
	ReplaceNlToBr            bool            `json:"replace_nl_to_br"`
	PublicAccess             bool            `json:"public_access"`
	RateClosed               bool            `json:"rate_closed"`
	AllowMakeStakes          bool            `json:"allow_make_stakes"`
	DisplayMonitoring        int             `json:"display_monitoring"`
	DisplayAnnouncement      int             `json:"display_announcement"`
	CertificatePlaces        int             `json:"certificate_places"`
	CertificateAccessMode    int             `json:"certificate_access_mode"`
	ForUserID                int             `json:"for_user_id"`
	PrimaryDomain            string          `json:"primary_domain"`
	Authors                  []GameAuthor    `json:"authors"`
	Team                     *Team           `json:"team"`
	Zone                     *Zone           `json:"zone"`
	Raw                      json.RawMessage `json:"-"`
}

// GameAuthor is models.GameAuthor.
type GameAuthor struct {
	UserID      int     `json:"user_id"`
	Login       string  `json:"login"`
	Name        string  `json:"name"`
	AvatarURL   string  `json:"avatar_url"`
	GenderID    int     `json:"gender_id"`
	CommonIndex float64 `json:"common_index"`
}

// Team is models.Team.
type Team struct {
	ID           int     `json:"id"`
	Name         string  `json:"name"`
	CaptainID    int     `json:"captain_id"`
	CreateDate   string  `json:"create_date"`
	FlagURL      string  `json:"flag_url"`
	HymnURL      string  `json:"hymn_url"`
	HasHymn      bool    `json:"has_hymn"`
	ForumLink    string  `json:"forum_link"`
	WebSite      string  `json:"web_site"`
	Points       float64 `json:"points"`
	FlagFileName string  `json:"flag_file_name"`
	HymnFileName string  `json:"hymn_file_name"`
}

// Zone is models.Zone.
type Zone struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// GamesResponse is models.GamesResponse — a paged game listing.
type GamesResponse struct {
	Items      []Game `json:"items"`
	TotalCount int    `json:"total_count"`
}

// HomeGamesResponse is models.HomeGamesResponse, trimmed to the catalog blocks.
type HomeGamesResponse struct {
	ComingGames []Game `json:"coming_games"`
	ActiveGames []Game `json:"active_games"`
}

// GameDetails is models.GameDetails.
type GameDetails struct {
	Game          *Game            `json:"game"`
	Authors       []GameAuthor     `json:"authors"`
	CanManageGame bool             `json:"can_manage_game"`
	FeeName       string           `json:"fee_name"`
	StatisticLink string           `json:"statistic_link"`
	GuestbookLink string           `json:"guestbook_link"`
	Winners       []GameWinner     `json:"winners"`
	TotalWinners  int              `json:"total_winners"`
	PlayerStats   *json.RawMessage `json:"player_stats"`
}

// GameWinner is models.Winner.
type GameWinner struct {
	Place     int     `json:"place"`
	UserID    int     `json:"user_id"`
	Login     string  `json:"login"`
	TeamID    int     `json:"team_id"`
	TeamName  string  `json:"team_name"`
	Points    float64 `json:"points"`
	BestTime  string  `json:"best_time"`
	FinalTime string  `json:"final_time"`
}

// GameFeeBox is models.GameFeeBoxModel — the join block of a game page. It
// carries the countdown the legacy engine only exposed as StartCounter in HTML.
type GameFeeBox struct {
	Game           *Game  `json:"game"`
	Status         string `json:"status"`
	CanEnter       bool   `json:"can_enter"`
	CanMakeFee     bool   `json:"can_make_fee"`
	CanDismiss     bool   `json:"can_dismiss"`
	FeeAccepted    bool   `json:"fee_accepted"`
	FeeText        string `json:"fee_text"`
	HasRequest     bool   `json:"has_request"`
	SecondsToStart int    `json:"seconds_to_start"`
	ShowTimer      bool   `json:"show_timer"`
	ShowEnterBox   bool   `json:"show_enter_box"`
	TeamName       string `json:"team_name"`
	EnterGameLink  string `json:"enter_game_link"`
}

// GameJoinResponse is models.GameJoinResponse — the answer to make-fee.
type GameJoinResponse struct {
	Success                 bool   `json:"success"`
	Message                 string `json:"message"`
	FeeAccepted             bool   `json:"fee_accepted"`
	FeeAcceptedText         string `json:"fee_accepted_text"`
	ShowPointsGameAttention bool   `json:"show_points_game_attention"`
	PointsGameAttentionText string `json:"points_game_attention_text"`
}

// TeamsListResponse is models.TeamsListResponse — a paged team search.
type TeamsListResponse struct {
	Items      []TeamListItem `json:"items"`
	TotalCount int            `json:"total_count"`
	TotalPages int            `json:"total_pages"`
	Page       int            `json:"page"`
	PageSize   int            `json:"page_size"`
	SortField  string         `json:"sort_field"`
	Mode       string         `json:"mode"`
}

// TeamListItem is models.TeamListItem.
type TeamListItem struct {
	ID           int     `json:"id"`
	Name         string  `json:"name"`
	CaptainID    int     `json:"captain_id"`
	CaptainLogin string  `json:"captain_login"`
	CreateDate   string  `json:"create_date"`
	Points       float64 `json:"points"`
	MembersCount int     `json:"members_count"`
	SiteID       int     `json:"site_id"`
}

// TeamPreview is models.TeamPreview — a team named in an invitation or request.
type TeamPreview struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	CaptainID int    `json:"captain_id"`
}

// InvitationResponseRequest is models.InvitationResponseRequest.
type InvitationResponseRequest struct {
	Accept bool `json:"accept"`
}

// TeamUpdateRequest is models.TeamUpdateRequest. PUT /teams/{id} replaces all
// three fields, so callers must send the current values of the ones they keep.
type TeamUpdateRequest struct {
	Name      string `json:"name"`
	WebSite   string `json:"web_site"`
	ForumLink string `json:"forum_link"`
}

// TeamInviteRequest is the body of POST /teams/{id}/invitations, which the API
// document describes only in prose ("пригласить в команду по логину").
type TeamInviteRequest struct {
	Login string `json:"login"`
}
