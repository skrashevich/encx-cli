// Package encx provides a Go client for the Encounter (en.cx) game engine JSON API.
//
// The Encounter platform is an international network of urban quest games.
// This package implements the full game engine API: authentication, game state polling,
// code submission, bonus codes, penalty hints, and game discovery.
package encx

// LoginResponse is the response from the /login/signin endpoint.
type LoginResponse struct {
	Error                int      `json:"Error"`
	Message              string   `json:"Message"`
	IpUnblockUrl         *string  `json:"IpUnblockUrl"`
	BruteForceUnblockUrl *string  `json:"BruteForceUnblockUrl"`
	ConfirmEmailUrl      *string  `json:"ConfirmEmailUrl"`
	CaptchaUrl           *string  `json:"CaptchaUrl"`
	AdminWhoCanActivate  []string `json:"AdminWhoCanActivate"`
}

// LoginOptions holds optional parameters for the Login request.
type LoginOptions struct {
	Network      int    // 1=Encounter (default), 2=QuestUa
	MagicNumbers string // CAPTCHA digits when Error==1
}

// GameModel is the full game state returned by the game engine.
type GameModel struct {
	Event             any            `json:"Event"`
	GameId            int            `json:"GameId"`
	GameNumber        int            `json:"GameNumber"`
	GameTitle         string         `json:"GameTitle"`
	GameTypeId        int            `json:"GameTypeId"`
	GameZoneId        int            `json:"GameZoneId"`
	LevelSequence     int            `json:"LevelSequence"`
	UserId            int            `json:"UserId"`
	TeamId            int            `json:"TeamId"`
	Login             string         `json:"Login"`
	TeamName          string         `json:"TeamName"`
	IsCaptain         bool           `json:"IsCaptain"`
	GameDateTimeStart string         `json:"GameDateTimeStart"`
	Levels            []LevelSummary `json:"Levels"`
	Level             *Level         `json:"Level"`
	EngineAction      *EngineAction  `json:"EngineAction"`
}

// Level represents a game level with its current state.
type Level struct {
	LevelId              int            `json:"LevelId"`
	Number               int            `json:"Number"`
	Name                 string         `json:"Name"`
	Timeout              int            `json:"Timeout"`
	TimeoutSecondsRemain int            `json:"TimeoutSecondsRemain"`
	TimeoutAward         int            `json:"TimeoutAward"`
	IsPassed             bool           `json:"IsPassed"`
	Dismissed            bool           `json:"Dismissed"`
	StartTime            *DateTime      `json:"StartTime"`
	HasAnswerBlockRule   bool           `json:"HasAnswerBlockRule"`
	BlockDuration        int            `json:"BlockDuration"`
	BlockTargetId        int            `json:"BlockTargetId"`
	AttemtsNumber        int            `json:"AttemtsNumber"`
	AttemtsPeriod        int            `json:"AttemtsPeriod"`
	RequiredSectorsCount int            `json:"RequiredSectorsCount"`
	PassedSectorsCount   int            `json:"PassedSectorsCount"`
	PassedBonusesCount   int            `json:"PassedBonusesCount"`
	SectorsLeftToClose   int            `json:"SectorsLeftToClose"`
	Tasks                []LevelTask    `json:"Tasks"`
	Task                 *LevelTask     `json:"Task"`
	Messages             []AdminMessage `json:"Messages"`
	Sectors              []Sector       `json:"Sectors"`
	Helps                []Help         `json:"Helps"`
	Bonuses              []Bonus        `json:"Bonuses"`
	PenaltyHelps         []Help         `json:"PenaltyHelps"`
	MixedActions         []CodeAction   `json:"MixedActions"`
}

// LevelSummary is a brief level entry as returned in the GameModel.Levels array.
type LevelSummary struct {
	LevelId     int          `json:"LevelId"`
	LevelNumber int          `json:"LevelNumber"`
	LevelName   string       `json:"LevelName"`
	Dismissed   bool         `json:"Dismissed"`
	IsPassed    bool         `json:"IsPassed"`
	Task        *LevelTask   `json:"Task"`
	LevelAction *ActionResult `json:"LevelAction"`
}

// LevelTask holds the task/assignment text for a level.
type LevelTask struct {
	TaskText          string `json:"TaskText"`
	TaskTextFormatted string `json:"TaskTextFormatted"`
	ReplaceNlToBr     bool   `json:"ReplaceNlToBr"`
}

// AdminMessage is a message from game organizers.
type AdminMessage struct {
	OwnerId     int    `json:"OwnerId"`
	OwnerLogin  string `json:"OwnerLogin"`
	MessageId   int    `json:"MessageId"`
	MessageText string `json:"MessageText"`
	WrappedText string `json:"WrappedText"`
	ReplaceNl2Br bool  `json:"ReplaceNl2Br"`
}

// Help represents a hint (regular or penalty). Both Helps and PenaltyHelps
// arrays in the API use the same structure.
type Help struct {
	HelpId           int     `json:"HelpId"`
	Number           int     `json:"Number"`
	HelpText         *string `json:"HelpText"`
	IsPenalty        bool    `json:"IsPenalty"`
	Penalty          int     `json:"Penalty"`
	PenaltyComment   *string `json:"PenaltyComment"`
	RequestConfirm   bool    `json:"RequestConfirm"`
	PenaltyHelpState int     `json:"PenaltyHelpState"` // 0=locked, 1=requested/opened, 2=confirmed
	RemainSeconds    int     `json:"RemainSeconds"`
	PenaltyMessage   *string `json:"PenaltyMessage"`
}

// Sector represents a sector within a level.
type Sector struct {
	SectorId   int    `json:"SectorId"`
	Order      int    `json:"Order"`
	Name       string `json:"Name"`
	IsAnswered bool   `json:"IsAnswered"`
	Answer     string `json:"Answer"`
}

// Bonus represents a bonus task within a level.
type Bonus struct {
	BonusId        int    `json:"BonusId"`
	Name           string `json:"Name"`
	Number         int    `json:"Number"`
	Task           string `json:"Task"`
	Help           string `json:"Help"`
	IsAnswered     bool   `json:"IsAnswered"`
	Answer         string `json:"Answer"`
	Expired        bool   `json:"Expired"`
	SecondsToStart int    `json:"SecondsToStart"`
	SecondsLeft    int    `json:"SecondsLeft"`
	AwardTime      int    `json:"AwardTime"`
	Negative       bool   `json:"Negative"`
}

// CodeAction represents a code entry in the action log.
type CodeAction struct {
	ActionId      int       `json:"ActionId"`
	LevelId       int       `json:"LevelId"`
	LevelNumber   int       `json:"LevelNumber"`
	UserId        int       `json:"UserId"`
	Kind          int       `json:"Kind"` // 1=level, 2=bonus
	Login         string    `json:"Login"`
	Answer        string    `json:"Answer"`
	AnswForm      *string   `json:"AnswForm"`
	EnterDateTime *DateTime `json:"EnterDateTime"`
	LocDateTime   string    `json:"LocDateTime"`
	IsCorrect     bool      `json:"IsCorrect"`
	Award         *Duration `json:"Award"`
	LocAward      *string   `json:"LocAward"`
	Penalty       int       `json:"Penalty"`
	Negative      bool      `json:"Negative"`
}

// EngineAction holds the result of the last game action.
type EngineAction struct {
	LevelNumber   int                  `json:"LevelNumber"`
	LevelAction   *ActionResult        `json:"LevelAction"`
	BonusAction   *ActionResult        `json:"BonusAction"`
	PenaltyAction *PenaltyActionResult `json:"PenaltyAction"`
	GameId        int                  `json:"GameId"`
	LevelId       int                  `json:"LevelId"`
}

// ActionResult indicates whether the last submitted answer was correct.
type ActionResult struct {
	Answer          *string `json:"Answer"`
	IsCorrectAnswer *bool   `json:"IsCorrectAnswer"`
}

// PenaltyActionResult holds the result of a penalty hint action.
type PenaltyActionResult struct {
	PenaltyId  int `json:"PenaltyId"`
	ActionType int `json:"ActionType"` // 0=none, 1=request
}

// DomainGame represents a game listed on a domain's main page (from HTML scraping).
type DomainGame struct {
	Title  string `json:"title"`
	GameId int    `json:"gameId"`
}

// GameListResponse is the JSON response from GET /home/?json=1.
type GameListResponse struct {
	ComingGames []GameInfo `json:"ComingGames"`
	ActiveGames []GameInfo `json:"ActiveGames"`
}

// GameInfo holds full game metadata returned by the /home/?json=1 endpoint.
type GameInfo struct {
	GameID                int       `json:"GameID"`
	GameNum               int       `json:"GameNum"`
	SiteID                int       `json:"SiteID,omitempty"`
	LangID                int       `json:"LangID,omitempty"`
	CompetitionID         int       `json:"CompetitionID,omitempty"`
	OwnerID               int       `json:"OwnerID,omitempty"`
	LevelNumber           int       `json:"LevelNumber,omitempty"`
	CreateDateTime        *DateTime `json:"CreateDateTime"`
	StartDateTime         *DateTime `json:"StartDateTime"`
	FinishDateTime        *DateTime `json:"FinishDateTime"`
	Title                 string    `json:"Title"`
	Descr                 string    `json:"Descr"`
	DescrWrapped          string    `json:"DescrWrapped,omitempty"`
	GameTypeID            int       `json:"GameTypeID"`            // 0=single, 1=team, 2=personal
	ZoneId                int       `json:"ZoneId"`                // 0=quest, 1=brainstorm, 2=photohunt, etc.
	LevelsSequence        int       `json:"LevelsSequence,omitempty"`
	ScenarioAvailability  int       `json:"ScenarioAvailability,omitempty"`
	MaxPlayers            int       `json:"MaxPlayers"`
	MaxTeamMembers        int       `json:"MaxTeamMembers"`
	ShowInCalendar        bool      `json:"ShowInCalendar"`
	FeeType               int       `json:"FeeType"`
	FeeCurrencyId         int       `json:"FeeCurrencyId"`
	FeeName               string    `json:"FeeName"`
	ShowFee               int       `json:"ShowFee"`
	Fee                   *Money    `json:"Fee"`
	Prize                 *Money    `json:"Prize"`
	PrizeType             int       `json:"PrizeType,omitempty"`
	PrizeTypeSymbol       string    `json:"PrizeTypeSymbol,omitempty"`
	TSRemain              *Duration `json:"TSRemain"`
	Started               bool      `json:"Started"`
	Finished              bool      `json:"Finished"`
	InProgress            bool      `json:"InProgress"`
	IsSectorsSupported    bool      `json:"IsSectorsSupported,omitempty"`
	IsOnlineStatAvailable bool      `json:"IsOnlineStatAvailable,omitempty"`
	IsComplexitySupported bool      `json:"IsComplexitySupported,omitempty"`
	IsModerated           bool      `json:"IsModerated,omitempty"`
	ComplexityFactor      int       `json:"ComplexityFactor,omitempty"`
	ComplexityMembersFactor int     `json:"ComplexityMembersFactor,omitempty"`
	QualityRate           int       `json:"QualityRate,omitempty"`
	QualityRateFormatted  string    `json:"QualityRateFormatted,omitempty"`
	TopicId               int       `json:"TopicId,omitempty"`
	AcceptRateFromDateTime *DateTime `json:"AcceptRateFromDateTime,omitempty"`
	RequestLastDate       *DateTime `json:"RequestLastDate,omitempty"`
	HideLevelsNames       bool      `json:"HideLevelsNames,omitempty"`
	AlwaysAvailable       bool      `json:"AlwaysAvailable,omitempty"`
	PublicAccess          bool      `json:"PublicAccess,omitempty"`
	DisplayMonitoring     int       `json:"DisplayMonitoring,omitempty"`
}

// DateTime represents a date-time value as returned by the EN API.
type DateTime struct {
	Value     float64 `json:"Value"`
	Timestamp int64   `json:"Timestamp"`
}

// Money represents a monetary value (fee or prize) as returned by the EN API.
type Money struct {
	Cents                      int    `json:"Cents"`
	Value                      int    `json:"Value"`
	Formated                   string `json:"Formated"`
	FormatedFull               string `json:"FormatedFull,omitempty"`
	DefaultCultureFormated     string `json:"DefaultCultureFormated,omitempty"`
	DefaultCultureShortFormated string `json:"DefaultCultureShortFormated,omitempty"`
}

// Duration represents a time duration as returned by the EN API.
type Duration struct {
	Ticks             int64   `json:"Ticks,omitempty"`
	Days              int     `json:"Days"`
	Hours             int     `json:"Hours"`
	Milliseconds      int     `json:"Milliseconds,omitempty"`
	Minutes           int     `json:"Minutes"`
	Seconds           int     `json:"Seconds"`
	TotalDays         float64 `json:"TotalDays,omitempty"`
	TotalHours        float64 `json:"TotalHours,omitempty"`
	TotalMilliseconds float64 `json:"TotalMilliseconds,omitempty"`
	TotalMinutes      float64 `json:"TotalMinutes,omitempty"`
	TotalSeconds      float64 `json:"TotalSeconds"`
}

// GameStatisticsResponse is the JSON response from GET /gamestatistics/full/{gameId}?json=1.
type GameStatisticsResponse struct {
	Game               *GameInfo          `json:"Game"`
	Level              *LevelStatInfo     `json:"Level"`
	StatItems          [][]StatItem       `json:"StatItems"`
	Levels             []LevelStatInfo    `json:"Levels"`
	IsLevelNamesVisible bool              `json:"IsLevelNamesVisible"`
	LevelPlayers       []LevelPlayerCount `json:"LevelPlayers"`
	User               *UserProfile       `json:"User"`
	PagerVisible       bool               `json:"PagerVisible"`
	ShowAdminWarning   bool               `json:"ShowAdminWarning"`
}

// StatItem represents a single team/player entry in the game statistics.
type StatItem struct {
	ActionTime     *DateTime `json:"ActionTime"`
	UserId         int       `json:"UserId"`
	LevelId        int       `json:"LevelId"`
	TeamId         int       `json:"TeamId"`
	UserName       string    `json:"UserName"`
	TeamName       string    `json:"TeamName"`
	LevelNum       int       `json:"LevelNum"`
	SpentSeconds   int       `json:"SpentSeconds"`
	LevelOrder     int       `json:"LevelOrder"`
	SpentLevelTime *Duration `json:"SpentLevelTime"`
	PassType       int       `json:"PassType"`
	Corrections    *Duration `json:"Corrections"`
	Scores         int       `json:"Scores"`
}

// LevelStatInfo holds level metadata used in game statistics.
type LevelStatInfo struct {
	LevelId       int    `json:"LevelId"`
	LevelNumber   int    `json:"LevelNumber"`
	LevelName     string `json:"LevelName"`
	Dismissed     bool   `json:"Dismissed"`
	PassedPlayers int    `json:"PassedPlayers"`
}

// LevelPlayerCount holds the number of players who reached a given level.
type LevelPlayerCount struct {
	LevelNum int `json:"LevelNum"`
	Count    int `json:"Count"`
}

// UserProfile represents the authenticated user's profile as returned in game statistics.
type UserProfile struct {
	ID              int       `json:"ID"`
	Login           string    `json:"Login"`
	FirstName       string    `json:"FirstName"`
	PatronymicName  string    `json:"PatronymicName"`
	LastName        string    `json:"LastName"`
	Email           string    `json:"Email"`
	EmailChecked    bool      `json:"EmailChecked"`
	GenderID        int       `json:"GenderID"` // 1=male, 2=female
	BirthDate       *DateTime `json:"BirthDate"`
	CityId          int       `json:"CityId"`
	CountryId       int       `json:"CountryId"`
	ProvinceId      int       `json:"ProvinceId"`
	TeamID          int       `json:"TeamID"`
	ParentID        int       `json:"ParentID"`
	SiteId          int       `json:"SiteId"`
	IsActive        bool      `json:"IsActive"`
	RegDateTime     *DateTime `json:"RegDateTime"`
	Points          float64   `json:"Points"`
	BonusPoints     float64   `json:"BonusPoints"`
	RankID          int       `json:"RankID"`
	StatusId        int       `json:"StatusId"`
	Network         int       `json:"Network"`
	LastVisitTime   *DateTime `json:"LastVisitTime"`
	VkId            *string   `json:"VkId"`
	FbId            *string   `json:"FbId"`
	TgId            *string   `json:"TgId"`
	GooId           *string   `json:"GooId"`
	IsSuperAdmin    bool      `json:"IsSuperAdmin"`
	BlockByIP       bool      `json:"BlockByIP"`
}
