package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/skrashevich/encx-cli/encx"
)

type importedHint struct {
	Title        string `json:"title,omitempty"`
	Text         string `json:"text"`
	DelaySeconds int    `json:"delay_seconds"`
}

type importedLevel struct {
	Number         int            `json:"number"`
	Name           string         `json:"name"`
	AutopassSecond int            `json:"autopass_seconds,omitempty"`
	Tasks          []string       `json:"tasks,omitempty"`
	Hints          []importedHint `json:"hints,omitempty"`
	SectorAnswers  [][]string     `json:"sector_answers,omitempty"`
}

type importedScenario struct {
	SourcePath     string          `json:"source_path"`
	Levels         []importedLevel `json:"levels"`
	EmbeddedAssets int             `json:"embedded_assets"`
	MissingAssets  []string        `json:"missing_assets,omitempty"`
}

type importScenarioOptions struct {
	SourcePath  string
	DryRun      bool
	SyncMissing bool
}

type assetRewriteState struct {
	baseDir       string
	cache         map[string]string
	embeddedCount int
	missingSet    map[string]struct{}
}

type importSyncStats struct {
	LevelsCreated   int `json:"levels_created"`
	NamesUpdated    int `json:"names_updated"`
	AutopassUpdated int `json:"autopass_updated"`
	TasksDeleted    int `json:"tasks_deleted,omitempty"`
	TasksCreated    int `json:"tasks_created"`
	HintsDeleted    int `json:"hints_deleted,omitempty"`
	HintsCreated    int `json:"hints_created"`
	SectorsDeleted  int `json:"sectors_deleted,omitempty"`
	SectorsCreated  int `json:"sectors_created"`
}

var (
	levelAnchorRe = regexp.MustCompile(`(?is)<a id="LevelsScenarioRepeater_ctl\d+_lnkLevelAnchorPoint" name="\d+"></a>`)
	levelTitleRe  = regexp.MustCompile(`(?is)Уровень №\s*(\d+)\s*(?:"([^"]*)")?`)
	autopassRe    = regexp.MustCompile(`(?is)Автопереход:\s*через\s*([^<]+)`)
	taskRe        = regexp.MustCompile(`(?is)<span[^>]*id="LevelsScenarioRepeater_ctl\d+_LevelTasksRepeater_ctl\d+_lblLevelTask"[^>]*>(.*?)</span>`)
	hintPairRe    = regexp.MustCompile(`(?is)<span[^>]*id="LevelsScenarioRepeater_ctl\d+_LevelHelpsRepeater_ctl\d+_lblLevelHelpTitle"[^>]*>(.*?)</span>\s*<br>\s*<span[^>]*id="LevelsScenarioRepeater_ctl\d+_LevelHelpsRepeater_ctl\d+_lblLevelHelp"[^>]*>(.*?)</span>`)
	answerRe      = regexp.MustCompile(`(?is)<span[^>]*id="LevelsScenarioRepeater_ctl\d+_SectorsRepeater_ctl(\d+)_LevelAnswersRepeater_ctl\d+_lblLevelAnswer"[^>]*>(.*?)</span>\s*-\s*<span[^>]*id="LevelsScenarioRepeater_ctl\d+_SectorsRepeater_ctl\d+_LevelAnswersRepeater_ctl\d+_lblAnswerFor"`)
	hintDelayRe   = regexp.MustCompile(`\(([^()]*)\)\s*$`)
	durationRe    = regexp.MustCompile(`(?i)(\d+)\s*(день|дня|дней|час(?:а|ов)?|минут(?:а|ы)?|секунд(?:а|ы)?)`)
	assetAttrRe   = regexp.MustCompile(`(?i)\b(src|href)\s*=\s*"([^"]+)"`)
	dataURIBlobRe = regexp.MustCompile(`(?i)data:([a-z0-9.+-]+/[a-z0-9.+-]+)?;base64,[a-z0-9+/=]+`)
)

const maxCreateLevelsPerRequest = 30

var waitForTransientRetry = func(opName string, err error) error {
	fmt.Println()
	fmt.Printf("Request failed during %s:\n%v\n", opName, err)
	fmt.Print("Press Enter to retry (or type 'abort' to stop): ")
	reader := bufio.NewReader(os.Stdin)
	input, readErr := reader.ReadString('\n')
	if readErr != nil {
		return fmt.Errorf("retry prompt read failed: %w", readErr)
	}
	if strings.EqualFold(strings.TrimSpace(input), "abort") {
		return errors.New("aborted by user during retry prompt")
	}
	return nil
}

func cmdImportScenario(ctx context.Context, cfg *config, client *encx.Client, args []string) {
	requireGameId(cfg)
	opts, err := parseImportScenarioArgs(args)
	if err != nil {
		fatal("%v", err)
	}
	opts.DryRun = opts.DryRun || cfg.importDryRun
	opts.SyncMissing = opts.SyncMissing || cfg.importSyncMissing

	scenario, err := parseScenarioFile(opts.SourcePath)
	if err != nil {
		fatal("Failed to parse scenario file: %v", err)
	}
	if len(scenario.Levels) == 0 {
		fatal("No levels found in scenario file")
	}

	progress := func(msg string) {
		if !cfg.jsonOutput {
			fmt.Println(msg)
		}
	}

	progress(fmt.Sprintf("Parsed %d level(s), embedding %d asset(s)", len(scenario.Levels), scenario.EmbeddedAssets))
	if len(scenario.MissingAssets) > 0 {
		progress(fmt.Sprintf("Warning: %d linked asset(s) were not found on disk", len(scenario.MissingAssets)))
	}

	totalTasks := 0
	totalHints := 0
	totalSectors := 0
	for _, lvl := range scenario.Levels {
		totalTasks += len(lvl.Tasks)
		totalHints += len(lvl.Hints)
		totalSectors += len(lvl.SectorAnswers)
	}

	if opts.DryRun {
		redactedLevels := redactScenarioBinaryPayloads(scenario.Levels)
		if cfg.jsonOutput {
			outputJSON(map[string]any{
				"success":         true,
				"dry_run":         true,
				"sync_missing":    opts.SyncMissing,
				"game_id":         cfg.gameId,
				"source_path":     scenario.SourcePath,
				"levels":          len(scenario.Levels),
				"tasks":           totalTasks,
				"hints":           totalHints,
				"sectors":         totalSectors,
				"embedded_assets": scenario.EmbeddedAssets,
				"missing_assets":  scenario.MissingAssets,
				"scenario":        redactedLevels,
			})
			return
		}
		action := "replace"
		if opts.SyncMissing {
			action = "sync"
		}
		fmt.Printf("Dry-run: would %s game %d with %d level(s), %d task(s), %d hint(s), %d sector(s)\n", action, cfg.gameId, len(scenario.Levels), totalTasks, totalHints, totalSectors)
		fmt.Printf("Source: %s\n", scenario.SourcePath)
		fmt.Printf("Embedded assets: %d\n", scenario.EmbeddedAssets)
		if len(scenario.MissingAssets) > 0 {
			fmt.Println("Missing assets:")
			for _, missing := range scenario.MissingAssets {
				fmt.Printf("  - %s\n", missing)
			}
		}
		for _, lvl := range redactedLevels {
			printDryRunLevel(lvl)
		}
		return
	}

	if opts.SyncMissing {
		stats, err := syncMissingScenario(ctx, cfg, client, scenario, progress)
		if err != nil {
			fatal("Failed to sync missing parts: %v", err)
		}
		if cfg.jsonOutput {
			outputJSON(map[string]any{
				"success":         true,
				"sync_missing":    true,
				"game_id":         cfg.gameId,
				"source_path":     scenario.SourcePath,
				"levels":          len(scenario.Levels),
				"embedded_assets": scenario.EmbeddedAssets,
				"missing_assets":  scenario.MissingAssets,
				"stats":           stats,
			})
			return
		}
		fmt.Printf("Scenario synced: levels+%d names~%d autopass~%d tasks-%d/+%d hints-%d/+%d sectors-%d/+%d\n",
			stats.LevelsCreated, stats.NamesUpdated, stats.AutopassUpdated,
			stats.TasksDeleted, stats.TasksCreated,
			stats.HintsDeleted, stats.HintsCreated,
			stats.SectorsDeleted, stats.SectorsCreated)
		return
	}

	var existing []encx.AdminLevel
	err = runWithAntiSpamRetry("read existing levels", func() error {
		var callErr error
		existing, callErr = client.AdminGetLevels(ctx, cfg.gameId)
		return callErr
	})
	if err != nil {
		fatal("Failed to read existing levels: %v", err)
	}
	if len(existing) > 0 {
		sort.Slice(existing, func(i, j int) bool { return existing[i].Number > existing[j].Number })
		for _, lvl := range existing {
			err := runWithAntiSpamRetry(fmt.Sprintf("delete existing level %d", lvl.Number), func() error {
				return client.AdminDeleteLevel(ctx, cfg.gameId, lvl.Number)
			})
			if err != nil {
				fatal("Failed to delete existing level %d: %v", lvl.Number, err)
			}
		}
		progress(fmt.Sprintf("Deleted %d existing level(s)", len(existing)))
	}

	batches := splitIntoBatches(len(scenario.Levels), maxCreateLevelsPerRequest)
	created := 0
	for i, batchSize := range batches {
		err = runWithAntiSpamRetry(fmt.Sprintf("create level batch %d/%d (%d level(s))", i+1, len(batches), batchSize), func() error {
			return client.AdminCreateLevels(ctx, cfg.gameId, batchSize)
		})
		if err != nil {
			fatal("Failed to create levels batch %d/%d: %v", i+1, len(batches), err)
		}
		created += batchSize
		progress(fmt.Sprintf("Created level batch %d/%d (+%d, total %d/%d)", i+1, len(batches), batchSize, created, len(scenario.Levels)))
	}

	for idx, lvl := range scenario.Levels {
		levelNum := idx + 1
		levelName := strings.TrimSpace(lvl.Name)
		if levelName == "" {
			levelName = fmt.Sprintf("Уровень %d", levelNum)
		}

		err := runWithAntiSpamRetry(fmt.Sprintf("set name for level %d", levelNum), func() error {
			return client.AdminUpdateComment(ctx, cfg.gameId, levelNum, levelName, "")
		})
		if err != nil {
			fatal("Failed to set name for level %d: %v", levelNum, err)
		}

		if lvl.AutopassSecond > 0 {
			h, m, s := splitSeconds(lvl.AutopassSecond)
			settings := encx.AdminLevelSettings{
				AutopassHours:   h,
				AutopassMinutes: m,
				AutopassSeconds: s,
			}
			err := runWithAntiSpamRetry(fmt.Sprintf("set autopass for level %d", levelNum), func() error {
				return client.AdminUpdateAutopass(ctx, cfg.gameId, levelNum, settings)
			})
			if err != nil {
				fatal("Failed to set autopass for level %d: %v", levelNum, err)
			}
		}

		for _, taskText := range lvl.Tasks {
			taskText = strings.TrimSpace(taskText)
			if taskText == "" {
				continue
			}
			task := encx.AdminTask{
				Text:      taskText,
				ReplaceNl: !strings.Contains(taskText, "<"),
			}
			err := runWithAntiSpamRetry(fmt.Sprintf("create task on level %d", levelNum), func() error {
				return client.AdminCreateTask(ctx, cfg.gameId, levelNum, task)
			})
			if err != nil {
				fatal("Failed to create task on level %d: %v", levelNum, err)
			}
		}

		for _, hint := range lvl.Hints {
			hintText := strings.TrimSpace(hint.Text)
			if hintText == "" {
				continue
			}
			h, m, s := splitSeconds(hint.DelaySeconds)
			payload := encx.AdminHint{
				Text:    hintText,
				Hours:   h,
				Minutes: m,
				Seconds: s,
			}
			err := runWithAntiSpamRetry(fmt.Sprintf("create hint on level %d", levelNum), func() error {
				return client.AdminCreateHint(ctx, cfg.gameId, levelNum, payload)
			})
			if err != nil {
				fatal("Failed to create hint on level %d: %v", levelNum, err)
			}
		}

		for sectorIdx, answers := range lvl.SectorAnswers {
			if len(answers) == 0 {
				continue
			}
			sector := encx.AdminSector{
				Name:    fmt.Sprintf("Сектор %d", sectorIdx+1),
				Answers: answers,
			}
			err := runWithAntiSpamRetry(fmt.Sprintf("create sector on level %d", levelNum), func() error {
				return client.AdminCreateSector(ctx, cfg.gameId, levelNum, sector)
			})
			if err != nil {
				fatal("Failed to create sector on level %d: %v", levelNum, err)
			}
		}

		progress(fmt.Sprintf("Imported level %d/%d: %s", levelNum, len(scenario.Levels), levelName))
	}

	if cfg.jsonOutput {
		outputJSON(map[string]any{
			"success":         true,
			"game_id":         cfg.gameId,
			"source_path":     scenario.SourcePath,
			"levels":          len(scenario.Levels),
			"tasks":           totalTasks,
			"hints":           totalHints,
			"sectors":         totalSectors,
			"embedded_assets": scenario.EmbeddedAssets,
			"missing_assets":  scenario.MissingAssets,
		})
		return
	}

	fmt.Printf("Scenario imported: %d levels, %d tasks, %d hints, %d sectors\n", len(scenario.Levels), totalTasks, totalHints, totalSectors)
}

func importScenarioNeedsAdmin(cfg *config, args []string) bool {
	opts, err := parseImportScenarioArgs(args)
	if err != nil {
		return true
	}
	return !(opts.DryRun || cfg.importDryRun)
}

func runWithAntiSpamRetry(opName string, fn func() error) error {
	for {
		err := fn()
		if err == nil {
			return nil
		}
		if isTransientImportError(err) {
			if promptErr := waitForTransientRetry(opName, err); promptErr != nil {
				return fmt.Errorf("%s: %w", opName, promptErr)
			}
			continue
		}
		return err
	}
}

func isTransientImportError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || os.IsTimeout(err) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "client.timeout exceeded") ||
		strings.Contains(msg, "timeout exceeded") ||
		strings.Contains(msg, "context deadline exceeded")
}

func parseImportScenarioArgs(args []string) (importScenarioOptions, error) {
	var opts importScenarioOptions
	for _, arg := range args {
		switch arg {
		case "--dry-run":
			opts.DryRun = true
		case "--sync-missing":
			opts.SyncMissing = true
		case "":
			continue
		default:
			if strings.HasPrefix(arg, "-") {
				return importScenarioOptions{}, fmt.Errorf("unknown flag %q. Usage: encli import-scenario -game-id <id> [--dry-run] [--sync-missing] <path-to-Game scenario.html>", arg)
			}
			if opts.SourcePath != "" {
				return importScenarioOptions{}, fmt.Errorf("only one source path is expected. Usage: encli import-scenario -game-id <id> [--dry-run] [--sync-missing] <path-to-Game scenario.html>")
			}
			opts.SourcePath = arg
		}
	}
	if opts.SourcePath == "" {
		return importScenarioOptions{}, fmt.Errorf("usage: encli import-scenario -game-id <id> [--dry-run] [--sync-missing] <path-to-Game scenario.html>")
	}
	return opts, nil
}

func parseScenarioFile(path string) (*importedScenario, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}

	state := &assetRewriteState{
		baseDir:    filepath.Dir(absPath),
		cache:      make(map[string]string),
		missingSet: make(map[string]struct{}),
	}

	levels, err := parseScenarioLevels(string(raw), state)
	if err != nil {
		return nil, err
	}
	missing := make([]string, 0, len(state.missingSet))
	for path := range state.missingSet {
		missing = append(missing, path)
	}
	sort.Strings(missing)

	return &importedScenario{
		SourcePath:     absPath,
		Levels:         levels,
		EmbeddedAssets: state.embeddedCount,
		MissingAssets:  missing,
	}, nil
}

func parseScenarioLevels(raw string, state *assetRewriteState) ([]importedLevel, error) {
	anchors := levelAnchorRe.FindAllStringIndex(raw, -1)
	if len(anchors) == 0 {
		return nil, fmt.Errorf("no level anchors found (LevelsScenarioRepeater)")
	}

	levels := make([]importedLevel, 0, len(anchors))
	for i, anchor := range anchors {
		start := anchor[0]
		end := len(raw)
		if i+1 < len(anchors) {
			end = anchors[i+1][0]
		}
		block := raw[start:end]
		level, ok := parseLevelBlock(block, state)
		if !ok {
			continue
		}
		levels = append(levels, level)
	}

	sort.Slice(levels, func(i, j int) bool { return levels[i].Number < levels[j].Number })
	return levels, nil
}

func parseLevelBlock(block string, state *assetRewriteState) (importedLevel, bool) {
	title := levelTitleRe.FindStringSubmatch(block)
	if len(title) < 2 {
		return importedLevel{}, false
	}
	levelNum, _ := strconv.Atoi(strings.TrimSpace(title[1]))
	levelName := ""
	if len(title) >= 3 {
		levelName = strings.TrimSpace(html.UnescapeString(title[2]))
	}
	level := importedLevel{
		Number: levelNum,
		Name:   levelName,
	}

	if m := autopassRe.FindStringSubmatch(block); len(m) >= 2 {
		level.AutopassSecond = parseRuDuration(strings.TrimSpace(html.UnescapeString(m[1])))
	}

	for _, taskMatch := range taskRe.FindAllStringSubmatch(block, -1) {
		if len(taskMatch) < 2 {
			continue
		}
		taskHTML := normalizeHTMLFragment(taskMatch[1], state)
		if taskHTML != "" {
			level.Tasks = append(level.Tasks, taskHTML)
		}
	}

	for _, hintMatch := range hintPairRe.FindAllStringSubmatch(block, -1) {
		if len(hintMatch) < 3 {
			continue
		}
		titleText := cleanInlineText(hintMatch[1])
		hintHTML := normalizeHTMLFragment(hintMatch[2], state)
		if hintHTML == "" {
			continue
		}
		level.Hints = append(level.Hints, importedHint{
			Title:        titleText,
			Text:         hintHTML,
			DelaySeconds: parseHintDelay(titleText),
		})
	}

	answersBySector := map[int][]string{}
	for _, answerMatch := range answerRe.FindAllStringSubmatch(block, -1) {
		if len(answerMatch) < 3 {
			continue
		}
		sectorIdx, err := strconv.Atoi(answerMatch[1])
		if err != nil {
			continue
		}
		answer := cleanInlineText(answerMatch[2])
		if answer == "" {
			continue
		}
		answersBySector[sectorIdx] = append(answersBySector[sectorIdx], answer)
	}
	if len(answersBySector) > 0 {
		keys := make([]int, 0, len(answersBySector))
		for key := range answersBySector {
			keys = append(keys, key)
		}
		sort.Ints(keys)
		level.SectorAnswers = make([][]string, 0, len(keys))
		for _, key := range keys {
			level.SectorAnswers = append(level.SectorAnswers, dedupeKeepOrder(answersBySector[key]))
		}
	}

	return level, true
}

func normalizeHTMLFragment(fragment string, state *assetRewriteState) string {
	out := strings.TrimSpace(fragment)
	out = strings.ReplaceAll(out, "\r\n", "\n")
	out = strings.ReplaceAll(out, "\r", "\n")
	out = embedLocalAssets(out, state)
	return strings.TrimSpace(out)
}

func cleanInlineText(v string) string {
	text := stripHTML(v)
	text = html.UnescapeString(text)
	return strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
}

func parseHintDelay(title string) int {
	match := hintDelayRe.FindStringSubmatch(title)
	if len(match) < 2 {
		return 0
	}
	return parseRuDuration(strings.TrimSpace(match[1]))
}

func parseRuDuration(s string) int {
	total := 0
	for _, match := range durationRe.FindAllStringSubmatch(strings.ToLower(s), -1) {
		if len(match) < 3 {
			continue
		}
		value, err := strconv.Atoi(match[1])
		if err != nil {
			continue
		}
		unit := match[2]
		switch {
		case unit == "день" || unit == "дня" || unit == "дней":
			total += value * 24 * 3600
		case strings.HasPrefix(unit, "час"):
			total += value * 3600
		case strings.HasPrefix(unit, "минут"):
			total += value * 60
		case strings.HasPrefix(unit, "секунд"):
			total += value
		}
	}
	return total
}

func splitSeconds(total int) (h, m, s int) {
	if total < 0 {
		total = 0
	}
	h = total / 3600
	m = (total % 3600) / 60
	s = total % 60
	return
}

func splitIntoBatches(total, maxBatch int) []int {
	if total <= 0 || maxBatch <= 0 {
		return nil
	}
	var out []int
	left := total
	for left > 0 {
		size := maxBatch
		if left < size {
			size = left
		}
		out = append(out, size)
		left -= size
	}
	return out
}

func scenarioTaskNorms(src importedLevel) []string {
	var out []string
	for _, t := range src.Tasks {
		if n := normalizeComparableText(t); n != "" {
			out = append(out, n)
		}
	}
	return out
}

func taskNormsMatch(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func hintDelayTextKey(delaySec int, text string) string {
	return fmt.Sprintf("%d|%s", delaySec, normalizeComparableText(text))
}

func scenarioHintKeys(src importedLevel) []string {
	var out []string
	for _, h := range src.Hints {
		text := strings.TrimSpace(h.Text)
		if text == "" {
			continue
		}
		out = append(out, hintDelayTextKey(h.DelaySeconds, text))
	}
	return out
}

func hintKeysMatch(a, b []string) bool {
	return taskNormsMatch(a, b)
}

func normalizedAnswerSet(answers []string) map[string]struct{} {
	set := make(map[string]struct{}, len(answers))
	for _, ans := range answers {
		if n := normalizeComparableText(ans); n != "" {
			set[n] = struct{}{}
		}
	}
	return set
}

func answerSetsEqual(a, b []string) bool {
	setA := normalizedAnswerSet(a)
	setB := normalizedAnswerSet(b)
	if len(setA) != len(setB) {
		return false
	}
	for k := range setA {
		if _, ok := setB[k]; !ok {
			return false
		}
	}
	return true
}

func nonEmptyScenarioSectorGroups(scenarioGroups [][]string) [][]string {
	var out [][]string
	for _, group := range scenarioGroups {
		answers := dedupeKeepOrder(group)
		if len(answers) > 0 {
			out = append(out, answers)
		}
	}
	return out
}

func answerSetKey(answers []string) string {
	set := normalizedAnswerSet(answers)
	if len(set) == 0 {
		return ""
	}
	norms := make([]string, 0, len(set))
	for k := range set {
		norms = append(norms, k)
	}
	sort.Strings(norms)
	return strings.Join(norms, "\x00")
}

func gameSectorsAnomalous(sectors []encx.AdminSector) bool {
	nameCount := map[string]int{}
	answerKeys := map[string]int{}
	for _, sec := range sectors {
		if answerSetKey(sec.Answers) == "" {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(sec.Name))
		if name != "" {
			nameCount[name]++
			if nameCount[name] > 1 {
				return true
			}
		}
		key := answerSetKey(sec.Answers)
		answerKeys[key]++
		if answerKeys[key] > 1 {
			return true
		}
	}
	return false
}

func gameSectorsWithAnswers(sectors []encx.AdminSector) []encx.AdminSector {
	out := make([]encx.AdminSector, 0, len(sectors))
	for _, sec := range sectors {
		if answerSetKey(sec.Answers) == "" {
			continue
		}
		out = append(out, sec)
	}
	return out
}

func sectorGroupsMatch(scenarioGroups [][]string, gameSectors []encx.AdminSector) bool {
	groups := nonEmptyScenarioSectorGroups(scenarioGroups)
	gameSectors = gameSectorsWithAnswers(gameSectors)
	if gameSectorsAnomalous(gameSectors) {
		return false
	}
	if len(groups) != len(gameSectors) {
		return false
	}
	for i, group := range groups {
		if !answerSetsEqual(group, gameSectors[i].Answers) {
			return false
		}
	}
	return true
}

func syncLevelTasksToScenario(ctx context.Context, client *encx.Client, gameID, levelNum int, src importedLevel, stats *importSyncStats) error {
	want := scenarioTaskNorms(src)
	var taskIDs []int
	err := runWithAntiSpamRetry(fmt.Sprintf("read level %d task IDs", levelNum), func() error {
		var callErr error
		taskIDs, callErr = client.AdminGetTaskIds(ctx, gameID, levelNum)
		return callErr
	})
	if err != nil {
		return err
	}
	var existing []string
	for _, taskID := range taskIDs {
		var task *encx.AdminTask
		err := runWithAntiSpamRetry(fmt.Sprintf("read level %d task %d", levelNum, taskID), func() error {
			var callErr error
			task, callErr = client.AdminGetTask(ctx, gameID, levelNum, taskID)
			return callErr
		})
		if err != nil {
			return err
		}
		if task != nil {
			if n := normalizeComparableText(task.Text); n != "" {
				existing = append(existing, n)
			}
		}
	}
	if taskNormsMatch(existing, want) {
		return nil
	}
	for _, taskID := range taskIDs {
		id := taskID
		err := runWithAntiSpamRetry(fmt.Sprintf("delete level %d task %d", levelNum, id), func() error {
			return client.AdminDeleteTask(ctx, gameID, levelNum, id)
		})
		if err != nil {
			return err
		}
		stats.TasksDeleted++
	}
	for _, srcTask := range src.Tasks {
		taskText := strings.TrimSpace(srcTask)
		if taskText == "" {
			continue
		}
		task := encx.AdminTask{
			Text:      taskText,
			ReplaceNl: !strings.Contains(taskText, "<"),
		}
		err := runWithAntiSpamRetry(fmt.Sprintf("create task on level %d", levelNum), func() error {
			return client.AdminCreateTask(ctx, gameID, levelNum, task)
		})
		if err != nil {
			return err
		}
		stats.TasksCreated++
	}
	return nil
}

func syncLevelHintsToScenario(ctx context.Context, client *encx.Client, gameID, levelNum int, src importedLevel, stats *importSyncStats) error {
	want := scenarioHintKeys(src)
	var hintIDs []int
	err := runWithAntiSpamRetry(fmt.Sprintf("read level %d hint IDs", levelNum), func() error {
		var callErr error
		hintIDs, callErr = client.AdminGetHintIds(ctx, gameID, levelNum)
		return callErr
	})
	if err != nil {
		return err
	}
	var existing []string
	for _, hintID := range hintIDs {
		var hint *encx.AdminHint
		err := runWithAntiSpamRetry(fmt.Sprintf("read level %d hint %d", levelNum, hintID), func() error {
			var callErr error
			hint, callErr = client.AdminGetHint(ctx, gameID, levelNum, hintID)
			return callErr
		})
		if err != nil {
			return err
		}
		if hint == nil {
			continue
		}
		sec := hint.Hours*3600 + hint.Minutes*60 + hint.Seconds
		text := strings.TrimSpace(hint.Text)
		if text == "" {
			continue
		}
		existing = append(existing, hintDelayTextKey(sec, text))
	}
	if hintKeysMatch(existing, want) {
		return nil
	}
	for _, hintID := range hintIDs {
		id := hintID
		err := runWithAntiSpamRetry(fmt.Sprintf("delete level %d hint %d", levelNum, id), func() error {
			return client.AdminDeleteHint(ctx, gameID, levelNum, id)
		})
		if err != nil {
			return err
		}
		stats.HintsDeleted++
	}
	for _, srcHint := range src.Hints {
		text := strings.TrimSpace(srcHint.Text)
		if text == "" {
			continue
		}
		h, m, s := splitSeconds(srcHint.DelaySeconds)
		payload := encx.AdminHint{Text: text, Hours: h, Minutes: m, Seconds: s}
		err := runWithAntiSpamRetry(fmt.Sprintf("create hint on level %d", levelNum), func() error {
			return client.AdminCreateHint(ctx, gameID, levelNum, payload)
		})
		if err != nil {
			return err
		}
		stats.HintsCreated++
	}
	return nil
}

func syncLevelSectorsToScenario(ctx context.Context, client *encx.Client, gameID, levelNum int, src importedLevel, stats *importSyncStats) error {
	var sectors []encx.AdminSector
	err := runWithAntiSpamRetry(fmt.Sprintf("read level %d sectors", levelNum), func() error {
		var callErr error
		sectors, callErr = client.AdminGetSectorAnswers(ctx, gameID, levelNum)
		return callErr
	})
	if err != nil {
		return err
	}
	wantGroups := nonEmptyScenarioSectorGroups(src.SectorAnswers)
	if sectorGroupsMatch(src.SectorAnswers, sectors) {
		return nil
	}

	if err := runWithAntiSpamRetry(fmt.Sprintf("clear sectors on level %d", levelNum), func() error {
		return client.AdminClearLevelSectors(ctx, gameID, levelNum)
	}); err != nil {
		return fmt.Errorf("level %d: clear sectors: %w", levelNum, err)
	}
	stats.SectorsDeleted += len(sectors)

	for i, answers := range wantGroups {
		sector := encx.AdminSector{
			Name:    fmt.Sprintf("Сектор %d", i+1),
			Answers: answers,
		}
		err := runWithAntiSpamRetry(fmt.Sprintf("create sector on level %d", levelNum), func() error {
			return client.AdminCreateSector(ctx, gameID, levelNum, sector)
		})
		if err != nil {
			return fmt.Errorf("level %d: create sector %d: %w", levelNum, i+1, err)
		}
		stats.SectorsCreated++
	}

	sectors, err = runWithAntiSpamRetryReadSectors(ctx, client, gameID, levelNum)
	if err != nil {
		return err
	}
	if !sectorGroupsMatch(src.SectorAnswers, sectors) {
		return fmt.Errorf("level %d: sectors still differ after sync: %s",
			levelNum, formatAdminSectorsSummary(gameSectorsWithAnswers(sectors)))
	}
	return nil
}

func formatAdminSectorsSummary(sectors []encx.AdminSector) string {
	if len(sectors) == 0 {
		return "(none)"
	}
	parts := make([]string, 0, len(sectors))
	for _, s := range sectors {
		name := strings.TrimSpace(s.Name)
		if name == "" {
			name = fmt.Sprintf("id=%d", s.ID)
		}
		parts = append(parts, fmt.Sprintf("%s:%v", name, s.Answers))
	}
	return strings.Join(parts, "; ")
}

func runWithAntiSpamRetryReadSectors(ctx context.Context, client *encx.Client, gameID, levelNum int) ([]encx.AdminSector, error) {
	var sectors []encx.AdminSector
	err := runWithAntiSpamRetry(fmt.Sprintf("read level %d sectors", levelNum), func() error {
		var callErr error
		sectors, callErr = client.AdminGetSectorAnswers(ctx, gameID, levelNum)
		return callErr
	})
	return sectors, err
}

func syncMissingScenario(ctx context.Context, cfg *config, client *encx.Client, scenario *importedScenario, progress func(string)) (importSyncStats, error) {
	stats := importSyncStats{}

	prevDelay := client.AdminDelay()
	client.SetAdminDelay(350 * time.Millisecond)
	defer client.SetAdminDelay(prevDelay)

	var levels []encx.AdminLevel
	err := runWithAntiSpamRetry("read existing levels", func() error {
		var callErr error
		levels, callErr = client.AdminGetLevels(ctx, cfg.gameId)
		return callErr
	})
	if err != nil {
		return stats, err
	}

	existingCount := len(levels)
	if existingCount < len(scenario.Levels) {
		missing := len(scenario.Levels) - existingCount
		for i, batch := range splitIntoBatches(missing, maxCreateLevelsPerRequest) {
			err := runWithAntiSpamRetry(fmt.Sprintf("create missing level batch %d (%d)", i+1, batch), func() error {
				return client.AdminCreateLevels(ctx, cfg.gameId, batch)
			})
			if err != nil {
				return stats, err
			}
			stats.LevelsCreated += batch
		}
		progress(fmt.Sprintf("Created %d missing level(s)", stats.LevelsCreated))
	}

	for idx, src := range scenario.Levels {
		levelNum := idx + 1
		levelName := strings.TrimSpace(src.Name)
		if levelName == "" {
			levelName = fmt.Sprintf("Уровень %d", levelNum)
		}

		var curName string
		var curComment string
		err := runWithAntiSpamRetry(fmt.Sprintf("read level %d comment/name", levelNum), func() error {
			var callErr error
			curName, curComment, callErr = client.AdminGetComment(ctx, cfg.gameId, levelNum)
			return callErr
		})
		if err != nil {
			return stats, err
		}
		if strings.TrimSpace(curName) != levelName {
			err := runWithAntiSpamRetry(fmt.Sprintf("update level %d name", levelNum), func() error {
				return client.AdminUpdateComment(ctx, cfg.gameId, levelNum, levelName, curComment)
			})
			if err != nil {
				return stats, err
			}
			stats.NamesUpdated++
		}

		if src.AutopassSecond > 0 {
			var curSettings *encx.AdminLevelSettings
			err := runWithAntiSpamRetry(fmt.Sprintf("read level %d settings", levelNum), func() error {
				var callErr error
				curSettings, callErr = client.AdminGetLevelSettings(ctx, cfg.gameId, levelNum)
				return callErr
			})
			if err != nil {
				return stats, err
			}
			targetH, targetM, targetS := splitSeconds(src.AutopassSecond)
			if curSettings == nil || curSettings.AutopassHours != targetH || curSettings.AutopassMinutes != targetM || curSettings.AutopassSeconds != targetS {
				settings := encx.AdminLevelSettings{
					AutopassHours:   targetH,
					AutopassMinutes: targetM,
					AutopassSeconds: targetS,
				}
				err := runWithAntiSpamRetry(fmt.Sprintf("update level %d autopass", levelNum), func() error {
					return client.AdminUpdateAutopass(ctx, cfg.gameId, levelNum, settings)
				})
				if err != nil {
					return stats, err
				}
				stats.AutopassUpdated++
			}
		}

		if err := syncLevelTasksToScenario(ctx, client, cfg.gameId, levelNum, src, &stats); err != nil {
			return stats, err
		}
		if err := syncLevelHintsToScenario(ctx, client, cfg.gameId, levelNum, src, &stats); err != nil {
			return stats, err
		}
		if err := syncLevelSectorsToScenario(ctx, client, cfg.gameId, levelNum, src, &stats); err != nil {
			return stats, err
		}

		progress(fmt.Sprintf("Synced level %d/%d: %s", levelNum, len(scenario.Levels), levelName))
	}

	return stats, nil
}

func normalizeComparableText(s string) string {
	s = html.UnescapeString(s)
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.Join(strings.Fields(s), " ")
}

func redactScenarioBinaryPayloads(levels []importedLevel) []importedLevel {
	out := make([]importedLevel, len(levels))
	for i, level := range levels {
		out[i] = level
		if len(level.Tasks) > 0 {
			out[i].Tasks = make([]string, len(level.Tasks))
			for j, task := range level.Tasks {
				out[i].Tasks[j] = redactBinaryPayloads(task)
			}
		}
		if len(level.Hints) > 0 {
			out[i].Hints = make([]importedHint, len(level.Hints))
			for j, hint := range level.Hints {
				out[i].Hints[j] = hint
				out[i].Hints[j].Text = redactBinaryPayloads(hint.Text)
			}
		}
	}
	return out
}

func redactBinaryPayloads(text string) string {
	if text == "" {
		return text
	}
	return dataURIBlobRe.ReplaceAllStringFunc(text, func(match string) string {
		meta := dataURIBlobRe.FindStringSubmatch(match)
		mimeType := "data-uri"
		if len(meta) > 1 && strings.TrimSpace(meta[1]) != "" {
			mimeType = strings.ToLower(strings.TrimSpace(meta[1]))
		}
		return fmt.Sprintf("[embedded %s omitted in dry-run]", mimeType)
	})
}

func printDryRunLevel(level importedLevel) {
	fmt.Printf("\n=== Level %d", level.Number)
	if strings.TrimSpace(level.Name) != "" {
		fmt.Printf(": %s", strings.TrimSpace(level.Name))
	}
	fmt.Println(" ===")
	if level.AutopassSecond > 0 {
		h, m, s := splitSeconds(level.AutopassSecond)
		fmt.Printf("Autopass: %02d:%02d:%02d\n", h, m, s)
	}
	if len(level.Tasks) > 0 {
		fmt.Printf("Tasks (%d):\n", len(level.Tasks))
		for i, task := range level.Tasks {
			fmt.Printf("  [Task %d]\n%s\n", i+1, indentBlock(task, "    "))
		}
	}
	if len(level.Hints) > 0 {
		fmt.Printf("Hints (%d):\n", len(level.Hints))
		for i, hint := range level.Hints {
			h, m, s := splitSeconds(hint.DelaySeconds)
			if strings.TrimSpace(hint.Title) != "" {
				fmt.Printf("  [Hint %d] %s (delay %02d:%02d:%02d)\n", i+1, hint.Title, h, m, s)
			} else {
				fmt.Printf("  [Hint %d] (delay %02d:%02d:%02d)\n", i+1, h, m, s)
			}
			fmt.Println(indentBlock(hint.Text, "    "))
		}
	}
	if len(level.SectorAnswers) > 0 {
		fmt.Printf("Sectors (%d):\n", len(level.SectorAnswers))
		for i, answers := range level.SectorAnswers {
			fmt.Printf("  [Sector %d]\n", i+1)
			for _, answer := range answers {
				fmt.Printf("    - %s\n", answer)
			}
		}
	}
}

func dedupeKeepOrder(items []string) []string {
	out := make([]string, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func embedLocalAssets(fragment string, state *assetRewriteState) string {
	if state == nil {
		return fragment
	}
	return assetAttrRe.ReplaceAllStringFunc(fragment, func(attr string) string {
		m := assetAttrRe.FindStringSubmatch(attr)
		if len(m) < 3 {
			return attr
		}
		pathValue := strings.TrimSpace(m[2])
		if skipAssetEmbedding(pathValue) {
			return attr
		}
		localPath := resolveAssetPath(state.baseDir, pathValue)
		if localPath == "" {
			return attr
		}
		dataURI, ok := state.cache[localPath]
		if !ok {
			payload, err := os.ReadFile(localPath)
			if err != nil {
				state.missingSet[pathValue] = struct{}{}
				return attr
			}
			mimeType := mime.TypeByExtension(strings.ToLower(filepath.Ext(localPath)))
			if mimeType == "" {
				mimeType = http.DetectContentType(payload)
			}
			if mimeType == "" {
				mimeType = "application/octet-stream"
			}
			dataURI = "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(payload)
			state.cache[localPath] = dataURI
			state.embeddedCount++
		}
		return fmt.Sprintf(`%s="%s"`, m[1], dataURI)
	})
}

func skipAssetEmbedding(pathValue string) bool {
	lower := strings.ToLower(pathValue)
	return strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "data:") ||
		strings.HasPrefix(lower, "mailto:") ||
		strings.HasPrefix(lower, "#") ||
		strings.HasPrefix(lower, "//")
}

func resolveAssetPath(baseDir, pathValue string) string {
	if baseDir == "" || pathValue == "" {
		return ""
	}
	clean := strings.ReplaceAll(pathValue, "\\", "/")
	if strings.HasPrefix(clean, "/") {
		return ""
	}
	local := filepath.Clean(filepath.Join(baseDir, filepath.FromSlash(clean)))
	// Block escaping outside the scenario directory.
	baseAbs, err := filepath.Abs(baseDir)
	if err != nil {
		return ""
	}
	localAbs, err := filepath.Abs(local)
	if err != nil {
		return ""
	}
	baseWithSep := baseAbs + string(os.PathSeparator)
	if localAbs != baseAbs && !strings.HasPrefix(localAbs, baseWithSep) {
		return ""
	}
	return localAbs
}
