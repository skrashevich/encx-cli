package encx

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// adminCall records one REST call the new admin backend made.
type adminCall struct {
	method string
	path   string
	body   string
}

const adminLevelsFixture = `{
  "game_id": 82448, "game_num": 12, "title": "Игра", "started": false,
  "can_manipulate_levels": true,
  "levels": [
    {"level_id": 811, "level_number": 1, "level_name": "Первый", "comment": "комментарий 1"},
    {"level_id": 812, "level_number": 2, "level_name": "Второй", "comment": "комментарий 2"},
    {"level_id": 813, "level_number": 3, "level_name": "Третий", "comment": ""}
  ]
}`

const adminEditorFixture = `{
  "game_id": 82448,
  "level": {"level_id": 812, "level_number": 2, "level_name": "Второй", "comment": "комментарий 2"},
  "timeout_sec": 3720, "timeout_time_award_sec": 300,
  "attempts_number": 5, "attempts_period_sec": 90, "block_type_id": 1,
  "passing_condition_id": 1, "required_sectors_count": 2,
  "tasks": [
    {"task_id": 9001, "level_id": 812, "task_text": "Задание", "replace_nl_to_br": true,
     "for_user_id": 0, "for_team_id": 0}
  ],
  "helps": [
    {"help_id": 7701, "level_id": 812, "help_number": 1, "help_text": "Подсказка",
     "timeout": 90061, "is_penalty": false, "for_user_id": 0, "for_team_id": 0}
  ],
  "penalty_helps": [
    {"help_id": 7801, "level_id": 812, "help_number": 1, "help_text": "Штрафная",
     "timeout": 0, "is_penalty": true, "penalty_time": 3661,
     "penalty_comment": "минус", "request_penalty_confirm": true,
     "for_user_id": 501, "for_team_id": 0}
  ],
  "bonuses": [
    {"bonus_id": 9101, "bonus_name": "Бонус", "task": "Найдите", "bonus_help": "Хинт",
     "answers": ["ответ1", "ответ2"], "bonus_time": 3661, "negative": true,
     "level_ids": [812], "has_absolute_limit": true,
     "valid_from": "01.09.2026 10:00:00", "valid_to": "01.09.2026 12:00:00",
     "has_delay": true, "delay_sec": 120, "has_relative_limit": true, "life_time_sec": 1800,
     "user_id": 0, "team_id": 77}
  ],
  "sectors": [
    {"sector_id": 5501, "level_id": 812, "sector_name": "Сектор A"},
    {"sector_id": 5502, "level_id": 812, "sector_name": "Сектор B"}
  ],
  "answers": [
    {"answer_id": 1, "sector_id": 5501, "answer_text": "a1"},
    {"answer_id": 2, "sector_id": 5501, "answer_text": "a2"},
    {"answer_id": 3, "sector_id": 5502, "answer_text": "b1"}
  ],
  "messages": [
    {"message_id": 3301, "message_text": "Сообщение", "replace_nl_to_br": true,
     "all_levels": false, "level_ids": [812], "required_points": 5}
  ]
}`

func newAdminClient(t *testing.T, calls *[]adminCall) *Client {
	t.Helper()
	return newEngineClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		*calls = append(*calls, adminCall{method: r.Method, path: r.URL.Path, body: string(body)})
		switch {
		case r.URL.Path == "/admin/games/82448/levels" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(adminLevelsFixture))
		case strings.HasSuffix(r.URL.Path, "/editor"):
			_, _ = w.Write([]byte(adminEditorFixture))
		case r.URL.Path == "/admin/games":
			// Shape taken from a live GET /admin/games: rows are keyed by
			// game_id and carry no started/finished flags.
			_, _ = w.Write([]byte(`{"items":[{"game_id":82448,"game_num":12,"title":"Игра",` +
				`"start_date_time":"2017-02-25T12:12:00Z","finish_date_time":"2099-03-05T12:00:00Z",` +
				`"owner_id":501,"owner_login":"svk"}],"total_count":1,"page":1}`))
		case strings.HasSuffix(r.URL.Path, "/sectors") && r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"sector_id":5599,"level_id":812,"sector_name":"Новый"}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	})
}

func adminCallPaths(calls []adminCall) []string {
	paths := make([]string, 0, len(calls))
	for _, call := range calls {
		paths = append(paths, call.method+" "+call.path)
	}
	return paths
}

func lastCall(t *testing.T, calls []adminCall) adminCall {
	t.Helper()
	if len(calls) == 0 {
		t.Fatal("no calls were made")
	}
	return calls[len(calls)-1]
}

func TestNewEngineAdminGetGames(t *testing.T) {
	var calls []adminCall
	c := newAdminClient(t, &calls)

	games, err := c.AdminGetGames(context.Background())
	if err != nil {
		t.Fatalf("AdminGetGames: %v", err)
	}
	if calls[0].path != "/admin/games" {
		t.Errorf("path = %q", calls[0].path)
	}
	if len(games) != 1 || games[0].ID != 82448 || games[0].Number != 12 || games[0].Title != "Игра" {
		t.Fatalf("games = %+v", games)
	}
}

// TestNewEngineAdminGamesLeavesStatusEmpty pins parity with the legacy manager,
// which does not publish a status either: the admin listing carries only a
// numeric status_id whose values the API does not document, and a label derived
// from the start and finish times would read as authoritative while being wrong
// for a game that was cancelled.
func TestNewEngineAdminGamesLeavesStatusEmpty(t *testing.T) {
	var calls []adminCall
	c := newAdminClient(t, &calls)

	games, err := c.AdminGetGames(context.Background())
	if err != nil {
		t.Fatalf("AdminGetGames: %v", err)
	}
	if len(games) != 1 {
		t.Fatalf("games = %+v", games)
	}
	if games[0].Status != "" {
		t.Errorf("Status = %q, want empty", games[0].Status)
	}
}

func TestNewEngineAdminGetLevels(t *testing.T) {
	var calls []adminCall
	c := newAdminClient(t, &calls)

	levels, err := c.AdminGetLevels(context.Background(), 82448)
	if err != nil {
		t.Fatalf("AdminGetLevels: %v", err)
	}
	if len(levels) != 3 {
		t.Fatalf("levels = %+v", levels)
	}
	if levels[1] != (AdminLevel{Number: 2, Name: "Второй", ID: 812}) {
		t.Errorf("levels[1] = %+v", levels[1])
	}
}

// TestNewEngineAdminResolvesLevelNumberToID pins the core translation: the
// legacy API addresses levels by number, the new one by ID.
func TestNewEngineAdminResolvesLevelNumberToID(t *testing.T) {
	var calls []adminCall
	c := newAdminClient(t, &calls)

	if err := c.AdminDeleteLevel(context.Background(), 82448, 2); err != nil {
		t.Fatalf("AdminDeleteLevel: %v", err)
	}
	want := []string{"GET /admin/games/82448/levels", "DELETE /admin/games/82448/levels/812"}
	got := adminCallPaths(calls)
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("calls = %v, want %v", got, want)
	}
}

func TestNewEngineAdminReportsMissingLevel(t *testing.T) {
	var calls []adminCall
	c := newAdminClient(t, &calls)

	err := c.AdminDeleteLevel(context.Background(), 82448, 99)
	if err == nil {
		t.Fatal("AdminDeleteLevel accepted a level that does not exist")
	}
	if !strings.Contains(err.Error(), "no level 99") {
		t.Errorf("error = %v", err)
	}
}

func TestNewEngineAdminCreateLevels(t *testing.T) {
	var calls []adminCall
	c := newAdminClient(t, &calls)

	if err := c.AdminCreateLevels(context.Background(), 82448, 3); err != nil {
		t.Fatalf("AdminCreateLevels: %v", err)
	}
	if len(calls) != 3 {
		t.Fatalf("calls = %v, want three creates", adminCallPaths(calls))
	}
	for i, call := range calls {
		if call.method != http.MethodPost || call.path != "/admin/games/82448/levels" {
			t.Errorf("calls[%d] = %s %s", i, call.method, call.path)
		}
	}
}

func TestNewEngineAdminRenameLevelsKeepsComments(t *testing.T) {
	var calls []adminCall
	c := newAdminClient(t, &calls)

	err := c.AdminRenameLevels(context.Background(), 82448, map[int]string{2: "Новое имя", 1: "Первое имя"})
	if err != nil {
		t.Fatalf("AdminRenameLevels: %v", err)
	}
	if len(calls) != 3 {
		t.Fatalf("calls = %v, want a read and two writes", adminCallPaths(calls))
	}
	// Renames run in level order, not map order.
	if calls[1].path != "/admin/games/82448/levels/811/meta" ||
		calls[2].path != "/admin/games/82448/levels/812/meta" {
		t.Errorf("calls = %v, want level 1 before level 2", adminCallPaths(calls))
	}

	var meta struct {
		LevelName string `json:"level_name"`
		Comment   string `json:"comment"`
	}
	if err := json.Unmarshal([]byte(calls[1].body), &meta); err != nil {
		t.Fatalf("body is not JSON: %q", calls[1].body)
	}
	if meta.LevelName != "Первое имя" {
		t.Errorf("level_name = %q", meta.LevelName)
	}
	if meta.Comment != "комментарий 1" {
		t.Errorf("comment = %q, want the existing comment preserved", meta.Comment)
	}
}

func TestNewEngineAdminLevelOrderOperations(t *testing.T) {
	t.Run("swap", func(t *testing.T) {
		var calls []adminCall
		c := newAdminClient(t, &calls)
		if err := c.AdminSwapLevels(context.Background(), 82448, 1, 3); err != nil {
			t.Fatalf("AdminSwapLevels: %v", err)
		}
		call := lastCall(t, calls)
		if call.path != "/admin/games/82448/levels/exchange" {
			t.Errorf("path = %q", call.path)
		}
		var body struct {
			Level1ID int `json:"level1_id"`
			Level2ID int `json:"level2_id"`
		}
		_ = json.Unmarshal([]byte(call.body), &body)
		if body.Level1ID != 811 || body.Level2ID != 813 {
			t.Errorf("body = %+v, want 811 and 813", body)
		}
	})

	// AdminInsertLevel means "put src after dst" on both engines, which is what
	// the legacy form's ddlInsertAfterSrc / ddlInsertAfterDst fields say.
	t.Run("insert after the named level", func(t *testing.T) {
		var calls []adminCall
		c := newAdminClient(t, &calls)
		if err := c.AdminInsertLevel(context.Background(), 82448, 3, 1); err != nil {
			t.Fatalf("AdminInsertLevel: %v", err)
		}
		call := lastCall(t, calls)
		if call.path != "/admin/games/82448/levels/put" {
			t.Errorf("path = %q", call.path)
		}
		var body struct {
			LevelID      int `json:"level_id"`
			AfterLevelID int `json:"after_level_id"`
		}
		_ = json.Unmarshal([]byte(call.body), &body)
		if body.LevelID != 813 || body.AfterLevelID != 811 {
			t.Errorf("body = %+v, want level 813 placed after level 811", body)
		}
	})

	t.Run("insert to the front", func(t *testing.T) {
		var calls []adminCall
		c := newAdminClient(t, &calls)
		if err := c.AdminInsertLevel(context.Background(), 82448, 3, 0); err != nil {
			t.Fatalf("AdminInsertLevel: %v", err)
		}
		var body struct {
			AfterLevelID int `json:"after_level_id"`
		}
		_ = json.Unmarshal([]byte(lastCall(t, calls).body), &body)
		if body.AfterLevelID != 0 {
			t.Errorf("after_level_id = %d, want 0 for the first position", body.AfterLevelID)
		}
	})

	t.Run("insert after a level that does not exist", func(t *testing.T) {
		var calls []adminCall
		c := newAdminClient(t, &calls)
		err := c.AdminInsertLevel(context.Background(), 82448, 3, 99)
		if err == nil {
			t.Fatal("AdminInsertLevel accepted an unknown destination level")
		}
		if !strings.Contains(err.Error(), "no level 99") {
			t.Errorf("error = %v", err)
		}
	})

	t.Run("clone", func(t *testing.T) {
		var calls []adminCall
		c := newAdminClient(t, &calls)
		if err := c.AdminCloneLevels(context.Background(), 82448, 4, 2); err != nil {
			t.Fatalf("AdminCloneLevels: %v", err)
		}
		call := lastCall(t, calls)
		if call.path != "/admin/games/82448/levels/copy" {
			t.Errorf("path = %q", call.path)
		}
		var body struct {
			FromLevelID int `json:"from_level_id"`
			Count       int `json:"count"`
		}
		_ = json.Unmarshal([]byte(call.body), &body)
		if body.FromLevelID != 812 || body.Count != 4 {
			t.Errorf("body = %+v", body)
		}
	})
}

func TestNewEngineAdminTasks(t *testing.T) {
	ctx := context.Background()

	t.Run("read", func(t *testing.T) {
		var calls []adminCall
		c := newAdminClient(t, &calls)
		ids, err := c.AdminGetTaskIds(ctx, 82448, 2)
		if err != nil {
			t.Fatalf("AdminGetTaskIds: %v", err)
		}
		if len(ids) != 1 || ids[0] != 9001 {
			t.Errorf("ids = %v", ids)
		}
		task, err := c.AdminGetTask(ctx, 82448, 2, 9001)
		if err != nil {
			t.Fatalf("AdminGetTask: %v", err)
		}
		if task.Text != "Задание" || !task.ReplaceNl || task.ForMemberID != "0" {
			t.Errorf("task = %+v", task)
		}
		if _, err := c.AdminGetTask(ctx, 82448, 2, 404); err == nil {
			t.Error("AdminGetTask invented a task")
		}
	})

	t.Run("write", func(t *testing.T) {
		var calls []adminCall
		c := newAdminClient(t, &calls)
		task := AdminTask{Text: "Новое", ReplaceNl: true, ForMemberID: "501"}
		if err := c.AdminCreateTask(ctx, 82448, 2, task); err != nil {
			t.Fatalf("AdminCreateTask: %v", err)
		}
		call := lastCall(t, calls)
		if call.method != http.MethodPost || call.path != "/admin/games/82448/levels/812/tasks" {
			t.Errorf("request = %s %s", call.method, call.path)
		}
		var body struct {
			TaskText      string `json:"task_text"`
			ReplaceNlToBr bool   `json:"replace_nl_to_br"`
			ForMemberID   int    `json:"for_member_id"`
		}
		_ = json.Unmarshal([]byte(call.body), &body)
		if body.TaskText != "Новое" || !body.ReplaceNlToBr || body.ForMemberID != 501 {
			t.Errorf("body = %+v", body)
		}

		calls = nil
		if err := c.AdminUpdateTask(ctx, 82448, 2, 9001, task); err != nil {
			t.Fatalf("AdminUpdateTask: %v", err)
		}
		if call := lastCall(t, calls); call.method != http.MethodPut ||
			call.path != "/admin/games/82448/levels/812/tasks/9001" {
			t.Errorf("request = %s %s", call.method, call.path)
		}

		calls = nil
		if err := c.AdminDeleteTask(ctx, 82448, 2, 9001); err != nil {
			t.Fatalf("AdminDeleteTask: %v", err)
		}
		if call := lastCall(t, calls); call.method != http.MethodDelete ||
			call.path != "/admin/games/82448/levels/812/tasks/9001" {
			t.Errorf("request = %s %s", call.method, call.path)
		}
	})
}

func TestNewEngineAdminHints(t *testing.T) {
	ctx := context.Background()

	t.Run("read splits the timeout into days", func(t *testing.T) {
		var calls []adminCall
		c := newAdminClient(t, &calls)
		ids, err := c.AdminGetHintIds(ctx, 82448, 2)
		if err != nil {
			t.Fatalf("AdminGetHintIds: %v", err)
		}
		// Regular and penalty hints share one ID space for the caller.
		if len(ids) != 2 || ids[0] != 7701 || ids[1] != 7801 {
			t.Errorf("ids = %v", ids)
		}

		hint, err := c.AdminGetHint(ctx, 82448, 2, 7701)
		if err != nil {
			t.Fatalf("AdminGetHint: %v", err)
		}
		// 90061s = 1d 01:01:01
		if hint.Days != 1 || hint.Hours != 1 || hint.Minutes != 1 || hint.Seconds != 1 {
			t.Errorf("timeout = %dd %02d:%02d:%02d", hint.Days, hint.Hours, hint.Minutes, hint.Seconds)
		}
		if hint.IsPenalty {
			t.Error("IsPenalty = true for a regular hint")
		}
	})

	t.Run("read penalty hints", func(t *testing.T) {
		var calls []adminCall
		c := newAdminClient(t, &calls)
		hint, err := c.AdminGetHint(ctx, 82448, 2, 7801)
		if err != nil {
			t.Fatalf("AdminGetHint: %v", err)
		}
		if !hint.IsPenalty || !hint.RequestConfirm || hint.PenaltyComment != "минус" {
			t.Errorf("hint = %+v", hint)
		}
		// 3661s = 01:01:01
		if hint.PenaltyHours != 1 || hint.PenaltyMinutes != 1 || hint.PenaltySeconds != 1 {
			t.Errorf("penalty = %02d:%02d:%02d", hint.PenaltyHours, hint.PenaltyMinutes, hint.PenaltySeconds)
		}
		if hint.ForMemberID != "501" {
			t.Errorf("ForMemberID = %q, want the user the hint is addressed to", hint.ForMemberID)
		}
	})

	t.Run("write folds days back into seconds", func(t *testing.T) {
		var calls []adminCall
		c := newAdminClient(t, &calls)
		hint := AdminHint{
			Text: "Текст", Days: 1, Hours: 1, Minutes: 1, Seconds: 1,
			IsPenalty: true, PenaltyHours: 1, PenaltyMinutes: 1, PenaltySeconds: 1,
			PenaltyComment: "минус", RequestConfirm: true,
		}
		if err := c.AdminCreateHint(ctx, 82448, 2, hint); err != nil {
			t.Fatalf("AdminCreateHint: %v", err)
		}
		call := lastCall(t, calls)
		if call.path != "/admin/games/82448/levels/812/helps" {
			t.Errorf("path = %q", call.path)
		}
		var body struct {
			Timeout               int    `json:"timeout"`
			PenaltyTime           int    `json:"penalty_time"`
			IsPenalty             bool   `json:"is_penalty"`
			PenaltyComment        string `json:"penalty_comment"`
			RequestPenaltyConfirm bool   `json:"request_penalty_confirm"`
		}
		_ = json.Unmarshal([]byte(call.body), &body)
		if body.Timeout != 90061 {
			t.Errorf("timeout = %d, want 90061", body.Timeout)
		}
		if body.PenaltyTime != 3661 || !body.IsPenalty || !body.RequestPenaltyConfirm ||
			body.PenaltyComment != "минус" {
			t.Errorf("body = %+v", body)
		}
	})
}

func TestNewEngineAdminBonuses(t *testing.T) {
	ctx := context.Background()

	t.Run("read", func(t *testing.T) {
		var calls []adminCall
		c := newAdminClient(t, &calls)
		bonus, err := c.AdminGetBonus(ctx, 82448, 2, 9101)
		if err != nil {
			t.Fatalf("AdminGetBonus: %v", err)
		}
		if bonus.Name != "Бонус" || bonus.Task != "Найдите" || bonus.Hint != "Хинт" {
			t.Errorf("bonus = %+v", bonus)
		}
		if len(bonus.Answers) != 2 || bonus.Answers[0] != "ответ1" {
			t.Errorf("answers = %v", bonus.Answers)
		}
		if bonus.AwardHours != 1 || bonus.AwardMinutes != 1 || bonus.AwardSeconds != 1 || !bonus.Negative {
			t.Errorf("award = %02d:%02d:%02d negative=%v",
				bonus.AwardHours, bonus.AwardMinutes, bonus.AwardSeconds, bonus.Negative)
		}
		if bonus.ValidFrom != "01.09.2026 10:00:00" || bonus.ValidTo != "01.09.2026 12:00:00" {
			t.Errorf("validity = %q..%q", bonus.ValidFrom, bonus.ValidTo)
		}
		if bonus.DelayMinutes != 2 || bonus.WorkMinutes != 30 {
			t.Errorf("delay/life = %+v", bonus)
		}
		if bonus.BonusFor != "77" {
			t.Errorf("BonusFor = %q, want the team id", bonus.BonusFor)
		}
	})

	t.Run("write", func(t *testing.T) {
		var calls []adminCall
		c := newAdminClient(t, &calls)
		bonus := AdminBonus{
			Name: "Б", Task: "T", Hint: "H", Answers: []string{"x"},
			LevelID: 812, AwardMinutes: 2, DelaySeconds: 30, WorkMinutes: 5,
			ValidFrom: "01.09.2026 10:00:00",
		}
		if err := c.AdminCreateBonus(ctx, 82448, 2, bonus); err != nil {
			t.Fatalf("AdminCreateBonus: %v", err)
		}
		var body struct {
			BonusTime        int      `json:"bonus_time"`
			AllLevels        bool     `json:"all_levels"`
			LevelIDs         []int    `json:"level_ids"`
			HasDelay         bool     `json:"has_delay"`
			DelaySec         int      `json:"delay_sec"`
			HasRelativeLimit bool     `json:"has_relative_limit"`
			LifeTimeSec      int      `json:"life_time_sec"`
			HasAbsoluteLimit bool     `json:"has_absolute_limit"`
			Answers          []string `json:"answers"`
		}
		_ = json.Unmarshal([]byte(lastCall(t, calls).body), &body)
		if body.BonusTime != 120 {
			t.Errorf("bonus_time = %d, want 120", body.BonusTime)
		}
		if body.AllLevels || len(body.LevelIDs) != 1 || body.LevelIDs[0] != 812 {
			t.Errorf("all_levels = %v, level_ids = %v, want the named level",
				body.AllLevels, body.LevelIDs)
		}
		if !body.HasDelay || body.DelaySec != 30 {
			t.Errorf("delay = %v/%d", body.HasDelay, body.DelaySec)
		}
		if !body.HasRelativeLimit || body.LifeTimeSec != 300 {
			t.Errorf("life time = %v/%d", body.HasRelativeLimit, body.LifeTimeSec)
		}
		if !body.HasAbsoluteLimit {
			t.Error("has_absolute_limit = false although valid_from was set")
		}
		if len(body.Answers) != 1 || body.Answers[0] != "x" {
			t.Errorf("answers = %v", body.Answers)
		}
	})

	// AdminBonus.LevelID carries the legacy rbAllLevels choice: 0 and -1 mean
	// the whole game (encx/admin.go legacyAdminCreateBonus). Sending the level
	// being edited instead would narrow a game-wide bonus on every update.
	t.Run("a bonus without a level is game-wide", func(t *testing.T) {
		for _, levelID := range []int{0, -1} {
			var calls []adminCall
			c := newAdminClient(t, &calls)
			bonus := AdminBonus{Name: "Б", LevelID: levelID}
			if err := c.AdminUpdateBonus(ctx, 82448, 2, 9101, bonus); err != nil {
				t.Fatalf("AdminUpdateBonus: %v", err)
			}
			var body struct {
				AllLevels bool  `json:"all_levels"`
				LevelIDs  []int `json:"level_ids"`
			}
			_ = json.Unmarshal([]byte(lastCall(t, calls).body), &body)
			if !body.AllLevels || len(body.LevelIDs) != 0 {
				t.Errorf("LevelID %d: all_levels = %v, level_ids = %v, want a game-wide bonus",
					levelID, body.AllLevels, body.LevelIDs)
			}
		}
	})

	t.Run("read reports a game-wide bonus as level 0", func(t *testing.T) {
		var calls []adminCall
		c := newEngineClient(t, func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			calls = append(calls, adminCall{method: r.Method, path: r.URL.Path, body: string(body)})
			if r.URL.Path == "/admin/games/82448/levels" {
				_, _ = w.Write([]byte(adminLevelsFixture))
				return
			}
			_, _ = w.Write([]byte(`{"level":{"level_id":812,"level_number":2},
			  "bonuses":[{"bonus_id":9101,"bonus_name":"Б","all_levels":true,"level_ids":[812]}]}`))
		})

		bonus, err := c.AdminGetBonus(ctx, 82448, 2, 9101)
		if err != nil {
			t.Fatalf("AdminGetBonus: %v", err)
		}
		if bonus.LevelID != 0 {
			t.Errorf("LevelID = %d, want 0 for a game-wide bonus", bonus.LevelID)
		}
	})
}

func TestNewEngineAdminSectorsAndAnswers(t *testing.T) {
	ctx := context.Background()

	t.Run("read groups answers by sector", func(t *testing.T) {
		var calls []adminCall
		c := newAdminClient(t, &calls)
		sectors, err := c.AdminGetSectorAnswers(ctx, 82448, 2)
		if err != nil {
			t.Fatalf("AdminGetSectorAnswers: %v", err)
		}
		if len(sectors) != 2 {
			t.Fatalf("sectors = %+v", sectors)
		}
		if sectors[0].ID != 5501 || len(sectors[0].Answers) != 2 {
			t.Errorf("sectors[0] = %+v", sectors[0])
		}
		if sectors[1].ID != 5502 || len(sectors[1].Answers) != 1 {
			t.Errorf("sectors[1] = %+v", sectors[1])
		}

		refs, err := c.AdminGetSectorRefs(ctx, 82448, 2)
		if err != nil {
			t.Fatalf("AdminGetSectorRefs: %v", err)
		}
		if len(refs) != 2 || refs[0].Answers != nil {
			t.Errorf("refs = %+v, want names without answers", refs)
		}
	})

	t.Run("create adds the answers too", func(t *testing.T) {
		var calls []adminCall
		c := newAdminClient(t, &calls)
		sector := AdminSector{Name: "Новый", Answers: []string{"n1", "n2"}}
		if err := c.AdminCreateSector(ctx, 82448, 2, sector); err != nil {
			t.Fatalf("AdminCreateSector: %v", err)
		}
		paths := adminCallPaths(calls)
		want := []string{
			"GET /admin/games/82448/levels",
			"POST /admin/games/82448/levels/812/sectors",
			"POST /admin/games/82448/levels/812/answers/batch",
		}
		if len(paths) != 3 || paths[1] != want[1] || paths[2] != want[2] {
			t.Fatalf("calls = %v, want %v", paths, want)
		}
		var batch struct {
			SectorID int `json:"sector_id"`
			Answers  []struct {
				AnswerText string `json:"answer_text"`
			} `json:"answers"`
		}
		_ = json.Unmarshal([]byte(calls[2].body), &batch)
		if batch.SectorID != 5599 {
			t.Errorf("sector_id = %d, want the id the server assigned", batch.SectorID)
		}
		if len(batch.Answers) != 2 || batch.Answers[0].AnswerText != "n1" {
			t.Errorf("answers = %+v", batch.Answers)
		}
	})

	t.Run("update reconciles the answer list", func(t *testing.T) {
		var calls []adminCall
		c := newAdminClient(t, &calls)
		// Sector 5501 currently holds a1 and a2; ask for a1 and a3.
		sector := AdminSector{Name: "Сектор A", Answers: []string{"a1", "a3"}}
		if err := c.AdminUpdateSector(ctx, 82448, 2, 5501, sector); err != nil {
			t.Fatalf("AdminUpdateSector: %v", err)
		}
		paths := adminCallPaths(calls)
		if !containsCall(paths, "DELETE /admin/games/82448/levels/812/answers/2") {
			t.Errorf("calls = %v, want a2 deleted", paths)
		}
		if containsCall(paths, "DELETE /admin/games/82448/levels/812/answers/1") {
			t.Errorf("calls = %v, want a1 kept", paths)
		}
		var batch struct {
			Answers []struct {
				AnswerText string `json:"answer_text"`
			} `json:"answers"`
		}
		_ = json.Unmarshal([]byte(lastCall(t, calls).body), &batch)
		if len(batch.Answers) != 1 || batch.Answers[0].AnswerText != "a3" {
			t.Errorf("added answers = %+v, want only a3", batch.Answers)
		}
	})

	t.Run("clear deletes every sector", func(t *testing.T) {
		var calls []adminCall
		c := newAdminClient(t, &calls)
		if err := c.AdminClearLevelSectors(ctx, 82448, 2); err != nil {
			t.Fatalf("AdminClearLevelSectors: %v", err)
		}
		paths := adminCallPaths(calls)
		for _, want := range []string{
			"DELETE /admin/games/82448/levels/812/sectors/5501",
			"DELETE /admin/games/82448/levels/812/sectors/5502",
		} {
			if !containsCall(paths, want) {
				t.Errorf("calls = %v, want %q", paths, want)
			}
		}
	})
}

func TestNewEngineAdminMessages(t *testing.T) {
	ctx := context.Background()
	var calls []adminCall
	c := newAdminClient(t, &calls)

	ids, err := c.AdminGetMessageIds(ctx, 82448, 2)
	if err != nil {
		t.Fatalf("AdminGetMessageIds: %v", err)
	}
	if len(ids) != 1 || ids[0] != 3301 {
		t.Errorf("ids = %v", ids)
	}

	message, err := c.AdminGetMessage(ctx, 82448, 2, 3301)
	if err != nil {
		t.Fatalf("AdminGetMessage: %v", err)
	}
	if message.Text != "Сообщение" || !message.ReplaceNlToBr {
		t.Errorf("message = %+v", message)
	}
	if message.ShowOnLevelsMode != 2 {
		t.Errorf("ShowOnLevelsMode = %d, want 2 (chosen levels)", message.ShowOnLevelsMode)
	}
	if message.RequiredPoints != "5" {
		t.Errorf("RequiredPoints = %q", message.RequiredPoints)
	}

	calls = nil
	err = c.AdminCreateMessage(ctx, 82448, 812, AdminGameMessage{
		Text: "Новое", ShowOnLevelsMode: 1, RequiredPoints: "3",
	})
	if err != nil {
		t.Fatalf("AdminCreateMessage: %v", err)
	}
	call := lastCall(t, calls)
	if call.path != "/admin/games/82448/levels/812/messages" {
		t.Errorf("path = %q", call.path)
	}
	var body struct {
		AllLevels      bool  `json:"all_levels"`
		LevelIDs       []int `json:"level_ids"`
		RequiredPoints int   `json:"required_points"`
	}
	_ = json.Unmarshal([]byte(call.body), &body)
	if !body.AllLevels || len(body.LevelIDs) != 0 || body.RequiredPoints != 3 {
		t.Errorf("body = %+v", body)
	}
}

func TestNewEngineAdminLevelSettings(t *testing.T) {
	ctx := context.Background()

	t.Run("read", func(t *testing.T) {
		var calls []adminCall
		c := newAdminClient(t, &calls)
		settings, err := c.AdminGetLevelSettings(ctx, 82448, 2)
		if err != nil {
			t.Fatalf("AdminGetLevelSettings: %v", err)
		}
		// 3720s = 01:02:00
		if settings.AutopassHours != 1 || settings.AutopassMinutes != 2 || settings.AutopassSeconds != 0 {
			t.Errorf("autopass = %+v", settings)
		}
		if !settings.TimeoutPenalty || settings.PenaltyMinutes != 5 {
			t.Errorf("penalty = %+v", settings)
		}
		if settings.AttemptsNumber != 5 || settings.AttemptsPeriodMinutes != 1 ||
			settings.AttemptsPeriodSeconds != 30 {
			t.Errorf("attempts = %+v", settings)
		}
		if settings.ApplyForPlayer != 1 {
			t.Errorf("ApplyForPlayer = %d, want 1 for block_type_id 1", settings.ApplyForPlayer)
		}
		if settings.RequiredSectorsCount != 2 {
			t.Errorf("RequiredSectorsCount = %d", settings.RequiredSectorsCount)
		}
	})

	t.Run("autopass", func(t *testing.T) {
		var calls []adminCall
		c := newAdminClient(t, &calls)
		err := c.AdminUpdateAutopass(ctx, 82448, 2, AdminLevelSettings{
			AutopassHours: 2, AutopassMinutes: 30,
			TimeoutPenalty: true, PenaltyMinutes: 10,
		})
		if err != nil {
			t.Fatalf("AdminUpdateAutopass: %v", err)
		}
		call := lastCall(t, calls)
		if call.method != http.MethodPut || call.path != "/admin/games/82448/levels/812/autopass" {
			t.Errorf("request = %s %s", call.method, call.path)
		}
		var body struct {
			TimeoutHours   int  `json:"timeout_hours"`
			TimeoutMinutes int  `json:"timeout_minutes"`
			PenaltyEnabled bool `json:"penalty_enabled"`
			PenaltyMinutes int  `json:"penalty_minutes"`
		}
		_ = json.Unmarshal([]byte(call.body), &body)
		if body.TimeoutHours != 2 || body.TimeoutMinutes != 30 ||
			!body.PenaltyEnabled || body.PenaltyMinutes != 10 {
			t.Errorf("body = %+v", body)
		}
	})

	t.Run("answer block", func(t *testing.T) {
		var calls []adminCall
		c := newAdminClient(t, &calls)
		err := c.AdminUpdateAnswerBlock(ctx, 82448, 2, AdminLevelSettings{
			AttemptsNumber: 3, AttemptsPeriodMinutes: 5, ApplyForPlayer: 0,
		})
		if err != nil {
			t.Fatalf("AdminUpdateAnswerBlock: %v", err)
		}
		call := lastCall(t, calls)
		if call.path != "/admin/games/82448/levels/812/settings" {
			t.Errorf("path = %q", call.path)
		}
		var body struct {
			Section        string `json:"section"`
			AttemptsNumber int    `json:"attempts_number"`
			BlockTypeID    int    `json:"block_type_id"`
		}
		_ = json.Unmarshal([]byte(call.body), &body)
		if body.Section != "blocking" || body.AttemptsNumber != 3 {
			t.Errorf("body = %+v", body)
		}
		if body.BlockTypeID != 2 {
			t.Errorf("block_type_id = %d, want 2 (team) when ApplyForPlayer is 0", body.BlockTypeID)
		}
	})

	t.Run("sector completion", func(t *testing.T) {
		var calls []adminCall
		c := newAdminClient(t, &calls)
		if err := c.AdminUpdateSectorCompletion(ctx, 82448, 2, 3); err != nil {
			t.Fatalf("AdminUpdateSectorCompletion: %v", err)
		}
		var body struct {
			Section              string `json:"section"`
			PassingConditionID   int    `json:"passing_condition_id"`
			RequiredSectorsCount int    `json:"required_sectors_count"`
		}
		_ = json.Unmarshal([]byte(lastCall(t, calls).body), &body)
		if body.Section != "sectors" || body.PassingConditionID != 1 || body.RequiredSectorsCount != 3 {
			t.Errorf("body = %+v", body)
		}

		// Zero must be sent, not omitted: "close every sector" is a value, and
		// a missing field would read as "leave the setting alone".
		calls = nil
		if err := c.AdminUpdateSectorCompletion(ctx, 82448, 2, 0); err != nil {
			t.Fatalf("AdminUpdateSectorCompletion: %v", err)
		}
		raw := lastCall(t, calls).body
		if !strings.Contains(raw, `"passing_condition_id":0`) {
			t.Errorf("body = %s, want an explicit passing_condition_id of 0", raw)
		}
		if !strings.Contains(raw, `"required_sectors_count":0`) {
			t.Errorf("body = %s, want an explicit required_sectors_count of 0", raw)
		}
	})

	t.Run("comment", func(t *testing.T) {
		var calls []adminCall
		c := newAdminClient(t, &calls)
		name, comment, err := c.AdminGetComment(ctx, 82448, 2)
		if err != nil {
			t.Fatalf("AdminGetComment: %v", err)
		}
		if name != "Второй" || comment != "комментарий 2" {
			t.Errorf("name/comment = %q/%q", name, comment)
		}

		calls = nil
		if err := c.AdminUpdateComment(ctx, 82448, 2, "Имя", "Коммент"); err != nil {
			t.Fatalf("AdminUpdateComment: %v", err)
		}
		call := lastCall(t, calls)
		if call.method != http.MethodPut || call.path != "/admin/games/82448/levels/812/meta" {
			t.Errorf("request = %s %s", call.method, call.path)
		}
	})
}

// TestNewEngineAdminSkipsThrottling pins that the pacing which protects the
// ASP.NET admin forms does not slow the REST engine: the delay only guards
// legacy page requests, and the new backend has its own rate limit.
func TestNewEngineAdminSkipsThrottling(t *testing.T) {
	var calls []adminCall
	c := newAdminClient(t, &calls)
	c.SetAdminDelay(2 * time.Second)

	start := time.Now()
	for i := 0; i < 3; i++ {
		if _, err := c.AdminGetLevels(context.Background(), 82448); err != nil {
			t.Fatalf("AdminGetLevels: %v", err)
		}
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("three admin reads took %s; the ASP.NET pacing must not apply to REST", elapsed)
	}
}

func containsCall(paths []string, want string) bool {
	for _, path := range paths {
		if path == want {
			return true
		}
	}
	return false
}

// TestNewEngineAdminMessageDefaultsToAllLevels pins the legacy default:
// AdminGameMessage.ShowOnLevelsMode is optional, and the ASP form treats
// anything other than 2 — including the zero value callers leave unset — as
// "show on all levels".
func TestNewEngineAdminMessageDefaultsToAllLevels(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name          string
		mode          int
		wantAllLevels bool
	}{
		{"unset", 0, true},
		{"explicit all levels", 1, true},
		{"chosen levels", 2, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls []adminCall
			c := newAdminClient(t, &calls)
			err := c.AdminCreateMessage(ctx, 82448, 812, AdminGameMessage{
				Text: "текст", ShowOnLevelsMode: tc.mode,
			})
			if err != nil {
				t.Fatalf("AdminCreateMessage: %v", err)
			}
			var body struct {
				AllLevels bool  `json:"all_levels"`
				LevelIDs  []int `json:"level_ids"`
			}
			_ = json.Unmarshal([]byte(lastCall(t, calls).body), &body)
			if body.AllLevels != tc.wantAllLevels {
				t.Errorf("all_levels = %v, want %v", body.AllLevels, tc.wantAllLevels)
			}
			if !tc.wantAllLevels && (len(body.LevelIDs) != 1 || body.LevelIDs[0] != 812) {
				t.Errorf("level_ids = %v, want the addressed level", body.LevelIDs)
			}
		})
	}
}

// TestNewEngineAdminHintCanClearItsFlags pins that a false reaches the server:
// with omitempty the cleared checkbox would simply not be sent and the old
// value would survive the update.
func TestNewEngineAdminHintCanClearItsFlags(t *testing.T) {
	var calls []adminCall
	c := newAdminClient(t, &calls)

	err := c.AdminUpdateHint(context.Background(), 82448, 2, 7801, AdminHint{
		Text: "Текст", IsPenalty: true, RequestConfirm: false, PenaltyComment: "",
	})
	if err != nil {
		t.Fatalf("AdminUpdateHint: %v", err)
	}
	raw := lastCall(t, calls).body
	for _, want := range []string{`"request_penalty_confirm":false`, `"penalty_comment":""`} {
		if !strings.Contains(raw, want) {
			t.Errorf("body = %s, want %s to be sent explicitly", raw, want)
		}
	}
}

func TestNewEngineAdminBonusCanClearItsLimits(t *testing.T) {
	var calls []adminCall
	c := newAdminClient(t, &calls)

	err := c.AdminUpdateBonus(context.Background(), 82448, 2, 9101, AdminBonus{
		Name: "Б", LevelID: 812,
	})
	if err != nil {
		t.Fatalf("AdminUpdateBonus: %v", err)
	}
	raw := lastCall(t, calls).body
	for _, want := range []string{
		`"has_absolute_limit":false`, `"has_delay":false`, `"has_relative_limit":false`,
	} {
		if !strings.Contains(raw, want) {
			t.Errorf("body = %s, want %s to be sent explicitly", raw, want)
		}
	}
}
