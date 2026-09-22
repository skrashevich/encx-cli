package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/skrashevich/encx-cli/encx"
	"github.com/skrashevich/encx-cli/encx/scenario"
)

// Parse once in Go, rather than asking the model to reconstruct hundreds of
// levels from paginated HTML. Source contents never need to fill the LLM context.
func readAgentScenario(path string) (*scenario.Document, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("scenario path is required")
	}
	abs, err := resolveLocalPath(path)
	if err != nil {
		return nil, err
	}
	doc, err := scenario.ParseFile(abs)
	if err != nil {
		return nil, err
	}
	if len(doc.Levels) == 0 {
		return nil, fmt.Errorf("no scenario levels found")
	}
	return doc, nil
}

func scenarioSummary(doc *scenario.Document) map[string]any {
	tasks, hints, penaltyHints, sectors, bonuses, comments := 0, 0, 0, 0, 0, 0
	for _, level := range doc.Levels {
		tasks += len(level.Tasks)
		hints += len(level.Hints)
		penaltyHints += len(level.PenaltyHints)
		sectors += len(scenarioAdminSectors(level))
		bonuses += len(level.Bonuses)
		if strings.TrimSpace(level.Comment) != "" {
			comments++
		}
	}
	return map[string]any{"source_path": doc.SourcePath, "source_game_id": doc.GameID, "title": doc.GameTitle,
		"levels": len(doc.Levels), "tasks": tasks, "hints": hints, "penalty_hints": penaltyHints, "sectors": sectors, "bonuses": bonuses, "comments": comments,
		"embedded_assets": doc.EmbeddedAssets, "missing_assets": doc.MissingAssets}
}

func scenarioDifferences(want, got *scenario.Document) []string {
	var diffs []string
	if len(want.Levels) != len(got.Levels) {
		diffs = append(diffs, fmt.Sprintf("level count: want %d, got %d", len(want.Levels), len(got.Levels)))
	}
	current := make(map[int]scenario.Level, len(got.Levels))
	for _, level := range got.Levels {
		if _, exists := current[level.Number]; exists {
			diffs = append(diffs, fmt.Sprintf("duplicate level number %d", level.Number))
		}
		current[level.Number] = level
	}
	for i, src := range want.Levels {
		n := i + 1
		cur, ok := current[n]
		if !ok {
			diffs = append(diffs, fmt.Sprintf("level %d missing", n))
			continue
		}
		var fields []string
		if !namesEquivalent(importLevelName(n, src.Name), cur.Name) {
			fields = append(fields, "name")
		}
		if scenario.NormalizeComparableText(src.Comment) != scenario.NormalizeComparableText(cur.Comment) {
			fields = append(fields, "comment")
		}
		if src.AutopassSecond != cur.AutopassSecond || src.AutopassPenaltySecond != cur.AutopassPenaltySecond {
			fields = append(fields, "autopass")
		}
		if src.RequiredSectorsCount != cur.RequiredSectorsCount {
			fields = append(fields, "sector_completion")
		}
		if !taskNormsMatch(scenarioTaskNorms(src), scenarioTaskNorms(cur)) {
			fields = append(fields, "tasks")
		}
		if !taskNormsMatch(scenarioHintKeys(src), scenarioHintKeys(cur)) {
			fields = append(fields, "hints")
		}
		if !bonusKeysMatch(scenarioBonusKeys(src, 0), scenarioBonusKeys(cur, 0)) {
			fields = append(fields, "bonuses")
		}
		if !sectorGroupsMatch(src, scenarioAdminSectors(cur)) {
			fields = append(fields, "sectors")
		}
		if len(fields) > 0 {
			diffs = append(diffs, fmt.Sprintf("level %d: %s", n, strings.Join(fields, ", ")))
		}
	}
	return diffs
}

func verifyScenarioImport(ctx context.Context, client *encx.Client, gameID int, doc *scenario.Document) ([]string, error) {
	reportToolProgress(ctx, "Verifying imported scenario: reading back from server", "Проверка сценария: повторное чтение с сервера")
	actual, err := client.GetAdminGameScenario(ctx, gameID)
	if err != nil {
		return nil, fmt.Errorf("read back imported scenario: %w", err)
	}
	return scenarioDifferences(doc, actual), nil
}

func toolInspectScenario(path string) {
	doc, err := readAgentScenario(path)
	if err != nil {
		fatal("Failed to parse scenario: %v", err)
	}
	outputJSON(scenarioSummary(doc))
}

func toolVerifyScenario(ctx context.Context, cfg *config, client *encx.Client, path string) {
	requireGameId(cfg)
	doc, err := readAgentScenario(path)
	if err != nil {
		fatal("Failed to parse scenario: %v", err)
	}
	differences, err := verifyScenarioImport(ctx, client, cfg.gameId, doc)
	if err != nil {
		fatal("Scenario verification failed: %v", err)
	}
	result := scenarioSummary(doc)
	result["game_id"] = cfg.gameId
	result["verified"] = len(differences) == 0
	result["difference_count"] = len(differences)
	result["differences"] = differences[:min(len(differences), 20)]
	outputJSON(result)
}

func toolImportScenario(ctx context.Context, cfg *config, client *encx.Client, path string) {
	requireGameId(cfg)
	reportToolProgress(ctx, "Reading scenario file", "Чтение файла сценария")
	doc, err := readAgentScenario(path)
	if err != nil {
		fatal("Failed to parse scenario: %v", err)
	}
	if len(doc.MissingAssets) > 0 {
		fatal("Scenario has %d missing local assets; restore them before importing", len(doc.MissingAssets))
	}
	stats, err := syncMissingScenario(ctx, cfg, client, doc, func(string) {})
	result := scenarioSummary(doc)
	result["game_id"] = cfg.gameId
	result["stats"] = stats
	result["success"] = false
	result["verified"] = false
	if err != nil {
		result["error"] = fmt.Sprintf("Import interrupted; changes already applied are in stats. Verify before retrying: %v", err)
		outputJSON(result)
		return
	}
	differences, err := verifyScenarioImport(ctx, client, cfg.gameId, doc)
	if err != nil {
		result["error"] = err.Error()
	} else if len(differences) > 0 {
		result["error"] = "Imported scenario does not match source; do not report completion"
		result["difference_count"] = len(differences)
		result["differences"] = differences[:min(len(differences), 20)]
	} else {
		result["success"] = true
		result["verified"] = true
	}
	outputJSON(result)
}
