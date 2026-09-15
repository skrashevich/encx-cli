package main

import (
	"encoding/json/v2"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/skrashevich/encx-cli/encx/scenario"
)

func TestScenarioImportToolIsAvailableAndGuarded(t *testing.T) {
	found := false
	for _, tool := range getTools() {
		if tool.Function.Name == "admin_import_scenario" {
			found = true
		}
	}
	if !found {
		t.Fatal("agent cannot use the deterministic HTML importer")
	}
	if !isMutationTool("admin_import_scenario") {
		t.Fatal("import bypasses mutation policy")
	}
	for _, tool := range getToolsForSession(&llmSession{securityMode: SecurityModeReadonly}) {
		if tool.Function.Name == "admin_import_scenario" {
			t.Fatal("import exposed in read-only mode")
		}
	}
}

func TestScenarioVerificationDetectsPartialImport(t *testing.T) {
	src := &scenario.Document{Levels: []scenario.Level{{Number: 1, Name: "Source", Tasks: []string{"task"}, Hints: []scenario.Hint{{Text: "hint", DelaySeconds: 60}}, Sectors: []scenario.Sector{{Name: "A", Answers: []string{"code"}}}, Bonuses: []scenario.Bonus{{Name: "B", Answers: []string{"bonus"}, AwardSeconds: 90}}, Comment: "author note", AutopassSecond: 300}}}
	actual := &scenario.Document{Levels: []scenario.Level{{Number: 1, Name: "Level #1"}}}
	diffs := scenarioDifferences(src, actual)
	if len(diffs) != 1 {
		t.Fatalf("differences=%v", diffs)
	}
	for _, field := range []string{"name", "tasks", "hints", "sectors", "bonuses", "comment", "autopass"} {
		if !strings.Contains(strings.Join(diffs, " "), field) {
			t.Errorf("missing %s: %v", field, diffs)
		}
	}
	if diffs := scenarioDifferences(src, src); len(diffs) != 0 {
		t.Fatalf("identical scenario rejected: %v", diffs)
	}
	actual.Levels = append(actual.Levels, scenario.Level{Number: 2})
	if !strings.Contains(strings.Join(scenarioDifferences(src, actual), " "), "count") {
		t.Fatal("extra levels ignored")
	}
}

func TestScenarioInspectReturnsSummaryNotHTML(t *testing.T) {
	root := t.TempDir()
	t.Setenv("LLM_FILES_ROOT", root)
	html := `<a id="LevelsScenarioRepeater_ctl00_lnkLevelAnchorPoint" name="1"></a>Уровень №1 "Start"<div class="scenarioBlock border_dark"><span id="LevelsScenarioRepeater_ctl00_LevelTasksRepeater_ctl00_lblLevelTask">` + strings.Repeat("task ", 4000) + `</span></div>`
	path := filepath.Join(root, "scenario.html")
	if err := os.WriteFile(path, []byte(html), 0600); err != nil {
		t.Fatal(err)
	}
	raw := executeToolCallSafe(t.Context(), &config{}, nil, &llmSession{}, "inspect_scenario_file", `{"path":"scenario.html"}`)
	if strings.Contains(raw, `"error"`) {
		t.Fatal(raw)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatal(err)
	}
	if result["levels"] != float64(1) || result["tasks"] != float64(1) || len(raw) > 2000 {
		t.Fatalf("not a compact parsed summary: %.500s", raw)
	}
}

func TestAgentImportDoesNotWaitOnStdinAfterNetworkFailure(t *testing.T) {
	oldMode := agentMode
	agentMode = true
	defer func() { agentMode = oldMode }()
	old := waitForTransientRetry
	waitForTransientRetry = func(string, error) error { t.Fatal("agent waited for terminal input"); return nil }
	defer func() { waitForTransientRetry = old }()
	calls := 0
	err := runWithAntiSpamRetry("create level", func() error { calls++; return io.ErrUnexpectedEOF })
	if err == nil || calls != 1 {
		t.Fatalf("ambiguous write retried: calls=%d error=%v", calls, err)
	}
}

func TestScenarioPreservesRepeatedAndEmptySectors(t *testing.T) {
	src := scenario.Level{Sectors: []scenario.Sector{{Name: "Формат: два слова", Answers: []string{"первый ответ"}}, {Name: "Формат: два слова", Answers: []string{"второй ответ"}}, {Name: "Пустой сектор"}}}
	got := scenarioAdminSectors(src)
	if len(got) != 3 {
		t.Fatalf("explicit empty sector lost: %v", got)
	}
	if !sectorGroupsMatch(src, got) {
		t.Fatal("repeated names from source are not duplicate imports")
	}
	if sectorGroupsMatch(src, got[:2]) {
		t.Fatal("missing empty sector not detected")
	}
	repeated := scenario.Level{Sectors: src.Sectors[:2]}
	if !sectorGroupsMatch(repeated, scenarioAdminSectors(repeated)) {
		t.Fatal("legitimate repeated sector labels rejected")
	}
}

func TestScenarioPreservesEmptyTimedBonus(t *testing.T) {
	bonus, ok := scenarioBonusToAdminBonus(scenario.Bonus{Number: 7, AwardSeconds: 1}, 42)
	if !ok || bonus.AwardSeconds != 1 {
		t.Fatal("empty timed bonus from source was discarded")
	}
}
