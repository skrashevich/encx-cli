package encxmobile

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExplicitAPIBaseLoadsSnakeCaseGame(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /sites/domain/encounter.svk.bar", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":1,"primary_domain":"encounter.svk.bar","is_site_active_by_rule":true}`))
	})
	mux.HandleFunc("GET /gameengines/encounter/play/424242", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"event":0,"game_id":424242,"game_title":"Review","level":{"level_id":1400001,"number":1,"name":"Welcome","tasks":[{"task_id":1,"task_text_formatted":"Task"}],"sectors":[{"sector_id":3010001,"name":"Sector"}]}}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	client := NewClientWithAPIOptions("encounter.svk.bar", false, false, 3, "ru", server.URL)
	result, err := client.GetGameModel(424242)
	if err != nil {
		t.Fatal(err)
	}
	var model struct {
		GameID int `json:"GameId"`
		Level  struct {
			ID    int   `json:"LevelId"`
			Tasks []any `json:"Tasks"`
		} `json:"Level"`
	}
	if err := json.Unmarshal([]byte(result), &model); err != nil {
		t.Fatal(err)
	}
	if model.GameID != 424242 || model.Level.ID != 1400001 || len(model.Level.Tasks) != 1 {
		t.Fatalf("lost game data: %s", result)
	}
	if client.Engine() != "new" {
		t.Fatalf("engine = %s", client.Engine())
	}
}

func TestEmptyAPIBaseKeepsLegacyCustomDomain(t *testing.T) {
	client := NewClientWithAPIOptions("legacy.example", false, false, 3, "ru", "")
	if client.APIBaseURL() != "" || client.Engine() != "legacy" {
		t.Fatalf("unexpected backend: %q %q", client.APIBaseURL(), client.Engine())
	}
}
