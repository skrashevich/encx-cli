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
	Event             any           `json:"Event"`
	GameId            int           `json:"GameId"`
	GameNumber        int           `json:"GameNumber"`
	GameTitle         string        `json:"GameTitle"`
	LevelSequence     int           `json:"LevelSequence"`
	UserId            int           `json:"UserId"`
	TeamId            int           `json:"TeamId"`
	Login             string        `json:"Login"`
	TeamName          string        `json:"TeamName"`
	GameDateTimeStart string        `json:"GameDateTimeStart"`
	Levels            []Level       `json:"Levels"`
	Level             *Level        `json:"Level"`
	EngineAction      *EngineAction `json:"EngineAction"`
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
	StartTime            string         `json:"StartTime"`
	HasAnswerBlockRule   bool           `json:"HasAnswerBlockRule"`
	BlockDuration        int            `json:"BlockDuration"`
	BlockTargetId        int            `json:"BlockTargetId"`
	AttemtsNumber        int            `json:"AttemtsNumber"`
	AttemtsPeriod        int            `json:"AttemtsPeriod"`
	RequiredSectorsCount int            `json:"RequiredSectorsCount"`
	PassedSectorsCount   int            `json:"PassedSectorsCount"`
	SectorsLeftToClose   int            `json:"SectorsLeftToClose"`
	Task                 *LevelTask     `json:"Task"`
	Messages             []AdminMessage `json:"Messages"`
	Sectors              []Sector       `json:"Sectors"`
	Helps                []Help         `json:"Helps"`
	Bonuses              []Bonus        `json:"Bonuses"`
	PenaltyHelps         []PenaltyHelp  `json:"PenaltyHelps"`
	MixedActions         []CodeAction   `json:"MixedActions"`
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

// Help represents a regular (non-penalty) hint.
type Help struct {
	HelpId        int    `json:"HelpId"`
	Number        int    `json:"Number"`
	HelpText      string `json:"HelpText"`
	RemainSeconds int    `json:"RemainSeconds"`
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

// PenaltyHelp represents a penalty hint that can be requested at a time cost.
type PenaltyHelp struct {
	PenaltyHelpId    int    `json:"PenaltyHelpId"`
	Number           int    `json:"Number"`
	HelpText         string `json:"HelpText"`
	IsPenalty        bool   `json:"IsPenalty"`
	Penalty          int    `json:"Penalty"`
	PenaltyComment   string `json:"PenaltyComment"`
	RequestConfirm   bool   `json:"RequestConfirm"`
	PenaltyHelpState int    `json:"PenaltyHelpState"` // 0=locked, 2=opened
	RemainSeconds    int    `json:"RemainSeconds"`
}

// CodeAction represents a code entry in the action log.
type CodeAction struct {
	ActionId      int    `json:"ActionId"`
	LevelId       int    `json:"LevelId"`
	LevelNumber   int    `json:"LevelNumber"`
	UserId        int    `json:"UserId"`
	Kind          int    `json:"Kind"` // 1=level, 2=bonus
	Login         string `json:"Login"`
	Answer        string `json:"Answer"`
	AnswForm      string `json:"AnswForm"`
	EnterDateTime string `json:"EnterDateTime"`
	LocDateTime   string `json:"LocDateTime"`
	IsCorrect     bool   `json:"IsCorrect"`
}

// EngineAction holds the result of the last game action.
type EngineAction struct {
	LevelAction *ActionResult `json:"LevelAction"`
	BonusAction *ActionResult `json:"BonusAction"`
}

// ActionResult indicates whether the last submitted answer was correct.
type ActionResult struct {
	Answer          string `json:"Answer"`
	IsCorrectAnswer *bool  `json:"IsCorrectAnswer"`
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
	GameID         int       `json:"GameID"`
	GameNum        int       `json:"GameNum"`
	CreateDateTime *DateTime `json:"CreateDateTime"`
	StartDateTime  *DateTime `json:"StartDateTime"`
	FinishDateTime *DateTime `json:"FinishDateTime"`
	Title          string    `json:"Title"`
	Descr          string    `json:"Descr"`
	GameTypeID     int       `json:"GameTypeID"` // 0=single, 1=team, 2=personal
	ZoneId         int       `json:"ZoneId"`     // 0=quest, 1=brainstorm, 2=photohunt, etc.
	MaxPlayers     int       `json:"MaxPlayers"`
	MaxTeamMembers int       `json:"MaxTeamMembers"`
	ShowInCalendar bool      `json:"ShowInCalendar"`
	FeeType        int       `json:"FeeType"`
	FeeCurrencyId  int       `json:"FeeCurrencyId"`
	FeeName        string    `json:"FeeName"`
	ShowFee        int       `json:"ShowFee"`
	Fee            *Money    `json:"Fee"`
	Prize          *Money    `json:"Prize"`
	TSRemain       *Duration `json:"TSRemain"`
	Started        bool      `json:"Started"`
	Finished       bool      `json:"Finished"`
	InProgress     bool      `json:"InProgress"`
}

// DateTime represents a date-time value as returned by the EN API.
type DateTime struct {
	Value     float64 `json:"Value"`
	Timestamp int64   `json:"Timestamp"`
}

// Money represents a monetary value (fee or prize) as returned by the EN API.
type Money struct {
	Cents    int    `json:"Cents"`
	Value    int    `json:"Value"`
	Formated string `json:"Formated"`
}

// Duration represents a time duration as returned by the EN API.
type Duration struct {
	Days         int     `json:"Days"`
	Hours        int     `json:"Hours"`
	Minutes      int     `json:"Minutes"`
	Seconds      int     `json:"Seconds"`
	TotalSeconds float64 `json:"TotalSeconds"`
}
