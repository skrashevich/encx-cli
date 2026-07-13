//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

var (
	cliBin      string // path to the built encli binary
	cliHome     string // isolated HOME with a saved session
	cliLoginErr error  // result of the one-time `encli login` in TestMain
)

func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "encli-e2e-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "e2e: mkdtemp:", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmp)

	cliBin = filepath.Join(tmp, "encli")
	build := exec.Command("go", "build", "-o", cliBin, "./cmd/encli")
	build.Dir = ".."
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "e2e: build encli: %v\n%s", err, out)
		os.Exit(1)
	}

	cliHome = filepath.Join(tmp, "home")
	if err := os.MkdirAll(cliHome, 0o700); err != nil {
		fmt.Fprintln(os.Stderr, "e2e: mkdir home:", err)
		os.Exit(1)
	}

	// The game engine only serves the account's most recent session, so the
	// whole run shares a single one: the library logs in once, and its
	// cookies are planted into the CLI session file. Session-churning tests
	// (fresh logins, logout) live in zz_session_test.go and run last.
	cliLoginErr = initAdminClient()
	if cliLoginErr == nil {
		if data, err := adminShared.ExportCookies(); err != nil {
			cliLoginErr = fmt.Errorf("export shared session cookies: %w", err)
		} else {
			dir := filepath.Join(cliHome, ".config", "encli")
			if err := os.MkdirAll(dir, 0o700); err != nil {
				cliLoginErr = err
			} else if err := os.WriteFile(filepath.Join(dir, e2eDomain()+".json"), data, 0o600); err != nil {
				cliLoginErr = err
			}
		}
	}

	os.Exit(m.Run())
}

// runCLIIn executes the encli binary with HOME pointed at the given directory
// and the e2e domain preset. Returns stdout, stderr, and the exit code.
func runCLIIn(home string, args ...string) (string, string, int) {
	full := append([]string{"-domain", e2eDomain()}, args...)
	cmd := exec.Command(cliBin, full...)
	cmd.Env = append(os.Environ(), "HOME="+home)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		code = -1
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		}
	}
	return stdout.String(), stderr.String(), code
}

// runCLI executes encli with the shared authenticated session.
func runCLI(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	if cliLoginErr != nil {
		t.Skipf("Shared CLI login unavailable: %v", cliLoginErr)
	}
	return runCLIIn(cliHome, args...)
}

// mustRunCLI runs encli and fails the test on a non-zero exit code.
func mustRunCLI(t *testing.T, args ...string) string {
	t.Helper()
	stdout, stderr, code := runCLI(t, args...)
	if code != 0 {
		t.Fatalf("encli %s exited with %d\nstdout: %s\nstderr: %s",
			strings.Join(args, " "), code, stdout, stderr)
	}
	return stdout
}

func gameIDArg() string { return strconv.Itoa(e2eGameID()) }

// --- Basic commands ---

func TestCLIVersion(t *testing.T) {
	// `version` is only recognized as the very first argument, so bypass the
	// runCLIIn helper that prepends -domain.
	out, err := exec.Command(cliBin, "version").CombinedOutput()
	if err != nil {
		t.Fatalf("version failed: %v\n%s", err, out)
	}
	if !strings.HasPrefix(string(out), "encli") {
		t.Errorf("Expected version output to start with 'encli', got %q", out)
	}
}

func TestCLIUnknownCommand(t *testing.T) {
	_, stderr, code := runCLIIn(cliHome, "no-such-command")
	if code == 0 {
		t.Fatal("Expected non-zero exit code for unknown command")
	}
	if !strings.Contains(stderr, "Unknown command") {
		t.Errorf("Expected 'Unknown command' in stderr, got %q", stderr)
	}
}

func TestCLIProfile(t *testing.T) {
	stdout := mustRunCLI(t, "-json", "profile")
	var profile struct {
		ID    int    `json:"id"`
		Login string `json:"login"`
	}
	if err := json.Unmarshal([]byte(stdout), &profile); err != nil {
		t.Fatalf("profile -json produced invalid JSON: %v\n%s", err, stdout)
	}
	if !strings.EqualFold(profile.Login, e2eLogin()) {
		t.Errorf("Expected login %q, got %q", e2eLogin(), profile.Login)
	}
	if profile.ID <= 0 {
		t.Errorf("Expected positive user id, got %d", profile.ID)
	}
}

func TestCLIGameList(t *testing.T) {
	stdout := mustRunCLI(t, "-json", "game-list")
	var list struct {
		ActiveGames []json.RawMessage `json:"ActiveGames"`
		ComingGames []json.RawMessage `json:"ComingGames"`
	}
	if err := json.Unmarshal([]byte(stdout), &list); err != nil {
		t.Fatalf("game-list -json produced invalid JSON: %v\n%s", err, stdout)
	}
	t.Logf("game-list: %d active, %d coming", len(list.ActiveGames), len(list.ComingGames))
}

func TestCLIStatusJSON(t *testing.T) {
	stdout := mustRunCLI(t, "-json", "-game-id", gameIDArg(), "status")
	var model struct {
		GameId    int    `json:"GameId"`
		GameTitle string `json:"GameTitle"`
	}
	if err := json.Unmarshal([]byte(stdout), &model); err != nil {
		t.Fatalf("status -json produced invalid JSON: %v\n%s", err, stdout)
	}
	if model.GameId != e2eGameID() {
		t.Errorf("Expected GameId %d, got %d", e2eGameID(), model.GameId)
	}
	t.Logf("status: game %q", model.GameTitle)
}

// TestCLIPlayerReadCommands smoke-tests every read-only player command
// against the active sandbox game.
func TestCLIPlayerReadCommands(t *testing.T) {
	commands := []string{"levels", "level", "sectors", "bonuses", "hints", "log", "messages", "game-stats", "games"}
	for _, cmd := range commands {
		stdout, stderr, code := runCLI(t, "-game-id", gameIDArg(), cmd)
		if code != 0 {
			t.Errorf("%s exited with %d\nstdout: %s\nstderr: %s", cmd, code, stdout, stderr)
			continue
		}
		if strings.TrimSpace(stdout) == "" {
			t.Errorf("Expected %s to produce output", cmd)
		}
	}
}

func TestCLISendWrongCode(t *testing.T) {
	code := uniqueName("wrong-code")
	stdout, stderr, exit := runCLI(t, "-game-id", gameIDArg(), "send-code", code)
	if exit != 0 {
		// The current level may legitimately reject level answers (bonus-only
		// level, finished game, ...) — that is a game-state condition, not a
		// CLI defect.
		t.Skipf("send-code not possible in current game state: %s", strings.TrimSpace(stderr))
	}
	if strings.TrimSpace(stdout) == "" {
		t.Error("Expected send-code to print a result")
	}
	t.Logf("send-code output: %s", strings.TrimSpace(stdout))
}

// --- Admin commands ---

type cliAdminGame struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
}

type cliAdminLevel struct {
	Number int    `json:"number"`
	Name   string `json:"name"`
	ID     int    `json:"id"`
}

func cliAdminLevels(t *testing.T) []cliAdminLevel {
	t.Helper()
	stdout := mustRunCLI(t, "-json", "-game-id", gameIDArg(), "admin-levels")
	var levels []cliAdminLevel
	if err := json.Unmarshal([]byte(stdout), &levels); err != nil {
		t.Fatalf("admin-levels -json produced invalid JSON: %v\n%s", err, stdout)
	}
	return levels
}

func TestCLIAdminGames(t *testing.T) {
	stdout := mustRunCLI(t, "-json", "admin-games")
	var games []cliAdminGame
	if err := json.Unmarshal([]byte(stdout), &games); err != nil {
		t.Fatalf("admin-games -json produced invalid JSON: %v\n%s", err, stdout)
	}
	found := false
	for _, g := range games {
		if g.ID == e2eGameID() {
			found = true
		}
	}
	if !found {
		t.Errorf("Expected sandbox game %d in admin-games output, got %+v", e2eGameID(), games)
	}
}

func TestCLIAdminLevels(t *testing.T) {
	levels := cliAdminLevels(t)
	if len(levels) == 0 {
		t.Fatal("Expected at least one level in the sandbox game")
	}
	for _, l := range levels {
		if l.ID <= 0 || l.Number <= 0 {
			t.Errorf("Level with invalid id/number: %+v", l)
		}
	}
}

// TestCLIAdminLevelLifecycle drives the admin workflow end to end through the
// CLI: create a level, name it, add a sector, read the level content back,
// and delete the level again.
func TestCLIAdminLevelLifecycle(t *testing.T) {
	baseline := cliAdminLevels(t)

	mustRunCLI(t, "-game-id", gameIDArg(), "admin-create-levels", "1")
	levels := cliAdminLevels(t)
	if len(levels) != len(baseline)+1 {
		t.Fatalf("Expected %d levels after create, got %d", len(baseline)+1, len(levels))
	}
	level := levels[len(levels)-1]
	levelNum := strconv.Itoa(level.Number)
	t.Logf("Created level %d (id=%d)", level.Number, level.ID)

	defer func() {
		stdout, stderr, code := runCLI(t, "-game-id", gameIDArg(), "admin-delete-level", levelNum)
		if code != 0 {
			t.Errorf("Cleanup: admin-delete-level exited with %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
			return
		}
		after := cliAdminLevels(t)
		if len(after) != len(baseline) {
			t.Errorf("Cleanup: expected %d levels after delete, got %d", len(baseline), len(after))
		}
	}()

	levelName := uniqueName("cli-level")
	mustRunCLI(t, "-game-id", gameIDArg(), "admin-set-comment", levelNum, levelName, "e2e cli comment")

	sectorName := uniqueName("cli-sector")
	mustRunCLI(t, "-game-id", gameIDArg(), "admin-create-sector", levelNum, sectorName, "e2eclianswer")

	content := mustRunCLI(t, "-json", "-game-id", gameIDArg(), "admin-level-content", levelNum)
	if !strings.Contains(content, levelName) {
		t.Errorf("Expected level content to contain level name %q\n%s", levelName, content)
	}
	if !strings.Contains(content, sectorName) {
		t.Errorf("Expected level content to contain sector %q\n%s", sectorName, content)
	}
}

// Session lifecycle tests (login/logout) live in zz_session_test.go so they
// run after everything else — see the comment there.
