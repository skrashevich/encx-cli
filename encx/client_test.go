package encx_test

import (
	"context"
	"os"
	"testing"

	"github.com/svk/encx/encx"
)

const testDomain = "demo.en.cx"

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
	resp, err := client.Login(context.Background(), testLogin(), testPassword())
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
	resp, err := client.Login(context.Background(), testLogin(), testPassword())
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
	resp, err := client.Login(context.Background(), "invalid_user_xxx", "wrong_pass")
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
	games, err := client.GetDomainGames(context.Background())
	if err != nil {
		t.Fatalf("GetDomainGames failed: %v", err)
	}
	if len(games) == 0 {
		t.Fatal("Expected at least one game on demo.en.cx, got 0")
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
	games, err := client.GetDomainGames(context.Background())
	if err != nil || len(games) == 0 {
		t.Skip("No games available for testing")
	}

	gid := games[0].GameId
	t.Logf("Testing with game ID: %d (%s)", gid, games[0].Title)

	model, err := client.GetGameModel(context.Background(), gid)
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

func TestGetGameList(t *testing.T) {
	skipIfNoIntegration(t)

	client := newTestClient()
	loginTestClient(t, client)

	list, err := client.GetGameList(context.Background())
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
