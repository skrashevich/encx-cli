package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/skrashevich/encx-cli/encx"
	"github.com/skrashevich/encx-cli/encx/scenario"
)

type importScenarioOptions struct {
	SourcePath  string
	DryRun      bool
	SyncMissing bool
}

type importSyncStats struct {
	LevelsCreated   int `json:"levels_created"`
	NamesUpdated    int `json:"names_updated"`
	AutopassUpdated int `json:"autopass_updated"`
	TasksDeleted    int `json:"tasks_deleted,omitempty"`
	TasksCreated    int `json:"tasks_created"`
	HintsDeleted    int `json:"hints_deleted,omitempty"`
	HintsCreated    int `json:"hints_created"`
	BonusesDeleted  int `json:"bonuses_deleted,omitempty"`
	BonusesCreated  int `json:"bonuses_created"`
	SectorsDeleted  int `json:"sectors_deleted,omitempty"`
	SectorsCreated  int `json:"sectors_created"`
}

var dataURIBlobRe = regexp.MustCompile(`(?i)data:([a-z0-9.+-]+/[a-z0-9.+-]+)?;base64,[a-z0-9+/=]+`)

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

	scenarioDoc, err := scenario.ParseFile(opts.SourcePath)
	if err != nil {
		fatal("Failed to parse scenario file: %v", err)
	}
	if len(scenarioDoc.Levels) == 0 {
		fatal("No levels found in scenario file")
	}

	progress := func(msg string) {
		if !cfg.jsonOutput {
			fmt.Println(msg)
		}
	}

	progress(fmt.Sprintf("Parsed %d level(s), embedding %d asset(s)", len(scenarioDoc.Levels), scenarioDoc.EmbeddedAssets))
	if len(scenarioDoc.MissingAssets) > 0 {
		progress(fmt.Sprintf("Warning: %d linked asset(s) were not found on disk", len(scenarioDoc.MissingAssets)))
	}

	totalTasks := 0
	totalHints := 0
	totalBonuses := 0
	totalSectors := 0
	for _, lvl := range scenarioDoc.Levels {
		totalTasks += len(lvl.Tasks)
		totalHints += len(lvl.Hints)
		totalBonuses += len(lvl.Bonuses)
		totalSectors += len(lvl.SectorAnswers)
	}

	if opts.DryRun {
		redactedLevels := redactScenarioBinaryPayloads(scenarioDoc.Levels)
		if cfg.jsonOutput {
			outputJSON(map[string]any{
				"success":         true,
				"dry_run":         true,
				"sync_missing":    opts.SyncMissing,
				"game_id":         cfg.gameId,
				"source_path":     scenarioDoc.SourcePath,
				"levels":          len(scenarioDoc.Levels),
				"tasks":           totalTasks,
				"hints":           totalHints,
				"bonuses":         totalBonuses,
				"sectors":         totalSectors,
				"embedded_assets": scenarioDoc.EmbeddedAssets,
				"missing_assets":  scenarioDoc.MissingAssets,
				"scenario":        redactedLevels,
			})
			return
		}
		action := "replace"
		if opts.SyncMissing {
			action = "sync"
		}
		fmt.Printf("Dry-run: would %s game %d with %d level(s), %d task(s), %d hint(s), %d bonus(es), %d sector(s)\n", action, cfg.gameId, len(scenarioDoc.Levels), totalTasks, totalHints, totalBonuses, totalSectors)
		fmt.Printf("Source: %s\n", scenarioDoc.SourcePath)
		fmt.Printf("Embedded assets: %d\n", scenarioDoc.EmbeddedAssets)
		if len(scenarioDoc.MissingAssets) > 0 {
			fmt.Println("Missing assets:")
			for _, missing := range scenarioDoc.MissingAssets {
				fmt.Printf("  - %s\n", missing)
			}
		}
		for _, lvl := range redactedLevels {
			printDryRunLevel(lvl)
		}
		return
	}

	if opts.SyncMissing {
		stats, err := syncMissingScenario(ctx, cfg, client, scenarioDoc, progress)
		if err != nil {
			fatal("Failed to sync missing parts: %v", err)
		}
		if cfg.jsonOutput {
			outputJSON(map[string]any{
				"success":         true,
				"sync_missing":    true,
				"game_id":         cfg.gameId,
				"source_path":     scenarioDoc.SourcePath,
				"levels":          len(scenarioDoc.Levels),
				"embedded_assets": scenarioDoc.EmbeddedAssets,
				"missing_assets":  scenarioDoc.MissingAssets,
				"stats":           stats,
			})
			return
		}
		fmt.Printf("Scenario synced: levels+%d names~%d autopass~%d tasks-%d/+%d hints-%d/+%d bonuses-%d/+%d sectors-%d/+%d\n",
			stats.LevelsCreated, stats.NamesUpdated, stats.AutopassUpdated,
			stats.TasksDeleted, stats.TasksCreated,
			stats.HintsDeleted, stats.HintsCreated,
			stats.BonusesDeleted, stats.BonusesCreated,
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

	batches := splitIntoBatches(len(scenarioDoc.Levels), maxCreateLevelsPerRequest)
	created := 0
	for i, batchSize := range batches {
		err = runWithAntiSpamRetry(fmt.Sprintf("create level batch %d/%d (%d level(s))", i+1, len(batches), batchSize), func() error {
			return client.AdminCreateLevels(ctx, cfg.gameId, batchSize)
		})
		if err != nil {
			fatal("Failed to create levels batch %d/%d: %v", i+1, len(batches), err)
		}
		created += batchSize
		progress(fmt.Sprintf("Created level batch %d/%d (+%d, total %d/%d)", i+1, len(batches), batchSize, created, len(scenarioDoc.Levels)))
	}

	importLevels, err := readAdminLevelsByNumber(ctx, client, cfg.gameId)
	if err != nil {
		fatal("Failed to read created levels: %v", err)
	}

	for idx, lvl := range scenarioDoc.Levels {
		levelNum := idx + 1
		levelName := importLevelName(levelNum, lvl.Name)

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

		if len(lvl.Bonuses) > 0 {
			levelID, ok := importLevels[levelNum]
			if !ok || levelID == 0 {
				fatal("Failed to resolve level ID for level %d while creating bonuses", levelNum)
			}
			for _, srcBonus := range lvl.Bonuses {
				bonus, ok := scenarioBonusToAdminBonus(srcBonus, levelID)
				if !ok {
					continue
				}
				err := runWithAntiSpamRetry(fmt.Sprintf("create bonus on level %d", levelNum), func() error {
					return client.AdminCreateBonus(ctx, cfg.gameId, levelNum, bonus)
				})
				if err != nil {
					fatal("Failed to create bonus on level %d: %v", levelNum, err)
				}
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
			if err := createScenarioSector(ctx, client, cfg.gameId, levelNum, sector); err != nil {
				fatal("Failed to create sector on level %d: %v", levelNum, err)
			}
		}

		progress(fmt.Sprintf("Imported level %d/%d: %s", levelNum, len(scenarioDoc.Levels), levelName))
	}

	if cfg.jsonOutput {
		outputJSON(map[string]any{
			"success":         true,
			"game_id":         cfg.gameId,
			"source_path":     scenarioDoc.SourcePath,
			"levels":          len(scenarioDoc.Levels),
			"tasks":           totalTasks,
			"hints":           totalHints,
			"bonuses":         totalBonuses,
			"sectors":         totalSectors,
			"embedded_assets": scenarioDoc.EmbeddedAssets,
			"missing_assets":  scenarioDoc.MissingAssets,
		})
		return
	}

	fmt.Printf("Scenario imported: %d levels, %d tasks, %d hints, %d bonuses, %d sectors\n", len(scenarioDoc.Levels), totalTasks, totalHints, totalBonuses, totalSectors)
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

func scenarioTaskNorms(src scenario.Level) []string {
	var out []string
	for _, t := range src.Tasks {
		if n := scenario.NormalizeComparableText(t); n != "" {
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
	return fmt.Sprintf("%d|%s", delaySec, scenario.NormalizeComparableText(text))
}

func scenarioHintKeys(src scenario.Level) []string {
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

func scenarioBonusToAdminBonus(src scenario.Bonus, levelID int) (encx.AdminBonus, bool) {
	name := strings.TrimSpace(src.Name)
	task := strings.TrimSpace(src.Task)
	answers := dedupeKeepOrder(src.Answers)
	if name == "" && task == "" && len(answers) == 0 {
		return encx.AdminBonus{}, false
	}
	h, m, s := splitSeconds(src.AwardSeconds)
	return encx.AdminBonus{
		Name:         name,
		Task:         task,
		LevelID:      levelID,
		Answers:      answers,
		AwardHours:   h,
		AwardMinutes: m,
		AwardSeconds: s,
	}, true
}

func bonusComparableKey(b encx.AdminBonus) string {
	return strings.Join([]string{
		scenario.NormalizeComparableText(b.Name),
		scenario.NormalizeComparableText(b.Task),
		scenario.NormalizeComparableText(b.Hint),
		fmt.Sprintf("%d:%d:%d:%t", b.AwardHours, b.AwardMinutes, b.AwardSeconds, b.Negative),
		answerSetKey(b.Answers),
	}, "\x00")
}

func scenarioBonusKeys(src scenario.Level, levelID int) []string {
	out := make([]string, 0, len(src.Bonuses))
	for _, bonus := range src.Bonuses {
		adminBonus, ok := scenarioBonusToAdminBonus(bonus, levelID)
		if !ok {
			continue
		}
		out = append(out, bonusComparableKey(adminBonus))
	}
	return out
}

func bonusKeysMatch(a, b []string) bool {
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

func normalizedAnswerSet(answers []string) map[string]struct{} {
	set := make(map[string]struct{}, len(answers))
	for _, ans := range answers {
		if n := scenario.NormalizeComparableText(ans); n != "" {
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

func syncLevelTasksToScenario(ctx context.Context, client *encx.Client, gameID, levelNum int, src scenario.Level, stats *importSyncStats) error {
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
			if n := scenario.NormalizeComparableText(task.Text); n != "" {
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

func syncLevelHintsToScenario(ctx context.Context, client *encx.Client, gameID, levelNum int, src scenario.Level, stats *importSyncStats) error {
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
	if taskNormsMatch(existing, want) {
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

func syncLevelBonusesToScenario(ctx context.Context, client *encx.Client, gameID, levelNum, levelID int, src scenario.Level, stats *importSyncStats) error {
	want := scenarioBonusKeys(src, levelID)
	var bonusIDs []int
	err := runWithAntiSpamRetry(fmt.Sprintf("read level %d bonus IDs", levelNum), func() error {
		var callErr error
		bonusIDs, callErr = client.AdminGetBonusIds(ctx, gameID, levelNum)
		return callErr
	})
	if err != nil {
		return err
	}

	existing := make([]string, 0, len(bonusIDs))
	for _, bonusID := range bonusIDs {
		var bonus *encx.AdminBonus
		id := bonusID
		err := runWithAntiSpamRetry(fmt.Sprintf("read level %d bonus %d", levelNum, id), func() error {
			var callErr error
			bonus, callErr = client.AdminGetBonus(ctx, gameID, levelNum, id)
			return callErr
		})
		if err != nil {
			return err
		}
		if bonus != nil {
			existing = append(existing, bonusComparableKey(*bonus))
		}
	}
	if bonusKeysMatch(existing, want) {
		return nil
	}

	for _, bonusID := range bonusIDs {
		id := bonusID
		err := runWithAntiSpamRetry(fmt.Sprintf("delete level %d bonus %d", levelNum, id), func() error {
			return client.AdminDeleteBonus(ctx, gameID, levelNum, id)
		})
		if err != nil {
			return err
		}
		stats.BonusesDeleted++
	}
	for _, srcBonus := range src.Bonuses {
		bonus, ok := scenarioBonusToAdminBonus(srcBonus, levelID)
		if !ok {
			continue
		}
		err := runWithAntiSpamRetry(fmt.Sprintf("create bonus on level %d", levelNum), func() error {
			return client.AdminCreateBonus(ctx, gameID, levelNum, bonus)
		})
		if err != nil {
			return err
		}
		stats.BonusesCreated++
	}
	return nil
}

func syncLevelSectorsToScenario(ctx context.Context, client *encx.Client, gameID, levelNum int, src scenario.Level, stats *importSyncStats) error {
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
		if err := createScenarioSector(ctx, client, gameID, levelNum, sector); err != nil {
			return fmt.Errorf("level %d: create sector %d: %w", levelNum, i+1, err)
		}
		stats.SectorsCreated++
	}

	err = runWithAntiSpamRetry(fmt.Sprintf("read level %d sectors", levelNum), func() error {
		var callErr error
		sectors, callErr = client.AdminGetSectorAnswers(ctx, gameID, levelNum)
		return callErr
	})
	if err != nil {
		return err
	}
	if !sectorGroupsMatch(src.SectorAnswers, sectors) {
		return fmt.Errorf("level %d: sectors still differ after sync: %s",
			levelNum, formatAdminSectorsSummary(gameSectorsWithAnswers(sectors)))
	}
	return nil
}

func createScenarioSector(ctx context.Context, client *encx.Client, gameID, levelNum int, sector encx.AdminSector) error {
	if len(sector.Answers) <= 1 {
		return runWithAntiSpamRetry(fmt.Sprintf("create sector on level %d", levelNum), func() error {
			return client.AdminCreateSector(ctx, gameID, levelNum, sector)
		})
	}

	var before []encx.AdminSector
	if err := runWithAntiSpamRetry(fmt.Sprintf("read level %d sectors before create", levelNum), func() error {
		var callErr error
		before, callErr = client.AdminGetSectorAnswers(ctx, gameID, levelNum)
		return callErr
	}); err != nil {
		return err
	}
	beforeIDs := make(map[int]struct{}, len(before))
	for _, sec := range before {
		if sec.ID > 0 {
			beforeIDs[sec.ID] = struct{}{}
		}
	}

	initial := sector
	if len(initial.Answers) > 1 {
		initial.Answers = initial.Answers[:1]
	}
	if err := runWithAntiSpamRetry(fmt.Sprintf("create sector on level %d", levelNum), func() error {
		return client.AdminCreateSector(ctx, gameID, levelNum, initial)
	}); err != nil {
		return err
	}

	created, err := findCreatedSector(ctx, client, gameID, levelNum, beforeIDs, sector.Name)
	if err != nil {
		return err
	}
	if created.ID <= 0 {
		return fmt.Errorf("created sector %q has no ID", sector.Name)
	}
	if !answerSetsEqual(created.Answers, sector.Answers) {
		remaining := remainingSectorAnswers(created.Answers, sector.Answers)
		if len(remaining) == 0 {
			return fmt.Errorf("sector %q answers differ after create: got %v, want %v", sector.Name, created.Answers, sector.Answers)
		}
		if err := runWithAntiSpamRetry(fmt.Sprintf("add %d answer(s) to sector %d on level %d", len(remaining), created.ID, levelNum), func() error {
			return client.AdminAddSectorAnswers(ctx, gameID, levelNum, created.ID, remaining)
		}); err != nil {
			return err
		}
	}

	var after []encx.AdminSector
	if err := runWithAntiSpamRetry(fmt.Sprintf("read level %d sectors after update", levelNum), func() error {
		var callErr error
		after, callErr = client.AdminGetSectorAnswers(ctx, gameID, levelNum)
		return callErr
	}); err != nil {
		return err
	}
	for _, sec := range after {
		if sec.ID == created.ID {
			if answerSetsEqual(sec.Answers, sector.Answers) {
				return nil
			}
			return fmt.Errorf("sector %q answers differ after create: got %v, want %v", sector.Name, sec.Answers, sector.Answers)
		}
	}
	return fmt.Errorf("created sector %q disappeared after update", sector.Name)
}

func findCreatedSector(ctx context.Context, client *encx.Client, gameID, levelNum int, beforeIDs map[int]struct{}, name string) (encx.AdminSector, error) {
	var sectors []encx.AdminSector
	if err := runWithAntiSpamRetry(fmt.Sprintf("read level %d sectors after create", levelNum), func() error {
		var callErr error
		sectors, callErr = client.AdminGetSectorAnswers(ctx, gameID, levelNum)
		return callErr
	}); err != nil {
		return encx.AdminSector{}, err
	}
	for i := len(sectors) - 1; i >= 0; i-- {
		sec := sectors[i]
		if _, existed := beforeIDs[sec.ID]; existed {
			continue
		}
		if strings.TrimSpace(sec.Name) == strings.TrimSpace(name) {
			return sec, nil
		}
	}
	for i := len(sectors) - 1; i >= 0; i-- {
		sec := sectors[i]
		if _, existed := beforeIDs[sec.ID]; !existed {
			return sec, nil
		}
	}
	return encx.AdminSector{}, fmt.Errorf("created sector %q was not found", name)
}

func remainingSectorAnswers(existing, want []string) []string {
	existingSet := normalizedAnswerSet(existing)
	var out []string
	seen := map[string]struct{}{}
	for _, answer := range want {
		n := scenario.NormalizeComparableText(answer)
		if n == "" {
			continue
		}
		if _, ok := existingSet[n]; ok {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, strings.TrimSpace(answer))
	}
	return out
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

func readAdminLevelsByNumber(ctx context.Context, client *encx.Client, gameID int) (map[int]int, error) {
	var levels []encx.AdminLevel
	err := runWithAntiSpamRetry("read levels", func() error {
		var callErr error
		levels, callErr = client.AdminGetLevels(ctx, gameID)
		return callErr
	})
	if err != nil {
		return nil, err
	}
	out := make(map[int]int, len(levels))
	for _, level := range levels {
		if level.Number > 0 && level.ID != 0 {
			out[level.Number] = level.ID
		}
	}
	return out, nil
}

func syncMissingScenario(ctx context.Context, cfg *config, client *encx.Client, doc *scenario.Document, progress func(string)) (importSyncStats, error) {
	stats := importSyncStats{}

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
	if existingCount < len(doc.Levels) {
		missing := len(doc.Levels) - existingCount
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
		levels, err = client.AdminGetLevels(ctx, cfg.gameId)
		if err != nil {
			return stats, fmt.Errorf("re-read existing levels: %w", err)
		}
	}
	levelIDs := make(map[int]int, len(levels))
	for _, level := range levels {
		if level.Number > 0 && level.ID != 0 {
			levelIDs[level.Number] = level.ID
		}
	}

	for idx, src := range doc.Levels {
		levelNum := idx + 1
		levelName := importLevelName(levelNum, src.Name)

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
		levelID := levelIDs[levelNum]
		if len(src.Bonuses) > 0 && levelID == 0 {
			return stats, fmt.Errorf("level %d: missing admin level ID for bonus import", levelNum)
		}
		if err := syncLevelBonusesToScenario(ctx, client, cfg.gameId, levelNum, levelID, src, &stats); err != nil {
			return stats, err
		}
		if err := syncLevelSectorsToScenario(ctx, client, cfg.gameId, levelNum, src, &stats); err != nil {
			return stats, err
		}

		progress(fmt.Sprintf("Synced level %d/%d: %s", levelNum, len(doc.Levels), levelName))
	}

	return stats, nil
}

func importLevelName(levelNum int, name string) string {
	if levelName := strings.TrimSpace(name); levelName != "" {
		return levelName
	}
	return fmt.Sprintf("Уровень %d", levelNum)
}

func redactScenarioBinaryPayloads(levels []scenario.Level) []scenario.Level {
	out := make([]scenario.Level, len(levels))
	for i, level := range levels {
		out[i] = level
		if len(level.Tasks) > 0 {
			out[i].Tasks = make([]string, len(level.Tasks))
			for j, task := range level.Tasks {
				out[i].Tasks[j] = redactBinaryPayloads(task)
			}
		}
		if len(level.Hints) > 0 {
			out[i].Hints = make([]scenario.Hint, len(level.Hints))
			for j, hint := range level.Hints {
				out[i].Hints[j] = hint
				out[i].Hints[j].Text = redactBinaryPayloads(hint.Text)
			}
		}
		if len(level.Bonuses) > 0 {
			out[i].Bonuses = make([]scenario.Bonus, len(level.Bonuses))
			for j, bonus := range level.Bonuses {
				out[i].Bonuses[j] = bonus
				out[i].Bonuses[j].Task = redactBinaryPayloads(bonus.Task)
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

func printDryRunLevel(level scenario.Level) {
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
	if len(level.Bonuses) > 0 {
		fmt.Printf("Bonuses (%d):\n", len(level.Bonuses))
		for i, bonus := range level.Bonuses {
			h, m, s := splitSeconds(bonus.AwardSeconds)
			fmt.Printf("  [Bonus %d] #%d %s (award %02d:%02d:%02d)\n", i+1, bonus.Number, strings.TrimSpace(bonus.Name), h, m, s)
			if strings.TrimSpace(bonus.Task) != "" {
				fmt.Println(indentBlock(bonus.Task, "    Task: "))
			}
			if len(bonus.Answers) > 0 {
				fmt.Printf("    Answers: %s\n", strings.Join(bonus.Answers, ", "))
			}
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
