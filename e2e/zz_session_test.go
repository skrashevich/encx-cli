//go:build e2e

package e2e

// Session-churning tests: each fresh login rebinds the account's game-engine
// access to the newest session, breaking engine calls made with the shared
// session. The file is named with a "zz" prefix so these tests run after
// everything else in the package.

import (
	"strings"
	"testing"

	"github.com/skrashevich/encx-cli/encx"
)

func TestZZLibLoginSuccess(t *testing.T) {
	client := newClient()
	resp, err := client.Login(t.Context(), e2eLogin(), e2ePassword())
	if err != nil {
		skipOnAntiSpam(t, err)
		t.Fatalf("Login failed: %v", err)
	}
	if resp.Error == 1 {
		t.Skipf("Login temporarily blocked by anti-brute-force protection: %s", encx.LoginErrorText(resp.Error))
	}
	if resp.Error != 0 {
		t.Fatalf("Expected Error==0, got %d: %s", resp.Error, encx.LoginErrorText(resp.Error))
	}
}

// TestZZCLILoginLogoutFlow verifies the full session lifecycle in an isolated
// HOME: commands fail without a session, login persists one, logout clears it.
func TestZZCLILoginLogoutFlow(t *testing.T) {
	home := t.TempDir()

	_, stderr, code := runCLIIn(home, "-game-id", gameIDArg(), "status")
	if code == 0 {
		t.Fatal("Expected status to fail without a session")
	}
	if !strings.Contains(stderr, "No saved session") {
		t.Errorf("Expected 'No saved session' error, got %q", stderr)
	}

	stdout, stderr, code := runCLIIn(home, "login", "-login", e2eLogin(), "-password", e2ePassword())
	if code != 0 {
		if strings.Contains(stderr, "anti-spam") || strings.Contains(stderr, "antispam") ||
			strings.Contains(stderr, "Превышено количество") {
			t.Skipf("Login blocked by domain protection: %s", strings.TrimSpace(stderr))
		}
		t.Fatalf("login exited with %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "Login successful") {
		t.Errorf("Expected 'Login successful', got %q", stdout)
	}

	out, stderr, code := runCLIIn(home, "-json", "profile")
	if code != 0 {
		t.Fatalf("profile with saved session exited with %d: %s", code, stderr)
	}
	if !strings.Contains(strings.ToLower(out), strings.ToLower(e2eLogin())) {
		t.Errorf("Expected profile output to mention %q, got %s", e2eLogin(), out)
	}

	if _, stderr, code := runCLIIn(home, "logout"); code != 0 {
		t.Fatalf("logout exited with %d: %s", code, stderr)
	}

	_, stderr, code = runCLIIn(home, "-game-id", gameIDArg(), "status")
	if code == 0 {
		t.Fatal("Expected status to fail after logout")
	}
	if !strings.Contains(stderr, "No saved session") {
		t.Errorf("Expected 'No saved session' after logout, got %q", stderr)
	}
}
