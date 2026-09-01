package encx

import (
	"fmt"
	"strings"
	"testing"

	"github.com/skrashevich/encx-cli/encx/enapi"
)

// The write paths that are neither the admin editor nor a plain read: sending
// codes, joining a game and the game lifecycle actions. They are the class most
// likely to report success while doing nothing, so each one is checked against
// what the engine reports afterwards.
//
//	ENCX_LIVE_DOMAIN=demo.en.cx ENCX_LIVE_LOGIN=… ENCX_LIVE_PASSWORD=… \
//	  go test ./encx/ -run TestLiveActions -v

// TestLiveActionsSendCode submits a code that cannot be right and checks that the
// verdict comes back as a verdict — the legacy engine expressed "not checked"
// as a nil IsCorrectAnswer, and the new mapping has to keep that distinction.
func TestLiveActionsSendCode(t *testing.T) {
	c := liveClient(t)
	gameID := liveGameID(t, c)
	ctx, cancel := liveContext(t)
	defer cancel()

	model, err := c.GetGameModel(ctx, gameID)
	if err != nil {
		t.Fatalf("GetGameModel(%d): %v", gameID, err)
	}
	if model.Level == nil {
		t.Skip("the game is not in a state that accepts answers")
	}
	if !model.Level.CanSubmitLevelAnswer() {
		t.Skipf("the level is blocked for another %d seconds", model.Level.BlockDuration)
	}

	after, err := c.SendCode(ctx, gameID, model.Level.LevelId, model.Level.Number, "encx-parity-probe")
	if err != nil {
		t.Fatalf("SendCode: %v", err)
	}
	if after.EngineAction == nil || after.EngineAction.LevelAction == nil {
		t.Fatalf("SendCode returned no action result: %+v", after.EngineAction)
	}
	action := after.EngineAction.LevelAction
	if action.Answer == nil || *action.Answer != "encx-parity-probe" {
		t.Errorf("the engine echoed answer %v, want the code that was sent", action.Answer)
	}
	switch {
	case action.IsCorrectAnswer == nil:
		// No verdict: the engine refused to judge, e.g. the answer-block rule.
		if after.EngineAction.RejectReason == "" {
			t.Error("the answer got no verdict and no reason; the caller cannot tell why")
		}
		t.Logf("not judged, reject reason %q", after.EngineAction.RejectReason)
	case *action.IsCorrectAnswer:
		t.Errorf("the engine accepted %q as a correct code", "encx-parity-probe")
	default:
		t.Logf("judged incorrect, as expected")
	}
}

// TestLiveActionsSendBonusCode does the same for bonus codes.
func TestLiveActionsSendBonusCode(t *testing.T) {
	c := liveClient(t)
	gameID := liveGameID(t, c)
	ctx, cancel := liveContext(t)
	defer cancel()

	model, err := c.GetGameModel(ctx, gameID)
	if err != nil {
		t.Fatalf("GetGameModel(%d): %v", gameID, err)
	}
	if model.Level == nil {
		t.Skip("the game is not in a state that accepts answers")
	}

	after, err := c.SendBonusCode(ctx, gameID, model.Level.LevelId, model.Level.Number, "encx-bonus-probe")
	if err != nil {
		t.Fatalf("SendBonusCode: %v", err)
	}
	if after.EngineAction == nil {
		t.Fatal("SendBonusCode returned no engine action")
	}
	if action := after.EngineAction.BonusAction; action != nil {
		if action.IsCorrectAnswer != nil && *action.IsCorrectAnswer {
			t.Errorf("the engine accepted %q as a correct bonus code", "encx-bonus-probe")
		}
		t.Logf("bonus verdict: %s", verdictText(action.IsCorrectAnswer))
		return
	}
	t.Logf("the level has no bonuses to judge; reject reason %q", after.EngineAction.RejectReason)
}

// verdictText spells out the three states an answer can be in, which is the
// distinction the legacy *bool carried and the new mapping has to preserve.
func verdictText(value *bool) string {
	switch {
	case value == nil:
		return "not judged"
	case *value:
		return "correct"
	default:
		return "incorrect"
	}
}

// TestLiveActionsLifecycle checks that the game management actions either do
// their job or fail loudly. A fresh game has nothing to award or rate, and the
// engine must say so rather than answer success over an untouched game.
func TestLiveActionsLifecycle(t *testing.T) {
	c := liveClient(t)
	gameID := scratchLevels(t, c, 1)
	ctx, cancel := liveAdminCtx(t)
	defer cancel()

	var lifecycle enapi.AdminGameLifecycle
	if err := c.api().GetJSON(ctx, fmt.Sprintf("/admin/games/%d/lifecycle", gameID), nil, &lifecycle); err != nil {
		t.Fatalf("lifecycle: %v", err)
	}
	t.Logf("lifecycle: points=%v rate=%v qi=%v",
		lifecycle.CanCalculatePoints, lifecycle.CanCloseRate, lifecycle.CanCalculateQI)

	cases := []struct {
		name    string
		allowed bool
		call    func() error
	}{
		{"AdminAwardPoints", lifecycle.CanCalculatePoints, func() error { return c.AdminAwardPoints(ctx, gameID) }},
		{"AdminEndRatings", lifecycle.CanCloseRate, func() error { return c.AdminEndRatings(ctx, gameID) }},
		{"AdminCalculateIK", lifecycle.CanCalculateQI, func() error { return c.AdminCalculateIK(ctx, gameID) }},
	}
	for _, tc := range cases {
		err := tc.call()
		switch {
		case tc.allowed && err != nil:
			t.Errorf("%s: the lifecycle allows it but it failed: %v", tc.name, err)
		case !tc.allowed && err == nil:
			t.Errorf("%s: reported success although the lifecycle says it is not available", tc.name)
		default:
			t.Logf("%s: allowed=%v err=%v", tc.name, tc.allowed, err)
		}
	}

	// Признание игры состоявшейся is irreversible and its status codes are not
	// published, so both methods must refuse instead of guessing.
	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"AdminDeliverGame", func() error { return c.AdminDeliverGame(ctx, gameID) }},
		{"AdminNotDeliverGame", func() error { return c.AdminNotDeliverGame(ctx, gameID) }},
	} {
		err := tc.call()
		if err == nil {
			t.Errorf("%s went ahead without a documented status_id", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), "status_id") {
			t.Errorf("%s: error = %v, want it to name the missing status_id", tc.name, err)
		}
	}
}

// TestLiveActionsEnterGame submits an application to a game this test owns, so
// nobody else's registration list is touched.
func TestLiveActionsEnterGame(t *testing.T) {
	c := liveClient(t)
	gameID := scratchLevels(t, c, 1)
	ctx, cancel := liveAdminCtx(t)
	defer cancel()

	body, err := c.EnterGame(ctx, gameID)
	if err != nil {
		// A refusal is a legitimate answer — the game may not accept entries —
		// as long as it is reported rather than swallowed.
		t.Logf("EnterGame(%d) refused: %v", gameID, err)
		return
	}
	if strings.TrimSpace(body) == "" {
		t.Error("EnterGame reported success with an empty body")
	}
	t.Logf("EnterGame(%d): %s", gameID, body)
}
