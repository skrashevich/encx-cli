package encx

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// catalogFixture is a trimmed but faithful copy of a real api.en.cx game entry.
const catalogFixture = `{
  "id": 29364, "site_id": 135, "lang_id": 2, "owner_id": 77322, "game_num": 23659,
  "game_type_id": 1, "zone_id": 0, "status_id": 3, "levels_sequence_id": 2,
  "title": "Тест-тест", "descr": "описание",
  "start_date_time": "2029-03-19T12:00:00Z",
  "finish_date_time": "2029-03-20T12:00:00Z",
  "create_date_time": "2019-03-12T12:45:01.133Z",
  "request_last_date": "2029-03-20T12:00:00Z",
  "accept_rate_from_date_time": "2029-03-19T12:00:00Z",
  "fee": 300, "prize": 5000, "fee_type_id": 2, "fee_currency_id": 1, "prize_type": 1,
  "fee_name": "Взнос за участие", "show_fee": 1,
  "max_players": 40, "max_team_members": 6, "scenario_availability": 2,
  "started": true, "finished": false, "is_moderated": true,
  "quality_rate": 8.5, "quality_rate_calculated": true, "author_index_calculated": true,
  "afc": 1, "topic_id": 30359, "display_monitoring": 1, "display_announcement": 0,
  "certificate_places": 3, "certificate_access_mode": 1, "show_finish_place": true,
  "hide_levels_names": false, "hide_players_list": false, "hide_game_descr": false,
  "replace_nl_to_br": false, "public_access": true, "rate_closed": false,
  "allow_make_stakes": true, "show_in_calendar": true,
  "is_available_after_finished": true, "stat_availability_type_id": 1,
  "authors": [{"user_id": 77322, "login": "darckloud", "common_index": 9.5}]
}`

func TestNewEngineGameListMapsHomeBlocks(t *testing.T) {
	var path string
	c := newEngineClient(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_, _ = w.Write([]byte(`{"active_games":[` + catalogFixture + `],"coming_games":[` + catalogFixture + `]}`))
	})

	list, err := c.GetGameList(context.Background())
	if err != nil {
		t.Fatalf("GetGameList: %v", err)
	}
	if path != "/games/home" {
		t.Errorf("path = %q, want /games/home", path)
	}
	if len(list.ActiveGames) != 1 || len(list.ComingGames) != 1 {
		t.Fatalf("games = %d active / %d coming, want 1 / 1", len(list.ActiveGames), len(list.ComingGames))
	}

	game := list.ActiveGames[0]
	if game.GameID != 29364 || game.GameNum != 23659 || game.Title != "Тест-тест" {
		t.Errorf("identity = %d/%d/%q", game.GameID, game.GameNum, game.Title)
	}
	if game.GameTypeID != GameTypeTeam || game.ZoneId != ZoneQuest || game.StatusId != 3 {
		t.Errorf("classification = %d/%d/%d", game.GameTypeID, game.ZoneId, game.StatusId)
	}
	if game.LevelsSequence != SequenceRandom || game.LevelsSequenceId != SequenceRandom {
		t.Errorf("LevelsSequence = %d/%d", game.LevelsSequence, game.LevelsSequenceId)
	}
	if game.MaxPlayers != 40 || game.MaxTeamMembers != 6 || game.ScenarioAvailability != 2 {
		t.Errorf("limits = %d/%d/%d", game.MaxPlayers, game.MaxTeamMembers, game.ScenarioAvailability)
	}
	if !game.Started || game.Finished || !game.InProgress {
		t.Errorf("state = started %v / finished %v / in progress %v", game.Started, game.Finished, game.InProgress)
	}
	if game.QualityRate != 8 {
		t.Errorf("QualityRate = %d, want 8 (truncated from 8.5)", game.QualityRate)
	}
	if game.Fee == nil || game.Fee.Value != 300 {
		t.Errorf("Fee = %+v, want 300", game.Fee)
	}
	if game.Prize == nil || game.Prize.Value != 5000 {
		t.Errorf("Prize = %+v, want 5000", game.Prize)
	}
	if game.FeeType != 2 || game.FeeCurrencyId != 1 || game.FeeName != "Взнос за участие" {
		t.Errorf("fee = %d/%d/%q", game.FeeType, game.FeeCurrencyId, game.FeeName)
	}
	if !game.PublicAccess || !game.AllowMakeStakes || !game.IsAvailableAfterFinished {
		t.Errorf("flags = %+v", game)
	}
}

func TestNewEngineGameListParsesTimestamps(t *testing.T) {
	c := newEngineClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"active_games":[` + catalogFixture + `],"coming_games":[]}`))
	})

	list, err := c.GetGameList(context.Background())
	if err != nil {
		t.Fatalf("GetGameList: %v", err)
	}
	game := list.ActiveGames[0]

	wantStart := time.Date(2029, 3, 19, 12, 0, 0, 0, time.UTC).Unix()
	if game.StartDateTime == nil || game.StartDateTime.Timestamp != wantStart {
		t.Errorf("StartDateTime = %+v, want unix %d", game.StartDateTime, wantStart)
	}
	if game.StartDateTime.Value != float64(wantStart) {
		t.Errorf("StartDateTime.Value = %v, want %d", game.StartDateTime.Value, wantStart)
	}
	wantCreate := time.Date(2019, 3, 12, 12, 45, 1, 133000000, time.UTC).Unix()
	if game.CreateDateTime == nil || game.CreateDateTime.Timestamp != wantCreate {
		t.Errorf("CreateDateTime = %+v, want unix %d (fractional seconds)", game.CreateDateTime, wantCreate)
	}
	if game.FinishDateTime == nil || game.RequestLastDate == nil || game.AcceptRateFromDateTime == nil {
		t.Error("one of the optional timestamps was dropped")
	}
	if game.TSRemain == nil || game.TSRemain.TotalSeconds <= 0 {
		t.Errorf("TSRemain = %+v, want a countdown to a future start", game.TSRemain)
	}
}

func TestDateTimeFromAPIAcceptsEngineLayouts(t *testing.T) {
	cases := map[string]int64{
		"2029-03-19T12:00:00Z":     time.Date(2029, 3, 19, 12, 0, 0, 0, time.UTC).Unix(),
		"2029-03-19T12:00:00":      time.Date(2029, 3, 19, 12, 0, 0, 0, time.UTC).Unix(),
		"2029-03-19 12:00:00":      time.Date(2029, 3, 19, 12, 0, 0, 0, time.UTC).Unix(),
		"2029-03-19":               time.Date(2029, 3, 19, 0, 0, 0, 0, time.UTC).Unix(),
		"2019-03-12T12:45:01.133Z": time.Date(2019, 3, 12, 12, 45, 1, 0, time.UTC).Unix(),
	}
	for value, want := range cases {
		got := dateTimeFromAPI(value)
		if got == nil || got.Timestamp != want {
			t.Errorf("dateTimeFromAPI(%q) = %+v, want unix %d", value, got, want)
		}
	}
	for _, value := range []string{"", "   ", "not a date"} {
		if got := dateTimeFromAPI(value); got != nil {
			t.Errorf("dateTimeFromAPI(%q) = %+v, want nil", value, got)
		}
	}
}

func TestNewEngineGameListPagesThroughCatalogEndpoints(t *testing.T) {
	var paths []string
	c := newEngineClient(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path+"?"+r.URL.RawQuery)
		_, _ = w.Write([]byte(`{"items":[` + catalogFixture + `],"total_count":1}`))
	})

	list, err := c.GetGameList(context.Background(), 2)
	if err != nil {
		t.Fatalf("GetGameList: %v", err)
	}
	want := []string{"/games/active?page=2", "/games/coming?page=2"}
	if len(paths) != 2 || paths[0] != want[0] || paths[1] != want[1] {
		t.Errorf("paths = %v, want %v", paths, want)
	}
	if len(list.ActiveGames) != 1 || len(list.ComingGames) != 1 {
		t.Errorf("list = %d active / %d coming", len(list.ActiveGames), len(list.ComingGames))
	}
}

func TestNewEngineDomainGamesDropsDuplicates(t *testing.T) {
	c := newEngineClient(t, func(w http.ResponseWriter, r *http.Request) {
		// The same game appears in both blocks, as it does on a live domain
		// while a game is starting.
		_, _ = w.Write([]byte(`{"active_games":[` + catalogFixture + `],"coming_games":[` + catalogFixture + `]}`))
	})

	games, err := c.GetDomainGames(context.Background())
	if err != nil {
		t.Fatalf("GetDomainGames: %v", err)
	}
	if len(games) != 1 {
		t.Fatalf("games = %+v, want one deduplicated entry", games)
	}
	if games[0].GameId != 29364 || games[0].Title != "Тест-тест" {
		t.Errorf("game = %+v", games[0])
	}
}

func TestNewEngineGameDetailsReturnsStructuredDocument(t *testing.T) {
	var path string
	c := newEngineClient(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_, _ = w.Write([]byte(`{"game":` + catalogFixture + `,"can_manage_game":true,"total_winners":2}`))
	})

	body, err := c.GetGameDetails(context.Background(), 29364)
	if err != nil {
		t.Fatalf("GetGameDetails: %v", err)
	}
	if path != "/games/29364/details" {
		t.Errorf("path = %q", path)
	}
	var details struct {
		Game struct {
			ID    int    `json:"id"`
			Title string `json:"title"`
		} `json:"game"`
		CanManageGame bool `json:"can_manage_game"`
		TotalWinners  int  `json:"total_winners"`
	}
	if err := json.Unmarshal([]byte(body), &details); err != nil {
		t.Fatalf("details are not JSON: %v", err)
	}
	if details.Game.ID != 29364 || details.Game.Title != "Тест-тест" {
		t.Errorf("game = %+v", details.Game)
	}
	if !details.CanManageGame || details.TotalWinners != 2 {
		t.Errorf("details = %+v", details)
	}
}

func TestNewEngineTimeoutToGameUsesFeeBox(t *testing.T) {
	var path string
	seconds := 0
	c := newEngineClient(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_, _ = w.Write([]byte(`{"seconds_to_start":` + strconv.Itoa(seconds) + `,"show_timer":true}`))
	})

	seconds = 4200
	got, err := c.GetTimeoutToGame(context.Background(), 29364)
	if err != nil {
		t.Fatalf("GetTimeoutToGame: %v", err)
	}
	if path != "/games/29364/fee-box" {
		t.Errorf("path = %q", path)
	}
	if got == nil || *got != 4200 {
		t.Errorf("timeout = %v, want 4200", got)
	}

	seconds = 0
	got, err = c.GetTimeoutToGame(context.Background(), 29364)
	if err != nil {
		t.Fatalf("GetTimeoutToGame: %v", err)
	}
	if got != nil {
		t.Errorf("timeout = %v, want nil once the game has started", *got)
	}
}

func TestNewEngineEnterGameUsesMakeFee(t *testing.T) {
	var path, method, query string
	success := true
	c := newEngineClient(t, func(w http.ResponseWriter, r *http.Request) {
		path, method, query = r.URL.Path, r.Method, r.URL.RawQuery
		if !success {
			_, _ = w.Write([]byte(`{"success":false,"message":"взнос не оплачен"}`))
			return
		}
		_, _ = w.Write([]byte(`{"success":true,"fee_accepted":true,"message":"ok"}`))
	})

	body, err := c.EnterGame(context.Background(), 29364)
	if err != nil {
		t.Fatalf("EnterGame: %v", err)
	}
	if path != "/games/29364/make-fee" || method != http.MethodPost {
		t.Errorf("request = %s %s", method, path)
	}
	if !containsQueryPair(query, "confirm", "yes") {
		t.Errorf("query = %q, want confirm=yes", query)
	}
	if !strings.Contains(body, "fee_accepted") {
		t.Errorf("body = %q", body)
	}

	success = false
	if _, err := c.EnterGame(context.Background(), 29364); err == nil {
		t.Fatal("EnterGame reported success on a rejected application")
	} else if !strings.Contains(err.Error(), "взнос не оплачен") {
		t.Errorf("error = %v, want the engine's message", err)
	}
}

func TestNewEngineFetchResourceResolvesAgainstAPIHost(t *testing.T) {
	var path string
	c := newEngineClient(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte{0x89, 'P', 'N', 'G'})
	})

	res, err := c.FetchResource(context.Background(), "/media/games/29364/task.png",
		ResourceOptions{RestrictToDomain: true})
	if err != nil {
		t.Fatalf("FetchResource: %v", err)
	}
	if path != "/media/games/29364/task.png" {
		t.Errorf("path = %q", path)
	}
	if res.ContentType != "image/png" || len(res.Data) != 4 {
		t.Errorf("resource = %+v", res)
	}
	if !strings.HasPrefix(res.URL, c.APIBaseURL()) {
		t.Errorf("URL = %q, want it resolved against %q", res.URL, c.APIBaseURL())
	}
}

func TestFetchResourceStillRefusesLocalAddresses(t *testing.T) {
	c := newEngineClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("a local address must not be fetched")
	})
	if _, err := c.FetchResource(context.Background(), "http://127.0.0.1:9/secret"); err == nil {
		t.Fatal("FetchResource accepted a loopback address")
	}
}
