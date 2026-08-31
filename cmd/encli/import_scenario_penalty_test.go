package main

import (
	"testing"

	"github.com/skrashevich/encx-cli/encx/scenario"
)

func TestScenarioPenaltyHintToAdminHint(t *testing.T) {
	got, ok := scenarioPenaltyHintToAdminHint(scenario.PenaltyHint{
		Text:           "D45R54",
		DelaySeconds:   30 * 60,
		PenaltySeconds: 3600 + 15*60,
		RequestConfirm: true,
		Comment:        "Код 1",
	})
	if !ok {
		t.Fatal("conversion refused a valid penalty hint")
	}
	if !got.IsPenalty {
		t.Error("IsPenalty = false, want true")
	}
	if got.Minutes != 30 || got.Hours != 0 || got.Seconds != 0 {
		t.Errorf("delay = %dh%dm%ds, want 0h30m0s", got.Hours, got.Minutes, got.Seconds)
	}
	if got.PenaltyHours != 1 || got.PenaltyMinutes != 15 {
		t.Errorf("penalty = %dh%dm, want 1h15m", got.PenaltyHours, got.PenaltyMinutes)
	}
	if !got.RequestConfirm {
		t.Error("RequestConfirm = false, want true")
	}
	if got.PenaltyComment != "Код 1" {
		t.Errorf("PenaltyComment = %q, want %q", got.PenaltyComment, "Код 1")
	}

	if _, ok := scenarioPenaltyHintToAdminHint(scenario.PenaltyHint{Text: "  "}); ok {
		t.Error("empty penalty hint was accepted")
	}
}

func TestScenarioHintKeysSeparatePenaltyHints(t *testing.T) {
	keys := scenarioHintKeys(scenario.Level{
		Hints:        []scenario.Hint{{Text: "обычная", DelaySeconds: 900}},
		PenaltyHints: []scenario.PenaltyHint{{Text: "обычная", DelaySeconds: 900, PenaltySeconds: 900}},
	})
	if len(keys) != 2 {
		t.Fatalf("keys = %v, want 2 entries", keys)
	}
	if keys[0] == keys[1] {
		t.Errorf("penalty hint shares a key with the regular hint: %q", keys[0])
	}
}

func TestNamesEquivalentIgnoresWhitespaceNoise(t *testing.T) {
	if !namesEquivalent("lvl 2 спойлер Попытки  ", "lvl 2 спойлер Попытки") {
		t.Error("trailing spaces made the names differ, sync would rewrite on every run")
	}
	if !namesEquivalent("Замена трешки 2    (9 8)", "Замена трешки 2 (9 8)") {
		t.Error("collapsed spaces made the names differ, sync would recreate sectors on every run")
	}
	if namesEquivalent("Первый", "Второй") {
		t.Error("different names compared equal")
	}
}

func TestPenaltyHintKeyCoversConfirmAndComment(t *testing.T) {
	base := scenario.PenaltyHint{Text: "код", DelaySeconds: 1800, PenaltySeconds: 900, RequestConfirm: true, Comment: "Код 1"}
	noConfirm := base
	noConfirm.RequestConfirm = false
	otherComment := base
	otherComment.Comment = "Код 2"

	keys := map[string]string{}
	for name, hint := range map[string]scenario.PenaltyHint{"base": base, "noConfirm": noConfirm, "otherComment": otherComment} {
		got := scenarioHintKeys(scenario.Level{PenaltyHints: []scenario.PenaltyHint{hint}})
		if len(got) != 1 {
			t.Fatalf("%s: keys = %v", name, got)
		}
		if prev, dup := keys[got[0]]; dup {
			t.Errorf("%s and %s share a key, a changed penalty hint would never re-sync", name, prev)
		}
		keys[got[0]] = name
	}
}

func TestImportLevelNameKeepsTrailingSpaces(t *testing.T) {
	if got := importLevelName(3, "lvl 2 спойлер Попытки  "); got != "lvl 2 спойлер Попытки  " {
		t.Errorf("name = %q, trailing spaces were trimmed", got)
	}
	if got := importLevelName(3, "   "); got != "Уровень 3" {
		t.Errorf("blank name = %q, want fallback", got)
	}
}

func TestScenarioAdminSectorsKeepVerbatimNames(t *testing.T) {
	sectors := scenarioAdminSectors(scenario.Level{
		Sectors: []scenario.Sector{
			{Name: "Замена трешки 2    (9 8 10 12 11)", Answers: []string{"код"}},
			{Name: "  ", Answers: []string{"код2"}},
		},
	})
	if len(sectors) != 2 {
		t.Fatalf("sectors = %d, want 2", len(sectors))
	}
	if sectors[0].Name != "Замена трешки 2    (9 8 10 12 11)" {
		t.Errorf("name = %q, spaces were collapsed", sectors[0].Name)
	}
	if sectors[1].Name != "Сектор 2" {
		t.Errorf("blank name = %q, want fallback", sectors[1].Name)
	}
}
