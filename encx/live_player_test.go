package encx

import (
	"encoding/json"
	"strings"
	"testing"
)

// The player-facing half of the parity check. Like the admin round-trip it runs
// against the domain named by ENCX_LIVE_DOMAIN and only reads, so it is safe on
// games this account does not own:
//
//	ENCX_LIVE_DOMAIN=demo.en.cx ENCX_LIVE_LOGIN=… ENCX_LIVE_PASSWORD=… \
//	  go test ./encx/ -run TestLivePlayer -v

// TestLivePlayerGameModelLevel reads a single level of a game the way a replay
// or a scenario reader does.
func TestLivePlayerGameModelLevel(t *testing.T) {
	c := liveClient(t)
	gameID := liveGameID(t, c)
	ctx, cancel := liveContext(t)
	defer cancel()

	model, err := c.GetGameModel(ctx, gameID)
	if err != nil {
		t.Fatalf("GetGameModel(%d): %v", gameID, err)
	}
	if len(model.Levels) == 0 || model.Level == nil {
		t.Skip("the game publishes no level list")
	}
	number := model.Levels[0].LevelNumber

	level, err := c.GetGameModelLevel(ctx, gameID, number)
	if err != nil {
		t.Fatalf("GetGameModelLevel(%d, %d): %v", gameID, number, err)
	}
	if level.GameId != gameID {
		t.Errorf("GameId = %d, want %d", level.GameId, gameID)
	}
	if level.Level == nil {
		t.Fatalf("GetGameModelLevel(%d) returned no level", number)
	}
	// The level parameter selects a level only in an assault game, where the
	// player picks which one to attack; the specification calls it "номер уровня
	// (штурм)" and the legacy client says the same. Everywhere else both engines
	// answer with the level the player is actually on, so the requested number is
	// only binding when the game lets a player choose.
	if level.LevelSequence == SequenceAssault && level.Level.Number != number {
		t.Errorf("assault game returned level %d, want the requested %d", level.Level.Number, number)
	}
	if level.Level.Number != model.Level.Number && level.LevelSequence != SequenceAssault {
		t.Errorf("level number = %d, want the current level %d", level.Level.Number, model.Level.Number)
	}
	// The engine reports timestamps as Unix seconds in both fields, so a level
	// that has a start time must carry a consistent pair rather than a zero.
	if start := level.Level.StartTime; start != nil {
		if start.Timestamp <= 0 || int64(start.Value) != start.Timestamp {
			t.Errorf("level start = %+v, want matching Unix seconds", start)
		}
	}
	t.Logf("level %d %q: %d tasks, %d sectors, %d helps, %d bonuses",
		level.Level.Number, level.Level.Name, len(level.Level.Tasks),
		len(level.Level.Sectors), len(level.Level.Helps), len(level.Level.Bonuses))
}

// TestLivePlayerTeams walks the team routes the legacy engine served as HTML.
func TestLivePlayerTeams(t *testing.T) {
	c := liveClient(t)
	ctx, cancel := liveContext(t)
	defer cancel()

	profile, err := c.GetProfile(ctx)
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	if profile.TeamID == 0 {
		t.Skip("the account is not in a team")
	}

	body, err := c.GetTeamDetails(ctx, profile.TeamID)
	if err != nil {
		t.Fatalf("GetTeamDetails(%d): %v", profile.TeamID, err)
	}
	var team struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(body), &team); err != nil {
		t.Fatalf("team details are not JSON: %v", err)
	}
	if team.ID != profile.TeamID {
		t.Errorf("team id = %d, want %d", team.ID, profile.TeamID)
	}

	mine, err := c.GetMyTeamDetails(ctx)
	if err != nil {
		t.Fatalf("GetMyTeamDetails: %v", err)
	}
	if mine != body {
		t.Error("GetMyTeamDetails and GetTeamDetails disagree about the same team")
	}

	info, err := c.GetTeamManagementInfo(ctx, profile.TeamID)
	if err != nil {
		t.Fatalf("GetTeamManagementInfo: %v", err)
	}
	if info.TeamID != profile.TeamID || info.TeamName != team.Name {
		t.Errorf("management info = %+v, want team %d %q", info, profile.TeamID, team.Name)
	}

	if _, err := c.GetTeamInvitations(ctx); err != nil {
		t.Errorf("GetTeamInvitations: %v", err)
	}
}

// TestLivePlayerScenario reads the structured scenario, which replaces the
// legacy HTML export.
//
// It builds its own game: the scenario of somebody else's game is gated by that
// game's scenario_availability, and a "forbidden" answer would say nothing about
// whether the mapping is right.
func TestLivePlayerScenario(t *testing.T) {
	c := liveClient(t)
	gameID := scratchLevels(t, c, 1)
	ctx, cancel := liveContext(t)
	defer cancel()

	if err := c.AdminCreateTask(ctx, gameID, 1, AdminTask{Text: "задание сценария"}); err != nil {
		t.Fatalf("AdminCreateTask: %v", err)
	}
	// Two sectors, not one: a level with a single sector publishes its answers
	// as a flat list with no sector heading, which is what the legacy HTML
	// export did as well, so one sector would not exercise the naming at all.
	sector := AdminSector{Name: "сектор сценария", Answers: []string{"ответ"}}
	if err := c.AdminCreateSector(ctx, gameID, 1, sector); err != nil {
		t.Fatalf("AdminCreateSector: %v", err)
	}
	second := AdminSector{Name: "второй сектор", Answers: []string{"ещё ответ"}}
	if err := c.AdminCreateSector(ctx, gameID, 1, second); err != nil {
		t.Fatalf("AdminCreateSector (second): %v", err)
	}
	if err := c.AdminCreateHint(ctx, gameID, 1, AdminHint{Text: "подсказка сценария", Minutes: 5}); err != nil {
		t.Fatalf("AdminCreateHint: %v", err)
	}

	doc, err := c.GetGameScenario(ctx, gameID)
	if err != nil {
		t.Fatalf("GetGameScenario(%d): %v", gameID, err)
	}
	if doc.GameID != 0 && doc.GameID != gameID {
		t.Errorf("scenario game id = %d, want %d", doc.GameID, gameID)
	}
	if len(doc.Levels) != 1 {
		t.Fatalf("scenario has %d levels, want 1", len(doc.Levels))
	}
	level := doc.Levels[0]
	if len(level.Tasks) == 0 || !strings.Contains(level.Tasks[0], "задание сценария") {
		t.Errorf("scenario level tasks = %+v", level.Tasks)
	}
	if len(level.Sectors) != 2 || level.Sectors[0].Name != sector.Name || level.Sectors[1].Name != second.Name {
		t.Errorf("scenario level sectors = %+v", level.Sectors)
	}
	if len(level.SectorAnswers) != len(level.Sectors) {
		t.Errorf("SectorAnswers has %d entries for %d sectors; the importer reads them by position",
			len(level.SectorAnswers), len(level.Sectors))
	}
	if len(level.Hints) == 0 || !strings.Contains(level.Hints[0].Text, "подсказка сценария") {
		t.Errorf("scenario level hints = %+v", level.Hints)
	}
	t.Logf("scenario of game %d: %d levels, first has %d tasks / %d sectors / %d hints",
		gameID, len(doc.Levels), len(level.Tasks), len(level.Sectors), len(level.Hints))

	// The HTML export exists only on the legacy engine. The method must say so
	// and name the replacement rather than return an empty page.
	if _, err := c.GetGameScenarioHTML(ctx, gameID); err == nil {
		t.Error("GetGameScenarioHTML succeeded on the new engine, which has no HTML export")
	} else if !strings.Contains(err.Error(), "GetGameScenario") {
		t.Errorf("error = %v, want it to name GetGameScenario", err)
	}
}

// TestLivePlayerFetchResource resolves a site-relative media reference, which on
// the new engine hangs off the API host rather than the site domain.
func TestLivePlayerFetchResource(t *testing.T) {
	c := liveClient(t)
	ctx, cancel := liveContext(t)
	defer cancel()

	resource, err := c.FetchResource(ctx, "/favicon.ico")
	if err != nil {
		t.Skipf("the domain serves no /favicon.ico through the API host: %v", err)
	}
	if len(resource.Data) == 0 {
		t.Error("FetchResource returned an empty body")
	}
	t.Logf("fetched %d bytes of %s", len(resource.Data), resource.ContentType)
}

// TestLivePlayerCatalogTimeout checks the countdown the legacy engine computed
// server-side and the new one derives from the fee box.
func TestLivePlayerCatalogTimeout(t *testing.T) {
	c := liveClient(t)
	ctx, cancel := liveContext(t)
	defer cancel()

	list, err := c.GetGameList(ctx)
	if err != nil {
		t.Fatalf("GetGameList: %v", err)
	}
	if len(list.ComingGames) == 0 {
		t.Skip("the domain has no upcoming game")
	}
	game := list.ComingGames[0]
	if game.TSRemain == nil {
		t.Errorf("game %d starts in the future but carries no countdown", game.GameID)
	}

	seconds, err := c.GetTimeoutToGame(ctx, game.GameID)
	if err != nil {
		t.Fatalf("GetTimeoutToGame(%d): %v", game.GameID, err)
	}
	if seconds == nil {
		t.Skipf("game %d publishes no seconds_to_start", game.GameID)
	}
	if *seconds <= 0 {
		t.Errorf("seconds to start = %d, want a positive countdown", *seconds)
	}
	t.Logf("game %d starts in %d seconds", game.GameID, *seconds)
}
