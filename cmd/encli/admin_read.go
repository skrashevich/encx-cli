package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/skrashevich/encx-cli/encx"
)

type adminLevelTaskView struct {
	ID int `json:"id"`
	encx.AdminTask
}

type adminLevelBonusView struct {
	ID int `json:"id"`
	encx.AdminBonus
}

type adminLevelHintView struct {
	ID int `json:"id"`
	encx.AdminHint
}

type adminLevelContent struct {
	GameID      int                      `json:"game_id"`
	LevelNumber int                      `json:"level_number"`
	Name        string                   `json:"name,omitempty"`
	Comment     string                   `json:"comment,omitempty"`
	Settings    *encx.AdminLevelSettings `json:"settings,omitempty"`
	Tasks       []adminLevelTaskView     `json:"tasks,omitempty"`
	Sectors     []encx.AdminSector       `json:"sectors,omitempty"`
	Bonuses     []adminLevelBonusView    `json:"bonuses,omitempty"`
	Hints       []adminLevelHintView     `json:"hints,omitempty"`
	Errors      map[string]string        `json:"errors,omitempty"`
}

func cmdAdminLevelContent(ctx context.Context, cfg *config, client *encx.Client, args []string) {
	requireGameId(cfg)
	if len(args) == 0 {
		fatal("Usage: encli admin-level-content -game-id <id> <level-number>")
	}
	levelNum, err := strconv.Atoi(args[0])
	if err != nil || levelNum <= 0 {
		fatal("Invalid level number: %s", args[0])
	}

	content := adminLevelContent{
		GameID:      cfg.gameId,
		LevelNumber: levelNum,
		Errors:      map[string]string{},
	}
	successes := 0

	if settings, err := client.AdminGetLevelSettings(ctx, cfg.gameId, levelNum); err != nil {
		content.Errors["settings"] = err.Error()
	} else {
		content.Settings = settings
		successes++
	}

	if name, comment, err := client.AdminGetComment(ctx, cfg.gameId, levelNum); err != nil {
		content.Errors["comment"] = err.Error()
	} else {
		content.Name = name
		content.Comment = comment
		successes++
	}

	if taskIDs, err := client.AdminGetTaskIds(ctx, cfg.gameId, levelNum); err != nil {
		content.Errors["tasks"] = err.Error()
	} else {
		content.Tasks = make([]adminLevelTaskView, 0, len(taskIDs))
		for _, taskID := range taskIDs {
			task, err := client.AdminGetTask(ctx, cfg.gameId, levelNum, taskID)
			if err != nil {
				content.Errors[fmt.Sprintf("task_%d", taskID)] = err.Error()
				continue
			}
			content.Tasks = append(content.Tasks, adminLevelTaskView{
				ID:        taskID,
				AdminTask: *task,
			})
		}
		successes++
	}

	if sectors, err := client.AdminGetSectorAnswers(ctx, cfg.gameId, levelNum); err != nil {
		content.Errors["sectors"] = err.Error()
	} else {
		content.Sectors = sectors
		successes++
	}

	if bonusIDs, err := client.AdminGetBonusIds(ctx, cfg.gameId, levelNum); err != nil {
		content.Errors["bonuses"] = err.Error()
	} else {
		content.Bonuses = make([]adminLevelBonusView, 0, len(bonusIDs))
		for _, bonusID := range bonusIDs {
			bonus, err := client.AdminGetBonus(ctx, cfg.gameId, levelNum, bonusID)
			if err != nil {
				content.Errors[fmt.Sprintf("bonus_%d", bonusID)] = err.Error()
				continue
			}
			content.Bonuses = append(content.Bonuses, adminLevelBonusView{
				ID:         bonusID,
				AdminBonus: *bonus,
			})
		}
		successes++
	}

	if hintIDs, err := client.AdminGetHintIds(ctx, cfg.gameId, levelNum); err != nil {
		content.Errors["hints"] = err.Error()
	} else {
		content.Hints = make([]adminLevelHintView, 0, len(hintIDs))
		for _, hintID := range hintIDs {
			hint, err := client.AdminGetHint(ctx, cfg.gameId, levelNum, hintID)
			if err != nil {
				content.Errors[fmt.Sprintf("hint_%d", hintID)] = err.Error()
				continue
			}
			content.Hints = append(content.Hints, adminLevelHintView{
				ID:        hintID,
				AdminHint: *hint,
			})
		}
		successes++
	}

	if len(content.Errors) == 0 {
		content.Errors = nil
	}
	if successes == 0 {
		errs := make([]string, 0, len(content.Errors))
		for section, msg := range content.Errors {
			errs = append(errs, fmt.Sprintf("%s: %s", section, msg))
		}
		fatal("Failed to read admin level content: %s", strings.Join(errs, "; "))
	}

	if cfg.jsonOutput {
		outputJSON(content)
		return
	}

	printAdminLevelContent(content)
}

func printAdminLevelContent(content adminLevelContent) {
	title := content.Name
	if title == "" {
		title = "(unnamed)"
	}
	fmt.Printf("Level %d: %s\n", content.LevelNumber, title)

	if content.Comment != "" {
		fmt.Printf("\nComment:\n%s\n", content.Comment)
	}

	if content.Settings != nil {
		fmt.Println("\nSettings:")
		fmt.Printf("  Autopass: %s\n", formatAdminClock(
			content.Settings.AutopassHours,
			content.Settings.AutopassMinutes,
			content.Settings.AutopassSeconds,
		))
		if content.Settings.TimeoutPenalty {
			fmt.Printf("  Timeout penalty: %s\n", formatAdminClock(
				content.Settings.PenaltyHours,
				content.Settings.PenaltyMinutes,
				content.Settings.PenaltySeconds,
			))
		}
		if content.Settings.AttemptsNumber > 0 {
			scope := "per team"
			if content.Settings.ApplyForPlayer == 1 {
				scope = "per player"
			}
			fmt.Printf("  Answer block: %d attempts / %s %s\n",
				content.Settings.AttemptsNumber,
				formatAdminClock(
					content.Settings.AttemptsPeriodHours,
					content.Settings.AttemptsPeriodMinutes,
					content.Settings.AttemptsPeriodSeconds,
				),
				scope,
			)
		}
	}

	if len(content.Tasks) > 0 {
		fmt.Println("\nTasks:")
		for _, task := range content.Tasks {
			fmt.Printf("  [task %d]\n", task.ID)
			fmt.Println(indentBlock(stripHTML(task.Text), "    "))
		}
	}

	if len(content.Sectors) > 0 {
		fmt.Println("\nSectors:")
		for i, sector := range content.Sectors {
			name := sector.Name
			if name == "" {
				name = fmt.Sprintf("Sector %d", i+1)
			}
			if sector.ID > 0 {
				fmt.Printf("  [sector %d] %s\n", sector.ID, name)
			} else {
				fmt.Printf("  %s\n", name)
			}
			if len(sector.Answers) > 0 {
				fmt.Printf("    Answers: %s\n", strings.Join(sector.Answers, ", "))
			}
		}
	}

	if len(content.Bonuses) > 0 {
		fmt.Println("\nBonuses:")
		for _, bonus := range content.Bonuses {
			fmt.Printf("  [bonus %d] %s\n", bonus.ID, bonus.Name)
			if bonus.Task != "" {
				fmt.Println(indentBlock(stripHTML(bonus.Task), "    Task: "))
			}
			if bonus.Hint != "" {
				fmt.Println(indentBlock(stripHTML(bonus.Hint), "    Hint: "))
			}
			if len(bonus.Answers) > 0 {
				fmt.Printf("    Answers: %s\n", strings.Join(bonus.Answers, ", "))
			}
		}
	}

	if len(content.Hints) > 0 {
		fmt.Println("\nHints:")
		for _, hint := range content.Hints {
			kind := "hint"
			if hint.IsPenalty {
				kind = "penalty hint"
			}
			fmt.Printf("  [%s %d] opens in %s\n", kind, hint.ID, formatAdminDelay(hint.Days, hint.Hours, hint.Minutes, hint.Seconds))
			if hint.Text != "" {
				fmt.Println(indentBlock(stripHTML(hint.Text), "    "))
			}
			if hint.PenaltyComment != "" {
				fmt.Println(indentBlock(stripHTML(hint.PenaltyComment), "    Comment: "))
			}
		}
	}

	if len(content.Errors) > 0 {
		fmt.Println("\nWarnings:")
		for section, msg := range content.Errors {
			fmt.Printf("  %s: %s\n", section, msg)
		}
	}
}

func formatAdminClock(h, m, s int) string {
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

func formatAdminDelay(days, h, m, s int) string {
	if days > 0 {
		return fmt.Sprintf("%dd %s", days, formatAdminClock(h, m, s))
	}
	return formatAdminClock(h, m, s)
}

func indentBlock(text, prefix string) string {
	if text == "" {
		return prefix
	}
	lines := strings.Split(text, "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}
