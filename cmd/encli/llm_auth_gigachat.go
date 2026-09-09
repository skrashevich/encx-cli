package main

import (
	"cmp"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// authMethodGigaChat runs the agent on Sber's GigaChat API, which authorizes
// with a short-lived OAuth access token rather than a static API key.
const authMethodGigaChat = "gigachat"

const (
	// defaultGigaChatAuthURL issues the access token. The port is part of the
	// address: the OAuth gateway does not answer on 443.
	defaultGigaChatAuthURL = "https://ngw.devices.sberbank.ru:9443/api/v2/oauth"

	// defaultGigaChatBaseURL serves /chat/completions.
	//
	// This is the newer of the two hosts Sber runs. The legacy
	// gigachat.devices.sberbank.ru/api/v1 answers the same way but serves only
	// the first and second model generations: asking it for a GigaChat-3 name
	// returns 404 "No such model". Point GIGACHAT_BASE_URL at it to reach the
	// GigaChat / GigaChat-Pro / GigaChat-Max names, which exist only there.
	defaultGigaChatBaseURL = "https://api.giga.chat/v1"

	// legacyGigaChatBaseURL is named only so the 404 hint can suggest it.
	legacyGigaChatBaseURL = "https://gigachat.devices.sberbank.ru/api/v1"

	// defaultGigaChatScope is the personal-account API version. Companies and
	// sole traders are issued GIGACHAT_API_B2B or GIGACHAT_API_CORP instead.
	defaultGigaChatScope = "GIGACHAT_API_PERS"

	// defaultGigaChatModel is the strongest model of the current generation.
	// An agent run is a long chain of function calls, and the lighter models
	// drop out of it far sooner, so the default favours completing the task.
	defaultGigaChatModel = "GigaChat-2-Max"
)

// gigachatTokenTTLSkew renews an access token before it expires.
//
// A token lives 30 minutes while a single agent turn can run for minutes, so a
// token with seconds of life left would be attached to a request and expire
// before GigaChat validated it.
const gigachatTokenTTLSkew = 2 * time.Minute

// gigachatTokenTimeout bounds one token exchange.
const gigachatTokenTimeout = 30 * time.Second

// gigachatRequestTimeout bounds one chat completion. It matches the 600s the
// OpenAI-compatible transport is given, so a slow model is not cut off here.
const gigachatRequestTimeout = 600 * time.Second

// gigachatConfig is the resolved GigaChat transport, read from the environment
// in one place so that resolveAgentConfig and the provider agree.
type gigachatConfig struct {
	credentials string // base64(client id:client secret), the portal's "authorization key"
	scope       string
	authURL     string
	baseURL     string
	model       string
	caBundle    string // PEM file with the Russian trusted root CA
	insecure    bool   // skip certificate verification
}

// gigachatInsecureNotice keeps the "not verifying the certificate" line to one
// per process rather than one per request.
var gigachatInsecureNotice sync.Once

// errNoGigaChatCredentials is returned when the authorization key is missing.
// It is a sentinel so transport auto-detection can tell "not configured" from
// a malformed configuration.
var errNoGigaChatCredentials = errors.New(
	"GIGACHAT_CREDENTIALS is required for the gigachat transport: " +
		"paste the authorization key from the GigaChat API project in your Sber personal account")

// gigachatConfigFromEnv resolves the transport from the environment.
//
// model is the generic LLM_MODEL value, so the variable that names the agent's
// model keeps working; GIGACHAT_MODEL wins when both are set. LLM_BASE_URL is
// deliberately NOT consulted: it means "the OpenAI-compatible endpoint", and
// honouring it here would send a GigaChat OAuth bearer token in an
// Authorization header to whatever third party it names. GIGACHAT_BASE_URL is
// the override for this transport.
func gigachatConfigFromEnv(model string) (gigachatConfig, error) {
	credentials := strings.TrimSpace(os.Getenv("GIGACHAT_CREDENTIALS"))
	if credentials == "" {
		return gigachatConfig{}, errNoGigaChatCredentials
	}
	caBundle := strings.TrimSpace(os.Getenv("GIGACHAT_CA_BUNDLE"))
	return gigachatConfig{
		credentials: credentials,
		scope:       cmp.Or(strings.TrimSpace(os.Getenv("GIGACHAT_SCOPE")), defaultGigaChatScope),
		authURL:     cmp.Or(strings.TrimSpace(os.Getenv("GIGACHAT_AUTH_URL")), defaultGigaChatAuthURL),
		baseURL: strings.TrimRight(
			cmp.Or(strings.TrimSpace(os.Getenv("GIGACHAT_BASE_URL")), defaultGigaChatBaseURL), "/"),
		model:    cmp.Or(strings.TrimSpace(os.Getenv("GIGACHAT_MODEL")), strings.TrimSpace(model), defaultGigaChatModel),
		caBundle: caBundle,
		insecure: gigachatSkipVerify(caBundle),
	}, nil
}

// gigachatSkipVerify decides whether to verify GigaChat's certificate.
//
// It defaults to NOT verifying, which is the opposite of what a transport
// should normally do. The reason is that GigaChat is served under the Russian
// national CA, which ships in no default trust store on any platform this tool
// runs on: verifying by default does not make the connection safer, it makes
// every connection fail. An operator who has the root installs it through
// GIGACHAT_CA_BUNDLE, which turns verification back on against a pool that can
// actually succeed; GIGACHAT_INSECURE=0 forces it on without one, for a host
// whose system store already carries the root.
func gigachatSkipVerify(caBundle string) bool {
	if requested, ok := parseBoolArg(strings.TrimSpace(os.Getenv("GIGACHAT_INSECURE"))); ok {
		return requested
	}
	return caBundle == ""
}

// hasGigaChatCredentials reports whether an authorization key is configured. It
// is the signal that lets an operator with no API key drop straight into --llm.
func hasGigaChatCredentials() bool {
	return strings.TrimSpace(os.Getenv("GIGACHAT_CREDENTIALS")) != ""
}

// gigachatTLSConfig builds the TLS settings for both the token exchange and the
// chat requests.
//
// GigaChat is served under the Russian national CA, which is in no default
// trust store, so a verifying client fails the handshake. GIGACHAT_CA_BUNDLE
// adds that root to the system pool rather than replacing it; without one,
// gigachatSkipVerify has already decided to skip verification.
func gigachatTLSConfig(cfg gigachatConfig) (*tls.Config, error) {
	if cfg.insecure {
		gigachatInsecureNotice.Do(func() {
			fmt.Fprintln(os.Stderr,
				"GigaChat: certificate verification is off. Point GIGACHAT_CA_BUNDLE at the Russian\n"+
					"trusted root certificate to turn it back on.")
		})
		//nolint:gosec // G402: GigaChat's root ships in no default trust store; see gigachatSkipVerify.
		return &tls.Config{InsecureSkipVerify: true}, nil
	}
	if cfg.caBundle == "" {
		return nil, nil
	}
	pem, err := os.ReadFile(cfg.caBundle)
	if err != nil {
		return nil, fmt.Errorf("read GIGACHAT_CA_BUNDLE %s: %w", cfg.caBundle, err)
	}
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("GIGACHAT_CA_BUNDLE %s contains no PEM certificate", cfg.caBundle)
	}
	return &tls.Config{RootCAs: pool}, nil
}

// gigachatHTTPClient builds the client both GigaChat endpoints are called
// through, preserving the proxy, HTTP/2 and timeout settings of the default
// transport while replacing only its TLS configuration.
func gigachatHTTPClient(cfg gigachatConfig, timeout time.Duration) (*http.Client, error) {
	tlsConfig, err := gigachatTLSConfig(cfg)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: timeout}
	if tlsConfig == nil {
		return client, nil
	}
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		client.Transport = &http.Transport{TLSClientConfig: tlsConfig}
		return client, nil
	}
	cloned := transport.Clone()
	cloned.TLSClientConfig = tlsConfig
	client.Transport = cloned
	return client, nil
}

// gigachatTokenStore holds the live access token for one authorization key,
// renews it as it nears expiry, and lets at most one renewal run at a time.
type gigachatTokenStore struct {
	credentials string
	scope       string
	authURL     string
	client      *http.Client

	// now is the clock used for expiry decisions; tests pin it.
	now func() time.Time

	mu        sync.Mutex
	token     string
	expiresAt time.Time
	inFlight  *gigachatTokenAttempt
}

// gigachatTokenAttempt is one exchange. token and err are written before done
// is closed, so whoever observes the close sees the outcome of THIS attempt
// without taking the store mutex.
type gigachatTokenAttempt struct {
	done  chan struct{}
	token string
	err   error
}

func newGigaChatTokenStore(cfg gigachatConfig, client *http.Client) *gigachatTokenStore {
	return &gigachatTokenStore{
		credentials: cfg.credentials,
		scope:       cfg.scope,
		authURL:     cfg.authURL,
		client:      client,
		now:         time.Now,
	}
}

// accessToken returns a token valid for at least the renewal skew, exchanging
// the authorization key for a new one when the current token is too close to
// expiry.
//
// A caller that gives up waiting does not cancel the exchange: it is shared, so
// abandoning it would leave the next caller to start a second one against an
// endpoint that allows only ten token requests a second.
func (s *gigachatTokenStore) accessToken(ctx context.Context) (string, error) {
	s.mu.Lock()
	if s.token != "" && s.now().Add(gigachatTokenTTLSkew).Before(s.expiresAt) {
		token := s.token
		s.mu.Unlock()
		return token, nil
	}
	attempt := s.inFlight
	if attempt == nil {
		attempt = s.startExchangeLocked()
	}
	s.mu.Unlock()

	select {
	case <-attempt.done:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	if attempt.err != nil {
		return "", attempt.err
	}
	return attempt.token, nil
}

// startExchangeLocked launches the one exchange allowed at a time and returns
// it. The caller holds s.mu.
func (s *gigachatTokenStore) startExchangeLocked() *gigachatTokenAttempt {
	attempt := &gigachatTokenAttempt{done: make(chan struct{})}
	s.inFlight = attempt

	go func() {
		// Deliberately not the caller's context: the attempt outlives whichever
		// caller started it, and every waiter shares its result.
		ctx, cancel := context.WithTimeout(context.Background(), gigachatTokenTimeout)
		defer cancel()

		token, expiresAt, err := s.exchange(ctx)

		s.mu.Lock()
		if err == nil {
			s.token, s.expiresAt = token, expiresAt
		}
		s.inFlight = nil
		s.mu.Unlock()

		attempt.token, attempt.err = token, err
		close(attempt.done)
	}()
	return attempt
}

// gigachatAccessToken is the token-endpoint response. expires_at is Unix
// milliseconds, not seconds and not a duration.
type gigachatAccessToken struct {
	AccessToken string `json:"access_token"`
	ExpiresAt   int64  `json:"expires_at"`
}

func (s *gigachatTokenStore) exchange(ctx context.Context) (string, time.Time, error) {
	form := url.Values{"scope": {s.scope}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.authURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("build the GigaChat token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Basic "+s.credentials)
	req.Header.Set("RqUID", newRqUID())
	req.Header.Set("User-Agent", "encli/"+version)

	resp, err := s.client.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("request a GigaChat access token: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("read the GigaChat token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", time.Time{}, fmt.Errorf("GigaChat token request failed: HTTP %d: %s",
			resp.StatusCode, summarizeDebugText(string(body), gigachatErrorBodyLimit))
	}

	var token gigachatAccessToken
	if err := json.Unmarshal(body, &token); err != nil {
		return "", time.Time{}, fmt.Errorf("parse the GigaChat token response: %w", err)
	}
	if strings.TrimSpace(token.AccessToken) == "" {
		return "", time.Time{}, errors.New("the GigaChat token response carried no access token")
	}
	debugf("gigachat: access token issued, expires_at=%d", token.ExpiresAt)
	return token.AccessToken, time.UnixMilli(token.ExpiresAt), nil
}

// invalidate drops a token the server rejected, so the next caller exchanges a
// fresh one instead of resending a credential that is already dead.
//
// It compares before clearing: a 401 raced against a renewal must not throw
// away the token that renewal just installed.
func (s *gigachatTokenStore) invalidate(rejected string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token == rejected {
		s.token, s.expiresAt = "", time.Time{}
	}
}

var (
	gigachatStoresMu sync.Mutex
	gigachatStores   = map[string]*gigachatTokenStore{}
)

// sharedGigaChatTokenStore returns one store per authorization key for the
// lifetime of the process.
//
// -web runs each chat turn in its own goroutine, so a store built per turn would
// let every turn exchange its own token against an endpoint that rate-limits
// those requests. One store per key serialises them into a single renewal.
func sharedGigaChatTokenStore(cfg gigachatConfig) (*gigachatTokenStore, error) {
	key := cfg.authURL + "\x00" + cfg.scope + "\x00" + cfg.credentials

	gigachatStoresMu.Lock()
	defer gigachatStoresMu.Unlock()
	if store, ok := gigachatStores[key]; ok {
		return store, nil
	}
	client, err := gigachatHTTPClient(cfg, gigachatTokenTimeout)
	if err != nil {
		return nil, err
	}
	store := newGigaChatTokenStore(cfg, client)
	gigachatStores[key] = store
	return store, nil
}

// resetGigaChatTokenStores drops the cache so tests do not inherit a store
// built by an earlier test.
func resetGigaChatTokenStores() {
	gigachatStoresMu.Lock()
	defer gigachatStoresMu.Unlock()
	clear(gigachatStores)
}

// newRqUID returns the RFC 4122 version 4 UUID the token endpoint requires as a
// per-request identifier.
func newRqUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand does not fail in practice; a time-derived value still gives
		// the endpoint a distinct identifier rather than an empty header.
		return fmt.Sprintf("%08x-0000-4000-8000-%012x", time.Now().Unix(), time.Now().UnixNano())
	}
	b[6] = b[6]&0x0f | 0x40 // version 4
	b[8] = b[8]&0x3f | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
