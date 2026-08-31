package encx

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

const statisticsFixture = `{
  "game_id": 82448, "game_num": 12, "game_title": "Тестовая игра",
  "game_type_id": 1, "zone_id": 0, "levels_sequence_id": 2, "total_levels": 2,
  "hide_levels_names": false, "can_view_stats": true, "is_game_author": true,
  "admin_warning": "статистика видна только автору",
  "current_page": 1, "total_pages": 3, "rows_per_page": 20, "sort_field": "time",
  "levels": [
    {"level_id": 811, "level_number": 1, "level_name": "Первый", "dismissed": false},
    {"level_id": 812, "level_number": 2, "level_name": "Второй", "dismissed": true}
  ],
  "level_stats": {
    "1": [
      {"action_id": 1, "level_id": 811, "level_num": 1, "level_order": 1, "position": 1,
       "user_id": 501, "user_login": "svk", "user_name": "Сергей", "team_id": 77,
       "team_name": "Команда", "spent_seconds": 3661, "scores": 10,
       "enter_date_time": "2026-08-31T18:01:01Z", "pass_type_id": 0},
      {"action_id": 2, "level_id": 811, "level_num": 1, "level_order": 1, "position": 2,
       "user_id": 502, "user_login": "other", "user_name": "", "team_id": 78,
       "team_name": "Другие", "spent_seconds": 4000, "scores": 8,
       "enter_date_time": "2026-08-31T18:06:40Z", "pass_type_id": 2}
    ],
    "2": [
      {"action_id": 3, "level_id": 812, "level_num": 2, "level_order": 2, "position": 1,
       "user_id": 501, "user_login": "svk", "user_name": "Сергей", "team_id": 77,
       "team_name": "Команда", "spent_seconds": 120, "scores": 5,
       "enter_date_time": "2026-08-31T18:03:01Z", "pass_type_id": 1}
    ]
  }
}`

func TestNewEngineStatisticsMapsLevelsAndRows(t *testing.T) {
	var path, query string
	c := newEngineClient(t, func(w http.ResponseWriter, r *http.Request) {
		path, query = r.URL.Path, r.URL.RawQuery
		_, _ = w.Write([]byte(statisticsFixture))
	})

	stats, err := c.GetGameStatistics(context.Background(), 82448)
	if err != nil {
		t.Fatalf("GetGameStatistics: %v", err)
	}
	if path != "/games/82448/statistics" {
		t.Errorf("path = %q", path)
	}
	if !containsQueryPair(query, "lang", "ru") {
		t.Errorf("query = %q, want lang=ru", query)
	}

	if len(stats.Levels) != 2 {
		t.Fatalf("Levels = %d, want 2", len(stats.Levels))
	}
	if l := stats.Levels[0]; l.LevelId != 811 || l.LevelNumber != 1 ||
		l.LevelName != "Первый" || l.Dismissed || l.PassedPlayers != 2 {
		t.Errorf("Levels[0] = %+v", l)
	}
	if l := stats.Levels[1]; l.LevelId != 812 || !l.Dismissed || l.PassedPlayers != 1 {
		t.Errorf("Levels[1] = %+v", l)
	}

	if len(stats.LevelPlayers) != 2 ||
		stats.LevelPlayers[0] != (LevelPlayerCount{LevelNum: 1, Count: 2}) ||
		stats.LevelPlayers[1] != (LevelPlayerCount{LevelNum: 2, Count: 1}) {
		t.Errorf("LevelPlayers = %+v", stats.LevelPlayers)
	}

	if len(stats.StatItems) != 2 {
		t.Fatalf("StatItems groups = %d, want 2 (one per published key, ordered)", len(stats.StatItems))
	}
	if len(stats.StatItems[0]) != 2 || len(stats.StatItems[1]) != 1 {
		t.Fatalf("StatItems sizes = %d/%d, want 2/1", len(stats.StatItems[0]), len(stats.StatItems[1]))
	}

	row := stats.StatItems[0][0]
	if row.UserId != 501 || row.UserName != "Сергей" || row.TeamId != 77 ||
		row.TeamName != "Команда" || row.LevelId != 811 || row.LevelNum != 1 ||
		row.LevelOrder != 1 || row.SpentSeconds != 3661 || row.Scores != 10 || row.PassType != 0 {
		t.Errorf("StatItems[0][0] = %+v", row)
	}
	if row.SpentLevelTime == nil || row.SpentLevelTime.Hours != 1 ||
		row.SpentLevelTime.Minutes != 1 || row.SpentLevelTime.Seconds != 1 {
		t.Errorf("SpentLevelTime = %+v, want 1h01m01s", row.SpentLevelTime)
	}
	if row.ActionTime == nil || row.ActionTime.Timestamp == 0 {
		t.Errorf("ActionTime = %+v", row.ActionTime)
	}

	// A row without a display name must still be identifiable.
	if got := stats.StatItems[0][1].UserName; got != "other" {
		t.Errorf("UserName = %q, want the login as a stand-in", got)
	}
	if got := stats.StatItems[0][1].PassType; got != 2 {
		t.Errorf("PassType = %d, want 2 (timeout autopass)", got)
	}
	if got := stats.StatItems[1][0].PassType; got != 1 {
		t.Errorf("PassType = %d, want 1 (dismissed by an admin)", got)
	}
}

func TestNewEngineStatisticsMapsDocumentFlags(t *testing.T) {
	c := newEngineClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(statisticsFixture))
	})

	stats, err := c.GetGameStatistics(context.Background(), 82448)
	if err != nil {
		t.Fatalf("GetGameStatistics: %v", err)
	}
	if !stats.IsLevelNamesVisible {
		t.Error("IsLevelNamesVisible = false, want true when hide_levels_names is false")
	}
	if !stats.ShowAdminWarning {
		t.Error("ShowAdminWarning = false, want true when the engine sent a warning")
	}
	if !stats.PagerVisible {
		t.Error("PagerVisible = false, want true for a 3-page result")
	}
	if stats.Game == nil || stats.Game.GameID != 82448 || stats.Game.GameNum != 12 ||
		stats.Game.Title != "Тестовая игра" || stats.Game.LevelsSequence != SequenceRandom {
		t.Errorf("Game = %+v", stats.Game)
	}
}

func TestNewEngineStatisticsHandlesEmptyResults(t *testing.T) {
	c := newEngineClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"levels":[
		  {"level_id":1,"level_number":1},
		  {"level_id":2,"level_number":2}
		],"level_stats":{}}`))
	})

	stats, err := c.GetGameStatistics(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetGameStatistics: %v", err)
	}
	if len(stats.Levels) != 2 {
		t.Fatalf("Levels = %d, want 2", len(stats.Levels))
	}
	for i, level := range stats.Levels {
		if level.PassedPlayers != 0 || stats.LevelPlayers[i].Count != 0 {
			t.Errorf("Levels[%d] = %+v, want no players", i, level)
		}
	}
	if stats.StatItems != nil {
		t.Errorf("StatItems = %+v, want nil when the engine sent no rows", stats.StatItems)
	}
	if stats.PagerVisible {
		t.Error("PagerVisible = true for a single page")
	}
}

// TestNewEngineStatisticsKeepsAggregateGroups pins the behaviour a live run on
// demo.en.cx corrected: the engine publishes per-player totals under negative
// keys (-1 total time, -2 net time) that belong to no level, and dropping them
// would silently lose the totals the statistics page shows.
func TestNewEngineStatisticsKeepsAggregateGroups(t *testing.T) {
	c := newEngineClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"levels":[
		  {"level_id":265006,"level_number":1,"level_name":"Первый"},
		  {"level_id":265007,"level_number":2,"level_name":"Второй"}
		],"level_stats":{
		  "2":[{"level_id":265007,"level_num":2,"user_login":"b","spent_seconds":31}],
		  "-1":[{"level_id":10,"level_num":-1,"level_order":-1,"user_login":"total","spent_seconds":49309}],
		  "1":[{"level_id":265006,"level_num":1,"user_login":"a","spent_seconds":47248}],
		  "-2":[{"level_id":10,"level_num":-2,"level_order":-2,"user_login":"net","spent_seconds":40939}]
		}}`))
	})

	stats, err := c.GetGameStatistics(context.Background(), 27053)
	if err != nil {
		t.Fatalf("GetGameStatistics: %v", err)
	}
	if len(stats.StatItems) != 4 {
		t.Fatalf("StatItems groups = %d, want 4 (two levels plus two aggregates)", len(stats.StatItems))
	}

	// Groups are ordered by key so a map's random iteration cannot reshuffle them.
	wantOrder := []int{-2, -1, 1, 2}
	for i, want := range wantOrder {
		if len(stats.StatItems[i]) != 1 {
			t.Fatalf("StatItems[%d] has %d rows, want 1", i, len(stats.StatItems[i]))
		}
		if got := stats.StatItems[i][0].LevelNum; got != want {
			t.Errorf("StatItems[%d] level_num = %d, want %d", i, got, want)
		}
	}

	// The level list still counts only real levels.
	if len(stats.Levels) != 2 || stats.Levels[0].PassedPlayers != 1 || stats.Levels[1].PassedPlayers != 1 {
		t.Errorf("Levels = %+v", stats.Levels)
	}
}

func TestNewEngineStatisticsAcceptsLevelIDKeys(t *testing.T) {
	c := newEngineClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"levels":[{"level_id":811,"level_number":7}],
		  "level_stats":{"811":[{"level_id":811,"level_num":7,"user_login":"svk","spent_seconds":60}]}}`))
	})

	stats, err := c.GetGameStatistics(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetGameStatistics: %v", err)
	}
	if len(stats.StatItems) != 1 || len(stats.StatItems[0]) != 1 {
		t.Fatalf("rows keyed by level id were dropped: %+v", stats.StatItems)
	}
}

func TestNewEngineProfileFetchesUser(t *testing.T) {
	var paths []string
	c := newEngineClient(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/auth/session":
			_, _ = w.Write([]byte(`{"user_id":501}`))
		case "/users/501":
			_, _ = w.Write([]byte(`{
			  "id":501,"login":"svk","first_name":"Сергей","last_name":"К",
			  "rank_sentence_key":"Colonel","points":3010.5,"team_id":77,
			  "team":{"id":77,"name":"Команда"},
			  "site":{"primary_domain":"tech.en.cx","city":{"name":"Москва"},
			          "province":{"name":"МО"},"country":{"name":"Россия"}}}`))
		}
	})

	profile, err := c.GetProfile(context.Background())
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	want := []string{"/auth/session", "/users/501"}
	if len(paths) != 2 || paths[0] != want[0] || paths[1] != want[1] {
		t.Errorf("paths = %v, want %v", paths, want)
	}
	if profile.ID != 501 || profile.Login != "svk" {
		t.Errorf("identity = %d/%q", profile.ID, profile.Login)
	}
	if profile.Name != "Сергей К" {
		t.Errorf("Name = %q, want the joined first and last name", profile.Name)
	}
	if profile.Rank != "Colonel" || profile.Points != "3010.5" {
		t.Errorf("rank/points = %q/%q", profile.Rank, profile.Points)
	}
	if profile.Team != "Команда" || profile.TeamID != 77 {
		t.Errorf("team = %q/%d", profile.Team, profile.TeamID)
	}
	if profile.Domain != "tech.en.cx" {
		t.Errorf("Domain = %q", profile.Domain)
	}
	if profile.Location != "Москва, МО, Россия" {
		t.Errorf("Location = %q", profile.Location)
	}
}

func TestNewEngineProfileUsesSessionUserWhenEmbedded(t *testing.T) {
	var calls int
	c := newEngineClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"user":{"id":7,"login":"embedded","points":1}}`))
	})

	profile, err := c.GetProfile(context.Background())
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	if calls != 1 {
		t.Errorf("requests = %d, want 1 (the session already carried the user)", calls)
	}
	if profile.Login != "embedded" || profile.ID != 7 {
		t.Errorf("profile = %+v", profile)
	}
}

func TestNewEngineProfileReportsAnonymousSession(t *testing.T) {
	c := newEngineClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	})

	_, err := c.GetProfile(context.Background())
	if err == nil {
		t.Fatal("GetProfile accepted a session without a user")
	}
	if !strings.Contains(err.Error(), "does not name a user") {
		t.Errorf("error = %v", err)
	}
}
