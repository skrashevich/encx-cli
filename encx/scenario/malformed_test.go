package scenario

import (
	"strings"
	"testing"
)

const levelAnchor = `<a id="LevelsScenarioRepeater_ctl00_lnkLevelAnchorPoint" name="1"></a>`

func parseSingleLevel(t *testing.T, body string) Level {
	t.Helper()
	doc, err := ParseString(levelAnchor+`<span>Уровень №1 "Тест"</span>`+body, "inline")
	if err != nil {
		t.Fatalf("ParseString: %v", err)
	}
	if len(doc.Levels) != 1 {
		t.Fatalf("levels = %d, want 1", len(doc.Levels))
	}
	return doc.Levels[0]
}

// An unclosed tag inside a penalty hint title used to make the title element
// swallow its own body, producing a reversed slice range and a panic.
func TestPenaltyHintTitleWithUnclosedTagDoesNotPanic(t *testing.T) {
	lvl := parseSingleLevel(t, `<div>
<span id="LevelsScenarioRepeater_ctl00_LevelPenaltyHelpsRepeater_ctl00_lblLevelHelpTitle">Штрафная <b>подсказка №1 для всех (30 минут)</span><br/>
<span class="green">Штрафное время:</span> <span class="red">15 минут</span><br/>
<span id="LevelsScenarioRepeater_ctl00_LevelPenaltyHelpsRepeater_ctl00_lblLevelHelp">D45R54</span>
</div>`)
	for _, h := range lvl.PenaltyHints {
		if h.Text == "" {
			t.Errorf("penalty hint parsed with empty text: %+v", h)
		}
	}
}

// The task span is a direct child of the level block in some exports, so an
// unclosed tag must not let the task absorb the level's hints and answers.
func TestUnclosedTaskDoesNotAbsorbNeighbouringFields(t *testing.T) {
	lvl := parseSingleLevel(t, `<div class="scenarioBlock">
<span id="LevelsScenarioRepeater_ctl00_LevelTasksRepeater_ctl00_lblLevelTask">Задание <span style="color:red">незакрытое
<span id="LevelsScenarioRepeater_ctl00_LevelHelpsRepeater_ctl00_lblLevelHelpTitle">Подсказка №1 для всех (10 минут)</span><br/>
<span id="LevelsScenarioRepeater_ctl00_LevelHelpsRepeater_ctl00_lblLevelHelp">СЕКРЕТ</span>
<span id="LevelsScenarioRepeater_ctl00_SectorsRepeater_ctl00_LevelAnswersRepeater_ctl00_lblLevelAnswer">ОТВЕТ</span> - <span id="LevelsScenarioRepeater_ctl00_SectorsRepeater_ctl00_LevelAnswersRepeater_ctl00_lblAnswerFor">для всех</span>
</div>`)
	if len(lvl.Tasks) != 1 {
		t.Fatalf("tasks = %d, want 1", len(lvl.Tasks))
	}
	if !strings.Contains(lvl.Tasks[0], "незакрытое") {
		t.Errorf("task content lost: %q", lvl.Tasks[0])
	}
	for _, leak := range []string{"СЕКРЕТ", "ОТВЕТ", "Подсказка №1"} {
		if strings.Contains(lvl.Tasks[0], leak) {
			t.Errorf("task absorbed %q from a neighbouring field:\n%s", leak, lvl.Tasks[0])
		}
	}
	if len(lvl.Hints) != 1 || lvl.Hints[0].Text != "СЕКРЕТ" {
		t.Errorf("hints = %+v, want the hint to stay its own field", lvl.Hints)
	}
	if len(lvl.Sectors) != 1 || len(lvl.Sectors[0].Answers) != 1 {
		t.Errorf("sectors = %+v, want the answer to stay its own field", lvl.Sectors)
	}
}

// A stray </div> in otherwise well-formed content must not truncate the field:
// the element's own closing tag comes first and wins.
func TestStrayClosingDivDoesNotTruncateClosedTask(t *testing.T) {
	lvl := parseSingleLevel(t, `<div>
<span id="LevelsScenarioRepeater_ctl00_LevelTasksRepeater_ctl00_lblLevelTask">НАЧАЛО </div> ХВОСТ</span>
</div>`)
	if len(lvl.Tasks) != 1 {
		t.Fatalf("tasks = %d, want 1", len(lvl.Tasks))
	}
	if !strings.Contains(lvl.Tasks[0], "ХВОСТ") {
		t.Errorf("task truncated at a stray </div>: %q", lvl.Tasks[0])
	}
}

// An unclosed element inside a non-div container is bounded by the next
// repeater field instead of being dropped.
func TestUnclosedTaskInsideTableIsKept(t *testing.T) {
	lvl := parseSingleLevel(t, `<table><tr><td>
<span id="LevelsScenarioRepeater_ctl00_LevelTasksRepeater_ctl00_lblLevelTask">Задание <span>незакрытое до конца
</td></tr></table>`)
	if len(lvl.Tasks) != 1 {
		t.Fatalf("tasks = %d, want 1", len(lvl.Tasks))
	}
	if !strings.Contains(lvl.Tasks[0], "незакрытое до конца") {
		t.Errorf("task dropped: %q", lvl.Tasks[0])
	}
}

// A repeater id nested inside well-formed content is not a field boundary —
// the element's own closing tag wins.
func TestNestedRepeaterIDDoesNotTruncateClosedField(t *testing.T) {
	lvl := parseSingleLevel(t, `<div>
<span id="LevelsScenarioRepeater_ctl00_lblLevelComment">НАЧАЛО <span id="LevelsScenarioRepeater_ctl00_inner">внутри</span> КОНЕЦ</span>
<span id="LevelsScenarioRepeater_ctl00_LevelTasksRepeater_ctl00_lblLevelTask">СМОТРИ <img src="x.png" id="LevelsScenarioRepeater_pic"/> И ЕЩЁ ТЕКСТ</span>
<span id="LevelsScenarioRepeater_ctl00_LevelHelpsRepeater_ctl00_lblLevelHelpTitle">Подсказка №1 для всех (10 минут)</span><br/>
<span id="LevelsScenarioRepeater_ctl00_LevelHelpsRepeater_ctl00_lblLevelHelp">НАЧАЛО<div id="LevelsScenarioRepeater_ctl00_x">СЕРЕДИНА</div>КОНЕЦ</span>
</div>`)
	if !strings.Contains(lvl.Comment, "КОНЕЦ") {
		t.Errorf("comment truncated at a nested repeater id: %q", lvl.Comment)
	}
	if len(lvl.Tasks) != 1 || !strings.Contains(lvl.Tasks[0], "И ЕЩЁ ТЕКСТ") {
		t.Errorf("task truncated at a nested repeater id: %q", lvl.Tasks)
	}
	if len(lvl.Hints) != 1 || !strings.Contains(lvl.Hints[0].Text, "КОНЕЦ") {
		t.Errorf("hint truncated at a nested repeater id: %+v", lvl.Hints)
	}
}

// Repeater indices repeat across levels, so pairing must use the outer index
// too — otherwise a title pairs with another level's body.
func TestHintPairingUsesOuterRepeaterIndex(t *testing.T) {
	raw := levelAnchor + `<span>Уровень №1 "Первый"</span><div>
<span id="LevelsScenarioRepeater_ctl00_LevelHelpsRepeater_ctl00_lblLevelHelpTitle">Подсказка №1 для всех (10 минут)</span><br/>
<span id="LevelsScenarioRepeater_ctl00_LevelHelpsRepeater_ctl00_lblLevelHelp">ТЕКСТ-УРОВНЯ-1</span>
</div>
<span>Уровень №2 "Второй"</span><div>
<span id="LevelsScenarioRepeater_ctl01_LevelHelpsRepeater_ctl00_lblLevelHelpTitle">Подсказка №1 для всех (20 минут)</span><br/>
<span id="LevelsScenarioRepeater_ctl01_LevelHelpsRepeater_ctl00_lblLevelHelp">ТЕКСТ-УРОВНЯ-2</span>
</div>`
	doc, err := ParseString(raw, "inline")
	if err != nil {
		t.Fatalf("ParseString: %v", err)
	}
	// Both outer indices live in a single anchor-delimited block here.
	hints := doc.Levels[0].Hints
	if len(hints) != 2 {
		t.Fatalf("hints = %d, want 2", len(hints))
	}
	if hints[0].Text != "ТЕКСТ-УРОВНЯ-1" || hints[1].Text != "ТЕКСТ-УРОВНЯ-2" {
		t.Errorf("cross-paired hints: %+v", hints)
	}
}
