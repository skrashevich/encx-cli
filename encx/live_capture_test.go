package encx

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// These are the tools that produced cmd/encx-mock/fixtures/live_*.json. They are
// kept because the mock is only worth what its shapes are worth: when the engine
// changes, the reference documents are refreshed by running these against a real
// domain rather than by editing them to match the client.

// TestCaptureAdminFixtures records the raw shape of every admin route against a
// game it builds and removes itself. The dumps are the ground truth for the
// mock's admin surface: a mock written from the specification would repeat the
// assumptions the live audit just disproved.
//
//	ENCX_LIVE_DOMAIN=demo.en.cx ENCX_LIVE_LOGIN=… ENCX_LIVE_PASSWORD=… \
//	  ENCX_CAPTURE_DIR=/tmp/admin-capture go test ./encx/ -run TestCaptureAdminFixtures -v
func TestCaptureAdminFixtures(t *testing.T) {
	dir := os.Getenv("ENCX_CAPTURE_DIR")
	if dir == "" {
		t.Skip("set ENCX_CAPTURE_DIR to capture admin fixtures")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	c := liveClient(t)
	gameID := scratchLevels(t, c, 3)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	dump := func(name, path string, q url.Values) {
		var raw json.RawMessage
		err := c.api().GetJSON(ctx, path, q, &raw)
		if err != nil {
			t.Logf("%-22s %s -> %v", name, path, err)
			raw = json.RawMessage(fmt.Sprintf(`{"__error":%q}`, err.Error()))
		}
		var pretty any
		_ = json.Unmarshal(raw, &pretty)
		out, _ := json.MarshalIndent(pretty, "", "  ")
		if err := os.WriteFile(filepath.Join(dir, name+".json"), out, 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		t.Logf("%-22s %s -> %d bytes", name, path, len(out))
	}

	// --- Populate the game so every collection has something in it. ---
	if err := c.AdminRenameLevels(ctx, gameID, map[int]string{
		1: "Первый уровень", 2: "Второй уровень", 3: "Третий уровень",
	}); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if err := c.AdminUpdateComment(ctx, gameID, 1, "Первый уровень", "комментарий автора"); err != nil {
		t.Fatalf("comment: %v", err)
	}
	if err := c.AdminCreateTask(ctx, gameID, 1, AdminTask{
		Text: "Задание первого уровня <b>с разметкой</b>", ReplaceNl: true,
	}); err != nil {
		t.Fatalf("task: %v", err)
	}
	if err := c.AdminCreateHint(ctx, gameID, 1, AdminHint{
		Text: "Обычная подсказка", Hours: 1, Minutes: 2, Seconds: 3,
	}); err != nil {
		t.Fatalf("hint: %v", err)
	}
	if err := c.AdminCreateHint(ctx, gameID, 1, AdminHint{
		Text: "Штрафная подсказка", Minutes: 5, IsPenalty: true,
		PenaltyMinutes: 10, PenaltyComment: "минус десять минут", RequestConfirm: true,
	}); err != nil {
		t.Fatalf("penalty hint: %v", err)
	}
	levels, err := c.AdminGetLevels(ctx, gameID)
	if err != nil {
		t.Fatalf("levels: %v", err)
	}
	if err := c.AdminCreateBonus(ctx, gameID, 1, AdminBonus{
		Name: "Бонус уровня", Task: "Найдите знак", Hint: "Он у входа",
		Answers: []string{"бонус1", "бонус2"}, LevelID: levels[0].ID,
		AwardMinutes: 7, DelayMinutes: 3, WorkMinutes: 9,
	}); err != nil {
		t.Fatalf("bonus: %v", err)
	}
	if err := c.AdminCreateBonus(ctx, gameID, 1, AdminBonus{
		Name: "Бонус всей игры", Answers: []string{"всюду"}, AwardMinutes: 1, Negative: true,
	}); err != nil {
		t.Fatalf("game bonus: %v", err)
	}
	if err := c.AdminCreateMessage(ctx, gameID, levels[0].ID, AdminGameMessage{
		Text: "Сообщение организаторов", ReplaceNlToBr: true,
	}); err != nil {
		t.Fatalf("message: %v", err)
	}
	for _, s := range []AdminSector{
		{Name: "Сектор А", Answers: []string{"альфа", "бета"}},
		{Name: "Сектор Б", Answers: []string{"гамма"}},
	} {
		if err := c.AdminCreateSector(ctx, gameID, 1, s); err != nil {
			t.Fatalf("sector %q: %v", s.Name, err)
		}
	}
	if err := c.AdminUpdateAutopass(ctx, gameID, 1, AdminLevelSettings{
		AutopassHours: 1, AutopassMinutes: 30, TimeoutPenalty: true, PenaltyMinutes: 15,
	}); err != nil {
		t.Fatalf("autopass: %v", err)
	}
	if err := c.AdminUpdateAnswerBlock(ctx, gameID, 1, AdminLevelSettings{
		AttemptsNumber: 3, AttemptsPeriodMinutes: 1, ApplyForPlayer: 1,
	}); err != nil {
		t.Fatalf("answer block: %v", err)
	}
	if err := c.AdminUpdateSectorCompletion(ctx, gameID, 1, 1); err != nil {
		t.Fatalf("sector completion: %v", err)
	}
	if err := c.AdminUpdateGameInfo(ctx, gameID, AdminGameInfo{
		Description: "Описание тестовой игры", MaxPlayers: "42", MaxTeamPlayers: "7",
		AuthorComplexity: "3", IsModerated: true, ShowFinishPlace: true,
	}); err != nil {
		t.Fatalf("game info: %v", err)
	}

	// --- Capture. ---
	dump("admin_games", "/admin/games", nil)
	dump("admin_game_editor", fmt.Sprintf("/admin/games/%d", gameID), nil)
	dump("admin_game_lifecycle", fmt.Sprintf("/admin/games/%d/lifecycle", gameID), nil)
	dump("admin_levels", fmt.Sprintf("/admin/games/%d/levels", gameID), nil)
	for i, level := range levels {
		dump(fmt.Sprintf("admin_level_editor_%d", i+1),
			fmt.Sprintf("/admin/games/%d/levels/%d/editor", gameID, level.ID), nil)
	}
	dump("game_monitoring", fmt.Sprintf("/games/%d/monitoring", gameID), url.Values{"tab": {"0"}})
	dump("game_corrections", fmt.Sprintf("/games/%d/corrections", gameID), nil)
	dump("game_scenario", fmt.Sprintf("/games/%d/scenario", gameID), url.Values{"lang": {"ru"}})
	dump("game_statistics", fmt.Sprintf("/games/%d/statistics", gameID), url.Values{"lang": {"ru"}})

	t.Logf("captured into %s (game %d)", dir, gameID)
}

// TestCaptureLegacyAdminPages records the raw HTML of the ASP.NET admin pages so
// the mock's legacy admin surface can be built from what the engine really
// serves. Authoring that HTML from the parsers instead would only prove the
// parsers agree with themselves.
//
//	ENCX_LEGACY_DOMAIN=svk.en.cx ENCX_LEGACY_LOGIN=… ENCX_LEGACY_PASSWORD=… \
//	  ENCX_LEGACY_GAME_ID=82448 ENCX_CAPTURE_DIR=/tmp/legacy-capture \
//	  go test ./encx/ -run TestCaptureLegacyAdminPages -v
func TestCaptureLegacyAdminPages(t *testing.T) {
	dir := os.Getenv("ENCX_CAPTURE_DIR")
	domain := os.Getenv("ENCX_LEGACY_DOMAIN")
	login := os.Getenv("ENCX_LEGACY_LOGIN")
	password := os.Getenv("ENCX_LEGACY_PASSWORD")
	if dir == "" || domain == "" || login == "" || password == "" {
		t.Skip("set ENCX_CAPTURE_DIR and ENCX_LEGACY_DOMAIN/LOGIN/PASSWORD")
	}
	gameID, err := strconv.Atoi(os.Getenv("ENCX_LEGACY_GAME_ID"))
	if err != nil {
		t.Fatalf("ENCX_LEGACY_GAME_ID: %v", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	c := New(domain, WithEngine(EngineLegacy), WithTimeout(60*time.Second))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := c.LoginComplete(ctx, login, password); err != nil {
		t.Fatalf("LoginComplete against %s: %v", domain, err)
	}

	grab := func(name, path string) {
		body, err := c.doGet(ctx, c.baseURL()+path)
		if err != nil {
			t.Logf("%-24s %s -> %v", name, path, err)
			return
		}
		if err := os.WriteFile(filepath.Join(dir, name+".html"), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		t.Logf("%-24s %s -> %d bytes", name, path, len(body))
	}

	grab("games_manager", "/Administration/GamesManager.aspx")
	grab("level_manager", fmt.Sprintf("/Administration/Games/LevelManager.aspx?gid=%d", gameID))
	grab("game_editor", fmt.Sprintf("/Administration/Games/GameEditor.aspx?gid=%d&action=edit", gameID))
	grab("corrections", fmt.Sprintf("/GameBonusPenaltyTime.aspx?gid=%d&lang=ru", gameID))
	grab("action_monitor", fmt.Sprintf("/Administration/Games/ActionMonitor.aspx?gid=%d&type=own", gameID))

	levels, err := c.AdminGetLevels(ctx, gameID)
	if err != nil {
		t.Fatalf("AdminGetLevels: %v", err)
	}
	t.Logf("game %d has %d levels", gameID, len(levels))
	if len(levels) == 0 {
		return
	}
	level := levels[0].Number

	grab("level_editor", fmt.Sprintf("/Administration/Games/LevelEditor.aspx?level=%d&gid=%d", level, gameID))
	grab("level_editor_answers",
		fmt.Sprintf("/Administration/Games/LevelEditor.aspx?level=%d&gid=%d&swanswers=1", level, gameID))
	grab("name_comment_edit",
		fmt.Sprintf("/Administration/Games/NameCommentEdit.aspx?gid=%d&level=%d", gameID, level))

	// The per-item editors need real ids, which only the level editor knows.
	if ids, err := c.AdminGetTaskIds(ctx, gameID, level); err == nil && len(ids) > 0 {
		grab("task_edit", fmt.Sprintf("/Administration/Games/TaskEdit.aspx?gid=%d&level=%d&task=%d",
			gameID, level, ids[0]))
	} else {
		t.Logf("no tasks on level %d: %v", level, err)
	}
	if ids, err := c.AdminGetHintIds(ctx, gameID, level); err == nil && len(ids) > 0 {
		grab("prompt_edit", fmt.Sprintf("/Administration/Games/PromptEdit.aspx?gid=%d&level=%d&prid=%d",
			gameID, level, ids[0]))
	} else {
		t.Logf("no hints on level %d: %v", level, err)
	}
	if ids, err := c.AdminGetBonusIds(ctx, gameID, level); err == nil && len(ids) > 0 {
		grab("bonus_edit", fmt.Sprintf("/Administration/Games/BonusEdit.aspx?gid=%d&level=%d&bonus=%d&action=edit",
			gameID, level, ids[0]))
	} else {
		t.Logf("no bonuses on level %d: %v", level, err)
	}
	if ids, err := c.AdminGetMessageIds(ctx, gameID, level); err == nil && len(ids) > 0 {
		grab("message_edit", fmt.Sprintf("/Administration/Games/MessageEdit.aspx?gid=%d&level=%d&mid=%d",
			gameID, level, ids[0]))
	} else {
		t.Logf("no messages on level %d: %v", level, err)
	}

	t.Logf("captured into %s", dir)
}
