package encx_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/skrashevich/encx-cli/encx"
)

const testDomain = "tech.en.cx"

func testLogin() string {
	if v := os.Getenv("ENCX_TEST_LOGIN"); v != "" {
		return v
	}
	return "svk"
}

func testPassword() string {
	if v := os.Getenv("ENCX_TEST_PASSWORD"); v != "" {
		return v
	}
	return "Fortuna321"
}

func skipIfNoIntegration(t *testing.T) {
	t.Helper()
	if os.Getenv("ENCX_INTEGRATION") == "" {
		t.Skip("Skipping integration test (set ENCX_INTEGRATION=1 to run)")
	}
}

func newTestClient() *encx.Client {
	return encx.New(testDomain, encx.WithInsecureTLS())
}

func loginTestClient(t *testing.T, client *encx.Client) {
	t.Helper()
	resp, err := client.Login(t.Context(), testLogin(), testPassword())
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	if resp.Error != 0 {
		t.Fatalf("Login error %d: %s", resp.Error, encx.LoginErrorText(resp.Error))
	}
}

func TestLogin(t *testing.T) {
	skipIfNoIntegration(t)

	client := newTestClient()
	resp, err := client.Login(t.Context(), testLogin(), testPassword())
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	if resp.Error != 0 {
		t.Fatalf("Expected Error==0, got %d: %s", resp.Error, encx.LoginErrorText(resp.Error))
	}
}

func TestLoginInvalidCredentials(t *testing.T) {
	skipIfNoIntegration(t)

	client := newTestClient()
	resp, err := client.Login(t.Context(), "invalid_user_xxx", "wrong_pass")
	if err != nil {
		t.Fatalf("Login request failed: %v", err)
	}
	if resp.Error == 0 {
		t.Fatal("Expected non-zero Error for invalid credentials, got 0")
	}
	t.Logf("Got expected error %d: %s", resp.Error, encx.LoginErrorText(resp.Error))
}

func TestGetDomainGames(t *testing.T) {
	skipIfNoIntegration(t)

	client := newTestClient()
	games, err := client.GetDomainGames(t.Context())
	if err != nil {
		t.Fatalf("GetDomainGames failed: %v", err)
	}
	if len(games) == 0 {
		t.Fatal("Expected at least one game on tech.en.cx, got 0")
	}
	for _, g := range games {
		t.Logf("Game: %d - %s", g.GameId, g.Title)
	}
}

func TestGetGameModel(t *testing.T) {
	skipIfNoIntegration(t)

	client := newTestClient()
	loginTestClient(t, client)

	// First get a game ID from the domain
	games, err := client.GetDomainGames(t.Context())
	if err != nil || len(games) == 0 {
		t.Skip("No games available for testing")
	}

	gid := games[0].GameId
	t.Logf("Testing with game ID: %d (%s)", gid, games[0].Title)

	model, err := client.GetGameModel(t.Context(), gid)
	if err != nil {
		t.Fatalf("GetGameModel failed: %v", err)
	}

	t.Logf("GameId: %d, Event: %v, UserId: %d", model.GameId, model.Event, model.UserId)
	if model.Level != nil {
		t.Logf("Level: %d - %s", model.Level.Number, model.Level.Name)
	}
}

func TestLoginErrorText(t *testing.T) {
	tests := []struct {
		code int
		want string
	}{
		{0, "Успешная авторизация"},
		{2, "Неправильный логин или пароль"},
		{99, "Неизвестная ошибка"},
	}
	for _, tt := range tests {
		got := encx.LoginErrorText(tt.code)
		if got != tt.want {
			t.Errorf("LoginErrorText(%d) = %q, want %q", tt.code, got, tt.want)
		}
	}
}

func TestEventText(t *testing.T) {
	tests := []struct {
		code int
		want string
	}{
		{0, "Игра в процессе"},
		{6, "Игра завершена"},
		{17, "Игра окончена"},
		{22, "Таймаут уровня"},
		{-1, "Неизвестный статус"},
	}
	for _, tt := range tests {
		got := encx.EventText(tt.code)
		if got != tt.want {
			t.Errorf("EventText(%d) = %q, want %q", tt.code, got, tt.want)
		}
	}
}

// --- JSON serialization unit tests ---

func TestLoginResponseJSON(t *testing.T) {
	resp := encx.LoginResponse{Error: 0, Message: "OK"}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal LoginResponse: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal LoginResponse: %v", err)
	}
	if parsed["Error"].(float64) != 0 {
		t.Errorf("Expected Error=0, got %v", parsed["Error"])
	}
}

func TestGameModelJSON(t *testing.T) {
	model := encx.GameModel{
		GameId:     123,
		GameTitle:  "Test Game",
		GameTypeId: 1,
		GameZoneId: 0,
		IsCaptain:  true,
		Level: &encx.Level{
			LevelId: 1,
			Number:  1,
			Name:    "Level 1",
			Tasks:   []encx.LevelTask{{TaskText: "Do something"}},
		},
	}
	data, err := json.Marshal(model)
	if err != nil {
		t.Fatalf("Marshal GameModel: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal GameModel: %v", err)
	}
	if parsed["GameId"].(float64) != 123 {
		t.Errorf("Expected GameId=123, got %v", parsed["GameId"])
	}
	if parsed["GameTitle"].(string) != "Test Game" {
		t.Errorf("Expected GameTitle='Test Game', got %v", parsed["GameTitle"])
	}
	if parsed["IsCaptain"].(bool) != true {
		t.Errorf("Expected IsCaptain=true, got %v", parsed["IsCaptain"])
	}
	level := parsed["Level"].(map[string]any)
	if level["Name"].(string) != "Level 1" {
		t.Errorf("Expected Level.Name='Level 1', got %v", level["Name"])
	}
}

func TestDomainGameJSON(t *testing.T) {
	games := []encx.DomainGame{
		{Title: "Game A", GameId: 1},
		{Title: "Game B", GameId: 2},
	}
	data, err := json.Marshal(games)
	if err != nil {
		t.Fatalf("Marshal DomainGame slice: %v", err)
	}
	var parsed []map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal DomainGame slice: %v", err)
	}
	if len(parsed) != 2 {
		t.Fatalf("Expected 2 games, got %d", len(parsed))
	}
	if parsed[0]["title"].(string) != "Game A" {
		t.Errorf("Expected title='Game A', got %v", parsed[0]["title"])
	}
}

func TestGameListResponseJSON(t *testing.T) {
	list := encx.GameListResponse{
		ActiveGames: []encx.GameInfo{{GameID: 10, Title: "Active"}},
		ComingGames: []encx.GameInfo{{GameID: 20, Title: "Coming"}},
	}
	data, err := json.Marshal(list)
	if err != nil {
		t.Fatalf("Marshal GameListResponse: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal GameListResponse: %v", err)
	}
	active := parsed["ActiveGames"].([]any)
	if len(active) != 1 {
		t.Errorf("Expected 1 active game, got %d", len(active))
	}
	coming := parsed["ComingGames"].([]any)
	if len(coming) != 1 {
		t.Errorf("Expected 1 coming game, got %d", len(coming))
	}
}

// --- Integration tests for JSON output ---

func TestLoginJSON(t *testing.T) {
	skipIfNoIntegration(t)

	client := newTestClient()
	resp, err := client.Login(t.Context(), testLogin(), testPassword())
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal login response: %v", err)
	}
	if !json.Valid(data) {
		t.Fatal("Login response is not valid JSON")
	}
	t.Logf("Login JSON: %s", data)
}

func TestGetDomainGamesJSON(t *testing.T) {
	skipIfNoIntegration(t)

	client := newTestClient()
	games, err := client.GetDomainGames(t.Context())
	if err != nil {
		t.Fatalf("GetDomainGames failed: %v", err)
	}
	data, err := json.Marshal(games)
	if err != nil {
		t.Fatalf("Marshal games: %v", err)
	}
	if !json.Valid(data) {
		t.Fatal("Games response is not valid JSON")
	}
	// Verify it's a JSON array
	var arr []any
	if err := json.Unmarshal(data, &arr); err != nil {
		t.Fatalf("Expected JSON array: %v", err)
	}
	t.Logf("Got %d games as JSON", len(arr))
}

func TestGetGameListJSON(t *testing.T) {
	skipIfNoIntegration(t)

	client := newTestClient()
	loginTestClient(t, client)

	list, err := client.GetGameList(t.Context())
	if err != nil {
		t.Fatalf("GetGameList failed: %v", err)
	}
	data, err := json.Marshal(list)
	if err != nil {
		t.Fatalf("Marshal game list: %v", err)
	}
	if !json.Valid(data) {
		t.Fatal("GameList response is not valid JSON")
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Expected JSON object: %v", err)
	}
	if _, ok := parsed["ComingGames"]; !ok {
		t.Error("Expected ComingGames field in JSON")
	}
	if _, ok := parsed["ActiveGames"]; !ok {
		t.Error("Expected ActiveGames field in JSON")
	}
	t.Logf("GameList JSON has %d active, %d coming", len(list.ActiveGames), len(list.ComingGames))
}

func TestGetGameList(t *testing.T) {
	skipIfNoIntegration(t)

	client := newTestClient()
	loginTestClient(t, client)

	list, err := client.GetGameList(t.Context())
	if err != nil {
		t.Fatalf("GetGameList failed: %v", err)
	}
	t.Logf("Active games: %d, Coming games: %d", len(list.ActiveGames), len(list.ComingGames))
	for _, g := range list.ActiveGames {
		t.Logf("Active: %d - %s (type=%d, zone=%d)", g.GameID, g.Title, g.GameTypeID, g.ZoneId)
	}
	for _, g := range list.ComingGames {
		t.Logf("Coming: %d - %s (type=%d, zone=%d)", g.GameID, g.Title, g.GameTypeID, g.ZoneId)
	}
}

func TestGetGameListPaginated(t *testing.T) {
	skipIfNoIntegration(t)

	client := newTestClient()
	loginTestClient(t, client)

	page1, err := client.GetGameList(t.Context(), 1)
	if err != nil {
		t.Fatalf("GetGameList page 1 failed: %v", err)
	}
	page2, err := client.GetGameList(t.Context(), 2)
	if err != nil {
		t.Fatalf("GetGameList page 2 failed: %v", err)
	}

	p1Total := len(page1.ActiveGames) + len(page1.ComingGames)
	p2Total := len(page2.ActiveGames) + len(page2.ComingGames)
	t.Logf("Page 1: %d games, Page 2: %d games", p1Total, p2Total)

	// Verify extended fields are populated
	allGames := append(page1.ActiveGames, page1.ComingGames...)
	if len(allGames) > 0 {
		g := allGames[0]
		if g.SiteID == 0 {
			t.Error("Expected SiteID to be populated")
		}
		t.Logf("Extended fields: SiteID=%d, OwnerID=%d, LevelNumber=%d, ComplexityFactor=%d",
			g.SiteID, g.OwnerID, g.LevelNumber, g.ComplexityFactor)
	}
}

func TestGetGameStatistics(t *testing.T) {
	skipIfNoIntegration(t)

	client := newTestClient()
	loginTestClient(t, client)

	games, err := client.GetDomainGames(t.Context())
	if err != nil || len(games) == 0 {
		t.Skip("No games available for testing")
	}

	gid := games[0].GameId
	t.Logf("Testing game statistics for game ID: %d (%s)", gid, games[0].Title)

	stats, err := client.GetGameStatistics(t.Context(), gid)
	if err != nil {
		t.Fatalf("GetGameStatistics failed: %v", err)
	}

	if stats.Game == nil {
		t.Fatal("Expected Game to be non-nil")
	}
	if stats.Game.GameID != gid {
		t.Errorf("Expected GameID=%d, got %d", gid, stats.Game.GameID)
	}

	t.Logf("Game: %s (ID: %d)", stats.Game.Title, stats.Game.GameID)
	t.Logf("Levels: %d, IsLevelNamesVisible: %v", len(stats.Levels), stats.IsLevelNamesVisible)
	t.Logf("StatItems (level groups): %d", len(stats.StatItems))

	for _, l := range stats.Levels {
		t.Logf("  Level %d: %q dismissed=%v passedPlayers=%d",
			l.LevelNumber, l.LevelName, l.Dismissed, l.PassedPlayers)
	}
	for _, lp := range stats.LevelPlayers {
		t.Logf("  LevelPlayers: level %d -> %d players", lp.LevelNum, lp.Count)
	}

	if stats.User != nil {
		t.Logf("User: %s (ID: %d, Points: %.2f, Rank: %d)",
			stats.User.Login, stats.User.ID, stats.User.Points, stats.User.RankID)
	}
}

func TestGetGameStatisticsJSON(t *testing.T) {
	skipIfNoIntegration(t)

	client := newTestClient()
	loginTestClient(t, client)

	games, err := client.GetDomainGames(t.Context())
	if err != nil || len(games) == 0 {
		t.Skip("No games available for testing")
	}

	stats, err := client.GetGameStatistics(t.Context(), games[0].GameId)
	if err != nil {
		t.Fatalf("GetGameStatistics failed: %v", err)
	}

	data, err := json.Marshal(stats)
	if err != nil {
		t.Fatalf("Marshal game statistics: %v", err)
	}
	if !json.Valid(data) {
		t.Fatal("Game statistics response is not valid JSON")
	}
	t.Logf("GameStatistics JSON size: %d bytes", len(data))
}

// --- Unit tests for new types ---

func TestGameStatisticsResponseJSON(t *testing.T) {
	stats := encx.GameStatisticsResponse{
		Game: &encx.GameInfo{GameID: 100, Title: "Test"},
		Levels: []encx.LevelStatInfo{
			{LevelId: 1, LevelNumber: 1, LevelName: "First"},
		},
		StatItems: [][]encx.StatItem{
			{
				{UserId: 10, TeamName: "Team A", LevelNum: 1, SpentSeconds: 3600},
			},
		},
		LevelPlayers: []encx.LevelPlayerCount{
			{LevelNum: 1, Count: 5},
		},
		User: &encx.UserProfile{
			ID: 42, Login: "testuser", Points: 100.5,
		},
		IsLevelNamesVisible: true,
	}

	data, err := json.Marshal(stats)
	if err != nil {
		t.Fatalf("Marshal GameStatisticsResponse: %v", err)
	}
	if !json.Valid(data) {
		t.Fatal("GameStatisticsResponse is not valid JSON")
	}

	var parsed encx.GameStatisticsResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal GameStatisticsResponse: %v", err)
	}
	if parsed.Game.GameID != 100 {
		t.Errorf("Expected GameID=100, got %d", parsed.Game.GameID)
	}
	if parsed.User.Login != "testuser" {
		t.Errorf("Expected Login='testuser', got %q", parsed.User.Login)
	}
	if len(parsed.Levels) != 1 || parsed.Levels[0].LevelName != "First" {
		t.Error("Levels not correctly roundtripped")
	}
	if len(parsed.StatItems) != 1 || parsed.StatItems[0][0].TeamName != "Team A" {
		t.Error("StatItems not correctly roundtripped")
	}
}

func TestExtendedGameInfoJSON(t *testing.T) {
	g := encx.GameInfo{
		GameID:            42,
		Title:             "Extended",
		SiteID:            100,
		OwnerID:           200,
		LevelNumber:       5,
		ComplexityFactor:  360,
		QualityRate:       -1,
		IsSectorsSupported: true,
		TopicId:           12345,
	}

	data, err := json.Marshal(g)
	if err != nil {
		t.Fatalf("Marshal extended GameInfo: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal extended GameInfo: %v", err)
	}
	if parsed["SiteID"].(float64) != 100 {
		t.Errorf("Expected SiteID=100, got %v", parsed["SiteID"])
	}
	if parsed["OwnerID"].(float64) != 200 {
		t.Errorf("Expected OwnerID=200, got %v", parsed["OwnerID"])
	}
	if parsed["TopicId"].(float64) != 12345 {
		t.Errorf("Expected TopicId=12345, got %v", parsed["TopicId"])
	}
}
