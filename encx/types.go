// Package encx provides a Go client for the Encounter (en.cx) game engine JSON API.
//
// The Encounter platform is an international network of urban quest games.
// This package implements the full game engine API: authentication, game state polling,
// code submission, bonus codes, penalty hints, and game discovery.
package encx

import "encoding/json"

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
	Task                 *LevelTask     `json:"Task,omitempty"`
	Messages             []AdminMessage `json:"Messages"`
	Sectors              []Sector       `json:"Sectors"`
	Helps                []Help         `json:"Helps"`
	Bonuses              []Bonus        `json:"Bonuses"`
	PenaltyHelps         []Help         `json:"PenaltyHelps"`
	MixedActions         []CodeAction   `json:"MixedActions"`
}

// CanSubmitLevelAnswer reports whether the documented level state allows
// submitting LevelAction.Answer. BonusAction.Answer is not blocked by this rule.
func (l *Level) CanSubmitLevelAnswer() bool {
	if l == nil {
		return false
	}
	if l.IsPassed || l.Dismissed {
		return false
	}
	return !l.HasAnswerBlockRule || l.BlockDuration <= 0
}

// LevelSummary is a brief level entry as returned in the GameModel.Levels array.
type LevelSummary struct {
	LevelId     int           `json:"LevelId"`
	LevelNumber int           `json:"LevelNumber"`
	LevelName   string        `json:"LevelName"`
	Dismissed   bool          `json:"Dismissed"`
	IsPassed    bool          `json:"IsPassed"`
	Task        *LevelTask    `json:"Task"`
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
	OwnerId      int    `json:"OwnerId"`
	OwnerLogin   string `json:"OwnerLogin"`
	MessageId    int    `json:"MessageId"`
	MessageText  string `json:"MessageText"`
	WrappedText  string `json:"WrappedText"`
	ReplaceNl2Br bool   `json:"ReplaceNl2Br"`
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
	SectorId   int        `json:"SectorId"`
	Order      int        `json:"Order"`
	Name       string     `json:"Name"`
	IsAnswered bool       `json:"IsAnswered"`
	Answer     FlexString `json:"Answer"`

	raw *sectorRawState
}

type sectorRawState struct {
	answer         json.RawMessage
	originalAnswer FlexString
}

// RawAnswerJSON returns a copy of Answer in its original JSON form.
func (s Sector) RawAnswerJSON() json.RawMessage {
	if s.raw == nil {
		return nil
	}
	return append(json.RawMessage(nil), s.raw.answer...)
}

// UnmarshalJSON decodes a sector while retaining its original Answer JSON.
func (s *Sector) UnmarshalJSON(data []byte) error {
	type sectorJSON Sector
	var value sectorJSON
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	var raw struct {
		Answer json.RawMessage `json:"Answer"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*s = Sector(value)
	s.raw = &sectorRawState{
		answer:         append(json.RawMessage(nil), raw.Answer...),
		originalAnswer: s.Answer,
	}
	return nil
}

// MarshalJSON encodes a sector while preserving an Answer JSON decoded from the API.
func (s Sector) MarshalJSON() ([]byte, error) {
	type sectorJSON Sector
	value := sectorJSON(s)
	var answer json.RawMessage
	if s.raw != nil {
		answer = s.raw.answer
	}
	if len(answer) == 0 || s.raw == nil || s.Answer != s.raw.originalAnswer {
		var err error
		answer, err = json.Marshal(s.Answer)
		if err != nil {
			return nil, err
		}
	}
	return json.Marshal(struct {
		*sectorJSON
		Answer json.RawMessage `json:"Answer"`
	}{
		sectorJSON: &value,
		Answer:     answer,
	})
}

// Bonus represents a bonus task within a level.
type Bonus struct {
	BonusId        int        `json:"BonusId"`
	Name           string     `json:"Name"`
	Number         int        `json:"Number"`
	Task           string     `json:"Task"`
	Help           string     `json:"Help"`
	IsAnswered     bool       `json:"IsAnswered"`
	Answer         FlexString `json:"Answer"`
	Expired        bool       `json:"Expired"`
	SecondsToStart int        `json:"SecondsToStart"`
	SecondsLeft    int        `json:"SecondsLeft"`
	AwardTime      int        `json:"AwardTime"`
	Negative       bool       `json:"Negative"`

	raw *bonusRawState
}

type bonusRawState struct {
	answer         json.RawMessage
	originalAnswer FlexString
	name           json.RawMessage
	originalName   string
	task           json.RawMessage
	originalTask   string
	help           json.RawMessage
	originalHelp   string
}

// RawAnswerJSON returns a copy of Answer in its original JSON form.
func (b Bonus) RawAnswerJSON() json.RawMessage {
	if b.raw == nil {
		return nil
	}
	return append(json.RawMessage(nil), b.raw.answer...)
}

// UnmarshalJSON decodes a bonus while retaining its original Answer JSON.
func (b *Bonus) UnmarshalJSON(data []byte) error {
	type bonusJSON Bonus
	var value bonusJSON
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	var raw struct {
		Answer json.RawMessage `json:"Answer"`
		Name   json.RawMessage `json:"Name"`
		Task   json.RawMessage `json:"Task"`
		Help   json.RawMessage `json:"Help"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*b = Bonus(value)
	b.raw = &bonusRawState{
		answer:         append(json.RawMessage(nil), raw.Answer...),
		originalAnswer: b.Answer,
		name:           append(json.RawMessage(nil), raw.Name...),
		originalName:   b.Name,
		task:           append(json.RawMessage(nil), raw.Task...),
		originalTask:   b.Task,
		help:           append(json.RawMessage(nil), raw.Help...),
		originalHelp:   b.Help,
	}
	return nil
}

// MarshalJSON encodes a bonus while preserving an Answer JSON decoded from the API.
func (b Bonus) MarshalJSON() ([]byte, error) {
	type bonusJSON Bonus
	value := bonusJSON(b)
	var answer, name, task, help json.RawMessage
	if b.raw != nil {
		answer = b.raw.answer
		name = b.raw.name
		task = b.raw.task
		help = b.raw.help
	}
	if len(answer) == 0 || b.raw == nil || b.Answer != b.raw.originalAnswer {
		var err error
		answer, err = json.Marshal(b.Answer)
		if err != nil {
			return nil, err
		}
	}
	if len(name) == 0 || b.raw == nil || b.Name != b.raw.originalName {
		name, _ = json.Marshal(b.Name)
	}
	if len(task) == 0 || b.raw == nil || b.Task != b.raw.originalTask {
		task, _ = json.Marshal(b.Task)
	}
	if len(help) == 0 || b.raw == nil || b.Help != b.raw.originalHelp {
		help, _ = json.Marshal(b.Help)
	}
	return json.Marshal(struct {
		*bonusJSON
		Answer json.RawMessage `json:"Answer"`
		Name   json.RawMessage `json:"Name"`
		Task   json.RawMessage `json:"Task"`
		Help   json.RawMessage `json:"Help"`
	}{
		bonusJSON: &value,
		Answer:    answer,
		Name:      name,
		Task:      task,
		Help:      help,
	})
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

	raw *codeActionRawState
}

type codeActionRawState struct {
	locDateTime         json.RawMessage
	originalLocDateTime string
}

// UnmarshalJSON decodes a code action while retaining its original local time JSON.
func (a *CodeAction) UnmarshalJSON(data []byte) error {
	type codeActionJSON CodeAction
	var value codeActionJSON
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	var raw struct {
		LocDateTime json.RawMessage `json:"LocDateTime"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*a = CodeAction(value)
	a.raw = &codeActionRawState{
		locDateTime:         append(json.RawMessage(nil), raw.LocDateTime...),
		originalLocDateTime: a.LocDateTime,
	}
	return nil
}

// MarshalJSON encodes a code action while preserving a decoded local time JSON.
func (a CodeAction) MarshalJSON() ([]byte, error) {
	type codeActionJSON CodeAction
	value := codeActionJSON(a)
	var locDateTime json.RawMessage
	if a.raw != nil {
		locDateTime = a.raw.locDateTime
	}
	if len(locDateTime) == 0 || a.raw == nil || a.LocDateTime != a.raw.originalLocDateTime {
		var err error
		locDateTime, err = json.Marshal(a.LocDateTime)
		if err != nil {
			return nil, err
		}
	}
	return json.Marshal(struct {
		*codeActionJSON
		LocDateTime json.RawMessage `json:"LocDateTime"`
	}{
		codeActionJSON: &value,
		LocDateTime:    locDateTime,
	})
}

// EngineAction holds the result of the last game action.
type EngineAction struct {
	LevelNumber   int                  `json:"LevelNumber"`
	LevelAction   *ActionResult        `json:"LevelAction"`
	BonusAction   *ActionResult        `json:"BonusAction"`
	PenaltyAction *PenaltyActionResult `json:"PenaltyAction"`
	GameId        int                  `json:"GameId"`
	LevelId       int                  `json:"LevelId"`
	// RejectReason explains why an answer was not judged, e.g. "answer_blocked"
	// when the level's answer-block rule refused it. Only the new engine
	// reports it; the legacy one leaves it empty.
	RejectReason string `json:"RejectReason,omitempty"`
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
	ComingGames          []GameInfo `json:"ComingGames"`
	ActiveGames          []GameInfo `json:"ActiveGames"`
	Error                int        `json:"Error,omitempty"`
	Message              string     `json:"Message,omitempty"`
	IpUnblockUrl         *string    `json:"IpUnblockUrl,omitempty"`
	BruteForceUnblockUrl *string    `json:"BruteForceUnblockUrl,omitempty"`
	ConfirmEmailUrl      *string    `json:"ConfirmEmailUrl,omitempty"`
	CaptchaUrl           *string    `json:"CaptchaUrl,omitempty"`
	AdminWhoCanActivate  []string   `json:"AdminWhoCanActivate,omitempty"`

	rawFields map[string]json.RawMessage
}

// UnmarshalJSON decodes a game list while retaining explicit observed keys.
func (r *GameListResponse) UnmarshalJSON(data []byte) error {
	type gameListResponseJSON GameListResponse
	var value gameListResponseJSON
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	rawFields, err := rawJSONFields(data)
	if err != nil {
		return err
	}
	*r = GameListResponse(value)
	r.rawFields = rawFields
	return nil
}

// MarshalJSON encodes a game list while retaining explicit keys decoded from the API.
func (r GameListResponse) MarshalJSON() ([]byte, error) {
	type gameListResponseJSON GameListResponse
	return marshalWithRawFields(gameListResponseJSON(r), r.rawFields, nil)
}

// GameInfo holds full game metadata returned by the /home/?json=1 endpoint.
type GameInfo struct {
	GameID                  int       `json:"GameID"`
	GameNum                 int       `json:"GameNum"`
	SiteID                  int       `json:"SiteID,omitempty"`
	LangID                  int       `json:"LangID,omitempty"`
	CompetitionID           int       `json:"CompetitionID,omitempty"`
	OwnerID                 int       `json:"OwnerID,omitempty"`
	LevelNumber             int       `json:"LevelNumber,omitempty"`
	CreateDateTime          *DateTime `json:"CreateDateTime"`
	StartDateTime           *DateTime `json:"StartDateTime"`
	FinishDateTime          *DateTime `json:"FinishDateTime"`
	Title                   string    `json:"Title"`
	Descr                   string    `json:"Descr"`
	DescrWrapped            string    `json:"DescrWrapped,omitempty"`
	GameTypeID              int       `json:"GameTypeID"` // 0=single, 1=team, 2=personal
	ZoneId                  int       `json:"ZoneId"`     // 0=quest, 1=brainstorm, 2=photohunt, etc.
	LevelsSequence          int       `json:"LevelsSequence,omitempty"`
	ScenarioAvailability    int       `json:"ScenarioAvailability,omitempty"`
	MaxPlayers              int       `json:"MaxPlayers"`
	MaxTeamMembers          int       `json:"MaxTeamMembers"`
	ShowInCalendar          bool      `json:"ShowInCalendar"`
	FeeType                 int       `json:"FeeType"`
	FeeCurrencyId           int       `json:"FeeCurrencyId"`
	FeeName                 string    `json:"FeeName"`
	ShowFee                 int       `json:"ShowFee"`
	Fee                     *Money    `json:"Fee"`
	Prize                   *Money    `json:"Prize"`
	PrizeType               int       `json:"PrizeType,omitempty"`
	PrizeTypeSymbol         string    `json:"PrizeTypeSymbol,omitempty"`
	TSRemain                *Duration `json:"TSRemain"`
	Started                 bool      `json:"Started"`
	Finished                bool      `json:"Finished"`
	InProgress              bool      `json:"InProgress"`
	IsSectorsSupported      bool      `json:"IsSectorsSupported,omitempty"`
	IsOnlineStatAvailable   bool      `json:"IsOnlineStatAvailable,omitempty"`
	IsComplexitySupported   bool      `json:"IsComplexitySupported,omitempty"`
	IsModerated             bool      `json:"IsModerated,omitempty"`
	ComplexityFactor        int       `json:"ComplexityFactor,omitempty"`
	ComplexityMembersFactor int       `json:"ComplexityMembersFactor,omitempty"`
	QualityRate             int       `json:"QualityRate,omitempty"`
	QualityRateFormatted    string    `json:"QualityRateFormatted,omitempty"`
	TopicId                 int       `json:"TopicId,omitempty"`
	AcceptRateFromDateTime  *DateTime `json:"AcceptRateFromDateTime,omitempty"`
	RequestLastDate         *DateTime `json:"RequestLastDate,omitempty"`
	HideLevelsNames         bool      `json:"HideLevelsNames,omitempty"`
	AlwaysAvailable         bool      `json:"AlwaysAvailable,omitempty"`
	PublicAccess            bool      `json:"PublicAccess,omitempty"`
	DisplayMonitoring       int       `json:"DisplayMonitoring,omitempty"`

	Owner                    any  `json:"Owner,omitempty"`
	Type                     int  `json:"Type,omitempty"`
	CertificatePlaces        int  `json:"CertificatePlaces,omitempty"`
	CertificateAccessMode    int  `json:"CertificateAccessMode,omitempty"`
	ShowFinishPlace          bool `json:"ShowFinishPlace,omitempty"`
	StatusId                 int  `json:"StatusId,omitempty"`
	Status                   int  `json:"Status,omitempty"`
	IsAvailableAfterFinished bool `json:"IsAvailableAfterFinished,omitempty"`
	StatAvailabilityTypeID   int  `json:"StatAvailabilityTypeID,omitempty"`
	StatAvailabilityType     int  `json:"StatAvailabilityType,omitempty"`
	RateClosed               bool `json:"RateClosed,omitempty"`
	LevelsSequenceId         int  `json:"LevelsSequenceId,omitempty"`
	QualityRateCalculated    bool `json:"QualityRateCalculated,omitempty"`
	Zone                     int  `json:"Zone,omitempty"`
	AllowMakeStakes          bool `json:"AllowMakeStakes,omitempty"`
	HidePlayersList          bool `json:"HidePlayersList,omitempty"`
	ReplaceNlToBr            bool `json:"ReplaceNlToBr,omitempty"`
	HideGameDescr            bool `json:"HideGameDescr,omitempty"`
	DisplayAnnouncement      int  `json:"DisplayAnnouncement,omitempty"`
	ForUserID                int  `json:"ForUserID,omitempty"`
	// AFC arrives as a fractional number on some domains (e.g. 0.1 on tech.en.cx).
	AFC                   float64 `json:"AFC,omitempty"`
	IsQualityRateVisible  bool    `json:"IsQualityRateVisible,omitempty"`
	AuthorIndexCalculated bool    `json:"AuthorIndexCalculated,omitempty"`
	State                 int     `json:"State,omitempty"`
	IsModified            bool    `json:"IsModified,omitempty"`
	IsNewObject           bool    `json:"IsNewObject,omitempty"`
	ReadOnly              bool    `json:"ReadOnly,omitempty"`
	SyncRoot              any     `json:"SyncRoot,omitempty"`

	raw *gameInfoRawState
}

type gameInfoRawState struct {
	feeName         json.RawMessage
	originalFeeName string
	fields          map[string]json.RawMessage
}

// UnmarshalJSON decodes game metadata while retaining nullable and explicit JSON fields.
func (g *GameInfo) UnmarshalJSON(data []byte) error {
	type gameInfoJSON GameInfo
	var value gameInfoJSON
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	rawFields, err := rawJSONFields(data)
	if err != nil {
		return err
	}
	*g = GameInfo(value)
	g.raw = &gameInfoRawState{
		feeName:         append(json.RawMessage(nil), rawFields["FeeName"]...),
		originalFeeName: g.FeeName,
		fields:          rawFields,
	}
	return nil
}

// MarshalJSON encodes game metadata while preserving its decoded FeeName JSON.
func (g GameInfo) MarshalJSON() ([]byte, error) {
	type gameInfoJSON GameInfo
	overrides := map[string]json.RawMessage(nil)
	var rawFields map[string]json.RawMessage
	if g.raw != nil {
		rawFields = g.raw.fields
		if len(g.raw.feeName) > 0 && g.FeeName == g.raw.originalFeeName {
			overrides = map[string]json.RawMessage{"FeeName": g.raw.feeName}
		}
	}
	return marshalWithRawFields(gameInfoJSON(g), rawFields, overrides)
}

func rawJSONFields(data []byte) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	return fields, nil
}

func marshalWithRawFields(value any, rawFields, overrides map[string]json.RawMessage) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	fields, err := rawJSONFields(encoded)
	if err != nil {
		return nil, err
	}
	for key, raw := range rawFields {
		if _, ok := fields[key]; !ok {
			fields[key] = raw
		}
	}
	for key, raw := range overrides {
		fields[key] = raw
	}
	return json.Marshal(fields)
}

// DateTime represents a date-time value as returned by the EN API.
type DateTime struct {
	Value     float64 `json:"Value"`
	Timestamp int64   `json:"Timestamp"`
}

// Money represents a monetary value (fee or prize) as returned by the EN API.
type Money struct {
	Cents                       int    `json:"Cents"`
	Value                       int    `json:"Value"`
	Formated                    string `json:"Formated"`
	FormatedFull                string `json:"FormatedFull,omitempty"`
	DefaultCultureFormated      string `json:"DefaultCultureFormated,omitempty"`
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
	Game                *GameInfo          `json:"Game"`
	Level               *LevelStatInfo     `json:"Level"`
	StatItems           [][]StatItem       `json:"StatItems"`
	Levels              []LevelStatInfo    `json:"Levels"`
	IsLevelNamesVisible bool               `json:"IsLevelNamesVisible"`
	LevelPlayers        []LevelPlayerCount `json:"LevelPlayers"`
	User                *UserProfile       `json:"User"`
	PagerVisible        bool               `json:"PagerVisible"`
	ShowAdminWarning    bool               `json:"ShowAdminWarning"`
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
	ID             int       `json:"ID"`
	Login          string    `json:"Login"`
	FirstName      string    `json:"FirstName"`
	PatronymicName string    `json:"PatronymicName"`
	LastName       string    `json:"LastName"`
	Email          string    `json:"Email"`
	EmailChecked   bool      `json:"EmailChecked"`
	GenderID       int       `json:"GenderID"` // 1=male, 2=female
	BirthDate      *DateTime `json:"BirthDate"`
	CityId         int       `json:"CityId"`
	CountryId      int       `json:"CountryId"`
	ProvinceId     int       `json:"ProvinceId"`
	TeamID         int       `json:"TeamID"`
	ParentID       int       `json:"ParentID"`
	SiteId         int       `json:"SiteId"`
	IsActive       bool      `json:"IsActive"`
	RegDateTime    *DateTime `json:"RegDateTime"`
	Points         float64   `json:"Points"`
	BonusPoints    float64   `json:"BonusPoints"`
	RankID         int       `json:"RankID"`
	StatusId       int       `json:"StatusId"`
	Network        int       `json:"Network"`
	LastVisitTime  *DateTime `json:"LastVisitTime"`
	VkId           *string   `json:"VkId"`
	FbId           *string   `json:"FbId"`
	TgId           *string   `json:"TgId"`
	GooId          *string   `json:"GooId"`
	IsSuperAdmin   bool      `json:"IsSuperAdmin"`
	BlockByIP      bool      `json:"BlockByIP"`
}
