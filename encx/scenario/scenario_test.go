package scenario

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseFile(t *testing.T) {
	dir := t.TempDir()
	assetsDir := filepath.Join(dir, "scenario_files")
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		t.Fatalf("mkdir assets: %v", err)
	}
	assetPath := filepath.Join(assetsDir, "pic.png")
	if err := os.WriteFile(assetPath, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, 0o644); err != nil {
		t.Fatalf("write asset: %v", err)
	}

	htmlDoc := `
<a id="LevelsScenarioRepeater_ctl00_lnkLevelAnchorPoint" name="1"></a>
Уровень №1 "Бриф"
<div class="scenarioBlock border_dark">
  <div class="white bold">Автопереход: через 3 минуты<br><br></div>
  <span id="LevelsScenarioRepeater_ctl00_LevelTasksRepeater_ctl00_lblLevelTask">Текст<br><img src="./scenario_files/pic.png"></span>
  <span id="LevelsScenarioRepeater_ctl00_LevelHelpsRepeater_ctl00_lblLevelHelpTitle">Подсказка №1 для всех (1 час 5 минут)</span><br>
  <span id="LevelsScenarioRepeater_ctl00_LevelHelpsRepeater_ctl00_lblLevelHelp">Текст подсказки</span>
  <span id="LevelsScenarioRepeater_ctl00_SectorsRepeater_ctl00_LevelAnswersRepeater_ctl00_lblLevelAnswer"><span class="nonLatinChar">код1</span>66</span> - <span id="LevelsScenarioRepeater_ctl00_SectorsRepeater_ctl00_LevelAnswersRepeater_ctl00_lblAnswerFor">для всех</span>
  <span id="LevelsScenarioRepeater_ctl00_SectorsRepeater_ctl00_LevelAnswersRepeater_ctl01_lblLevelAnswer">код2</span> - <span id="LevelsScenarioRepeater_ctl00_SectorsRepeater_ctl00_LevelAnswersRepeater_ctl01_lblAnswerFor">для всех</span>
</div>
<a id="LevelsScenarioRepeater_ctl01_lnkLevelAnchorPoint" name="2"></a>
Уровень №2 "Финиш"
<div class="scenarioBlock border_dark">
  <span id="LevelsScenarioRepeater_ctl01_LevelTasksRepeater_ctl00_lblLevelTask">Второй</span>
  <span id="LevelsScenarioRepeater_ctl01_SectorsRepeater_ctl00_LevelAnswersRepeater_ctl00_lblLevelAnswer">answer</span>
</div>
`
	docPath := filepath.Join(dir, "game scenario.html")
	if err := os.WriteFile(docPath, []byte(htmlDoc), 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}

	scenarioDoc, err := ParseFile(docPath)
	if err != nil {
		t.Fatalf("ParseFile error: %v", err)
	}
	if len(scenarioDoc.Levels) != 2 {
		t.Fatalf("expected 2 levels, got %d", len(scenarioDoc.Levels))
	}

	l1 := scenarioDoc.Levels[0]
	if l1.Number != 1 || l1.Name != "Бриф" {
		t.Fatalf("unexpected level #1 metadata: %+v", l1)
	}
	if l1.AutopassSecond != 180 {
		t.Fatalf("expected autopass 180s, got %d", l1.AutopassSecond)
	}
	if len(l1.Hints) != 1 || l1.Hints[0].DelaySeconds != 3900 {
		t.Fatalf("unexpected hint parse: %+v", l1.Hints)
	}
	if len(l1.SectorAnswers) != 1 || len(l1.SectorAnswers[0]) != 2 {
		t.Fatalf("unexpected sector answers: %+v", l1.SectorAnswers)
	}
	if l1.SectorAnswers[0][0] != "код166" {
		t.Fatalf("expected first code to preserve suffix, got %q", l1.SectorAnswers[0][0])
	}
	if scenarioDoc.EmbeddedAssets != 1 {
		t.Fatalf("expected embedded asset count 1, got %d", scenarioDoc.EmbeddedAssets)
	}
	if len(l1.Tasks) == 0 || !strings.Contains(l1.Tasks[0], "data:image/png;base64,") {
		t.Fatalf("expected embedded data URL in task, got: %q", strings.Join(l1.Tasks, "\n"))
	}
}

func TestParseRuDuration(t *testing.T) {
	tests := map[string]int{
		"3 минуты":              180,
		"1 час 5 минут":         3900,
		"2 часа 45 минут":       9900,
		"1 день 1 час 1 минута": 90060,
	}
	for input, expected := range tests {
		got := ParseRuDuration(input)
		if got != expected {
			t.Fatalf("ParseRuDuration(%q) = %d, expected %d", input, got, expected)
		}
	}
}

func TestMatchAnswer(t *testing.T) {
	accepted := []string{"поехалистрадать66"}
	if !MatchAnswer("поехалистрадать66", accepted) {
		t.Fatal("expected exact match")
	}
	if !MatchAnswer("ПОЕХАЛИСТРАДАТЬ66", accepted) {
		t.Fatal("expected case-insensitive match")
	}
	if MatchAnswer("wrong", accepted) {
		t.Fatal("expected mismatch")
	}
}

func TestMatchAnswerCaseInsensitive(t *testing.T) {
	cases := []struct {
		submitted string
		accepted  []string
	}{
		{"тест", []string{"ТЕСТ"}},
		{"ТЕСТ", []string{"тест"}},
		{"КРАБАТ", []string{"Крабат"}},
		{"ARCTIINAE", []string{"Arctiinae"}},
		{"УЖИН В ЭММАУСЕ", []string{"Ужин в Эммаусе"}},
		{"answer42", []string{"Answer42"}},
		{"КОН3", []string{"кон3"}},
	}
	for _, tc := range cases {
		if !MatchAnswer(tc.submitted, tc.accepted) {
			t.Fatalf("MatchAnswer(%q, %v) should match", tc.submitted, tc.accepted)
		}
	}
}

func TestParseBonuses(t *testing.T) {
	htmlDoc := `
<a id="LevelsScenarioRepeater_ctl00_lnkLevelAnchorPoint" name="3"></a>
Уровень №3 "Eyes"
<div class="scenarioBlock border_dark">
  <span id="LevelsScenarioRepeater_ctl00_LevelBonusesRepeater_ctl00_lblBonusNum" class="light_yellow bold">Бонус №19 "глаз 19 " для всех</span>
  <br><br>
  <span class="green">Бонусное время:  3 минуты</span>
  <br><br>
  <span class="green">Задание</span><br>
  <span class="white"></span>
  <br><br>
  <span class="green">Ответы</span><br>
  <span id="LevelsScenarioRepeater_ctl00_LevelBonusesRepeater_ctl00_BonusAnswersRepeater_ctl00_lblBonusAnswer" class="bold w00FF00x10"><span class="nonLatinChar">кон</span>3</span>
  <br><br><br>
  <span id="LevelsScenarioRepeater_ctl00_LevelBonusesRepeater_ctl04_lblBonusNum" class="light_yellow bold">Бонус №21 для всех</span>
  <br><br>
  <span class="green">Бонусное время:  10 секунд</span>
  <br><br />
  <span class="green">Задание</span><br />
  <span class="white"></span>
  <br/><br/>
  <span class="green">Ответы</span><br/>
  <span id="LevelsScenarioRepeater_ctl00_LevelBonusesRepeater_ctl04_BonusAnswersRepeater_ctl00_lblBonusAnswer" class="bold w00FF00x10">solo</span>
  <br><br><br>
  <span id="LevelsScenarioRepeater_ctl00_LevelBonusesRepeater_ctl06_lblBonusNum" class="light_yellow bold">Бонус №22 "&quot;Углы&quot;" для всех</span>
  <br><br>
  <span class="green">Бонусное время:  7 минут</span>
  <br/><br />
  <span class="green">Задание</span><br />
  <span class="white"><div>task<br />line</div></span>
  <br/><br/>
  <span class="green">Подсказка</span><br />
  <span class="white"><script>show("hint")</script></span>
  <br/><br/>
  <span class="green">Ответы</span><br/>
  <span id="LevelsScenarioRepeater_ctl00_LevelBonusesRepeater_ctl06_BonusAnswersRepeater_ctl00_lblBonusAnswer" class="bold w00FF00x10">combo answer</span>
  <br><br><br>
  <span id="LevelsScenarioRepeater_ctl00_LevelBonusesRepeater_ctl02_lblBonusNum" class="light_yellow bold">Бонус №20 "глаз 20" для всех</span>
  <br><br>
  <span class="green">Бонусное время:  3 минуты</span>
  <br><br>
  <span class="green">Задание</span><br>
  <span class="white"></span>
  <br><br>
  <span class="green">Ответы</span><br>
  <span id="LevelsScenarioRepeater_ctl00_LevelBonusesRepeater_ctl02_BonusAnswersRepeater_ctl00_lblBonusAnswer" class="bold w00FF00x10">рим8</span>
</div>
`
	levels, err := parseLevels(htmlDoc, &assetRewriteState{cache: make(map[string]string), missingSet: make(map[string]struct{})})
	if err != nil {
		t.Fatalf("parseLevels: %v", err)
	}
	if len(levels) != 1 {
		t.Fatalf("expected 1 level, got %d", len(levels))
	}
	bonuses := levels[0].Bonuses
	if len(bonuses) != 4 {
		t.Fatalf("expected 4 bonuses, got %d", len(bonuses))
	}
	if bonuses[0].Number != 19 || bonuses[0].Name != "глаз 19" || bonuses[0].AwardSeconds != 180 {
		t.Fatalf("bonus 19: %+v", bonuses[0])
	}
	if len(bonuses[0].Answers) != 1 || bonuses[0].Answers[0] != "кон3" {
		t.Fatalf("bonus 19 answers: %+v", bonuses[0].Answers)
	}
	if bonuses[1].Number != 20 || bonuses[1].Answers[0] != "рим8" {
		t.Fatalf("bonus 20: %+v", bonuses[1])
	}
	if bonuses[2].Number != 21 || bonuses[2].Name != "" || bonuses[2].AwardSeconds != 10 || bonuses[2].Answers[0] != "solo" {
		t.Fatalf("bonus 21: %+v", bonuses[2])
	}
	if bonuses[3].Number != 22 || bonuses[3].Name != `"Углы"` || bonuses[3].AwardSeconds != 420 {
		t.Fatalf("bonus 22 metadata: %+v", bonuses[3])
	}
	if !strings.Contains(bonuses[3].Task, "task<br />line") || !strings.Contains(bonuses[3].Hint, `show("hint")`) {
		t.Fatalf("bonus 22 text: %+v", bonuses[3])
	}
}
