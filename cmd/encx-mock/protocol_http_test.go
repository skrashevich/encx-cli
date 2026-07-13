package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestProfileTransitionIsOneShotAndRequiresMatchingAction(t *testing.T) {
	t.Run("one shot", func(t *testing.T) {
		s := profileTestServer(t, []levelTopology{
			{Number: 1, SectorCount: 1, RequiredSectorCount: 1},
			{Number: 2, SectorCount: 1, RequiredSectorCount: 1},
		})
		s.fixtures.profile.Records = []protocolRecord{{Kind: "level-action", Variant: "transition", Event: 19}}
		st := profileState(s)
		handler := gameHandler(s, st)

		transition := gameRequest(t, handler, http.MethodPost, "LevelAction.Answer=1")
		var transitionModel map[string]any
		if err := json.NewDecoder(transition.Body).Decode(&transitionModel); err != nil {
			t.Fatal(err)
		}
		if transitionModel["Event"] != float64(19) || transitionModel["Level"] != nil {
			t.Fatalf("transition = %#v", transitionModel)
		}
		if levels, ok := transitionModel["Levels"].([]any); !ok || len(levels) != 0 {
			t.Fatalf("transition Levels = %#v, want empty array", transitionModel["Levels"])
		}

		active := gameRequest(t, handler, http.MethodGet, "")
		var activeModel map[string]any
		if err := json.NewDecoder(active.Body).Decode(&activeModel); err != nil {
			t.Fatal(err)
		}
		if activeModel["Event"] != float64(0) {
			t.Fatalf("active Event = %v, want 0", activeModel["Event"])
		}
		if level, _ := activeModel["Level"].(map[string]any); level == nil || level["Number"] != float64(2) {
			t.Fatalf("active Level = %#v, want level 2", activeModel["Level"])
		}
	})

	t.Run("mismatched action", func(t *testing.T) {
		s := profileTestServer(t, []levelTopology{
			{Number: 1, SectorCount: 1, RequiredSectorCount: 1},
			{Number: 2, SectorCount: 1, RequiredSectorCount: 1},
		})
		s.fixtures.profile.TransitionEvents = []int{22}
		s.fixtures.profile.Records = []protocolRecord{{Kind: "bonus-action", Variant: "transition", Event: 22}}
		st := profileState(s)

		response := gameRequest(t, gameHandler(s, st), http.MethodPost, "LevelAction.Answer=1")
		var model map[string]any
		if err := json.NewDecoder(response.Body).Decode(&model); err != nil {
			t.Fatal(err)
		}
		if model["Event"] != float64(0) {
			t.Fatalf("mismatched action Event = %v, want 0", model["Event"])
		}
		if model["Level"] == nil {
			t.Fatalf("mismatched action returned transition: %#v", model)
		}
	})

	t.Run("incorrect action", func(t *testing.T) {
		s := profileTestServer(t, []levelTopology{
			{Number: 1, SectorCount: 1, RequiredSectorCount: 1},
			{Number: 2, SectorCount: 1, RequiredSectorCount: 1},
		})
		s.fixtures.profile.Records = []protocolRecord{{Kind: "level-action", Variant: "transition", Event: 19}}
		st := profileState(s)
		handler := gameHandler(s, st)

		response := gameRequest(t, handler, http.MethodPost, "LevelAction.Answer=incorrect")
		var posted map[string]any
		if err := json.NewDecoder(response.Body).Decode(&posted); err != nil {
			t.Fatal(err)
		}
		if posted["Event"] != float64(0) || posted["Level"] == nil {
			t.Fatalf("incorrect action returned transition: %#v", posted)
		}

		active := gameRequest(t, handler, http.MethodGet, "")
		var polled map[string]any
		if err := json.NewDecoder(active.Body).Decode(&polled); err != nil {
			t.Fatal(err)
		}
		if level, _ := polled["Level"].(map[string]any); level == nil || level["Number"] != float64(1) {
			t.Fatalf("GET Level = %#v, want current level 1", polled["Level"])
		}
	})
}

func TestProfileTransitionRequiresLevelAdvanceOrCompletion(t *testing.T) {
	t.Run("correct non-final sector", func(t *testing.T) {
		s := profileTestServer(t, []levelTopology{
			{Number: 1, SectorCount: 2, RequiredSectorCount: 2},
			{Number: 2, SectorCount: 1, RequiredSectorCount: 1},
		})
		s.fixtures.profile.Records = []protocolRecord{{Kind: "level-action", Variant: "transition", Event: 19}}
		st := profileState(s)

		response := gameRequest(t, gameHandler(s, st), http.MethodPost, "LevelAction.Answer=1")
		assertNormalSnapshotWithoutPendingTransition(t, response, st)
	})

	t.Run("correct bonus", func(t *testing.T) {
		s := profileTestServer(t, []levelTopology{
			{Number: 1, SectorCount: 1, RequiredSectorCount: 1, BonusCount: 1},
			{Number: 2, SectorCount: 1, RequiredSectorCount: 1},
		})
		s.fixtures.profile.Records = []protocolRecord{{Kind: "bonus-action", Variant: "transition", Event: 22}}
		st := profileState(s)

		response := gameRequest(t, gameHandler(s, st), http.MethodPost, "BonusAction.Answer=bonus-1")
		assertNormalSnapshotWithoutPendingTransition(t, response, st)
	})

	t.Run("final required sector", func(t *testing.T) {
		s := profileTestServer(t, []levelTopology{
			{Number: 1, SectorCount: 1, RequiredSectorCount: 1},
			{Number: 2, SectorCount: 1, RequiredSectorCount: 1},
		})
		s.fixtures.profile.Records = []protocolRecord{{Kind: "level-action", Variant: "transition", Event: 19}}
		st := profileState(s)

		response := gameRequest(t, gameHandler(s, st), http.MethodPost, "LevelAction.Answer=1")
		var transition map[string]any
		if err := json.NewDecoder(response.Body).Decode(&transition); err != nil {
			t.Fatal(err)
		}
		if transition["Event"] != float64(19) || transition["Level"] != nil || st.PendingTransition != 0 {
			t.Fatalf("transition = %#v, pending = %d", transition, st.PendingTransition)
		}
	})
}

func assertNormalSnapshotWithoutPendingTransition(t *testing.T, response *httptest.ResponseRecorder, st *sessionState) {
	t.Helper()
	var model map[string]any
	if err := json.NewDecoder(response.Body).Decode(&model); err != nil {
		t.Fatal(err)
	}
	if model["Event"] != float64(0) || model["Level"] == nil || st.PendingTransition != 0 {
		t.Fatalf("response = %#v, pending = %d", model, st.PendingTransition)
	}
}

func TestEngineResponseCarriesProtocolHeadersAndSyntheticCookies(t *testing.T) {
	s := profileTestServer(t, []levelTopology{{Number: 1, SectorCount: 1, RequiredSectorCount: 1}})
	st := profileState(s)
	response := gameRequest(t, withCommonHeaders(gameHandler(s, st)), http.MethodGet, "")

	if got := response.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if got := response.Header().Get("Pragma"); got != "no-cache" {
		t.Fatalf("Pragma = %q", got)
	}
	expires, err := http.ParseTime(response.Header().Get("Expires"))
	if err != nil || !expires.Before(time.Now()) {
		t.Fatalf("Expires = %q, want expired HTTP date", response.Header().Get("Expires"))
	}
	if got := response.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
	cookies := strings.Join(response.Header().Values("Set-Cookie"), "\n")
	if !strings.Contains(cookies, "stoken=mock-stoken") || !strings.Contains(cookies, "Domain=mock.en.cx") {
		t.Fatalf("protocol cookies = %q", cookies)
	}
}

func TestConfiguredAntiBotRedirectAndPZDCRemainIndependent(t *testing.T) {
	t.Setenv("ENCX_MOCK_ANTIBOT_ATTEMPTS", "1")
	s := profileTestServer(t, []levelTopology{{Number: 1, SectorCount: 1, RequiredSectorCount: 1}})
	s.antiBotAnswerAttempts = antiBotAnswerAttemptsFromEnv()
	st := profileState(s)
	handler := gameHandler(s, st)

	redirect := gameRequest(t, handler, http.MethodPost, "LevelAction.Answer=1")
	if redirect.Code != http.StatusFound || redirect.Header().Get("Location") != "/NotHumanRequest.aspx?return=redacted" {
		t.Fatalf("anti-bot response = %d %q", redirect.Code, redirect.Header().Get("Location"))
	}

	s = profileTestServer(t, []levelTopology{{Number: 1, SectorCount: 1, RequiredSectorCount: 1}})
	s.antiBotAnswerAttempts = antiBotAnswerAttemptsFromEnv()
	s.silentUntil = make(map[string]time.Time)
	st = profileState(s)
	st.AuthKey = "profile-auth"
	handler = gameHandler(s, st)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	response := gameRequestWithContext(t, handler, ctx, "LevelAction.Answer=PZDC")
	if response.Code == http.StatusFound {
		t.Fatal("PZDC was converted into anti-bot redirect")
	}
	if _, ok := s.silentUntil[st.AuthKey]; !ok {
		t.Fatal("PZDC transport-failure state was not activated")
	}
}

func TestLegacyEmptyPostRemainsPollWithoutConsumingAntiBotAttempt(t *testing.T) {
	t.Setenv("ENCX_MOCK_ANTIBOT_ATTEMPTS", "1")
	s := profileTestServer(t, []levelTopology{{Number: 1, SectorCount: 1, RequiredSectorCount: 1}})
	s.antiBotAnswerAttempts = antiBotAnswerAttemptsFromEnv()
	s.fixtures.profile.Records = []protocolRecord{{Kind: "level-action", Variant: "transition", Event: 19}}
	st := profileState(s)
	handler := gameHandler(s, st)

	poll := gameRequest(t, handler, http.MethodPost, "")
	if poll.Code != http.StatusOK || poll.Header().Get("Location") != "" {
		t.Fatalf("legacy poll response = %d %q, want normal response without redirect", poll.Code, poll.Header().Get("Location"))
	}
	var pollModel map[string]any
	if err := json.NewDecoder(poll.Body).Decode(&pollModel); err != nil {
		t.Fatal(err)
	}
	if pollModel["Event"] != float64(0) || pollModel["Level"] == nil {
		t.Fatalf("legacy poll model = %#v, want normal poll without transition", pollModel)
	}
	if st.AnswerAttempts != 0 || st.LastAction != nil || st.PendingTransition != 0 {
		t.Fatalf("legacy poll mutated action state: %#v", st)
	}

	action := gameRequest(t, handler, http.MethodPost, "LevelAction.Answer=1")
	if action.Code != http.StatusFound || action.Header().Get("Location") != "/NotHumanRequest.aspx?return=redacted" {
		t.Fatalf("first action response = %d %q, want configured anti-bot redirect", action.Code, action.Header().Get("Location"))
	}
}

func gameRequestWithContext(t *testing.T, handler http.Handler, ctx context.Context, form string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/gameengines/encounter/play/%d/", mockGameID), strings.NewReader(form)).WithContext(ctx)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(&http.Cookie{Name: "SESSION_ID", Value: "test"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
