package encxmobile

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sipeed/picoclaw/pkg/auth"
	"github.com/sipeed/picoclaw/pkg/providers"
)

// AuthMethodCodex selects a ChatGPT subscription instead of an API key.
const AuthMethodCodex = "codex"

// CodexDeviceLogin drives the ChatGPT device-authorization flow.
//
// The device flow suits a phone: there is no redirect URI to catch and no local
// callback listener to run. The app shows UserCode, sends the player to
// VerifyURL in a browser, and polls until they approve.
//
// Usage from Swift:
//
//	login, err := StartCodexDeviceLogin()
//	// show login.UserCode(), open login.VerifyURL()
//	credentialJSON, err := login.Wait(300)   // blocking; call off the main thread
//	// store credentialJSON in the Keychain, pass it back in the agent config
type CodexDeviceLogin struct {
	mu           sync.Mutex
	deviceAuthID string
	userCode     string
	verifyURL    string
	interval     time.Duration
	cancelled    bool
}

// StartCodexDeviceLogin asks OpenAI for a device code.
func StartCodexDeviceLogin() (*CodexDeviceLogin, error) {
	info, err := auth.RequestDeviceCode(auth.OpenAIOAuthConfig())
	if err != nil {
		return nil, fmt.Errorf("encxmobile: start ChatGPT login: %w", err)
	}
	interval := time.Duration(info.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	return &CodexDeviceLogin{
		deviceAuthID: info.DeviceAuthID,
		userCode:     info.UserCode,
		verifyURL:    info.VerifyURL,
		interval:     interval,
	}, nil
}

// UserCode is the code the player types on the verification page.
func (l *CodexDeviceLogin) UserCode() string { return l.userCode }

// VerifyURL is the page where the player approves the login.
func (l *CodexDeviceLogin) VerifyURL() string { return l.verifyURL }

// IntervalSeconds is the polling interval OpenAI asked for.
func (l *CodexDeviceLogin) IntervalSeconds() int64 {
	return int64(l.interval / time.Second)
}

// defaultCodexLoginTimeout matches the window PicoClaw's own CLI allows: the
// player has to switch apps, sign in and type a code.
const defaultCodexLoginTimeout = 15 * time.Minute

// Poll checks once whether the player has approved. It returns the credential
// JSON on success and an empty string while the approval is still pending.
//
// The upstream endpoint reports "not approved yet" as an HTTP error, which
// PicoClaw surfaces as a bare "pending" error, so that one is translated back
// into the pending state instead of being shown to the player as a failure.
func (l *CodexDeviceLogin) Poll() (string, error) {
	return l.pollOnce(func() (*auth.AuthCredential, error) {
		return auth.PollDeviceCodeOnce(auth.OpenAIOAuthConfig(), l.deviceAuthID, l.userCode)
	})
}

// pollOnce is the transport-free half of Poll, so the pending/failure decision
// can be exercised without a live issuer.
func (l *CodexDeviceLogin) pollOnce(ask func() (*auth.AuthCredential, error)) (string, error) {
	cred, err := ask()
	if err != nil {
		if isDeviceApprovalPending(err) {
			return "", nil
		}
		return "", fmt.Errorf("encxmobile: ChatGPT login failed: %w", err)
	}
	if cred == nil {
		return "", nil
	}
	return marshalJSON(cred)
}

func isDeviceApprovalPending(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "pending")
}

// Wait polls until the player approves, the timeout expires, or Cancel is
// called. It blocks, so callers must keep it off the UI thread.
//
// Transient poll failures do not abort the login: the player is in another app
// and cannot react to them. The last one is reported only if the wait runs out.
func (l *CodexDeviceLogin) Wait(timeoutSeconds int64) (string, error) {
	timeout := time.Duration(timeoutSeconds) * time.Second
	if timeoutSeconds <= 0 {
		timeout = defaultCodexLoginTimeout
	}
	deadline := time.Now().Add(timeout)

	return l.waitFor(deadline, l.Poll)
}

func (l *CodexDeviceLogin) waitFor(deadline time.Time, poll func() (string, error)) (string, error) {
	var lastErr error
	for {
		if l.isCancelled() {
			return "", errors.New("encxmobile: ChatGPT login was cancelled")
		}

		credentialJSON, err := poll()
		switch {
		case err != nil:
			lastErr = err
		case credentialJSON != "":
			return credentialJSON, nil
		}

		if time.Now().After(deadline) {
			if lastErr != nil {
				return "", lastErr
			}
			return "", errors.New("encxmobile: ChatGPT login timed out before approval")
		}
		time.Sleep(l.interval)
	}
}

// Cancel stops a running Wait.
func (l *CodexDeviceLogin) Cancel() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cancelled = true
}

func (l *CodexDeviceLogin) isCancelled() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.cancelled
}

// codexCredential is the persisted half of a ChatGPT login.
type codexCredential struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	AccountID    string    `json:"account_id,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	Email        string    `json:"email,omitempty"`
}

func parseCodexCredential(credentialJSON string) (*codexCredential, error) {
	trimmed := strings.TrimSpace(credentialJSON)
	if trimmed == "" {
		return nil, errors.New("encxmobile: the ChatGPT credential is empty; sign in again")
	}
	var cred codexCredential
	if err := json.Unmarshal([]byte(trimmed), &cred); err != nil {
		return nil, fmt.Errorf("encxmobile: parse ChatGPT credential: %w", err)
	}
	if strings.TrimSpace(cred.AccessToken) == "" {
		return nil, errors.New("encxmobile: the ChatGPT credential has no access token; sign in again")
	}
	return &cred, nil
}

func (c *codexCredential) authCredential() *auth.AuthCredential {
	return &auth.AuthCredential{
		AccessToken:  c.AccessToken,
		RefreshToken: c.RefreshToken,
		AccountID:    c.AccountID,
		ExpiresAt:    c.ExpiresAt,
		Email:        c.Email,
		Provider:     "openai",
		AuthMethod:   "oauth",
	}
}

// codexTokenStore keeps the live credential for one session and refreshes it.
//
// PicoClaw's own refresh path reads and writes ~/.picoclaw credentials on disk,
// which does not exist on iOS, so the session owns the credential instead and
// the host persists it to the Keychain after each turn.
type codexTokenStore struct {
	mu sync.Mutex
	// refreshed guards the one renewal allowed for a credential without an expiry.
	refreshed bool
	cred      *auth.AuthCredential
}

func newCodexTokenStore(cred *codexCredential) *codexTokenStore {
	return &codexTokenStore{cred: cred.authCredential()}
}

// tokenSource matches the refresh callback PicoClaw's Codex provider expects:
// it returns the current access token and account ID, refreshing first when the
// token has expired.
func (s *codexTokenStore) tokenSource() func() (string, string, error) {
	return func() (string, string, error) {
		s.mu.Lock()
		defer s.mu.Unlock()

		if !s.needsRefreshLocked() {
			return s.cred.AccessToken, s.cred.AccountID, nil
		}
		if strings.TrimSpace(s.cred.RefreshToken) == "" {
			return "", "", errors.New("the ChatGPT session expired and cannot be refreshed; sign in again")
		}
		renewed, err := auth.RefreshAccessToken(s.cred, auth.OpenAIOAuthConfig())
		if err != nil {
			return "", "", fmt.Errorf("refreshing the ChatGPT session: %w", err)
		}
		s.cred = renewed
		s.refreshed = true
		return s.cred.AccessToken, s.cred.AccountID, nil
	}
}

// tokenRefreshSkew renews a token slightly before it expires.
//
// The Codex provider fetches the token once per LLM call, and one turn can make
// a dozen of them over several minutes. A token with seconds of life left would
// otherwise be attached to a request and expire before OpenAI validates it,
// failing the turn with a 401 that no retry covers.
const tokenRefreshSkew = 5 * time.Minute

// needsRefreshLocked reports whether the access token should be renewed. The
// caller holds s.mu.
func (s *codexTokenStore) needsRefreshLocked() bool {
	if s.cred.ExpiresAt.IsZero() {
		// Without an expiry there is no schedule to follow. Renew once so the
		// session starts on a token of known age, then leave it alone: refreshing
		// on every call would rotate the refresh token a dozen times per turn.
		return !s.refreshed && s.cred.RefreshToken != ""
	}
	return time.Now().Add(tokenRefreshSkew).After(s.cred.ExpiresAt)
}

// credentialJSON returns the current credential so the host can persist a
// refreshed token.
func (s *codexTokenStore) credentialJSON() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return marshalJSON(&codexCredential{
		AccessToken:  s.cred.AccessToken,
		RefreshToken: s.cred.RefreshToken,
		AccountID:    s.cred.AccountID,
		ExpiresAt:    s.cred.ExpiresAt,
		Email:        s.cred.Email,
	})
}

// newCodexProvider builds a PicoClaw provider backed by a ChatGPT subscription.
func newCodexProvider(credentialJSON string) (providers.LLMProvider, *codexTokenStore, error) {
	cred, err := parseCodexCredential(credentialJSON)
	if err != nil {
		return nil, nil, err
	}
	store := newCodexTokenStore(cred)
	provider := providers.NewCodexProviderWithTokenSource(
		cred.AccessToken,
		cred.AccountID,
		store.tokenSource(),
	)
	return provider, store, nil
}
