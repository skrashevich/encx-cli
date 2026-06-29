package main

import (
	"testing"

	"github.com/skrashevich/encx-cli/encx/scenario"
)

func TestProcessScenarioAnswerCaseInsensitive(t *testing.T) {
	s := &server{
		scenario: &scenario.Document{
			Levels: []scenario.Level{
				{
					Number:        1,
					Name:          "Test level",
					SectorAnswers: [][]string{{"ТЕСТ"}},
				},
			},
		},
	}
	st := &sessionState{
		Login:        "demo",
		CurrentIdx:   0,
		Passed:       []bool{false},
		SectorPassed: [][]bool{{false}},
	}

	if !s.processScenarioAnswer(st, "тест") {
		t.Fatal("expected lowercase submission to match uppercase code")
	}
	if !st.SectorPassed[0][0] {
		t.Fatal("sector should be marked passed")
	}

	st2 := &sessionState{
		Login:        "demo",
		CurrentIdx:   0,
		Passed:       []bool{false},
		SectorPassed: [][]bool{{false}},
	}
	if !s.processScenarioAnswer(st2, "ТЕСТ") {
		t.Fatal("expected uppercase submission to match uppercase code")
	}
}

func TestProcessScenarioBonusAnswer(t *testing.T) {
	s := &server{
		scenario: &scenario.Document{
			Levels: []scenario.Level{
				{
					Number: 3,
					Name:   "Eyes",
					Bonuses: []scenario.Bonus{
						{Number: 19, Name: "глаз 19", AwardSeconds: 180, Answers: []string{"кон3"}},
					},
				},
			},
		},
	}
	st := &sessionState{
		Login:           "demo",
		CurrentIdx:      0,
		Passed:          []bool{false},
		SectorPassed:    [][]bool{{false}},
		AnsweredBonuses: make(map[int]bool),
		BonusAnswers:    make(map[int]string),
	}

	if !s.processScenarioAnswer(st, "кон3") {
		t.Fatal("expected bonus code to be accepted")
	}
	bonusID := scenarioBonusID(0, 19)
	if !st.AnsweredBonuses[bonusID] {
		t.Fatal("bonus 19 should be marked answered")
	}
	if st.BonusAnswers[bonusID] != "кон3" {
		t.Fatalf("bonus answer = %q", st.BonusAnswers[bonusID])
	}
	if st.LastAction == nil || st.LastAction.BonusAction == nil || st.LastAction.BonusAction.IsCorrectAnswer == nil || !*st.LastAction.BonusAction.IsCorrectAnswer {
		t.Fatal("expected correct BonusAction in last engine action")
	}
}

func TestProcessScenarioBonusAnswerAcceptsAnyVariant(t *testing.T) {
	s := &server{
		scenario: &scenario.Document{
			Levels: []scenario.Level{
				{
					Number: 3,
					Name:   "Eyes",
					Bonuses: []scenario.Bonus{
						{Number: 19, Name: "глаз 19", AwardSeconds: 180, Answers: []string{"кон3", "рим8"}},
					},
				},
			},
		},
	}
	st := &sessionState{
		Login:           "demo",
		CurrentIdx:      0,
		Passed:          []bool{false},
		SectorPassed:    [][]bool{{false}},
		AnsweredBonuses: make(map[int]bool),
		BonusAnswers:    make(map[int]string),
	}

	if !s.processScenarioAnswer(st, "рим8") {
		t.Fatal("expected alternate bonus code to be accepted")
	}
	bonusID := scenarioBonusID(0, 19)
	if st.BonusAnswers[bonusID] != "рим8" {
		t.Fatalf("bonus answer = %q", st.BonusAnswers[bonusID])
	}
}

func TestProcessScenarioAnswerDoesNotAcceptDefaultLevelCode(t *testing.T) {
	s := &server{
		scenario: &scenario.Document{
			Levels: []scenario.Level{
				{Number: 1, Name: "Test level", SectorAnswers: [][]string{{"scenario-code"}}},
			},
		},
	}
	st := &sessionState{
		Login:        "demo",
		CurrentIdx:   0,
		Passed:       []bool{false},
		SectorPassed: [][]bool{{false}},
	}

	if s.processScenarioAnswer(st, "CODE-1") {
		t.Fatal("default fixture code must not pass a scenario level")
	}
	if st.Passed[0] {
		t.Fatal("scenario level should remain active")
	}
}

func TestScenarioSectorAnswersRemainDistinct(t *testing.T) {
	s := &server{
		scenario: &scenario.Document{
			Levels: []scenario.Level{
				{Number: 1, Name: "Test level", SectorAnswers: [][]string{{"first"}, {"second"}}},
			},
		},
	}
	st := &sessionState{
		Login:        "demo",
		CurrentIdx:   0,
		Passed:       []bool{false},
		SectorPassed: [][]bool{{false, false}},
	}

	if !s.processScenarioAnswer(st, "first") || !s.processScenarioAnswer(st, "second") {
		t.Fatal("expected both scenario sector answers to be accepted")
	}
	lvl := s.scenario.Levels[0]
	if got := submittedSectorAnswer(st, 0, 0, lvl); got != "first" {
		t.Fatalf("sector 1 answer = %q, want first", got)
	}
	if got := submittedSectorAnswer(st, 0, 1, lvl); got != "second" {
		t.Fatalf("sector 2 answer = %q, want second", got)
	}
}
