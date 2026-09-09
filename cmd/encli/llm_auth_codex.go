package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sipeed/picoclaw/pkg/auth"
	"github.com/sipeed/picoclaw/pkg/fileutil"
	"github.com/sipeed/picoclaw/pkg/providers"
)

// authMethodCodex runs the agent on a ChatGPT subscription (the backend the
// Codex CLI talks to) instead of an OpenAI-compatible API key.
const authMethodCodex = "codex"

// authMethodAPIKey is the explicit spelling of the default transport, so
// LLM_AUTH can be used to opt out of a stored ChatGPT credential.
const authMethodAPIKey = "apikey"

// defaultCodexModel is what the ChatGPT backend serves. The OpenRouter default
// would be rejected by the Codex endpoint, so subscription runs need their own.
const defaultCodexModel = "gpt-5.3-codex-spark"

// codexAuthFileEnvVar relocates the credential file; tests use it to stay out
// of the developer's real home directory.
const codexAuthFileEnvVar = "ENCLI_CODEX_AUTH_FILE"

// codexTokenRefreshSkew renews an access token slightly before it expires.
//
// One agent run makes a request per turn over several minutes, so a token with
// seconds of life left would be attached to a request and expire before OpenAI
// validated it, failing the turn with a 401 that no retry covers.
const codexTokenRefreshSkew = 5 * time.Minute

// codexRefreshTimeout bounds a single renewal attempt.
//
// PicoClaw's RefreshAccessToken posts through http.DefaultClient with no timeout
// and no context, so a blackholed issuer would otherwise wedge every -web chat
// turn for as long as the connection stayed open, and cancelling the run would
// not release them.
//
// This bounds the wait, not the request: the attempt keeps running and keeps the
// inFlight latch, which is what stops the next turn opening a second renewal
// with the same refresh token. The mutex alone cannot do that, because a caller
// that stops waiting releases it while its attempt is still live.
//
// A var, not a const, so tests can pin a short deadline instead of waiting it out.
var codexRefreshTimeout = 30 * time.Second

// codexRefreshRetryCooldown throttles renewal after a failed attempt, so a dead
// issuer costs one call a minute rather than one per agent turn.
const codexRefreshRetryCooldown = time.Minute

// errNoCodexCredential is returned when no ChatGPT sign-in is stored. It is a
// sentinel so codex-status can distinguish "not signed in" from a broken file.
var errNoCodexCredential = errors.New("not signed in to ChatGPT. Run: encli codex-login")

// codexCredential is the persisted half of a ChatGPT sign-in. It is a subset of
// auth.AuthCredential: provider and auth method are implied by the file.
type codexCredential struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	AccountID    string    `json:"account_id,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
}

// registerLLMAuthFlag exposes the transport choice on every surface that runs
// the agent: --llm, -chat and -web.
//
// The default is empty rather than os.Getenv("LLM_AUTH") so that LLM_AUTH is
// read in exactly one place, resolveAgentConfig, which also has to serve
// callers that never parsed a flag set.
func registerLLMAuthFlag(fs *flag.FlagSet, cfg *config) {
	fs.StringVar(&cfg.llmAuth, "llm-auth", "",
		"Agent LLM transport: apikey (default) or codex for a ChatGPT subscription (env: LLM_AUTH)")
}

// codexModel picks the model for a subscription run, resolving the name the
// same way the backend will.
//
// PicoClaw's Codex provider substitutes its own default for any model the
// ChatGPT backend does not serve, and its warning about that is invisible
// because the agent disables PicoClaw's console logger. Without resolving the
// name here, the execution report would print the requested model while a
// different one answered.
//
// An openai/ namespace is stripped, any other namespace is refused, and what
// remains must be a lower-cased gpt-, o3 or o4 family name.
//
// The guarantee is not that this agrees with resolveCodexModel on every input —
// it does not, and deliberately so: upstream checks the remaining namespace in an
// else-branch of the openai/ strip, so it forwards openai/gpt-4o/turbo with the
// slash intact, while this refuses it. The guarantee is that whatever this
// returns is a fixed point of upstream's resolution, so the name in the
// execution report is the name the backend serves. That holds by construction:
// every return is either defaultCodexModel or a lower-cased, slash-free
// gpt-/o3/o4 name, and upstream passes both through unchanged.
func codexModel(configured string) string {
	name := strings.ToLower(strings.TrimSpace(configured))
	if name == "" {
		return defaultCodexModel
	}
	if after, ok := strings.CutPrefix(name, "openai/"); ok {
		name = after
	}
	if !strings.Contains(name, "/") &&
		(strings.HasPrefix(name, "gpt-") || strings.HasPrefix(name, "o3") || strings.HasPrefix(name, "o4")) {
		return name
	}
	// -web resolves the transport on every config poll, so say this once per
	// process rather than once per request.
	codexModelWarning.Do(func() {
		fmt.Fprintf(os.Stderr,
			"%s is not a model the ChatGPT backend serves; using %s instead.\n"+
				"Set LLM_MODEL to a gpt-, o3- or o4-family name to choose another.\n",
			configured, defaultCodexModel)
	})
	return defaultCodexModel
}

// codexModelWarning keeps the substitution notice to one line per process.
var codexModelWarning sync.Once

// codexAuthFile resolves the credential path.
//
// It lives in a subdirectory rather than directly in sessionDir() because
// AuthRegistry.ListStatus globs sessionDir()/*.json and reads every match as an
// Encounter domain session. A credential sitting there would surface in the -web
// auth panel as a bogus "codex-auth" domain whose Logout button deletes it.
func codexAuthFile() string {
	if path := strings.TrimSpace(os.Getenv(codexAuthFileEnvVar)); path != "" {
		return path
	}
	return filepath.Join(sessionDir(), "codex", "auth.json")
}

// credentialPathCollides reports whether path lands in the non-recursive
// sessionDir()/*.json glob that AuthRegistry.ListStatus reads as domain sessions.
func credentialPathCollides(path string) bool {
	return filepath.Dir(path) == sessionDir() && filepath.Ext(path) == ".json"
}

// warnIfCredentialPathCollides reports an override that puts the credential back
// inside the glob the default path exists to avoid. It is only reachable through
// ENCLI_CODEX_AUTH_FILE, so a warning is the right weight: the operator chose the
// path, but the consequence — a -web logout deleting the sign-in — is silent.
func warnIfCredentialPathCollides(path string) {
	if !credentialPathCollides(path) {
		return
	}
	fmt.Fprintf(os.Stderr,
		"warning: %s puts the ChatGPT credential in %s, where -web lists every *.json as an\n"+
			"Encounter domain session and its Logout button would delete it. Prefer a subdirectory.\n",
		codexAuthFileEnvVar, sessionDir())
}

func loadCodexCredential() (*codexCredential, error) {
	path := codexAuthFile()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errNoCodexCredential
		}
		return nil, fmt.Errorf("read ChatGPT credential %s: %w", path, err)
	}
	var cred codexCredential
	if err := json.Unmarshal(data, &cred); err != nil {
		return nil, fmt.Errorf("parse ChatGPT credential %s: %w", path, err)
	}
	if strings.TrimSpace(cred.AccessToken) == "" {
		return nil, fmt.Errorf("ChatGPT credential %s has no access token. Run: encli codex-login", path)
	}
	return &cred, nil
}

func saveCodexCredential(cred *codexCredential) error {
	if cred == nil || strings.TrimSpace(cred.AccessToken) == "" {
		return errors.New("refusing to store a ChatGPT credential without an access token")
	}
	data, err := json.MarshalIndent(cred, "", "  ")
	if err != nil {
		return fmt.Errorf("encode ChatGPT credential: %w", err)
	}
	path := codexAuthFile()
	warnIfCredentialPathCollides(path)
	// MkdirAll first, with 0700: WriteFileAtomic would create the directory 0755.
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	// Written through a temp file and renamed, for two reasons: os.WriteFile
	// applies its mode only when creating, so it would leave a pre-existing file
	// world-readable, and a truncating write interrupted mid-refresh would leave
	// a credential that no longer parses.
	if err := fileutil.WriteFileAtomic(path, data, 0600); err != nil {
		return fmt.Errorf("write ChatGPT credential %s: %w", path, err)
	}
	return nil
}

func deleteCodexCredential() error {
	if err := os.Remove(codexAuthFile()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// hasCodexCredential reports whether a usable sign-in is on disk. It is the
// signal that lets an operator with no API key drop straight into --llm.
func hasCodexCredential() bool {
	_, err := loadCodexCredential()
	return err == nil
}

func codexCredentialFromAuth(cred *auth.AuthCredential) *codexCredential {
	return &codexCredential{
		AccessToken:  cred.AccessToken,
		RefreshToken: cred.RefreshToken,
		AccountID:    cred.AccountID,
		ExpiresAt:    cred.ExpiresAt,
	}
}

func (c *codexCredential) authCredential() *auth.AuthCredential {
	return &auth.AuthCredential{
		AccessToken:  c.AccessToken,
		RefreshToken: c.RefreshToken,
		AccountID:    c.AccountID,
		ExpiresAt:    c.ExpiresAt,
		Provider:     "openai",
		AuthMethod:   "oauth",
	}
}

// cliCodexTokenStore holds the live credential for one agent run, renews it
// when it nears expiry and writes the renewal back to disk so the next run
// starts from a fresh token.
type cliCodexTokenStore struct {
	mu sync.Mutex
	// refreshed records that a credential without an expiry has already been
	// renewed successfully; lastRefreshAttempt throttles retries after a failure.
	refreshed          bool
	lastRefreshAttempt time.Time
	cred               *auth.AuthCredential

	// inFlight is non-nil while a renewal is running, and is closed when it
	// finishes. A caller may stop waiting before the attempt returns, so the
	// mutex alone cannot keep renewals exclusive: without this latch the next
	// turn would start a second renewal with the same refresh token, the issuer
	// would rotate it out from under the first, and whichever landed last would
	// persist a credential the issuer had already invalidated.
	inFlight *codexRefreshAttempt

	// refresh and persist are injected so the refresh policy can be tested
	// without an OAuth issuer or a real home directory.
	refresh func(*auth.AuthCredential) (*auth.AuthCredential, error)
	persist func(*codexCredential) error
}

func newCLICodexTokenStore(cred *codexCredential) *cliCodexTokenStore {
	return &cliCodexTokenStore{
		cred: cred.authCredential(),
		refresh: func(current *auth.AuthCredential) (*auth.AuthCredential, error) {
			return auth.RefreshAccessToken(current, auth.OpenAIOAuthConfig())
		},
		persist: saveCodexCredential,
	}
}

// codexRefreshAttempt is one renewal. err is written before done is closed, so
// whoever observes the close sees it without taking the store mutex — and sees
// the outcome of THIS attempt rather than of whichever finished most recently.
type codexRefreshAttempt struct {
	done chan struct{}
	err  error
}

// tokenSource matches the refresh callback PicoClaw's Codex provider expects:
// the current access token and account ID, refreshed first when needed.
//
// At most one renewal runs at a time, and everyone who needs a fresh token waits
// for that one rather than starting another with the same refresh token.
func (s *cliCodexTokenStore) tokenSource() func() (string, string, error) {
	return func() (string, string, error) {
		s.mu.Lock()
		if !s.needsRefreshLocked() {
			token, account := s.cred.AccessToken, s.cred.AccountID
			s.mu.Unlock()
			return token, account, nil
		}
		attempt := s.inFlight
		if attempt == nil {
			if strings.TrimSpace(s.cred.RefreshToken) == "" {
				s.mu.Unlock()
				return "", "", errors.New("the ChatGPT session expired and cannot be refreshed. Run: encli codex-login")
			}
			attempt = s.startRefreshLocked()
		}
		s.mu.Unlock()

		timer := time.NewTimer(codexRefreshTimeout)
		defer timer.Stop()
		select {
		case <-attempt.done:
		case <-timer.C:
			// Give up waiting, but leave the attempt holding the latch: it is still
			// the only rotation in flight, and it commits its own result if it lands.
			return "", "", fmt.Errorf("the ChatGPT issuer did not answer within %s", codexRefreshTimeout)
		}

		s.mu.Lock()
		token, account, expiresAt := s.cred.AccessToken, s.cred.AccountID, s.cred.ExpiresAt
		s.mu.Unlock()

		if attempt.err != nil {
			// Renewal starts a whole skew before expiry, so the token in hand is
			// usually still valid. Failing the turn then would throw away exactly the
			// slack the skew exists to provide. Only a token past its expiry — or one
			// with no expiry to judge by — makes the failure fatal.
			if !expiresAt.IsZero() && time.Now().Before(expiresAt) {
				debugf("codex: renewal failed, continuing on a token valid until %s: %v",
					expiresAt.Format(time.RFC3339), attempt.err)
				return token, account, nil
			}
			return "", "", fmt.Errorf("refreshing the ChatGPT session: %w", attempt.err)
		}
		return token, account, nil
	}
}

// startRefreshLocked launches the one renewal allowed at a time and returns it.
// The caller holds s.mu.
//
// The attempt commits its own result, so a renewal that lands after its starter
// stopped waiting is still kept: discarding it would strand the credential on a
// refresh token the issuer has already rotated.
func (s *cliCodexTokenStore) startRefreshLocked() *codexRefreshAttempt {
	attempt := &codexRefreshAttempt{done: make(chan struct{})}
	s.inFlight = attempt
	s.lastRefreshAttempt = time.Now()
	// Read under the lock so the goroutine never touches these fields directly.
	previous, refresh := s.cred, s.refresh

	go func() {
		renewed, err := refresh(previous)

		s.mu.Lock()
		if err == nil && renewed != nil {
			if renewed.AccountID == "" {
				renewed.AccountID = previous.AccountID
			}
			s.cred = renewed
			s.refreshed = true
			if perr := s.persist(codexCredentialFromAuth(renewed)); perr != nil {
				// The run can continue on the token in memory; only the next process
				// would have to refresh again.
				debugf("codex: persisting the refreshed credential failed: %v", perr)
			}
		}
		s.inFlight = nil
		s.mu.Unlock()

		attempt.err = err
		close(attempt.done)
	}()
	return attempt
}

// needsRefreshLocked reports whether the access token should be renewed. The
// caller holds s.mu.
func (s *cliCodexTokenStore) needsRefreshLocked() bool {
	if !s.cred.ExpiresAt.IsZero() {
		return time.Now().Add(codexTokenRefreshSkew).After(s.cred.ExpiresAt)
	}
	// Without an expiry there is no schedule to follow. Exactly one SUCCESSFUL
	// renewal is allowed, so the run starts on a token of known age without
	// rotating the refresh token on every call. A failed attempt rotates nothing,
	// so it stays retryable — but behind a cooldown, otherwise a dead issuer would
	// be dialled once per agent turn for the life of a -chat or -web process.
	if s.refreshed || s.cred.RefreshToken == "" {
		return false
	}
	return s.lastRefreshAttempt.IsZero() || time.Since(s.lastRefreshAttempt) >= codexRefreshRetryCooldown
}

// snapshot returns the credential the provider should start from.
func (s *cliCodexTokenStore) snapshot() (string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cred.AccessToken, s.cred.AccountID
}

var (
	codexStoresMu sync.Mutex
	codexStores   = map[string]*cliCodexTokenStore{}
)

// sharedCodexTokenStore returns one store per credential file for the lifetime
// of the process.
//
// -web runs each chat turn in its own goroutine, so a store built per turn would
// let two turns crossing the refresh window call RefreshAccessToken with the
// same refresh token. OpenAI rotates it, so one rotation is burned and the last
// writer can persist a credential that no longer works — signing the operator
// out. One store per file serialises those refreshes into a single renewal.
func sharedCodexTokenStore(path string, cred *codexCredential) *cliCodexTokenStore {
	codexStoresMu.Lock()
	defer codexStoresMu.Unlock()
	if store, ok := codexStores[path]; ok {
		return store
	}
	store := newCLICodexTokenStore(cred)
	codexStores[path] = store
	return store
}

// resetCodexTokenStores drops the cache so tests do not inherit a store built by
// an earlier test.
func resetCodexTokenStores() {
	codexStoresMu.Lock()
	defer codexStoresMu.Unlock()
	clear(codexStores)
}

// newCodexProvider builds a PicoClaw provider backed by the stored ChatGPT
// subscription.
func newCodexProvider() (providers.LLMProvider, error) {
	cred, err := loadCodexCredential()
	if err != nil {
		return nil, err
	}
	// The cached store may already hold a token newer than the file, so the
	// provider starts from the store rather than from what was just read.
	store := sharedCodexTokenStore(codexAuthFile(), cred)
	accessToken, accountID := store.snapshot()
	return providers.NewCodexProviderWithTokenSource(
		accessToken,
		accountID,
		store.tokenSource(),
	), nil
}

// --- CLI commands ---

// runCodexLogin drives the chosen OAuth flow.
//
// PicoClaw prints the sign-in URL and the device code to stdout, which would
// interleave with the JSON result and leave it unparseable. Under -json those
// prompts go to stderr instead — the operator still has to read them, but a
// script reading stdout gets only the result.
func runCodexLogin(cfg *config) (*auth.AuthCredential, error) {
	if cfg.jsonOutput {
		original := os.Stdout
		os.Stdout = os.Stderr
		defer func() { os.Stdout = original }()
	}
	if cfg.codexDevice {
		return auth.LoginDeviceCode(auth.OpenAIOAuthConfig())
	}
	return auth.LoginBrowserWithOptions(auth.OpenAIOAuthConfig(),
		auth.LoginBrowserOptions{NoBrowser: cfg.codexNoBrowser})
}

func cmdCodexLogin(cfg *config) {
	cred, err := runCodexLogin(cfg)
	if err != nil {
		fatal("ChatGPT sign-in failed: %v", err)
	}
	if err := saveCodexCredential(codexCredentialFromAuth(cred)); err != nil {
		fatal("%v", err)
	}
	if cfg.jsonOutput {
		outputJSON(map[string]any{"success": true, "path": codexAuthFile()})
		return
	}
	fmt.Println("Signed in to ChatGPT")
	fmt.Printf("Credential stored in %s\n", codexAuthFile())
	fmt.Println("Run the agent with: encli --llm \"<prompt>\"")
}

func cmdCodexLogout(cfg *config) {
	if err := deleteCodexCredential(); err != nil {
		fatal("Failed to remove the ChatGPT credential: %v", err)
	}
	if cfg.jsonOutput {
		outputJSON(map[string]any{"success": true})
		return
	}
	fmt.Println("ChatGPT credential cleared")
}

func cmdCodexStatus(cfg *config) {
	cred, err := loadCodexCredential()
	if err != nil {
		if cfg.jsonOutput {
			outputJSON(map[string]any{"signed_in": false, "error": err.Error()})
			os.Exit(1)
		}
		fatal("%v", err)
	}
	expiry := ""
	if !cred.ExpiresAt.IsZero() {
		expiry = cred.ExpiresAt.Format(time.RFC3339)
	}
	if cfg.jsonOutput {
		outputJSON(map[string]any{
			"signed_in":  true,
			"account_id": cred.AccountID,
			"expires_at": expiry,
			"expired":    !cred.ExpiresAt.IsZero() && time.Now().After(cred.ExpiresAt),
			"path":       codexAuthFile(),
		})
		return
	}
	fmt.Println("Signed in to ChatGPT")
	if cred.AccountID != "" {
		fmt.Printf("  Account ID: %s\n", cred.AccountID)
	}
	switch {
	case expiry == "":
		fmt.Println("  Expires:    unknown (the token is refreshed on first use)")
	case time.Now().After(cred.ExpiresAt):
		fmt.Printf("  Expires:    %s (expired; it is refreshed on next use)\n", expiry)
	default:
		fmt.Printf("  Expires:    %s\n", expiry)
	}
	fmt.Printf("  Stored in:  %s\n", codexAuthFile())
}
