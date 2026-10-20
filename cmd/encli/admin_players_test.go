package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/skrashevich/encx-cli/encx"
)

func TestParticipationToolsExposureAndReadonly(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		mutation bool
		required []string
	}{
		{"admin_players", false, []string{"game_id"}},
		{"admin_delete_player", true, []string{"game_id", "player_id"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			found := false
			for _, tool := range getTools() {
				if tool.Function.Name != tc.name {
					continue
				}
				found = true
				var schema struct {
					Required []string `json:"required"`
				}
				if err := json.Unmarshal(tool.Function.Parameters, &schema); err != nil {
					t.Fatal(err)
				}
				if strings.Join(schema.Required, ",") != strings.Join(tc.required, ",") {
					t.Errorf("required fields = %v", schema.Required)
				}
			}
			if !found {
				t.Fatalf("missing tool %s", tc.name)
			}
			if got := isMutationTool(tc.name); got != tc.mutation {
				t.Errorf("mutation = %t", got)
			}
			readonlyExposed := false
			for _, tool := range getToolsForSession(&llmSession{securityMode: SecurityModeReadonly}) {
				if tool.Function.Name == tc.name {
					readonlyExposed = true
				}
			}
			if readonlyExposed == tc.mutation {
				t.Errorf("readonly exposure = %t", readonlyExposed)
			}
		})
	}
}

func TestDeletePlayerReadonlyExecution(t *testing.T) {
	result := executeToolCallSafe(t.Context(), &config{}, nil,
		&llmSession{securityMode: SecurityModeReadonly}, "admin_delete_player", `{"game_id":83074,"player_id":17}`)
	if !strings.Contains(result, "blocked in read-only mode") {
		t.Fatal(result)
	}
}

func TestDeletePlayerRequiresEntryID(t *testing.T) {
	for _, args := range []string{`{"game_id":83074}`, `{"game_id":83074,"player_id":0}`, `{"game_id":83074,"player_id":-1}`} {
		result := executeToolCallSafe(t.Context(), &config{}, nil, nil, "admin_delete_player", args)
		if !strings.Contains(result, "player_id must be positive") {
			t.Fatal(result)
		}
	}
}

func TestParticipationToolsRequireGameID(t *testing.T) {
	for _, name := range []string{"admin_players", "admin_delete_player"} {
		for _, args := range []string{`{"player_id":17}`, `{"game_id":0,"player_id":17}`, `{"game_id":-1,"player_id":17}`} {
			result := executeToolCallSafe(t.Context(), &config{gameId: 42}, nil, nil, name, args)
			if !strings.Contains(result, "game_id must be positive") {
				t.Fatal(result)
			}
		}
	}
}

func TestParticipationCLIAndLLMExecution(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	present := true
	deletes := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/Administration/Games/LevelManager.aspx":
			fmt.Fprint(w, "admin")
		case "/GamePlayers.aspx":
			if r.URL.Query().Get("gid") != "83074" {
				t.Errorf("wrong game: %s", r.URL)
			}
			if r.URL.Query().Get("action") == "delete" {
				if r.URL.Query().Get("player") != "17" {
					t.Errorf("wrong entry: %s", r.URL)
				}
				deletes++
				present = false
				fmt.Fprint(w, "ok")
				return
			}
			if r.URL.Query().Get("status") != "-1" {
				t.Errorf("not all statuses: %s", r.URL)
			}
			fmt.Fprint(w, `<a id="lnkPlayersAll">All</a>`)
			if present {
				fmt.Fprint(w, `<span id="spanTotal">1</span><table><tr id="trPlayer"><td><a id="lnkPlayer" href="/UserDetails.aspx?uid=99">Player</a><a href="/GamePlayers.aspx?gid=83074&amp;action=delete&amp;player=17">Delete</a></td></tr></table>`)
			} else {
				fmt.Fprint(w, `<span id="spanTotal">0</span>`)
			}
		default:
			t.Errorf("unexpected request: %s", r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	cfg := &config{domain: strings.TrimPrefix(srv.URL, "http://"), gameId: 83074, jsonOutput: true}
	client := encx.New(cfg.domain, encx.WithHTTP(), encx.WithEngine(encx.EngineLegacy), encx.WithAdminDelay(0))
	saveSession(cfg, client)
	for _, via := range []string{"cli", "llm"} {
		t.Run(via, func(t *testing.T) {
			present = true
			var raw string
			if via == "cli" {
				raw = captureStdout(t, func() { cmdAdminPlayers(t.Context(), cfg, client) })
			} else {
				raw = executeToolCallSafe(t.Context(), cfg, client, nil, "admin_players", `{"game_id":83074}`)
			}
			var players []encx.AdminGamePlayer
			if err := json.Unmarshal([]byte(raw), &players); err != nil {
				t.Fatalf("%s: %v", raw, err)
			}
			if len(players) != 1 || players[0].PlayerID != 17 || players[0].UserID != 99 || !players[0].CanDelete {
				t.Fatalf("players = %+v", players)
			}
			before := deletes
			if via == "cli" {
				raw = captureStdout(t, func() { cmdAdminDeletePlayer(t.Context(), cfg, client, []string{"17"}) })
			} else {
				raw = executeToolCallSafe(t.Context(), cfg, client, nil, "admin_delete_player", `{"game_id":83074,"player_id":17}`)
			}
			var result struct {
				Success  bool
				PlayerID int `json:"player_id"`
			}
			if err := json.Unmarshal([]byte(raw), &result); err != nil {
				t.Fatalf("%s: %v", raw, err)
			}
			if !result.Success || result.PlayerID != 17 || present || deletes != before+1 {
				t.Fatalf("unexpected deletion: %s, present=%t deletes=%d", raw, present, deletes)
			}
		})
	}
}
