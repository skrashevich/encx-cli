// Command encli is a CLI tool for interacting with the Encounter (en.cx) game engine.
package main

import (
	"bufio"
	"cmp"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"golang.org/x/term"

	"github.com/skrashevich/encx-cli/encx"
)

var version = "dev"

// Config holds parsed CLI configuration.
type config struct {
	domain   string
	login    string
	password string
	gameId   int
	insecure bool
	useHTTP  bool
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	if os.Args[1] == "-v" || os.Args[1] == "--version" || os.Args[1] == "version" {
		fmt.Println("encli", version)
		return
	}

	// Find the subcommand: first arg that doesn't start with '-'
	var cmd string
	var cmdIdx int
	for i := 1; i < len(os.Args); i++ {
		if !strings.HasPrefix(os.Args[i], "-") {
			cmd = os.Args[i]
			cmdIdx = i
			break
		}
		// Skip flag value (e.g. -domain demo.en.cx)
		if i+1 < len(os.Args) && !strings.HasPrefix(os.Args[i+1], "-") &&
			strings.HasPrefix(os.Args[i], "-") && !strings.Contains(os.Args[i], "=") &&
			os.Args[i] != "-insecure" && os.Args[i] != "-http" {
			i++ // skip the value
		}
	}

	if cmd == "" {
		printUsage()
		os.Exit(1)
	}

	// Remove subcommand from args so flag.Parse works on the rest
	args := make([]string, 0, len(os.Args)-1)
	args = append(args, os.Args[1:cmdIdx]...)
	args = append(args, os.Args[cmdIdx+1:]...)

	fs := flag.NewFlagSet("encli", flag.ExitOnError)
	cfg := &config{}

	fs.StringVar(&cfg.domain, "domain", cmp.Or(os.Getenv("ENCX_DOMAIN"), "demo.en.cx"), "Encounter domain (env: ENCX_DOMAIN)")
	fs.StringVar(&cfg.login, "login", os.Getenv("ENCX_LOGIN"), "Login username (env: ENCX_LOGIN)")
	fs.StringVar(&cfg.password, "password", os.Getenv("ENCX_PASSWORD"), "Login password (env: ENCX_PASSWORD)")
	fs.IntVar(&cfg.gameId, "game-id", envInt("ENCX_GAME_ID", 0), "Game ID (env: ENCX_GAME_ID)")
	fs.BoolVar(&cfg.insecure, "insecure", envBool("ENCX_INSECURE"), "Skip TLS verification (env: ENCX_INSECURE)")
	fs.BoolVar(&cfg.useHTTP, "http", false, "Use plain HTTP instead of HTTPS")

	fs.Usage = func() { printCommandHelp(cmd) }
	fs.Parse(args)

	var opts []encx.Option
	if cfg.insecure {
		opts = append(opts, encx.WithInsecureTLS())
	}
	if cfg.useHTTP {
		opts = append(opts, encx.WithHTTP())
	}

	client := encx.New(cfg.domain, opts...)
	ctx := context.Background()
	positional := fs.Args()

	switch cmd {
	case "login":
		cmdLogin(ctx, cfg, client)
	case "logout":
		cmdLogout(cfg)
	case "games":
		loadSession(cfg, client)
		cmdGames(ctx, client)
	case "game-list":
		loadSession(cfg, client)
		cmdGameList(ctx, client)
	case "status":
		requireAuth(ctx, cfg, client)
		cmdStatus(ctx, cfg, client)
	case "task":
		requireAuth(ctx, cfg, client)
		cmdTask(ctx, cfg, client)
	case "messages":
		requireAuth(ctx, cfg, client)
		cmdMessages(ctx, cfg, client)
	case "enter":
		requireAuth(ctx, cfg, client)
		cmdEnter(ctx, cfg, client)
	case "send-code":
		requireAuth(ctx, cfg, client)
		cmdSendCode(ctx, cfg, client, positional)
	case "send-bonus":
		requireAuth(ctx, cfg, client)
		cmdSendBonus(ctx, cfg, client, positional)
	case "hint":
		requireAuth(ctx, cfg, client)
		cmdHint(ctx, cfg, client, positional)
	case "help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `encli — Encounter (en.cx) game engine client

Usage: encli <command> [flags]

Commands:
  login       Authenticate and save session
  logout      Clear saved session
  games       List available games (HTML scraping)
  game-list   List games with full details (JSON API)
  status      Show current game state
  task        Show current level task/assignment
  messages    Show messages from organizers
  enter       Enter a game (submit application)
  send-code   Send a level code
  send-bonus  Send a bonus code
  hint        Request a penalty hint

Global flags:
  -domain      Encounter domain (default: demo.en.cx)
  -login       Login username
  -password    Login password
  -game-id     Game ID
  -insecure    Skip TLS certificate verification
  -http        Use plain HTTP instead of HTTPS

Environment variables:
  ENCX_DOMAIN     Domain (default: demo.en.cx)
  ENCX_LOGIN      Login username
  ENCX_PASSWORD   Login password
  ENCX_GAME_ID    Game ID
  ENCX_INSECURE   Skip TLS verification (1/true)

Examples:
  encli login -login svk -password secret -insecure
  encli games
  encli game-list
  encli status -game-id 27053
  encli task -game-id 27053
  encli messages -game-id 27053
  encli enter -game-id 27053
  encli send-code -game-id 27053 "CODE123"
  encli send-bonus -game-id 27053 "BONUS1"
  encli hint -game-id 27053 42
  encli logout
`)
}

func printCommandHelp(cmd string) {
	switch cmd {
	case "login":
		fmt.Fprintln(os.Stderr, "Usage: encli login [-login <user>] [-password <pass>] [-domain <domain>] [-insecure] [-http]")
		fmt.Fprintln(os.Stderr, "  Authenticate and save session. Prompts for credentials if not provided.")
	case "logout":
		fmt.Fprintln(os.Stderr, "Usage: encli logout [-domain <domain>]")
		fmt.Fprintln(os.Stderr, "  Clear saved session for the specified domain.")
	case "games":
		fmt.Fprintln(os.Stderr, "Usage: encli games [-domain <domain>] [-insecure] [-http]")
		fmt.Fprintln(os.Stderr, "  List available games by scraping domain HTML page.")
	case "game-list":
		fmt.Fprintln(os.Stderr, "Usage: encli game-list [-domain <domain>] [-insecure] [-http]")
		fmt.Fprintln(os.Stderr, "  List games with full details via JSON API (/home/?json=1).")
	case "status":
		fmt.Fprintln(os.Stderr, "Usage: encli status -game-id <id> [-domain <domain>]")
		fmt.Fprintln(os.Stderr, "  Show current game state: level, sectors, bonuses, hints, messages.")
	case "task":
		fmt.Fprintln(os.Stderr, "Usage: encli task -game-id <id>")
		fmt.Fprintln(os.Stderr, "  Show current level task/assignment text.")
	case "messages":
		fmt.Fprintln(os.Stderr, "Usage: encli messages -game-id <id>")
		fmt.Fprintln(os.Stderr, "  Show messages from game organizers.")
	case "enter":
		fmt.Fprintln(os.Stderr, "Usage: encli enter -game-id <id>")
		fmt.Fprintln(os.Stderr, "  Submit application to enter a game.")
	case "send-code":
		fmt.Fprintln(os.Stderr, "Usage: encli send-code -game-id <id> <code>")
		fmt.Fprintln(os.Stderr, "  Send a level code answer.")
	case "send-bonus":
		fmt.Fprintln(os.Stderr, "Usage: encli send-bonus -game-id <id> <code>")
		fmt.Fprintln(os.Stderr, "  Send a bonus code answer.")
	case "hint":
		fmt.Fprintln(os.Stderr, "Usage: encli hint -game-id <id> <hint-id>")
		fmt.Fprintln(os.Stderr, "  Request a penalty hint by its ID.")
	default:
		printUsage()
	}
}

// --- Session persistence ---

func sessionDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "encli")
}

func sessionFile(cfg *config) string {
	safe := strings.ReplaceAll(cfg.domain, "/", "_")
	safe = strings.ReplaceAll(safe, ":", "_")
	return filepath.Join(sessionDir(), safe+".json")
}

func saveSession(cfg *config, client *encx.Client) {
	data, err := client.ExportCookies()
	if err != nil {
		return
	}
	os.MkdirAll(sessionDir(), 0700)
	os.WriteFile(sessionFile(cfg), data, 0600)
}

func loadSession(cfg *config, client *encx.Client) bool {
	data, err := os.ReadFile(sessionFile(cfg))
	if err != nil {
		return false
	}
	return client.ImportCookies(data) == nil
}

func requireAuth(ctx context.Context, cfg *config, client *encx.Client) {
	if loadSession(cfg, client) && cfg.login == "" {
		return
	}
	if cfg.login == "" || cfg.password == "" {
		fatal("No saved session. Run 'encli login' first, or pass -login and -password")
	}
	resp, err := client.Login(ctx, cfg.login, cfg.password)
	if err != nil {
		fatal("Login failed: %v", err)
	}
	if resp.Error != 0 {
		fatal("Login error %d: %s", resp.Error, encx.LoginErrorText(resp.Error))
	}
	saveSession(cfg, client)
}

// --- Commands ---

func cmdLogin(ctx context.Context, cfg *config, client *encx.Client) {
	if cfg.login == "" {
		cfg.login = prompt("Login: ")
	}
	if cfg.password == "" {
		cfg.password = promptPassword("Password: ")
	}
	resp, err := client.Login(ctx, cfg.login, cfg.password)
	if err != nil {
		fatal("Login failed: %v", err)
	}
	if resp.Error != 0 {
		fatal("Login error %d: %s", resp.Error, encx.LoginErrorText(resp.Error))
	}
	saveSession(cfg, client)
	fmt.Printf("Login successful (session saved to %s)\n", sessionFile(cfg))
}

func cmdLogout(cfg *config) {
	os.Remove(sessionFile(cfg))
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

func cmdGameList(ctx context.Context, client *encx.Client) {
	list, err := client.GetGameList(ctx)
	if err != nil {
		fatal("Failed to get game list: %v", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)

	if len(list.ActiveGames) > 0 {
		fmt.Fprintln(w, "=== Active Games ===")
		fmt.Fprintln(w, "ID\tTitle\tType\tZone")
		fmt.Fprintln(w, "--\t-----\t----\t----")
		for _, g := range list.ActiveGames {
			fmt.Fprintf(w, "%d\t%s\t%s\t%s\n", g.GameID, g.Title, gameTypeName(g.GameTypeID), zoneName(g.ZoneId))
		}
		fmt.Fprintln(w)
	}

	if len(list.ComingGames) > 0 {
		fmt.Fprintln(w, "=== Coming Games ===")
		fmt.Fprintln(w, "ID\tTitle\tType\tZone\tStart")
		fmt.Fprintln(w, "--\t-----\t----\t----\t-----")
		for _, g := range list.ComingGames {
			start := ""
			if g.StartDateTime != nil && g.StartDateTime.Timestamp > 0 {
				t := time.Unix(g.StartDateTime.Timestamp, 0)
				start = t.Format("02.01.2006 15:04")
			}
			fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\n", g.GameID, g.Title, gameTypeName(g.GameTypeID), zoneName(g.ZoneId), start)
		}
	}

	w.Flush()

	if len(list.ActiveGames) == 0 && len(list.ComingGames) == 0 {
		fmt.Println("No games found")
	}
}

func cmdStatus(ctx context.Context, cfg *config, client *encx.Client) {
	requireGameId(cfg)
	model, err := client.GetGameModel(ctx, cfg.gameId)
	if err != nil {
		fatal("Failed to get game model: %v", err)
	}

	eventCode := parseEventCode(model.Event)

	fmt.Printf("Game:    %s (ID: %d)\n", model.GameTitle, model.GameId)
	if model.TeamName != "" {
		fmt.Printf("Team:    %s (ID: %d)\n", model.TeamName, model.TeamId)
	}
	fmt.Printf("Player:  %s (ID: %d)\n", model.Login, model.UserId)
	fmt.Printf("Event:   %s (%d)\n", encx.EventText(eventCode), eventCode)

	if eventCode != encx.EventGameNormal {
		return
	}

	if model.Level == nil {
		fmt.Println("No active level")
		return
	}

	l := model.Level
	fmt.Printf("\nLevel %d/%d: %s\n", l.Number, len(model.Levels), l.Name)

	if l.IsPassed {
		fmt.Println("Status:  PASSED")
	}
	if l.Dismissed {
		fmt.Println("Status:  DISMISSED")
	}

	if l.Timeout > 0 {
		fmt.Printf("Timer:   %s remaining (timeout award: %ds)\n",
			formatDuration(l.TimeoutSecondsRemain), l.TimeoutAward)
	}

	if l.HasAnswerBlockRule && l.BlockDuration > 0 {
		fmt.Printf("Block:   %s remaining (%d attempts per %ds)\n",
			formatDuration(l.BlockDuration), l.AttemtsNumber, l.AttemtsPeriod)
	}

	fmt.Printf("Sectors: %d/%d closed (need: %d)\n",
		l.PassedSectorsCount, l.PassedSectorsCount+l.SectorsLeftToClose, l.RequiredSectorsCount)

	// Task preview
	if l.Task != nil && l.Task.TaskText != "" {
		text := stripHTML(l.Task.TaskText)
		if len(text) > 120 {
			text = text[:120] + "..."
		}
		fmt.Printf("\nTask:    %s\n", text)
	}

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

	if len(l.Helps) > 0 {
		fmt.Println("\nHints:")
		for _, h := range l.Helps {
			if h.HelpText != "" {
				fmt.Printf("  #%d: %s\n", h.Number, stripHTML(h.HelpText))
			} else {
				fmt.Printf("  #%d: [opens in %s]\n", h.Number, formatDuration(h.RemainSeconds))
			}
		}
	}

	if len(l.Bonuses) > 0 {
		fmt.Println("\nBonuses:")
		for _, b := range l.Bonuses {
			status := "[ ]"
			if b.IsAnswered {
				status = "[x]"
			}
			extra := ""
			if b.Expired {
				extra = " (expired)"
			} else if b.SecondsToStart > 0 {
				extra = fmt.Sprintf(" (starts in %s)", formatDuration(b.SecondsToStart))
			} else if b.SecondsLeft > 0 {
				extra = fmt.Sprintf(" (%s left)", formatDuration(b.SecondsLeft))
			}
			fmt.Printf("  %s %s%s\n", status, b.Name, extra)
		}
	}

	if len(l.PenaltyHelps) > 0 {
		fmt.Println("\nPenalty Hints:")
		for _, h := range l.PenaltyHelps {
			switch {
			case h.PenaltyHelpState == 2 && h.HelpText != "":
				fmt.Printf("  #%d: %s (penalty: %ds)\n", h.Number, stripHTML(h.HelpText), h.Penalty)
			case h.PenaltyComment != "":
				fmt.Printf("  #%d: %s (penalty: %ds, pid: %d)\n", h.Number, h.PenaltyComment, h.Penalty, h.PenaltyHelpId)
			default:
				fmt.Printf("  #%d: [locked, opens in %s] (penalty: %ds, pid: %d)\n",
					h.Number, formatDuration(h.RemainSeconds), h.Penalty, h.PenaltyHelpId)
			}
		}
	}

	if len(l.Messages) > 0 {
		fmt.Printf("\nMessages (%d):\n", len(l.Messages))
		for _, m := range l.Messages {
			fmt.Printf("  [%s] %s\n", m.OwnerLogin, stripHTML(m.MessageText))
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
			kind := "L"
			if a.Kind == 2 {
				kind = "B"
			}
			ts := ""
			if parts := strings.SplitN(a.LocDateTime, " ", 2); len(parts) == 2 {
				ts = parts[1]
			}
			fmt.Printf("  [%s/%s] %s %-15s %s\n", mark, kind, ts, a.Login, a.Answer)
		}
	}
}

func cmdTask(ctx context.Context, cfg *config, client *encx.Client) {
	requireGameId(cfg)
	model, err := client.GetGameModel(ctx, cfg.gameId)
	if err != nil {
		fatal("Failed to get game model: %v", err)
	}
	if model.Level == nil {
		fatal("No active level")
	}
	l := model.Level
	fmt.Printf("Level %d: %s\n\n", l.Number, l.Name)
	if l.Task == nil || l.Task.TaskText == "" {
		fmt.Println("No task text")
		return
	}
	fmt.Println(stripHTML(l.Task.TaskText))
}

func cmdMessages(ctx context.Context, cfg *config, client *encx.Client) {
	requireGameId(cfg)
	model, err := client.GetGameModel(ctx, cfg.gameId)
	if err != nil {
		fatal("Failed to get game model: %v", err)
	}
	if model.Level == nil {
		fatal("No active level")
	}
	if len(model.Level.Messages) == 0 {
		fmt.Println("No messages")
		return
	}
	for _, m := range model.Level.Messages {
		fmt.Printf("[%s]: %s\n\n", m.OwnerLogin, stripHTML(m.MessageText))
	}
}

func cmdEnter(ctx context.Context, cfg *config, client *encx.Client) {
	requireGameId(cfg)
	_, err := client.EnterGame(ctx, cfg.gameId)
	if err != nil {
		fatal("Failed to enter game: %v", err)
	}
	fmt.Println("Enter game request sent. Use 'encli status' to check game state.")
}

func cmdSendCode(ctx context.Context, cfg *config, client *encx.Client, args []string) {
	requireGameId(cfg)
	if len(args) == 0 {
		fatal("Usage: encli send-code -game-id <id> <code>")
	}
	code := args[0]

	model, err := client.GetGameModel(ctx, cfg.gameId)
	if err != nil {
		fatal("Failed to get game model: %v", err)
	}
	if model.Level == nil {
		fatal("No active level")
	}

	result, err := client.SendCode(ctx, cfg.gameId, model.Level.LevelId, model.Level.Number, code)
	if err != nil {
		fatal("Failed to send code: %v", err)
	}
	printActionResult(result, "Level")
}

func cmdSendBonus(ctx context.Context, cfg *config, client *encx.Client, args []string) {
	requireGameId(cfg)
	if len(args) == 0 {
		fatal("Usage: encli send-bonus -game-id <id> <code>")
	}
	code := args[0]

	model, err := client.GetGameModel(ctx, cfg.gameId)
	if err != nil {
		fatal("Failed to get game model: %v", err)
	}
	if model.Level == nil {
		fatal("No active level")
	}

	result, err := client.SendBonusCode(ctx, cfg.gameId, model.Level.LevelId, model.Level.Number, code)
	if err != nil {
		fatal("Failed to send bonus code: %v", err)
	}
	printActionResult(result, "Bonus")
}

func cmdHint(ctx context.Context, cfg *config, client *encx.Client, args []string) {
	requireGameId(cfg)
	if len(args) == 0 {
		fatal("Usage: encli hint -game-id <id> <hint-id>")
	}
	pid, err := strconv.Atoi(args[0])
	if err != nil {
		fatal("Invalid hint ID: %s", args[0])
	}

	model, err := client.GetPenaltyHint(ctx, cfg.gameId, pid)
	if err != nil {
		fatal("Failed to request hint: %v", err)
	}
	fmt.Println("Penalty hint requested")
	if model.Level != nil {
		for _, h := range model.Level.PenaltyHelps {
			if h.PenaltyHelpId == pid {
				if h.HelpText != "" {
					fmt.Printf("Hint #%d: %s\n", h.Number, stripHTML(h.HelpText))
				} else if h.PenaltyComment != "" {
					fmt.Printf("Hint #%d: %s\n", h.Number, h.PenaltyComment)
				}
			}
		}
	}
}

// --- Helpers ---

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

func requireGameId(cfg *config) {
	if cfg.gameId == 0 {
		fatal("-game-id is required")
	}
}

func parseEventCode(event any) int {
	switch v := event.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		n, _ := strconv.Atoi(v)
		return n
	default:
		return -1
	}
}

func gameTypeName(id int) string {
	switch id {
	case encx.GameTypeSingle:
		return "single"
	case encx.GameTypeTeam:
		return "team"
	case encx.GameTypePersonal:
		return "personal"
	default:
		return fmt.Sprintf("type-%d", id)
	}
}

func zoneName(id int) string {
	switch id {
	case encx.ZoneQuest:
		return "quest"
	case encx.ZoneBrainstorm:
		return "brainstorm"
	case encx.ZonePhotohunt:
		return "photohunt"
	case encx.ZoneWetWar:
		return "wetwar"
	case encx.ZoneCompetition, encx.ZoneCompetition2:
		return "competition"
	case encx.ZonePhotoextreme:
		return "photoextreme"
	case encx.ZonePoints:
		return "points"
	case encx.ZoneQuiz:
		return "quiz"
	default:
		return fmt.Sprintf("zone-%d", id)
	}
}

// stripHTML removes HTML tags from text for terminal display.
func stripHTML(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	result := b.String()
	result = strings.ReplaceAll(result, "&nbsp;", " ")
	result = strings.ReplaceAll(result, "&amp;", "&")
	result = strings.ReplaceAll(result, "&lt;", "<")
	result = strings.ReplaceAll(result, "&gt;", ">")
	result = strings.ReplaceAll(result, "&quot;", "\"")
	result = strings.ReplaceAll(result, "&#39;", "'")
	return strings.TrimSpace(result)
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

func prompt(label string) string {
	fmt.Fprint(os.Stderr, label)
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	return strings.TrimSpace(scanner.Text())
}

func promptPassword(label string) string {
	fmt.Fprint(os.Stderr, label)
	b, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		fatal("Failed to read password: %v", err)
	}
	return string(b)
}


func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func envBool(key string) bool {
	v := os.Getenv(key)
	return v == "1" || v == "true"
}
