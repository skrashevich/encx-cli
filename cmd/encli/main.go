// Command encli is a CLI tool for interacting with the Encounter (en.cx) game engine.
package main

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json"
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

var (
	version   = "dev"
	jsonMode  bool
	debugMode bool
)

// Config holds parsed CLI configuration.
type config struct {
	domain     string
	login      string
	password   string
	gameId     int
	insecure   bool
	useHTTP    bool
	jsonOutput bool
	debug      bool
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

	// Check for --llm mode: encli [flags] --llm <natural language prompt>
	for i := 1; i < len(os.Args); i++ {
		if os.Args[i] == "--llm" {
			flagArgs, promptParts, err := splitLLMArgs(os.Args[1:])
			if err != nil {
				fatal("%v", err)
			}
			if len(promptParts) == 0 {
				fatal("Usage: encli [flags] --llm <prompt>")
			}

			fs := flag.NewFlagSet("encli", flag.ExitOnError)
			cfg := &config{}
			fs.StringVar(&cfg.domain, "domain", cmp.Or(os.Getenv("ENCX_DOMAIN"), "tech.en.cx"), "Encounter domain")
			fs.StringVar(&cfg.login, "login", os.Getenv("ENCX_LOGIN"), "Login username")
			fs.StringVar(&cfg.password, "password", os.Getenv("ENCX_PASSWORD"), "Login password")
			fs.IntVar(&cfg.gameId, "game-id", envInt("ENCX_GAME_ID", 0), "Game ID")
			fs.BoolVar(&cfg.insecure, "insecure", envBool("ENCX_INSECURE"), "Skip TLS verification")
			fs.BoolVar(&cfg.useHTTP, "http", false, "Use plain HTTP")
			fs.BoolVar(&cfg.jsonOutput, "json", false, "Output as JSON")
			fs.BoolVar(&cfg.debug, "debug", envBool("ENCX_DEBUG"), "Enable debug logging")
			fs.Parse(flagArgs)
			debugMode = cfg.debug
			debugf("starting llm mode: domain=%s game_id=%d insecure=%v http=%v json=%v login_set=%v password_set=%v prompt_len=%d",
				cfg.domain, cfg.gameId, cfg.insecure, cfg.useHTTP, cfg.jsonOutput, cfg.login != "", cfg.password != "", len(strings.Join(promptParts, " ")))

			var opts []encx.Option
			if cfg.insecure {
				opts = append(opts, encx.WithInsecureTLS())
			}
			if cfg.useHTTP {
				opts = append(opts, encx.WithHTTP())
			}
			if cfg.debug {
				opts = append(opts, encx.WithDebugLogger(debugf))
			}

			client := encx.New(cfg.domain, opts...)
			debugf("created encx client for domain=%s", cfg.domain)
			loadSession(cfg, client)
			cmdLLM(context.Background(), cfg, client, strings.Join(promptParts, " "))
			return
		}
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
		// Skip flag value (e.g. -domain tech.en.cx)
		if i+1 < len(os.Args) && !strings.HasPrefix(os.Args[i+1], "-") &&
			strings.HasPrefix(os.Args[i], "-") && !strings.Contains(os.Args[i], "=") &&
			!isBoolFlag(os.Args[i]) {
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

	fs.StringVar(&cfg.domain, "domain", cmp.Or(os.Getenv("ENCX_DOMAIN"), "tech.en.cx"), "Encounter domain (env: ENCX_DOMAIN)")
	fs.StringVar(&cfg.login, "login", os.Getenv("ENCX_LOGIN"), "Login username (env: ENCX_LOGIN)")
	fs.StringVar(&cfg.password, "password", os.Getenv("ENCX_PASSWORD"), "Login password (env: ENCX_PASSWORD)")
	fs.IntVar(&cfg.gameId, "game-id", envInt("ENCX_GAME_ID", 0), "Game ID (env: ENCX_GAME_ID)")
	fs.BoolVar(&cfg.insecure, "insecure", envBool("ENCX_INSECURE"), "Skip TLS verification (env: ENCX_INSECURE)")
	fs.BoolVar(&cfg.useHTTP, "http", false, "Use plain HTTP instead of HTTPS")
	fs.BoolVar(&cfg.jsonOutput, "json", false, "Output results as JSON")
	fs.BoolVar(&cfg.debug, "debug", envBool("ENCX_DEBUG"), "Enable debug logging (env: ENCX_DEBUG)")

	fs.Usage = func() { printCommandHelp(cmd) }
	fs.Parse(args)
	jsonMode = cfg.jsonOutput
	debugMode = cfg.debug
	debugf("parsed command=%s domain=%s game_id=%d insecure=%v http=%v json=%v login_set=%v password_set=%v positional=%d",
		cmd, cfg.domain, cfg.gameId, cfg.insecure, cfg.useHTTP, cfg.jsonOutput, cfg.login != "", cfg.password != "", len(fs.Args()))

	var opts []encx.Option
	if cfg.insecure {
		opts = append(opts, encx.WithInsecureTLS())
	}
	if cfg.useHTTP {
		opts = append(opts, encx.WithHTTP())
	}
	if cfg.debug {
		opts = append(opts, encx.WithDebugLogger(debugf))
	}

	client := encx.New(cfg.domain, opts...)
	debugf("created encx client for domain=%s", cfg.domain)
	ctx := context.Background()
	positional := fs.Args()
	debugf("dispatching command=%s positional=%v", cmd, positional)

	switch cmd {
	case "login":
		cmdLogin(ctx, cfg, client)
	case "logout":
		cmdLogout(cfg)
	case "games":
		loadSession(cfg, client)
		cmdGames(ctx, cfg, client)
	case "game-list":
		loadSession(cfg, client)
		cmdGameList(ctx, cfg, client)
	case "status":
		requireAuth(ctx, cfg, client)
		cmdStatus(ctx, cfg, client)
	case "level":
		requireAuth(ctx, cfg, client)
		cmdLevel(ctx, cfg, client)
	case "messages":
		requireAuth(ctx, cfg, client)
		cmdMessages(ctx, cfg, client)
	case "enter":
		requireAuth(ctx, cfg, client)
		cmdEnter(ctx, cfg, client)
	case "levels":
		requireAuth(ctx, cfg, client)
		cmdLevels(ctx, cfg, client)
	case "bonuses":
		requireAuth(ctx, cfg, client)
		cmdBonuses(ctx, cfg, client)
	case "hints":
		requireAuth(ctx, cfg, client)
		cmdHints(ctx, cfg, client)
	case "sectors":
		requireAuth(ctx, cfg, client)
		cmdSectors(ctx, cfg, client)
	case "log":
		requireAuth(ctx, cfg, client)
		cmdLog(ctx, cfg, client)
	case "send-code":
		requireAuth(ctx, cfg, client)
		cmdSendCode(ctx, cfg, client, positional)
	case "send-bonus":
		requireAuth(ctx, cfg, client)
		cmdSendBonus(ctx, cfg, client, positional)
	case "hint":
		requireAuth(ctx, cfg, client)
		cmdHint(ctx, cfg, client, positional)
	case "game-stats":
		requireAuth(ctx, cfg, client)
		cmdGameStats(ctx, cfg, client)
	case "profile":
		requireAuth(ctx, cfg, client)
		cmdProfile(ctx, cfg, client)

	// Admin commands
	case "admin-games":
		requireAuth(ctx, cfg, client)
		cmdAdminGames(ctx, cfg, client)
	case "admin-levels":
		requireAuth(ctx, cfg, client)
		cmdAdminLevels(ctx, cfg, client)
	case "admin-level-content":
		requireAuth(ctx, cfg, client)
		cmdAdminLevelContent(ctx, cfg, client, positional)
	case "admin-create-levels":
		requireAuth(ctx, cfg, client)
		cmdAdminCreateLevels(ctx, cfg, client, positional)
	case "admin-delete-level":
		requireAuth(ctx, cfg, client)
		cmdAdminDeleteLevel(ctx, cfg, client, positional)
	case "admin-rename-level":
		requireAuth(ctx, cfg, client)
		cmdAdminRenameLevel(ctx, cfg, client, positional)
	case "admin-set-autopass":
		requireAuth(ctx, cfg, client)
		cmdAdminUpdateAutopass(ctx, cfg, client, positional)
	case "admin-set-block":
		requireAuth(ctx, cfg, client)
		cmdAdminUpdateAnswerBlock(ctx, cfg, client, positional)
	case "admin-create-bonus":
		requireAuth(ctx, cfg, client)
		cmdAdminCreateBonus(ctx, cfg, client, positional)
	case "admin-delete-bonus":
		requireAuth(ctx, cfg, client)
		cmdAdminDeleteBonus(ctx, cfg, client, positional)
	case "admin-create-sector":
		requireAuth(ctx, cfg, client)
		cmdAdminCreateSector(ctx, cfg, client, positional)
	case "admin-delete-sector":
		requireAuth(ctx, cfg, client)
		cmdAdminDeleteSector(ctx, cfg, client, positional)
	case "admin-create-hint":
		requireAuth(ctx, cfg, client)
		cmdAdminCreateHint(ctx, cfg, client, positional)
	case "admin-delete-hint":
		requireAuth(ctx, cfg, client)
		cmdAdminDeleteHint(ctx, cfg, client, positional)
	case "admin-create-task":
		requireAuth(ctx, cfg, client)
		cmdAdminCreateTask(ctx, cfg, client, positional)
	case "admin-set-comment":
		requireAuth(ctx, cfg, client)
		cmdAdminSetComment(ctx, cfg, client, positional)
	case "admin-teams":
		requireAuth(ctx, cfg, client)
		cmdAdminTeams(ctx, cfg, client)
	case "admin-corrections":
		requireAuth(ctx, cfg, client)
		cmdAdminCorrections(ctx, cfg, client)
	case "admin-add-correction":
		requireAuth(ctx, cfg, client)
		cmdAdminAddCorrection(ctx, cfg, client, positional)
	case "admin-delete-correction":
		requireAuth(ctx, cfg, client)
		cmdAdminDeleteCorrection(ctx, cfg, client, positional)
	case "admin-wipe-game":
		requireAuth(ctx, cfg, client)
		cmdAdminWipeGame(ctx, cfg, client)
	case "admin-copy-game":
		requireAuth(ctx, cfg, client)
		cmdAdminCopyGame(ctx, cfg, client, positional)

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
  level       Show current level task/assignment
  levels      Show all levels with pass status
  bonuses     Show bonuses for current level
  hints       Show hints (regular and penalty) for current level
  sectors     Show sectors for current level
  log         Show recent code submissions
  messages    Show messages from organizers
  enter       Enter a game (submit application)
  send-code   Send a level code
  send-bonus  Send a bonus code
  hint        Request a penalty hint
  game-stats  Show game statistics (levels, teams, rankings)
  profile     Show your profile

Admin commands (require game editor rights):
  admin-games              List your authored games
  admin-levels             List levels with IDs (admin)
  admin-level-content      Read full level content from admin panel
  admin-create-levels      Create new levels
  admin-delete-level       Delete a level by number
  admin-rename-level       Rename a level
  admin-set-autopass       Set level autopass timer
  admin-set-block          Set level answer block settings
  admin-create-bonus       Create a bonus on a level
  admin-delete-bonus       Delete a bonus by ID
  admin-create-sector      Create a sector on a level
  admin-delete-sector      Delete a sector by ID
  admin-create-hint        Create a hint on a level
  admin-delete-hint        Delete a hint by ID
  admin-create-task        Create a task on a level
  admin-set-comment        Set level name and comment
  admin-teams              List teams in the game
  admin-corrections        List bonus/penalty time corrections
  admin-add-correction     Add a time correction
  admin-delete-correction  Delete a time correction
  admin-wipe-game          Completely reset a game (delete all content)
  admin-copy-game          Copy entire game to another game

LLM mode:
  --llm <prompt>  Natural language command (uses OpenRouter API)
                  Example: encli --llm "скопируй игру 82033 в 82034"

Global flags:
  -domain      Encounter domain (default: tech.en.cx)
  -login       Login username
  -password    Login password
  -game-id     Game ID
  -insecure    Skip TLS certificate verification
  -http        Use plain HTTP instead of HTTPS
  -debug       Enable debug logging

Environment variables:
  ENCX_DOMAIN          Domain (default: tech.en.cx)
  ENCX_LOGIN           Login username
  ENCX_PASSWORD        Login password
  ENCX_GAME_ID         Game ID
  ENCX_INSECURE        Skip TLS verification (1/true)
  ENCX_DEBUG           Enable debug logging (1/true)
  OPENROUTER_API_KEY   API key for --llm mode (required)
  OPENROUTER_MODEL     LLM model override (default: openai/gpt-oss-120b:free)

Examples:
  encli login -login svk -password secret -insecure
  encli games
  encli game-list
  encli status -game-id 27053
  encli level -game-id 27053
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
	case "level":
		fmt.Fprintln(os.Stderr, "Usage: encli level -game-id <id>")
		fmt.Fprintln(os.Stderr, "  Show current level task/assignment text.")
	case "messages":
		fmt.Fprintln(os.Stderr, "Usage: encli messages -game-id <id>")
		fmt.Fprintln(os.Stderr, "  Show messages from game organizers.")
	case "levels":
		fmt.Fprintln(os.Stderr, "Usage: encli levels -game-id <id>")
		fmt.Fprintln(os.Stderr, "  Show all levels with their pass/dismiss status.")
	case "bonuses":
		fmt.Fprintln(os.Stderr, "Usage: encli bonuses -game-id <id>")
		fmt.Fprintln(os.Stderr, "  Show bonuses for the current level.")
	case "hints":
		fmt.Fprintln(os.Stderr, "Usage: encli hints -game-id <id>")
		fmt.Fprintln(os.Stderr, "  Show hints (regular and penalty) for the current level.")
	case "sectors":
		fmt.Fprintln(os.Stderr, "Usage: encli sectors -game-id <id>")
		fmt.Fprintln(os.Stderr, "  Show sectors for the current level.")
	case "log":
		fmt.Fprintln(os.Stderr, "Usage: encli log -game-id <id>")
		fmt.Fprintln(os.Stderr, "  Show recent code submissions (action log).")
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
	case "game-stats":
		fmt.Fprintln(os.Stderr, "Usage: encli game-stats -game-id <id>")
		fmt.Fprintln(os.Stderr, "  Show game statistics: levels, teams, rankings.")
	case "profile":
		fmt.Fprintln(os.Stderr, "Usage: encli profile")
		fmt.Fprintln(os.Stderr, "  Show your profile (login, name, rank, team, points).")
	case "admin-games":
		fmt.Fprintln(os.Stderr, "Usage: encli admin-games")
		fmt.Fprintln(os.Stderr, "  List games where you are an author or have admin access.")
	case "admin-levels":
		fmt.Fprintln(os.Stderr, "Usage: encli admin-levels -game-id <id>")
		fmt.Fprintln(os.Stderr, "  List all levels with their IDs (admin panel).")
	case "admin-level-content":
		fmt.Fprintln(os.Stderr, "Usage: encli admin-level-content -game-id <id> <level-number>")
		fmt.Fprintln(os.Stderr, "  Read admin-side level content: task text, sector answers, bonuses, hints, comments, settings.")
	case "admin-create-levels":
		fmt.Fprintln(os.Stderr, "Usage: encli admin-create-levels -game-id <id> <count>")
		fmt.Fprintln(os.Stderr, "  Create the specified number of new levels.")
	case "admin-delete-level":
		fmt.Fprintln(os.Stderr, "Usage: encli admin-delete-level -game-id <id> <level-number>")
		fmt.Fprintln(os.Stderr, "  Delete a level by its number (from the end).")
	case "admin-rename-level":
		fmt.Fprintln(os.Stderr, "Usage: encli admin-rename-level -game-id <id> <level-id> <new-name>")
		fmt.Fprintln(os.Stderr, "  Rename a level (use level ID from admin-levels).")
	case "admin-set-autopass":
		fmt.Fprintln(os.Stderr, "Usage: encli admin-set-autopass -game-id <id> <level-number> <HH:MM:SS> [penalty HH:MM:SS]")
		fmt.Fprintln(os.Stderr, "  Set autopass timer. Optional penalty time if timeout penalty enabled.")
	case "admin-set-block":
		fmt.Fprintln(os.Stderr, "Usage: encli admin-set-block -game-id <id> <level-number> <attempts> <period HH:MM:SS> [player]")
		fmt.Fprintln(os.Stderr, "  Set answer block: max attempts per period. Add 'player' to apply per player.")
	case "admin-create-bonus":
		fmt.Fprintln(os.Stderr, "Usage: encli admin-create-bonus -game-id <id> <level-num> <level-id> <name> <answer1> [answer2 ...]")
		fmt.Fprintln(os.Stderr, "  Create a bonus with one or more answers.")
	case "admin-delete-bonus":
		fmt.Fprintln(os.Stderr, "Usage: encli admin-delete-bonus -game-id <id> <level-number> <bonus-id>")
		fmt.Fprintln(os.Stderr, "  Delete a bonus by its ID.")
	case "admin-create-sector":
		fmt.Fprintln(os.Stderr, "Usage: encli admin-create-sector -game-id <id> <level-number> <name> <answer1> [answer2 ...]")
		fmt.Fprintln(os.Stderr, "  Create a sector with one or more answers.")
	case "admin-delete-sector":
		fmt.Fprintln(os.Stderr, "Usage: encli admin-delete-sector -game-id <id> <level-number> <sector-id>")
		fmt.Fprintln(os.Stderr, "  Delete a sector by its ID.")
	case "admin-create-hint":
		fmt.Fprintln(os.Stderr, "Usage: encli admin-create-hint -game-id <id> <level-number> <delay HH:MM:SS> <text>")
		fmt.Fprintln(os.Stderr, "  Create a hint with the specified delay before it opens.")
	case "admin-delete-hint":
		fmt.Fprintln(os.Stderr, "Usage: encli admin-delete-hint -game-id <id> <level-number> <hint-id>")
		fmt.Fprintln(os.Stderr, "  Delete a hint by its ID.")
	case "admin-create-task":
		fmt.Fprintln(os.Stderr, "Usage: encli admin-create-task -game-id <id> <level-number> <text>")
		fmt.Fprintln(os.Stderr, "  Create a task (assignment) on the specified level.")
	case "admin-set-comment":
		fmt.Fprintln(os.Stderr, "Usage: encli admin-set-comment -game-id <id> <level-number> <name> [comment]")
		fmt.Fprintln(os.Stderr, "  Set level name and optional comment (visible to organizers).")
	case "admin-teams":
		fmt.Fprintln(os.Stderr, "Usage: encli admin-teams -game-id <id>")
		fmt.Fprintln(os.Stderr, "  List teams registered in the game (admin panel).")
	case "admin-corrections":
		fmt.Fprintln(os.Stderr, "Usage: encli admin-corrections -game-id <id>")
		fmt.Fprintln(os.Stderr, "  List bonus/penalty time corrections for the game.")
	case "admin-add-correction":
		fmt.Fprintln(os.Stderr, "Usage: encli admin-add-correction -game-id <id> <team> <bonus|penalty> <HH:MM:SS> [level] [comment]")
		fmt.Fprintln(os.Stderr, "  Add a time correction. Level '0' applies to all levels.")
	case "admin-delete-correction":
		fmt.Fprintln(os.Stderr, "Usage: encli admin-delete-correction -game-id <id> <correction-id>")
		fmt.Fprintln(os.Stderr, "  Delete a time correction by its ID.")
	case "admin-wipe-game":
		fmt.Fprintln(os.Stderr, "Usage: encli admin-wipe-game -game-id <id>")
		fmt.Fprintln(os.Stderr, "  Completely reset a game: delete all bonuses, levels, and corrections.")
		fmt.Fprintln(os.Stderr, "  After wipe the game is an empty shell. Use before admin-copy-game for clean copy.")
	case "admin-copy-game":
		fmt.Fprintln(os.Stderr, "Usage: encli admin-copy-game -game-id <source-id> <target-id>")
		fmt.Fprintln(os.Stderr, "  Copy entire game (levels, settings, bonuses, sectors, hints) to target game.")
		fmt.Fprintln(os.Stderr, "  Target game levels are created automatically if needed.")
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
		debugf("save session skipped: export cookies failed: %v", err)
		return
	}
	path := sessionFile(cfg)
	debugf("saving session to %s (%d bytes)", path, len(data))
	os.MkdirAll(sessionDir(), 0700)
	os.WriteFile(path, data, 0600)
}

func loadSession(cfg *config, client *encx.Client) bool {
	path := sessionFile(cfg)
	data, err := os.ReadFile(path)
	if err != nil {
		debugf("load session miss: %s: %v", path, err)
		return false
	}
	if err := client.ImportCookies(data); err != nil {
		debugf("load session failed: %s: %v", path, err)
		return false
	}
	debugf("loaded session from %s (%d bytes)", path, len(data))
	return true
}

func requireAuth(ctx context.Context, cfg *config, client *encx.Client) {
	debugf("require auth: login_set=%v password_set=%v", cfg.login != "", cfg.password != "")
	if loadSession(cfg, client) && cfg.login == "" {
		debugf("require auth: using saved session")
		return
	}
	if cfg.login == "" || cfg.password == "" {
		fatal("No saved session. Run 'encli login' first, or pass -login and -password")
	}
	debugf("require auth: logging in with explicit credentials for %s", cfg.login)
	resp, err := client.Login(ctx, cfg.login, cfg.password)
	if err != nil {
		fatal("Login failed: %v", err)
	}
	if resp.Error != 0 {
		fatal("Login error %d: %s", resp.Error, encx.LoginErrorText(resp.Error))
	}
	debugf("require auth: login successful for %s", cfg.login)
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
	debugf("cmd login: attempting login for %s", cfg.login)
	resp, err := client.Login(ctx, cfg.login, cfg.password)
	if err != nil {
		fatal("Login failed: %v", err)
	}
	if resp.Error != 0 {
		fatal("Login error %d: %s", resp.Error, encx.LoginErrorText(resp.Error))
	}
	debugf("cmd login: login successful for %s", cfg.login)
	saveSession(cfg, client)
	if cfg.jsonOutput {
		outputJSON(map[string]any{"success": true, "session_file": sessionFile(cfg)})
		return
	}
	fmt.Printf("Login successful (session saved to %s)\n", sessionFile(cfg))
}

func cmdLogout(cfg *config) {
	path := sessionFile(cfg)
	debugf("cmd logout: removing session file %s", path)
	os.Remove(path)
	if cfg.jsonOutput {
		outputJSON(map[string]any{"success": true})
		return
	}
	fmt.Println("Session cleared")
}

func cmdGames(ctx context.Context, cfg *config, client *encx.Client) {
	games, err := client.GetDomainGames(ctx)
	if err != nil {
		fatal("Failed to get games: %v", err)
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
		fmt.Fprintf(w, "%d\t%s\n", g.GameId, g.Title)
	}
	w.Flush()
}

func cmdGameList(ctx context.Context, cfg *config, client *encx.Client) {
	list, err := client.GetGameList(ctx)
	if err != nil {
		fatal("Failed to get game list: %v", err)
	}
	if cfg.jsonOutput {
		outputJSON(list)
		return
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

	if cfg.jsonOutput {
		outputJSON(model)
		return
	}

	eventCode := parseEventCode(model.Event)

	fmt.Printf("Game:    %s (ID: %d)\n", model.GameTitle, model.GameId)
	if model.TeamName != "" {
		captain := ""
		if model.IsCaptain {
			captain = " [captain]"
		}
		fmt.Printf("Team:    %s (ID: %d)%s\n", model.TeamName, model.TeamId, captain)
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

	if l.PassedBonusesCount > 0 {
		fmt.Printf("Bonuses passed: %d\n", l.PassedBonusesCount)
	}

	// Task preview (from Tasks array or legacy Task field)
	taskText := ""
	if len(l.Tasks) > 0 {
		taskText = l.Tasks[0].TaskText
	} else if l.Task != nil {
		taskText = l.Task.TaskText
	}
	if taskText != "" {
		text := stripHTML(taskText)
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
			if h.HelpText != nil && *h.HelpText != "" {
				fmt.Printf("  #%d: %s\n", h.Number, stripHTML(*h.HelpText))
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
			case h.PenaltyHelpState >= 1 && h.HelpText != nil && *h.HelpText != "":
				fmt.Printf("  #%d: %s (penalty: %ds)\n", h.Number, stripHTML(*h.HelpText), h.Penalty)
			case h.PenaltyComment != nil && *h.PenaltyComment != "":
				fmt.Printf("  #%d: %s (penalty: %ds, pid: %d)\n", h.Number, *h.PenaltyComment, h.Penalty, h.HelpId)
			default:
				fmt.Printf("  #%d: [locked, opens in %s] (penalty: %ds, pid: %d)\n",
					h.Number, formatDuration(h.RemainSeconds), h.Penalty, h.HelpId)
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

func cmdLevel(ctx context.Context, cfg *config, client *encx.Client) {
	requireGameId(cfg)
	model, err := client.GetGameModel(ctx, cfg.gameId)
	if err != nil {
		fatal("Failed to get game model: %v", err)
	}
	if model.Level == nil {
		fatal("No active level")
	}
	l := model.Level
	if cfg.jsonOutput {
		outputJSON(map[string]any{"level": l.Number, "name": l.Name, "tasks": l.Tasks, "task": l.Task})
		return
	}
	fmt.Printf("Level %d: %s\n\n", l.Number, l.Name)
	taskText := ""
	if len(l.Tasks) > 0 {
		taskText = l.Tasks[0].TaskText
	} else if l.Task != nil {
		taskText = l.Task.TaskText
	}
	if taskText == "" {
		fmt.Println("No task text")
		return
	}
	fmt.Println(stripHTML(taskText))
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
	if cfg.jsonOutput {
		outputJSON(model.Level.Messages)
		return
	}
	if len(model.Level.Messages) == 0 {
		fmt.Println("No messages")
		return
	}
	for _, m := range model.Level.Messages {
		fmt.Printf("[%s]: %s\n\n", m.OwnerLogin, stripHTML(m.MessageText))
	}
}

func cmdLevels(ctx context.Context, cfg *config, client *encx.Client) {
	requireGameId(cfg)
	model, err := client.GetGameModel(ctx, cfg.gameId)
	if err != nil {
		fatal("Failed to get game model: %v", err)
	}
	if cfg.jsonOutput {
		outputJSON(model.Levels)
		return
	}
	if len(model.Levels) == 0 {
		fmt.Println("No levels")
		return
	}
	fmt.Printf("Game: %s (%d levels)\n\n", model.GameTitle, len(model.Levels))
	for _, l := range model.Levels {
		status := "[ ]"
		if l.IsPassed {
			status = "[v]"
		} else if l.Dismissed {
			status = "[x]"
		}
		current := ""
		if model.Level != nil && model.Level.LevelId == l.LevelId {
			current = " <-- current"
		}
		fmt.Printf("  %s %d. %s%s\n", status, l.LevelNumber, l.LevelName, current)
	}
}

func cmdBonuses(ctx context.Context, cfg *config, client *encx.Client) {
	requireGameId(cfg)
	model, err := client.GetGameModel(ctx, cfg.gameId)
	if err != nil {
		fatal("Failed to get game model: %v", err)
	}
	if model.Level == nil {
		fatal("No active level")
	}
	l := model.Level
	if cfg.jsonOutput {
		outputJSON(l.Bonuses)
		return
	}
	if len(l.Bonuses) == 0 {
		fmt.Println("No bonuses on this level")
		return
	}
	fmt.Printf("Level %d: %s — Bonuses (%d)\n\n", l.Number, l.Name, len(l.Bonuses))
	for _, b := range l.Bonuses {
		status := "[ ]"
		if b.IsAnswered {
			status = "[v]"
		}
		fmt.Printf("  %s #%d %s\n", status, b.Number, b.Name)
		if b.Task != "" {
			fmt.Printf("       Task: %s\n", stripHTML(b.Task))
		}
		if b.IsAnswered && b.Answer != "" {
			fmt.Printf("       Answer: %s\n", b.Answer)
		}
		if b.Expired {
			fmt.Printf("       (expired)\n")
		} else if b.SecondsToStart > 0 {
			fmt.Printf("       Starts in: %s\n", formatDuration(b.SecondsToStart))
		} else if b.SecondsLeft > 0 {
			fmt.Printf("       Time left: %s\n", formatDuration(b.SecondsLeft))
		}
		if b.AwardTime > 0 {
			fmt.Printf("       Award: -%s\n", formatDuration(b.AwardTime))
		}
	}
}

func cmdHints(ctx context.Context, cfg *config, client *encx.Client) {
	requireGameId(cfg)
	model, err := client.GetGameModel(ctx, cfg.gameId)
	if err != nil {
		fatal("Failed to get game model: %v", err)
	}
	if model.Level == nil {
		fatal("No active level")
	}
	l := model.Level
	if cfg.jsonOutput {
		outputJSON(map[string]any{"helps": l.Helps, "penalty_helps": l.PenaltyHelps})
		return
	}
	if len(l.Helps) == 0 && len(l.PenaltyHelps) == 0 {
		fmt.Println("No hints on this level")
		return
	}
	fmt.Printf("Level %d: %s\n", l.Number, l.Name)

	if len(l.Helps) > 0 {
		fmt.Printf("\nHints (%d):\n", len(l.Helps))
		for _, h := range l.Helps {
			if h.HelpText != nil && *h.HelpText != "" {
				fmt.Printf("  #%d: %s\n", h.Number, stripHTML(*h.HelpText))
			} else if h.RemainSeconds > 0 {
				fmt.Printf("  #%d: [opens in %s]\n", h.Number, formatDuration(h.RemainSeconds))
			} else {
				fmt.Printf("  #%d: [available]\n", h.Number)
			}
		}
	}

	if len(l.PenaltyHelps) > 0 {
		fmt.Printf("\nPenalty Hints (%d):\n", len(l.PenaltyHelps))
		for _, h := range l.PenaltyHelps {
			state := "locked"
			switch h.PenaltyHelpState {
			case 1:
				state = "opened"
			case 2:
				state = "confirmed"
			}
			if h.HelpText != nil && *h.HelpText != "" {
				fmt.Printf("  #%d [%s]: %s (penalty: %s)\n", h.Number, state, stripHTML(*h.HelpText), formatDuration(h.Penalty))
			} else {
				desc := ""
				if h.PenaltyComment != nil && *h.PenaltyComment != "" {
					desc = *h.PenaltyComment
				}
				if h.RemainSeconds > 0 {
					fmt.Printf("  #%d [%s]: %s (penalty: %s, opens in %s, pid: %d)\n",
						h.Number, state, desc, formatDuration(h.Penalty), formatDuration(h.RemainSeconds), h.HelpId)
				} else {
					fmt.Printf("  #%d [%s]: %s (penalty: %s, pid: %d)\n",
						h.Number, state, desc, formatDuration(h.Penalty), h.HelpId)
				}
			}
		}
	}
}

func cmdSectors(ctx context.Context, cfg *config, client *encx.Client) {
	requireGameId(cfg)
	model, err := client.GetGameModel(ctx, cfg.gameId)
	if err != nil {
		fatal("Failed to get game model: %v", err)
	}
	if model.Level == nil {
		fatal("No active level")
	}
	l := model.Level
	if cfg.jsonOutput {
		outputJSON(l.Sectors)
		return
	}
	if len(l.Sectors) == 0 {
		fmt.Println("No sectors on this level")
		return
	}
	fmt.Printf("Level %d: %s — Sectors (%d/%d, need %d)\n\n",
		l.Number, l.Name, l.PassedSectorsCount, l.PassedSectorsCount+l.SectorsLeftToClose, l.RequiredSectorsCount)
	for _, s := range l.Sectors {
		status := "[ ]"
		if s.IsAnswered {
			status = "[v]"
		}
		fmt.Printf("  %s %s", status, s.Name)
		if s.IsAnswered && s.Answer != "" {
			fmt.Printf(" — %s", s.Answer)
		}
		fmt.Println()
	}
}

func cmdLog(ctx context.Context, cfg *config, client *encx.Client) {
	requireGameId(cfg)
	model, err := client.GetGameModel(ctx, cfg.gameId)
	if err != nil {
		fatal("Failed to get game model: %v", err)
	}
	if model.Level == nil {
		fatal("No active level")
	}
	l := model.Level
	if cfg.jsonOutput {
		outputJSON(l.MixedActions)
		return
	}
	if len(l.MixedActions) == 0 {
		fmt.Println("No code submissions yet")
		return
	}
	fmt.Printf("Level %d: %s — Code log (%d entries)\n\n", l.Number, l.Name, len(l.MixedActions))
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "Time\tPlayer\tCode\tType\tResult")
	fmt.Fprintln(w, "----\t------\t----\t----\t------")
	for _, a := range l.MixedActions {
		ts := a.LocDateTime
		kind := "level"
		if a.Kind == 2 {
			kind = "bonus"
		}
		result := "wrong"
		if a.IsCorrect {
			result = "CORRECT"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", ts, a.Login, a.Answer, kind, result)
	}
	w.Flush()
}

func cmdEnter(ctx context.Context, cfg *config, client *encx.Client) {
	requireGameId(cfg)
	body, err := client.EnterGame(ctx, cfg.gameId)
	if err != nil {
		fatal("Failed to enter game: %v", err)
	}
	if cfg.jsonOutput {
		outputJSON(map[string]any{"success": true, "body": body})
		return
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
	if cfg.jsonOutput {
		outputJSON(result.EngineAction)
		return
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
	if cfg.jsonOutput {
		outputJSON(result.EngineAction)
		return
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
	if cfg.jsonOutput {
		outputJSON(model)
		return
	}
	fmt.Println("Penalty hint requested")
	if model.Level != nil {
		for _, h := range model.Level.PenaltyHelps {
			if h.HelpId == pid {
				if h.HelpText != nil && *h.HelpText != "" {
					fmt.Printf("Hint #%d: %s\n", h.Number, stripHTML(*h.HelpText))
				} else if h.PenaltyComment != nil && *h.PenaltyComment != "" {
					fmt.Printf("Hint #%d: %s\n", h.Number, *h.PenaltyComment)
				}
			}
		}
	}
}

func cmdGameStats(ctx context.Context, cfg *config, client *encx.Client) {
	requireGameId(cfg)
	stats, err := client.GetGameStatistics(ctx, cfg.gameId)
	if err != nil {
		fatal("Failed to get game statistics: %v", err)
	}

	if cfg.jsonOutput {
		outputJSON(stats)
		return
	}

	if stats.Game == nil {
		fatal("No game data in statistics response")
	}

	g := stats.Game
	fmt.Printf("Game:     %s (ID: %d)\n", g.Title, g.GameID)
	fmt.Printf("Type:     %s / %s\n", gameTypeName(g.GameTypeID), zoneName(g.ZoneId))
	fmt.Printf("Levels:   %d\n", len(stats.Levels))
	fmt.Printf("Status:   started=%v finished=%v inProgress=%v\n", g.Started, g.Finished, g.InProgress)

	if len(stats.Levels) > 0 {
		fmt.Println("\nLevels:")
		w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "#\tName\tDismissed\tPlayers")
		fmt.Fprintln(w, "-\t----\t---------\t-------")
		for _, l := range stats.Levels {
			name := l.LevelName
			if name == "" {
				name = "-"
			}
			players := 0
			for _, lp := range stats.LevelPlayers {
				if lp.LevelNum == l.LevelNumber {
					players = lp.Count
					break
				}
			}
			fmt.Fprintf(w, "%d\t%s\t%v\t%d\n", l.LevelNumber, name, l.Dismissed, players)
		}
		w.Flush()
	}

	if len(stats.StatItems) > 0 {
		fmt.Println("\nResults:")
		w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "Level\tTeam\tPlayer\tTime")
		fmt.Fprintln(w, "-----\t----\t------\t----")
		for _, levelItems := range stats.StatItems {
			for _, item := range levelItems {
				fmt.Fprintf(w, "%d\t%s\t%s\t%s\n",
					item.LevelNum, item.TeamName, item.UserName,
					formatDuration(item.SpentSeconds))
			}
		}
		w.Flush()
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

func outputJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}

func fatal(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if agentMode {
		panic(agentFatalError{Message: msg})
	}
	if jsonMode {
		outputJSON(map[string]string{"error": msg})
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}

func debugf(format string, args ...any) {
	if !debugMode {
		return
	}
	fmt.Fprintf(os.Stderr, "[debug %s] "+format+"\n", append([]any{time.Now().Format("15:04:05.000")}, args...)...)
}

func isBoolFlag(arg string) bool {
	switch arg {
	case "-insecure", "-http", "-json", "-debug":
		return true
	default:
		return false
	}
}

func isValueFlag(arg string) bool {
	switch arg {
	case "-domain", "-login", "-password", "-game-id":
		return true
	default:
		return false
	}
}

func splitLLMArgs(args []string) ([]string, []string, error) {
	var (
		flagArgs   []string
		promptArgs []string
		seenLLM    bool
	)

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--llm" {
			seenLLM = true
			continue
		}

		switch {
		case isBoolFlag(arg):
			flagArgs = append(flagArgs, arg)
		case isValueFlag(arg):
			if i+1 >= len(args) {
				return nil, nil, fmt.Errorf("flag %s requires a value", arg)
			}
			flagArgs = append(flagArgs, arg, args[i+1])
			i++
		case strings.HasPrefix(arg, "-") && strings.Contains(arg, "="):
			name, _, _ := strings.Cut(arg, "=")
			if isBoolFlag(name) || isValueFlag(name) {
				flagArgs = append(flagArgs, arg)
				continue
			}
			promptArgs = append(promptArgs, arg)
		default:
			promptArgs = append(promptArgs, arg)
		}
	}

	if !seenLLM {
		return nil, nil, fmt.Errorf("missing --llm")
	}

	return flagArgs, promptArgs, nil
}

func summarizeDebugText(s string, limit int) string {
	s = strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
	if limit > 0 && len(s) > limit {
		return s[:limit] + "..."
	}
	return s
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
