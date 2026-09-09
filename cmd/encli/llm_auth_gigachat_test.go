package main

import (
	"context"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// gigachatTokenServer serves the OAuth endpoint, recording what it was asked
// for and how often.
type gigachatTokenServer struct {
	server   *httptest.Server
	requests atomic.Int64

	mu       sync.Mutex
	lastRq   string
	lastAuth string
	lastForm string
}

func newGigaChatTokenServer(t *testing.T, handler http.HandlerFunc) *gigachatTokenServer {
	t.Helper()
	stub := &gigachatTokenServer{}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.requests.Add(1)
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse token form: %v", err)
		}
		stub.mu.Lock()
		stub.lastRq = r.Header.Get("RqUID")
		stub.lastAuth = r.Header.Get("Authorization")
		stub.lastForm = r.Form.Encode()
		stub.mu.Unlock()
		handler(w, r)
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

func (s *gigachatTokenServer) snapshot() (rqUID, authorization, form string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastRq, s.lastAuth, s.lastForm
}

func testGigaChatStore(t *testing.T, authURL string, client *http.Client) *gigachatTokenStore {
	t.Helper()
	return newGigaChatTokenStore(gigachatConfig{
		credentials: "Y2xpZW50OnNlY3JldA==",
		scope:       defaultGigaChatScope,
		authURL:     authURL,
	}, client)
}

func TestGigaChatConfigFromEnvDefaults(t *testing.T) {
	isolateLLMEnv(t)
	t.Setenv("GIGACHAT_CREDENTIALS", "  key-1  ")

	cfg, err := gigachatConfigFromEnv("")
	if err != nil {
		t.Fatalf("gigachatConfigFromEnv: %v", err)
	}
	if cfg.credentials != "key-1" {
		t.Fatalf("credentials = %q, want the trimmed key", cfg.credentials)
	}
	if cfg.scope != defaultGigaChatScope {
		t.Fatalf("scope = %q, want %q", cfg.scope, defaultGigaChatScope)
	}
	if cfg.authURL != defaultGigaChatAuthURL {
		t.Fatalf("authURL = %q, want %q", cfg.authURL, defaultGigaChatAuthURL)
	}
	if cfg.baseURL != defaultGigaChatBaseURL {
		t.Fatalf("baseURL = %q, want %q", cfg.baseURL, defaultGigaChatBaseURL)
	}
	if cfg.model != defaultGigaChatModel {
		t.Fatalf("model = %q, want %q", cfg.model, defaultGigaChatModel)
	}
	if !cfg.insecure {
		t.Fatal("insecure = false with no CA bundle; GigaChat's root is in no default trust store")
	}
}

func TestGigaChatConfigFromEnvPrecedence(t *testing.T) {
	isolateLLMEnv(t)
	t.Setenv("GIGACHAT_CREDENTIALS", "key-1")
	t.Setenv("GIGACHAT_SCOPE", "GIGACHAT_API_B2B")
	t.Setenv("GIGACHAT_AUTH_URL", "https://auth.example/oauth")

	// LLM_MODEL is the fallback for the model...
	cfg, err := gigachatConfigFromEnv("generic-model")
	if err != nil {
		t.Fatalf("gigachatConfigFromEnv: %v", err)
	}
	if cfg.model != "generic-model" {
		t.Fatalf("model = %q, want the generic model", cfg.model)
	}
	if cfg.scope != "GIGACHAT_API_B2B" || cfg.authURL != "https://auth.example/oauth" {
		t.Fatalf("config = %+v", cfg)
	}

	// ...and the GIGACHAT_-prefixed values win over it.
	t.Setenv("GIGACHAT_BASE_URL", "https://giga.example/api/v1/")
	t.Setenv("GIGACHAT_MODEL", "GigaChat-2-Pro")
	cfg, err = gigachatConfigFromEnv("generic-model")
	if err != nil {
		t.Fatalf("gigachatConfigFromEnv: %v", err)
	}
	if cfg.baseURL != "https://giga.example/api/v1" {
		t.Fatalf("baseURL = %q, want GIGACHAT_BASE_URL with its trailing slash removed", cfg.baseURL)
	}
	if cfg.model != "GigaChat-2-Pro" {
		t.Fatalf("model = %q, want GIGACHAT_MODEL to win", cfg.model)
	}
}

// A GigaChat run must never be pointed at LLM_BASE_URL: that variable names an
// OpenAI-compatible endpoint, and honouring it here would put a GigaChat OAuth
// bearer token in an Authorization header sent to that third party.
func TestGigaChatConfigIgnoresGenericBaseURL(t *testing.T) {
	isolateLLMEnv(t)
	t.Setenv("GIGACHAT_CREDENTIALS", "key-1")
	t.Setenv("LLM_BASE_URL", "https://openrouter.ai/api/v1")

	cfg, err := gigachatConfigFromEnv("")
	if err != nil {
		t.Fatalf("gigachatConfigFromEnv: %v", err)
	}
	if cfg.baseURL != defaultGigaChatBaseURL {
		t.Fatalf("baseURL = %q, want LLM_BASE_URL ignored", cfg.baseURL)
	}

	resolved, err := resolveAgentConfig(&config{llmAuth: authMethodGigaChat})
	if err != nil {
		t.Fatalf("resolveAgentConfig: %v", err)
	}
	if resolved.BaseURL != defaultGigaChatBaseURL {
		t.Fatalf("BaseURL = %q, want LLM_BASE_URL ignored", resolved.BaseURL)
	}
}

// GigaChat's root ships in no default trust store, so verification is off
// unless the operator supplies the root or asks for it explicitly.
func TestGigaChatSkipVerifyDefaults(t *testing.T) {
	isolateLLMEnv(t)
	if !gigachatSkipVerify("") {
		t.Fatal("gigachatSkipVerify = false with no CA bundle; the handshake would always fail")
	}
	if gigachatSkipVerify("/tmp/roots.pem") {
		t.Fatal("gigachatSkipVerify = true although a CA bundle was supplied")
	}

	t.Setenv("GIGACHAT_INSECURE", "0")
	if gigachatSkipVerify("") {
		t.Fatal("GIGACHAT_INSECURE=0 did not turn verification back on")
	}
	t.Setenv("GIGACHAT_INSECURE", "1")
	if !gigachatSkipVerify("/tmp/roots.pem") {
		t.Fatal("GIGACHAT_INSECURE=1 did not override the CA bundle")
	}
}

func TestGigaChatConfigFromEnvRequiresCredentials(t *testing.T) {
	isolateLLMEnv(t)
	if _, err := gigachatConfigFromEnv(""); err == nil {
		t.Fatal("gigachatConfigFromEnv succeeded without GIGACHAT_CREDENTIALS")
	}
	if hasGigaChatCredentials() {
		t.Fatal("hasGigaChatCredentials = true with no key configured")
	}
	t.Setenv("GIGACHAT_CREDENTIALS", "key-1")
	if !hasGigaChatCredentials() {
		t.Fatal("hasGigaChatCredentials = false with a key configured")
	}
}

func TestGigaChatTokenStoreExchange(t *testing.T) {
	expires := time.Now().Add(30 * time.Minute)
	stub := newGigaChatTokenServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
			t.Errorf("Content-Type = %q", got)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok-1","expires_at":` +
			strconv.FormatInt(expires.UnixMilli(), 10) + `}`))
	})

	store := testGigaChatStore(t, stub.server.URL, stub.server.Client())
	token, err := store.accessToken(context.Background())
	if err != nil {
		t.Fatalf("accessToken: %v", err)
	}
	if token != "tok-1" {
		t.Fatalf("token = %q, want tok-1", token)
	}

	rqUID, authorization, form := stub.snapshot()
	if authorization != "Basic Y2xpZW50OnNlY3JldA==" {
		t.Fatalf("Authorization = %q, want the key sent as Basic", authorization)
	}
	if form != "scope="+defaultGigaChatScope {
		t.Fatalf("form = %q, want the scope", form)
	}
	uuidV4 := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if !uuidV4.MatchString(rqUID) {
		t.Fatalf("RqUID = %q, want a version 4 UUID", rqUID)
	}
	if !store.expiresAt.Equal(time.UnixMilli(expires.UnixMilli())) {
		t.Fatalf("expiresAt = %v, want expires_at read as Unix milliseconds", store.expiresAt)
	}
}

func TestGigaChatTokenStoreCachesUntilSkew(t *testing.T) {
	issued := 0
	stub := newGigaChatTokenServer(t, func(w http.ResponseWriter, r *http.Request) {
		issued++
		_, _ = w.Write([]byte(`{"access_token":"tok-` + strconv.Itoa(issued) + `","expires_at":` +
			strconv.FormatInt(time.Now().Add(30*time.Minute).UnixMilli(), 10) + `}`))
	})

	store := testGigaChatStore(t, stub.server.URL, stub.server.Client())
	for range 3 {
		token, err := store.accessToken(context.Background())
		if err != nil {
			t.Fatalf("accessToken: %v", err)
		}
		if token != "tok-1" {
			t.Fatalf("token = %q, want the cached tok-1", token)
		}
	}
	if got := stub.requests.Load(); got != 1 {
		t.Fatalf("token requests = %d, want 1", got)
	}
}

func TestGigaChatTokenStoreRenewsInsideSkew(t *testing.T) {
	issued := 0
	stub := newGigaChatTokenServer(t, func(w http.ResponseWriter, r *http.Request) {
		issued++
		// Every token is already inside the renewal skew.
		_, _ = w.Write([]byte(`{"access_token":"tok-` + strconv.Itoa(issued) + `","expires_at":` +
			strconv.FormatInt(time.Now().Add(gigachatTokenTTLSkew/2).UnixMilli(), 10) + `}`))
	})

	store := testGigaChatStore(t, stub.server.URL, stub.server.Client())
	first, err := store.accessToken(context.Background())
	if err != nil {
		t.Fatalf("accessToken: %v", err)
	}
	second, err := store.accessToken(context.Background())
	if err != nil {
		t.Fatalf("accessToken: %v", err)
	}
	if first != "tok-1" || second != "tok-2" {
		t.Fatalf("tokens = %q, %q; want a renewal once the token is inside the skew", first, second)
	}
}

func TestGigaChatTokenStoreSingleFlight(t *testing.T) {
	release := make(chan struct{})
	stub := newGigaChatTokenServer(t, func(w http.ResponseWriter, r *http.Request) {
		<-release
		_, _ = w.Write([]byte(`{"access_token":"tok-1","expires_at":` +
			strconv.FormatInt(time.Now().Add(30*time.Minute).UnixMilli(), 10) + `}`))
	})

	store := testGigaChatStore(t, stub.server.URL, stub.server.Client())
	var wg sync.WaitGroup
	tokens := make([]string, 8)
	errs := make([]error, 8)
	for i := range tokens {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			tokens[idx], errs[idx] = store.accessToken(context.Background())
		}(i)
	}
	// Give every goroutine time to reach the store before the exchange returns.
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	for i := range tokens {
		if errs[i] != nil {
			t.Fatalf("accessToken[%d]: %v", i, errs[i])
		}
		if tokens[i] != "tok-1" {
			t.Fatalf("accessToken[%d] = %q, want tok-1", i, tokens[i])
		}
	}
	if got := stub.requests.Load(); got != 1 {
		t.Fatalf("token requests = %d, want a single exchange shared by every caller", got)
	}
}

func TestGigaChatTokenStoreHTTPError(t *testing.T) {
	stub := newGigaChatTokenServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Invalid authorization key"}`))
	})

	store := testGigaChatStore(t, stub.server.URL, stub.server.Client())
	_, err := store.accessToken(context.Background())
	if err == nil {
		t.Fatal("accessToken succeeded against a 401")
	}
	if !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "Invalid authorization key") {
		t.Fatalf("error = %v, want the status and the body", err)
	}
	if store.token != "" {
		t.Fatalf("token = %q, want nothing cached after a failed exchange", store.token)
	}
}

func TestGigaChatTokenStoreRejectsEmptyToken(t *testing.T) {
	stub := newGigaChatTokenServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"","expires_at":0}`))
	})

	store := testGigaChatStore(t, stub.server.URL, stub.server.Client())
	if _, err := store.accessToken(context.Background()); err == nil {
		t.Fatal("accessToken accepted a response with no token")
	}
}

func TestGigaChatTLSConfigCABundle(t *testing.T) {
	isolateLLMEnv(t)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"tok-1","expires_at":` +
			strconv.FormatInt(time.Now().Add(30*time.Minute).UnixMilli(), 10) + `}`))
	}))
	t.Cleanup(server.Close)

	bundle := filepath.Join(t.TempDir(), "roots.pem")
	encoded := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(bundle, encoded, 0600); err != nil {
		t.Fatalf("write CA bundle: %v", err)
	}

	cfg := gigachatConfig{credentials: "key-1", scope: defaultGigaChatScope, authURL: server.URL, caBundle: bundle}
	client, err := gigachatHTTPClient(cfg, gigachatTokenTimeout)
	if err != nil {
		t.Fatalf("gigachatHTTPClient: %v", err)
	}
	if _, err := newGigaChatTokenStore(cfg, client).accessToken(context.Background()); err != nil {
		t.Fatalf("accessToken with the server root in GIGACHAT_CA_BUNDLE: %v", err)
	}

	// Verification is only skipped when nothing was supplied; a bundle that does
	// not cover the server still fails closed.
	plain, err := gigachatHTTPClient(gigachatConfig{authURL: server.URL, caBundle: bundle + ".other"}, gigachatTokenTimeout)
	if err == nil {
		t.Fatal("gigachatHTTPClient accepted a CA bundle that does not exist")
	}
	plain, err = gigachatHTTPClient(gigachatConfig{authURL: server.URL}, gigachatTokenTimeout)
	if err != nil {
		t.Fatalf("gigachatHTTPClient: %v", err)
	}
	if _, err := newGigaChatTokenStore(gigachatConfig{
		credentials: "key-1", scope: defaultGigaChatScope, authURL: server.URL,
	}, plain).accessToken(context.Background()); err == nil {
		t.Fatal("a config that neither supplies a root nor skips verification trusted an unknown root")
	}

	// GIGACHAT_INSECURE is the documented escape hatch when the root cannot be
	// installed.
	insecureCfg := gigachatConfig{credentials: "key-1", scope: defaultGigaChatScope, authURL: server.URL, insecure: true}
	insecureClient, err := gigachatHTTPClient(insecureCfg, gigachatTokenTimeout)
	if err != nil {
		t.Fatalf("gigachatHTTPClient: %v", err)
	}
	if _, err := newGigaChatTokenStore(insecureCfg, insecureClient).accessToken(context.Background()); err != nil {
		t.Fatalf("accessToken with GIGACHAT_INSECURE: %v", err)
	}
}

func TestGigaChatTLSConfigRejectsUnusableBundle(t *testing.T) {
	dir := t.TempDir()
	if _, err := gigachatTLSConfig(gigachatConfig{caBundle: filepath.Join(dir, "missing.pem")}); err == nil {
		t.Fatal("gigachatTLSConfig accepted a missing CA bundle")
	}
	empty := filepath.Join(dir, "empty.pem")
	if err := os.WriteFile(empty, []byte("not a certificate"), 0600); err != nil {
		t.Fatalf("write bundle: %v", err)
	}
	if _, err := gigachatTLSConfig(gigachatConfig{caBundle: empty}); err == nil {
		t.Fatal("gigachatTLSConfig accepted a bundle with no PEM block")
	}
}

func TestResolveAgentConfigGigaChatExplicit(t *testing.T) {
	isolateLLMEnv(t)
	t.Setenv("GIGACHAT_CREDENTIALS", "key-1")
	t.Setenv("GIGACHAT_MODEL", "GigaChat-2-Pro")

	cfg, err := resolveAgentConfig(&config{llmAuth: "GigaChat"})
	if err != nil {
		t.Fatalf("resolveAgentConfig: %v", err)
	}
	if cfg.AuthMethod != authMethodGigaChat {
		t.Fatalf("AuthMethod = %q, want %q", cfg.AuthMethod, authMethodGigaChat)
	}
	if cfg.Model != "GigaChat-2-Pro" {
		t.Fatalf("Model = %q", cfg.Model)
	}
	if cfg.BaseURL != defaultGigaChatBaseURL {
		t.Fatalf("BaseURL = %q, want %q", cfg.BaseURL, defaultGigaChatBaseURL)
	}
	if cfg.APIKey != "" {
		t.Fatalf("APIKey = %q, want the GigaChat transport to carry no static key", cfg.APIKey)
	}
}

func TestResolveAgentConfigGigaChatAutoDetected(t *testing.T) {
	isolateLLMEnv(t)
	t.Setenv("GIGACHAT_CREDENTIALS", "key-1")

	cfg, err := resolveAgentConfig(&config{})
	if err != nil {
		t.Fatalf("resolveAgentConfig: %v", err)
	}
	if cfg.AuthMethod != authMethodGigaChat {
		t.Fatalf("AuthMethod = %q, want the GigaChat key to be picked up on its own", cfg.AuthMethod)
	}
}

func TestResolveAgentConfigGigaChatYieldsToExplicitAPIKey(t *testing.T) {
	isolateLLMEnv(t)
	t.Setenv("GIGACHAT_CREDENTIALS", "key-1")
	t.Setenv("LLM_API_KEY", "sk-test")

	cfg, err := resolveAgentConfig(&config{})
	if err != nil {
		t.Fatalf("resolveAgentConfig: %v", err)
	}
	if cfg.AuthMethod != "" {
		t.Fatalf("AuthMethod = %q, want an explicit API key to stay on the default transport", cfg.AuthMethod)
	}
	if cfg.BaseURL != defaultLLMBaseURL {
		t.Fatalf("BaseURL = %q, want %q", cfg.BaseURL, defaultLLMBaseURL)
	}
}

func TestResolveAgentConfigGigaChatWithoutCredentials(t *testing.T) {
	isolateLLMEnv(t)
	_, err := resolveAgentConfig(&config{llmAuth: authMethodGigaChat})
	if err == nil {
		t.Fatal("resolveAgentConfig succeeded for gigachat with no authorization key")
	}
	if !strings.Contains(err.Error(), "GIGACHAT_CREDENTIALS") {
		t.Fatalf("error = %v, want it to name the missing variable", err)
	}
}

func TestResolveAgentConfigUnknownMethodListsGigaChat(t *testing.T) {
	isolateLLMEnv(t)
	_, err := resolveAgentConfig(&config{llmAuth: "nonsense"})
	if err == nil {
		t.Fatal("resolveAgentConfig accepted an unknown auth method")
	}
	if !strings.Contains(err.Error(), authMethodGigaChat) {
		t.Fatalf("error = %v, want gigachat listed among the transports", err)
	}
}

func TestResolveAgentPricingSkipsGigaChat(t *testing.T) {
	if pricing := resolveAgentPricing(context.Background(), AgentConfig{
		AuthMethod: authMethodGigaChat,
		BaseURL:    defaultGigaChatBaseURL,
	}); pricing != nil {
		t.Fatalf("pricing = %+v, want no cost line for GigaChat", pricing)
	}
}

func TestNewRqUIDIsUnique(t *testing.T) {
	seen := map[string]struct{}{}
	for range 100 {
		id := newRqUID()
		if _, duplicate := seen[id]; duplicate {
			t.Fatalf("newRqUID repeated %q", id)
		}
		seen[id] = struct{}{}
	}
}
