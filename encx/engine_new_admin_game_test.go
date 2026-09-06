package encx

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

const gameEditorFixture = `{
  "game": {
    "id": 82448, "game_num": 12, "title": "Игра", "descr": "Описание",
    "prize": 5000, "started": false,
    "start_date_time": "2026-09-01T15:00:00Z",
    "finish_date_time": "2026-09-01T21:00:00Z",
    "request_last_date": "2026-09-01T18:00:00Z",
    "accept_rate_from_date_time": "2026-09-01T21:00:00Z",
    "is_moderated": true, "show_finish_place": true,
    "stat_availability_type_id": 2, "scenario_availability": 3,
    "max_players": 40, "max_team_members": 6, "show_fee": 1,
    "certificate_access_mode": 1, "certificate_places": 3, "afc": 0.5
  },
  "authors": [{"user_id": 1, "login": "svk"}, {"user_id": 2, "login": "second"}]
}`

const correctionsFixture = `{
  "game_id": 82448, "can_add": true, "is_admin": true,
  "items": [
    {"correct_id": 41, "correct_date_time": "31.08.2026 12:00:00",
     "correct_text": "Бонус", "comment": "за помощь", "correction_type": 1,
     "correction_value": -600, "value_text": "10 минут",
     "level_id": 812, "level_num": 2, "team_id": 77, "team_name": "Команда"},
    {"correct_id": 42, "correct_date_time": "31.08.2026 13:00:00",
     "correct_text": "Штраф", "comment": "", "correction_type": 2,
     "correction_value": 300, "value_text": "5 минут",
     "level_id": 0, "level_num": 0, "user_id": 501, "login": "svk"}
  ],
  "levels": [
    {"level_id": 811, "level_num": 1, "name": "Первый"},
    {"level_id": 812, "level_num": 2, "name": "Второй"}
  ],
  "players": [
    {"id": 77, "game_player_id": 900, "label": "Команда"},
    {"id": 78, "game_player_id": 901, "label": "Другие"}
  ]
}`

func newGameAdminClient(t *testing.T, calls *[]adminCall) *Client {
	t.Helper()
	return newEngineClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		*calls = append(*calls, adminCall{method: r.Method, path: r.URL.Path, body: string(body)})
		switch {
		case r.URL.Path == "/admin/games/82448" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(gameEditorFixture))
		case strings.HasSuffix(r.URL.Path, "/corrections") && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(correctionsFixture))
		case strings.HasSuffix(r.URL.Path, "/monitoring"):
			_, _ = w.Write([]byte(`{"game_id":82448,"mode":"main","actions":[
			  {"action_id":1,"level_id":812,"level_number":2,"user_id":501,"user_login":"svk",
			   "team_id":77,"team_name":"Команда","answer":"код","answer_date_time":"31.08.2026 12:00:00",
			   "is_correct":true,"sectors_info":"1 из 3"},
			  {"action_id":2,"level_id":812,"level_number":2,"user_login":"other",
			   "answer":"мимо","answer_date_time":"31.08.2026 12:01:00","is_correct":false}
			]}`))
		case r.URL.Path == "/admin/games/82448/levels" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(newAdminLevelBook().render()))
		case strings.HasSuffix(r.URL.Path, "/editor"):
			_, _ = w.Write([]byte(`{"game_id":82448,
			  "level":{"level_id":812,"level_number":2},
			  "members":[{"id":0,"label":"Для всех"},{"id":77,"label":"Команда"}]}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	})
}

func TestNewEngineAdminGetGameInfo(t *testing.T) {
	var calls []adminCall
	c := newGameAdminClient(t, &calls)

	info, err := c.AdminGetGameInfo(context.Background(), 82448)
	if err != nil {
		t.Fatalf("AdminGetGameInfo: %v", err)
	}
	if calls[0].path != "/admin/games/82448" {
		t.Errorf("path = %q", calls[0].path)
	}
	if info.Title != "Игра" || info.Description != "Описание" {
		t.Errorf("info = %+v", info)
	}
	if info.Authors != "svk,second" {
		t.Errorf("Authors = %q, want the author logins", info.Authors)
	}
	if info.Prize != "5000" || info.MaxPlayers != "40" || info.MaxTeamPlayers != "6" {
		t.Errorf("numbers = %q/%q/%q", info.Prize, info.MaxPlayers, info.MaxTeamPlayers)
	}
	if !info.IsModerated || !info.ShowFinishPlace {
		t.Errorf("flags = %+v", info)
	}
	if info.GameStatAvailability != "2" || info.GameScenarioAvailability != "3" {
		t.Errorf("availability = %q/%q", info.GameStatAvailability, info.GameScenarioAvailability)
	}
	// The legacy editor's ddlAuthorsCompexity is ten times the REST afc, so an
	// afc of 0.5 is the 5 a legacy caller reads and writes.
	if info.AuthorComplexity != "5" {
		t.Errorf("AuthorComplexity = %q, want %q", info.AuthorComplexity, "5")
	}
}

// TestNewEngineAdminGameInfoRoundTrip pins the property the two engines have to
// share: reading a game and writing the same values back changes nothing. The
// prize used to be multiplied by a hundred on the way out, which turned a
// read-modify-write into a hundredfold raise and, past the server's limit, into
// an outright rejection.
func TestNewEngineAdminGameInfoRoundTrip(t *testing.T) {
	var calls []adminCall
	c := newGameAdminClient(t, &calls)
	ctx := context.Background()

	info, err := c.AdminGetGameInfo(ctx, 82448)
	if err != nil {
		t.Fatalf("AdminGetGameInfo: %v", err)
	}
	if err := c.AdminUpdateGameInfo(ctx, 82448, *info); err != nil {
		t.Fatalf("AdminUpdateGameInfo: %v", err)
	}

	var body map[string]any
	if err := json.Unmarshal([]byte(lastCall(t, calls).body), &body); err != nil {
		t.Fatalf("body is not JSON: %q", lastCall(t, calls).body)
	}
	if body["prize_cents"] != float64(5000) {
		t.Errorf("prize_cents = %v, want the 5000 that was read back", body["prize_cents"])
	}
	if body["afc"] != 0.5 {
		t.Errorf("afc = %v, want the 0.5 that was read back", body["afc"])
	}
	if body["max_players"] != float64(40) || body["max_team_members"] != float64(6) {
		t.Errorf("limits = %v/%v", body["max_players"], body["max_team_members"])
	}
	if body["start_date_time"] != "2026-09-01T15:00:00Z" {
		t.Errorf("start_date_time = %v, want the start that was read back", body["start_date_time"])
	}
}

// The start is editable on both engines, so it has to reach the route rather
// than be dropped on the way — the gap that left it settable only in the web
// admin panel.
func TestNewEngineAdminUpdateGameInfoWritesStart(t *testing.T) {
	var calls []adminCall
	c := newGameAdminClient(t, &calls)

	err := c.AdminUpdateGameInfo(context.Background(), 82448, AdminGameInfo{
		StartDateTime: "2026-09-10T18:00:00+03:00",
	})
	if err != nil {
		t.Fatalf("AdminUpdateGameInfo: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(lastCall(t, calls).body), &body); err != nil {
		t.Fatalf("body is not JSON: %q", lastCall(t, calls).body)
	}
	if body["start_date_time"] != "2026-09-10T18:00:00+03:00" {
		t.Errorf("start_date_time = %v", body["start_date_time"])
	}
}

// A game that has begun reads back without a start: neither engine may move it,
// and the legacy editor disables the field, so a read-modify-write must not try.
func TestNewEngineAdminGetGameInfoHidesStartOfStartedGame(t *testing.T) {
	var calls []adminCall
	c := newEngineClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls = append(calls, adminCall{method: r.Method, path: r.URL.Path, body: string(body)})
		_, _ = w.Write([]byte(strings.Replace(gameEditorFixture,
			`"started": false`, `"started": true`, 1)))
	})
	ctx := context.Background()

	info, err := c.AdminGetGameInfo(ctx, 82448)
	if err != nil {
		t.Fatalf("AdminGetGameInfo: %v", err)
	}
	if info.StartDateTime != "" {
		t.Errorf("StartDateTime = %q, want it withheld for a started game", info.StartDateTime)
	}
	if err := c.AdminUpdateGameInfo(ctx, 82448, *info); err != nil {
		t.Fatalf("AdminUpdateGameInfo: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(lastCall(t, calls).body), &body); err != nil {
		t.Fatalf("body is not JSON: %q", lastCall(t, calls).body)
	}
	if _, ok := body["start_date_time"]; ok {
		t.Errorf("start_date_time = %v was sent for a started game", body["start_date_time"])
	}
}

// The legacy editor's date spelling carries no zone while this route stores an
// instant, so it is refused with an explanation instead of being sent as a time
// that is off by the domain's offset.
func TestNewEngineAdminUpdateGameInfoRefusesLegacyDateSpelling(t *testing.T) {
	var calls []adminCall
	c := newGameAdminClient(t, &calls)

	err := c.AdminUpdateGameInfo(context.Background(), 82448, AdminGameInfo{
		StartDateTime: "10.09.2026 18:00:00",
	})
	if err == nil {
		t.Fatal("AdminUpdateGameInfo accepted a legacy-spelled date")
	}
	if !strings.Contains(err.Error(), "RFC3339") {
		t.Errorf("error = %v, want it to name the format the route takes", err)
	}
	if len(calls) != 0 {
		t.Errorf("the request was sent anyway: %v", adminCallPaths(calls))
	}
}

// The new engine accepts afc in 0..1, i.e. 0..10 in the legacy spelling. A
// value outside that range is refused before the request goes out, so the
// caller learns what is wrong instead of reading "Validation failed".
func TestNewEngineAdminUpdateGameInfoRejectsOutOfRangeComplexity(t *testing.T) {
	var calls []adminCall
	c := newGameAdminClient(t, &calls)

	err := c.AdminUpdateGameInfo(context.Background(), 82448, AdminGameInfo{AuthorComplexity: "15"})
	if err == nil {
		t.Fatal("AdminUpdateGameInfo accepted an author complexity of 15")
	}
	if !strings.Contains(err.Error(), "вне диапазона") {
		t.Errorf("error = %v", err)
	}
	if len(calls) != 0 {
		t.Errorf("the request was sent anyway: %v", adminCallPaths(calls))
	}
}

// TestNewEngineAdminUpdateGameInfoSendsOnlyFilledFields pins the partial-update
// contract: AdminGameInfo uses strings because the legacy editor was a form, so
// an empty field means "leave it alone" and must not be transmitted as a zero.
func TestNewEngineAdminUpdateGameInfoSendsOnlyFilledFields(t *testing.T) {
	var calls []adminCall
	c := newGameAdminClient(t, &calls)

	err := c.AdminUpdateGameInfo(context.Background(), 82448, AdminGameInfo{
		Title:       "Новое имя",
		MaxPlayers:  "50",
		IsModerated: true,
	})
	if err != nil {
		t.Fatalf("AdminUpdateGameInfo: %v", err)
	}
	call := lastCall(t, calls)
	if call.method != http.MethodPatch || call.path != "/admin/games/82448" {
		t.Errorf("request = %s %s", call.method, call.path)
	}

	var body map[string]any
	if err := json.Unmarshal([]byte(call.body), &body); err != nil {
		t.Fatalf("body is not JSON: %q", call.body)
	}
	if body["title"] != "Новое имя" {
		t.Errorf("title = %v", body["title"])
	}
	if body["max_players"] != float64(50) {
		t.Errorf("max_players = %v", body["max_players"])
	}
	if body["is_moderated"] != true {
		t.Errorf("is_moderated = %v", body["is_moderated"])
	}
	for _, key := range []string{"descr", "prize_cents", "max_team_members", "authors", "afc"} {
		if _, ok := body[key]; ok {
			t.Errorf("%s was sent although the caller left it empty", key)
		}
	}
}

func TestNewEngineAdminLifecycleActions(t *testing.T) {
	cases := []struct {
		name string
		call func(*Client) error
		path string
	}{
		{"award points", func(c *Client) error { return c.AdminAwardPoints(context.Background(), 82448) },
			"/admin/games/82448/points/calculate"},
		{"end ratings", func(c *Client) error { return c.AdminEndRatings(context.Background(), 82448) },
			"/admin/games/82448/rate/close"},
		{"quality index", func(c *Client) error { return c.AdminCalculateIK(context.Background(), 82448) },
			"/admin/games/82448/quality-index"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls []adminCall
			c := newGameAdminClient(t, &calls)
			if err := tc.call(c); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			call := lastCall(t, calls)
			if call.method != http.MethodPost || call.path != tc.path {
				t.Errorf("request = %s %s, want POST %s", call.method, call.path, tc.path)
			}
		})
	}
}

// TestNewEngineAdminStatusChangeRefusesUnknownCodes pins that an irreversible
// operation is not attempted with a guessed status_id.
func TestNewEngineAdminStatusChangeRefusesUnknownCodes(t *testing.T) {
	var calls []adminCall
	c := newGameAdminClient(t, &calls)
	ctx := context.Background()

	for _, call := range []struct {
		name string
		fn   func() error
	}{
		{"deliver", func() error { return c.AdminDeliverGame(ctx, 82448) }},
		{"not deliver", func() error { return c.AdminNotDeliverGame(ctx, 82448) }},
	} {
		err := call.fn()
		if err == nil {
			t.Fatalf("%s: the status change ran with an unknown status_id", call.name)
		}
		if !strings.Contains(err.Error(), "status_id") {
			t.Errorf("%s: error = %v, want it to name the missing code", call.name, err)
		}
	}
	if len(calls) != 0 {
		t.Errorf("calls = %v, want no request sent", adminCallPaths(calls))
	}
}

func TestNewEngineAdminStatusChangeUsesConfiguredCode(t *testing.T) {
	var calls []adminCall
	c := newGameAdminClient(t, &calls)

	previous := GameStatusDelivered
	GameStatusDelivered = 1
	defer func() { GameStatusDelivered = previous }()

	if err := c.AdminDeliverGame(context.Background(), 82448); err != nil {
		t.Fatalf("AdminDeliverGame: %v", err)
	}
	call := lastCall(t, calls)
	if call.method != http.MethodPut || call.path != "/admin/games/82448/status" {
		t.Errorf("request = %s %s", call.method, call.path)
	}
	var body struct {
		StatusID int `json:"status_id"`
	}
	_ = json.Unmarshal([]byte(call.body), &body)
	if body.StatusID != 1 {
		t.Errorf("status_id = %d, want the configured code", body.StatusID)
	}
}

func TestNewEngineAdminGetCorrections(t *testing.T) {
	var calls []adminCall
	c := newGameAdminClient(t, &calls)

	corrections, err := c.AdminGetCorrections(context.Background(), 82448)
	if err != nil {
		t.Fatalf("AdminGetCorrections: %v", err)
	}
	if calls[0].path != "/games/82448/corrections" {
		t.Errorf("path = %q", calls[0].path)
	}
	if len(corrections) != 2 {
		t.Fatalf("corrections = %+v", corrections)
	}
	if corrections[0] != (AdminCorrection{
		ID: "41", DateTime: "31.08.2026 12:00:00", Team: "Команда",
		Level: "2", Reason: "Бонус", Time: "10 минут", Comment: "за помощь",
	}) {
		t.Errorf("corrections[0] = %+v", corrections[0])
	}
	// A game-wide correction has no level; the legacy page left the cell empty.
	if corrections[1].Level != "" {
		t.Errorf("Level = %q, want empty for a game-wide correction", corrections[1].Level)
	}
	if corrections[1].Team != "svk" {
		t.Errorf("Team = %q, want the login when there is no team", corrections[1].Team)
	}
}

func TestNewEngineAdminAddCorrectionResolvesNames(t *testing.T) {
	var calls []adminCall
	c := newGameAdminClient(t, &calls)

	err := c.AdminAddCorrection(context.Background(), 82448, AdminCorrectionAdd{
		TeamName: "Команда", LevelName: "Второй", Comment: "за помощь",
		CorrectionType: "1", Hours: "1", Minutes: "30",
	})
	if err != nil {
		t.Fatalf("AdminAddCorrection: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("calls = %v, want a read then a write", adminCallPaths(calls))
	}
	call := calls[1]
	if call.method != http.MethodPost || call.path != "/games/82448/corrections" {
		t.Errorf("request = %s %s", call.method, call.path)
	}
	var body struct {
		PlayerID     int    `json:"player_id"`
		GamePlayerID int    `json:"game_player_id"`
		LevelID      int    `json:"level_id"`
		IsBonus      bool   `json:"is_bonus"`
		Seconds      int    `json:"seconds"`
		Comment      string `json:"comment"`
	}
	if err := json.Unmarshal([]byte(call.body), &body); err != nil {
		t.Fatalf("body is not JSON: %q", call.body)
	}
	if body.PlayerID != 77 || body.GamePlayerID != 900 {
		t.Errorf("player = %d/%d, want the resolved team", body.PlayerID, body.GamePlayerID)
	}
	if body.LevelID != 812 {
		t.Errorf("level_id = %d, want the resolved level", body.LevelID)
	}
	if !body.IsBonus {
		t.Error("is_bonus = false for correction type 1")
	}
	if body.Seconds != 5400 {
		t.Errorf("seconds = %d, want 5400", body.Seconds)
	}
	if body.Comment != "за помощь" {
		t.Errorf("comment = %q", body.Comment)
	}
}

func TestNewEngineAdminAddCorrectionRejectsUnknownNames(t *testing.T) {
	var calls []adminCall
	c := newGameAdminClient(t, &calls)
	ctx := context.Background()

	err := c.AdminAddCorrection(ctx, 82448, AdminCorrectionAdd{TeamName: "нет такой", LevelName: "0"})
	if err == nil || !strings.Contains(err.Error(), "not in the game") {
		t.Errorf("error = %v, want a report that the participant is unknown", err)
	}

	err = c.AdminAddCorrection(ctx, 82448, AdminCorrectionAdd{TeamName: "Команда", LevelName: "Нет уровня"})
	if err == nil || !strings.Contains(err.Error(), "not in the game") {
		t.Errorf("error = %v, want a report that the level is unknown", err)
	}
}

func TestNewEngineAdminAddCorrectionForAllLevels(t *testing.T) {
	var calls []adminCall
	c := newGameAdminClient(t, &calls)

	err := c.AdminAddCorrection(context.Background(), 82448, AdminCorrectionAdd{
		TeamName: "Другие", LevelName: "0", CorrectionType: "2", Minutes: "5",
	})
	if err != nil {
		t.Fatalf("AdminAddCorrection: %v", err)
	}
	var body struct {
		LevelID int  `json:"level_id"`
		IsBonus bool `json:"is_bonus"`
		Seconds int  `json:"seconds"`
	}
	_ = json.Unmarshal([]byte(lastCall(t, calls).body), &body)
	if body.LevelID != 0 {
		t.Errorf("level_id = %d, want 0 for all levels", body.LevelID)
	}
	if body.IsBonus {
		t.Error("is_bonus = true for correction type 2 (penalty)")
	}
	if body.Seconds != 300 {
		t.Errorf("seconds = %d, want 300", body.Seconds)
	}
}

func TestNewEngineAdminDeleteCorrection(t *testing.T) {
	var calls []adminCall
	c := newGameAdminClient(t, &calls)

	if err := c.AdminDeleteCorrection(context.Background(), 82448, "41"); err != nil {
		t.Fatalf("AdminDeleteCorrection: %v", err)
	}
	call := lastCall(t, calls)
	if call.method != http.MethodDelete || call.path != "/games/82448/corrections/41" {
		t.Errorf("request = %s %s", call.method, call.path)
	}

	if err := c.AdminDeleteCorrection(context.Background(), 82448, "  "); err == nil {
		t.Error("AdminDeleteCorrection accepted an empty id")
	}
}

func TestNewEngineAdminGetTeamsReadsForMemberOptions(t *testing.T) {
	var calls []adminCall
	c := newGameAdminClient(t, &calls)

	teams, err := c.AdminGetTeams(context.Background(), 82448, 2)
	if err != nil {
		t.Fatalf("AdminGetTeams: %v", err)
	}
	if len(teams) != 2 {
		t.Fatalf("teams = %+v", teams)
	}
	if teams[0] != (AdminTeam{ID: "0", Name: "Для всех"}) {
		t.Errorf("teams[0] = %+v", teams[0])
	}
	if teams[1] != (AdminTeam{ID: "77", Name: "Команда"}) {
		t.Errorf("teams[1] = %+v", teams[1])
	}
}

func TestNewEngineAdminActionMonitor(t *testing.T) {
	var calls []adminCall
	c := newGameAdminClient(t, &calls)

	entries, err := c.AdminGetActionMonitor(context.Background(), 82448)
	if err != nil {
		t.Fatalf("AdminGetActionMonitor: %v", err)
	}
	if calls[0].path != "/games/82448/monitoring" {
		t.Errorf("path = %q", calls[0].path)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %+v", entries)
	}
	if entries[0] != (AdminActionMonitorEntry{
		Number: "2", Participant: "Команда", Direction: "+",
		Answer: "код", DateTime: "31.08.2026 12:00:00", Sectors: "1 из 3",
	}) {
		t.Errorf("entries[0] = %+v", entries[0])
	}
	if entries[1].Direction != "-" || entries[1].Participant != "other" {
		t.Errorf("entries[1] = %+v", entries[1])
	}
}
