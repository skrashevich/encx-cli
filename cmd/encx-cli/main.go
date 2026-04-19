// Command encx-cli is a CLI tool for interacting with the Encounter (en.cx) game engine.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/svk/encx/encx"
)

var (
	domain   = flag.String("domain", "demo.en.cx", "Encounter domain")
	login    = flag.String("login", "", "Login username")
	password = flag.String("password", "", "Login password")
	gameId   = flag.Int("game-id", 0, "Game ID")
	insecure = flag.Bool("insecure", false, "Skip TLS certificate verification")
	useHTTP  = flag.Bool("http", false, "Use plain HTTP instead of HTTPS")
)

func usage() {
	fmt.Fprintf(os.Stderr, `Usage: encx-cli [flags] <command>

Commands:
  login       Authenticate and save session
  logout      Clear saved session
  games       List available games on the domain
  status      Show current game state
  send-code   Send a level code (first positional arg)
  send-bonus  Send a bonus code (first positional arg)
  hint        Request a penalty hint (first positional arg = hint ID)

Flags:
`)
	flag.PrintDefaults()
}

func main() {
	flag.Usage = usage
	flag.Parse()

	if flag.NArg() < 1 {
		usage()
		os.Exit(1)
	}

	cmd := flag.Arg(0)

	var opts []encx.Option
	if *insecure {
		opts = append(opts, encx.WithInsecureTLS())
	}
	if *useHTTP {
		opts = append(opts, encx.WithHTTP())
	}

	client := encx.New(*domain, opts...)
	ctx := context.Background()

	switch cmd {
	case "login":
		cmdLogin(ctx, client)
	case "logout":
		cmdLogout()
	case "games":
		loadSession(client)
		cmdGames(ctx, client)
	case "status":
		requireAuth(ctx, client)
		cmdStatus(ctx, client)
	case "send-code":
		requireAuth(ctx, client)
		cmdSendCode(ctx, client)
	case "send-bonus":
		requireAuth(ctx, client)
		cmdSendBonus(ctx, client)
	case "hint":
		requireAuth(ctx, client)
		cmdHint(ctx, client)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		usage()
		os.Exit(1)
	}
}

// --- Session persistence ---

func sessionDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "encx-cli")
}

func sessionFile() string {
	// Sanitize domain for filename
	safe := strings.ReplaceAll(*domain, "/", "_")
	safe = strings.ReplaceAll(safe, ":", "_")
	return filepath.Join(sessionDir(), safe+".json")
}

func saveSession(client *encx.Client) {
	data, err := client.ExportCookies()
	if err != nil {
		return
	}
	os.MkdirAll(sessionDir(), 0700)
	os.WriteFile(sessionFile(), data, 0600)
}

func loadSession(client *encx.Client) bool {
	data, err := os.ReadFile(sessionFile())
	if err != nil {
		return false
	}
	return client.ImportCookies(data) == nil
}

func clearSession() {
	os.Remove(sessionFile())
}

func requireAuth(ctx context.Context, client *encx.Client) {
	// Try saved session first
	if loadSession(client) && *login == "" {
		return
	}

	// Fall back to login with credentials
	if *login == "" || *password == "" {
		fatal("No saved session. Run 'login' first, or pass --login and --password")
	}
	resp, err := client.Login(ctx, *login, *password)
	if err != nil {
		fatal("Login failed: %v", err)
	}
	if resp.Error != 0 {
		fatal("Login error %d: %s", resp.Error, encx.LoginErrorText(resp.Error))
	}
	saveSession(client)
}

// --- Commands ---

func cmdLogin(ctx context.Context, client *encx.Client) {
	if *login == "" || *password == "" {
		fatal("--login and --password are required")
	}
	resp, err := client.Login(ctx, *login, *password)
	if err != nil {
		fatal("Login failed: %v", err)
	}
	if resp.Error != 0 {
		fmt.Printf("Login error %d: %s\n", resp.Error, encx.LoginErrorText(resp.Error))
		os.Exit(1)
	}
	saveSession(client)
	fmt.Printf("Login successful (session saved to %s)\n", sessionFile())
}

func cmdLogout() {
	clearSession()
	fmt.Println("Session cleared")
}

func cmdGames(ctx context.Context, client *encx.Client) {
	games, err := client.GetDomainGames(ctx)
	if err != nil {
		fatal("Failed to get games: %v", err)
	}
	if len(games) == 0 {
		fmt.Println("No games found")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tTitle")
	fmt.Fprintln(w, "--\t-----")
	for _, g := range games {
		fmt.Fprintf(w, "%d\t%s\n", g.GameId, g.Title)
	}
	w.Flush()
}

func cmdStatus(ctx context.Context, client *encx.Client) {
	requireGameId()
	model, err := client.GetGameModel(ctx, *gameId)
	if err != nil {
		fatal("Failed to get game model: %v", err)
	}

	fmt.Printf("Game ID: %d\n", model.GameId)
	fmt.Printf("User ID: %d\n", model.UserId)
	fmt.Printf("Event:   %v\n", model.Event)

	if model.Level == nil {
		fmt.Println("No active level")
		return
	}

	l := model.Level
	fmt.Printf("\nLevel %d/%d: %s\n", l.Number, len(model.Levels), l.Name)

	if l.Timeout > 0 {
		fmt.Printf("Timer:   %s remaining (timeout award: %ds)\n",
			formatDuration(l.TimeoutSecondsRemain), l.TimeoutAward)
	}
	fmt.Printf("Sectors: %d left to close (required: %d)\n",
		l.SectorsLeftToClose, l.RequiredSectorsCount)

	if len(l.Sectors) > 0 {
		fmt.Println("\nSectors:")
		for _, s := range l.Sectors {
			status := "[ ]"
			if s.IsAnswered {
				status = "[x]"
			}
			fmt.Printf("  %s %s", status, s.Name)
			if s.IsAnswered && s.Answer != "" {
				fmt.Printf(" (%s)", s.Answer)
			}
			fmt.Println()
		}
	}

	if len(l.Bonuses) > 0 {
		fmt.Println("\nBonuses:")
		for _, b := range l.Bonuses {
			status := "[ ]"
			if b.IsAnswered {
				status = "[x]"
			}
			fmt.Printf("  %s %s\n", status, b.Name)
		}
	}

	if len(l.PenaltyHelps) > 0 {
		fmt.Println("\nPenalty Hints:")
		for _, h := range l.PenaltyHelps {
			if h.PenaltyComment != "" {
				fmt.Printf("  #%d: %s (penalty: %ds)\n", h.Number, h.PenaltyComment, h.Penalty)
			} else {
				fmt.Printf("  #%d: [locked, opens in %s] (penalty: %ds, pid: %d)\n",
					h.Number, formatDuration(h.RemainSeconds), h.Penalty, h.PenaltyHelpId)
			}
		}
	}

	if len(l.MixedActions) > 0 {
		fmt.Printf("\nRecent codes (%d):\n", len(l.MixedActions))
		limit := min(len(l.MixedActions), 10)
		for _, a := range l.MixedActions[:limit] {
			mark := "x"
			if a.IsCorrect {
				mark = "v"
			}
			ts := ""
			if parts := strings.SplitN(a.LocDateTime, " ", 2); len(parts) == 2 {
				ts = parts[1]
			}
			fmt.Printf("  [%s] %s %-15s %s\n", mark, ts, a.Login, a.Answer)
		}
	}
}

func cmdSendCode(ctx context.Context, client *encx.Client) {
	requireGameId()
	code := flag.Arg(1)
	if code == "" {
		fatal("Usage: encx-cli send-code <code>")
	}

	model, err := client.GetGameModel(ctx, *gameId)
	if err != nil {
		fatal("Failed to get game model: %v", err)
	}
	if model.Level == nil {
		fatal("No active level")
	}

	result, err := client.SendCode(ctx, *gameId, model.Level.LevelId, code)
	if err != nil {
		fatal("Failed to send code: %v", err)
	}
	printActionResult(result, "Level")
}

func cmdSendBonus(ctx context.Context, client *encx.Client) {
	requireGameId()
	code := flag.Arg(1)
	if code == "" {
		fatal("Usage: encx-cli send-bonus <code>")
	}

	model, err := client.GetGameModel(ctx, *gameId)
	if err != nil {
		fatal("Failed to get game model: %v", err)
	}
	if model.Level == nil {
		fatal("No active level")
	}

	result, err := client.SendBonusCode(ctx, *gameId, model.Level.LevelId, code)
	if err != nil {
		fatal("Failed to send bonus code: %v", err)
	}
	printActionResult(result, "Bonus")
}

func cmdHint(ctx context.Context, client *encx.Client) {
	requireGameId()
	pidStr := flag.Arg(1)
	if pidStr == "" {
		fatal("Usage: encx-cli hint <penalty-hint-id>")
	}
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		fatal("Invalid hint ID: %s", pidStr)
	}

	model, err := client.GetPenaltyHint(ctx, *gameId, pid)
	if err != nil {
		fatal("Failed to request hint: %v", err)
	}
	fmt.Println("Penalty hint requested")
	if model.Level != nil {
		for _, h := range model.Level.PenaltyHelps {
			if h.PenaltyHelpId == pid && h.PenaltyComment != "" {
				fmt.Printf("Hint #%d: %s\n", h.Number, h.PenaltyComment)
			}
		}
	}
}

func printActionResult(model *encx.GameModel, actionType string) {
	if model.EngineAction == nil {
		fmt.Println("Code sent (no action result in response)")
		return
	}

	var result *encx.ActionResult
	switch actionType {
	case "Level":
		result = model.EngineAction.LevelAction
	case "Bonus":
		result = model.EngineAction.BonusAction
	}

	if result == nil || result.IsCorrectAnswer == nil {
		fmt.Println("Code sent")
		return
	}

	if *result.IsCorrectAnswer {
		fmt.Println("CORRECT!")
	} else {
		fmt.Println("Wrong code")
	}
}

func requireGameId() {
	if *gameId == 0 {
		fatal("--game-id is required")
	}
}

func formatDuration(seconds int) string {
	h := seconds / 3600
	m := (seconds % 3600) / 60
	s := seconds % 60
	if h > 0 {
		return fmt.Sprintf("%dh%02dm%02ds", h, m, s)
	}
	return fmt.Sprintf("%dm%02ds", m, s)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
