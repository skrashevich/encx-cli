package main

import (
	"testing"
	"time"

	"github.com/skrashevich/encx-cli/encx/scenario"
)

func TestScenarioLevelTimeout(t *testing.T) {
	lvl := scenario.Level{AutopassSecond: 180}
	started := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	now := started.Add(90 * time.Second)

	timeout, remain, award := scenarioLevelTimeout(lvl, started, now)
	if timeout != 180 || remain != 90 || award != 0 {
		t.Fatalf("timeout=%d remain=%d award=%d", timeout, remain, award)
	}
}

func TestApplyScenarioAutopassAdvancesLevel(t *testing.T) {
	s := &server{
		scenario: &scenario.Document{
			Levels: []scenario.Level{
				{Number: 1, Name: "L1", AutopassSecond: 0},
				{Number: 2, Name: "L2", AutopassSecond: 180},
				{Number: 3, Name: "L3", AutopassSecond: 0},
			},
		},
	}
	start := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	st := &sessionState{
		CurrentIdx:     1,
		Passed:         []bool{true, false, false},
		SectorPassed:   [][]bool{{true}, {false}, {false}},
		LevelStartedAt: []time.Time{start, start, {}},
	}
	s.applyScenarioAutopass(st, start.Add(3*time.Minute))

	if st.CurrentIdx != 2 {
		t.Fatalf("CurrentIdx = %d, want 2", st.CurrentIdx)
	}
	if !st.Passed[1] {
		t.Fatal("level 2 should be marked passed")
	}
	if st.LevelStartedAt[2].IsZero() {
		t.Fatal("level 3 clock should start after autopass")
	}
}
