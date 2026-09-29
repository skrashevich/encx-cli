package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/skrashevich/encx-cli/encx"
)

func TestPlayerGameModelSelectsAssaultLevel(t *testing.T) {
	var requestedLevel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedLevel = r.URL.Query().Get("level")
		_, _ = fmt.Fprint(w, `{"GameId":82913,"LevelSequence":3,"Level":{"LevelId":811,"Number":2,"Name":"Второй"}}`)
	}))
	t.Cleanup(server.Close)
	c := encx.New(strings.TrimPrefix(server.URL, "http://"), encx.WithHTTP(), encx.WithEngine(encx.EngineLegacy))
	model, err := playerGameModel(t.Context(), &config{gameId: 82913, levelNumber: 2}, c)
	if err != nil {
		t.Fatal(err)
	}
	if requestedLevel != "2" || model.Level.Number != 2 {
		t.Fatalf("request level = %q, result = %+v", requestedLevel, model.Level)
	}
}

func TestPlayerGameModelRejectsIgnoredLevelSelection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"GameId":82913,"LevelSequence":0,"Level":{"LevelId":811,"Number":1}}`)
	}))
	t.Cleanup(server.Close)
	c := encx.New(strings.TrimPrefix(server.URL, "http://"), encx.WithHTTP(), encx.WithEngine(encx.EngineLegacy))
	_, err := playerGameModel(t.Context(), &config{gameId: 82913, levelNumber: 2}, c)
	if err == nil || !strings.Contains(err.Error(), "does not use assault") {
		t.Fatalf("error = %v", err)
	}
}

func TestSendCodeAddressesSelectedAssaultLevel(t *testing.T) {
	var sentLevelID, sentLevelNumber, sentCode string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			sentLevelID = r.Form.Get("LevelId")
			sentLevelNumber = r.Form.Get("LevelNumber")
			sentCode = r.Form.Get("LevelAction.Answer")
		}
		_, _ = fmt.Fprint(w, `{"GameId":82913,"LevelSequence":3,"Level":{"LevelId":812,"Number":2}}`)
	}))
	t.Cleanup(server.Close)
	c := encx.New(strings.TrimPrefix(server.URL, "http://"), encx.WithHTTP(), encx.WithEngine(encx.EngineLegacy))
	cmdSendCode(t.Context(), &config{gameId: 82913, levelNumber: 2, jsonOutput: true}, c, []string{"CODE"})
	if sentLevelID != "812" || sentLevelNumber != "2" || sentCode != "CODE" {
		t.Fatalf("sent level=%s number=%s code=%s", sentLevelID, sentLevelNumber, sentCode)
	}
}

func TestLevelSequenceNames(t *testing.T) {
	for _, tt := range []struct {
		name string
		id   int
	}{
		{"linear", 0}, {"specified", 1}, {"random", 2}, {"assault", 3}, {"dynamic-random", 4},
	} {
		id, ok := levelSequenceID(tt.name)
		if !ok || id != tt.id || levelSequenceName(id) != tt.name {
			t.Errorf("%q => %d/%v/%q", tt.name, id, ok, levelSequenceName(id))
		}
	}
}
