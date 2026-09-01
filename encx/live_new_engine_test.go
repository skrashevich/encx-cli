package encx

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"testing"
	"time"
)

// Live tests run against a real Encounter domain served by the new engine.
// They are skipped unless the credentials are supplied through the environment,
// so no secret ever reaches the repository:
//
//	ENCX_LIVE_DOMAIN=demo.en.cx ENCX_LIVE_LOGIN=… ENCX_LIVE_PASSWORD=… \
//	  go test ./encx/ -run TestLiveNewEngine -v
//
// ENCX_LIVE_GAME_ID optionally names a game to exercise the engine and
// statistics routes.
const (
	liveDomainEnv   = "ENCX_LIVE_DOMAIN"
	liveLoginEnv    = "ENCX_LIVE_LOGIN"
	livePasswordEnv = "ENCX_LIVE_PASSWORD"
	liveGameEnv     = "ENCX_LIVE_GAME_ID"
)

func liveClient(t *testing.T) *Client {
	t.Helper()
	domain := os.Getenv(liveDomainEnv)
	login := os.Getenv(liveLoginEnv)
	password := os.Getenv(livePasswordEnv)
	if domain == "" || login == "" || password == "" {
		t.Skipf("set %s, %s and %s to run live tests", liveDomainEnv, liveLoginEnv, livePasswordEnv)
	}

	c := New(domain, WithEngine(EngineNew), WithTimeout(30*time.Second))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := c.LoginComplete(ctx, login, password); err != nil {
		t.Fatalf("LoginComplete against %s: %v", domain, err)
	}
	return c
}

func liveContext(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 30*time.Second)
}

func TestLiveNewEngineAutoDetectsTheBackend(t *testing.T) {
	domain := os.Getenv(liveDomainEnv)
	if domain == "" {
		t.Skipf("set %s to run live tests", liveDomainEnv)
	}
	c := New(domain, WithEngine(EngineAuto), WithTimeout(30*time.Second))
	if got := c.Engine(); got != EngineNew {
		t.Errorf("Engine() = %q for %s, want new", got, domain)
	}
}

func TestLiveNewEngineProfile(t *testing.T) {
	c := liveClient(t)
	ctx, cancel := liveContext(t)
	defer cancel()

	profile, err := c.GetProfile(ctx)
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	if profile.ID == 0 || profile.Login == "" {
		t.Errorf("profile = %+v, want an identified user", profile)
	}
	if profile.Login != os.Getenv(liveLoginEnv) {
		t.Errorf("Login = %q, want the account we signed in as", profile.Login)
	}
	t.Logf("profile: id=%d login=%s team=%q rank=%q", profile.ID, profile.Login, profile.Team, profile.Rank)
}

func TestLiveNewEngineCatalog(t *testing.T) {
	c := liveClient(t)
	ctx, cancel := liveContext(t)
	defer cancel()

	list, err := c.GetGameList(ctx)
	if err != nil {
		t.Fatalf("GetGameList: %v", err)
	}
	if len(list.ActiveGames)+len(list.ComingGames) == 0 {
		t.Skip("the domain has no games to inspect")
	}
	for _, game := range append(append([]GameInfo{}, list.ActiveGames...), list.ComingGames...) {
		if game.GameID == 0 || game.Title == "" {
			t.Errorf("game = %+v, want an id and a title", game)
		}
		if game.StartDateTime == nil || game.StartDateTime.Timestamp <= 0 {
			t.Errorf("game %d has no start time", game.GameID)
		}
	}

	games, err := c.GetDomainGames(ctx)
	if err != nil {
		t.Fatalf("GetDomainGames: %v", err)
	}
	seen := map[int]bool{}
	for _, game := range games {
		if seen[game.GameId] {
			t.Errorf("game %d listed twice", game.GameId)
		}
		seen[game.GameId] = true
	}
	t.Logf("catalog: %d active, %d coming, %d unique", len(list.ActiveGames), len(list.ComingGames), len(games))
}

func liveGameID(t *testing.T, c *Client) int {
	t.Helper()
	if raw := os.Getenv(liveGameEnv); raw != "" {
		id, err := strconv.Atoi(raw)
		if err != nil {
			t.Fatalf("%s = %q is not a number", liveGameEnv, raw)
		}
		return id
	}
	ctx, cancel := liveContext(t)
	defer cancel()
	games, err := c.GetDomainGames(ctx)
	if err != nil || len(games) == 0 {
		t.Skipf("no game to test against (set %s): %v", liveGameEnv, err)
	}
	return games[0].GameId
}

func TestLiveNewEngineGameModel(t *testing.T) {
	c := liveClient(t)
	gameID := liveGameID(t, c)
	ctx, cancel := liveContext(t)
	defer cancel()

	model, err := c.GetGameModel(ctx, gameID)
	if err != nil {
		t.Fatalf("GetGameModel(%d): %v", gameID, err)
	}
	if model.GameId != gameID {
		t.Errorf("GameId = %d, want %d", model.GameId, gameID)
	}
	if model.GameTitle == "" {
		t.Error("GameTitle is empty")
	}
	if model.Login == "" || model.UserId == 0 {
		t.Errorf("the state does not identify the player: login=%q user=%d", model.Login, model.UserId)
	}
	// GameDateTimeStart must reach callers in the legacy display format, not as
	// the raw Unix seconds the new engine sends.
	if start := model.GameDateTimeStart; start != "" {
		if _, err := time.Parse(engineTimeLayout, start); err != nil {
			t.Errorf("GameDateTimeStart = %q, want %q", start, engineTimeLayout)
		}
	}
	event, ok := model.Event.(int)
	if !ok {
		t.Fatalf("Event = %v (%T), want an int", model.Event, model.Event)
	}
	t.Logf("game %d: event=%d (%s) levels=%d", gameID, event, EventText(event), len(model.Levels))
}

func TestLiveNewEngineGameDetails(t *testing.T) {
	c := liveClient(t)
	gameID := liveGameID(t, c)
	ctx, cancel := liveContext(t)
	defer cancel()

	body, err := c.GetGameDetails(ctx, gameID)
	if err != nil {
		t.Fatalf("GetGameDetails(%d): %v", gameID, err)
	}
	var details struct {
		Game struct {
			ID int `json:"id"`
		} `json:"game"`
	}
	if err := json.Unmarshal([]byte(body), &details); err != nil {
		t.Fatalf("details are not JSON: %v", err)
	}
	if details.Game.ID != gameID {
		t.Errorf("details game id = %d, want %d", details.Game.ID, gameID)
	}

	if _, err := c.GetTimeoutToGame(ctx, gameID); err != nil {
		t.Errorf("GetTimeoutToGame(%d): %v", gameID, err)
	}
}

func TestLiveNewEngineStatistics(t *testing.T) {
	c := liveClient(t)
	gameID := liveGameID(t, c)
	ctx, cancel := liveContext(t)
	defer cancel()

	stats, err := c.GetGameStatistics(ctx, gameID)
	if err != nil {
		t.Skipf("statistics are not available for game %d: %v", gameID, err)
	}
	if len(stats.Levels) == 0 {
		t.Skip("the game has no levels with statistics")
	}
	if len(stats.StatItems) == 0 {
		t.Skip("the game has no recorded results")
	}

	rows := 0
	for _, group := range stats.StatItems {
		rows += len(group)
	}
	if rows == 0 {
		t.Error("StatItems groups are all empty")
	}
	// Aggregate groups carry negative level numbers and must survive the mapping.
	levelNums := map[int]bool{}
	for _, group := range stats.StatItems {
		for _, item := range group {
			levelNums[item.LevelNum] = true
		}
	}
	// Time corrections turn raw level time into standing time. The new engine
	// publishes them in a list of their own instead of inside every row, so a
	// mapping that ignores that list ranks teams differently from the game's
	// official result.
	corrected := 0
	for _, group := range stats.StatItems {
		for _, item := range group {
			if item.Corrections != nil {
				corrected++
			}
		}
	}
	t.Logf("statistics: %d levels, %d groups, %d rows, %d corrected rows, level numbers %v",
		len(stats.Levels), len(stats.StatItems), rows, corrected, sortedKeys(levelNums))

	// The counts published per level have to match the rows the same document
	// carries: they are derived from them, so a partially read table would
	// report totals that are wrong rather than merely short.
	for _, level := range stats.Levels {
		counted := 0
		for _, group := range stats.StatItems {
			for _, item := range group {
				if item.LevelNum == level.LevelNumber {
					counted++
				}
			}
		}
		if level.PassedPlayers != counted {
			t.Errorf("level %d reports %d players but carries %d rows",
				level.LevelNumber, level.PassedPlayers, counted)
		}
	}
}

func sortedKeys(set map[int]bool) []int {
	keys := make([]int, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

func TestLiveNewEngineSessionRoundTrip(t *testing.T) {
	c := liveClient(t)
	data, err := c.ExportCookies()
	if err != nil {
		t.Fatalf("ExportCookies: %v", err)
	}

	restored := New(os.Getenv(liveDomainEnv), WithEngine(EngineNew), WithTimeout(30*time.Second))
	if err := restored.ImportCookies(data); err != nil {
		t.Fatalf("ImportCookies: %v", err)
	}
	if restored.APIToken() == "" {
		t.Fatal("the restored client has no bearer token")
	}

	ctx, cancel := liveContext(t)
	defer cancel()
	if err := restored.VerifyAdminSession(ctx); err != nil {
		t.Errorf("the restored session was rejected: %v", err)
	}
}
