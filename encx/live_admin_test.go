package encx

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"
)

// The admin round-trip runs against a game this test creates itself, so it can
// write freely without touching anybody else's data. Set the same ENCX_LIVE_*
// variables the read-only live tests use:
//
//	ENCX_LIVE_DOMAIN=demo.en.cx ENCX_LIVE_LOGIN=… ENCX_LIVE_PASSWORD=… \
//	  go test ./encx/ -run TestLiveAdmin -v
//
// Every check is a round-trip: whatever the test writes through the public
// encx.Client API it reads back through the same API and compares against what
// the legacy engine would have reported. A mapping that loses a field, inverts a
// flag or picks the wrong unit fails here even though the request itself
// succeeded.

// scratchGame creates a game on the live domain and registers its removal.
func scratchGame(t *testing.T, c *Client) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	id, err := c.AdminCreateGame(ctx, scratchGameParams())
	if err != nil {
		t.Fatalf("create scratch game: %v", err)
	}
	t.Logf("scratch game %d created", id)

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if err := c.api().Delete(ctx, fmt.Sprintf("/admin/games/%d", id), nil, nil); err != nil {
			t.Errorf("delete scratch game %d: %v", id, err)
			return
		}
		t.Logf("scratch game %d deleted", id)
	})
	return id
}

func scratchGameParams() AdminCreateGameParams {
	return AdminCreateGameParams{
		Title:          fmt.Sprintf("encx parity %d", time.Now().UnixNano()),
		Description:    "temporary game created by the encx parity test",
		GameType:       1,
		StartDateTime:  time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
		FinishDateTime: time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339),
	}
}

// scratchLevels creates a game with the requested number of levels.
func scratchLevels(t *testing.T, c *Client, count int) (gameID int) {
	t.Helper()
	gameID = scratchGame(t, c)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := c.AdminCreateLevels(ctx, gameID, count); err != nil {
		t.Fatalf("AdminCreateLevels(%d): %v", count, err)
	}
	levels, err := c.AdminGetLevels(ctx, gameID)
	if err != nil {
		t.Fatalf("AdminGetLevels: %v", err)
	}
	if len(levels) != count {
		t.Fatalf("AdminCreateLevels(%d) produced %d levels", count, len(levels))
	}
	return gameID
}

func liveAdminCtx(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 60*time.Second)
}

// TestLiveAdminCreateGame checks that a game created through AdminCreateGame
// really exists: the returned id must open in the editor with the title and
// description that were sent, and it must appear in the admin game list.
func TestLiveAdminCreateGame(t *testing.T) {
	c := liveClient(t)
	ctx, cancel := liveAdminCtx(t)
	defer cancel()

	params := scratchGameParams()
	params.IsModerated = true
	gameID, err := c.AdminCreateGame(ctx, params)
	if err != nil {
		t.Fatalf("AdminCreateGame: %v", err)
	}
	if gameID <= 0 {
		t.Fatalf("AdminCreateGame returned id %d", gameID)
	}
	t.Logf("created game %d", gameID)
	t.Cleanup(func() {
		ctx, cancel := liveAdminCtx(t)
		defer cancel()
		if err := c.api().Delete(ctx, fmt.Sprintf("/admin/games/%d", gameID), nil, nil); err != nil {
			t.Errorf("delete game %d: %v", gameID, err)
		}
	})

	info, err := c.AdminGetGameInfo(ctx, gameID)
	if err != nil {
		t.Fatalf("AdminGetGameInfo(%d): %v", gameID, err)
	}
	if info.Title != params.Title {
		t.Errorf("title = %q, want %q", info.Title, params.Title)
	}
	if info.Description != params.Description {
		t.Errorf("description = %q, want %q", info.Description, params.Description)
	}
	if !info.IsModerated {
		t.Error("is_moderated did not survive creation")
	}

	games, err := c.AdminGetGames(ctx)
	if err != nil {
		t.Fatalf("AdminGetGames: %v", err)
	}
	found := false
	for _, game := range games {
		if game.ID == gameID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("game %d is missing from AdminGetGames", gameID)
	}
}

// TestLiveAdminScratchGameShape reports what the new engine actually stores for
// a freshly created game, which is the ground truth the mappings must match.
func TestLiveAdminScratchGameShape(t *testing.T) {
	c := liveClient(t)
	gameID := scratchLevels(t, c, 1)
	ctx, cancel := liveAdminCtx(t)
	defer cancel()

	levels, err := c.AdminGetLevels(ctx, gameID)
	if err != nil {
		t.Fatalf("AdminGetLevels: %v", err)
	}
	var raw json.RawMessage
	path := fmt.Sprintf("/admin/games/%d/levels/%d/editor", gameID, levels[0].ID)
	if err := c.api().GetJSON(ctx, path, nil, &raw); err != nil {
		t.Fatalf("level editor: %v", err)
	}
	if os.Getenv("ENCX_LIVE_DUMP") != "" {
		t.Logf("GET %s: %s", path, raw)
	} else {
		t.Logf("GET %s: %d bytes (set ENCX_LIVE_DUMP=1 to print)", path, len(raw))
	}
}

// TestLiveAdminLevels exercises the level manager: create, rename, comment,
// swap, insert and clone all address levels by number, so a mis-resolved id
// shows up as the wrong level carrying the change.
func TestLiveAdminLevels(t *testing.T) {
	c := liveClient(t)
	gameID := scratchLevels(t, c, 3)
	ctx, cancel := liveAdminCtx(t)
	defer cancel()

	levels, err := c.AdminGetLevels(ctx, gameID)
	if err != nil {
		t.Fatalf("AdminGetLevels: %v", err)
	}
	for i, level := range levels {
		if level.Number != i+1 {
			t.Errorf("level %d has number %d, want %d", i, level.Number, i+1)
		}
		if level.ID == 0 {
			t.Errorf("level %d has no id", level.Number)
		}
	}

	names := map[int]string{1: "первый", 2: "второй", 3: "третий"}
	if err := c.AdminRenameLevels(ctx, gameID, names); err != nil {
		t.Fatalf("AdminRenameLevels: %v", err)
	}
	levels, err = c.AdminGetLevels(ctx, gameID)
	if err != nil {
		t.Fatalf("AdminGetLevels after rename: %v", err)
	}
	for _, level := range levels {
		if want := names[level.Number]; level.Name != want {
			t.Errorf("level %d name = %q, want %q", level.Number, level.Name, want)
		}
	}

	// AdminUpdateComment writes both the name and the comment; AdminGetComment
	// must read back exactly that pair.
	if err := c.AdminUpdateComment(ctx, gameID, 2, "второй*", "комментарий уровня"); err != nil {
		t.Fatalf("AdminUpdateComment: %v", err)
	}
	name, comment, err := c.AdminGetComment(ctx, gameID, 2)
	if err != nil {
		t.Fatalf("AdminGetComment: %v", err)
	}
	if name != "второй*" || comment != "комментарий уровня" {
		t.Errorf("AdminGetComment = (%q, %q), want (%q, %q)", name, comment, "второй*", "комментарий уровня")
	}

	// A rename must not wipe the comment the level already carries: the legacy
	// form posted the names alone and left everything else in place.
	if err := c.AdminRenameLevels(ctx, gameID, map[int]string{2: "второй"}); err != nil {
		t.Fatalf("AdminRenameLevels (single): %v", err)
	}
	name, comment, err = c.AdminGetComment(ctx, gameID, 2)
	if err != nil {
		t.Fatalf("AdminGetComment after rename: %v", err)
	}
	if name != "второй" {
		t.Errorf("level 2 name = %q, want %q", name, "второй")
	}
	if comment != "комментарий уровня" {
		t.Errorf("rename dropped the comment: got %q", comment)
	}

	// Reordering is the one place where the deployed backend answers 204 and
	// changes nothing. Either outcome is acceptable here — what must never
	// happen is the third one: success reported over an unchanged game.
	reorder(t, c, gameID, "AdminSwapLevels(1,3)",
		func() error { return c.AdminSwapLevels(ctx, gameID, 1, 3) },
		func(before []string) []string {
			after := append([]string(nil), before...)
			after[0], after[2] = after[2], after[0]
			return after
		})
	// Insert level 1 after level 3, the legacy ddlInsertAfterSrc/Dst meaning.
	reorder(t, c, gameID, "AdminInsertLevel(1,3)",
		func() error { return c.AdminInsertLevel(ctx, gameID, 1, 3) },
		func(before []string) []string {
			return []string{before[1], before[2], before[0]}
		})

	// Clone: two more levels shaped like level 1.
	if err := c.AdminCloneLevels(ctx, gameID, 2, 1); err != nil {
		t.Fatalf("AdminCloneLevels: %v", err)
	}
	levels, err = c.AdminGetLevels(ctx, gameID)
	if err != nil {
		t.Fatalf("AdminGetLevels after clone: %v", err)
	}
	if len(levels) != 5 {
		t.Errorf("after cloning 2 levels the game has %d, want 5", len(levels))
	}

	if err := c.AdminDeleteLevel(ctx, gameID, 5); err != nil {
		t.Fatalf("AdminDeleteLevel: %v", err)
	}
	levels, err = c.AdminGetLevels(ctx, gameID)
	if err != nil {
		t.Fatalf("AdminGetLevels after delete: %v", err)
	}
	if len(levels) != 4 {
		t.Errorf("after deleting one level the game has %d, want 4", len(levels))
	}
}

// reorder runs a level-reordering call and holds it to the only contract that
// matters for a transparent engine switch: the game must end up in the order the
// caller asked for, or the call must say it failed.
//
// The deployed backend answers 204 for both reorder routes and leaves the game
// untouched, so the second branch is what happens today. The test accepts it —
// and the day the routes start working, the first branch takes over without
// anyone having to touch this file. What it never accepts is a reported success
// over an unchanged game.
func reorder(t *testing.T, c *Client, gameID int, label string, op func() error, want func([]string) []string) {
	t.Helper()
	ctx, cancel := liveAdminCtx(t)
	defer cancel()

	levels, err := c.AdminGetLevels(ctx, gameID)
	if err != nil {
		t.Fatalf("%s: AdminGetLevels before: %v", label, err)
	}
	before := levelNames(levels)
	expected := want(before)

	opErr := op()

	levels, err = c.AdminGetLevels(ctx, gameID)
	if err != nil {
		t.Fatalf("%s: AdminGetLevels after: %v", label, err)
	}
	after := levelNames(levels)

	switch {
	case opErr == nil && reflect.DeepEqual(after, expected):
		t.Logf("%s: applied, %v -> %v", label, before, after)
	case opErr == nil:
		t.Errorf("%s reported success but the order is %v, want %v", label, after, expected)
	case reflect.DeepEqual(after, expected):
		t.Errorf("%s reported %v although the order did change to %v", label, opErr, after)
	default:
		t.Logf("%s: the engine did not apply it and said so: %v", label, opErr)
	}
}

func levelNames(levels []AdminLevel) []string {
	names := make([]string, 0, len(levels))
	for _, level := range levels {
		names = append(names, level.Name)
	}
	return names
}

// TestLiveAdminTasks round-trips a level task.
func TestLiveAdminTasks(t *testing.T) {
	c := liveClient(t)
	gameID := scratchLevels(t, c, 1)
	ctx, cancel := liveAdminCtx(t)
	defer cancel()

	want := AdminTask{Text: "задание уровня <b>1</b>", ReplaceNl: true}
	if err := c.AdminCreateTask(ctx, gameID, 1, want); err != nil {
		t.Fatalf("AdminCreateTask: %v", err)
	}
	ids, err := c.AdminGetTaskIds(ctx, gameID, 1)
	if err != nil {
		t.Fatalf("AdminGetTaskIds: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("AdminGetTaskIds = %v, want one task", ids)
	}
	got, err := c.AdminGetTask(ctx, gameID, 1, ids[0])
	if err != nil {
		t.Fatalf("AdminGetTask: %v", err)
	}
	if got.Text != want.Text {
		t.Errorf("task text = %q, want %q", got.Text, want.Text)
	}
	if got.ReplaceNl != want.ReplaceNl {
		t.Errorf("task replace_nl = %v, want %v", got.ReplaceNl, want.ReplaceNl)
	}
	if got.ForMemberID != "0" {
		t.Errorf("task for_member_id = %q, want %q", got.ForMemberID, "0")
	}

	updated := AdminTask{Text: "исправленное задание", ReplaceNl: false}
	if err := c.AdminUpdateTask(ctx, gameID, 1, ids[0], updated); err != nil {
		t.Fatalf("AdminUpdateTask: %v", err)
	}
	got, err = c.AdminGetTask(ctx, gameID, 1, ids[0])
	if err != nil {
		t.Fatalf("AdminGetTask after update: %v", err)
	}
	if got.Text != updated.Text {
		t.Errorf("after update task text = %q, want %q", got.Text, updated.Text)
	}
	if got.ReplaceNl {
		t.Error("after update replace_nl is still set; clearing the checkbox did not travel")
	}

	if err := c.AdminDeleteTask(ctx, gameID, 1, ids[0]); err != nil {
		t.Fatalf("AdminDeleteTask: %v", err)
	}
	ids, err = c.AdminGetTaskIds(ctx, gameID, 1)
	if err != nil {
		t.Fatalf("AdminGetTaskIds after delete: %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("after delete the level still has tasks %v", ids)
	}
}

// TestLiveAdminHints round-trips a plain hint and a penalty hint, which the
// legacy engine kept in two different repeaters and the new one in two arrays.
func TestLiveAdminHints(t *testing.T) {
	c := liveClient(t)
	gameID := scratchLevels(t, c, 1)
	ctx, cancel := liveAdminCtx(t)
	defer cancel()

	plain := AdminHint{Text: "обычная подсказка", Hours: 1, Minutes: 2, Seconds: 3}
	if err := c.AdminCreateHint(ctx, gameID, 1, plain); err != nil {
		t.Fatalf("AdminCreateHint: %v", err)
	}
	penalty := AdminHint{
		Text:           "штрафная подсказка",
		Minutes:        5,
		IsPenalty:      true,
		PenaltyMinutes: 10,
		PenaltyComment: "минус десять минут",
		RequestConfirm: true,
	}
	if err := c.AdminCreateHint(ctx, gameID, 1, penalty); err != nil {
		t.Fatalf("AdminCreateHint (penalty): %v", err)
	}

	ids, err := c.AdminGetHintIds(ctx, gameID, 1)
	if err != nil {
		t.Fatalf("AdminGetHintIds: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("AdminGetHintIds = %v, want two hints", ids)
	}

	byText := map[string]*AdminHint{}
	for _, id := range ids {
		hint, err := c.AdminGetHint(ctx, gameID, 1, id)
		if err != nil {
			t.Fatalf("AdminGetHint(%d): %v", id, err)
		}
		byText[hint.Text] = hint
	}

	got, ok := byText[plain.Text]
	if !ok {
		t.Fatalf("the plain hint is missing; read back %v", keysOf(byText))
	}
	if got.Hours != 1 || got.Minutes != 2 || got.Seconds != 3 || got.Days != 0 {
		t.Errorf("plain hint timeout = %dd %dh %dm %ds, want 0d 1h 2m 3s",
			got.Days, got.Hours, got.Minutes, got.Seconds)
	}
	if got.IsPenalty {
		t.Error("the plain hint came back marked as a penalty hint")
	}

	got, ok = byText[penalty.Text]
	if !ok {
		t.Fatalf("the penalty hint is missing; read back %v", keysOf(byText))
	}
	if !got.IsPenalty {
		t.Error("the penalty hint came back without is_penalty")
	}
	if got.Minutes != 5 {
		t.Errorf("penalty hint timeout = %dm, want 5m", got.Minutes)
	}
	if got.PenaltyMinutes != 10 || got.PenaltyHours != 0 || got.PenaltySeconds != 0 {
		t.Errorf("penalty time = %dh %dm %ds, want 0h 10m 0s",
			got.PenaltyHours, got.PenaltyMinutes, got.PenaltySeconds)
	}
	if got.PenaltyComment != penalty.PenaltyComment {
		t.Errorf("penalty comment = %q, want %q", got.PenaltyComment, penalty.PenaltyComment)
	}
	if !got.RequestConfirm {
		t.Error("request_confirm did not survive the round trip")
	}

	// Clearing the penalty flags must travel too: the legacy form sent every
	// checkbox on every save.
	cleared := AdminHint{Text: penalty.Text, Minutes: 5, IsPenalty: true, PenaltyMinutes: 10}
	penaltyID := hintIDByText(t, c, gameID, penalty.Text)
	if err := c.AdminUpdateHint(ctx, gameID, 1, penaltyID, cleared); err != nil {
		t.Fatalf("AdminUpdateHint: %v", err)
	}
	got, err = c.AdminGetHint(ctx, gameID, 1, penaltyID)
	if err != nil {
		t.Fatalf("AdminGetHint after update: %v", err)
	}
	if got.RequestConfirm {
		t.Error("request_confirm stayed set after being cleared")
	}
	if got.PenaltyComment != "" {
		t.Errorf("penalty comment stayed %q after being cleared", got.PenaltyComment)
	}

	if err := c.AdminDeleteHint(ctx, gameID, 1, penaltyID); err != nil {
		t.Fatalf("AdminDeleteHint: %v", err)
	}
	ids, err = c.AdminGetHintIds(ctx, gameID, 1)
	if err != nil {
		t.Fatalf("AdminGetHintIds after delete: %v", err)
	}
	if len(ids) != 1 {
		t.Errorf("after deleting one hint the level has %d", len(ids))
	}
}

func hintIDByText(t *testing.T, c *Client, gameID int, text string) int {
	t.Helper()
	ctx, cancel := liveAdminCtx(t)
	defer cancel()
	ids, err := c.AdminGetHintIds(ctx, gameID, 1)
	if err != nil {
		t.Fatalf("AdminGetHintIds: %v", err)
	}
	for _, id := range ids {
		hint, err := c.AdminGetHint(ctx, gameID, 1, id)
		if err != nil {
			t.Fatalf("AdminGetHint(%d): %v", id, err)
		}
		if hint.Text == text {
			return id
		}
	}
	t.Fatalf("no hint with text %q", text)
	return 0
}

func keysOf[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}

// TestLiveAdminBonuses checks the bonus round-trip, including the game-wide
// choice the legacy form expressed as rbAllLevels.
func TestLiveAdminBonuses(t *testing.T) {
	c := liveClient(t)
	gameID := scratchLevels(t, c, 2)
	ctx, cancel := liveAdminCtx(t)
	defer cancel()

	levels, err := c.AdminGetLevels(ctx, gameID)
	if err != nil {
		t.Fatalf("AdminGetLevels: %v", err)
	}

	scoped := AdminBonus{
		Name:         "бонус уровня",
		Task:         "задание бонуса",
		Hint:         "подсказка бонуса",
		Answers:      []string{"ответ1", "ответ2"},
		LevelID:      levels[0].ID,
		AwardMinutes: 7,
		DelayMinutes: 3,
		WorkMinutes:  9,
	}
	if err := c.AdminCreateBonus(ctx, gameID, 1, scoped); err != nil {
		t.Fatalf("AdminCreateBonus: %v", err)
	}
	wide := AdminBonus{Name: "бонус игры", Answers: []string{"всюду"}, AwardMinutes: 1}
	if err := c.AdminCreateBonus(ctx, gameID, 1, wide); err != nil {
		t.Fatalf("AdminCreateBonus (game-wide): %v", err)
	}

	ids, err := c.AdminGetBonusIds(ctx, gameID, 1)
	if err != nil {
		t.Fatalf("AdminGetBonusIds: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("AdminGetBonusIds = %v, want two bonuses", ids)
	}

	byName := map[string]*AdminBonus{}
	for _, id := range ids {
		bonus, err := c.AdminGetBonus(ctx, gameID, 1, id)
		if err != nil {
			t.Fatalf("AdminGetBonus(%d): %v", id, err)
		}
		byName[bonus.Name] = bonus
	}

	got, ok := byName[scoped.Name]
	if !ok {
		t.Fatalf("the level bonus is missing; read back %v", keysOf(byName))
	}
	if got.Task != scoped.Task || got.Hint != scoped.Hint {
		t.Errorf("bonus task/hint = %q/%q, want %q/%q", got.Task, got.Hint, scoped.Task, scoped.Hint)
	}
	if !reflect.DeepEqual(got.Answers, scoped.Answers) {
		t.Errorf("bonus answers = %v, want %v", got.Answers, scoped.Answers)
	}
	if got.AwardMinutes != 7 || got.AwardHours != 0 || got.AwardSeconds != 0 {
		t.Errorf("award time = %dh %dm %ds, want 0h 7m 0s", got.AwardHours, got.AwardMinutes, got.AwardSeconds)
	}
	if got.DelayMinutes != 3 {
		t.Errorf("delay = %dh %dm %ds, want 3m", got.DelayHours, got.DelayMinutes, got.DelaySeconds)
	}
	if got.WorkMinutes != 9 {
		t.Errorf("life time = %dh %dm %ds, want 9m", got.WorkHours, got.WorkMinutes, got.WorkSeconds)
	}
	if got.LevelID != levels[0].ID {
		t.Errorf("bonus level id = %d, want %d", got.LevelID, levels[0].ID)
	}

	got, ok = byName[wide.Name]
	if !ok {
		t.Fatalf("the game-wide bonus is missing; read back %v", keysOf(byName))
	}
	if got.LevelID != 0 {
		t.Errorf("game-wide bonus reports level %d, want 0", got.LevelID)
	}

	// Read-modify-write must not narrow the game-wide bonus to this level.
	wideID := bonusIDByName(t, c, gameID, wide.Name)
	if err := c.AdminUpdateBonus(ctx, gameID, 1, wideID, *got); err != nil {
		t.Fatalf("AdminUpdateBonus: %v", err)
	}
	after, err := c.AdminGetBonus(ctx, gameID, 1, wideID)
	if err != nil {
		t.Fatalf("AdminGetBonus after update: %v", err)
	}
	if after.LevelID != 0 {
		t.Errorf("after a read-modify-write the game-wide bonus is pinned to level %d", after.LevelID)
	}

	if err := c.AdminDeleteBonus(ctx, gameID, 1, wideID); err != nil {
		t.Fatalf("AdminDeleteBonus: %v", err)
	}
	ids, err = c.AdminGetBonusIds(ctx, gameID, 1)
	if err != nil {
		t.Fatalf("AdminGetBonusIds after delete: %v", err)
	}
	if len(ids) != 1 {
		t.Errorf("after deleting one bonus the level has %d", len(ids))
	}
}

func bonusIDByName(t *testing.T, c *Client, gameID int, name string) int {
	t.Helper()
	ctx, cancel := liveAdminCtx(t)
	defer cancel()
	ids, err := c.AdminGetBonusIds(ctx, gameID, 1)
	if err != nil {
		t.Fatalf("AdminGetBonusIds: %v", err)
	}
	for _, id := range ids {
		bonus, err := c.AdminGetBonus(ctx, gameID, 1, id)
		if err != nil {
			t.Fatalf("AdminGetBonus(%d): %v", id, err)
		}
		if bonus.Name == name {
			return id
		}
	}
	t.Fatalf("no bonus named %q", name)
	return 0
}

// TestLiveAdminMessages checks the message round-trip and the "all levels"
// default the legacy form expressed by anything other than mode 2.
func TestLiveAdminMessages(t *testing.T) {
	c := liveClient(t)
	gameID := scratchLevels(t, c, 2)
	ctx, cancel := liveAdminCtx(t)
	defer cancel()

	levels, err := c.AdminGetLevels(ctx, gameID)
	if err != nil {
		t.Fatalf("AdminGetLevels: %v", err)
	}

	// ShowOnLevelsMode left unset means "all levels" on the legacy engine.
	all := AdminGameMessage{Text: "сообщение всем уровням", ReplaceNlToBr: true}
	if err := c.AdminCreateMessage(ctx, gameID, levels[0].ID, all); err != nil {
		t.Fatalf("AdminCreateMessage: %v", err)
	}
	one := AdminGameMessage{
		Text:             "сообщение одному уровню",
		ShowOnLevelsMode: 2,
		LevelIDs:         []int{levels[1].ID},
	}
	if err := c.AdminCreateMessage(ctx, gameID, levels[0].ID, one); err != nil {
		t.Fatalf("AdminCreateMessage (single level): %v", err)
	}

	ids, err := c.AdminGetMessageIds(ctx, gameID, 1)
	if err != nil {
		t.Fatalf("AdminGetMessageIds: %v", err)
	}
	byText := map[string]*AdminGameMessage{}
	for _, id := range ids {
		message, err := c.AdminGetMessage(ctx, gameID, 1, id)
		if err != nil {
			t.Fatalf("AdminGetMessage(%d): %v", id, err)
		}
		byText[message.Text] = message
	}

	got, ok := byText[all.Text]
	if !ok {
		t.Fatalf("the all-levels message is missing; read back %v", keysOf(byText))
	}
	if got.ShowOnLevelsMode != 1 {
		t.Errorf("all-levels message mode = %d, want 1", got.ShowOnLevelsMode)
	}
	if !got.ReplaceNlToBr {
		t.Error("replace_nl_to_br did not survive the round trip")
	}

	if _, ok := byText[one.Text]; ok {
		t.Errorf("the level-2 message is listed on level 1: %v", keysOf(byText))
	}
	level2, err := c.AdminGetMessageIds(ctx, gameID, 2)
	if err != nil {
		t.Fatalf("AdminGetMessageIds(level 2): %v", err)
	}
	found := false
	for _, id := range level2 {
		message, err := c.AdminGetMessage(ctx, gameID, 2, id)
		if err != nil {
			t.Fatalf("AdminGetMessage(level 2, %d): %v", id, err)
		}
		if message.Text == one.Text {
			found = true
			if message.ShowOnLevelsMode != 2 {
				t.Errorf("single-level message mode = %d, want 2", message.ShowOnLevelsMode)
			}
		}
	}
	if !found {
		t.Error("the message addressed to level 2 is not listed there")
	}
}

// TestLiveAdminSectors round-trips sectors and their answers.
func TestLiveAdminSectors(t *testing.T) {
	c := liveClient(t)
	gameID := scratchLevels(t, c, 1)
	ctx, cancel := liveAdminCtx(t)
	defer cancel()

	first := AdminSector{Name: "сектор один", Answers: []string{"альфа", "бета"}}
	if err := c.AdminCreateSector(ctx, gameID, 1, first); err != nil {
		t.Fatalf("AdminCreateSector: %v", err)
	}
	second := AdminSector{Name: "сектор два", Answers: []string{"гамма"}}
	if err := c.AdminCreateSector(ctx, gameID, 1, second); err != nil {
		t.Fatalf("AdminCreateSector (second): %v", err)
	}

	sectors, err := c.AdminGetSectorAnswers(ctx, gameID, 1)
	if err != nil {
		t.Fatalf("AdminGetSectorAnswers: %v", err)
	}
	if len(sectors) != 2 {
		t.Fatalf("AdminGetSectorAnswers returned %d sectors, want 2", len(sectors))
	}
	bySector := map[string][]string{}
	idByName := map[string]int{}
	for _, sector := range sectors {
		bySector[sector.Name] = sector.Answers
		idByName[sector.Name] = sector.ID
	}
	if !reflect.DeepEqual(bySector[first.Name], first.Answers) {
		t.Errorf("sector %q answers = %v, want %v", first.Name, bySector[first.Name], first.Answers)
	}
	if !reflect.DeepEqual(bySector[second.Name], second.Answers) {
		t.Errorf("sector %q answers = %v, want %v", second.Name, bySector[second.Name], second.Answers)
	}

	refs, err := c.AdminGetSectorRefs(ctx, gameID, 1)
	if err != nil {
		t.Fatalf("AdminGetSectorRefs: %v", err)
	}
	if len(refs) != 2 {
		t.Errorf("AdminGetSectorRefs returned %d sectors, want 2", len(refs))
	}

	if err := c.AdminAddSectorAnswers(ctx, gameID, 1, idByName[second.Name], []string{"дельта"}); err != nil {
		t.Fatalf("AdminAddSectorAnswers: %v", err)
	}
	sectors, err = c.AdminGetSectorAnswers(ctx, gameID, 1)
	if err != nil {
		t.Fatalf("AdminGetSectorAnswers after add: %v", err)
	}
	for _, sector := range sectors {
		if sector.Name != second.Name {
			continue
		}
		if !reflect.DeepEqual(sector.Answers, []string{"гамма", "дельта"}) {
			t.Errorf("after adding an answer sector %q has %v", sector.Name, sector.Answers)
		}
	}

	// Updating a sector replaces its answer list, which is what saving the
	// legacy form did.
	renamed := AdminSector{Name: "сектор один*", Answers: []string{"альфа", "омега"}}
	if err := c.AdminUpdateSector(ctx, gameID, 1, idByName[first.Name], renamed); err != nil {
		t.Fatalf("AdminUpdateSector: %v", err)
	}
	sectors, err = c.AdminGetSectorAnswers(ctx, gameID, 1)
	if err != nil {
		t.Fatalf("AdminGetSectorAnswers after update: %v", err)
	}
	found := false
	for _, sector := range sectors {
		if sector.ID != idByName[first.Name] {
			continue
		}
		found = true
		if sector.Name != renamed.Name {
			t.Errorf("sector name = %q, want %q", sector.Name, renamed.Name)
		}
		if !reflect.DeepEqual(sector.Answers, renamed.Answers) {
			t.Errorf("sector answers = %v, want %v", sector.Answers, renamed.Answers)
		}
	}
	if !found {
		t.Error("the updated sector disappeared")
	}

	if err := c.AdminDeleteSector(ctx, gameID, 1, idByName[second.Name]); err != nil {
		t.Fatalf("AdminDeleteSector: %v", err)
	}
	sectors, err = c.AdminGetSectorAnswers(ctx, gameID, 1)
	if err != nil {
		t.Fatalf("AdminGetSectorAnswers after delete: %v", err)
	}
	if len(sectors) != 1 {
		t.Errorf("after deleting one sector the level has %d", len(sectors))
	}

	if err := c.AdminClearLevelSectors(ctx, gameID, 1); err != nil {
		t.Fatalf("AdminClearLevelSectors: %v", err)
	}
	sectors, err = c.AdminGetSectorAnswers(ctx, gameID, 1)
	if err != nil {
		t.Fatalf("AdminGetSectorAnswers after clear: %v", err)
	}
	if len(sectors) != 0 {
		t.Errorf("AdminClearLevelSectors left %d sectors", len(sectors))
	}
}

// TestLiveAdminLevelSettings round-trips autopass, answer blocking and the
// sector completion rule.
func TestLiveAdminLevelSettings(t *testing.T) {
	c := liveClient(t)
	gameID := scratchLevels(t, c, 1)
	ctx, cancel := liveAdminCtx(t)
	defer cancel()

	autopass := AdminLevelSettings{
		AutopassHours:   1,
		AutopassMinutes: 30,
		TimeoutPenalty:  true,
		PenaltyMinutes:  15,
	}
	if err := c.AdminUpdateAutopass(ctx, gameID, 1, autopass); err != nil {
		t.Fatalf("AdminUpdateAutopass: %v", err)
	}
	got, err := c.AdminGetLevelSettings(ctx, gameID, 1)
	if err != nil {
		t.Fatalf("AdminGetLevelSettings: %v", err)
	}
	if got.AutopassHours != 1 || got.AutopassMinutes != 30 || got.AutopassSeconds != 0 {
		t.Errorf("autopass = %dh %dm %ds, want 1h 30m 0s",
			got.AutopassHours, got.AutopassMinutes, got.AutopassSeconds)
	}
	if !got.TimeoutPenalty {
		t.Error("timeout penalty did not survive the round trip")
	}
	if got.PenaltyMinutes != 15 || got.PenaltyHours != 0 {
		t.Errorf("autopass penalty = %dh %dm %ds, want 0h 15m 0s",
			got.PenaltyHours, got.PenaltyMinutes, got.PenaltySeconds)
	}

	block := AdminLevelSettings{
		AttemptsNumber:        3,
		AttemptsPeriodHours:   0,
		AttemptsPeriodMinutes: 1,
		ApplyForPlayer:        1,
	}
	if err := c.AdminUpdateAnswerBlock(ctx, gameID, 1, block); err != nil {
		t.Fatalf("AdminUpdateAnswerBlock: %v", err)
	}
	got, err = c.AdminGetLevelSettings(ctx, gameID, 1)
	if err != nil {
		t.Fatalf("AdminGetLevelSettings after answer block: %v", err)
	}
	if got.AttemptsNumber != 3 {
		t.Errorf("attempts number = %d, want 3", got.AttemptsNumber)
	}
	if got.AttemptsPeriodMinutes != 1 || got.AttemptsPeriodHours != 0 || got.AttemptsPeriodSeconds != 0 {
		t.Errorf("attempts period = %dh %dm %ds, want 0h 1m 0s",
			got.AttemptsPeriodHours, got.AttemptsPeriodMinutes, got.AttemptsPeriodSeconds)
	}
	if got.ApplyForPlayer != 1 {
		t.Errorf("apply for player = %d, want 1", got.ApplyForPlayer)
	}

	block.ApplyForPlayer = 0
	if err := c.AdminUpdateAnswerBlock(ctx, gameID, 1, block); err != nil {
		t.Fatalf("AdminUpdateAnswerBlock (team): %v", err)
	}
	got, err = c.AdminGetLevelSettings(ctx, gameID, 1)
	if err != nil {
		t.Fatalf("AdminGetLevelSettings after team block: %v", err)
	}
	if got.ApplyForPlayer != 0 {
		t.Errorf("apply for player = %d after switching back to the team, want 0", got.ApplyForPlayer)
	}

	// The level needs sectors before a completion rule means anything.
	for _, name := range []string{"с1", "с2", "с3"} {
		if err := c.AdminCreateSector(ctx, gameID, 1, AdminSector{Name: name, Answers: []string{name}}); err != nil {
			t.Fatalf("AdminCreateSector(%s): %v", name, err)
		}
	}
	if err := c.AdminUpdateSectorCompletion(ctx, gameID, 1, 2); err != nil {
		t.Fatalf("AdminUpdateSectorCompletion(2): %v", err)
	}
	got, err = c.AdminGetLevelSettings(ctx, gameID, 1)
	if err != nil {
		t.Fatalf("AdminGetLevelSettings after sector completion: %v", err)
	}
	if got.RequiredSectorsCount != 2 {
		t.Errorf("required sectors = %d, want 2", got.RequiredSectorsCount)
	}

	// Zero means "close every sector" on the legacy engine, and the reader must
	// report it as zero again rather than echoing a stale count.
	if err := c.AdminUpdateSectorCompletion(ctx, gameID, 1, 0); err != nil {
		t.Fatalf("AdminUpdateSectorCompletion(0): %v", err)
	}
	got, err = c.AdminGetLevelSettings(ctx, gameID, 1)
	if err != nil {
		t.Fatalf("AdminGetLevelSettings after all-sectors: %v", err)
	}
	if got.RequiredSectorsCount != 0 {
		t.Errorf("required sectors = %d after switching to all sectors, want 0", got.RequiredSectorsCount)
	}
}

// TestLiveAdminGameInfo round-trips the game editor fields.
func TestLiveAdminGameInfo(t *testing.T) {
	c := liveClient(t)
	gameID := scratchGame(t, c)
	ctx, cancel := liveAdminCtx(t)
	defer cancel()

	before, err := c.AdminGetGameInfo(ctx, gameID)
	if err != nil {
		t.Fatalf("AdminGetGameInfo: %v", err)
	}
	if before.Title == "" {
		t.Error("the game editor reports no title")
	}
	if before.Authors == "" {
		t.Error("the game editor reports no authors")
	}

	update := AdminGameInfo{
		Title:          "encx parity renamed",
		Description:    "обновлённое описание",
		MaxPlayers:     "42",
		MaxTeamPlayers: "7",
		// The legacy dropdown offers whole numbers, and the engine stores the
		// value it maps to (afc) to a tenth, so a fractional legacy value would
		// be rounded away. Whole numbers survive intact.
		AuthorComplexity: "3",
		IsModerated:      true,
		ShowFinishPlace:  true,
	}
	if err := c.AdminUpdateGameInfo(ctx, gameID, update); err != nil {
		t.Fatalf("AdminUpdateGameInfo: %v", err)
	}
	after, err := c.AdminGetGameInfo(ctx, gameID)
	if err != nil {
		t.Fatalf("AdminGetGameInfo after update: %v", err)
	}
	if after.Title != update.Title {
		t.Errorf("title = %q, want %q", after.Title, update.Title)
	}
	if after.Description != update.Description {
		t.Errorf("description = %q, want %q", after.Description, update.Description)
	}
	if after.MaxPlayers != update.MaxPlayers {
		t.Errorf("max players = %q, want %q", after.MaxPlayers, update.MaxPlayers)
	}
	if after.MaxTeamPlayers != update.MaxTeamPlayers {
		t.Errorf("max team players = %q, want %q", after.MaxTeamPlayers, update.MaxTeamPlayers)
	}
	if after.AuthorComplexity != update.AuthorComplexity {
		t.Errorf("author complexity = %q, want %q", after.AuthorComplexity, update.AuthorComplexity)
	}
	if !after.IsModerated {
		t.Error("is_moderated did not survive the round trip")
	}
	if !after.ShowFinishPlace {
		t.Error("show_finish_place did not survive the round trip")
	}
	// An empty field means "leave it alone" — the description just written must
	// still be there after an update that does not mention it.
	if err := c.AdminUpdateGameInfo(ctx, gameID, AdminGameInfo{Title: "encx parity renamed twice"}); err != nil {
		t.Fatalf("AdminUpdateGameInfo (partial): %v", err)
	}
	after, err = c.AdminGetGameInfo(ctx, gameID)
	if err != nil {
		t.Fatalf("AdminGetGameInfo after partial update: %v", err)
	}
	if after.Description != update.Description {
		t.Errorf("a partial update cleared the description: %q", after.Description)
	}
	if after.MaxPlayers != update.MaxPlayers {
		t.Errorf("a partial update cleared max players: %q", after.MaxPlayers)
	}
}

// TestLiveAdminReadOnlyRoutes exercises the admin reads that need no fixture
// beyond an empty game.
func TestLiveAdminReadOnlyRoutes(t *testing.T) {
	c := liveClient(t)
	gameID := scratchLevels(t, c, 1)
	ctx, cancel := liveAdminCtx(t)
	defer cancel()

	games, err := c.AdminGetGames(ctx)
	if err != nil {
		t.Fatalf("AdminGetGames: %v", err)
	}
	found := false
	for _, game := range games {
		if game.ID == gameID {
			found = true
			if game.Title == "" {
				t.Error("the scratch game has no title in the admin listing")
			}
			if game.Number == 0 {
				t.Error("the scratch game has no number in the admin listing")
			}
		}
	}
	if !found {
		t.Errorf("AdminGetGames (%d games) does not list the scratch game %d", len(games), gameID)
	}

	if _, err := c.AdminGetTeams(ctx, gameID, 1); err != nil {
		t.Errorf("AdminGetTeams: %v", err)
	}
	if _, err := c.AdminGetActionMonitor(ctx, gameID); err != nil {
		t.Errorf("AdminGetActionMonitor: %v", err)
	}
	if _, err := c.AdminGetCorrections(ctx, gameID); err != nil {
		t.Errorf("AdminGetCorrections: %v", err)
	}
	if _, err := c.GetGameScenario(ctx, gameID); err != nil {
		t.Errorf("GetGameScenario: %v", err)
	}
}
