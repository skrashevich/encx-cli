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
		GameId:    123,
		GameTitle: "Test Game",
		Level: &encx.Level{
			LevelId: 1,
			Number:  1,
			Name:    "Level 1",
			Task:    &encx.LevelTask{TaskText: "Do something"},
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
