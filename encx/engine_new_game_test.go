package encx

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"testing"

	"github.com/skrashevich/encx-cli/encx/enapi"
)

func loadEngineStateFixture(t *testing.T) *enapi.EngineState {
	t.Helper()
	data, err := os.ReadFile("testdata/engine_state.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var state enapi.EngineState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	return &state
}

func TestGameModelFromEngineStateMapsHeader(t *testing.T) {
	model := gameModelFromEngineState(loadEngineStateFixture(t))

	if model.GameId != 82448 || model.GameNumber != 12 {
		t.Errorf("game = %d/%d, want 82448/12", model.GameId, model.GameNumber)
	}
	if model.GameTitle != "Тестовая игра" {
		t.Errorf("GameTitle = %q", model.GameTitle)
	}
	if model.GameTypeId != GameTypeTeam || model.GameZoneId != ZoneQuest {
		t.Errorf("type/zone = %d/%d", model.GameTypeId, model.GameZoneId)
	}
	if model.LevelSequence != SequenceAssault {
		t.Errorf("LevelSequence = %d, want %d", model.LevelSequence, SequenceAssault)
	}
	if model.UserId != 501 || model.TeamId != 77 || model.Login != "svk" || model.TeamName != "Команда" {
		t.Errorf("player = %d/%d/%q/%q", model.UserId, model.TeamId, model.Login, model.TeamName)
	}
	if !model.IsCaptain {
		t.Error("IsCaptain = false, want true")
	}
	if model.GameDateTimeStart != "2026-08-31 20:00:00" {
		t.Errorf("GameDateTimeStart = %q", model.GameDateTimeStart)
	}
	if event, ok := model.Event.(int); !ok || event != EventGameNormal {
		t.Errorf("Event = %v, want %d", model.Event, EventGameNormal)
	}
}

// TestGameStartFromEngineNormalizesUnixSeconds pins a difference a live run on
// demo.en.cx exposed: the new engine sends GameDateTimeStart as Unix seconds in
// a string, where the legacy engine sent a formatted time that callers display
// verbatim.
func TestGameStartFromEngineNormalizesUnixSeconds(t *testing.T) {
	if got := gameStartFromEngine("1501015980"); got != "2017-07-25 20:53:00" {
		t.Errorf("gameStartFromEngine(unix) = %q, want 2017-07-25 20:53:00", got)
	}
	// Anything that is not a bare timestamp is passed through untouched.
	for _, value := range []string{"2026-08-31 20:00:00", "", "не дата"} {
		if got := gameStartFromEngine(value); got != value {
			t.Errorf("gameStartFromEngine(%q) = %q, want it unchanged", value, got)
		}
	}
}

func TestGameModelFromEngineStateMapsLevelStrip(t *testing.T) {
	model := gameModelFromEngineState(loadEngineStateFixture(t))

	if len(model.Levels) != 2 {
		t.Fatalf("Levels = %d, want 2", len(model.Levels))
	}
	first, second := model.Levels[0], model.Levels[1]
	if first.LevelId != 811 || first.LevelNumber != 1 || first.LevelName != "Первый" || !first.IsPassed {
		t.Errorf("Levels[0] = %+v", first)
	}
	if second.LevelId != 812 || !second.Dismissed || second.IsPassed {
		t.Errorf("Levels[1] = %+v", second)
	}
}

func TestGameModelFromEngineStateMapsCurrentLevel(t *testing.T) {
	level := gameModelFromEngineState(loadEngineStateFixture(t)).Level
	if level == nil {
		t.Fatal("Level is nil")
	}

	if level.LevelId != 812 || level.Number != 2 || level.Name != "Второй" {
		t.Errorf("level identity = %d/%d/%q", level.LevelId, level.Number, level.Name)
	}
	if level.Timeout != 3600 || level.TimeoutSecondsRemain != 1800 || level.TimeoutAward != 120 {
		t.Errorf("timeout = %d/%d/%d", level.Timeout, level.TimeoutSecondsRemain, level.TimeoutAward)
	}
	if !level.HasAnswerBlockRule || level.BlockDuration != 45 || level.BlockTargetId != 2 {
		t.Errorf("answer block = %v/%d/%d", level.HasAnswerBlockRule, level.BlockDuration, level.BlockTargetId)
	}
	if level.AttemtsNumber != 5 || level.AttemtsPeriod != 300 {
		t.Errorf("attempts = %d/%d", level.AttemtsNumber, level.AttemtsPeriod)
	}
	if level.RequiredSectorsCount != 3 || level.PassedSectorsCount != 1 ||
		level.PassedBonusesCount != 2 || level.SectorsLeftToClose != 2 {
		t.Errorf("sector counters = %d/%d/%d/%d", level.RequiredSectorsCount,
			level.PassedSectorsCount, level.PassedBonusesCount, level.SectorsLeftToClose)
	}
	if level.StartTime == nil || level.StartTime.Timestamp != 1787998200 {
		t.Errorf("StartTime = %+v, want unix 1787998200", level.StartTime)
	}
}

func TestGameModelFromEngineStateMapsCollections(t *testing.T) {
	level := gameModelFromEngineState(loadEngineStateFixture(t)).Level

	if len(level.Tasks) != 1 || level.Tasks[0].TaskText != "Задание <b>уровня</b>" ||
		level.Tasks[0].TaskTextFormatted != "Задание <b>уровня</b>" {
		t.Errorf("Tasks = %+v", level.Tasks)
	}
	if len(level.Messages) != 1 || level.Messages[0].MessageId != 3301 ||
		level.Messages[0].OwnerLogin != "author" || level.Messages[0].MessageText != "Сообщение оргов" {
		t.Errorf("Messages = %+v", level.Messages)
	}

	if len(level.Sectors) != 2 {
		t.Fatalf("Sectors = %d, want 2", len(level.Sectors))
	}
	if s := level.Sectors[0]; s.SectorId != 5501 || s.Order != 1 || s.Name != "Сектор A" ||
		!s.IsAnswered || s.Answer != "ответA" {
		t.Errorf("Sectors[0] = %+v", s)
	}
	if s := level.Sectors[1]; s.IsAnswered || s.Answer != "" {
		t.Errorf("Sectors[1] = %+v, want unanswered", s)
	}

	if len(level.Helps) != 1 {
		t.Fatalf("Helps = %d, want 1", len(level.Helps))
	}
	help := level.Helps[0]
	if help.HelpId != 7701 || help.Number != 1 || help.IsPenalty {
		t.Errorf("Helps[0] = %+v", help)
	}
	if help.HelpText == nil || *help.HelpText != "Подсказка один" {
		t.Errorf("Helps[0].HelpText = %v", help.HelpText)
	}

	if len(level.PenaltyHelps) != 1 {
		t.Fatalf("PenaltyHelps = %d, want 1", len(level.PenaltyHelps))
	}
	penalty := level.PenaltyHelps[0]
	if !penalty.IsPenalty || penalty.Penalty != 300 || !penalty.RequestConfirm ||
		penalty.PenaltyHelpState != 1 || penalty.RemainSeconds != 42 {
		t.Errorf("PenaltyHelps[0] = %+v", penalty)
	}
	if penalty.PenaltyComment == nil || *penalty.PenaltyComment != "минус 5 минут" {
		t.Errorf("PenaltyHelps[0].PenaltyComment = %v", penalty.PenaltyComment)
	}

	if len(level.Bonuses) != 1 {
		t.Fatalf("Bonuses = %d, want 1", len(level.Bonuses))
	}
	bonus := level.Bonuses[0]
	if bonus.BonusId != 9101 || bonus.Name != "Бонус" || bonus.Task != "Найдите" ||
		bonus.Help != "Подсказка бонуса" || !bonus.IsAnswered || bonus.Answer != "бонусответ" ||
		bonus.SecondsLeft != 900 || bonus.AwardTime != 180 || bonus.Expired || bonus.Negative {
		t.Errorf("Bonuses[0] = %+v", bonus)
	}

	if len(level.MixedActions) != 2 {
		t.Fatalf("MixedActions = %d, want 2", len(level.MixedActions))
	}
	action := level.MixedActions[0]
	if action.LevelId != 812 || action.UserId != 501 || action.Login != "svk" ||
		action.Answer != "ответA" || action.Kind != 1 || !action.IsCorrect {
		t.Errorf("MixedActions[0] = %+v", action)
	}
	if action.EnterDateTime == nil || action.EnterDateTime.Timestamp != 1787998800 {
		t.Errorf("MixedActions[0].EnterDateTime = %+v", action.EnterDateTime)
	}
	if level.MixedActions[1].IsCorrect {
		t.Error("MixedActions[1] should be a wrong answer")
	}
}

func TestGameModelFromEngineStateMapsEngineAction(t *testing.T) {
	action := gameModelFromEngineState(loadEngineStateFixture(t)).EngineAction
	if action == nil {
		t.Fatal("EngineAction is nil")
	}
	if action.GameId != 82448 || action.LevelId != 812 || action.LevelNumber != 2 {
		t.Errorf("EngineAction = %+v", action)
	}
	if action.LevelAction == nil {
		t.Fatal("LevelAction is nil, want the submitted answer")
	}
	if action.LevelAction.Answer == nil || *action.LevelAction.Answer != "ответA" {
		t.Errorf("LevelAction.Answer = %v", action.LevelAction.Answer)
	}
	if action.LevelAction.IsCorrectAnswer == nil || !*action.LevelAction.IsCorrectAnswer {
		t.Errorf("LevelAction.IsCorrectAnswer = %v", action.LevelAction.IsCorrectAnswer)
	}
	if action.BonusAction != nil {
		t.Errorf("BonusAction = %+v, want nil when no bonus was submitted", action.BonusAction)
	}
}

func TestGameModelFromEngineStateHandlesNil(t *testing.T) {
	if gameModelFromEngineState(nil) != nil {
		t.Error("nil state should map to a nil model")
	}
	empty := gameModelFromEngineState(&enapi.EngineState{})
	if empty == nil {
		t.Fatal("empty state mapped to nil")
	}
	if empty.Level != nil || empty.Levels != nil || empty.EngineAction != nil {
		t.Errorf("empty state produced %+v", empty)
	}
}

// TestCanSubmitLevelAnswerParityAcrossEngines pins the rule both engines must
// agree on: it is the level state, not the transport, that decides whether a
// level answer may be sent.
func TestCanSubmitLevelAnswerParityAcrossEngines(t *testing.T) {
	cases := []struct {
		name        string
		legacy      string
		engineState string
		want        bool
	}{
		{
			name:        "open level",
			legacy:      `{"Level":{"LevelId":1,"HasAnswerBlockRule":false}}`,
			engineState: `{"level":{"level_id":1,"has_answer_block_rule":false}}`,
			want:        true,
		},
		{
			name:        "answers blocked",
			legacy:      `{"Level":{"LevelId":1,"HasAnswerBlockRule":true,"BlockDuration":30}}`,
			engineState: `{"level":{"level_id":1,"has_answer_block_rule":true,"block_duration":30}}`,
			want:        false,
		},
		{
			name:        "block rule with no duration left",
			legacy:      `{"Level":{"LevelId":1,"HasAnswerBlockRule":true,"BlockDuration":0}}`,
			engineState: `{"level":{"level_id":1,"has_answer_block_rule":true,"block_duration":0}}`,
			want:        true,
		},
		{
			name:        "passed level",
			legacy:      `{"Level":{"LevelId":1,"IsPassed":true}}`,
			engineState: `{"level":{"level_id":1,"is_passed":true}}`,
			want:        false,
		},
		{
			name:        "dismissed level",
			legacy:      `{"Level":{"LevelId":1,"Dismissed":true}}`,
			engineState: `{"level":{"level_id":1,"dismissed":true}}`,
			want:        false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var legacyModel GameModel
			if err := json.Unmarshal([]byte(tc.legacy), &legacyModel); err != nil {
				t.Fatalf("decode legacy model: %v", err)
			}
			var state enapi.EngineState
			if err := json.Unmarshal([]byte(tc.engineState), &state); err != nil {
				t.Fatalf("decode engine state: %v", err)
			}
			newModel := gameModelFromEngineState(&state)

			legacyAllows := legacyModel.Level.CanSubmitLevelAnswer()
			newAllows := newModel.Level.CanSubmitLevelAnswer()
			if legacyAllows != tc.want || newAllows != tc.want {
				t.Errorf("CanSubmitLevelAnswer: legacy=%v new=%v, want %v", legacyAllows, newAllows, tc.want)
			}
		})
	}
}

func containsQueryPair(rawQuery, key, want string) bool {
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return false
	}
	return values.Get(key) == want
}

// legacyAnswerForm builds the form values callers passed to GetGameModel before
// SendCode existed; both engines must still honour them.
func legacyAnswerForm(levelID, levelNumber, action, answer string) url.Values {
	form := url.Values{}
	form.Set("LevelId", levelID)
	form.Set("LevelNumber", levelNumber)
	form.Set(action, answer)
	return form
}

// engineCall records what the new engine sent to the compatibility route.
type engineCall struct {
	method string
	path   string
	query  string
	msg    enapi.EngineClientMessage
}

func newEngineGameClient(t *testing.T, calls *[]engineCall) *Client {
	t.Helper()
	return newEngineClient(t, func(w http.ResponseWriter, r *http.Request) {
		call := engineCall{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery}
		if r.Method == http.MethodPost {
			_ = json.NewDecoder(r.Body).Decode(&call.msg)
		}
		*calls = append(*calls, call)
		state, err := os.ReadFile("testdata/engine_state.json")
		if err != nil {
			t.Errorf("read fixture: %v", err)
			return
		}
		_, _ = w.Write(state)
	})
}

func TestNewEngineGameCallsUseCompatibilityRoute(t *testing.T) {
	var calls []engineCall
	c := newEngineGameClient(t, &calls)
	ctx := context.Background()

	if _, err := c.GetGameModel(ctx, 82448); err != nil {
		t.Fatalf("GetGameModel: %v", err)
	}
	if _, err := c.GetGameModelLevel(ctx, 82448, 3); err != nil {
		t.Fatalf("GetGameModelLevel: %v", err)
	}
	if _, err := c.SendCode(ctx, 82448, 812, 2, "код"); err != nil {
		t.Fatalf("SendCode: %v", err)
	}
	if _, err := c.SendBonusCode(ctx, 82448, 812, 2, "бонус"); err != nil {
		t.Fatalf("SendBonusCode: %v", err)
	}
	if _, err := c.GetPenaltyHint(ctx, 82448, 44); err != nil {
		t.Fatalf("GetPenaltyHint: %v", err)
	}

	if len(calls) != 5 {
		t.Fatalf("calls = %d, want 5", len(calls))
	}
	for i, call := range calls {
		if call.path != "/gameengines/encounter/play/82448" {
			t.Errorf("calls[%d].path = %q", i, call.path)
		}
		if !containsQueryPair(call.query, "json", "1") {
			t.Errorf("calls[%d].query = %q, want json=1 (the route 404s without it)", i, call.query)
		}
	}

	if calls[0].method != http.MethodGet {
		t.Errorf("GetGameModel used %s, want GET", calls[0].method)
	}
	if !containsQueryPair(calls[1].query, "level", "3") {
		t.Errorf("GetGameModelLevel query = %q, want level=3", calls[1].query)
	}

	code := calls[2].msg
	if code.Type != enapi.EngineMessageAnswer || code.Answer != "код" ||
		code.LevelID != 812 || code.LevelNumber != 2 {
		t.Errorf("SendCode message = %+v", code)
	}
	bonus := calls[3].msg
	if bonus.Type != enapi.EngineMessageBonus || bonus.Answer != "бонус" {
		t.Errorf("SendBonusCode message = %+v", bonus)
	}
	hint := calls[4].msg
	if hint.Type != enapi.EngineMessagePenalty || hint.PenaltyID != 44 ||
		hint.PenaltyAct != enapi.PenaltyActApprove {
		t.Errorf("GetPenaltyHint message = %+v", hint)
	}
}

func TestNewEngineGetGameModelTranslatesLegacyFormValues(t *testing.T) {
	var calls []engineCall
	c := newEngineGameClient(t, &calls)

	form := legacyAnswerForm("812", "2", "LevelAction.Answer", "старый вызов")
	if _, err := c.GetGameModel(context.Background(), 82448, form); err != nil {
		t.Fatalf("GetGameModel: %v", err)
	}
	if len(calls) != 1 || calls[0].method != http.MethodPost {
		t.Fatalf("calls = %+v, want one POST", calls)
	}
	msg := calls[0].msg
	if msg.Type != enapi.EngineMessageAnswer || msg.Answer != "старый вызов" ||
		msg.LevelID != 812 || msg.LevelNumber != 2 {
		t.Errorf("message = %+v", msg)
	}
}

func TestNewEngineDecodesFixtureThroughTheClient(t *testing.T) {
	var calls []engineCall
	c := newEngineGameClient(t, &calls)

	model, err := c.GetGameModel(context.Background(), 82448)
	if err != nil {
		t.Fatalf("GetGameModel: %v", err)
	}
	if model.Level == nil || len(model.Level.Sectors) != 2 || len(model.Levels) != 2 {
		t.Fatalf("model was not fully decoded: %+v", model)
	}
}

// TestEngineActionKeepsAMissingVerdictUnknown pins the bug a player hit on
// mobile: a level with an answer-block rule refuses submissions inside the block
// window, and the engine answers with the code echoed back, reject_reason set
// and no verdict at all. Decoding that absence as false told the player their
// code was wrong when it was never checked — and hid that the attempt had not
// been spent, so the app looked like it simply stopped sending codes.
func TestEngineActionKeepsAMissingVerdictUnknown(t *testing.T) {
	// Captured verbatim from api.en.cx while a level's 1-attempt-per-60s rule
	// was blocking: level_ok is absent, not false.
	const blocked = `{"engine_action":{"game_id":27053,"level_id":265006,
	  "level_number":1,"request_level_id":265006,"request_level_number":1,
	  "reject_reason":"answer_blocked","level_answer":"код игрока"}}`

	var state enapi.EngineState
	if err := json.Unmarshal([]byte(blocked), &state); err != nil {
		t.Fatalf("decode: %v", err)
	}
	action := gameModelFromEngineState(&state).EngineAction
	if action == nil || action.LevelAction == nil {
		t.Fatalf("EngineAction = %+v, want the submitted answer reported", action)
	}
	if action.LevelAction.Answer == nil || *action.LevelAction.Answer != "код игрока" {
		t.Errorf("Answer = %v", action.LevelAction.Answer)
	}
	if action.LevelAction.IsCorrectAnswer != nil {
		t.Errorf("IsCorrectAnswer = %v, want nil: the engine never judged this answer",
			*action.LevelAction.IsCorrectAnswer)
	}
	if action.RejectReason != enapi.RejectAnswerBlocked {
		t.Errorf("RejectReason = %q, want %q", action.RejectReason, enapi.RejectAnswerBlocked)
	}
}

func TestEngineActionReportsARealVerdict(t *testing.T) {
	cases := map[string]bool{
		`{"engine_action":{"level_answer":"код","level_ok":true}}`:  true,
		`{"engine_action":{"level_answer":"код","level_ok":false}}`: false,
	}
	for body, want := range cases {
		var state enapi.EngineState
		if err := json.Unmarshal([]byte(body), &state); err != nil {
			t.Fatalf("decode %s: %v", body, err)
		}
		action := gameModelFromEngineState(&state).EngineAction
		if action.LevelAction == nil || action.LevelAction.IsCorrectAnswer == nil {
			t.Fatalf("%s: the verdict was dropped", body)
		}
		if got := *action.LevelAction.IsCorrectAnswer; got != want {
			t.Errorf("%s: IsCorrectAnswer = %v, want %v", body, got, want)
		}
		if action.RejectReason != "" {
			t.Errorf("%s: RejectReason = %q, want empty", body, action.RejectReason)
		}
	}
}

// TestBlockedLevelIsReportedAsNotSubmittable is the other half of the same
// story: while the block window runs the engine reports the remaining seconds,
// so a caller that asks before sending is told not to.
func TestBlockedLevelIsReportedAsNotSubmittable(t *testing.T) {
	const state = `{"level":{"level_id":265006,"number":1,
	  "has_answer_block_rule":true,"block_duration":59,
	  "attempts_number":1,"attempts_period":60}}`

	var decoded enapi.EngineState
	if err := json.Unmarshal([]byte(state), &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	level := gameModelFromEngineState(&decoded).Level
	if level.BlockDuration != 59 || !level.HasAnswerBlockRule {
		t.Fatalf("level = %+v", level)
	}
	if level.CanSubmitLevelAnswer() {
		t.Error("CanSubmitLevelAnswer = true while the answer-block window is running")
	}
	if level.AttemtsNumber != 1 || level.AttemtsPeriod != 60 {
		t.Errorf("attempts = %d/%d, want 1 per 60s", level.AttemtsNumber, level.AttemtsPeriod)
	}
}
