package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/auth"
	"github.com/sipeed/picoclaw/pkg/providers"
)

// isolateLLMEnv clears every variable resolveAgentConfig reads and points the
// credential file at a temporary directory, so a real sign-in on the developer
// machine cannot change what these tests see.
func isolateLLMEnv(t *testing.T) string {
	t.Helper()
	for _, key := range []string{
		"LLM_AUTH", "LLM_API_KEY", "OPENROUTER_API_KEY",
		"LLM_MODEL", "OPENROUTER_MODEL", "LLM_BASE_URL", "OPENROUTER_BASE_URL",
		"GIGACHAT_CREDENTIALS", "GIGACHAT_SCOPE", "GIGACHAT_AUTH_URL",
		"GIGACHAT_BASE_URL", "GIGACHAT_MODEL", "GIGACHAT_CA_BUNDLE", "GIGACHAT_INSECURE",
	} {
		t.Setenv(key, "")
	}
	path := filepath.Join(t.TempDir(), "codex-auth.json")
	t.Setenv(codexAuthFileEnvVar, path)
	resetCodexTokenStores()
	resetGigaChatTokenStores()
	t.Cleanup(resetCodexTokenStores)
	t.Cleanup(resetGigaChatTokenStores)
	return path
}

func writeTestCodexCredential(t *testing.T, cred *codexCredential) {
	t.Helper()
	if err := saveCodexCredential(cred); err != nil {
		t.Fatalf("saveCodexCredential: %v", err)
	}
}

func TestCodexCredentialRoundTrip(t *testing.T) {
	path := isolateLLMEnv(t)
	expires := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	writeTestCodexCredential(t, &codexCredential{
		AccessToken:  "access-1",
		RefreshToken: "refresh-1",
		AccountID:    "acct-1",
		ExpiresAt:    expires,
	})

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat credential: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Fatalf("credential mode = %v, want 0600", perm)
	}

	got, err := loadCodexCredential()
	if err != nil {
		t.Fatalf("loadCodexCredential: %v", err)
	}
	if got.AccessToken != "access-1" || got.RefreshToken != "refresh-1" || got.AccountID != "acct-1" {
		t.Fatalf("credential = %+v", got)
	}
	if !got.ExpiresAt.Equal(expires) {
		t.Fatalf("credential = %+v, want expiry %v", got, expires)
	}
	if !hasCodexCredential() {
		t.Fatal("hasCodexCredential = false after a successful save")
	}

	if err := deleteCodexCredential(); err != nil {
		t.Fatalf("deleteCodexCredential: %v", err)
	}
	if hasCodexCredential() {
		t.Fatal("hasCodexCredential = true after delete")
	}
	// Removing an absent credential is what codex-logout does on a clean machine.
	if err := deleteCodexCredential(); err != nil {
		t.Fatalf("deleteCodexCredential on a missing file: %v", err)
	}
}

// os.WriteFile applies its mode only when it creates the file, so a credential
// written over an existing world-readable file would stay world-readable.
func TestSaveCodexCredentialTightensAnExistingFile(t *testing.T) {
	path := isolateLLMEnv(t)
	if err := os.WriteFile(path, []byte(`{"access_token":"old"}`), 0644); err != nil {
		t.Fatalf("seed credential: %v", err)
	}
	writeTestCodexCredential(t, &codexCredential{AccessToken: "new"})

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat credential: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Fatalf("credential mode = %v, want 0600 after overwriting a 0644 file", perm)
	}
	got, err := loadCodexCredential()
	if err != nil {
		t.Fatalf("loadCodexCredential: %v", err)
	}
	if got.AccessToken != "new" {
		t.Fatalf("access token = %q", got.AccessToken)
	}
	// The atomic write must not leave its temp file behind.
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("directory holds %d entries, want only the credential", len(entries))
	}
}

// -web runs chat turns concurrently; a store per turn would let two of them
// rotate the same refresh token and log the operator out.
func TestCodexProvidersShareOneTokenStore(t *testing.T) {
	isolateLLMEnv(t)
	writeTestCodexCredential(t, &codexCredential{
		AccessToken:  "access-1",
		RefreshToken: "refresh-1",
		AccountID:    "acct-1",
	})

	if _, err := newCodexProvider(); err != nil {
		t.Fatalf("first newCodexProvider: %v", err)
	}
	if _, err := newCodexProvider(); err != nil {
		t.Fatalf("second newCodexProvider: %v", err)
	}
	if len(codexStores) != 1 {
		t.Fatalf("token stores = %d, want one per credential file", len(codexStores))
	}

	// A token refreshed by one provider must be visible to the next one built,
	// instead of being re-read stale from disk.
	store := codexStores[codexAuthFile()]
	store.refresh = func(*auth.AuthCredential) (*auth.AuthCredential, error) {
		return &auth.AuthCredential{AccessToken: "fresh", ExpiresAt: time.Now().Add(time.Hour)}, nil
	}
	store.persist = func(*codexCredential) error { return nil }
	store.cred.ExpiresAt = time.Now().Add(time.Minute)
	if _, _, err := store.tokenSource()(); err != nil {
		t.Fatalf("tokenSource: %v", err)
	}
	if token, _ := store.snapshot(); token != "fresh" {
		t.Fatalf("snapshot token = %q, want the refreshed one", token)
	}
}

// The point of sharing the store is that concurrent -web turns produce exactly
// one rotation of the refresh token, not one per turn.
func TestConcurrentTurnsRefreshTheTokenOnce(t *testing.T) {
	isolateLLMEnv(t)
	writeTestCodexCredential(t, &codexCredential{
		AccessToken:  "stale",
		RefreshToken: "refresh-1",
		AccountID:    "acct-1",
		ExpiresAt:    time.Now().Add(time.Minute),
	})

	var refreshes atomic.Int64
	store := sharedCodexTokenStore(codexAuthFile(), &codexCredential{
		AccessToken:  "stale",
		RefreshToken: "refresh-1",
		AccountID:    "acct-1",
		ExpiresAt:    time.Now().Add(time.Minute),
	})
	store.refresh = func(*auth.AuthCredential) (*auth.AuthCredential, error) {
		refreshes.Add(1)
		return &auth.AuthCredential{
			AccessToken:  "fresh",
			RefreshToken: "refresh-2",
			ExpiresAt:    time.Now().Add(time.Hour),
		}, nil
	}

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			provider, err := newCodexProvider()
			if err != nil {
				t.Errorf("newCodexProvider: %v", err)
				return
			}
			_ = provider
			token, _, err := store.tokenSource()()
			if err != nil {
				t.Errorf("tokenSource: %v", err)
				return
			}
			if token != "fresh" {
				t.Errorf("token = %q, want the refreshed one", token)
			}
		}()
	}
	wg.Wait()

	if got := refreshes.Load(); got != 1 {
		t.Fatalf("refreshes = %d, want exactly 1 across concurrent turns", got)
	}
}

// AuthRegistry.ListStatus reads every sessionDir()/*.json as an Encounter domain
// session, and its Logout deletes the file it names. A credential stored there
// would appear in the -web auth panel as a domain whose Logout signs the
// operator out of ChatGPT, so it must stay out of that glob.
func TestCodexCredentialIsNotMistakenForADomainSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(codexAuthFileEnvVar, "")
	resetCodexTokenStores()
	t.Cleanup(resetCodexTokenStores)

	writeTestCodexCredential(t, &codexCredential{AccessToken: "access-1"})
	if !hasCodexCredential() {
		t.Fatal("the credential was not stored at the default path")
	}

	for _, status := range NewAuthRegistry().ListStatus() {
		t.Errorf("credential surfaced as domain %q (session %s)", status.Domain, status.SessionPath)
	}

	// The registry would delete it under that domain name; prove it cannot.
	NewAuthRegistry().Logout("codex-auth")
	NewAuthRegistry().Logout("auth")
	if !hasCodexCredential() {
		t.Fatal("a web logout deleted the ChatGPT credential")
	}
}

// ENCLI_CODEX_AUTH_FILE can undo the default path's safety, so a collision is
// worth saying out loud.
func TestCredentialPathCollisionIsDetected(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := sessionDir()

	for _, path := range []string{
		filepath.Join(dir, "auth.json"),
		filepath.Join(dir, "codex-auth.json"),
	} {
		if !credentialPathCollides(path) {
			t.Errorf("credentialPathCollides(%q) = false, want true", path)
		}
	}
	for _, path := range []string{
		filepath.Join(dir, "codex", "auth.json"), // the default
		filepath.Join(dir, "codex-auth.token"),   // not matched by *.json
		filepath.Join(t.TempDir(), "auth.json"),  // outside the session dir
	} {
		if credentialPathCollides(path) {
			t.Errorf("credentialPathCollides(%q) = true, want false", path)
		}
	}
}

func TestSaveCodexCredentialWarnsAboutACollidingOverride(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	resetCodexTokenStores()
	t.Cleanup(resetCodexTokenStores)

	capture := func(path string) string {
		t.Helper()
		t.Setenv(codexAuthFileEnvVar, path)
		read, write, err := os.Pipe()
		if err != nil {
			t.Fatalf("pipe: %v", err)
		}
		original := os.Stderr
		os.Stderr = write
		saveErr := saveCodexCredential(&codexCredential{AccessToken: "access-1"})
		os.Stderr = original
		write.Close()
		var buf strings.Builder
		if _, err := io.Copy(&buf, read); err != nil {
			t.Fatalf("read stderr: %v", err)
		}
		read.Close()
		if saveErr != nil {
			t.Fatalf("saveCodexCredential: %v", saveErr)
		}
		return buf.String()
	}

	if out := capture(filepath.Join(sessionDir(), "codex-auth.json")); !strings.Contains(out, codexAuthFileEnvVar) {
		t.Errorf("a colliding override produced no warning, got %q", out)
	}
	if out := capture(filepath.Join(sessionDir(), "codex", "auth.json")); out != "" {
		t.Errorf("the default path must not warn, got %q", out)
	}
}

func TestLoadCodexCredentialReportsMissingSignIn(t *testing.T) {
	isolateLLMEnv(t)
	_, err := loadCodexCredential()
	if !errors.Is(err, errNoCodexCredential) {
		t.Fatalf("error = %v, want errNoCodexCredential", err)
	}
}

func TestLoadCodexCredentialRejectsEmptyAccessToken(t *testing.T) {
	path := isolateLLMEnv(t)
	if err := os.WriteFile(path, []byte(`{"refresh_token":"r"}`), 0600); err != nil {
		t.Fatalf("write credential: %v", err)
	}
	_, err := loadCodexCredential()
	if err == nil || !strings.Contains(err.Error(), "codex-login") {
		t.Fatalf("error = %v, want a re-sign-in hint", err)
	}
}

func TestCodexTokenStoreKeepsAValidToken(t *testing.T) {
	store := newCLICodexTokenStore(&codexCredential{
		AccessToken:  "access-1",
		RefreshToken: "refresh-1",
		AccountID:    "acct-1",
		ExpiresAt:    time.Now().Add(time.Hour),
	})
	store.refresh = func(*auth.AuthCredential) (*auth.AuthCredential, error) {
		t.Fatal("refresh must not run for a token that is not near expiry")
		return nil, nil
	}
	store.persist = func(*codexCredential) error { return nil }

	token, accountID, err := store.tokenSource()()
	if err != nil {
		t.Fatalf("tokenSource: %v", err)
	}
	if token != "access-1" || accountID != "acct-1" {
		t.Fatalf("token = %q, account = %q", token, accountID)
	}
}

func TestCodexTokenStoreRefreshesAndPersists(t *testing.T) {
	store := newCLICodexTokenStore(&codexCredential{
		AccessToken:  "stale",
		RefreshToken: "refresh-1",
		AccountID:    "acct-1",
		// Inside codexTokenRefreshSkew, so the token must be renewed before use.
		ExpiresAt: time.Now().Add(time.Minute),
	})
	refreshes := 0
	var sawRefreshToken string
	store.refresh = func(current *auth.AuthCredential) (*auth.AuthCredential, error) {
		// Runs on the goroutine refreshBounded starts, so it must not call t.Fatal.
		refreshes++
		sawRefreshToken = current.RefreshToken
		// A renewal that omits the account ID must not lose it.
		return &auth.AuthCredential{
			AccessToken:  "fresh",
			RefreshToken: "refresh-2",
			ExpiresAt:    time.Now().Add(time.Hour),
		}, nil
	}
	var persisted []*codexCredential
	store.persist = func(cred *codexCredential) error {
		persisted = append(persisted, cred)
		return nil
	}

	source := store.tokenSource()
	token, accountID, err := source()
	if err != nil {
		t.Fatalf("tokenSource: %v", err)
	}
	if token != "fresh" || accountID != "acct-1" {
		t.Fatalf("token = %q, account = %q", token, accountID)
	}
	if sawRefreshToken != "refresh-1" {
		t.Fatalf("refresh was called with token %q", sawRefreshToken)
	}
	if len(persisted) != 1 || persisted[0].AccessToken != "fresh" || persisted[0].RefreshToken != "refresh-2" {
		t.Fatalf("persisted = %+v", persisted)
	}

	// The renewed token is far from expiry, so the next call must reuse it.
	if _, _, err := source(); err != nil {
		t.Fatalf("second tokenSource: %v", err)
	}
	if refreshes != 1 {
		t.Fatalf("refreshes = %d, want 1", refreshes)
	}
}

func TestCodexTokenStoreRefreshesOnceWithoutExpiry(t *testing.T) {
	store := newCLICodexTokenStore(&codexCredential{
		AccessToken:  "unknown-age",
		RefreshToken: "refresh-1",
	})
	refreshes := 0
	store.refresh = func(*auth.AuthCredential) (*auth.AuthCredential, error) {
		refreshes++
		return &auth.AuthCredential{AccessToken: "fresh", RefreshToken: "refresh-2"}, nil
	}
	store.persist = func(*codexCredential) error { return nil }

	source := store.tokenSource()
	for range 3 {
		if _, _, err := source(); err != nil {
			t.Fatalf("tokenSource: %v", err)
		}
	}
	if refreshes != 1 {
		t.Fatalf("refreshes = %d, want exactly 1 for a credential without an expiry", refreshes)
	}
}

// A failed renewal rotates nothing, so it must not permanently disable renewal
// for a long-lived -chat or -web process — but it must not be retried on every
// turn either, since the issuer is dialled under the store mutex.
func TestCodexTokenStoreThrottlesRetriesAfterAFailedRefresh(t *testing.T) {
	store := newCLICodexTokenStore(&codexCredential{
		AccessToken:  "unknown-age",
		RefreshToken: "refresh-1",
	})
	refreshes := 0
	store.refresh = func(*auth.AuthCredential) (*auth.AuthCredential, error) {
		refreshes++
		return nil, errors.New("issuer unreachable")
	}
	store.persist = func(*codexCredential) error { return nil }

	source := store.tokenSource()
	if _, _, err := source(); err == nil {
		t.Fatal("the first call must surface the refresh failure")
	}
	// Within the cooldown the run continues on the token already in hand.
	token, _, err := source()
	if err != nil {
		t.Fatalf("second tokenSource: %v", err)
	}
	if token != "unknown-age" {
		t.Fatalf("token = %q, want the credential we still hold", token)
	}
	if refreshes != 1 {
		t.Fatalf("refreshes = %d, want the retry throttled inside the cooldown", refreshes)
	}

	// Once the cooldown lapses the renewal re-arms: a transient failure must not
	// disable it for the life of the process.
	store.mu.Lock()
	store.lastRefreshAttempt = time.Now().Add(-codexRefreshRetryCooldown - time.Second)
	store.mu.Unlock()
	if _, _, err := source(); err == nil {
		t.Fatal("the re-armed attempt must surface the failure again")
	}
	if refreshes != 2 {
		t.Fatalf("refreshes = %d, want a retry after the cooldown lapsed", refreshes)
	}

	// A success closes it for good, so the refresh token is not rotated per turn.
	store.refresh = func(*auth.AuthCredential) (*auth.AuthCredential, error) {
		refreshes++
		return &auth.AuthCredential{AccessToken: "fresh", RefreshToken: "refresh-2"}, nil
	}
	store.mu.Lock()
	store.lastRefreshAttempt = time.Now().Add(-codexRefreshRetryCooldown - time.Second)
	store.mu.Unlock()
	if _, _, err := source(); err != nil {
		t.Fatalf("successful refresh: %v", err)
	}
	for range 3 {
		if _, _, err := source(); err != nil {
			t.Fatalf("after success: %v", err)
		}
	}
	if refreshes != 3 {
		t.Fatalf("refreshes = %d, want no further renewal after a successful one", refreshes)
	}
}

// Giving up on the wait must not give up the exclusion: the abandoned attempt
// still holds the only rotation in flight. Without the latch, each later turn
// starts another renewal with the same refresh token, the issuer rotates it out
// from under the earlier ones, and the last writer persists a credential the
// issuer has already invalidated.
func TestStalledRefreshDoesNotStackConcurrentRenewals(t *testing.T) {
	store := newCLICodexTokenStore(&codexCredential{
		AccessToken:  "stale",
		RefreshToken: "refresh-1",
		ExpiresAt:    time.Now().Add(-time.Hour), // the ordinary expiry path
	})
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	var started, peak atomic.Int64
	store.refresh = func(*auth.AuthCredential) (*auth.AuthCredential, error) {
		running := started.Add(1)
		for {
			was := peak.Load()
			if running <= was || peak.CompareAndSwap(was, running) {
				break
			}
		}
		<-release
		started.Add(-1)
		return nil, errors.New("unblocked after the test")
	}
	store.persist = func(*codexCredential) error { return nil }

	original := codexRefreshTimeout
	codexRefreshTimeout = 30 * time.Millisecond
	t.Cleanup(func() { codexRefreshTimeout = original })

	source := store.tokenSource()
	for turn := range 4 {
		if _, _, err := source(); err == nil {
			t.Fatalf("turn %d: a stalled renewal must not report success", turn+1)
		}
	}

	if got := peak.Load(); got != 1 {
		t.Fatalf("concurrent renewals = %d, want exactly 1 across four stalled turns", got)
	}
}

// The ordinary expiry path has no cooldown, so a failed renewal must stay
// retryable on the next turn — but strictly one at a time, and many callers
// arriving together must share a single attempt rather than each opening one.
func TestExpiredCredentialRetriesSeriallyUnderLoad(t *testing.T) {
	store := newCLICodexTokenStore(&codexCredential{
		AccessToken:  "stale",
		RefreshToken: "refresh-1",
		AccountID:    "acct-1",
		ExpiresAt:    time.Now().Add(-time.Hour),
	})
	var attempts, peak atomic.Int64
	fail := true
	var failMu sync.Mutex
	store.refresh = func(*auth.AuthCredential) (*auth.AuthCredential, error) {
		running := attempts.Add(1)
		for {
			was := peak.Load()
			if running <= was || peak.CompareAndSwap(was, running) {
				break
			}
		}
		defer attempts.Add(-1)
		time.Sleep(time.Millisecond)
		failMu.Lock()
		defer failMu.Unlock()
		if fail {
			fail = false
			return nil, errors.New("issuer hiccup")
		}
		return &auth.AuthCredential{
			AccessToken: "fresh",
			ExpiresAt:   time.Now().Add(time.Hour),
		}, nil
	}
	store.persist = func(*codexCredential) error { return nil }

	source := store.tokenSource()
	// The first wave hits a failing issuer, the second finds it healthy again.
	for range 2 {
		var wg sync.WaitGroup
		for range 6 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _, _ = source()
			}()
		}
		wg.Wait()
	}

	if got := peak.Load(); got != 1 {
		t.Fatalf("concurrent renewals = %d, want the latch to serialise them", got)
	}
	if token, account := store.snapshot(); token != "fresh" || account != "acct-1" {
		t.Fatalf("token = %q, account = %q, want the retry to have succeeded", token, account)
	}
}

// A renewal that lands after its starter stopped waiting still rotated the token
// at the issuer, so its result must be adopted rather than dropped.
func TestLateRefreshIsAdoptedNotDiscarded(t *testing.T) {
	store := newCLICodexTokenStore(&codexCredential{
		AccessToken:  "stale",
		RefreshToken: "refresh-1",
		AccountID:    "acct-1",
		ExpiresAt:    time.Now().Add(-time.Hour),
	})
	release := make(chan struct{})
	store.refresh = func(*auth.AuthCredential) (*auth.AuthCredential, error) {
		<-release
		return &auth.AuthCredential{
			AccessToken:  "late",
			RefreshToken: "refresh-2",
			ExpiresAt:    time.Now().Add(time.Hour),
		}, nil
	}
	var persisted []*codexCredential
	var persistMu sync.Mutex
	store.persist = func(cred *codexCredential) error {
		persistMu.Lock()
		defer persistMu.Unlock()
		persisted = append(persisted, cred)
		return nil
	}

	original := codexRefreshTimeout
	codexRefreshTimeout = 30 * time.Millisecond
	t.Cleanup(func() { codexRefreshTimeout = original })

	source := store.tokenSource()
	if _, _, err := source(); err == nil {
		t.Fatal("the starter must report the bounded wait failing")
	}

	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if token, account := store.snapshot(); token == "late" {
			if account != "acct-1" {
				t.Fatalf("account = %q, want the one carried forward", account)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the late renewal was discarded instead of committed")
		}
		time.Sleep(5 * time.Millisecond)
	}

	persistMu.Lock()
	defer persistMu.Unlock()
	if len(persisted) != 1 || persisted[0].RefreshToken != "refresh-2" {
		t.Fatalf("persisted = %+v, want the rotated token written to disk", persisted)
	}
}

// Renewal begins a whole skew before expiry, so a failed attempt inside that
// window must not fail the turn: the token in hand is still valid, and the skew
// exists precisely to absorb this.
func TestFailedRefreshInsideTheSkewKeepsTheValidToken(t *testing.T) {
	store := newCLICodexTokenStore(&codexCredential{
		AccessToken:  "still-valid",
		RefreshToken: "refresh-1",
		AccountID:    "acct-1",
		// Inside the skew, so a renewal is due, but four minutes of life remain.
		ExpiresAt: time.Now().Add(4 * time.Minute),
	})
	store.refresh = func(*auth.AuthCredential) (*auth.AuthCredential, error) {
		return nil, errors.New("issuer down")
	}
	store.persist = func(*codexCredential) error { return nil }

	token, account, err := store.tokenSource()()
	if err != nil {
		t.Fatalf("a failed renewal inside the skew must not fail the turn: %v", err)
	}
	if token != "still-valid" || account != "acct-1" {
		t.Fatalf("token = %q, account = %q", token, account)
	}
}

// Past the expiry there is no valid token to fall back to, so the failure is
// fatal to the turn and must be reported rather than papered over.
func TestFailedRefreshPastExpiryFailsTheTurn(t *testing.T) {
	store := newCLICodexTokenStore(&codexCredential{
		AccessToken:  "dead",
		RefreshToken: "refresh-1",
		ExpiresAt:    time.Now().Add(-time.Minute),
	})
	store.refresh = func(*auth.AuthCredential) (*auth.AuthCredential, error) {
		return nil, errors.New("issuer down")
	}
	store.persist = func(*codexCredential) error { return nil }

	if _, _, err := store.tokenSource()(); err == nil {
		t.Fatal("an expired token with a failed renewal must surface the failure")
	}
}

// picoclaw's RefreshAccessToken has no timeout, so an unresponsive issuer must
// not wedge the run forever.
func TestCodexTokenStoreBoundsAStalledRefresh(t *testing.T) {
	store := newCLICodexTokenStore(&codexCredential{
		AccessToken:  "stale",
		RefreshToken: "refresh-1",
		ExpiresAt:    time.Now().Add(-time.Hour),
	})
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	store.refresh = func(*auth.AuthCredential) (*auth.AuthCredential, error) {
		<-release
		return nil, errors.New("unblocked after the test")
	}
	store.persist = func(*codexCredential) error { return nil }

	// Pin the deadline low so the test does not wait the production timeout.
	original := codexRefreshTimeout
	codexRefreshTimeout = 50 * time.Millisecond
	t.Cleanup(func() { codexRefreshTimeout = original })

	done := make(chan error, 1)
	go func() {
		_, _, err := store.tokenSource()()
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "did not answer") {
			t.Fatalf("error = %v, want the bounded-wait failure", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a stalled refresh was not bounded")
	}
}

func TestCodexTokenStoreFailsWhenItCannotRefresh(t *testing.T) {
	store := newCLICodexTokenStore(&codexCredential{
		AccessToken: "stale",
		ExpiresAt:   time.Now().Add(-time.Hour),
	})
	store.refresh = func(*auth.AuthCredential) (*auth.AuthCredential, error) {
		t.Fatal("refresh must not run without a refresh token")
		return nil, nil
	}
	store.persist = func(*codexCredential) error { return nil }

	_, _, err := store.tokenSource()()
	if err == nil || !strings.Contains(err.Error(), "codex-login") {
		t.Fatalf("error = %v, want a re-sign-in hint", err)
	}
}

func TestCodexTokenStoreSurvivesAFailedPersist(t *testing.T) {
	store := newCLICodexTokenStore(&codexCredential{
		AccessToken:  "stale",
		RefreshToken: "refresh-1",
		ExpiresAt:    time.Now().Add(time.Minute),
	})
	store.refresh = func(*auth.AuthCredential) (*auth.AuthCredential, error) {
		return &auth.AuthCredential{AccessToken: "fresh", ExpiresAt: time.Now().Add(time.Hour)}, nil
	}
	store.persist = func(*codexCredential) error { return errors.New("read-only home") }

	token, _, err := store.tokenSource()()
	if err != nil {
		t.Fatalf("a failed persist must not fail the run: %v", err)
	}
	if token != "fresh" {
		t.Fatalf("token = %q", token)
	}
}

func TestResolveAgentConfigCodexNeedsASignIn(t *testing.T) {
	isolateLLMEnv(t)
	_, err := resolveAgentConfig(&config{llmAuth: authMethodCodex})
	if err == nil || !strings.Contains(err.Error(), "codex-login") {
		t.Fatalf("error = %v, want a codex-login hint", err)
	}
}

func TestResolveAgentConfigCodexUsesTheCodexDefaultModel(t *testing.T) {
	isolateLLMEnv(t)
	writeTestCodexCredential(t, &codexCredential{AccessToken: "access-1"})

	got, err := resolveAgentConfig(&config{llmAuth: "CODEX"})
	if err != nil {
		t.Fatalf("resolveAgentConfig: %v", err)
	}
	if got.AuthMethod != authMethodCodex {
		t.Fatalf("auth method = %q", got.AuthMethod)
	}
	if got.Model != defaultCodexModel {
		t.Fatalf("model = %q, want %q", got.Model, defaultCodexModel)
	}
	if got.APIKey != "" || got.BaseURL != "" {
		t.Fatalf("subscription config must carry no API key or base URL: %+v", got)
	}
}

// codexModel must resolve a name to whatever the ChatGPT backend would actually
// serve, so the execution report never credits a model that did not answer.
// The expectations mirror PicoClaw's resolveCodexModel outcome for each input.
func TestCodexModelResolvesTheNameTheBackendWillServe(t *testing.T) {
	for _, tc := range []struct {
		configured, want, why string
	}{
		{"", defaultCodexModel, "unset"},
		{"   ", defaultCodexModel, "blank"},
		{"gpt-5.1-codex-mini", "gpt-5.1-codex-mini", "gpt- family passes"},
		{"o3", "o3", "o3 family passes"},
		{"o4-mini", "o4-mini", "o4 family passes"},
		{"GPT-5.3-Codex", "gpt-5.3-codex", "matched case-insensitively, like upstream"},
		{"openai/gpt-4o", "gpt-4o", "an openai/ namespace is stripped, not refused"},
		{"openrouter/free", defaultCodexModel, "another namespace"},
		{"anthropic/claude-sonnet-4", defaultCodexModel, "another namespace"},
		// Bare vendor names carry no namespace to catch them: these are the ones a
		// slash-only check let through and the backend then silently substituted.
		{"claude-sonnet-4", defaultCodexModel, "bare vendor name"},
		{"qwen3-coder", defaultCodexModel, "bare vendor name"},
		{"deepseek-chat", defaultCodexModel, "bare vendor name"},
		{"grok-4", defaultCodexModel, "bare vendor name"},
		{"llama-3", defaultCodexModel, "bare vendor name"},
		{"some-future-family", defaultCodexModel, "unknown family falls back visibly"},
	} {
		if got := codexModel(tc.configured); got != tc.want {
			t.Errorf("codexModel(%q) = %q, want %q (%s)", tc.configured, got, tc.want, tc.why)
		}
	}
}

// The report is only truthful if codexModel's output is a fixed point of the
// backend's own resolution. Asserting the shape proves that without carrying a
// copy of upstream's table: upstream passes through a lower-cased, slash-free
// gpt-/o3/o4 name unchanged, and defaultCodexModel is one.
func TestCodexModelAlwaysReturnsAServedName(t *testing.T) {
	inputs := []string{
		"", "   ", "gpt-4o", "GPT-4O", "o3", "o4-mini", "openai/gpt-4o",
		"openai/gpt-4o/turbo", "openai/openai/gpt-4o", "openai/", "gpt-4o/turbo",
		"o3/mini", "/gpt-4o", "openrouter/free", "anthropic/claude-sonnet-4",
		"claude-sonnet-4", "qwen3-coder", "deepseek-chat", "grok-4", "llama-3",
		"glm-4", "gpt-", "o", "codex", "some-future-family", " openai/gpt-4o ",
	}
	for _, in := range inputs {
		got := codexModel(in)
		if got != strings.ToLower(got) {
			t.Errorf("codexModel(%q) = %q, which is not lower-cased", in, got)
		}
		if strings.Contains(got, "/") {
			t.Errorf("codexModel(%q) = %q, which still carries a namespace", in, got)
		}
		served := got == defaultCodexModel ||
			strings.HasPrefix(got, "gpt-") || strings.HasPrefix(got, "o3") || strings.HasPrefix(got, "o4")
		if !served {
			t.Errorf("codexModel(%q) = %q, which the backend would substitute", in, got)
		}
	}
}

func TestResolveAgentConfigCodexFallsBackFromAnOpenRouterModel(t *testing.T) {
	isolateLLMEnv(t)
	writeTestCodexCredential(t, &codexCredential{AccessToken: "access-1"})
	t.Setenv("LLM_MODEL", "openrouter/free")

	got, err := resolveAgentConfig(&config{llmAuth: authMethodCodex})
	if err != nil {
		t.Fatalf("resolveAgentConfig: %v", err)
	}
	if got.Model != defaultCodexModel {
		t.Fatalf("model = %q, want %q", got.Model, defaultCodexModel)
	}
}

func TestResolveAgentConfigCodexHonoursLLMModel(t *testing.T) {
	isolateLLMEnv(t)
	writeTestCodexCredential(t, &codexCredential{AccessToken: "access-1"})
	t.Setenv("LLM_MODEL", "gpt-5.1-codex-mini")

	got, err := resolveAgentConfig(&config{llmAuth: authMethodCodex})
	if err != nil {
		t.Fatalf("resolveAgentConfig: %v", err)
	}
	if got.Model != "gpt-5.1-codex-mini" {
		t.Fatalf("model = %q", got.Model)
	}
}

func TestResolveAgentConfigCodexFromEnvironment(t *testing.T) {
	isolateLLMEnv(t)
	writeTestCodexCredential(t, &codexCredential{AccessToken: "access-1"})
	t.Setenv("LLM_AUTH", authMethodCodex)

	got, err := resolveAgentConfig(&config{})
	if err != nil {
		t.Fatalf("resolveAgentConfig: %v", err)
	}
	if got.AuthMethod != authMethodCodex {
		t.Fatalf("auth method = %q", got.AuthMethod)
	}
}

func TestResolveAgentConfigPrefersAStoredCredentialOverNothing(t *testing.T) {
	isolateLLMEnv(t)
	writeTestCodexCredential(t, &codexCredential{AccessToken: "access-1"})

	got, err := resolveAgentConfig(&config{})
	if err != nil {
		t.Fatalf("resolveAgentConfig: %v", err)
	}
	if got.AuthMethod != authMethodCodex {
		t.Fatalf("auth method = %q, want the stored subscription", got.AuthMethod)
	}
}

// A local proxy needs no API key, so the no-key branch must not be read as
// "nothing is configured": an explicit endpoint outranks a leftover credential.
func TestResolveAgentConfigKeepsAnExplicitLocalEndpoint(t *testing.T) {
	for _, key := range []string{"LLM_BASE_URL", "OPENROUTER_BASE_URL"} {
		t.Run(key, func(t *testing.T) {
			isolateLLMEnv(t)
			writeTestCodexCredential(t, &codexCredential{AccessToken: "access-1"})
			t.Setenv(key, "http://127.0.0.1:8317/v1")

			got, err := resolveAgentConfig(&config{})
			if err != nil {
				t.Fatalf("resolveAgentConfig: %v", err)
			}
			if got.AuthMethod != "" {
				t.Fatalf("auth method = %q, want the configured endpoint to win", got.AuthMethod)
			}
			if got.BaseURL != "http://127.0.0.1:8317/v1" {
				t.Fatalf("base URL = %q", got.BaseURL)
			}
			if got.Model != defaultLLMModel {
				t.Fatalf("model = %q", got.Model)
			}
		})
	}
}

// The endpoint only outranks auto-selection; an explicit request still wins.
func TestResolveAgentConfigCodexOverridesAnExplicitEndpoint(t *testing.T) {
	isolateLLMEnv(t)
	writeTestCodexCredential(t, &codexCredential{AccessToken: "access-1"})
	t.Setenv("LLM_BASE_URL", "http://127.0.0.1:8317/v1")

	got, err := resolveAgentConfig(&config{llmAuth: authMethodCodex})
	if err != nil {
		t.Fatalf("resolveAgentConfig: %v", err)
	}
	if got.AuthMethod != authMethodCodex {
		t.Fatalf("auth method = %q", got.AuthMethod)
	}
}

func TestResolveAgentConfigKeepsTheAPIKeyPath(t *testing.T) {
	isolateLLMEnv(t)
	// A stored subscription must not silently override an explicit API key.
	writeTestCodexCredential(t, &codexCredential{AccessToken: "access-1"})
	t.Setenv("LLM_API_KEY", "sk-test")

	got, err := resolveAgentConfig(&config{})
	if err != nil {
		t.Fatalf("resolveAgentConfig: %v", err)
	}
	if got.AuthMethod != "" {
		t.Fatalf("auth method = %q, want the API key path", got.AuthMethod)
	}
	if got.APIKey != "sk-test" || got.Model != defaultLLMModel || got.BaseURL != defaultLLMBaseURL {
		t.Fatalf("config = %+v", got)
	}
}

func TestResolveAgentConfigAPIKeyOptOutIgnoresTheCredential(t *testing.T) {
	isolateLLMEnv(t)
	writeTestCodexCredential(t, &codexCredential{AccessToken: "access-1"})

	_, err := resolveAgentConfig(&config{llmAuth: authMethodAPIKey})
	if err == nil || !strings.Contains(err.Error(), "LLM_API_KEY") {
		t.Fatalf("error = %v, want the missing-API-key error", err)
	}
}

func TestResolveAgentConfigMissingKeyPointsAtCodexLogin(t *testing.T) {
	isolateLLMEnv(t)
	_, err := resolveAgentConfig(&config{})
	if err == nil {
		t.Fatal("resolveAgentConfig succeeded without any credential")
	}
	if !strings.Contains(err.Error(), "LLM_API_KEY") || !strings.Contains(err.Error(), "codex-login") {
		t.Fatalf("error = %v, want both transports mentioned", err)
	}
}

func TestResolveAgentConfigRejectsAnUnknownAuthMethod(t *testing.T) {
	isolateLLMEnv(t)
	_, err := resolveAgentConfig(&config{llmAuth: "chatgpt"})
	if err == nil || !strings.Contains(err.Error(), "apikey, codex or gigachat") {
		t.Fatalf("error = %v, want the accepted values", err)
	}
}

func TestNewPicoProviderBuildsACodexProvider(t *testing.T) {
	isolateLLMEnv(t)
	writeTestCodexCredential(t, &codexCredential{AccessToken: "access-1", AccountID: "acct-1"})

	provider, err := newPicoProvider(AgentConfig{AuthMethod: authMethodCodex, Model: defaultCodexModel})
	if err != nil {
		t.Fatalf("newPicoProvider: %v", err)
	}
	if _, ok := provider.(*providers.CodexProvider); !ok {
		t.Fatalf("provider = %T, want *providers.CodexProvider", provider)
	}
}

func TestNewPicoProviderCodexNeedsACredential(t *testing.T) {
	isolateLLMEnv(t)
	_, err := newPicoProvider(AgentConfig{AuthMethod: authMethodCodex})
	if !errors.Is(err, errNoCodexCredential) {
		t.Fatalf("error = %v, want errNoCodexCredential", err)
	}
}

func TestNewPicoProviderKeepsTheHTTPPathForAPIKeys(t *testing.T) {
	isolateLLMEnv(t)
	provider, err := newPicoProvider(AgentConfig{
		APIKey:  "sk-test",
		Model:   defaultLLMModel,
		BaseURL: defaultLLMBaseURL,
	})
	if err != nil {
		t.Fatalf("newPicoProvider: %v", err)
	}
	if _, ok := provider.(*providers.CodexProvider); ok {
		t.Fatal("the API key path must not build a Codex provider")
	}
}

// The loop in main() that finds the subcommand skips the argument after a flag
// unless isBoolFlag knows the flag takes no value. An unclassified boolean flag
// therefore swallows the subcommand: `encli -device codex-login` would print the
// usage instead of signing in.
func TestAuthFlagsAreClassifiedForSubcommandParsing(t *testing.T) {
	for _, flagName := range []string{"-device", "--device", "-no-browser", "--no-browser"} {
		if !isBoolFlag(flagName) {
			t.Errorf("isBoolFlag(%q) = false; the subcommand after it would be read as its value", flagName)
		}
	}
	for _, flagName := range []string{"-llm-auth", "--llm-auth"} {
		if !isValueFlag(flagName) {
			t.Errorf("isValueFlag(%q) = false; its value would leak into the prompt", flagName)
		}
		if isBoolFlag(flagName) {
			t.Errorf("isBoolFlag(%q) = true; it takes a value", flagName)
		}
	}
}

func TestSplitLLMArgsKeepsLLMAuthOutOfThePrompt(t *testing.T) {
	flagArgs, promptArgs, err := splitLLMArgs(
		[]string{"-llm-auth", "codex", "-game-id", "42", "--llm", "покажи", "статус"})
	if err != nil {
		t.Fatalf("splitLLMArgs: %v", err)
	}
	if strings.Join(flagArgs, " ") != "-llm-auth codex -game-id 42" {
		t.Fatalf("flags = %q", flagArgs)
	}
	if strings.Join(promptArgs, " ") != "покажи статус" {
		t.Fatalf("prompt = %q", promptArgs)
	}
}

func TestResolveAgentPricingReportsTheSubscription(t *testing.T) {
	pricing := resolveAgentPricing(t.Context(), AgentConfig{AuthMethod: authMethodCodex})
	if pricing == nil || !pricing.isSubscription {
		t.Fatalf("pricing = %+v, want a subscription", pricing)
	}
	if cost := computeLLMCost(pricing, 10_000, 5_000); cost != 0 {
		t.Fatalf("cost = %v, want 0 on a subscription", cost)
	}

	report := formatAgentExecutionReport(&llmSession{}, defaultCodexModel, pricing,
		time.Second, time.Second, 0, 1, 0, 100, 50)
	if !strings.Contains(report, "ChatGPT subscription") {
		t.Fatalf("report = %q", report)
	}
	russian := formatAgentExecutionReport(&llmSession{preferRussian: true}, defaultCodexModel, pricing,
		time.Second, time.Second, 0, 1, 0, 100, 50)
	if !strings.Contains(russian, "подписка ChatGPT") {
		t.Fatalf("russian report = %q", russian)
	}
}
