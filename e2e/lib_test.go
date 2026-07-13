//go:build e2e

package e2e

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/skrashevich/encx-cli/encx"
)

// --- Authentication ---

// TestLibLoginInvalidCredentials deliberately uses a nonexistent account —
// failing a login on the real account would count towards the domain's
// anti-brute-force limit and block subsequent runs.
func TestLibLoginInvalidCredentials(t *testing.T) {
	client := newClient()
	resp, err := client.Login(t.Context(), "e2e-no-such-user-xxx", "definitely-wrong-password-e2e")
	if err != nil {
		skipOnAntiSpam(t, err)
		t.Fatalf("Login request failed: %v", err)
	}
	if resp.Error == 0 {
		t.Fatal("Expected non-zero Error for invalid credentials, got 0")
	}
	t.Logf("Got expected error %d: %s", resp.Error, encx.LoginErrorText(resp.Error))
}

func TestLibProfile(t *testing.T) {
	client := adminClient(t)
	profile, err := client.GetProfile(t.Context())
	if err != nil {
		skipOnAntiSpam(t, err)
		t.Fatalf("GetProfile failed: %v", err)
	}
	if !strings.EqualFold(profile.Login, e2eLogin()) {
		t.Errorf("Expected login %q, got %q", e2eLogin(), profile.Login)
	}
	if profile.ID <= 0 {
		t.Errorf("Expected positive user ID, got %d", profile.ID)
	}
	t.Logf("Profile: id=%d login=%s team=%s", profile.ID, profile.Login, profile.Team)
}

func TestLibCookieRoundtrip(t *testing.T) {
	src := adminClient(t)
	data, err := src.ExportCookies()
	if err != nil {
		t.Fatalf("ExportCookies failed: %v", err)
	}

	restored := newClient()
	if err := restored.ImportCookies(data); err != nil {
		t.Fatalf("ImportCookies failed: %v", err)
	}
	profile, err := restored.GetProfile(t.Context())
	if err != nil {
		skipOnAntiSpam(t, err)
		t.Fatalf("GetProfile with imported cookies failed: %v", err)
	}
	if !strings.EqualFold(profile.Login, e2eLogin()) {
		t.Errorf("Expected login %q via restored session, got %q", e2eLogin(), profile.Login)
	}
}

// --- Discovery ---

func TestLibGetDomainGames(t *testing.T) {
	client := newClient()
	games, err := client.GetDomainGames(t.Context())
	if err != nil {
		skipOnAntiSpam(t, err)
		t.Fatalf("GetDomainGames failed: %v", err)
	}
	for _, g := range games {
		t.Logf("Game: %d - %s", g.GameId, g.Title)
	}
}

func TestLibGetGameList(t *testing.T) {
	client := adminClient(t)
	list, err := client.GetGameList(t.Context())
	if err != nil {
		skipOnAntiSpam(t, err)
		t.Fatalf("GetGameList failed: %v", err)
	}
	t.Logf("Active games: %d, Coming games: %d", len(list.ActiveGames), len(list.ComingGames))
}

func TestLibAdminGames(t *testing.T) {
	client := adminClient(t)
	games, err := client.AdminGetGames(t.Context())
	if err != nil {
		skipOnAntiSpam(t, err)
		t.Fatalf("AdminGetGames failed: %v", err)
	}
	found := slices.ContainsFunc(games, func(g encx.AdminGame) bool { return g.ID == e2eGameID() })
	if !found {
		t.Errorf("Expected sandbox game %d in admin games list, got %+v", e2eGameID(), games)
	}
}

// --- Admin lifecycle on a fresh level ---

// TestLibAdminLevelLifecycle creates a new level at the end of the sandbox
// game, fills it with content (comment, task, sector, hint, bonus), verifies
// everything reads back, then deletes the content and the level itself.
func TestLibAdminLevelLifecycle(t *testing.T) {
	client := adminClient(t)
	ctx := t.Context()
	gameID := e2eGameID()

	baseline, err := client.AdminGetLevels(ctx, gameID)
	if err != nil {
		skipOnAntiSpam(t, err)
		t.Fatalf("AdminGetLevels failed: %v", err)
	}

	if err := client.AdminCreateLevels(ctx, gameID, 1); err != nil {
		t.Fatalf("AdminCreateLevels failed: %v", err)
	}
	levels, err := client.AdminGetLevels(ctx, gameID)
	if err != nil {
		t.Fatalf("AdminGetLevels after create failed: %v", err)
	}
	if len(levels) != len(baseline)+1 {
		t.Fatalf("Expected %d levels after create, got %d", len(baseline)+1, len(levels))
	}
	level := levels[len(levels)-1]
	t.Logf("Created level %d (id=%d)", level.Number, level.ID)

	// Delete the level even if any assertion below fails.
	defer func() {
		if err := client.AdminDeleteLevel(context.WithoutCancel(ctx), gameID, level.Number); err != nil {
			t.Errorf("Cleanup: AdminDeleteLevel(%d) failed: %v", level.Number, err)
			return
		}
		after, err := client.AdminGetLevels(context.WithoutCancel(ctx), gameID)
		if err != nil {
			t.Errorf("Cleanup: AdminGetLevels after delete failed: %v", err)
			return
		}
		if len(after) != len(baseline) {
			t.Errorf("Cleanup: expected %d levels after delete, got %d", len(baseline), len(after))
		}
	}()

	// Comment (level name in the admin panel).
	levelName := uniqueName("level")
	if err := client.AdminUpdateComment(ctx, gameID, level.Number, levelName, "e2e comment"); err != nil {
		t.Fatalf("AdminUpdateComment failed: %v", err)
	}
	gotName, gotComment, err := client.AdminGetComment(ctx, gameID, level.Number)
	if err != nil {
		t.Fatalf("AdminGetComment failed: %v", err)
	}
	if gotName != levelName {
		t.Errorf("Expected level name %q, got %q", levelName, gotName)
	}
	if gotComment != "e2e comment" {
		t.Errorf("Expected comment %q, got %q", "e2e comment", gotComment)
	}

	// Task.
	taskText := uniqueName("task")
	if err := client.AdminCreateTask(ctx, gameID, level.Number, encx.AdminTask{Text: taskText}); err != nil {
		t.Fatalf("AdminCreateTask failed: %v", err)
	}
	taskIDs, err := client.AdminGetTaskIds(ctx, gameID, level.Number)
	if err != nil {
		t.Fatalf("AdminGetTaskIds failed: %v", err)
	}
	if len(taskIDs) != 1 {
		t.Fatalf("Expected 1 task, got %d", len(taskIDs))
	}
	task, err := client.AdminGetTask(ctx, gameID, level.Number, taskIDs[0])
	if err != nil {
		t.Fatalf("AdminGetTask failed: %v", err)
	}
	if !strings.Contains(task.Text, taskText) {
		t.Errorf("Expected task text to contain %q, got %q", taskText, task.Text)
	}

	// Sector with two answers.
	sectorName := uniqueName("sector")
	sector := encx.AdminSector{Name: sectorName, Answers: []string{"e2eanswer1", "e2eanswer2"}}
	if err := client.AdminCreateSector(ctx, gameID, level.Number, sector); err != nil {
		t.Fatalf("AdminCreateSector failed: %v", err)
	}
	sectors, err := client.AdminGetSectorAnswers(ctx, gameID, level.Number)
	if err != nil {
		t.Fatalf("AdminGetSectorAnswers failed: %v", err)
	}
	si := slices.IndexFunc(sectors, func(s encx.AdminSector) bool { return s.Name == sectorName })
	if si < 0 {
		t.Fatalf("Expected sector %q in list, got %+v", sectorName, sectors)
	}
	for _, want := range sector.Answers {
		if !slices.Contains(sectors[si].Answers, want) {
			t.Errorf("Expected sector answers to contain %q, got %v", want, sectors[si].Answers)
		}
	}

	// Hint.
	hintText := uniqueName("hint")
	if err := client.AdminCreateHint(ctx, gameID, level.Number, encx.AdminHint{Text: hintText, Minutes: 5}); err != nil {
		t.Fatalf("AdminCreateHint failed: %v", err)
	}
	hintIDs, err := client.AdminGetHintIds(ctx, gameID, level.Number)
	if err != nil {
		t.Fatalf("AdminGetHintIds failed: %v", err)
	}
	if len(hintIDs) != 1 {
		t.Fatalf("Expected 1 hint, got %d", len(hintIDs))
	}
	hint, err := client.AdminGetHint(ctx, gameID, level.Number, hintIDs[0])
	if err != nil {
		t.Fatalf("AdminGetHint failed: %v", err)
	}
	if !strings.Contains(hint.Text, hintText) {
		t.Errorf("Expected hint text to contain %q, got %q", hintText, hint.Text)
	}

	// Bonus.
	bonusName := uniqueName("bonus")
	bonus := encx.AdminBonus{Name: bonusName, LevelID: level.ID, Answers: []string{"e2ebonus"}}
	if err := client.AdminCreateBonus(ctx, gameID, level.Number, bonus); err != nil {
		t.Fatalf("AdminCreateBonus failed: %v", err)
	}
	bonusIDs, err := client.AdminGetBonusIds(ctx, gameID, level.Number)
	if err != nil {
		t.Fatalf("AdminGetBonusIds failed: %v", err)
	}
	if len(bonusIDs) != 1 {
		t.Fatalf("Expected 1 bonus, got %d", len(bonusIDs))
	}

	// Delete the content in reverse order and verify it is gone.
	if err := client.AdminDeleteBonus(ctx, gameID, level.Number, bonusIDs[0]); err != nil {
		t.Fatalf("AdminDeleteBonus failed: %v", err)
	}
	if ids, err := client.AdminGetBonusIds(ctx, gameID, level.Number); err != nil {
		t.Fatalf("AdminGetBonusIds after delete failed: %v", err)
	} else if len(ids) != 0 {
		t.Errorf("Expected 0 bonuses after delete, got %d", len(ids))
	}

	if err := client.AdminDeleteHint(ctx, gameID, level.Number, hintIDs[0]); err != nil {
		t.Fatalf("AdminDeleteHint failed: %v", err)
	}
	if ids, err := client.AdminGetHintIds(ctx, gameID, level.Number); err != nil {
		t.Fatalf("AdminGetHintIds after delete failed: %v", err)
	} else if len(ids) != 0 {
		t.Errorf("Expected 0 hints after delete, got %d", len(ids))
	}

	if sectors[si].ID > 0 {
		if err := client.AdminDeleteSector(ctx, gameID, level.Number, sectors[si].ID); err != nil {
			t.Fatalf("AdminDeleteSector failed: %v", err)
		}
	}
}

// --- Player-side scenarios ---

func TestLibPlayerGameModel(t *testing.T) {
	client := playerClient(t)
	model := ensureActiveGame(t, client)

	if model.GameId != e2eGameID() {
		t.Errorf("Expected GameId %d, got %d", e2eGameID(), model.GameId)
	}
	if !strings.EqualFold(model.Login, e2eLogin()) {
		t.Errorf("Expected Login %q, got %q", e2eLogin(), model.Login)
	}
	if len(model.Levels) == 0 {
		t.Error("Expected at least one level in the game")
	}
	if model.Level == nil {
		t.Fatal("Expected an active level")
	}
	t.Logf("Game %q: level %d/%d %q", model.GameTitle, model.Level.Number, len(model.Levels), model.Level.Name)
}

func TestLibPlayerSendWrongCode(t *testing.T) {
	client := playerClient(t)
	model := ensureActiveGame(t, client)
	if model.Level == nil {
		t.Fatal("Expected an active level")
	}
	if !model.Level.CanSubmitLevelAnswer() {
		t.Skip("Current level does not accept level answers")
	}

	code := uniqueName("wrong-code")
	result, err := client.SendCode(t.Context(), e2eGameID(), model.Level.LevelId, model.Level.Number, code)
	if err != nil {
		skipOnAntiSpam(t, err)
		t.Fatalf("SendCode failed: %v", err)
	}
	if result.Level == nil {
		t.Fatal("Expected level in SendCode response")
	}
	if result.Level.Number != model.Level.Number {
		t.Errorf("Wrong code must not advance the level: was %d, now %d", model.Level.Number, result.Level.Number)
	}
	for _, a := range result.Level.MixedActions {
		if a.Answer == code && a.IsCorrect {
			t.Errorf("Wrong code %q was accepted as correct", code)
		}
	}
}

// TestLibPlayerSectorFlow creates two sectors on the current level, verifies
// the player sees them in the game model, checks that a wrong code closes
// nothing, and deletes the sectors again.
//
// Deliberately NOT covered: closing a sector with a correct code. The engine
// refuses to delete a sector once participants have answered it ("started by
// participants"), so every run would leave an immortal sector behind.
// Sending a correct code end-to-end is covered by TestLibPlayerBonusFlow,
// which goes through the same LevelAction.Answer submission path.
func TestLibPlayerSectorFlow(t *testing.T) {
	admin := adminClient(t)
	player := playerClient(t)
	ctx := t.Context()
	gameID := e2eGameID()

	model := ensureActiveGame(t, player)
	if model.Level == nil {
		t.Fatal("Expected an active level")
	}
	levelNum := model.Level.Number
	levelID := model.Level.LevelId

	name1 := uniqueName("sec1")
	name2 := uniqueName("sec2")
	answer1 := strings.ReplaceAll(uniqueName("secans1"), "-", "")
	answer2 := strings.ReplaceAll(uniqueName("secans2"), "-", "")
	if err := admin.AdminCreateSector(ctx, gameID, levelNum, encx.AdminSector{Name: name1, Answers: []string{answer1}}); err != nil {
		t.Fatalf("AdminCreateSector(%q) failed: %v", name1, err)
	}
	defer func() {
		cctx := context.WithoutCancel(ctx)
		sectors, err := admin.AdminGetSectorAnswers(cctx, gameID, levelNum)
		if err != nil {
			t.Errorf("Cleanup: AdminGetSectorAnswers failed: %v", err)
			return
		}
		for _, s := range sectors {
			if (s.Name == name1 || s.Name == name2) && s.ID > 0 {
				if err := admin.AdminDeleteSector(cctx, gameID, levelNum, s.ID); err != nil {
					t.Errorf("Cleanup: AdminDeleteSector(%d %q) failed: %v", s.ID, s.Name, err)
				}
			}
		}
	}()
	if err := admin.AdminCreateSector(ctx, gameID, levelNum, encx.AdminSector{Name: name2, Answers: []string{answer2}}); err != nil {
		t.Fatalf("AdminCreateSector(%q) failed: %v", name2, err)
	}

	// The player must see both sectors in the game model, still open.
	model, err := player.GetGameModel(ctx, gameID)
	if err != nil {
		t.Fatalf("GetGameModel after sector creation failed: %v", err)
	}
	if model.Level == nil || model.Level.Number != levelNum {
		t.Fatalf("Active level changed unexpectedly")
	}
	passedBefore := model.Level.PassedSectorsCount
	for _, name := range []string{name1, name2} {
		found := false
		for _, s := range model.Level.Sectors {
			if s.Name == name {
				found = true
				if s.IsAnswered {
					t.Errorf("Freshly created sector %q must not be answered", name)
				}
			}
		}
		if !found {
			t.Errorf("Expected sector %q in the player game model, got %d sectors", name, len(model.Level.Sectors))
		}
	}

	// A wrong code must close nothing and keep the level in place.
	result, err := player.SendCode(ctx, gameID, levelID, levelNum, strings.ReplaceAll(uniqueName("secwrong"), "-", ""))
	if err != nil {
		t.Fatalf("SendCode with wrong answer failed: %v", err)
	}
	if result.Level == nil || result.Level.Number != levelNum {
		t.Fatalf("Wrong code must not advance the level")
	}
	if result.Level.PassedSectorsCount != passedBefore {
		t.Errorf("Expected %d closed sectors after wrong code, got %d", passedBefore, result.Level.PassedSectorsCount)
	}
	for _, s := range result.Level.Sectors {
		if (s.Name == name1 || s.Name == name2) && s.IsAnswered {
			t.Errorf("Sector %q must remain open after a wrong code", s.Name)
		}
	}
}

// TestLibPlayerBonusFlow covers sending a correct bonus code. The bonus is
// created with a zero time award, so team results are unaffected.
func TestLibPlayerBonusFlow(t *testing.T) {
	client := adminClient(t)
	player := playerClient(t)
	ctx := t.Context()
	gameID := e2eGameID()

	model := ensureActiveGame(t, player)
	if model.Level == nil {
		t.Fatal("Expected an active level")
	}
	levelNum := model.Level.Number
	levelID := model.Level.LevelId

	before, err := client.AdminGetBonusIds(ctx, gameID, levelNum)
	if err != nil {
		t.Fatalf("AdminGetBonusIds failed: %v", err)
	}

	bonusName := uniqueName("bonus")
	bonusAnswer := strings.ReplaceAll(uniqueName("bonusans"), "-", "")
	bonus := encx.AdminBonus{Name: bonusName, LevelID: levelID, Answers: []string{bonusAnswer}}
	if err := client.AdminCreateBonus(ctx, gameID, levelNum, bonus); err != nil {
		t.Fatalf("AdminCreateBonus failed: %v", err)
	}
	after, err := client.AdminGetBonusIds(ctx, gameID, levelNum)
	if err != nil {
		t.Fatalf("AdminGetBonusIds after create failed: %v", err)
	}
	var created []int
	for _, id := range after {
		if !slices.Contains(before, id) {
			created = append(created, id)
		}
	}
	if len(created) != 1 {
		t.Fatalf("Expected exactly 1 new bonus id, got %v", created)
	}
	defer func() {
		if err := client.AdminDeleteBonus(context.WithoutCancel(ctx), gameID, levelNum, created[0]); err != nil {
			t.Errorf("Cleanup: AdminDeleteBonus(%d) failed: %v", created[0], err)
		}
	}()

	// On a level without an answer block rule the engine only accepts codes
	// through LevelAction.Answer (SendCode) and silently drops
	// BonusAction.Answer; the dedicated bonus action exists for blocked
	// levels only.
	send := player.SendCode
	if model.Level.HasAnswerBlockRule {
		send = player.SendBonusCode
	}
	result, err := send(ctx, gameID, levelID, levelNum, bonusAnswer)
	if err != nil {
		t.Fatalf("Sending bonus answer failed: %v", err)
	}
	if result.Level == nil {
		t.Fatal("Expected level in SendBonusCode response")
	}
	answered := false
	for _, b := range result.Level.Bonuses {
		if b.Name == bonusName && b.IsAnswered {
			answered = true
		}
	}
	if !answered {
		t.Errorf("Expected bonus %q to be answered by code %q", bonusName, bonusAnswer)
	}
}

// TestLibPlayerPenaltyHintFlow creates a penalty hint, verifies the player
// sees it (locked) in the game model, and deletes it again.
//
// Deliberately NOT covered: taking the hint with GetPenaltyHint. Once a
// player takes a penalty hint the engine refuses to ever delete it ("не
// может быть удалена, так как по ней было начислено штрафное время"), so
// every run would leave an immortal hint on the level.
func TestLibPlayerPenaltyHintFlow(t *testing.T) {
	client := adminClient(t)
	player := playerClient(t)
	ctx := t.Context()
	gameID := e2eGameID()

	model := ensureActiveGame(t, player)
	if model.Level == nil {
		t.Fatal("Expected an active level")
	}
	levelNum := model.Level.Number

	before, err := client.AdminGetHintIds(ctx, gameID, levelNum)
	if err != nil {
		t.Fatalf("AdminGetHintIds failed: %v", err)
	}

	// The engine rejects penalty hints with a zero penalty, so use a token
	// 10-second one; taking it once in the sandbox game costs nothing real.
	comment := uniqueName("penalty-comment")
	text := uniqueName("penalty-text")
	hint := encx.AdminHint{Text: text, IsPenalty: true, PenaltySeconds: 10, PenaltyComment: comment}
	if err := client.AdminCreateHint(ctx, gameID, levelNum, hint); err != nil {
		t.Fatalf("AdminCreateHint(penalty) failed: %v", err)
	}
	after, err := client.AdminGetHintIds(ctx, gameID, levelNum)
	if err != nil {
		t.Fatalf("AdminGetHintIds after create failed: %v", err)
	}
	var created []int
	for _, id := range after {
		if !slices.Contains(before, id) {
			created = append(created, id)
		}
	}
	if len(created) != 1 {
		t.Fatalf("Expected exactly 1 new hint id, got %v", created)
	}
	hintID := created[0]

	// The player sees the penalty hint via the game model; its player-side id
	// (HelpId) matches the admin-side id and the comment we set. The text
	// must stay hidden until the hint is taken.
	model, err = player.GetGameModel(ctx, gameID)
	if err != nil {
		t.Fatalf("GetGameModel after hint creation failed: %v", err)
	}
	if model.Level == nil {
		t.Fatal("Expected an active level")
	}
	found := false
	for _, h := range model.Level.PenaltyHelps {
		if h.HelpId != hintID {
			continue
		}
		found = true
		if h.PenaltyComment == nil || !strings.Contains(*h.PenaltyComment, comment) {
			t.Errorf("Expected penalty hint %d to carry comment %q", hintID, comment)
		}
		if h.Penalty != 10 {
			t.Errorf("Expected 10s penalty, got %d", h.Penalty)
		}
		if h.HelpText != nil && strings.Contains(*h.HelpText, text) {
			t.Errorf("Penalty hint text must be hidden before the hint is taken")
		}
	}
	if !found {
		t.Fatalf("Penalty hint %d not visible in game model", hintID)
	}

	// An untaken penalty hint deletes cleanly; verify it is gone on both sides.
	if err := client.AdminDeleteHint(ctx, gameID, levelNum, hintID); err != nil {
		t.Fatalf("AdminDeleteHint(%d) failed: %v", hintID, err)
	}
	ids, err := client.AdminGetHintIds(ctx, gameID, levelNum)
	if err != nil {
		t.Fatalf("AdminGetHintIds after delete failed: %v", err)
	}
	if slices.Contains(ids, hintID) {
		t.Errorf("Expected hint %d to be deleted, still listed in %v", hintID, ids)
	}
}

func TestLibGameStatistics(t *testing.T) {
	client := adminClient(t)
	stats, err := client.GetGameStatistics(t.Context(), e2eGameID())
	if err != nil {
		skipOnAntiSpam(t, err)
		t.Fatalf("GetGameStatistics failed: %v", err)
	}
	if stats.Game == nil {
		t.Fatal("Expected Game in statistics response")
	}
	if stats.Game.GameID != e2eGameID() {
		t.Errorf("Expected GameID %d, got %d", e2eGameID(), stats.Game.GameID)
	}
	t.Logf("Statistics: %d levels, %d stat groups", len(stats.Levels), len(stats.StatItems))
}
