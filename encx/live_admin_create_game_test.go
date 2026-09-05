package encx

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// Creating a game is the one admin operation that cannot be exercised against a
// scratch game, because it is what makes one. The test therefore creates a game
// of its own on the live legacy domain and removes it again afterwards:
//
//	ENCX_LIVE_DOMAIN=svk.en.cx ENCX_LIVE_LOGIN=… ENCX_LIVE_PASSWORD=… \
//	  go test ./encx/ -run TestLiveAdminCreateGameLegacy -v

// liveLegacyClient logs in to the live domain and fails unless it speaks the
// legacy engine, since GameCreate.aspx only exists there.
func liveLegacyClient(t *testing.T) *Client {
	t.Helper()
	domain := os.Getenv(liveDomainEnv)
	login := os.Getenv(liveLoginEnv)
	password := os.Getenv(livePasswordEnv)
	if domain == "" || login == "" || password == "" {
		t.Skipf("set %s, %s and %s to run live tests", liveDomainEnv, liveLoginEnv, livePasswordEnv)
	}

	probe := New(domain, WithEngine(EngineAuto), WithTimeout(60*time.Second))
	if engine := probe.Engine(); engine != EngineLegacy {
		t.Skipf("%s runs the %s engine, this test needs a legacy domain", domain, engine)
	}

	c := New(domain, WithEngine(EngineLegacy), WithTimeout(60*time.Second))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := c.LoginComplete(ctx, login, password); err != nil {
		t.Fatalf("LoginComplete against %s: %v", domain, err)
	}
	return c
}

func TestLiveAdminCreateGameLegacy(t *testing.T) {
	c := liveLegacyClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	title := fmt.Sprintf("encx create %d", time.Now().UnixNano())
	params := AdminCreateGameParams{
		Title:          title,
		Description:    "temporary game created by the encx live create test",
		GameType:       1,
		ZoneID:         1,
		StartDateTime:  time.Now().Add(48 * time.Hour).Format(time.RFC3339),
		FinishDateTime: time.Now().Add(72 * time.Hour).Format(time.RFC3339),
	}

	id, err := c.AdminCreateGame(ctx, params)
	if err != nil {
		t.Fatalf("legacyAdminCreateGame: %v", err)
	}
	if id == 0 {
		t.Fatal("legacyAdminCreateGame returned game id 0")
	}
	t.Logf("created game %d", id)

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		u := fmt.Sprintf("%s/Administration/GamesManager.aspx?gid=%d&page=1&action=Delete", c.baseURL(), id)
		if _, err := c.doGet(ctx, u); err != nil {
			t.Errorf("delete game %d: %v", id, err)
			return
		}
		t.Logf("deleted game %d", id)
	})

	info, err := c.AdminGetGameInfo(ctx, id)
	if err != nil {
		t.Fatalf("AdminGetGameInfo(%d): %v", id, err)
	}
	if info.Title != title {
		t.Errorf("title = %q, want %q", info.Title, title)
	}
	if !strings.Contains(info.Description, "encx live create test") {
		t.Errorf("description = %q, want it to carry the text we sent", info.Description)
	}

	games, err := c.AdminGetGames(ctx)
	if err != nil {
		t.Fatalf("AdminGetGames: %v", err)
	}
	found := false
	for _, game := range games {
		if game.ID == id {
			found = true
			if game.Title != title {
				t.Errorf("listed title = %q, want %q", game.Title, title)
			}
			break
		}
	}
	if !found {
		t.Errorf("game %d is missing from AdminGetGames (%d games listed)", id, len(games))
	}
}
