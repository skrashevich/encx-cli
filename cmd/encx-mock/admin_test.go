package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/skrashevich/encx-cli/encx"
)

// The admin surface is where every defect the live audit found was hiding, and
// it is the half that had no mock at all. These tests drive the public
// encx.Client against the mock, so the mappings are exercised end to end
// offline: what the client writes it must read back.

func adminClient(t *testing.T) (*encx.Client, context.Context) {
	t.Helper()
	_, api := newMockServers(t)
	return signedInClient(t, api), context.Background()
}

func TestMockAdminLevelsRoundTrip(t *testing.T) {
	c, ctx := adminClient(t)

	before, err := c.AdminGetLevels(ctx, mockGameID)
	if err != nil {
		t.Fatalf("AdminGetLevels: %v", err)
	}
	if len(before) == 0 {
		t.Fatal("the mock game has no levels")
	}
	for i, level := range before {
		if level.Number != i+1 {
			t.Errorf("levels[%d].Number = %d, want %d", i, level.Number, i+1)
		}
		if level.ID == 0 {
			t.Errorf("levels[%d] has no id", i)
		}
	}

	if err := c.AdminCreateLevels(ctx, mockGameID, 2); err != nil {
		t.Fatalf("AdminCreateLevels: %v", err)
	}
	after, err := c.AdminGetLevels(ctx, mockGameID)
	if err != nil {
		t.Fatalf("AdminGetLevels after create: %v", err)
	}
	if len(after) != len(before)+2 {
		t.Fatalf("levels = %d, want %d", len(after), len(before)+2)
	}

	last := len(after)
	names := map[int]string{last: "Последний"}
	if err := c.AdminRenameLevels(ctx, mockGameID, names); err != nil {
		t.Fatalf("AdminRenameLevels: %v", err)
	}
	if err := c.AdminUpdateComment(ctx, mockGameID, last, "Последний", "комментарий"); err != nil {
		t.Fatalf("AdminUpdateComment: %v", err)
	}
	name, comment, err := c.AdminGetComment(ctx, mockGameID, last)
	if err != nil {
		t.Fatalf("AdminGetComment: %v", err)
	}
	if name != "Последний" || comment != "комментарий" {
		t.Errorf("AdminGetComment = (%q, %q)", name, comment)
	}

	// A rename must leave the comment alone, which is what the legacy form did.
	if err := c.AdminRenameLevels(ctx, mockGameID, map[int]string{last: "Переименован"}); err != nil {
		t.Fatalf("AdminRenameLevels (single): %v", err)
	}
	if _, comment, err = c.AdminGetComment(ctx, mockGameID, last); err != nil {
		t.Fatalf("AdminGetComment after rename: %v", err)
	}
	if comment != "комментарий" {
		t.Errorf("rename dropped the comment: %q", comment)
	}

	if err := c.AdminDeleteLevel(ctx, mockGameID, last); err != nil {
		t.Fatalf("AdminDeleteLevel: %v", err)
	}
	if after, err = c.AdminGetLevels(ctx, mockGameID); err != nil {
		t.Fatalf("AdminGetLevels after delete: %v", err)
	}
	if len(after) != len(before)+1 {
		t.Errorf("levels = %d after delete, want %d", len(after), len(before)+1)
	}
}

// The reorder routes are the one place the deployed backend answers success and
// changes nothing. A correct engine applies them, and the client's verification
// has to accept that; the quirk covers the other half.
func TestMockAdminReordersLevels(t *testing.T) {
	c, ctx := adminClient(t)

	if err := c.AdminRenameLevels(ctx, mockGameID, map[int]string{1: "A", 2: "B", 3: "C"}); err != nil {
		t.Fatalf("AdminRenameLevels: %v", err)
	}
	order := func() []string {
		levels, err := c.AdminGetLevels(ctx, mockGameID)
		if err != nil {
			t.Fatalf("AdminGetLevels: %v", err)
		}
		names := make([]string, 0, 3)
		for i, level := range levels {
			if i == 3 {
				break
			}
			names = append(names, level.Name)
		}
		return names
	}

	if err := c.AdminSwapLevels(ctx, mockGameID, 1, 3); err != nil {
		t.Fatalf("AdminSwapLevels: %v", err)
	}
	if got := order(); !reflect.DeepEqual(got, []string{"C", "B", "A"}) {
		t.Errorf("after swapping 1 and 3 the order is %v, want [C B A]", got)
	}

	if err := c.AdminInsertLevel(ctx, mockGameID, 1, 3); err != nil {
		t.Fatalf("AdminInsertLevel: %v", err)
	}
	if got := order(); !reflect.DeepEqual(got, []string{"B", "A", "C"}) {
		t.Errorf("after moving level 1 after level 3 the order is %v, want [B A C]", got)
	}
}

// With the deployed backend's quirk turned on the routes answer 204 and change
// nothing, and the client has to notice rather than report success.
func TestMockAdminReorderQuirkIsCaught(t *testing.T) {
	s := newMockServer(t)
	s.admin.reorderIsNoOp = true
	c, ctx := adminClientFor(t, s), context.Background()

	err := c.AdminSwapLevels(ctx, mockGameID, 1, 2)
	if err == nil {
		t.Fatal("AdminSwapLevels reported success although the mock changed nothing")
	}
	if !strings.Contains(err.Error(), "порядок уровней не изменился") {
		t.Errorf("error = %v", err)
	}

	if err := c.AdminInsertLevel(ctx, mockGameID, 1, 2); err == nil {
		t.Fatal("AdminInsertLevel reported success although the mock changed nothing")
	}
}

func TestMockAdminTasksRoundTrip(t *testing.T) {
	c, ctx := adminClient(t)

	want := encx.AdminTask{Text: "Задание <b>уровня</b>", ReplaceNl: true}
	if err := c.AdminCreateTask(ctx, mockGameID, 1, want); err != nil {
		t.Fatalf("AdminCreateTask: %v", err)
	}
	ids, err := c.AdminGetTaskIds(ctx, mockGameID, 1)
	if err != nil {
		t.Fatalf("AdminGetTaskIds: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("task ids = %v, want one", ids)
	}
	got, err := c.AdminGetTask(ctx, mockGameID, 1, ids[0])
	if err != nil {
		t.Fatalf("AdminGetTask: %v", err)
	}
	if got.Text != want.Text || !got.ReplaceNl || got.ForMemberID != "0" {
		t.Errorf("task = %+v, want %+v", got, want)
	}

	if err := c.AdminUpdateTask(ctx, mockGameID, 1, ids[0],
		encx.AdminTask{Text: "Исправлено"}); err != nil {
		t.Fatalf("AdminUpdateTask: %v", err)
	}
	if got, err = c.AdminGetTask(ctx, mockGameID, 1, ids[0]); err != nil {
		t.Fatalf("AdminGetTask after update: %v", err)
	}
	if got.Text != "Исправлено" || got.ReplaceNl {
		t.Errorf("after update task = %+v; clearing the checkbox did not travel", got)
	}

	if err := c.AdminDeleteTask(ctx, mockGameID, 1, ids[0]); err != nil {
		t.Fatalf("AdminDeleteTask: %v", err)
	}
	if ids, err = c.AdminGetTaskIds(ctx, mockGameID, 1); err != nil || len(ids) != 0 {
		t.Errorf("task ids after delete = %v (err %v)", ids, err)
	}
}

func TestMockAdminHintsRoundTrip(t *testing.T) {
	c, ctx := adminClient(t)

	plain := encx.AdminHint{Text: "Обычная", Hours: 1, Minutes: 2, Seconds: 3}
	penalty := encx.AdminHint{
		Text: "Штрафная", Minutes: 5, IsPenalty: true,
		PenaltyMinutes: 10, PenaltyComment: "минус", RequestConfirm: true,
	}
	for _, hint := range []encx.AdminHint{plain, penalty} {
		if err := c.AdminCreateHint(ctx, mockGameID, 1, hint); err != nil {
			t.Fatalf("AdminCreateHint(%q): %v", hint.Text, err)
		}
	}

	ids, err := c.AdminGetHintIds(ctx, mockGameID, 1)
	if err != nil {
		t.Fatalf("AdminGetHintIds: %v", err)
	}
	byText := map[string]*encx.AdminHint{}
	for _, id := range ids {
		hint, err := c.AdminGetHint(ctx, mockGameID, 1, id)
		if err != nil {
			t.Fatalf("AdminGetHint(%d): %v", id, err)
		}
		byText[hint.Text] = hint
	}

	got := byText[plain.Text]
	if got == nil {
		t.Fatalf("the plain hint is missing: %v", ids)
	}
	if got.Days != 0 || got.Hours != 1 || got.Minutes != 2 || got.Seconds != 3 || got.IsPenalty {
		t.Errorf("plain hint = %+v", got)
	}

	got = byText[penalty.Text]
	if got == nil {
		t.Fatalf("the penalty hint is missing: %v", ids)
	}
	if !got.IsPenalty || got.Minutes != 5 || got.PenaltyMinutes != 10 ||
		got.PenaltyComment != "минус" || !got.RequestConfirm {
		t.Errorf("penalty hint = %+v", got)
	}
}

func TestMockAdminBonusesRoundTrip(t *testing.T) {
	c, ctx := adminClient(t)

	levels, err := c.AdminGetLevels(ctx, mockGameID)
	if err != nil {
		t.Fatalf("AdminGetLevels: %v", err)
	}
	scoped := encx.AdminBonus{
		Name: "Бонус уровня", Task: "Найдите", Hint: "Подсказка",
		Answers: []string{"a1", "a2"}, LevelID: levels[0].ID,
		AwardMinutes: 7, DelayMinutes: 3, WorkMinutes: 9,
	}
	wide := encx.AdminBonus{Name: "Бонус игры", Answers: []string{"всюду"}, AwardMinutes: 1}
	for _, bonus := range []encx.AdminBonus{scoped, wide} {
		if err := c.AdminCreateBonus(ctx, mockGameID, 1, bonus); err != nil {
			t.Fatalf("AdminCreateBonus(%q): %v", bonus.Name, err)
		}
	}

	ids, err := c.AdminGetBonusIds(ctx, mockGameID, 1)
	if err != nil {
		t.Fatalf("AdminGetBonusIds: %v", err)
	}
	byName := map[string]*encx.AdminBonus{}
	idByName := map[string]int{}
	for _, id := range ids {
		bonus, err := c.AdminGetBonus(ctx, mockGameID, 1, id)
		if err != nil {
			t.Fatalf("AdminGetBonus(%d): %v", id, err)
		}
		byName[bonus.Name] = bonus
		idByName[bonus.Name] = id
	}

	got := byName[scoped.Name]
	if got == nil {
		t.Fatalf("the level bonus is missing: %v", ids)
	}
	if !reflect.DeepEqual(got.Answers, scoped.Answers) ||
		got.AwardMinutes != 7 || got.DelayMinutes != 3 || got.WorkMinutes != 9 ||
		got.LevelID != levels[0].ID {
		t.Errorf("level bonus = %+v", got)
	}

	got = byName[wide.Name]
	if got == nil {
		t.Fatalf("the game-wide bonus is missing: %v", ids)
	}
	if got.LevelID != 0 {
		t.Errorf("game-wide bonus reports level %d, want 0", got.LevelID)
	}

	// Reading a game-wide bonus and writing it straight back must not pin it to
	// the level that happened to be open.
	if err := c.AdminUpdateBonus(ctx, mockGameID, 1, idByName[wide.Name], *got); err != nil {
		t.Fatalf("AdminUpdateBonus: %v", err)
	}
	after, err := c.AdminGetBonus(ctx, mockGameID, 1, idByName[wide.Name])
	if err != nil {
		t.Fatalf("AdminGetBonus after update: %v", err)
	}
	if after.LevelID != 0 {
		t.Errorf("a read-modify-write pinned the game-wide bonus to level %d", after.LevelID)
	}
}

func TestMockAdminSectorsRoundTrip(t *testing.T) {
	c, ctx := adminClient(t)

	if err := c.AdminClearLevelSectors(ctx, mockGameID, 1); err != nil {
		t.Fatalf("AdminClearLevelSectors: %v", err)
	}
	for _, sector := range []encx.AdminSector{
		{Name: "Сектор А", Answers: []string{"альфа", "бета"}},
		{Name: "Сектор Б", Answers: []string{"гамма"}},
	} {
		if err := c.AdminCreateSector(ctx, mockGameID, 1, sector); err != nil {
			t.Fatalf("AdminCreateSector(%q): %v", sector.Name, err)
		}
	}

	sectors, err := c.AdminGetSectorAnswers(ctx, mockGameID, 1)
	if err != nil {
		t.Fatalf("AdminGetSectorAnswers: %v", err)
	}
	if len(sectors) != 2 {
		t.Fatalf("sectors = %d, want 2", len(sectors))
	}
	if sectors[0].Name != "Сектор А" ||
		!reflect.DeepEqual(sectors[0].Answers, []string{"альфа", "бета"}) {
		t.Errorf("sectors[0] = %+v", sectors[0])
	}

	if err := c.AdminAddSectorAnswers(ctx, mockGameID, 1, sectors[1].ID, []string{"дельта"}); err != nil {
		t.Fatalf("AdminAddSectorAnswers: %v", err)
	}
	// Updating a sector replaces its answers, which is what saving the legacy
	// form did.
	if err := c.AdminUpdateSector(ctx, mockGameID, 1, sectors[0].ID,
		encx.AdminSector{Name: "Сектор А*", Answers: []string{"альфа", "омега"}}); err != nil {
		t.Fatalf("AdminUpdateSector: %v", err)
	}

	sectors, err = c.AdminGetSectorAnswers(ctx, mockGameID, 1)
	if err != nil {
		t.Fatalf("AdminGetSectorAnswers after edits: %v", err)
	}
	if sectors[0].Name != "Сектор А*" ||
		!reflect.DeepEqual(sectors[0].Answers, []string{"альфа", "омега"}) {
		t.Errorf("after update sectors[0] = %+v", sectors[0])
	}
	if !reflect.DeepEqual(sectors[1].Answers, []string{"гамма", "дельта"}) {
		t.Errorf("after adding an answer sectors[1] = %+v", sectors[1])
	}

	refs, err := c.AdminGetSectorRefs(ctx, mockGameID, 1)
	if err != nil || len(refs) != 2 {
		t.Errorf("AdminGetSectorRefs = %+v (err %v)", refs, err)
	}

	if err := c.AdminClearLevelSectors(ctx, mockGameID, 1); err != nil {
		t.Fatalf("AdminClearLevelSectors (again): %v", err)
	}
	if sectors, err = c.AdminGetSectorAnswers(ctx, mockGameID, 1); err != nil || len(sectors) != 0 {
		t.Errorf("sectors after clear = %+v (err %v)", sectors, err)
	}
}

// The autopass award is stored signed. A penalty and a bonus of the same length
// differ only in that sign, and reading one as the other inverts the rule the
// author set.
func TestMockAdminLevelSettingsRoundTrip(t *testing.T) {
	c, ctx := adminClient(t)

	if err := c.AdminUpdateAutopass(ctx, mockGameID, 1, encx.AdminLevelSettings{
		AutopassHours: 1, AutopassMinutes: 30, TimeoutPenalty: true, PenaltyMinutes: 15,
	}); err != nil {
		t.Fatalf("AdminUpdateAutopass: %v", err)
	}
	got, err := c.AdminGetLevelSettings(ctx, mockGameID, 1)
	if err != nil {
		t.Fatalf("AdminGetLevelSettings: %v", err)
	}
	if got.AutopassHours != 1 || got.AutopassMinutes != 30 {
		t.Errorf("autopass = %+v", got)
	}
	if !got.TimeoutPenalty || got.PenaltyMinutes != 15 {
		t.Errorf("timeout penalty = %+v", got)
	}

	// A positive award is a bonus, and a read-modify-write must not erase it.
	if err := c.AdminUpdateAutopass(ctx, mockGameID, 1, encx.AdminLevelSettings{
		AutopassHours: 1, TimeoutPenalty: false, PenaltyMinutes: 15,
	}); err != nil {
		t.Fatalf("AdminUpdateAutopass (bonus): %v", err)
	}
	if got, err = c.AdminGetLevelSettings(ctx, mockGameID, 1); err != nil {
		t.Fatalf("AdminGetLevelSettings (bonus): %v", err)
	}
	if got.TimeoutPenalty {
		t.Error("a positive award was read back as a penalty")
	}
	if got.PenaltyMinutes != 15 {
		t.Errorf("the positive award was lost: %+v", got)
	}

	if err := c.AdminUpdateAnswerBlock(ctx, mockGameID, 1, encx.AdminLevelSettings{
		AttemptsNumber: 3, AttemptsPeriodMinutes: 1, ApplyForPlayer: 1,
	}); err != nil {
		t.Fatalf("AdminUpdateAnswerBlock: %v", err)
	}
	if got, err = c.AdminGetLevelSettings(ctx, mockGameID, 1); err != nil {
		t.Fatalf("AdminGetLevelSettings (block): %v", err)
	}
	if got.AttemptsNumber != 3 || got.AttemptsPeriodMinutes != 1 || got.ApplyForPlayer != 1 {
		t.Errorf("answer block = %+v", got)
	}

	// required_sectors_count only means something under the counting condition.
	if err := c.AdminUpdateSectorCompletion(ctx, mockGameID, 1, 2); err != nil {
		t.Fatalf("AdminUpdateSectorCompletion(2): %v", err)
	}
	if got, err = c.AdminGetLevelSettings(ctx, mockGameID, 1); err != nil || got.RequiredSectorsCount != 2 {
		t.Errorf("required sectors = %+v (err %v)", got, err)
	}
	if err := c.AdminUpdateSectorCompletion(ctx, mockGameID, 1, 0); err != nil {
		t.Fatalf("AdminUpdateSectorCompletion(0): %v", err)
	}
	if got, err = c.AdminGetLevelSettings(ctx, mockGameID, 1); err != nil || got.RequiredSectorsCount != 0 {
		t.Errorf("required sectors after switching to all = %+v (err %v)", got, err)
	}
}

// Reading the game editor and writing it straight back must change nothing.
// The prize used to be multiplied by a hundred on the way out.
func TestMockAdminGameInfoRoundTrip(t *testing.T) {
	c, ctx := adminClient(t)

	if err := c.AdminUpdateGameInfo(ctx, mockGameID, encx.AdminGameInfo{
		Title: "Изменённая игра", Description: "Описание",
		StartDateTime: "2026-09-10T15:00:00Z",
		MaxPlayers:    "42", MaxTeamPlayers: "7", AuthorComplexity: "3",
		IsModerated: true, ShowFinishPlace: true,
	}); err != nil {
		t.Fatalf("AdminUpdateGameInfo: %v", err)
	}
	info, err := c.AdminGetGameInfo(ctx, mockGameID)
	if err != nil {
		t.Fatalf("AdminGetGameInfo: %v", err)
	}
	if info.Title != "Изменённая игра" || info.Description != "Описание" ||
		info.MaxPlayers != "42" || info.MaxTeamPlayers != "7" ||
		!info.IsModerated || !info.ShowFinishPlace {
		t.Errorf("info = %+v", info)
	}
	if info.StartDateTime != "2026-09-10T15:00:00Z" {
		t.Errorf("StartDateTime = %q, want the start that was written", info.StartDateTime)
	}
	// The legacy dropdown is ten times the REST afc, and the engine stores afc
	// to a tenth — so a whole number survives the trip.
	if info.AuthorComplexity != "3" {
		t.Errorf("AuthorComplexity = %q, want %q", info.AuthorComplexity, "3")
	}

	if err := c.AdminUpdateGameInfo(ctx, mockGameID, *info); err != nil {
		t.Fatalf("writing the same info back: %v", err)
	}
	again, err := c.AdminGetGameInfo(ctx, mockGameID)
	if err != nil {
		t.Fatalf("AdminGetGameInfo again: %v", err)
	}
	if !reflect.DeepEqual(info, again) {
		t.Errorf("a read-modify-write changed the game:\n before %+v\n after  %+v", info, again)
	}

	// An empty field means "leave it alone".
	if err := c.AdminUpdateGameInfo(ctx, mockGameID,
		encx.AdminGameInfo{Title: "Только заголовок"}); err != nil {
		t.Fatalf("partial update: %v", err)
	}
	if again, err = c.AdminGetGameInfo(ctx, mockGameID); err != nil {
		t.Fatalf("AdminGetGameInfo after partial: %v", err)
	}
	if again.Description != "Описание" || again.MaxPlayers != "42" {
		t.Errorf("a partial update cleared other fields: %+v", again)
	}

	// The engine accepts afc in 0..1, i.e. 0..10 in the legacy spelling.
	if err := c.AdminUpdateGameInfo(ctx, mockGameID,
		encx.AdminGameInfo{AuthorComplexity: "15"}); err == nil {
		t.Error("AdminUpdateGameInfo accepted an author complexity of 15")
	}
}

func TestMockAdminMessagesRoundTrip(t *testing.T) {
	c, ctx := adminClient(t)

	levels, err := c.AdminGetLevels(ctx, mockGameID)
	if err != nil {
		t.Fatalf("AdminGetLevels: %v", err)
	}
	// ShowOnLevelsMode left unset means "all levels" on the legacy engine.
	if err := c.AdminCreateMessage(ctx, mockGameID, levels[0].ID,
		encx.AdminGameMessage{Text: "Всем уровням", ReplaceNlToBr: true}); err != nil {
		t.Fatalf("AdminCreateMessage: %v", err)
	}
	if err := c.AdminCreateMessage(ctx, mockGameID, levels[0].ID, encx.AdminGameMessage{
		Text: "Второму уровню", ShowOnLevelsMode: 2, LevelIDs: []int{levels[1].ID},
	}); err != nil {
		t.Fatalf("AdminCreateMessage (single level): %v", err)
	}

	ids, err := c.AdminGetMessageIds(ctx, mockGameID, 1)
	if err != nil {
		t.Fatalf("AdminGetMessageIds: %v", err)
	}
	found := false
	for _, id := range ids {
		message, err := c.AdminGetMessage(ctx, mockGameID, 1, id)
		if err != nil {
			t.Fatalf("AdminGetMessage(%d): %v", id, err)
		}
		if message.Text != "Всем уровням" {
			continue
		}
		found = true
		if message.ShowOnLevelsMode != 1 {
			t.Errorf("all-levels message mode = %d, want 1", message.ShowOnLevelsMode)
		}
		if !message.ReplaceNlToBr {
			t.Error("replace_nl_to_br did not survive")
		}
	}
	if !found {
		t.Error("the all-levels message is not listed on level 1")
	}

	level2, err := c.AdminGetMessageIds(ctx, mockGameID, 2)
	if err != nil {
		t.Fatalf("AdminGetMessageIds(2): %v", err)
	}
	scoped := false
	for _, id := range level2 {
		message, err := c.AdminGetMessage(ctx, mockGameID, 2, id)
		if err != nil {
			t.Fatalf("AdminGetMessage(2, %d): %v", id, err)
		}
		if message.Text == "Второму уровню" {
			scoped = true
			if message.ShowOnLevelsMode != 2 {
				t.Errorf("single-level message mode = %d, want 2", message.ShowOnLevelsMode)
			}
		}
	}
	if !scoped {
		t.Error("the message addressed to level 2 is not listed there")
	}
}

// AdminGetGames walks every page; the mock publishes the pager fields so the
// walk terminates the way it does against the real listing.
func TestMockAdminGetGames(t *testing.T) {
	c, ctx := adminClient(t)

	games, err := c.AdminGetGames(ctx)
	if err != nil {
		t.Fatalf("AdminGetGames: %v", err)
	}
	if len(games) != 1 || games[0].ID != mockGameID {
		t.Fatalf("games = %+v, want the mock game", games)
	}
	if games[0].Title == "" || games[0].Number == 0 {
		t.Errorf("games[0] = %+v, want a title and a number", games[0])
	}
}

// A game that has not started has nothing to award or rate, and the engine says
// so rather than answering success over an untouched game.
func TestMockAdminLifecycleRefusesWhatIsUnavailable(t *testing.T) {
	c, ctx := adminClient(t)

	for name, call := range map[string]func() error{
		"AdminAwardPoints": func() error { return c.AdminAwardPoints(ctx, mockGameID) },
		"AdminEndRatings":  func() error { return c.AdminEndRatings(ctx, mockGameID) },
		"AdminCalculateIK": func() error { return c.AdminCalculateIK(ctx, mockGameID) },
	} {
		if err := call(); err == nil {
			t.Errorf("%s reported success on a game that has not started", name)
		}
	}

	// The status codes are not published, so these refuse instead of guessing.
	for name, call := range map[string]func() error{
		"AdminDeliverGame":    func() error { return c.AdminDeliverGame(ctx, mockGameID) },
		"AdminNotDeliverGame": func() error { return c.AdminNotDeliverGame(ctx, mockGameID) },
	} {
		err := call()
		if err == nil {
			t.Errorf("%s went ahead without a documented status_id", name)
			continue
		}
		if !strings.Contains(err.Error(), "status_id") {
			t.Errorf("%s: error = %v", name, err)
		}
	}
}

// The corrections route refuses a game that has not started, where the legacy
// page rendered an empty table. The client turns that into an empty list only
// for a game it administers.
func TestMockAdminCorrectionsOnAGameThatHasNotStarted(t *testing.T) {
	c, ctx := adminClient(t)

	corrections, err := c.AdminGetCorrections(ctx, mockGameID)
	if err != nil {
		t.Fatalf("AdminGetCorrections: %v", err)
	}
	if len(corrections) != 0 {
		t.Errorf("corrections = %+v, want none", corrections)
	}

	if _, err := c.AdminGetActionMonitor(ctx, mockGameID); err != nil {
		t.Errorf("AdminGetActionMonitor: %v", err)
	}
	if _, err := c.AdminGetTeams(ctx, mockGameID, 1); err != nil {
		t.Errorf("AdminGetTeams: %v", err)
	}
}

// ErrSectorStarted is exported and callers branch on it, so the new engine has
// to be able to produce it: a refusal the sector survives is that condition.
func TestMockAdminSectorStartedIsReported(t *testing.T) {
	s := newMockServer(t)
	c, ctx := adminClientFor(t, s), context.Background()

	if err := c.AdminCreateSector(ctx, mockGameID, 1,
		encx.AdminSector{Name: "Начатый сектор", Answers: []string{"код"}}); err != nil {
		t.Fatalf("AdminCreateSector: %v", err)
	}

	// A started game refuses to drop a sector, and the sector is still there
	// afterwards — which is exactly what the legacy client checked for.
	s.admin.refuseSectorDeletes = true
	err := c.AdminClearLevelSectors(ctx, mockGameID, 1)
	if err == nil {
		t.Fatal("AdminClearLevelSectors reported success over a level it could not clear")
	}
	if !errors.Is(err, encx.ErrSectorStarted) {
		t.Errorf("error = %v, want ErrSectorStarted", err)
	}

	// A transient failure is not an unremovable sector: a caller that branches on
	// the sentinel must not be sent away from a retry that would work.
	s.admin.refuseSectorDeletes = false
	if err := c.AdminClearLevelSectors(ctx, mockGameID, 1); err != nil {
		t.Errorf("AdminClearLevelSectors after the refusal lifted: %v", err)
	}
}
