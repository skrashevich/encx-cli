package encx

import (
	"context"
	"fmt"
	"github.com/skrashevich/encx-cli/encx/enapi"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const scenarioExportFixture = `{
  "game": {"id": 82448, "game_num": 12, "title": "Тестовая игра"},
  "is_classic_game": true,
  "levels": [
    {
      "level_id": 811, "level_number": 1, "level_name": "Первый",
      "comment": "комментарий",
      "autopass_text": "через 1 час 30 минут, 5 минут штрафа",
      "sectors_completion_rule": "для прохождения задания необходимо выполнить 2 сектора",
      "tasks": [{"task_id": 1, "task_text": "Задание уровня"}, {"task_id": 2, "task_text": "   "}],
      "helps": [{"help_id": 10, "title": "Подсказка 1", "help_text": "Текст", "timeout": 600}],
      "penalty_helps": [{"help_id": 20, "title": "Штрафная 1", "help_text": "Штраф",
                         "timeout": 0, "is_penalty": true, "penalty_time": 300,
                         "penalty_comment": "минус 5 минут", "request_penalty_confirm": true}],
      "sectors": [
        {"sector_id": 1, "sector_name": "Сектор A", "display_name": "Сектор A",
         "answers": [{"answer_id": 1, "answer_text": "a1"}, {"answer_id": 2, "answer_text": "a2"}]},
        {"sector_id": 2, "sector_name": "", "display_name": "Сектор B",
         "answers": [{"answer_id": 3, "answer_text": "b1"}, {"answer_id": 4, "answer_text": "  "}]}
      ],
      "bonuses": [
        {"bonus_id": 5, "bonus_name": "Бонус", "task": "Найдите", "bonus_help": "Хинт",
         "answers": ["x", "y"], "bonus_time": 120}
      ]
    },
    {"level_id": 812, "level_number": 2, "level_name": "Второй",
     "sectors_completion_rule": "необходимо выполнить все секторы"}
  ]
}`

func TestNewEngineGameScenarioMapsToDocument(t *testing.T) {
	var path, query string
	c := newEngineClient(t, func(w http.ResponseWriter, r *http.Request) {
		path, query = r.URL.Path, r.URL.RawQuery
		_, _ = w.Write([]byte(scenarioExportFixture))
	})

	doc, err := c.GetGameScenario(context.Background(), 82448)
	if err != nil {
		t.Fatalf("GetGameScenario: %v", err)
	}
	if path != "/games/82448/scenario" {
		t.Errorf("path = %q", path)
	}
	if !containsQueryPair(query, "lang", "ru") {
		t.Errorf("query = %q", query)
	}

	if doc.GameID != 82448 || doc.GameNum != 12 || doc.GameTitle != "Тестовая игра" {
		t.Errorf("document header = %+v", doc)
	}
	if len(doc.Levels) != 2 {
		t.Fatalf("levels = %d, want 2", len(doc.Levels))
	}

	level := doc.Levels[0]
	if level.Number != 1 || level.Name != "Первый" || level.Comment != "комментарий" {
		t.Errorf("level = %+v", level)
	}
	if level.AutopassSecond != 5400 || level.AutopassPenaltySecond != 300 {
		t.Errorf("autopass = %d/%d, want 5400/300", level.AutopassSecond, level.AutopassPenaltySecond)
	}
	if level.RequiredSectorsCount != 2 {
		t.Errorf("RequiredSectorsCount = %d, want 2", level.RequiredSectorsCount)
	}
	if len(level.Tasks) != 1 || level.Tasks[0] != "Задание уровня" {
		t.Errorf("tasks = %+v, want the blank one dropped", level.Tasks)
	}

	if len(level.Hints) != 1 || level.Hints[0].Text != "Текст" || level.Hints[0].DelaySeconds != 600 {
		t.Errorf("hints = %+v", level.Hints)
	}
	if len(level.PenaltyHints) != 1 {
		t.Fatalf("penalty hints = %+v", level.PenaltyHints)
	}
	penalty := level.PenaltyHints[0]
	if penalty.Text != "Штраф" || penalty.PenaltySeconds != 300 ||
		!penalty.RequestConfirm || penalty.Comment != "минус 5 минут" {
		t.Errorf("penalty hint = %+v", penalty)
	}

	if len(level.Sectors) != 2 || len(level.SectorAnswers) != 2 {
		t.Fatalf("sectors = %d, answers = %d", len(level.Sectors), len(level.SectorAnswers))
	}
	if level.Sectors[0].Name != "Сектор A" || level.Sectors[1].Name != "Сектор B" {
		t.Errorf("sector names = %+v", level.Sectors)
	}
	if len(level.SectorAnswers[0]) != 2 || level.SectorAnswers[0][0] != "a1" {
		t.Errorf("sector 1 answers = %v", level.SectorAnswers[0])
	}
	if len(level.SectorAnswers[1]) != 1 || level.SectorAnswers[1][0] != "b1" {
		t.Errorf("sector 2 answers = %v, want the blank one dropped", level.SectorAnswers[1])
	}

	if len(level.Bonuses) != 1 {
		t.Fatalf("bonuses = %+v", level.Bonuses)
	}
	bonus := level.Bonuses[0]
	if bonus.Name != "Бонус" || bonus.Task != "Найдите" || bonus.Hint != "Хинт" ||
		bonus.AwardSeconds != 120 || len(bonus.Answers) != 2 {
		t.Errorf("bonus = %+v", bonus)
	}

	// "все секторы" is recorded as zero, exactly as the HTML parser does.
	if doc.Levels[1].RequiredSectorsCount != 0 {
		t.Errorf("level 2 RequiredSectorsCount = %d, want 0", doc.Levels[1].RequiredSectorsCount)
	}
}

func TestNewEngineGameScenarioReportsEngineError(t *testing.T) {
	c := newEngineClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"error":{"type":"forbidden","key":"ScenarioUnavailable",
		  "message":"Сценарий недоступен"}}`))
	})

	_, err := c.GetGameScenario(context.Background(), 82448)
	if err == nil {
		t.Fatal("GetGameScenario returned an empty document instead of the engine's refusal")
	}
	if !strings.Contains(err.Error(), "Сценарий недоступен") {
		t.Errorf("error = %v", err)
	}
}

// TestNewEngineGameScenarioHTMLIsRefused pins that the HTML-only accessor says
// so rather than handing back JSON an HTML parser would choke on.
func TestNewEngineGameScenarioHTMLIsRefused(t *testing.T) {
	c := newEngineClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("no request should be sent")
	})

	_, err := c.GetGameScenarioHTML(context.Background(), 82448)
	if err == nil {
		t.Fatal("GetGameScenarioHTML answered on the new engine")
	}
	if !strings.Contains(err.Error(), "GetGameScenario") {
		t.Errorf("error = %v, want it to name the replacement", err)
	}
}

func TestLegacyGameScenarioParsesTheExportPage(t *testing.T) {
	t.Setenv(EngineEnvVar, "legacy")
	const page = `<html><body>
	  <a id="LevelsScenarioRepeater_ctl00_lnkLevelAnchorPoint" name="1"></a>
	  Уровень №1 "Первый"
	  <span id="LevelsScenarioRepeater_ctl00_LevelTasksRepeater_ctl00_lblLevelTask">Задание</span>
	</body></html>`

	var path string
	srv := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_, _ = w.Write([]byte(page))
	}))
	srv.Start()

	c := New(strings.TrimPrefix(srv.URL, "http://"), WithHTTP(), WithAdminDelay(0))
	doc, err := c.GetGameScenario(context.Background(), 82448)
	if err != nil {
		t.Fatalf("GetGameScenario: %v", err)
	}
	if path != "/GameScenario.aspx" {
		t.Errorf("path = %q, want the ASP export page", path)
	}
	if len(doc.Levels) != 1 || doc.Levels[0].Number != 1 || doc.Levels[0].Name != "Первый" {
		t.Errorf("levels = %+v", doc.Levels)
	}
	if len(doc.Levels[0].Tasks) != 1 || doc.Levels[0].Tasks[0] != "Задание" {
		t.Errorf("tasks = %+v", doc.Levels[0].Tasks)
	}

	// The legacy engine still hands back the raw page when it is asked for.
	body, err := c.GetGameScenarioHTML(context.Background(), 82448)
	if err != nil {
		t.Fatalf("GetGameScenarioHTML: %v", err)
	}
	if !strings.Contains(body, "lnkLevelAnchorPoint") {
		t.Errorf("HTML export = %q", body)
	}
}

func TestScenarioExportPreservesPenaltyBonus(t *testing.T) {
	for _, bonus := range []enapi.BonusScenario{{BonusTime: -120}, {BonusTime: 120, BonusTimeText: "Штрафное время: 2 минуты"}} {
		got := scenarioLevelFromAPI(enapi.LevelScenario{Bonuses: []enapi.BonusScenario{bonus}}).Bonuses[0]
		if !got.Negative || got.AwardSeconds != 120 {
			t.Fatalf("penalty bonus misread: %+v", got)
		}
	}
}

func TestScenarioExportDoesNotUseGeneratedBonusTitleAsName(t *testing.T) {
	c := newEngineClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"game":{"id":32055},"levels":[{"level_number":15,"bonuses":[{"title":"Бонус №1 для всех","bonus_time":1},{"bonus_name":"Named","title":"Бонус №2 Named для всех","bonus_time":1}]}]}`))
	})
	doc, err := c.GetGameScenario(t.Context(), 32055)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Levels[0].Bonuses[0].Name != "" {
		t.Fatalf("generated title became authored name: %q", doc.Levels[0].Bonuses[0].Name)
	}
	if doc.Levels[0].Bonuses[1].Name != "Named" {
		t.Fatal("lost authored bonus name")
	}
}

func TestAdminScenarioReadsBonusSignByID(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(fmt.Sprint(missing), func(t *testing.T) {
			c := newEngineClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/games/82448/scenario":
					_, _ = w.Write([]byte(scenarioExportFixture))
				case "/admin/games/82448/levels/811/editor":
					if missing {
						_, _ = w.Write([]byte(`{"bonuses":[]}`))
					} else {
						_, _ = w.Write([]byte(`{"bonuses":[{"bonus_id":999,"negative":false},{"bonus_id":5,"negative":true}]}`))
					}
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
					http.NotFound(w, r)
				}
			})
			doc, err := c.GetAdminGameScenario(t.Context(), 82448)
			if missing {
				if err == nil {
					t.Fatal("missing administrative bonus was accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !doc.Levels[0].Bonuses[0].Negative || doc.Levels[0].Bonuses[0].AwardSeconds != 120 {
				t.Fatalf("lost bonus sign or duration: %+v", doc.Levels[0].Bonuses[0])
			}
		})
	}
}
