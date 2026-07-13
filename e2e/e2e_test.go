//go:build e2e

// Package e2e contains end-to-end tests for the encx library and the encli
// CLI tool against a real Encounter domain.
//
// The tests are excluded from regular builds by the "e2e" build tag. Run them
// explicitly:
//
//	go test -tags e2e ./e2e -v
//
// Configuration (environment variables):
//
//	ENCX_E2E_DOMAIN    Encounter domain          (default: svk.en.cx)
//	ENCX_E2E_LOGIN     account login             (default: skrashevich)
//	ENCX_E2E_PASSWORD  account password
//	ENCX_E2E_GAME_ID   sandbox game id           (default: 82448)
//
// The account must be an author of the sandbox game. Tests create their own
// levels at the end of the game, verify content, and delete everything they
// created. Existing levels are never modified.
package e2e

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/skrashevich/encx-cli/encx"
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func e2eDomain() string   { return envOr("ENCX_E2E_DOMAIN", "svk.en.cx") }
func e2eLogin() string    { return envOr("ENCX_E2E_LOGIN", "") }
func e2ePassword() string { return envOr("ENCX_E2E_PASSWORD", "") }

func e2eGameID() int {
	if v := os.Getenv("ENCX_E2E_GAME_ID"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 82448
}

func newClient() *encx.Client {
	return encx.New(e2eDomain())
}

// skipOnAntiSpam skips the test if the domain answered with an anti-spam
// challenge, mirroring the convention used by the integration tests in encx.
func skipOnAntiSpam(t *testing.T, err error) {
	t.Helper()
	if encx.IsAntiSpam(err) {
		t.Skipf("Domain anti-spam active: %s", encx.AntiSpamURLFromError(err))
	}
}

// uniqueName returns a name that is recognizably test-generated and unique
// within a run, so leftovers from crashed runs are easy to spot and clean up.
func uniqueName(prefix string) string {
	return fmt.Sprintf("e2e-%s-%d", prefix, time.Now().UnixNano())
}

var (
	adminOnce       sync.Once
	adminShared     *encx.Client
	adminErr        error
	sessionSnapshot []byte // cookies exported right after the shared login
)

// initAdminClient logs in the shared library client exactly once per run.
// It is called from TestMain before any test (in particular before negative
// login scenarios) so the session is established while the account has no
// failed-attempt history.
func initAdminClient() error {
	adminOnce.Do(func() {
		c := newClient()
		adminErr = c.LoginComplete(context.Background(), e2eLogin(), e2ePassword())
		if adminErr == nil {
			adminShared = c
			sessionSnapshot, adminErr = c.ExportCookies()
		}
	})
	return adminErr
}

// playerClient returns a fresh client carrying the pristine session cookies
// captured right after login. Game-engine endpoints reject sessions whose
// cookies were rotated by admin-panel requests, so player-side calls must
// not share the admin client's live cookie jar.
func playerClient(t *testing.T) *encx.Client {
	t.Helper()
	adminClient(t) // ensure the shared session exists (or skip/fail)
	c := newClient()
	if err := c.ImportCookies(sessionSnapshot); err != nil {
		t.Fatalf("ImportCookies for player client failed: %v", err)
	}
	return c
}

// isLoginBlocked reports whether the error is the domain's anti-brute-force
// response ("Превышено количество неправильных попыток авторизации").
func isLoginBlocked(err error) bool {
	return err != nil && strings.Contains(err.Error(), "Превышено количество")
}

// adminClient returns a shared client with a full (admin-capable) session.
// The login happens once per test run to avoid tripping the domain's
// anti-brute-force protection with repeated logins.
func adminClient(t *testing.T) *encx.Client {
	t.Helper()
	if err := initAdminClient(); err != nil {
		skipOnAntiSpam(t, err)
		if isLoginBlocked(err) {
			t.Skipf("Login temporarily blocked by anti-brute-force protection: %v", err)
		}
		t.Fatalf("Admin login failed: %v", err)
	}
	return adminShared
}

func eventCode(event any) int {
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

const finishTimeLayout = "02.01.2006 15:04:05"

// extendGameFinish pushes the sandbox game's finish date three months into
// the future so player-side scenarios can run.
func extendGameFinish(ctx context.Context, admin *encx.Client) error {
	info, err := admin.AdminGetGameInfo(ctx, e2eGameID())
	if err != nil {
		return fmt.Errorf("read game info: %w", err)
	}
	info.FinishDateTime = time.Now().AddDate(0, 3, 0).Format(finishTimeLayout)
	if err := admin.AdminUpdateGameInfo(ctx, e2eGameID(), *info); err != nil {
		return fmt.Errorf("update game info: %w", err)
	}
	return nil
}

// ensureActiveGame fetches the sandbox game model and, if the game has
// finished, extends its finish date and retries. Tests that need a running
// game are skipped when the game cannot be (re)activated.
func ensureActiveGame(t *testing.T, c *encx.Client) *encx.GameModel {
	t.Helper()
	ctx := t.Context()
	model, err := c.GetGameModel(ctx, e2eGameID())
	if err != nil {
		skipOnAntiSpam(t, err)
		t.Fatalf("GetGameModel(%d) failed: %v", e2eGameID(), err)
	}
	if eventCode(model.Event) == encx.EventGameNormal {
		return model
	}
	t.Logf("Game %d is not active (event %d), extending finish date", e2eGameID(), eventCode(model.Event))
	if err := extendGameFinish(ctx, adminClient(t)); err != nil {
		t.Skipf("Game %d is not active and could not be extended: %v", e2eGameID(), err)
	}
	model, err = c.GetGameModel(ctx, e2eGameID())
	if err != nil {
		t.Fatalf("GetGameModel(%d) after extension failed: %v", e2eGameID(), err)
	}
	if code := eventCode(model.Event); code != encx.EventGameNormal {
		t.Skipf("Game %d is still not active after extension (event %d)", e2eGameID(), code)
	}
	return model
}
