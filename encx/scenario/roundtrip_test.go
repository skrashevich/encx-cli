package scenario

import (
	"os"
	"strings"
	"testing"
)

func parseFixture(t *testing.T) *Document {
	t.Helper()
	doc, err := ParseFile("testdata/nested_scenario.html")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(doc.Levels) != 2 {
		t.Fatalf("levels = %d, want 2", len(doc.Levels))
	}
	return doc
}

func TestParseNestedTaskIsNotTruncated(t *testing.T) {
	lvl := parseFixture(t).Levels[0]
	if len(lvl.Tasks) != 1 {
		t.Fatalf("tasks = %d, want 1", len(lvl.Tasks))
	}
	task := lvl.Tasks[0]
	for _, want := range []string{
		"Заголовок задания",
		"Первый абзац задания.",
		"Нажми, чтобы увидеть карту",
		"Последний абзац задания — он терялся до исправления.",
	} {
		if !strings.Contains(task, want) {
			t.Errorf("task is missing %q\ngot: %s", want, task)
		}
	}
}

func TestParseNestedCommentAndHintAreNotTruncated(t *testing.T) {
	lvl := parseFixture(t).Levels[0]
	if !strings.Contains(lvl.Comment, "вложенным span") || !strings.Contains(lvl.Comment, "внутри") {
		t.Errorf("comment truncated: %q", lvl.Comment)
	}
	if len(lvl.Hints) != 1 {
		t.Fatalf("hints = %d, want 1", len(lvl.Hints))
	}
	hint := lvl.Hints[0]
	if !strings.Contains(hint.Text, "текст обычной подсказки") {
		t.Errorf("hint truncated: %q", hint.Text)
	}
	if hint.DelaySeconds != 15*60 {
		t.Errorf("hint delay = %d, want %d", hint.DelaySeconds, 15*60)
	}
}

func TestParsePenaltyHints(t *testing.T) {
	lvl := parseFixture(t).Levels[0]
	if len(lvl.PenaltyHints) != 2 {
		t.Fatalf("penalty hints = %d, want 2", len(lvl.PenaltyHints))
	}
	first := lvl.PenaltyHints[0]
	if first.Text != "D45R54" {
		t.Errorf("text = %q, want D45R54", first.Text)
	}
	if first.DelaySeconds != 30*60 {
		t.Errorf("delay = %d, want %d", first.DelaySeconds, 30*60)
	}
	if first.PenaltySeconds != 15*60 {
		t.Errorf("penalty = %d, want %d", first.PenaltySeconds, 15*60)
	}
	if !first.RequestConfirm {
		t.Error("request confirm = false, want true")
	}
	if first.Comment != "Код 1" {
		t.Errorf("comment = %q, want %q", first.Comment, "Код 1")
	}

	second := lvl.PenaltyHints[1]
	if second.PenaltySeconds != 3600 {
		t.Errorf("penalty = %d, want 3600", second.PenaltySeconds)
	}
	if second.RequestConfirm {
		t.Error("request confirm = true, want false")
	}

	// Penalty hints must not leak into the regular hint list.
	for _, h := range lvl.Hints {
		if strings.HasPrefix(h.Title, "Штрафная") {
			t.Errorf("penalty hint %q leaked into Hints", h.Title)
		}
	}
}

func TestParsePreservesVerbatimNames(t *testing.T) {
	lvl := parseFixture(t).Levels[0]
	if lvl.Name != "Первый уровень  " {
		t.Errorf("level name = %q, want %q", lvl.Name, "Первый уровень  ")
	}
	if len(lvl.Sectors) != 2 {
		t.Fatalf("sectors = %d, want 2", len(lvl.Sectors))
	}
	if lvl.Sectors[0].Name != "Замена трешки 2    (9 8 10 12 11)" {
		t.Errorf("sector name = %q, spaces were collapsed", lvl.Sectors[0].Name)
	}
}

func TestParseNestedAnswersAndBonuses(t *testing.T) {
	lvl := parseFixture(t).Levels[0]
	if got := lvl.Sectors[0].Answers; len(got) != 1 || got[0] != "Локация_снята_425235" {
		t.Errorf("sector answers = %v, want [Локация_снята_425235]", got)
	}
	if lvl.RequiredSectorsCount != 2 {
		t.Errorf("required sectors = %d, want 2", lvl.RequiredSectorsCount)
	}
	if len(lvl.Bonuses) != 1 {
		t.Fatalf("bonuses = %d, want 1", len(lvl.Bonuses))
	}
	bonus := lvl.Bonuses[0]
	if !strings.Contains(bonus.Task, "и хвостом") {
		t.Errorf("bonus task truncated: %q", bonus.Task)
	}
	if got := bonus.Answers; len(got) != 1 || got[0] != "бонусответ" {
		t.Errorf("bonus answers = %v, want [бонусответ]", got)
	}
}

func TestParseUnclosedMarkupKeepsTask(t *testing.T) {
	lvl := parseFixture(t).Levels[1]
	if len(lvl.Tasks) != 1 {
		t.Fatalf("tasks = %d, want 1", len(lvl.Tasks))
	}
	if !strings.Contains(lvl.Tasks[0], "до самого конца") {
		t.Errorf("task dropped on unclosed markup: %q", lvl.Tasks[0])
	}
}

// TestScenarioTextIsLossless asserts that every visible line inside a level
// block of the export is present in the parsed model, so that re-importing the
// document reproduces the original scenario. Set ENCX_SCENARIO_FILE to run the
// same check against a real GameScenario.aspx export.
func TestScenarioTextIsLossless(t *testing.T) {
	path := os.Getenv("ENCX_SCENARIO_FILE")
	if path == "" {
		path = "testdata/nested_scenario.html"
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	doc, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if missing := missingScenarioText(string(raw), doc); len(missing) > 0 {
		t.Errorf("%d visible line(s) lost during parsing:", len(missing))
		for i, line := range missing {
			if i == 20 {
				t.Errorf("  ... and %d more", len(missing)-i)
				break
			}
			t.Errorf("  %s", line)
		}
	}
}
