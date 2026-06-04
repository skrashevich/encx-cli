package main

import (
	"testing"
	"time"

	"github.com/skrashevich/encx-cli/encx/scenario"
)

func TestScenarioHelpsLockedUntilDelay(t *testing.T) {
	start := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	st := &sessionState{
		LevelStartedAt: []time.Time{start},
	}
	lvl := scenario.Level{
		Hints: []scenario.Hint{
			{Title: "Подсказка №1 для всех (1 час)", Text: "secret 1", DelaySeconds: 3600},
			{Title: "Подсказка №2 для всех (2 часа)", Text: "secret 2", DelaySeconds: 7200},
		},
	}

	helps := scenarioHelps(st, 0, lvl, start.Add(30*time.Minute))
	if helps[0]["HelpText"] != nil {
		t.Fatal("hint 1 should stay locked after 30 minutes")
	}
	if helps[0]["RemainSeconds"] != 1800 {
		t.Fatalf("hint 1 remain=%v want 1800", helps[0]["RemainSeconds"])
	}
	if helps[1]["HelpText"] != nil {
		t.Fatal("hint 2 should stay locked after 30 minutes")
	}
	if helps[1]["RemainSeconds"] != 5400 {
		t.Fatalf("hint 2 remain=%v want 5400", helps[1]["RemainSeconds"])
	}

	before := scenarioHelps(st, 0, lvl, start.Add(2*time.Hour-time.Minute))
	if before[1]["HelpText"] != nil {
		t.Fatal("hint 2 should stay locked before 2 hours")
	}
	if before[1]["RemainSeconds"] != 60 {
		t.Fatalf("hint 2 remain=%v want 60", before[1]["RemainSeconds"])
	}

	open := scenarioHelps(st, 0, lvl, start.Add(2*time.Hour))
	if open[0]["HelpText"] == nil {
		t.Fatal("hint 1 should open after 2 hours")
	}
	if open[0]["RemainSeconds"] != 0 {
		t.Fatalf("hint 1 remain=%v want 0", open[0]["RemainSeconds"])
	}
	if open[1]["HelpText"] == nil {
		t.Fatal("hint 2 should open at 2 hours")
	}
	if open[1]["RemainSeconds"] != 0 {
		t.Fatalf("hint 2 remain=%v want 0", open[1]["RemainSeconds"])
	}
}
