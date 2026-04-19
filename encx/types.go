// Package encx provides a Go client for the Encounter (en.cx) game engine JSON API.
//
// The Encounter platform is an international network of urban quest games.
// This package implements the full game engine API: authentication, game state polling,
// code submission, bonus codes, penalty hints, and game discovery.
package encx

// LoginResponse is the response from the /login/signin endpoint.
type LoginResponse struct {
	Error   int    `json:"Error"`
	Message string `json:"Message"`
}

// GameModel is the full game state returned by the game engine.
type GameModel struct {
	Event         any     `json:"Event"`
	GameId        int     `json:"GameId"`
	UserId        int     `json:"UserId"`
	LevelSequence int     `json:"LevelSequence"`
	Levels        []Level `json:"Levels"`
	Level         *Level  `json:"Level"`
	EngineAction  *EngineAction `json:"EngineAction"`
}

// Level represents a game level with its current state.
type Level struct {
	LevelId             int           `json:"LevelId"`
	Number              int           `json:"Number"`
	Name                string        `json:"Name"`
	Timeout             int           `json:"Timeout"`
	TimeoutSecondsRemain int          `json:"TimeoutSecondsRemain"`
	TimeoutAward        int           `json:"TimeoutAward"`
	SectorsLeftToClose  int           `json:"SectorsLeftToClose"`
	RequiredSectorsCount int          `json:"RequiredSectorsCount"`
	Sectors             []Sector      `json:"Sectors"`
	Bonuses             []Bonus       `json:"Bonuses"`
	PenaltyHelps        []PenaltyHelp `json:"PenaltyHelps"`
	MixedActions        []CodeAction  `json:"MixedActions"`
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
	BonusId    int    `json:"BonusId"`
	Name       string `json:"Name"`
	Task       string `json:"Task"`
	Help       string `json:"Help"`
	IsAnswered bool   `json:"IsAnswered"`
	Answer     string `json:"Answer"`
}

// PenaltyHelp represents a penalty hint that can be requested at a time cost.
type PenaltyHelp struct {
	PenaltyHelpId  int    `json:"PenaltyHelpId"`
	Number         int    `json:"Number"`
	RemainSeconds  int    `json:"RemainSeconds"`
	PenaltyComment string `json:"PenaltyComment"`
	Penalty        int    `json:"Penalty"`
}

// CodeAction represents a code entry in the action log.
type CodeAction struct {
	ActionId    int    `json:"ActionId"`
	UserId      int    `json:"UserId"`
	Login       string `json:"Login"`
	Answer      string `json:"Answer"`
	LocDateTime string `json:"LocDateTime"`
	IsCorrect   bool   `json:"IsCorrect"`
	Kind        int    `json:"Kind"`
}

// EngineAction holds the result of the last game action.
type EngineAction struct {
	LevelAction *ActionResult `json:"LevelAction"`
	BonusAction *ActionResult `json:"BonusAction"`
}

// ActionResult indicates whether the last submitted answer was correct.
type ActionResult struct {
	IsCorrectAnswer *bool `json:"IsCorrectAnswer"`
}

// DomainGame represents a game listed on a domain's main page.
type DomainGame struct {
	Title  string `json:"title"`
	GameId int    `json:"gameId"`
}
