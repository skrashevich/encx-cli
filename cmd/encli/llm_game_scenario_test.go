package main

import (
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/skrashevich/encx-cli/encx"
	"github.com/skrashevich/encx-cli/encx/scenario"
)

func TestAdminGameScenarioReadsCompleteDocument(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	task := strings.Repeat("Полный текст задания. ", 1500) + "END-OF-TASK"
	quoted, err := json.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	reads := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected write: %s", r.Method)
		}
		switch r.URL.Path {
		case "/auth/session":
			fmt.Fprint(w, `{}`)
		case "/games/42/scenario":
			reads++
			fmt.Fprintf(w, `{"game":{"id":42,"title":"Whole game"},"levels":[{"level_id":1,"level_number":1,"level_name":"First","tasks":[{"task_text":%s}],"helps":[{"help_text":"Full hint","timeout":60}],"sectors":[{"sector_name":"Sector","answers":[{"answer_text":"CODE"}]}]},{"level_id":2,"level_number":2,"level_name":"Last","tasks":[{"task_text":"Last task"}]}]}`, quoted)
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	cfg := &config{domain: "test.en.cx", engine: "new", apiBaseURL: server.URL}
	client := encx.New(cfg.domain, encx.WithEngine(encx.EngineNew), encx.WithAPIBaseURL(server.URL))
	if err := client.ImportCookies([]byte(`{"apiToken":"test-token"}`)); err != nil {
		t.Fatal(err)
	}
	saveSession(cfg, client)
	session := &llmSession{securityMode: SecurityModeReadonly}
	raw := executeToolCallSafe(t.Context(), cfg, client, session, "admin_game_scenario", `{"game_id":42}`)
	result := prepareToolResultForLLM("admin_game_scenario", raw)
	var doc scenario.Document
	if err := json.Unmarshal([]byte(result), &doc); err != nil {
		t.Fatalf("%v: %s", err, result)
	}
	if reads != 1 || doc.GameID != 42 || len(doc.Levels) != 2 {
		t.Fatalf("incomplete scenario: reads=%d, result=%s", reads, result)
	}
	if doc.Levels[0].Tasks[0] != task || doc.Levels[1].Tasks[0] != "Last task" || doc.Levels[0].Hints[0].Text != "Full hint" || doc.Levels[0].Sectors[0].Answers[0] != "CODE" {
		t.Fatal("scenario content lost")
	}
	runtime := &picoLegacyToolRuntime{input: &AgentRunInput{Cfg: cfg, Session: session}}
	runtime.afterToolResult("admin_game_scenario", `{"game_id":42}`, result)
	if missing := missingLevelsForContentSummary(session, "покажи сценарий"); len(missing) != 0 || len(session.loadedLevelContent) != 2 {
		t.Fatalf("coverage not recorded: %+v", session.loadedLevelContent)
	}
	if !runtime.repeatedContentRead("admin_game_scenario", `{"game_id":42,"unused":true}`) {
		t.Fatal("repeat read not guarded")
	}
	runtime.afterToolResult("admin_update_task", `{}`, `{"success":true}`)
	if runtime.repeatedContentRead("admin_game_scenario", `{"game_id":42}`) {
		t.Fatal("mutation did not invalidate read guard")
	}
	found := false
	for _, tool := range getToolsForSession(session) {
		if tool.Function.Name == "admin_game_scenario" {
			found = true
		}
	}
	if !found {
		t.Fatal("whole scenario tool unavailable in read-only mode")
	}
}

func TestAdminGameScenarioErrorsDoNotMarkLoaded(t *testing.T) {
	for _, result := range []string{`{"error":"access denied"}`, `{"levels":`, `{"success":true}`} {
		session := &llmSession{}
		recordLevelEnumeration(session, `{"levels":[{"number":1}]}`)
		runtime := &picoLegacyToolRuntime{input: &AgentRunInput{Session: session}}
		runtime.afterToolResult("admin_game_scenario", `{"game_id":42}`, result)
		if len(missingLevelsForContentSummary(session, "сценарий")) != 1 {
			t.Fatalf("invalid result marked loaded: %s", result)
		}
	}
	for _, args := range []string{`{}`, `{"game_id":0}`, `{"game_id":-1}`} {
		result := executeToolCallSafe(t.Context(), &config{}, nil, &llmSession{}, "admin_game_scenario", args)
		if !strings.Contains(result, "game_id must be positive") {
			t.Fatalf("invalid ID accepted: %s", result)
		}
	}
}
