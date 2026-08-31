package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/skrashevich/encx-cli/encx"
)

// newMockServers starts the mock twice over one game state: once on the legacy
// routes and once on the new-engine routes.
func newMockServers(t *testing.T) (legacy, api *httptest.Server) {
	t.Helper()
	s := newMockServer(t)

	legacyMux := s.routes()
	apiMux := http.NewServeMux()
	s.registerNewAPIRoutes(apiMux, legacyMux)

	legacy = httptest.NewServer(withCommonHeaders(legacyMux))
	api = httptest.NewServer(withCommonHeaders(newEngineFallbackMux(apiMux, legacyMux)))
	t.Cleanup(legacy.Close)
	t.Cleanup(api.Close)
	return legacy, api
}

func mockClient(t *testing.T, srv *httptest.Server, mode encx.EngineMode) *encx.Client {
	t.Helper()
	t.Setenv(encx.EngineEnvVar, "")
	host := strings.TrimPrefix(srv.URL, "http://")
	return encx.New(host,
		encx.WithHTTP(),
		encx.WithAdminDelay(0),
		encx.WithAPIBaseURL(srv.URL),
		encx.WithEngine(mode))
}

func TestMockServesTheNewEngineVersionProbe(t *testing.T) {
	_, api := newMockServers(t)
	c := mockClient(t, api, encx.EngineAuto)
	if got := c.Engine(); got != encx.EngineNew {
		t.Errorf("Engine = %q, want new: the mock answers GET /version", got)
	}
}

func TestMockLoginWorksOnBothEngines(t *testing.T) {
	legacy, api := newMockServers(t)
	ctx := context.Background()

	for _, tc := range []struct {
		name string
		srv  *httptest.Server
		mode encx.EngineMode
	}{
		{"legacy", legacy, encx.EngineLegacy},
		{"new", api, encx.EngineNew},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := mockClient(t, tc.srv, tc.mode)
			resp, err := c.Login(ctx, "player", "secret")
			if err != nil {
				t.Fatalf("Login: %v", err)
			}
			if resp.Error != 0 {
				t.Fatalf("Error = %d (%s)", resp.Error, encx.LoginErrorText(resp.Error))
			}
			if err := c.VerifyAdminSession(ctx); err != nil {
				t.Errorf("VerifyAdminSession: %v", err)
			}
		})
	}
}

func TestMockRejectsBadCredentialsOnBothEngines(t *testing.T) {
	legacy, api := newMockServers(t)
	ctx := context.Background()

	for _, tc := range []struct {
		name string
		srv  *httptest.Server
		mode encx.EngineMode
	}{
		{"legacy", legacy, encx.EngineLegacy},
		{"new", api, encx.EngineNew},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := mockClient(t, tc.srv, tc.mode)
			resp, err := c.Login(ctx, "fail", "fail")
			if err != nil {
				t.Fatalf("Login: %v", err)
			}
			if resp.Error == 0 {
				t.Error("the mock accepted the credentials it is supposed to reject")
			}
		})
	}
}

// TestMockGameModelAgreesAcrossEngines is the point of the whole exercise: the
// same game state must reach the caller identically whichever engine served it.
func TestMockGameModelAgreesAcrossEngines(t *testing.T) {
	legacy, api := newMockServers(t)
	ctx := context.Background()

	legacyClient := mockClient(t, legacy, encx.EngineLegacy)
	if _, err := legacyClient.Login(ctx, "player", "secret"); err != nil {
		t.Fatalf("legacy Login: %v", err)
	}
	legacyModel, err := legacyClient.GetGameModel(ctx, mockGameID)
	if err != nil {
		t.Fatalf("legacy GetGameModel: %v", err)
	}

	apiClient := mockClient(t, api, encx.EngineNew)
	if _, err := apiClient.Login(ctx, "player", "secret"); err != nil {
		t.Fatalf("new Login: %v", err)
	}
	newModel, err := apiClient.GetGameModel(ctx, mockGameID)
	if err != nil {
		t.Fatalf("new GetGameModel: %v", err)
	}

	if legacyModel.GameId != newModel.GameId || legacyModel.GameTitle != newModel.GameTitle {
		t.Errorf("game identity differs: %d/%q vs %d/%q",
			legacyModel.GameId, legacyModel.GameTitle, newModel.GameId, newModel.GameTitle)
	}
	if legacyModel.TeamId != newModel.TeamId || legacyModel.Login != newModel.Login {
		t.Errorf("player differs: %d/%q vs %d/%q",
			legacyModel.TeamId, legacyModel.Login, newModel.TeamId, newModel.Login)
	}
	if len(legacyModel.Levels) != len(newModel.Levels) {
		t.Errorf("level strip differs: %d vs %d", len(legacyModel.Levels), len(newModel.Levels))
	}
	if legacyModel.Level == nil || newModel.Level == nil {
		t.Fatalf("one engine reported no current level: legacy=%v new=%v",
			legacyModel.Level != nil, newModel.Level != nil)
	}
	if legacyModel.Level.LevelId != newModel.Level.LevelId ||
		legacyModel.Level.Number != newModel.Level.Number {
		t.Errorf("current level differs: %+v vs %+v", legacyModel.Level, newModel.Level)
	}
	if len(legacyModel.Level.Sectors) != len(newModel.Level.Sectors) {
		t.Errorf("sectors differ: %d vs %d",
			len(legacyModel.Level.Sectors), len(newModel.Level.Sectors))
	}
	if legacyModel.Level.CanSubmitLevelAnswer() != newModel.Level.CanSubmitLevelAnswer() {
		t.Error("the engines disagree on whether an answer may be submitted")
	}
}

func TestMockAcceptsCodesOnTheNewEngine(t *testing.T) {
	_, api := newMockServers(t)
	ctx := context.Background()

	c := mockClient(t, api, encx.EngineNew)
	if _, err := c.Login(ctx, "player", "secret"); err != nil {
		t.Fatalf("Login: %v", err)
	}
	model, err := c.GetGameModel(ctx, mockGameID)
	if err != nil {
		t.Fatalf("GetGameModel: %v", err)
	}
	if model.Level == nil {
		t.Fatal("no current level to answer")
	}

	// A wrong code must come back as a wrong answer, not as an error.
	after, err := c.SendCode(ctx, mockGameID, model.Level.LevelId, model.Level.Number, "заведомо неверный код")
	if err != nil {
		t.Fatalf("SendCode: %v", err)
	}
	if after.EngineAction == nil || after.EngineAction.LevelAction == nil {
		t.Fatalf("the engine reported no action result: %+v", after.EngineAction)
	}
	if after.EngineAction.LevelAction.IsCorrectAnswer == nil {
		t.Fatal("IsCorrectAnswer is nil")
	}
	if *after.EngineAction.LevelAction.IsCorrectAnswer {
		t.Error("a wrong code was accepted")
	}
}

func TestMockCatalogAgreesAcrossEngines(t *testing.T) {
	legacy, api := newMockServers(t)
	ctx := context.Background()

	legacyClient := mockClient(t, legacy, encx.EngineLegacy)
	if _, err := legacyClient.Login(ctx, "player", "secret"); err != nil {
		t.Fatalf("legacy Login: %v", err)
	}
	legacyGames, err := legacyClient.GetDomainGames(ctx)
	if err != nil {
		t.Fatalf("legacy GetDomainGames: %v", err)
	}

	apiClient := mockClient(t, api, encx.EngineNew)
	if _, err := apiClient.Login(ctx, "player", "secret"); err != nil {
		t.Fatalf("new Login: %v", err)
	}
	newGames, err := apiClient.GetDomainGames(ctx)
	if err != nil {
		t.Fatalf("new GetDomainGames: %v", err)
	}

	if len(legacyGames) != len(newGames) {
		t.Fatalf("catalog sizes differ: %d vs %d", len(legacyGames), len(newGames))
	}
	for i := range legacyGames {
		if legacyGames[i] != newGames[i] {
			t.Errorf("game %d differs: %+v vs %+v", i, legacyGames[i], newGames[i])
		}
	}
}

// newMockServer builds a server with the default fixtures, as main does.
func newMockServer(t *testing.T) *server {
	t.Helper()
	fixtures, err := loadFixtures()
	if err != nil {
		t.Fatalf("loadFixtures: %v", err)
	}
	return &server{
		fixtures:              fixtures,
		sessions:              make(map[string]*sessionState),
		authStates:            make(map[string]*sessionState),
		silentUntil:           make(map[string]time.Time),
		antiBotAnswerAttempts: map[int]bool{},
	}
}
