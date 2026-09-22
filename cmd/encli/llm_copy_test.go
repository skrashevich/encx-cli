package main

import (
	"encoding/json/v2"
	"fmt"
	"github.com/skrashevich/encx-cli/encx"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestCopyGameSourceDomainSchema(t *testing.T) {
	foundInspect := false
	for _, tool := range getTools() {
		if tool.Function.Name == "inspect_game_scenario" {
			foundInspect = true
		}
		if tool.Function.Name != "admin_copy_game" {
			continue
		}
		var schema struct {
			Properties map[string]any `json:"properties"`
		}
		if err := json.Unmarshal(tool.Function.Parameters, &schema); err != nil {
			t.Fatal(err)
		}
		if schema.Properties["source_domain"] == nil {
			t.Error("copy cannot address a source on another domain")
		}
	}
	if !foundInspect {
		t.Error("agent cannot inspect source before creating target")
	}
}

func TestCopyGameInvalidIDsPreserveTargetContext(t *testing.T) {
	for _, args := range []string{
		`{"source_game_id":82864,"target_game_id":0,"source_domain":"svk.en.cx"}`,
		`{"source_game_id":0,"target_game_id":123,"source_domain":"svk.en.cx"}`,
		`{"source_game_id":123,"target_game_id":123}`,
	} {
		cfg := &config{domain: "tech.en.cx", gameId: 123}
		result := executeToolCallSafe(t.Context(), cfg, nil, &llmSession{}, "admin_copy_game", args)
		if !strings.Contains(result, `"error"`) {
			t.Fatalf("invalid copy accepted: %s", result)
		}
		if cfg.domain != "tech.en.cx" || cfg.gameId != 123 {
			t.Fatalf("target context overwritten: %+v", cfg)
		}
	}
}

func TestInspectGameScenarioUsesSourceDomainAndSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	sourceReads := 0
	sourceServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-En-Domain") != "svk.en.cx" || r.Header.Get("Authorization") != "Bearer source-token" {
			t.Errorf("wrong source domain or session: %s, %s", r.Header.Get("X-En-Domain"), r.Header.Get("Authorization"))
		}
		if r.Method != http.MethodGet {
			t.Errorf("source was modified: %s", r.Method)
		}
		switch r.URL.Path {
		case "/auth/session":
			fmt.Fprint(w, `{}`)
		case "/games/82864/scenario":
			sourceReads++
			fmt.Fprint(w, `{"game":{"id":82864,"title":"Source game"},"levels":[{"level_id":1,"level_number":1,"level_name":"Start","tasks":[{"task_id":1,"task_text":"Source task"}]}]}`)
		default:
			t.Errorf("unexpected source path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer sourceServer.Close()
	sourceCfg := &config{domain: "svk.en.cx"}
	source := encx.New("svk.en.cx", encx.WithEngine(encx.EngineNew), encx.WithAPIBaseURL(sourceServer.URL))
	if err := source.ImportCookies([]byte(`{"apiToken":"source-token"}`)); err != nil {
		t.Fatal(err)
	}
	saveSession(sourceCfg, source)
	cfg := &config{domain: "tech.en.cx", gameId: 42, engine: "new", apiBaseURL: sourceServer.URL}
	raw := executeToolCallSafe(t.Context(), cfg, nil, &llmSession{}, "inspect_game_scenario", `{"source_domain":"https://svk.en.cx/","source_game_id":82864}`)
	if strings.Contains(raw, `"error"`) || !strings.Contains(raw, `"Source game"`) || sourceReads != 1 {
		t.Fatalf("source inspection failed: %s (reads=%d)", raw, sourceReads)
	}
	if cfg.domain != "tech.en.cx" || cfg.gameId != 42 {
		t.Fatal("inspection changed destination")
	}
	if endpoint := os.Getenv("ENCLI_COPY_TEST_URL"); endpoint != "" {
		u, err := url.Parse(endpoint)
		if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" {
			t.Fatal("only a loopback mock is allowed")
		}
		target := encx.New("tech.en.cx", encx.WithEngine(encx.EngineNew), encx.WithAPIBaseURL(endpoint), encx.WithAdminDelay(0))
		if err := target.LoginComplete(t.Context(), "copy-test", "copy-test"); err != nil {
			t.Fatal(err)
		}
		levels, err := target.AdminGetLevels(t.Context(), 424242)
		if err != nil {
			t.Fatal(err)
		}
		for i := len(levels) - 1; i >= 0; i-- {
			if err := target.AdminDeleteLevel(t.Context(), 424242, levels[i].Number); err != nil {
				t.Fatal(err)
			}
		}
		cfg.login, cfg.password = "copy-test", "copy-test"
		for attempt := range 2 {
			raw = executeToolCallSafe(t.Context(), cfg, target, &llmSession{}, "admin_copy_game", `{"source_domain":"svk.en.cx","source_game_id":82864,"target_game_id":424242}`)
			var result struct {
				Success  bool            `json:"success"`
				Verified bool            `json:"verified"`
				Stats    importSyncStats `json:"stats"`
			}
			if err := json.Unmarshal([]byte(raw), &result); err != nil {
				t.Fatal(err)
			}
			if !result.Success || !result.Verified {
				t.Fatalf("cross-domain copy failed: %s", raw)
			}
			if attempt == 1 && result.Stats != (importSyncStats{}) {
				t.Fatalf("repeat copy changed target: %s", raw)
			}
			t.Log(raw)
		}
		actual, err := target.GetAdminGameScenario(t.Context(), 424242)
		if err != nil {
			t.Fatal(err)
		}
		if len(actual.Levels) != 1 || len(actual.Levels[0].Tasks) != 1 || actual.Levels[0].Tasks[0] != "Source task" {
			t.Fatalf("wrong destination content: %+v", actual)
		}
		if cfg.domain != "tech.en.cx" || cfg.gameId != 42 {
			t.Fatal("copy changed original context")
		}
	}

}

func TestCopyGameSourceFailureDoesNotTouchTarget(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := &config{domain: "tech.en.cx", gameId: 42}
	raw := executeToolCallSafe(t.Context(), cfg, nil, &llmSession{}, "admin_copy_game", `{"source_domain":"svk.en.cx","source_game_id":82864,"target_game_id":42}`)
	if !strings.Contains(raw, "No saved session for source domain svk.en.cx") {
		t.Fatal(raw)
	}
	if cfg.gameId != 42 || cfg.domain != "tech.en.cx" {
		t.Fatal("source failure changed destination")
	}
}

func TestScenarioSourceDomainValidation(t *testing.T) {
	for _, raw := range []string{"svk.en.cx", "https://SVK.en.cx/", "http://svk.en.cx"} {
		domain, err := scenarioSourceDomain(raw, "tech.en.cx")
		if err != nil || domain != "svk.en.cx" {
			t.Fatalf("%q: %s %v", raw, domain, err)
		}
	}
	for _, raw := range []string{"svk.en.cx.evil.example", "https://svk.en.cx/path", "https://user@svk.en.cx", "file:///svk.en.cx", "../svk.en.cx"} {
		if _, err := scenarioSourceDomain(raw, "tech.en.cx"); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
}
