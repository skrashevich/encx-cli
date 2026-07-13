package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/skrashevich/encx-cli/encx"
	"github.com/skrashevich/encx-cli/encx/scenario"
)

func TestProfileUsesAllLevelsAndRequiredSectorThreshold(t *testing.T) {
	s := profileTestServer(t, []levelTopology{
		{Number: 1, SectorCount: 21, RequiredSectorCount: 19, BonusCount: 1},
		{Number: 2, SectorCount: 2, RequiredSectorCount: 2},
		{Number: 3, SectorCount: 3, RequiredSectorCount: 3},
		{Number: 4, SectorCount: 4, RequiredSectorCount: 4},
	})
	st := profileState(s)
	if s.levelCount() != 4 || len(st.SectorPassed[0]) != 21 {
		t.Fatalf("profile topology was not applied")
	}
	for i := 0; i < 19; i++ {
		if !s.processLevelAnswer(st, currentLevelID(st), currentLevelNum(st), fmt.Sprintf("sector-%d", i)) {
			t.Fatalf("sector %d was not accepted", i)
		}
	}
	if !st.Passed[0] || st.CurrentIdx != 1 {
		t.Fatal("level should pass at 19 of 21 sectors")
	}
}

func TestProfileBonusAndLevelActionsAreSeparated(t *testing.T) {
	s := profileTestServer(t, []levelTopology{{Number: 1, SectorCount: 1, RequiredSectorCount: 1, BonusCount: 1}})
	st := profileState(s)
	s.processBonusAnswer(st, currentLevelID(st), currentLevelNum(st), "1")
	if st.SectorPassed[0][0] {
		t.Fatal("bonus answer must not pass a sector")
	}
	s.processLevelAnswer(st, currentLevelID(st), currentLevelNum(st), "bonus-1")
	if st.AnsweredBonuses[mockBonusBaseID+1] {
		t.Fatal("level answer must not pass a bonus")
	}
}

func TestContradictoryLevelIdentityDoesNotMutate(t *testing.T) {
	s := profileTestServer(t, []levelTopology{{Number: 1, SectorCount: 1, RequiredSectorCount: 1}})
	st := profileState(s)
	if s.processLevelAnswer(st, currentLevelID(st)+1, currentLevelNum(st), "1") {
		t.Fatal("contradictory identity was accepted")
	}
	if st.SectorPassed[0][0] || len(st.Actions) != 0 {
		t.Fatal("contradictory identity mutated state")
	}
}

func TestActionsHaveStableTimestampsAndGetIsIdle(t *testing.T) {
	s := profileTestServer(t, []levelTopology{{Number: 1, SectorCount: 2, RequiredSectorCount: 2}})
	st := profileState(s)
	s.processLevelAnswer(st, currentLevelID(st), currentLevelNum(st), "1")
	first := codeActionsToMaps(st.Actions, time.Now())[0]["EnterDateTime"]
	time.Sleep(time.Millisecond)
	second := codeActionsToMaps(st.Actions, time.Now())[0]["EnterDateTime"]
	if first.(*encx.DateTime).Timestamp != second.(*encx.DateTime).Timestamp {
		t.Fatal("action timestamp changed between renders")
	}
	st.LastAction = nil
	model, err := s.buildGameModelResponse(st, time.Now())
	if err != nil || model["EngineAction"].(map[string]any)["LevelId"] != 0 {
		t.Fatal("ordinary subsequent GET must be idle")
	}
}

func TestScenarioOverridesProfileGameContent(t *testing.T) {
	s := profileTestServer(t, []levelTopology{{Number: 1, SectorCount: 21, RequiredSectorCount: 19}})
	s.scenario = &scenario.Document{Levels: []scenario.Level{{Number: 1, Name: "Scenario", SectorAnswers: [][]string{{"scenario-only"}}}}}
	st := profileState(s)
	if s.levelCount() != 1 || len(st.SectorPassed[0]) != 1 {
		t.Fatal("scenario must own topology when combined with a profile")
	}
	if s.processLevelAnswer(st, currentLevelID(st), currentLevelNum(st), "1") {
		t.Fatal("profile/default answer must not match scenario content")
	}
	if !s.processLevelAnswer(st, currentLevelID(st), currentLevelNum(st), "scenario-only") {
		t.Fatal("scenario answer must remain authoritative")
	}
}

func TestProfileBonusCorrectnessIsNull(t *testing.T) {
	s := profileTestServer(t, []levelTopology{{Number: 1, BonusCount: 1}})
	st := profileState(s)
	if !s.processBonusAnswer(st, currentLevelID(st), currentLevelNum(st), "bonus-1") {
		t.Fatal("profile bonus answer was not accepted")
	}
	if st.LastAction.BonusAction.IsCorrectAnswer != nil {
		t.Fatal("profile bonus correctness must preserve null variant")
	}
}

func TestProfileBonusProjectionAcceptsIntegerBonusID(t *testing.T) {
	s := profileTestServer(t, []levelTopology{{Number: 1, BonusCount: 1}})
	level := s.fixtures.gameModelTemplate["Level"].(map[string]any)
	level["Bonuses"] = []any{map[string]any{"BonusId": mockBonusBaseID + 1, "IsAnswered": false, "Answer": nil}}
	st := profileState(s)
	st.AnsweredBonuses[mockBonusBaseID+1] = true
	st.BonusAnswers[mockBonusBaseID+1] = "bonus-1"

	model, err := s.buildGameModelResponse(st, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	bonus := model["Level"].(map[string]any)["Bonuses"].([]any)[0].(map[string]any)
	if answered, _ := bonus["IsAnswered"].(bool); !answered {
		t.Fatal("integer BonusId was not projected as answered")
	}
	if bonus["Answer"].(map[string]any)["Answer"] != "bonus-1" {
		t.Fatalf("bonus answer = %#v", bonus["Answer"])
	}
}

func TestDefaultFixtureSectorIDsUseLegacySequence(t *testing.T) {
	fixtures, err := loadFixtures()
	if err != nil {
		t.Fatal(err)
	}
	s := &server{fixtures: fixtures}
	st := profileState(s)

	for _, levelIdx := range []int{1, 2} {
		st.CurrentIdx = levelIdx
		model, err := s.buildGameModelResponse(st, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		sectors := model["Level"].(map[string]any)["Sectors"].([]any)
		for i, item := range sectors {
			got, ok := valueInt(item.(map[string]any)["SectorId"])
			if !ok {
				t.Fatalf("level %d sector %d has non-integer SectorId", levelIdx+1, i+1)
			}
			want := mockSectorBaseID + levelIdx*mockSectorsPerLevel + i + 1
			if got != want {
				t.Fatalf("level %d sector %d ID = %d, want %d", levelIdx+1, i+1, got, want)
			}
		}
	}
}

func TestLegacyDispatcherRecordsOnlyBonusAction(t *testing.T) {
	s := profileTestServer(t, []levelTopology{{Number: 1, SectorCount: 1, RequiredSectorCount: 1, BonusCount: 1}})
	st := profileState(s)
	if !s.processAnswer(st, "bonus-1") {
		t.Fatal("bonus was not accepted")
	}
	if len(st.Actions) != 1 || st.Actions[0].Kind != 2 || !st.Actions[0].IsCorrect {
		t.Fatalf("actions = %#v, want exactly one correct bonus action", st.Actions)
	}
}

func TestScenarioWithProfileLayersTransitionVariant(t *testing.T) {
	s := profileTestServer(t, []levelTopology{{Number: 1, SectorCount: 21, RequiredSectorCount: 19}})
	s.fixtures.profile.TransitionEvents = []int{19, 22}
	s.fixtures.profile.Records = []protocolRecord{
		{Kind: "level-action", Variant: "transition", Event: 19},
		{Kind: "bonus-action", Variant: "transition", Event: 22},
	}
	s.scenario = &scenario.Document{Levels: []scenario.Level{{
		Number: 1, Name: "Scenario", SectorAnswers: [][]string{{"scenario-only"}, {"second"}},
		Bonuses: []scenario.Bonus{{Number: 1, Answers: []string{"scenario-bonus"}}},
	}}}
	st := profileState(s)
	handler := gameHandler(s, st)

	response := gameRequest(t, handler, http.MethodPost, "LevelAction.Answer=scenario-only")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body)
	}
	var model map[string]any
	if err := json.NewDecoder(response.Body).Decode(&model); err != nil {
		t.Fatal(err)
	}
	if model["Event"] != float64(0) || model["Level"] == nil {
		t.Fatalf("non-final sector response = %#v, want normal snapshot", model)
	}
	if st.PendingTransition != 0 {
		t.Fatalf("non-final sector scheduled transition %d", st.PendingTransition)
	}
	activeResponse := gameRequest(t, handler, http.MethodGet, "")
	if err := json.NewDecoder(activeResponse.Body).Decode(&model); err != nil {
		t.Fatal(err)
	}
	level := model["Level"].(map[string]any)
	summary := model["Levels"].([]any)[0].(map[string]any)
	if summary["LevelName"] != "Scenario" || len(level["Sectors"].([]any)) != 2 {
		t.Fatalf("scenario topology was not preserved: %#v", level)
	}
	bonusResponse := gameRequest(t, handler, http.MethodPost, "BonusAction.Answer=scenario-bonus")
	if err := json.NewDecoder(bonusResponse.Body).Decode(&model); err != nil {
		t.Fatal(err)
	}
	if model["Event"] != float64(0) || model["Level"] == nil {
		t.Fatalf("bonus response = %#v, want normal snapshot", model)
	}
	if st.PendingTransition != 0 {
		t.Fatalf("bonus scheduled transition %d", st.PendingTransition)
	}
}

func TestGamePostRejectsInvalidDuplicateAndContradictoryIdentity(t *testing.T) {
	s := profileTestServer(t, []levelTopology{{Number: 1, SectorCount: 1, RequiredSectorCount: 1}})
	st := profileState(s)
	handler := gameHandler(s, st)
	for _, form := range []string{
		"LevelId=not-a-number&LevelAction.Answer=1",
		fmt.Sprintf("LevelId=%d&LevelId=%d&LevelAction.Answer=1", currentLevelID(st), currentLevelID(st)),
		"LevelNumber=not-a-number&LevelAction.Answer=1",
		fmt.Sprintf("LevelNumber=%d&LevelNumber=%d&LevelAction.Answer=1", currentLevelNum(st), currentLevelNum(st)),
		fmt.Sprintf("LevelId=%d&LevelNumber=%d&LevelAction.Answer=1", currentLevelID(st)+1, currentLevelNum(st)),
	} {
		response := gameRequest(t, handler, http.MethodPost, form)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("form %q status = %d, want 400", form, response.Code)
		}
		if st.SectorPassed[0][0] || len(st.Actions) != 0 {
			t.Fatalf("form %q mutated state: %#v", form, st)
		}
	}
	if response := gameRequest(t, handler, http.MethodPost, "LevelAction.Answer=1"); response.Code != http.StatusOK || !st.SectorPassed[0][0] {
		t.Fatalf("absent identity status = %d, state = %#v", response.Code, st)
	}
}

func TestGamePostRejectsMultipleActionFields(t *testing.T) {
	s := profileTestServer(t, []levelTopology{{Number: 1, SectorCount: 1, RequiredSectorCount: 1, BonusCount: 1}})
	st := profileState(s)
	response := gameRequest(t, gameHandler(s, st), http.MethodPost, "LevelAction.Answer=1&BonusAction.Answer=bonus-1")
	if response.Code != http.StatusBadRequest || len(st.Actions) != 0 {
		t.Fatalf("status = %d, actions = %#v", response.Code, st.Actions)
	}
}

func TestGamePostPreservesRawAnswerAndNextGetIsIdle(t *testing.T) {
	s := profileTestServer(t, []levelTopology{{Number: 1, SectorCount: 1, RequiredSectorCount: 1}})
	s.scenario = &scenario.Document{Levels: []scenario.Level{{Number: 1, SectorAnswers: [][]string{{"code"}}}}}
	st := profileState(s)
	handler := gameHandler(s, st)
	rawAnswer := "  code  "
	response := gameRequest(t, handler, http.MethodPost, "LevelAction.Answer="+url.QueryEscape(rawAnswer))
	if response.Code != http.StatusOK || len(st.Actions) != 1 || st.Actions[0].Answer != rawAnswer {
		t.Fatalf("POST did not preserve raw answer: status=%d actions=%#v", response.Code, st.Actions)
	}
	var posted map[string]any
	if err := json.NewDecoder(response.Body).Decode(&posted); err != nil {
		t.Fatal(err)
	}
	if posted["EngineAction"].(map[string]any)["LevelAction"].(map[string]any)["Answer"] != rawAnswer {
		t.Fatalf("EngineAction = %#v", posted["EngineAction"])
	}
	idle := gameRequest(t, handler, http.MethodGet, "")
	var polled map[string]any
	if err := json.NewDecoder(idle.Body).Decode(&polled); err != nil {
		t.Fatal(err)
	}
	if polled["EngineAction"].(map[string]any)["LevelId"] != float64(0) {
		t.Fatalf("idle EngineAction = %#v", polled["EngineAction"])
	}
}

func TestActionTimestampPreservesYearAcrossPolls(t *testing.T) {
	actionTime := time.Date(2025, time.December, 31, 23, 59, 59, 0, time.UTC)
	s := profileTestServer(t, []levelTopology{{Number: 1, SectorCount: 1, RequiredSectorCount: 1}})
	st := profileState(s)
	s.processLevelAnswer(st, currentLevelID(st), currentLevelNum(st), "1")
	if len(st.Actions) != 1 || st.Actions[0].EnterDateTime == nil {
		t.Fatalf("mutation did not store action time: %#v", st.Actions)
	}
	action := st.Actions[0]
	action.EnterDateTime = dt(actionTime)
	action.LocDateTime = actionTime.Format("02.01 15:04:05")
	rendered := codeActionsToMaps([]encx.CodeAction{action}, time.Date(2026, time.January, 1, 0, 0, 1, 0, time.UTC))
	if got := rendered[0]["EnterDateTime"].(*encx.DateTime).Timestamp; got != actionTime.Unix() {
		t.Fatalf("timestamp = %d, want %d", got, actionTime.Unix())
	}
}

func profileTestServer(t *testing.T, levels []levelTopology) *server {
	t.Helper()
	fixtures, err := loadFixtures()
	if err != nil {
		t.Fatal(err)
	}
	fixtures.profile = &protocolProfile{Levels: levels}
	return &server{fixtures: fixtures}
}

func profileState(s *server) *sessionState {
	st := &sessionState{
		Login: "profile", Passed: s.newSessionPassedState(), SectorPassed: s.newSessionSectorState(),
		SectorAnswers: s.newSessionSectorAnswerState(), AnsweredBonuses: map[int]bool{}, BonusAnswers: map[int]string{},
	}
	return st
}

func gameHandler(s *server, st *sessionState) http.Handler {
	s.sessions = map[string]*sessionState{"test": st}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			s.handleGamePlayGET(w, r)
		case http.MethodPost:
			s.handleGamePlayPOST(w, r)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
}

func gameRequest(t *testing.T, handler http.Handler, method, form string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, fmt.Sprintf("/gameengines/encounter/play/%d/", mockGameID), nil)
	request.AddCookie(&http.Cookie{Name: "SESSION_ID", Value: "test"})
	if method == http.MethodPost {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Body = io.NopCloser(strings.NewReader(form))
		request.ContentLength = int64(len(form))
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
