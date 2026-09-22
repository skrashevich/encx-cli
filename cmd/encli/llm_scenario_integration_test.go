package main

import (
	"context"
	"encoding/json/v2"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/skrashevich/encx-cli/encx"
)

// Opt in with a local encx-mock server and a GameScenario HTML file. Never
// accepts a remote host: this creates and edits a disposable test game.
func TestScenarioImportRoundTripLocal(t *testing.T) {
	endpoint, path := os.Getenv("ENCLI_IMPORT_TEST_URL"), os.Getenv("ENCLI_IMPORT_TEST_SCENARIO")
	if endpoint == "" || path == "" {
		t.Skip("set ENCLI_IMPORT_TEST_URL and ENCLI_IMPORT_TEST_SCENARIO")
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "http" || u.Hostname() != "127.0.0.1" {
		t.Fatal("only a loopback mock is allowed")
	}
	c := encx.New(u.Host, encx.WithHTTP(), encx.WithEngine(encx.EngineNew), encx.WithAPIBaseURL(endpoint), encx.WithAdminDelay(0))
	if err = c.LoginComplete(t.Context(), "import-test", "import-test"); err != nil {
		t.Fatal(err)
	}
	id := 424242 // the disposable game provided by encx-mock
	levels, err := c.AdminGetLevels(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	for i := len(levels) - 1; i >= 0; i-- {
		if err := c.AdminDeleteLevel(t.Context(), id, levels[i].Number); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &config{domain: u.Host, gameId: id, jsonOutput: true}
	var statuses []string
	var raw string
	runWithToolProgress(t.Context(), AgentCallbacks{OnStatus: func(_, message string) {
		statuses = append(statuses, message)
	}}, &llmSession{preferRussian: true}, "admin_import_scenario", 5*time.Second, func(ctx context.Context) string {
		raw = captureStdout(t, func() { toolImportScenario(ctx, cfg, c, path) })
		return raw
	})
	for _, stage := range []string{"Чтение файла", "Чтение существующих", "Импорт уровня", "секторы и ответы", "Завершён уровень", "Проверка сценария"} {
		if !strings.Contains(strings.Join(statuses, "\n"), stage) {
			t.Errorf("missing progress stage %q in %v", stage, statuses)
		}
	}
	var result struct {
		Success  bool            `json:"success"`
		Verified bool            `json:"verified"`
		Stats    importSyncStats `json:"stats"`
	}
	if err = json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Success || !result.Verified {
		t.Fatalf("round trip failed: %s", raw)
	}
	t.Log(raw)
	// A stale nonzero timeout must also be cleared when absent in the source.
	doc, err := readAgentScenario(path)
	if err != nil {
		t.Fatal(err)
	}
	for i, level := range doc.Levels {
		if level.AutopassSecond != 0 {
			continue
		}
		if err := c.AdminUpdateAutopass(t.Context(), id, i+1, encx.AdminLevelSettings{AutopassMinutes: 59}); err != nil {
			t.Fatal(err)
		}
		raw = captureStdout(t, func() { toolImportScenario(t.Context(), cfg, c, path) })
		if err := json.Unmarshal([]byte(raw), &result); err != nil {
			t.Fatal(err)
		}
		if !result.Verified || result.Stats.AutopassUpdated != 1 {
			t.Fatalf("stale timeout not repaired: %s", raw)
		}
		break
	}

	raw = captureStdout(t, func() { toolImportScenario(t.Context(), cfg, c, path) })
	if err = json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Success || !result.Verified || result.Stats != (importSyncStats{}) {
		t.Fatalf("second import changed existing content: %s", raw)
	}
	if strings.Contains(raw, `"error"`) {
		t.Fatal(raw)
	}
}
