package main

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/auth"
)

// fakeCodexIssuer stands in for the OAuth issuer. The manager calls exchange
// exactly where auth.ExchangeCodeForTokens would run, so the whole HTTP surface
// can be driven without a network.
type fakeCodexIssuer struct {
	mu       sync.Mutex
	calls    int
	code     string
	verifier string
	redirect string

	cred *auth.AuthCredential
	err  error
}

func (f *fakeCodexIssuer) exchange(_ auth.OAuthProviderConfig, code, verifier, redirectURI string) (*auth.AuthCredential, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.code, f.verifier, f.redirect = code, verifier, redirectURI
	if f.err != nil {
		return nil, f.err
	}
	return f.cred, nil
}

func (f *fakeCodexIssuer) exchanges() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// newCodexTestServer builds a hub whose sign-in manager talks to issuer and
// binds an ephemeral callback port. The registered port 1455 is not usable here:
// a developer machine may well have the Codex CLI or another encli holding it.
func newCodexTestServer(t *testing.T, issuer *fakeCodexIssuer) (*httptest.Server, *codexLoginManager) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	isolateLLMEnv(t)

	manager := &codexLoginManager{
		flows:       make(map[string]*codexLoginFlow),
		oauthConfig: auth.OpenAIOAuthConfig,
		exchange:    issuer.exchange,
		persist:     saveCodexCredential,
		listen:      func(int) (net.Listener, int, error) { return listenOnPort(0) },
	}
	hub := &webHub{
		cfg:        &config{},
		registry:   NewAuthRegistry(),
		store:      NewChatStore(),
		sse:        newSSEHub(),
		codexLogin: manager,
	}
	srv := httptest.NewTestServer(t, hub.newMux())
	srv.Start()
	return srv, manager
}

func codexSignedInIssuer() *fakeCodexIssuer {
	return &fakeCodexIssuer{cred: &auth.AuthCredential{
		AccessToken:  "access-1",
		RefreshToken: "refresh-1",
		AccountID:    "acct-1",
		ExpiresAt:    time.Now().Add(time.Hour),
	}}
}

func codexJSON(t *testing.T, method, rawURL, body string) (int, map[string]any, string) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = stringsReader(body)
	}
	req, err := http.NewRequest(method, rawURL, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, rawURL, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return res.StatusCode, out, string(raw)
}

// startCodexLogin opens a sign-in over HTTP and returns the flow the manager
// holds, so the test can reach the PKCE state the issuer would echo back.
func startCodexLogin(t *testing.T, srv *httptest.Server, manager *codexLoginManager, body string) (*codexLoginFlow, map[string]any) {
	t.Helper()
	status, payload, raw := codexJSON(t, http.MethodPost, srv.URL+"/api/v1/llm/codex/login", body)
	if status != http.StatusCreated {
		t.Fatalf("login status = %d, want 201 (body %s)", status, raw)
	}
	id, _ := payload["id"].(string)
	if id == "" {
		t.Fatalf("login payload carries no id: %s", raw)
	}
	flow, ok := manager.get(id)
	if !ok {
		t.Fatalf("the manager forgot flow %q immediately", id)
	}
	return flow, payload
}

func codexFlowStatus(t *testing.T, srv *httptest.Server, id string) map[string]any {
	t.Helper()
	status, payload, raw := codexJSON(t, http.MethodGet, srv.URL+"/api/v1/llm/codex/login/"+id, "")
	if status != http.StatusOK {
		t.Fatalf("flow status = %d, want 200 (body %s)", status, raw)
	}
	return payload
}

func TestWebCodexLoginStartDescribesTheAuthorizeURL(t *testing.T) {
	srv, manager := newCodexTestServer(t, codexSignedInIssuer())

	flow, payload := startCodexLogin(t, srv, manager, "")
	authorizeURL, _ := payload["authorize_url"].(string)
	redirectURI, _ := payload["redirect_uri"].(string)
	if authorizeURL == "" || redirectURI == "" {
		t.Fatalf("login payload = %+v", payload)
	}

	u, err := url.Parse(authorizeURL)
	if err != nil {
		t.Fatalf("authorize_url is not a URL: %v", err)
	}
	q := u.Query()
	// Without PKCE and a state the redirect could be replayed or forged, so both
	// have to reach the issuer.
	if q.Get("code_challenge") == "" {
		t.Errorf("authorize_url carries no code_challenge: %s", authorizeURL)
	}
	if got := q.Get("state"); got != flow.state {
		t.Errorf("authorize_url state = %q, want the flow's %q", got, flow.state)
	}
	if got := q.Get("redirect_uri"); got != redirectURI {
		t.Errorf("authorize_url redirect_uri = %q, want %q", got, redirectURI)
	}
	if flow.listener == nil {
		t.Fatal("the browser flow opened no callback listener")
	}

	if got := codexFlowStatus(t, srv, flow.ID)["status"]; got != codexLoginPending {
		t.Fatalf("status = %v, want %q before the redirect lands", got, codexLoginPending)
	}
}

// The callback listener is the whole point of the browser flow: the operator
// only completes the prompt, and the redirect finishes the sign-in.
func TestWebCodexLoginCompletesOnTheRedirect(t *testing.T) {
	issuer := codexSignedInIssuer()
	srv, manager := newCodexTestServer(t, issuer)
	flow, _ := startCodexLogin(t, srv, manager, "")

	res, err := http.Get(codexCallbackURL(flow, "auth-code-1", flow.state))
	if err != nil {
		t.Fatalf("redirect: %v", err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("redirect status = %d, body %s", res.StatusCode, body)
	}

	payload := codexFlowStatus(t, srv, flow.ID)
	if payload["status"] != codexLoginSuccess {
		t.Fatalf("status = %+v, want %q", payload, codexLoginSuccess)
	}
	if payload["account_id"] != "acct-1" {
		t.Errorf("account_id = %v, want the issuer's", payload["account_id"])
	}
	if issuer.exchanges() != 1 {
		t.Fatalf("exchanges = %d, want exactly one", issuer.exchanges())
	}
	// The code must be exchanged against the same PKCE material and redirect the
	// authorize URL advertised, or the issuer would reject it.
	if issuer.code != "auth-code-1" || issuer.verifier != flow.verifier || issuer.redirect != flow.RedirectURI {
		t.Errorf("exchange saw code=%q verifier=%q redirect=%q", issuer.code, issuer.verifier, issuer.redirect)
	}

	if !hasCodexCredential() {
		t.Fatal("a successful sign-in stored no credential")
	}
	cred, err := loadCodexCredential()
	if err != nil {
		t.Fatalf("loadCodexCredential: %v", err)
	}
	if cred.AccessToken != "access-1" || cred.RefreshToken != "refresh-1" || cred.AccountID != "acct-1" {
		t.Fatalf("credential = %+v", cred)
	}
}

// The state is the CSRF check: a redirect that did not come from the flow this
// panel started must not be exchanged.
func TestWebCodexLoginRejectsAForeignState(t *testing.T) {
	issuer := codexSignedInIssuer()
	srv, manager := newCodexTestServer(t, issuer)
	flow, _ := startCodexLogin(t, srv, manager, "")

	res, err := http.Get(codexCallbackURL(flow, "auth-code-1", "someone-elses-state"))
	if err != nil {
		t.Fatalf("redirect: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("redirect status = %d, want 400", res.StatusCode)
	}

	payload := codexFlowStatus(t, srv, flow.ID)
	if payload["status"] != codexLoginError {
		t.Fatalf("status = %+v, want %q", payload, codexLoginError)
	}
	if !strings.Contains(payload["error"].(string), "state") {
		t.Errorf("error = %v, want it to name the state mismatch", payload["error"])
	}
	if issuer.exchanges() != 0 {
		t.Errorf("exchanges = %d, want the code never exchanged", issuer.exchanges())
	}
	if hasCodexCredential() {
		t.Fatal("a forged redirect stored a credential")
	}
}

func TestWebCodexLoginReportsARefusedRedirect(t *testing.T) {
	issuer := codexSignedInIssuer()
	srv, manager := newCodexTestServer(t, issuer)
	flow, _ := startCodexLogin(t, srv, manager, "")

	res, err := http.Get(codexCallbackURL(flow, "", flow.state) + "&error=access_denied")
	if err != nil {
		t.Fatalf("redirect: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("redirect status = %d, want 400", res.StatusCode)
	}
	payload := codexFlowStatus(t, srv, flow.ID)
	if payload["status"] != codexLoginError {
		t.Fatalf("status = %+v, want %q", payload, codexLoginError)
	}
	if !strings.Contains(payload["error"].(string), "access_denied") {
		t.Errorf("error = %v, want the issuer's reason", payload["error"])
	}
	if hasCodexCredential() {
		t.Fatal("a refused sign-in stored a credential")
	}
}

// The manual path is the only one available when the browser runs on another
// machine, so a pasted redirect URL has to finish the flow.
func TestWebCodexLoginAcceptsAPastedRedirectURL(t *testing.T) {
	issuer := codexSignedInIssuer()
	srv, manager := newCodexTestServer(t, issuer)
	flow, payload := startCodexLogin(t, srv, manager, `{"no_browser":true}`)
	if flow.listener != nil {
		t.Fatal("no_browser still opened a callback listener")
	}
	redirectURI, _ := payload["redirect_uri"].(string)

	pasted := redirectURI + "?code=manual-code&state=" + url.QueryEscape(flow.state)
	body, err := json.Marshal(map[string]string{"code": pasted})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	status, got, raw := codexJSON(t, http.MethodPost, srv.URL+"/api/v1/llm/codex/login/"+flow.ID+"/code", string(body))
	if status != http.StatusOK {
		t.Fatalf("code status = %d, body %s", status, raw)
	}
	if got["status"] != codexLoginSuccess {
		t.Fatalf("status = %+v, want %q", got, codexLoginSuccess)
	}
	if issuer.code != "manual-code" {
		t.Errorf("exchange saw code %q", issuer.code)
	}
	if !hasCodexCredential() {
		t.Fatal("the manual path stored no credential")
	}
}

// A bare code is accepted too, since some operators copy only the parameter.
func TestWebCodexLoginAcceptsABareCode(t *testing.T) {
	issuer := codexSignedInIssuer()
	srv, manager := newCodexTestServer(t, issuer)
	flow, _ := startCodexLogin(t, srv, manager, `{"no_browser":true}`)

	status, got, raw := codexJSON(t, http.MethodPost,
		srv.URL+"/api/v1/llm/codex/login/"+flow.ID+"/code", `{"code":"bare-code"}`)
	if status != http.StatusOK {
		t.Fatalf("code status = %d, body %s", status, raw)
	}
	if got["status"] != codexLoginSuccess {
		t.Fatalf("status = %+v, want %q", got, codexLoginSuccess)
	}
	if issuer.code != "bare-code" {
		t.Errorf("exchange saw code %q", issuer.code)
	}
}

// Skipping the state check on the pasted path would leave a way around the CSRF
// protection the listener enforces.
func TestWebCodexLoginRejectsAPastedURLFromAnotherFlow(t *testing.T) {
	issuer := codexSignedInIssuer()
	srv, manager := newCodexTestServer(t, issuer)
	flow, payload := startCodexLogin(t, srv, manager, `{"no_browser":true}`)
	redirectURI, _ := payload["redirect_uri"].(string)

	pasted := redirectURI + "?code=manual-code&state=someone-elses-state"
	body, err := json.Marshal(map[string]string{"code": pasted})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	status, _, raw := codexJSON(t, http.MethodPost,
		srv.URL+"/api/v1/llm/codex/login/"+flow.ID+"/code", string(body))
	if status != http.StatusBadRequest {
		t.Fatalf("code status = %d, want 400 (body %s)", status, raw)
	}
	if issuer.exchanges() != 0 {
		t.Errorf("exchanges = %d, want the code never exchanged", issuer.exchanges())
	}
	if hasCodexCredential() {
		t.Fatal("a pasted URL from another sign-in stored a credential")
	}
	// The flow itself is untouched, so the operator can paste the right URL.
	if got := codexFlowStatus(t, srv, flow.ID)["status"]; got != codexLoginPending {
		t.Fatalf("status = %v, want the flow still %q", got, codexLoginPending)
	}
}

func TestWebCodexLoginReportsAFailedExchange(t *testing.T) {
	issuer := &fakeCodexIssuer{err: errors.New("issuer refused the code")}
	srv, manager := newCodexTestServer(t, issuer)
	flow, _ := startCodexLogin(t, srv, manager, `{"no_browser":true}`)

	status, _, raw := codexJSON(t, http.MethodPost,
		srv.URL+"/api/v1/llm/codex/login/"+flow.ID+"/code", `{"code":"bare-code"}`)
	if status != http.StatusBadRequest {
		t.Fatalf("code status = %d, want 400 (body %s)", status, raw)
	}
	payload := codexFlowStatus(t, srv, flow.ID)
	if payload["status"] != codexLoginError {
		t.Fatalf("status = %+v, want %q", payload, codexLoginError)
	}
	if hasCodexCredential() {
		t.Fatal("a failed exchange stored a credential")
	}
}

// An issuer that answers without a token has not signed anyone in, and storing
// that credential would leave the agent authenticating with an empty string.
func TestWebCodexLoginRejectsACredentialWithoutAToken(t *testing.T) {
	issuer := &fakeCodexIssuer{cred: &auth.AuthCredential{AccountID: "acct-1"}}
	srv, manager := newCodexTestServer(t, issuer)
	flow, _ := startCodexLogin(t, srv, manager, `{"no_browser":true}`)

	status, _, raw := codexJSON(t, http.MethodPost,
		srv.URL+"/api/v1/llm/codex/login/"+flow.ID+"/code", `{"code":"bare-code"}`)
	if status != http.StatusBadRequest {
		t.Fatalf("code status = %d, want 400 (body %s)", status, raw)
	}
	if hasCodexCredential() {
		t.Fatal("an empty access token was stored as a sign-in")
	}
}

func TestWebCodexLoginUnknownFlow(t *testing.T) {
	srv, _ := newCodexTestServer(t, codexSignedInIssuer())

	if status, _, raw := codexJSON(t, http.MethodGet, srv.URL+"/api/v1/llm/codex/login/nope", ""); status != http.StatusNotFound {
		t.Errorf("status of an unknown flow = %d, want 404 (body %s)", status, raw)
	}
	if status, _, raw := codexJSON(t, http.MethodPost, srv.URL+"/api/v1/llm/codex/login/nope/code", `{"code":"x"}`); status != http.StatusNotFound {
		t.Errorf("code for an unknown flow = %d, want 404 (body %s)", status, raw)
	}
	if status, _, raw := codexJSON(t, http.MethodDelete, srv.URL+"/api/v1/llm/codex/login/nope", ""); status != http.StatusNotFound {
		t.Errorf("cancel of an unknown flow = %d, want 404 (body %s)", status, raw)
	}
}

// Cancelling releases the callback port and stops the sign-in being addressable,
// which is what an operator who closed the browser tab needs.
func TestWebCodexLoginCancelReleasesTheFlow(t *testing.T) {
	srv, manager := newCodexTestServer(t, codexSignedInIssuer())
	flow, _ := startCodexLogin(t, srv, manager, "")
	addr := flow.listener.Addr().String()

	status, _, raw := codexJSON(t, http.MethodDelete, srv.URL+"/api/v1/llm/codex/login/"+flow.ID, "")
	if status != http.StatusOK {
		t.Fatalf("cancel status = %d, body %s", status, raw)
	}
	if _, ok := manager.get(flow.ID); ok {
		t.Fatal("the cancelled sign-in is still addressable")
	}
	if !flow.settled() {
		t.Fatal("the cancelled flow is still pending")
	}
	// The port must be free for the next attempt.
	l, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("the callback port was not released: %v", err)
	}
	l.Close()
}

func TestWebCodexStatusAndLogout(t *testing.T) {
	srv, _ := newCodexTestServer(t, codexSignedInIssuer())

	status, payload, raw := codexJSON(t, http.MethodGet, srv.URL+"/api/v1/llm/codex/status", "")
	if status != http.StatusOK {
		t.Fatalf("status = %d, body %s", status, raw)
	}
	if payload["signed_in"] != false {
		t.Fatalf("signed_in = %v on a clean machine", payload["signed_in"])
	}
	if payload["path"] != codexAuthFile() {
		t.Errorf("path = %v, want %q", payload["path"], codexAuthFile())
	}

	expires := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	writeTestCodexCredential(t, &codexCredential{
		AccessToken: "access-1",
		AccountID:   "acct-1",
		ExpiresAt:   expires,
	})
	_, payload, raw = codexJSON(t, http.MethodGet, srv.URL+"/api/v1/llm/codex/status", "")
	if payload["signed_in"] != true {
		t.Fatalf("signed_in = %v after a sign-in (body %s)", payload["signed_in"], raw)
	}
	if payload["account_id"] != "acct-1" {
		t.Errorf("account_id = %v", payload["account_id"])
	}
	if payload["expires_at"] != expires.Format(time.RFC3339) {
		t.Errorf("expires_at = %v, want %q", payload["expires_at"], expires.Format(time.RFC3339))
	}
	if payload["expired"] != false {
		t.Errorf("expired = %v for a token valid for another hour", payload["expired"])
	}

	status, payload, raw = codexJSON(t, http.MethodPost, srv.URL+"/api/v1/llm/codex/logout", "")
	if status != http.StatusOK {
		t.Fatalf("logout status = %d, body %s", status, raw)
	}
	if payload["signed_in"] != false {
		t.Fatalf("logout returned signed_in = %v", payload["signed_in"])
	}
	if hasCodexCredential() {
		t.Fatal("the credential survived the logout")
	}
	// Signing out of a machine that was never signed in is not an error.
	if status, _, raw := codexJSON(t, http.MethodPost, srv.URL+"/api/v1/llm/codex/logout", ""); status != http.StatusOK {
		t.Fatalf("second logout status = %d, body %s", status, raw)
	}
}

// An expired stored token is still a sign-in; the panel has to show it as one
// that needs renewing rather than as no sign-in at all.
func TestWebCodexStatusReportsAnExpiredToken(t *testing.T) {
	srv, _ := newCodexTestServer(t, codexSignedInIssuer())
	writeTestCodexCredential(t, &codexCredential{
		AccessToken: "access-1",
		ExpiresAt:   time.Now().Add(-time.Hour).UTC().Truncate(time.Second),
	})

	_, payload, raw := codexJSON(t, http.MethodGet, srv.URL+"/api/v1/llm/codex/status", "")
	if payload["signed_in"] != true || payload["expired"] != true {
		t.Fatalf("status = %s", raw)
	}
}

// pinCodexLoginTimings shrinks the sign-in deadlines for one test and restores
// them afterwards, so the expiry paths are exercised in milliseconds instead of
// minutes.
func pinCodexLoginTimings(t *testing.T, ttl, retention time.Duration) {
	t.Helper()
	oldTTL, oldRetention := codexLoginTTL, codexLoginRetention
	codexLoginTTL, codexLoginRetention = ttl, retention
	t.Cleanup(func() { codexLoginTTL, codexLoginRetention = oldTTL, oldRetention })
}

// waitForCodex polls cond until it holds or the budget runs out. The expiry it
// waits on is driven by a timer, so there is nothing to synchronise on.
func waitForCodex(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// A sign-in nobody finishes must expire on its own. The flow is what holds the
// callback port, so an operator who opened the panel and walked away would
// otherwise block every later attempt — including their own.
func TestWebCodexLoginExpiresAndReleasesThePort(t *testing.T) {
	// Retention is left long so the panel's poll lands inside the window where
	// the outcome is still addressable; retirement is covered by its own test.
	pinCodexLoginTimings(t, 50*time.Millisecond, 30*time.Second)
	srv, manager := newCodexTestServer(t, codexSignedInIssuer())
	flow, _ := startCodexLogin(t, srv, manager, "")
	addr := flow.listener.Addr().String()

	waitForCodex(t, "the sign-in to expire", flow.settled)

	// The panel gets the reason, not a bare disappearance.
	payload := codexFlowStatus(t, srv, flow.ID)
	if payload["status"] != codexLoginError {
		t.Fatalf("status = %v, want %q", payload["status"], codexLoginError)
	}
	if msg, _ := payload["error"].(string); !strings.Contains(msg, "not completed within") {
		t.Fatalf("error = %q, want the timeout reason", msg)
	}

	l, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("the callback port was not released on expiry: %v", err)
	}
	l.Close()

	if hasCodexCredential() {
		t.Fatal("an expired sign-in stored a credential")
	}
}

// A deadline short enough to fire while start is still assembling the flow must
// not race or panic. The TTL is armed last for exactly this reason, and the
// deadlines are tunable vars, so somebody will eventually pin one this short.
func TestWebCodexLoginSurvivesAnImmediateDeadline(t *testing.T) {
	pinCodexLoginTimings(t, time.Nanosecond, 30*time.Second)
	srv, manager := newCodexTestServer(t, codexSignedInIssuer())
	flow, _ := startCodexLogin(t, srv, manager, "")

	waitForCodex(t, "the immediate deadline to land", flow.settled)
	payload := codexFlowStatus(t, srv, flow.ID)
	if payload["status"] != codexLoginError {
		t.Fatalf("status = %v, want %q", payload["status"], codexLoginError)
	}
	if _, ok := manager.get(flow.ID); !ok {
		t.Fatal("the flow was retired before its retention window")
	}
}

// A finished sign-in stops being addressable once the retention window passes,
// so a completed flow id cannot be polled forever.
func TestWebCodexLoginRetiresAFinishedFlow(t *testing.T) {
	pinCodexLoginTimings(t, 30*time.Second, 40*time.Millisecond)
	srv, manager := newCodexTestServer(t, codexSignedInIssuer())
	flow, _ := startCodexLogin(t, srv, manager, "")

	res, err := http.Get(codexCallbackURL(flow, "auth-code-1", flow.state))
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	res.Body.Close()
	if !flow.settled() {
		t.Fatal("the redirect did not finish the sign-in")
	}

	waitForCodex(t, "the finished sign-in to be retired", func() bool {
		_, ok := manager.get(flow.ID)
		return !ok
	})
	status, _, _ := codexJSON(t, http.MethodGet, srv.URL+"/api/v1/llm/codex/login/"+flow.ID, "")
	if status != http.StatusNotFound {
		t.Fatalf("status of a retired flow = %d, want 404", status)
	}
}

// codexCallbackURL addresses the listener directly rather than through the
// "localhost" in RedirectURI, so the test does not depend on how that name
// resolves on the machine running it.
func codexCallbackURL(flow *codexLoginFlow, code, state string) string {
	q := url.Values{}
	if code != "" {
		q.Set("code", code)
	}
	q.Set("state", state)
	return "http://" + flow.listener.Addr().String() + "/auth/callback?" + q.Encode()
}
