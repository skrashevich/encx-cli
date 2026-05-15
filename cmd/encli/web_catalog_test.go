package main

import (
	"testing"

	"github.com/skrashevich/encx-cli/encx"
)

func TestMergeWebGameOptions(t *testing.T) {
	list := &encx.GameListResponse{
		ActiveGames: []encx.GameInfo{
			{GameID: 1, Title: "Active", Finished: false},
			{GameID: 99, Title: "Done", Finished: true},
		},
		ComingGames: []encx.GameInfo{
			{GameID: 2, Title: "Soon", Finished: false},
		},
	}
	admin := []encx.AdminGame{
		{ID: 1, Title: "Active admin"},
		{ID: 3, Title: "Admin only"},
	}
	got := mergeWebGameOptions(list, admin)
	if len(got) != 3 {
		t.Fatalf("want 3 games, got %d: %+v", len(got), got)
	}
	byID := map[int]webGameOption{}
	for _, g := range got {
		byID[g.ID] = g
	}
	if byID[1].Role != "both" {
		t.Fatalf("game 1 role: %s", byID[1].Role)
	}
	if byID[3].Role != "admin" {
		t.Fatalf("game 3 role: %s", byID[3].Role)
	}
	if _, ok := byID[99]; ok {
		t.Fatal("finished game should be excluded")
	}
}
