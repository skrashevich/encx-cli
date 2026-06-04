package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/skrashevich/encx-cli/encx"
	"github.com/skrashevich/encx-cli/encx/scenario"
)

func TestParseScenarioFile(t *testing.T) {
	dir := t.TempDir()
	assetsDir := filepath.Join(dir, "scenario_files")
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		t.Fatalf("mkdir assets: %v", err)
	}
	assetPath := filepath.Join(assetsDir, "pic.png")
	// Minimal PNG signature bytes are enough for MIME detection.
	if err := os.WriteFile(assetPath, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, 0o644); err != nil {
		t.Fatalf("write asset: %v", err)
	}

	htmlDoc := `
<a id="LevelsScenarioRepeater_ctl00_lnkLevelAnchorPoint" name="1"></a>
Уровень №1 "Бриф"
<div class="scenarioBlock border_dark">
  <div class="white bold">Автопереход: через 3 минуты<br><br></div>
  <span id="LevelsScenarioRepeater_ctl00_LevelTasksRepeater_ctl00_lblLevelTask">Текст<br><img src="./scenario_files/pic.png"></span>
  <span id="LevelsScenarioRepeater_ctl00_LevelHelpsRepeater_ctl00_lblLevelHelpTitle">Подсказка №1 для всех (1 час 5 минут)</span><br>
  <span id="LevelsScenarioRepeater_ctl00_LevelHelpsRepeater_ctl00_lblLevelHelp">Текст подсказки</span>
  <span id="LevelsScenarioRepeater_ctl00_SectorsRepeater_ctl00_LevelAnswersRepeater_ctl00_lblLevelAnswer"><span class="nonLatinChar">код1</span>66</span> - <span id="LevelsScenarioRepeater_ctl00_SectorsRepeater_ctl00_LevelAnswersRepeater_ctl00_lblAnswerFor">для всех</span>
  <span id="LevelsScenarioRepeater_ctl00_SectorsRepeater_ctl00_LevelAnswersRepeater_ctl01_lblLevelAnswer">код2</span> - <span id="LevelsScenarioRepeater_ctl00_SectorsRepeater_ctl00_LevelAnswersRepeater_ctl01_lblAnswerFor">для всех</span>
</div>
<a id="LevelsScenarioRepeater_ctl01_lnkLevelAnchorPoint" name="2"></a>
Уровень №2 "Финиш"
<div class="scenarioBlock border_dark">
  <span id="LevelsScenarioRepeater_ctl01_LevelTasksRepeater_ctl00_lblLevelTask">Второй</span>
  <span id="LevelsScenarioRepeater_ctl01_SectorsRepeater_ctl00_LevelAnswersRepeater_ctl00_lblLevelAnswer">answer</span>
</div>
`
	docPath := filepath.Join(dir, "game scenario.html")
	if err := os.WriteFile(docPath, []byte(htmlDoc), 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}

	scenarioDoc, err := scenario.ParseFile(docPath)
	if err != nil {
		t.Fatalf("ParseFile error: %v", err)
	}
	if len(scenarioDoc.Levels) != 2 {
		t.Fatalf("expected 2 levels, got %d", len(scenarioDoc.Levels))
	}

	l1 := scenarioDoc.Levels[0]
	if l1.Number != 1 || l1.Name != "Бриф" {
		t.Fatalf("unexpected level #1 metadata: %+v", l1)
	}
	if l1.AutopassSecond != 180 {
		t.Fatalf("expected autopass 180s, got %d", l1.AutopassSecond)
	}
	if len(l1.Hints) != 1 || l1.Hints[0].DelaySeconds != 3900 {
		t.Fatalf("unexpected hint parse: %+v", l1.Hints)
	}
	if len(l1.SectorAnswers) != 1 || len(l1.SectorAnswers[0]) != 2 {
		t.Fatalf("unexpected sector answers: %+v", l1.SectorAnswers)
	}
	if l1.SectorAnswers[0][0] != "код166" {
		t.Fatalf("expected first code to preserve suffix, got %q", l1.SectorAnswers[0][0])
	}
	if scenarioDoc.EmbeddedAssets != 1 {
		t.Fatalf("expected embedded asset count 1, got %d", scenarioDoc.EmbeddedAssets)
	}
	if len(l1.Tasks) == 0 || !strings.Contains(l1.Tasks[0], "data:image/png;base64,") {
		t.Fatalf("expected embedded data URL in task, got: %q", strings.Join(l1.Tasks, "\n"))
	}
}

func TestParseRuDuration(t *testing.T) {
	tests := map[string]int{
		"3 минуты":              180,
		"1 час 5 минут":         3900,
		"2 часа 45 минут":       9900,
		"1 день 1 час 1 минута": 90060,
	}
	for input, expected := range tests {
		got := scenario.ParseRuDuration(input)
		if got != expected {
			t.Fatalf("parseRuDuration(%q) = %d, expected %d", input, got, expected)
		}
	}
}

func TestParseImportScenarioArgs(t *testing.T) {
	opts, err := parseImportScenarioArgs([]string{"--dry-run", "/tmp/scenario.html"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !opts.DryRun {
		t.Fatalf("expected DryRun=true")
	}
	if opts.SourcePath != "/tmp/scenario.html" {
		t.Fatalf("unexpected source path: %q", opts.SourcePath)
	}
	opts, err = parseImportScenarioArgs([]string{"--sync-missing", "/tmp/scenario.html"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !opts.SyncMissing {
		t.Fatalf("expected SyncMissing=true")
	}

	if _, err := parseImportScenarioArgs([]string{"--unknown", "/tmp/scenario.html"}); err == nil {
		t.Fatalf("expected error for unknown flag")
	}
	if _, err := parseImportScenarioArgs([]string{}); err == nil {
		t.Fatalf("expected error for missing path")
	}
}

func TestImportScenarioNeedsAdmin(t *testing.T) {
	cfg := &config{}
	if importScenarioNeedsAdmin(cfg, []string{"--dry-run", "/tmp/scenario.html"}) {
		t.Fatal("plain dry-run must not require admin access")
	}
	if importScenarioNeedsAdmin(cfg, []string{"--dry-run", "--sync-missing", "/tmp/scenario.html"}) {
		t.Fatal("sync dry-run must only parse the scenario")
	}
	if !importScenarioNeedsAdmin(cfg, []string{"/tmp/scenario.html"}) {
		t.Fatal("real import must require admin access")
	}
}

func TestRunWithAntiSpamRetryTimeoutThenSuccess(t *testing.T) {
	origWait := waitForTransientRetry
	defer func() { waitForTransientRetry = origWait }()

	retryCalls := 0
	waitForTransientRetry = func(opName string, err error) error {
		retryCalls++
		return nil
	}

	calls := 0
	err := runWithAntiSpamRetry("test timeout", func() error {
		calls++
		if calls == 1 {
			return context.DeadlineExceeded
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if retryCalls != 1 {
		t.Fatalf("expected 1 transient retry prompt, got %d", retryCalls)
	}
	if calls != 2 {
		t.Fatalf("expected 2 calls, got %d", calls)
	}
}

func TestSplitIntoBatches(t *testing.T) {
	tests := []struct {
		total int
		max   int
		want  []int
	}{
		{total: 0, max: 30, want: nil},
		{total: 1, max: 30, want: []int{1}},
		{total: 30, max: 30, want: []int{30}},
		{total: 31, max: 30, want: []int{30, 1}},
		{total: 75, max: 30, want: []int{30, 30, 15}},
	}
	for _, tc := range tests {
		got := splitIntoBatches(tc.total, tc.max)
		if len(got) != len(tc.want) {
			t.Fatalf("splitIntoBatches(%d,%d) len=%d want=%d", tc.total, tc.max, len(got), len(tc.want))
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("splitIntoBatches(%d,%d)[%d]=%d want=%d", tc.total, tc.max, i, got[i], tc.want[i])
			}
		}
	}
}

func TestNormalizeComparableText(t *testing.T) {
	in := "  a \r\n b\tc  "
	got := scenario.NormalizeComparableText(in)
	if got != "a b c" {
		t.Fatalf("normalizeComparableText got %q", got)
	}
}

func TestRedactBinaryPayloads(t *testing.T) {
	in := `<img src="data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAUA">`
	got := redactBinaryPayloads(in)
	if strings.Contains(got, "iVBORw0KGgo") {
		t.Fatalf("expected base64 to be redacted, got %q", got)
	}
	if !strings.Contains(got, "[embedded image/png omitted in dry-run]") {
		t.Fatalf("expected redaction marker, got %q", got)
	}
}

func TestSectorGroupsMatch(t *testing.T) {
	scenario := [][]string{{"поехалистрадать66"}, {"другойкод"}}
	game := []encx.AdminSector{
		{Answers: []string{"поехалистрадать"}},
		{Answers: []string{"другойкод"}},
	}
	if sectorGroupsMatch(scenario, game) {
		t.Fatal("truncated answer must not match scenario")
	}
	game[0].Answers = []string{"поехалистрадать66"}
	if !sectorGroupsMatch(scenario, game) {
		t.Fatal("expected match when sector answers equal scenario")
	}
}

func TestSectorGroupsMatchRejectsCombinedOrExtraAnswers(t *testing.T) {
	want := [][]string{{"alpha", "beta"}}
	if sectorGroupsMatch(want, []encx.AdminSector{{ID: 1, Answers: []string{"alpha beta"}}}) {
		t.Fatal("combined answer must not satisfy strict scenario sync")
	}
	if sectorGroupsMatch(want, []encx.AdminSector{{ID: 1, Answers: []string{"alpha", "beta", "extra"}}}) {
		t.Fatal("extra answer must not satisfy strict scenario sync")
	}
}

func TestSectorGroupsMatchIgnoresEmptyStartedSectors(t *testing.T) {
	scenario := [][]string{{"поехалистрадать66"}}
	game := []encx.AdminSector{
		{ID: 3486043, Name: "Сектор 1"},
		{ID: 3486323, Name: "Сектор 1 (import missing)", Answers: []string{"поехалистрадать66"}},
		{ID: 3486975, Name: "Сектор 3486043"},
	}
	if !sectorGroupsMatch(scenario, game) {
		t.Fatal("empty sectors left by started teams must not count as scenario answers")
	}
}

func TestGameSectorsAnomalous(t *testing.T) {
	game := []encx.AdminSector{
		{Name: "Сектор 1", Answers: []string{"поехалистрадать"}},
		{Name: "Сектор 1", Answers: []string{"поехалистрадать66"}},
	}
	if !gameSectorsAnomalous(game) {
		t.Fatal("duplicate sector names must be anomalous")
	}
	scenario := [][]string{{"поехалистрадать66"}}
	if sectorGroupsMatch(scenario, game) {
		t.Fatal("anomalous game sectors must not match scenario")
	}
}

func TestLiveSyncLevel1Sectors82311(t *testing.T) {
	if os.Getenv("ENCX_LIVE") != "1" {
		t.Skip("set ENCX_LIVE=1 to run")
	}
	const scenarioPath = "/Users/svk/Downloads/moscow.en.cx __ Game scenario.html"
	cfg := &config{domain: "tech.en.cx", gameId: 82311}
	client := encx.New(cfg.domain)
	if !loadSession(cfg, client) {
		t.Fatal("no saved session; run encli login")
	}
	scenarioDoc, err := scenario.ParseFile(scenarioPath)
	if err != nil {
		t.Fatal(err)
	}
	stats := &importSyncStats{}
	if err := syncLevelSectorsToScenario(context.Background(), client, cfg.gameId, 1, scenarioDoc.Levels[0], stats); err != nil {
		t.Fatal(err)
	}
	sectors, err := client.AdminGetSectorAnswers(context.Background(), cfg.gameId, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(sectors) != 1 || len(sectors[0].Answers) != 1 || sectors[0].Answers[0] != "поехалистрадать66" {
		t.Fatalf("unexpected sectors after sync: %+v", sectors)
	}
}
