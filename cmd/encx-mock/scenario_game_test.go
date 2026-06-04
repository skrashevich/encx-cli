package main

import (
	"testing"
	"time"

	"github.com/skrashevich/encx-cli/encx"
	"github.com/skrashevich/encx-cli/encx/scenario"
)

func TestAppStoreReviewScenarioFixture(t *testing.T) {
	doc, err := loadScenario("fixtures/app_store_review_scenario.html")
	if err != nil {
		t.Fatalf("loadScenario: %v", err)
	}
	if doc.GameTitle != "Encounter App Store Review" {
		t.Fatalf("GameTitle = %q", doc.GameTitle)
	}
	if doc.GameID != mockGameID {
		t.Fatalf("GameID = %d, want %d", doc.GameID, mockGameID)
	}
	if len(doc.Levels) != 3 {
		t.Fatalf("levels = %d, want 3", len(doc.Levels))
	}
	if got := doc.Levels[0].SectorAnswers; len(got) != 1 || len(got[0]) != 1 || got[0][0] != "REVIEW-START" {
		t.Fatalf("level 1 answers: %+v", got)
	}
	if got := doc.Levels[1].SectorAnswers; len(got) != 2 || got[0][0] != "MAP-101" || got[1][0] != "LOCK-202" {
		t.Fatalf("level 2 answers: %+v", got)
	}
	if bonuses := doc.Levels[1].Bonuses; len(bonuses) != 1 || len(bonuses[0].Answers) != 1 || bonuses[0].Answers[0] != "BONUS-5" {
		t.Fatalf("level 2 bonuses: %+v", bonuses)
	}
	if doc.Levels[2].AutopassSecond != 30 {
		t.Fatalf("level 3 autopass = %d, want 30", doc.Levels[2].AutopassSecond)
	}
}

func TestResolveScenarioPath(t *testing.T) {
	t.Setenv("ENCX_MOCK_SCENARIO", "/from/env")

	if got := resolveScenarioPath(""); got != "/from/env" {
		t.Fatalf("empty flag: got %q, want /from/env", got)
	}
	if got := resolveScenarioPath("/from/flag"); got != "/from/flag" {
		t.Fatalf("flag set: got %q, want /from/flag", got)
	}
}

func TestCompletedScenarioUsesFinishedEvent(t *testing.T) {
	s := &server{
		scenario: &scenario.Document{
			Levels: []scenario.Level{
				{Number: 1, Name: "Finish", SectorAnswers: [][]string{{"FINISH"}}},
			},
		},
	}
	st := &sessionState{
		Login:        "demo",
		CurrentIdx:   0,
		Passed:       []bool{true},
		SectorPassed: [][]bool{{true}},
		Completed:    true,
	}

	model, err := s.buildGameModelResponse(st, time.Now())
	if err != nil {
		t.Fatalf("buildGameModelResponse: %v", err)
	}
	if model["Event"] != encx.EventGameFinished {
		t.Fatalf("Event = %v, want %d", model["Event"], encx.EventGameFinished)
	}
}

func TestCompletedFixtureUsesFinishedEvent(t *testing.T) {
	fixtures, err := loadFixtures()
	if err != nil {
		t.Fatalf("loadFixtures: %v", err)
	}
	s := &server{fixtures: fixtures}
	st := &sessionState{
		Login:        "demo",
		CurrentIdx:   mockLevelCount - 1,
		Passed:       []bool{true, true, true},
		SectorPassed: [][]bool{{true, true, true, true}, {true, true, true, true}, {true, true, true, true}},
		Completed:    true,
	}

	model, err := s.buildGameModelResponse(st, time.Now())
	if err != nil {
		t.Fatalf("buildGameModelResponse: %v", err)
	}
	if model["Event"] != encx.EventGameFinished {
		t.Fatalf("Event = %v, want %d", model["Event"], encx.EventGameFinished)
	}
}
