package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/skrashevich/encx-cli/encx"
)

func cmdProfile(ctx context.Context, cfg *config, client *encx.Client) {
	profile, err := client.GetProfile(ctx)
	if err != nil {
		fatal("Failed to get profile: %v", err)
	}
	if cfg.jsonOutput {
		outputJSON(profile)
		return
	}
	fmt.Printf("ID:      %d\n", profile.ID)
	fmt.Printf("Login:   %s\n", profile.Login)
	fmt.Printf("Name:    %s\n", profile.Name)
	fmt.Printf("Rank:    %s\n", profile.Rank)
	if profile.Team != "" {
		fmt.Printf("Team:    %s (ID: %d)\n", profile.Team, profile.TeamID)
	}
	if profile.Domain != "" {
		fmt.Printf("Domain:  %s\n", profile.Domain)
	}
	if profile.Points != "" {
		fmt.Printf("Points:  %s\n", profile.Points)
	}
}

func cmdAdminGames(ctx context.Context, cfg *config, client *encx.Client) {
	games, err := client.AdminGetGames(ctx)
	if err != nil {
		fatal("Failed to get admin games: %v", err)
	}
	if cfg.jsonOutput {
		outputJSON(games)
		return
	}
	if len(games) == 0 {
		fmt.Println("No games found")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tTitle")
	fmt.Fprintln(w, "--\t-----")
	for _, g := range games {
		fmt.Fprintf(w, "%d\t%s\n", g.ID, g.Title)
	}
	w.Flush()
}

func cmdAdminLevels(ctx context.Context, cfg *config, client *encx.Client) {
	requireGameId(cfg)
	levels, err := client.AdminGetLevels(ctx, cfg.gameId)
	if err != nil {
		fatal("Failed to get levels: %v", err)
	}
	if cfg.jsonOutput {
		outputJSON(levels)
		return
	}
	if len(levels) == 0 {
		fmt.Println("No levels")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "#\tID\tName")
	fmt.Fprintln(w, "-\t--\t----")
	for _, l := range levels {
		fmt.Fprintf(w, "%d\t%d\t%s\n", l.Number, l.ID, l.Name)
	}
	w.Flush()
}

func cmdAdminCreateLevels(ctx context.Context, cfg *config, client *encx.Client, args []string) {
	requireGameId(cfg)
	if len(args) == 0 {
		fatal("Usage: encli admin-create-levels -game-id <id> <count>")
	}
	count, err := strconv.Atoi(args[0])
	if err != nil || count <= 0 {
		fatal("Invalid level count: %s", args[0])
	}
	if err := client.AdminCreateLevels(ctx, cfg.gameId, count); err != nil {
		fatal("Failed to create levels: %v", err)
	}
	if cfg.jsonOutput {
		outputJSON(map[string]any{"success": true, "count": count})
		return
	}
	fmt.Printf("Created %d level(s)\n", count)
}

func cmdAdminDeleteLevel(ctx context.Context, cfg *config, client *encx.Client, args []string) {
	requireGameId(cfg)
	if len(args) == 0 {
		fatal("Usage: encli admin-delete-level -game-id <id> <level-number>")
	}
	lvlNum, err := strconv.Atoi(args[0])
	if err != nil || lvlNum <= 0 {
		fatal("Invalid level number: %s", args[0])
	}
	if err := client.AdminDeleteLevel(ctx, cfg.gameId, lvlNum); err != nil {
		fatal("Failed to delete level: %v", err)
	}
	if cfg.jsonOutput {
		outputJSON(map[string]any{"success": true, "level": lvlNum})
		return
	}
	fmt.Printf("Level %d deleted\n", lvlNum)
}

func cmdAdminRenameLevel(ctx context.Context, cfg *config, client *encx.Client, args []string) {
	requireGameId(cfg)
	if len(args) < 2 {
		fatal("Usage: encli admin-rename-level -game-id <id> <level-id> <new-name>")
	}
	lvlID, err := strconv.Atoi(args[0])
	if err != nil {
		fatal("Invalid level ID: %s", args[0])
	}
	name := strings.Join(args[1:], " ")
	if err := client.AdminRenameLevels(ctx, cfg.gameId, map[int]string{lvlID: name}); err != nil {
		fatal("Failed to rename level: %v", err)
	}
	if cfg.jsonOutput {
		outputJSON(map[string]any{"success": true, "level_id": lvlID, "name": name})
		return
	}
	fmt.Printf("Level %d renamed to %q\n", lvlID, name)
}

func cmdAdminUpdateAutopass(ctx context.Context, cfg *config, client *encx.Client, args []string) {
	requireGameId(cfg)
	if len(args) < 2 {
		fatal("Usage: encli admin-set-autopass -game-id <id> <level-number> <HH:MM:SS> [penalty HH:MM:SS]")
	}
	lvlNum, err := strconv.Atoi(args[0])
	if err != nil || lvlNum <= 0 {
		fatal("Invalid level number: %s", args[0])
	}
	h, m, s := parseHMS(args[1])
	settings := encx.AdminLevelSettings{
		AutopassHours:   h,
		AutopassMinutes: m,
		AutopassSeconds: s,
	}
	if len(args) >= 3 {
		ph, pm, ps := parseHMS(args[2])
		settings.TimeoutPenalty = true
		settings.PenaltyHours = ph
		settings.PenaltyMinutes = pm
		settings.PenaltySeconds = ps
	}
	if err := client.AdminUpdateAutopass(ctx, cfg.gameId, lvlNum, settings); err != nil {
		fatal("Failed to update autopass: %v", err)
	}
	if cfg.jsonOutput {
		outputJSON(map[string]any{"success": true, "level": lvlNum, "settings": settings})
		return
	}
	fmt.Printf("Autopass updated for level %d\n", lvlNum)
}

func cmdAdminUpdateAnswerBlock(ctx context.Context, cfg *config, client *encx.Client, args []string) {
	requireGameId(cfg)
	if len(args) < 3 {
		fatal("Usage: encli admin-set-block -game-id <id> <level-number> <attempts> <period HH:MM:SS> [player]")
	}
	lvlNum, err := strconv.Atoi(args[0])
	if err != nil || lvlNum <= 0 {
		fatal("Invalid level number: %s", args[0])
	}
	attempts, err := strconv.Atoi(args[1])
	if err != nil {
		fatal("Invalid attempts count: %s", args[1])
	}
	h, m, s := parseHMS(args[2])
	applyFor := 0
	if len(args) >= 4 && args[3] == "player" {
		applyFor = 1
	}
	settings := encx.AdminLevelSettings{
		AttemptsNumber:        attempts,
		AttemptsPeriodHours:   h,
		AttemptsPeriodMinutes: m,
		AttemptsPeriodSeconds: s,
		ApplyForPlayer:        applyFor,
	}
	if err := client.AdminUpdateAnswerBlock(ctx, cfg.gameId, lvlNum, settings); err != nil {
		fatal("Failed to update answer block: %v", err)
	}
	if cfg.jsonOutput {
		outputJSON(map[string]any{"success": true, "level": lvlNum, "settings": settings})
		return
	}
	fmt.Printf("Answer block updated for level %d\n", lvlNum)
}

func cmdAdminCreateBonus(ctx context.Context, cfg *config, client *encx.Client, args []string) {
	requireGameId(cfg)
	if len(args) < 4 {
		fatal("Usage: encli admin-create-bonus -game-id <id> <level-num> <level-id> <name> <answer1> [answer2 ...]")
	}
	lvlNum, err := strconv.Atoi(args[0])
	if err != nil || lvlNum <= 0 {
		fatal("Invalid level number: %s", args[0])
	}
	lvlID, err := strconv.Atoi(args[1])
	if err != nil {
		fatal("Invalid level ID: %s", args[1])
	}
	name := args[2]
	answers := args[3:]

	bonus := encx.AdminBonus{
		Name:    name,
		LevelID: lvlID,
		Answers: answers,
	}
	if err := client.AdminCreateBonus(ctx, cfg.gameId, lvlNum, bonus); err != nil {
		fatal("Failed to create bonus: %v", err)
	}
	if cfg.jsonOutput {
		outputJSON(map[string]any{"success": true, "level": lvlNum, "name": name})
		return
	}
	fmt.Printf("Bonus %q created on level %d\n", name, lvlNum)
}

func cmdAdminDeleteBonus(ctx context.Context, cfg *config, client *encx.Client, args []string) {
	requireGameId(cfg)
	if len(args) < 2 {
		fatal("Usage: encli admin-delete-bonus -game-id <id> <level-number> <bonus-id>")
	}
	lvlNum, err := strconv.Atoi(args[0])
	if err != nil || lvlNum <= 0 {
		fatal("Invalid level number: %s", args[0])
	}
	bonusId, err := strconv.Atoi(args[1])
	if err != nil {
		fatal("Invalid bonus ID: %s", args[1])
	}
	if err := client.AdminDeleteBonus(ctx, cfg.gameId, lvlNum, bonusId); err != nil {
		fatal("Failed to delete bonus: %v", err)
	}
	if cfg.jsonOutput {
		outputJSON(map[string]any{"success": true, "bonus_id": bonusId})
		return
	}
	fmt.Printf("Bonus %d deleted\n", bonusId)
}

func cmdAdminCreateSector(ctx context.Context, cfg *config, client *encx.Client, args []string) {
	requireGameId(cfg)
	if len(args) < 3 {
		fatal("Usage: encli admin-create-sector -game-id <id> <level-number> <name> <answer1> [answer2 ...]")
	}
	lvlNum, err := strconv.Atoi(args[0])
	if err != nil || lvlNum <= 0 {
		fatal("Invalid level number: %s", args[0])
	}
	name := args[1]
	answers := args[2:]

	sector := encx.AdminSector{
		Name:    name,
		Answers: answers,
	}
	if err := client.AdminCreateSector(ctx, cfg.gameId, lvlNum, sector); err != nil {
		fatal("Failed to create sector: %v", err)
	}
	if cfg.jsonOutput {
		outputJSON(map[string]any{"success": true, "level": lvlNum, "name": name})
		return
	}
	fmt.Printf("Sector %q created on level %d\n", name, lvlNum)
}

func cmdAdminDeleteSector(ctx context.Context, cfg *config, client *encx.Client, args []string) {
	requireGameId(cfg)
	if len(args) < 2 {
		fatal("Usage: encli admin-delete-sector -game-id <id> <level-number> <sector-id>")
	}
	lvlNum, err := strconv.Atoi(args[0])
	if err != nil || lvlNum <= 0 {
		fatal("Invalid level number: %s", args[0])
	}
	sectorId, err := strconv.Atoi(args[1])
	if err != nil {
		fatal("Invalid sector ID: %s", args[1])
	}
	if err := client.AdminDeleteSector(ctx, cfg.gameId, lvlNum, sectorId); err != nil {
		fatal("Failed to delete sector: %v", err)
	}
	if cfg.jsonOutput {
		outputJSON(map[string]any{"success": true, "sector_id": sectorId})
		return
	}
	fmt.Printf("Sector %d deleted\n", sectorId)
}

func cmdAdminCreateHint(ctx context.Context, cfg *config, client *encx.Client, args []string) {
	requireGameId(cfg)
	if len(args) < 3 {
		fatal("Usage: encli admin-create-hint -game-id <id> <level-number> <delay HH:MM:SS> <text>")
	}
	lvlNum, err := strconv.Atoi(args[0])
	if err != nil || lvlNum <= 0 {
		fatal("Invalid level number: %s", args[0])
	}
	h, m, s := parseHMS(args[1])
	text := strings.Join(args[2:], " ")

	hint := encx.AdminHint{
		Text:    text,
		Hours:   h,
		Minutes: m,
		Seconds: s,
	}
	if err := client.AdminCreateHint(ctx, cfg.gameId, lvlNum, hint); err != nil {
		fatal("Failed to create hint: %v", err)
	}
	if cfg.jsonOutput {
		outputJSON(map[string]any{"success": true, "level": lvlNum})
		return
	}
	fmt.Printf("Hint created on level %d (delay: %02d:%02d:%02d)\n", lvlNum, h, m, s)
}

func cmdAdminDeleteHint(ctx context.Context, cfg *config, client *encx.Client, args []string) {
	requireGameId(cfg)
	if len(args) < 2 {
		fatal("Usage: encli admin-delete-hint -game-id <id> <level-number> <hint-id>")
	}
	lvlNum, err := strconv.Atoi(args[0])
	if err != nil || lvlNum <= 0 {
		fatal("Invalid level number: %s", args[0])
	}
	hintId, err := strconv.Atoi(args[1])
	if err != nil {
		fatal("Invalid hint ID: %s", args[1])
	}
	if err := client.AdminDeleteHint(ctx, cfg.gameId, lvlNum, hintId); err != nil {
		fatal("Failed to delete hint: %v", err)
	}
	if cfg.jsonOutput {
		outputJSON(map[string]any{"success": true, "hint_id": hintId})
		return
	}
	fmt.Printf("Hint %d deleted\n", hintId)
}

func cmdAdminCreateTask(ctx context.Context, cfg *config, client *encx.Client, args []string) {
	requireGameId(cfg)
	if len(args) < 2 {
		fatal("Usage: encli admin-create-task -game-id <id> <level-number> <text>")
	}
	lvlNum, err := strconv.Atoi(args[0])
	if err != nil || lvlNum <= 0 {
		fatal("Invalid level number: %s", args[0])
	}
	text := strings.Join(args[1:], " ")

	task := encx.AdminTask{
		Text:      text,
		ReplaceNl: true,
	}
	if err := client.AdminCreateTask(ctx, cfg.gameId, lvlNum, task); err != nil {
		fatal("Failed to create task: %v", err)
	}
	if cfg.jsonOutput {
		outputJSON(map[string]any{"success": true, "level": lvlNum})
		return
	}
	fmt.Printf("Task created on level %d\n", lvlNum)
}

func cmdAdminSetComment(ctx context.Context, cfg *config, client *encx.Client, args []string) {
	requireGameId(cfg)
	if len(args) < 2 {
		fatal("Usage: encli admin-set-comment -game-id <id> <level-number> <name> [comment]")
	}
	lvlNum, err := strconv.Atoi(args[0])
	if err != nil || lvlNum <= 0 {
		fatal("Invalid level number: %s", args[0])
	}
	name := args[1]
	comment := ""
	if len(args) > 2 {
		comment = strings.Join(args[2:], " ")
	}
	if err := client.AdminUpdateComment(ctx, cfg.gameId, lvlNum, name, comment); err != nil {
		fatal("Failed to update comment: %v", err)
	}
	if cfg.jsonOutput {
		outputJSON(map[string]any{"success": true, "level": lvlNum, "name": name})
		return
	}
	fmt.Printf("Level %d: name=%q comment=%q\n", lvlNum, name, comment)
}

func cmdAdminTeams(ctx context.Context, cfg *config, client *encx.Client) {
	requireGameId(cfg)
	// Need at least one level to fetch teams from
	levels, err := client.AdminGetLevels(ctx, cfg.gameId)
	if err != nil {
		fatal("Failed to get levels: %v", err)
	}
	if len(levels) == 0 {
		fatal("No levels found in game")
	}
	teams, err := client.AdminGetTeams(ctx, cfg.gameId, levels[0].Number)
	if err != nil {
		fatal("Failed to get teams: %v", err)
	}
	if cfg.jsonOutput {
		outputJSON(teams)
		return
	}
	if len(teams) == 0 {
		fmt.Println("No teams found")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tName")
	fmt.Fprintln(w, "--\t----")
	for _, t := range teams {
		fmt.Fprintf(w, "%s\t%s\n", t.ID, t.Name)
	}
	w.Flush()
}

func cmdAdminCorrections(ctx context.Context, cfg *config, client *encx.Client) {
	requireGameId(cfg)
	corrections, err := client.AdminGetCorrections(ctx, cfg.gameId)
	if err != nil {
		fatal("Failed to get corrections: %v", err)
	}
	if cfg.jsonOutput {
		outputJSON(corrections)
		return
	}
	if len(corrections) == 0 {
		fmt.Println("No corrections")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tDateTime\tTeam\tLevel\tReason\tTime\tComment")
	fmt.Fprintln(w, "--\t--------\t----\t-----\t------\t----\t-------")
	for _, c := range corrections {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			c.ID, c.DateTime, c.Team, c.Level, c.Reason, c.Time, c.Comment)
	}
	w.Flush()
}

func cmdAdminAddCorrection(ctx context.Context, cfg *config, client *encx.Client, args []string) {
	requireGameId(cfg)
	if len(args) < 4 {
		fatal("Usage: encli admin-add-correction -game-id <id> <team> <type:bonus|penalty> <HH:MM:SS> [level] [comment]")
	}
	teamName := args[0]
	corrType := "1" // bonus
	if args[1] == "penalty" {
		corrType = "2"
	}
	h, m, s := parseHMS(args[2])

	levelName := "0"
	comment := ""
	if len(args) >= 4 {
		levelName = args[3]
	}
	if len(args) >= 5 {
		comment = strings.Join(args[4:], " ")
	}

	corr := encx.AdminCorrectionAdd{
		TeamName:       teamName,
		LevelName:      levelName,
		CorrectionType: corrType,
		Days:           "0",
		Hours:          strconv.Itoa(h),
		Minutes:        strconv.Itoa(m),
		Seconds:        strconv.Itoa(s),
		Comment:        comment,
	}
	if err := client.AdminAddCorrection(ctx, cfg.gameId, corr); err != nil {
		fatal("Failed to add correction: %v", err)
	}
	if cfg.jsonOutput {
		outputJSON(map[string]any{"success": true})
		return
	}
	fmt.Printf("Correction added: %s %s %02d:%02d:%02d\n", teamName, args[1], h, m, s)
}

func cmdAdminDeleteCorrection(ctx context.Context, cfg *config, client *encx.Client, args []string) {
	requireGameId(cfg)
	if len(args) == 0 {
		fatal("Usage: encli admin-delete-correction -game-id <id> <correction-id>")
	}
	if err := client.AdminDeleteCorrection(ctx, cfg.gameId, args[0]); err != nil {
		fatal("Failed to delete correction: %v", err)
	}
	if cfg.jsonOutput {
		outputJSON(map[string]any{"success": true, "id": args[0]})
		return
	}
	fmt.Printf("Correction %s deleted\n", args[0])
}

func cmdAdminWipeGame(ctx context.Context, cfg *config, client *encx.Client) {
	requireGameId(cfg)

	progress := func(msg string) {
		if !cfg.jsonOutput {
			fmt.Println(msg)
		}
	}

	if err := client.AdminWipeGame(ctx, cfg.gameId, progress); err != nil {
		fatal("Failed to wipe game: %v", err)
	}

	if cfg.jsonOutput {
		outputJSON(map[string]any{"success": true, "game_id": cfg.gameId})
	}
}

func cmdAdminCopyGame(ctx context.Context, cfg *config, client *encx.Client, args []string) {
	requireGameId(cfg)
	if len(args) == 0 {
		fatal("Usage: encli admin-copy-game -game-id <source-id> <target-id>")
	}
	targetId, err := strconv.Atoi(args[0])
	if err != nil || targetId <= 0 {
		fatal("Invalid target game ID: %s", args[0])
	}

	progress := func(msg string) {
		if !cfg.jsonOutput {
			fmt.Println(msg)
		}
	}

	if err := client.AdminCopyGame(ctx, cfg.gameId, targetId, progress); err != nil {
		fatal("Failed to copy game: %v", err)
	}

	if cfg.jsonOutput {
		outputJSON(map[string]any{"success": true, "source": cfg.gameId, "target": targetId})
	}
}

// parseHMS parses a time string in format "HH:MM:SS" or "MM:SS" or "SS".
func parseHMS(s string) (h, m, sec int) {
	parts := strings.Split(s, ":")
	switch len(parts) {
	case 3:
		h, _ = strconv.Atoi(parts[0])
		m, _ = strconv.Atoi(parts[1])
		sec, _ = strconv.Atoi(parts[2])
	case 2:
		m, _ = strconv.Atoi(parts[0])
		sec, _ = strconv.Atoi(parts[1])
	case 1:
		sec, _ = strconv.Atoi(parts[0])
	}
	return
}
