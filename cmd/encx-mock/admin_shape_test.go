package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/skrashevich/encx-cli/encx"
)

// A mock is only worth what its shapes are worth. These tests hold the admin
// responses against documents captured from the live engine, so the mock cannot
// drift into a shape the real server never sends — which is how the client came
// to believe things about this API that were not true.
//
// The reference documents in fixtures/live_*.json were recorded from demo.en.cx
// against a game the capture created and deleted.

func loadLiveReference(t *testing.T, name string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("fixtures", "live_"+name+".json"))
	if err != nil {
		t.Fatalf("read reference %s: %v", name, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse reference %s: %v", name, err)
	}
	return doc
}

// mockJSON asks the mock for a path and returns the decoded document.
func mockJSON(t *testing.T, srv *httptest.Server, path string) map[string]any {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+path, nil)
	if err != nil {
		t.Fatalf("request %s: %v", path, err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: HTTP %d", path, resp.StatusCode)
	}
	var doc map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return doc
}

// missingKeys reports reference keys the mock does not serve. The mock may carry
// fewer fields than the engine — it does not model every quiz setting — but a
// field the client reads has to be there, so the check is directed: everything
// the reference has and the mock lacks is listed, and the caller decides which
// of those matter.
func missingKeys(reference, actual map[string]any) []string {
	var missing []string
	for key := range reference {
		if _, ok := actual[key]; !ok {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	return missing
}

// extraKeys reports fields the mock invents. Those are the dangerous ones: a
// mock that answers with a key the engine never sends would let the client build
// on something that does not exist.
func extraKeys(reference, actual map[string]any) []string {
	var extra []string
	for key := range actual {
		if _, ok := reference[key]; !ok {
			extra = append(extra, key)
		}
	}
	sort.Strings(extra)
	return extra
}

func firstObject(t *testing.T, doc map[string]any, key string) map[string]any {
	t.Helper()
	list, ok := doc[key].([]any)
	if !ok || len(list) == 0 {
		return nil
	}
	object, ok := list[0].(map[string]any)
	if !ok {
		t.Fatalf("%s[0] is not an object", key)
	}
	return object
}

// adminShapeServer starts the mock with a level populated the way the capture's
// game was, so every collection in the reference has a counterpart.
func adminShapeServer(t *testing.T) *httptest.Server {
	t.Helper()
	s := newMockServer(t)
	c := adminClientFor(t, s)
	ctx := context.Background()

	if err := c.AdminCreateTask(ctx, mockGameID, 1, adminShapeTask); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	if err := c.AdminCreateHint(ctx, mockGameID, 1, adminShapeHint); err != nil {
		t.Fatalf("seed hint: %v", err)
	}
	if err := c.AdminCreateHint(ctx, mockGameID, 1, adminShapePenaltyHint); err != nil {
		t.Fatalf("seed penalty hint: %v", err)
	}
	if err := c.AdminCreateBonus(ctx, mockGameID, 1, adminShapeBonus); err != nil {
		t.Fatalf("seed bonus: %v", err)
	}
	if err := c.AdminCreateSector(ctx, mockGameID, 1, adminShapeSector); err != nil {
		t.Fatalf("seed sector: %v", err)
	}
	if err := c.AdminCreateMessage(ctx, mockGameID, s.admin.levels[0].id, adminShapeMessage); err != nil {
		t.Fatalf("seed message: %v", err)
	}

	legacyMux := s.routes()
	apiMux := http.NewServeMux()
	s.registerNewAPIRoutes(apiMux, legacyMux)
	s.registerAdminAPIRoutes(apiMux)
	srv := httptest.NewTestServer(t, withCommonHeaders(newEngineFallbackMux(apiMux, legacyMux)))
	srv.Start()
	return srv
}

// TestMockAdminLevelEditorMatchesTheLiveShape is the important one: the level
// editor is the document every level read goes through, and it is where the
// signed autopass award and the sector completion rule live.
func TestMockAdminLevelEditorMatchesTheLiveShape(t *testing.T) {
	srv := adminShapeServer(t)
	reference := loadLiveReference(t, "admin_level_editor")

	levels := mockJSON(t, srv, fmt.Sprintf("/admin/games/%d/levels", mockGameID))
	first := firstObject(t, levels, "levels")
	if first == nil {
		t.Fatal("the mock game has no levels")
	}
	levelID := int(first["level_id"].(float64))
	actual := mockJSON(t, srv, fmt.Sprintf("/admin/games/%d/levels/%d/editor", mockGameID, levelID))

	if extra := extraKeys(reference, actual); len(extra) > 0 {
		t.Errorf("the mock invents fields the engine does not send: %v", extra)
	}

	// The fields the client actually reads must all be present; the rest of the
	// engine's document (quiz settings, redquest) is deliberately not modelled.
	required := []string{
		"game_id", "game_num", "title", "level", "levels",
		"tasks", "helps", "penalty_helps", "bonuses", "sectors", "answers", "messages",
		"timeout_sec", "timeout_time_award_sec", "attempts_number", "attempts_period_sec",
		"block_type_id", "passing_condition_id", "required_sectors_count",
	}
	for _, key := range required {
		if _, ok := reference[key]; !ok {
			t.Fatalf("the reference itself lacks %q — the capture is stale", key)
		}
		if _, ok := actual[key]; !ok {
			t.Errorf("the mock does not serve %q", key)
		}
	}

	// Every collection's rows must carry the same field names as the engine's.
	for _, collection := range []string{"tasks", "helps", "penalty_helps", "bonuses", "sectors", "answers", "messages"} {
		want := firstObject(t, reference, collection)
		got := firstObject(t, actual, collection)
		if want == nil {
			t.Fatalf("the reference has no %s to compare against", collection)
		}
		if got == nil {
			t.Errorf("the mock serves no %s", collection)
			continue
		}
		if extra := extraKeys(want, got); len(extra) > 0 {
			t.Errorf("%s rows carry fields the engine does not send: %v", collection, extra)
		}
		if missing := missingKeys(want, got); len(missing) > 0 {
			t.Errorf("%s rows are missing engine fields: %v", collection, missing)
		}
	}
}

func TestMockAdminDocumentsMatchTheLiveShape(t *testing.T) {
	srv := adminShapeServer(t)

	cases := []struct {
		name     string
		path     string
		required []string
	}{
		{"admin_levels", fmt.Sprintf("/admin/games/%d/levels", mockGameID),
			[]string{"game_id", "levels", "can_manipulate_levels", "started"}},
		{"admin_game_editor", fmt.Sprintf("/admin/games/%d", mockGameID),
			[]string{"game", "authors"}},
		{"admin_game_lifecycle", fmt.Sprintf("/admin/games/%d/lifecycle", mockGameID),
			[]string{"game_id", "started", "can_calculate_points", "can_close_rate", "can_calculate_qi"}},
		{"game_monitoring", fmt.Sprintf("/games/%d/monitoring?tab=0", mockGameID),
			[]string{"game_id", "can_view", "actions", "total_pages", "page"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reference := loadLiveReference(t, tc.name)
			actual := mockJSON(t, srv, tc.path)

			if extra := extraKeys(reference, actual); len(extra) > 0 {
				t.Errorf("the mock invents fields the engine does not send: %v", extra)
			}
			for _, key := range tc.required {
				if _, ok := reference[key]; !ok {
					t.Fatalf("the reference itself lacks %q — the capture is stale", key)
				}
				if _, ok := actual[key]; !ok {
					t.Errorf("the mock does not serve %q", key)
				}
			}
		})
	}
}

// The game object inside the editor is what AdminGetGameInfo reads field by
// field, so its names are checked on their own.
func TestMockAdminGameObjectMatchesTheLiveShape(t *testing.T) {
	srv := adminShapeServer(t)
	reference := loadLiveReference(t, "admin_game_editor")
	actual := mockJSON(t, srv, fmt.Sprintf("/admin/games/%d", mockGameID))

	want, _ := reference["game"].(map[string]any)
	got, _ := actual["game"].(map[string]any)
	if want == nil || got == nil {
		t.Fatal("both documents must carry a game object")
	}
	if extra := extraKeys(want, got); len(extra) > 0 {
		t.Errorf("the mock's game object invents fields: %v", extra)
	}
	for _, key := range []string{
		"id", "title", "descr", "prize", "afc", "max_players", "max_team_members",
		"is_moderated", "show_finish_place", "stat_availability_type_id",
		"scenario_availability", "show_fee", "certificate_access_mode",
		"certificate_places", "finish_date_time", "request_last_date",
		"accept_rate_from_date_time", "started",
	} {
		if _, ok := want[key]; !ok {
			t.Fatalf("the reference game object lacks %q — the capture is stale", key)
		}
		if _, ok := got[key]; !ok {
			t.Errorf("the mock's game object does not serve %q", key)
		}
	}

	authors := firstObject(t, actual, "authors")
	if authors == nil {
		t.Fatal("the mock serves no authors")
	}
	if _, ok := authors["login"]; !ok {
		t.Error("an author row carries no login")
	}
}

// The signed autopass award is the field a mock is most likely to get wrong,
// because the sign is the whole meaning. It is pinned against the live value.
func TestMockAdminAutopassAwardIsSignedLikeTheEngine(t *testing.T) {
	reference := loadLiveReference(t, "admin_level_editor")
	award, ok := reference["timeout_time_award_sec"].(float64)
	if !ok {
		t.Fatal("the reference carries no timeout_time_award_sec")
	}
	if award >= 0 {
		t.Fatalf("the capture recorded %v; it was taken with a penalty configured, "+
			"so a non-negative value means the sign convention changed", award)
	}

	srv := adminShapeServer(t)
	levels := mockJSON(t, srv, fmt.Sprintf("/admin/games/%d/levels", mockGameID))
	levelID := int(firstObject(t, levels, "levels")["level_id"].(float64))
	path := fmt.Sprintf("/admin/games/%d/levels/%d", mockGameID, levelID)

	body := strings.NewReader(`{"timeout_hours":1,"timeout_minutes":30,"timeout_seconds":0,
	  "penalty_enabled":true,"penalty_hours":0,"penalty_minutes":15,"penalty_seconds":0,
	  "award_is_penalty":true}`)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPut, srv.URL+path+"/autopass", body)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("PUT autopass: %v", err)
	}
	_ = resp.Body.Close()

	editor := mockJSON(t, srv, path+"/editor")
	if got := editor["timeout_time_award_sec"].(float64); got != -900 {
		t.Errorf("timeout_time_award_sec = %v after a 15-minute penalty, want -900", got)
	}
}

// The seed mirrors what the capture's game contained, so every collection in
// the reference document has a row to compare against.
var (
	adminShapeTask = encx.AdminTask{Text: "Задание уровня", ReplaceNl: true}
	adminShapeHint = encx.AdminHint{Text: "Обычная подсказка", Hours: 1, Minutes: 2, Seconds: 3}

	adminShapePenaltyHint = encx.AdminHint{
		Text: "Штрафная подсказка", Minutes: 5, IsPenalty: true,
		PenaltyMinutes: 10, PenaltyComment: "минус десять минут", RequestConfirm: true,
	}
	adminShapeBonus = encx.AdminBonus{
		Name: "Бонус уровня", Task: "Найдите знак", Hint: "Он у входа",
		Answers: []string{"бонус1", "бонус2"}, LevelID: adminLevelIDBase + 1,
		AwardMinutes: 7, DelayMinutes: 3, WorkMinutes: 9,
	}
	adminShapeSector  = encx.AdminSector{Name: "Сектор А", Answers: []string{"альфа", "бета"}}
	adminShapeMessage = encx.AdminGameMessage{Text: "Сообщение организаторов", ReplaceNlToBr: true}
)
