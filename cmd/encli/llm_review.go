package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/skrashevich/encx-cli/encx"
	"os"
	"strings"
)

type pendingAdminFix struct {
	Title       string           `json:"title"`
	Summary     string           `json:"summary"`
	LevelNumber int              `json:"level_number,omitempty"`
	Steps       []pendingFixStep `json:"steps"`
}

type pendingFixStep struct {
	Tool      string         `json:"tool"`
	Arguments map[string]any `json:"arguments"`
}

type proposalOutcome struct {
	Title    string
	Applied  bool
	Skipped  bool
	Stopped  bool
	Error    string
	StepRuns int
}

func shouldExposeToolInReview(name string) bool {
	if name == "propose_admin_fix" {
		return true
	}
	return !isAdminMutationTool(name)
}

func isAdminMutationTool(name string) bool {
	switch name {
	case "admin_create_levels",
		"admin_delete_level",
		"admin_rename_level",
		"admin_set_autopass",
		"admin_set_block",
		"admin_create_bonus",
		"admin_delete_bonus",
		"admin_create_sector",
		"admin_delete_sector",
		"admin_update_sector",
		"admin_create_hint",
		"admin_delete_hint",
		"admin_create_task",
		"admin_set_comment",
		"admin_add_correction",
		"admin_delete_correction",
		"admin_wipe_game",
		"admin_copy_game",
		"admin_update_game",
		"admin_not_deliver":
		return true
	default:
		return false
	}
}

func isProposalMutationTool(name string) bool {
	switch name {
	case "admin_set_autopass",
		"admin_set_block",
		"admin_create_bonus",
		"admin_delete_bonus",
		"admin_create_sector",
		"admin_delete_sector",
		"admin_update_sector",
		"admin_create_hint",
		"admin_delete_hint",
		"admin_create_task",
		"admin_set_comment":
		return true
	default:
		return false
	}
}

func parsePendingAdminFix(args map[string]any, defaultGameID int) (pendingAdminFix, error) {
	fix := pendingAdminFix{
		Title:       strings.TrimSpace(getAnyString(args["title"])),
		Summary:     strings.TrimSpace(getAnyString(args["summary"])),
		LevelNumber: getAnyInt(args["level_number"]),
	}
	if fix.Title == "" {
		return pendingAdminFix{}, errors.New("title is required")
	}
	if fix.Summary == "" {
		return pendingAdminFix{}, errors.New("summary is required")
	}
	rawSteps, ok := args["steps"].([]any)
	if !ok || len(rawSteps) == 0 {
		return pendingAdminFix{}, errors.New("at least one step is required")
	}
	if len(rawSteps) > maxFixSteps {
		return pendingAdminFix{}, fmt.Errorf("too many steps: %d", len(rawSteps))
	}
	fix.Steps = make([]pendingFixStep, 0, len(rawSteps))
	for i, rawStep := range rawSteps {
		stepMap, ok := rawStep.(map[string]any)
		if !ok {
			return pendingAdminFix{}, fmt.Errorf("step %d is not an object", i+1)
		}
		toolName := strings.TrimSpace(getAnyString(stepMap["tool"]))
		if !isProposalMutationTool(toolName) {
			return pendingAdminFix{}, fmt.Errorf("step %d uses unsupported tool %q", i+1, toolName)
		}
		argMap, ok := stepMap["arguments"].(map[string]any)
		if !ok {
			return pendingAdminFix{}, fmt.Errorf("step %d must include object arguments", i+1)
		}
		copiedArgs := cloneAnyMap(argMap)
		if gid := getAnyInt(copiedArgs["game_id"]); gid == 0 {
			if defaultGameID == 0 {
				return pendingAdminFix{}, fmt.Errorf("step %d is missing game_id", i+1)
			}
			copiedArgs["game_id"] = defaultGameID
		} else if defaultGameID != 0 && gid != defaultGameID {
			return pendingAdminFix{}, fmt.Errorf("step %d targets game_id %d, expected %d", i+1, gid, defaultGameID)
		}
		fix.Steps = append(fix.Steps, pendingFixStep{
			Tool:      toolName,
			Arguments: copiedArgs,
		})
	}
	return fix, nil
}

func runPendingFixApprovals(ctx context.Context, cfg *config, client *encx.Client, session *llmSession) {
	outcomes := make([]proposalOutcome, 0, len(session.pendingFixes))
	for i, fix := range session.pendingFixes {
		printFixProposalForApproval(session, i+1, len(session.pendingFixes), fix)
		switch promptApprovalDecision(session) {
		case "yes":
			outcomes = append(outcomes, applyPendingAdminFix(ctx, cfg, client, session, fix))
		case "quit":
			outcomes = append(outcomes, proposalOutcome{Title: fix.Title, Stopped: true})
			printApprovalMessage(session, session.reviewText(
				"Stopping approval flow. Remaining proposals were not applied.",
				"Останавливаю цепочку согласований. Остальные предложения не применялись.",
			))
			printApprovalSummary(session, outcomes)
			return
		default:
			outcomes = append(outcomes, proposalOutcome{Title: fix.Title, Skipped: true})
			printApprovalMessage(session, session.reviewText(
				"Skipped.",
				"Пропущено.",
			))
		}
	}
	printApprovalSummary(session, outcomes)
}

func applyPendingAdminFix(ctx context.Context, cfg *config, client *encx.Client, session *llmSession, fix pendingAdminFix) proposalOutcome {
	outcome := proposalOutcome{Title: fix.Title}
	for i, step := range fix.Steps {
		// Inject proposal-level fields into step arguments when missing.
		if fix.LevelNumber != 0 && getAnyInt(step.Arguments["level_number"]) == 0 {
			step.Arguments["level_number"] = fix.LevelNumber
		}
		gameID := getAnyInt(step.Arguments["game_id"])
		if gameID == 0 {
			gameID = cfg.gameId
		}
		enriched, err := enrichProposalStep(ctx, client, gameID, step)
		if err != nil {
			outcome.Error = err.Error()
			printApprovalMessage(session, session.reviewText(
				fmt.Sprintf("Failed before execution: %v", err),
				fmt.Sprintf("Не удалось подготовить применение: %v", err),
			))
			return outcome
		}
		argsJSON, err := json.Marshal(enriched.Arguments)
		if err != nil {
			outcome.Error = err.Error()
			printApprovalMessage(session, session.reviewText(
				fmt.Sprintf("Failed to encode step %d: %v", i+1, err),
				fmt.Sprintf("Не удалось закодировать шаг %d: %v", i+1, err),
			))
			return outcome
		}
		printApprovalMessage(session, session.reviewText(
			fmt.Sprintf("Applying step %d/%d: %s", i+1, len(fix.Steps), describeProposalStep(enriched)),
			fmt.Sprintf("Применяю шаг %d/%d: %s", i+1, len(fix.Steps), describeProposalStep(enriched)),
		))
		session.applyingApprovedFix = true
		result := executeToolCallSafe(ctx, cfg, client, session, enriched.Tool, string(argsJSON))
		session.applyingApprovedFix = false
		outcome.StepRuns++
		if errMsg := extractToolError(result); errMsg != "" {
			outcome.Error = errMsg
			printApprovalMessage(session, session.reviewText(
				fmt.Sprintf("Step failed: %s", errMsg),
				fmt.Sprintf("Шаг завершился ошибкой: %s", errMsg),
			))
			return outcome
		}
	}
	outcome.Applied = true
	printApprovalMessage(session, session.reviewText(
		"Applied.",
		"Применено.",
	))
	return outcome
}

func enrichProposalStep(ctx context.Context, client *encx.Client, gameID int, step pendingFixStep) (pendingFixStep, error) {
	if step.Tool != "admin_create_bonus" {
		return step, nil
	}
	if getAnyInt(step.Arguments["level_id"]) != 0 {
		return step, nil
	}
	levelNum := getAnyInt(step.Arguments["level_number"])
	if gameID == 0 || levelNum == 0 {
		return step, errors.New("admin_create_bonus requires level_number and game_id")
	}
	levels, err := client.AdminGetLevels(ctx, gameID)
	if err != nil {
		return step, fmt.Errorf("resolve level_id for level %d: %w", levelNum, err)
	}
	for _, lvl := range levels {
		if lvl.Number == levelNum {
			step.Arguments["level_id"] = lvl.ID
			return step, nil
		}
	}
	return step, fmt.Errorf("level %d not found while resolving level_id", levelNum)
}

func describeProposalStep(step pendingFixStep) string {
	switch step.Tool {
	case "admin_set_comment":
		return fmt.Sprintf("set level %d name/comment to %q", getAnyInt(step.Arguments["level_number"]), getAnyString(step.Arguments["name"]))
	case "admin_set_autopass":
		return fmt.Sprintf("set autopass on level %d to %s", getAnyInt(step.Arguments["level_number"]), getAnyString(step.Arguments["time"]))
	case "admin_set_block":
		return fmt.Sprintf("update answer block on level %d", getAnyInt(step.Arguments["level_number"]))
	case "admin_create_bonus":
		return fmt.Sprintf("create bonus %q on level %d", getAnyString(step.Arguments["name"]), getAnyInt(step.Arguments["level_number"]))
	case "admin_delete_bonus":
		return fmt.Sprintf("delete bonus %d on level %d", getAnyInt(step.Arguments["bonus_id"]), getAnyInt(step.Arguments["level_number"]))
	case "admin_create_sector":
		return fmt.Sprintf("create sector %q on level %d", getAnyString(step.Arguments["name"]), getAnyInt(step.Arguments["level_number"]))
	case "admin_delete_sector":
		return fmt.Sprintf("delete sector %d on level %d", getAnyInt(step.Arguments["sector_id"]), getAnyInt(step.Arguments["level_number"]))
	case "admin_update_sector":
		return fmt.Sprintf("update sector %d on level %d", getAnyInt(step.Arguments["sector_id"]), getAnyInt(step.Arguments["level_number"]))
	case "admin_create_hint":
		return fmt.Sprintf("create hint on level %d with delay %s", getAnyInt(step.Arguments["level_number"]), getAnyString(step.Arguments["delay"]))
	case "admin_delete_hint":
		return fmt.Sprintf("delete hint %d on level %d", getAnyInt(step.Arguments["hint_id"]), getAnyInt(step.Arguments["level_number"]))
	case "admin_create_task":
		return fmt.Sprintf("create task on level %d", getAnyInt(step.Arguments["level_number"]))
	default:
		return step.Tool
	}
}

func printFixProposalForApproval(session *llmSession, idx, total int, fix pendingAdminFix) {
	lines := []string{
		fmt.Sprintf("[%d/%d] %s", idx, total, fix.Title),
	}
	if fix.LevelNumber > 0 {
		lines = append(lines, session.reviewText(
			fmt.Sprintf("Level: %d", fix.LevelNumber),
			fmt.Sprintf("Уровень: %d", fix.LevelNumber),
		))
	}
	lines = append(lines, session.reviewText(
		"Why: "+fix.Summary,
		"Почему: "+fix.Summary,
	))
	lines = append(lines, session.reviewText("Steps:", "Шаги:"))
	for _, step := range fix.Steps {
		lines = append(lines, "  - "+describeProposalStep(step))
	}
	printApprovalMessage(session, strings.Join(lines, "\n"))
}

func promptApprovalDecision(session *llmSession) string {
	for {
		answer := strings.ToLower(strings.TrimSpace(prompt(session.reviewText(
			"Apply this fix? [y/N/q]: ",
			"Применить это исправление? [y/N/q]: ",
		))))
		switch answer {
		case "y", "yes", "д", "да":
			return "yes"
		case "", "n", "no", "н", "нет":
			return "no"
		case "q", "quit", "в", "выход":
			return "quit"
		}
		printApprovalMessage(session, session.reviewText(
			"Please answer y, n, or q.",
			"Ответьте y, n или q.",
		))
	}
}

func printApprovalSummary(session *llmSession, outcomes []proposalOutcome) {
	if len(outcomes) == 0 {
		return
	}
	var applied, skipped, stopped, failed int
	for _, outcome := range outcomes {
		switch {
		case outcome.Applied:
			applied++
		case outcome.Stopped:
			stopped++
		case outcome.Error != "":
			failed++
		case outcome.Skipped:
			skipped++
		}
	}
	lines := []string{
		session.reviewText("Approval summary:", "Итог согласований:"),
		session.reviewText(fmt.Sprintf("Applied: %d", applied), fmt.Sprintf("Применено: %d", applied)),
		session.reviewText(fmt.Sprintf("Skipped: %d", skipped), fmt.Sprintf("Пропущено: %d", skipped)),
		session.reviewText(fmt.Sprintf("Failed: %d", failed), fmt.Sprintf("С ошибкой: %d", failed)),
	}
	if stopped > 0 {
		lines = append(lines, session.reviewText(
			fmt.Sprintf("Stopped early: %d", stopped),
			fmt.Sprintf("Остановлено досрочно: %d", stopped),
		))
	}
	for _, outcome := range outcomes {
		if outcome.Error == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", outcome.Title, outcome.Error))
	}
	fmt.Println(strings.Join(lines, "\n"))
}

func printApprovalMessage(session *llmSession, message string) {
	fmt.Fprintln(os.Stderr, message)
}
