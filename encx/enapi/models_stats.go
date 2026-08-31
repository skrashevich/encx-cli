package enapi

// GameStatisticsResponse is models.GameStatisticsResponse.
type GameStatisticsResponse struct {
	GameID               int                        `json:"game_id"`
	GameNum              int                        `json:"game_num"`
	GameTitle            string                     `json:"game_title"`
	GameTypeID           int                        `json:"game_type_id"`
	ZoneID               int                        `json:"zone_id"`
	LevelsSequenceID     int                        `json:"levels_sequence_id"`
	TotalLevels          int                        `json:"total_levels"`
	Levels               []StatLevelMeta            `json:"levels"`
	LevelStats           map[string][]LevelStatItem `json:"level_stats"`
	LevelCorrections     []LevelCorrectionSum       `json:"level_corrections"`
	HideLevelsNames      bool                       `json:"hide_levels_names"`
	CanViewStats         bool                       `json:"can_view_stats"`
	IsGameAuthor         bool                       `json:"is_game_author"`
	AdminWarning         string                     `json:"admin_warning"`
	NeedsConfirm         bool                       `json:"needs_confirm"`
	ConfirmMessage       string                     `json:"confirm_message"`
	CurrentPage          int                        `json:"current_page"`
	TotalPages           int                        `json:"total_pages"`
	RowsPerPage          int                        `json:"rows_per_page"`
	SortField            string                     `json:"sort_field"`
	StatAvailabilityType int                        `json:"stat_availability_type"`
	HasCorrections       bool                       `json:"has_corrections"`
	IsWetWars            bool                       `json:"is_wet_wars"`
}

// StatLevelMeta is models.StatLevelMeta.
type StatLevelMeta struct {
	LevelID     int    `json:"level_id"`
	LevelNumber int    `json:"level_number"`
	LevelName   string `json:"level_name"`
	Dismissed   bool   `json:"dismissed"`
}

// LevelStatItem is models.LevelStatItem — one player's pass of one level.
type LevelStatItem struct {
	ActionID      int    `json:"action_id"`
	LevelID       int    `json:"level_id"`
	LevelNum      int    `json:"level_num"`
	LevelOrder    int    `json:"level_order"`
	Position      int    `json:"position"`
	UserID        int    `json:"user_id"`
	UserLogin     string `json:"user_login"`
	UserName      string `json:"user_name"`
	TeamID        int    `json:"team_id"`
	TeamName      string `json:"team_name"`
	GamePlayerID  int    `json:"game_player_id"`
	CityID        int    `json:"city_id"`
	CityName      string `json:"city_name"`
	SpentSeconds  int    `json:"spent_seconds"`
	Scores        int    `json:"scores"`
	EnterDateTime string `json:"enter_date_time"`
	// PassTypeID: 0 answered, 1 dismissed by an admin, 2 timeout autopass.
	PassTypeID int `json:"pass_type_id"`
}

// LevelCorrectionSum is models.LevelCorrectionSum.
type LevelCorrectionSum struct {
	LevelID         int `json:"level_id"`
	TeamID          int `json:"team_id"`
	UserID          int `json:"user_id"`
	CorrectionValue int `json:"correction_value"`
}

// User is models.User, trimmed to the fields the profile needs.
type User struct {
	ID              int     `json:"id"`
	Login           string  `json:"login"`
	FirstName       string  `json:"first_name"`
	LastName        string  `json:"last_name"`
	PatronymicName  string  `json:"patronymic_name"`
	Email           string  `json:"email"`
	EmailChecked    bool    `json:"email_checked"`
	GenderID        int     `json:"gender_id"`
	BirthDate       string  `json:"birth_date"`
	CityID          int     `json:"city_id"`
	CountryID       int     `json:"country_id"`
	ProvinceID      int     `json:"province_id"`
	TeamID          int     `json:"team_id"`
	Team            *Team   `json:"team"`
	Site            *Site   `json:"site"`
	Points          float64 `json:"points"`
	RankID          int     `json:"rank_id"`
	RankSentenceKey string  `json:"rank_sentence_key"`
	StatusID        int     `json:"status_id"`
	RegDateTime     string  `json:"reg_date_time"`
	LastVisit       string  `json:"last_visit"`
	IsSuperAdmin    bool    `json:"is_super_admin"`
	IsBlacklisted   bool    `json:"is_blacklisted"`
	AvatarURL       string  `json:"avatar_url"`
	VkID            string  `json:"vk_id"`
	FbID            string  `json:"fb_id"`
	GooID           string  `json:"goo_id"`
}

// Site is models.Site, trimmed to what identifies a site and its domains.
type Site struct {
	ID            int          `json:"id"`
	Name          string       `json:"name"`
	PrimaryDomain string       `json:"primary_domain"`
	Domains       []SiteDomain `json:"domains"`
	NetworkID     int          `json:"network_id"`
	City          *City        `json:"city"`
	Province      *Province    `json:"province"`
	Country       *Country     `json:"country"`
}

// SiteDomain is models.SiteDomain — one of the domains a site answers on.
type SiteDomain struct {
	ID        int    `json:"id"`
	SiteID    int    `json:"site_id"`
	Domain    string `json:"domain"`
	IsPrimary bool   `json:"is_primary"`
}

// City is models.City.
type City struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// Province is models.Province.
type Province struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// Country is models.Country.
type Country struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// Session is the answer of GET /auth/session. The backend documents it only as
// a free-form object, so both a flat user and a nested one are accepted.
type Session struct {
	UserID int    `json:"user_id"`
	ID     int    `json:"id"`
	Login  string `json:"login"`
	User   *User  `json:"user"`
}

// CurrentUserID returns the signed-in user's ID regardless of the shape used.
func (s *Session) CurrentUserID() int {
	if s == nil {
		return 0
	}
	if s.UserID > 0 {
		return s.UserID
	}
	if s.ID > 0 {
		return s.ID
	}
	if s.User != nil {
		return s.User.ID
	}
	return 0
}
