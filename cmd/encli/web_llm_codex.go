package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/sipeed/picoclaw/pkg/auth"
)

// The browser sign-in PicoClaw ships (auth.LoginBrowserWithOptions) prints to
// stdout and blocks on stdin, so -web cannot use it: it drives the same OAuth
// flow from the exported primitives instead, and reports progress over HTTP.

// codexLoginTTL bounds one sign-in. It matches the timeout of PicoClaw's own
// browser flow, and it is what releases the callback port when an operator
// opens the panel and walks away.
const codexLoginTTL = 5 * time.Minute

// codexLoginRetention keeps a finished flow around long enough for the panel to
// poll its outcome, then drops it so a completed sign-in stops being addressable.
const codexLoginRetention = 2 * time.Minute

const (
	codexLoginPending = "pending"
	codexLoginSuccess = "success"
	codexLoginError   = "error"
)

// codexLoginFlow is one sign-in attempt: the PKCE material it started from, the
// callback listener waiting for the redirect, and the outcome the panel polls.
type codexLoginFlow struct {
	ID           string `json:"id"`
	AuthorizeURL string `json:"authorize_url"`
	RedirectURI  string `json:"redirect_uri"`

	verifier  string
	state     string
	createdAt time.Time

	mu        sync.Mutex
	status    string
	errMsg    string
	accountID string

	// stop closes the callback listener exactly once, whichever of the redirect,
	// the manual paste or the timeout finishes the flow first.
	stop sync.Once
	// server is nil when the flow was started without a local listener.
	server        *http.Server
	listener      net.Listener
	cancelTimeout func()
}

func (f *codexLoginFlow) snapshot() map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]any{
		"id":            f.ID,
		"status":        f.status,
		"authorize_url": f.AuthorizeURL,
		"redirect_uri":  f.RedirectURI,
	}
	if f.errMsg != "" {
		out["error"] = f.errMsg
	}
	if f.accountID != "" {
		out["account_id"] = f.accountID
	}
	return out
}

func (f *codexLoginFlow) settled() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status != codexLoginPending
}

// finish records the first outcome and releases the callback port. Later
// outcomes are dropped: a redirect that arrives after a manual paste already
// completed the sign-in must not overwrite it with a state-mismatch error.
func (f *codexLoginFlow) finish(accountID string, err error) {
	f.mu.Lock()
	if f.status == codexLoginPending {
		if err != nil {
			f.status, f.errMsg = codexLoginError, err.Error()
		} else {
			f.status, f.accountID = codexLoginSuccess, accountID
		}
	}
	f.mu.Unlock()
	f.release()
}

func (f *codexLoginFlow) release() {
	f.stop.Do(func() {
		if f.cancelTimeout != nil {
			f.cancelTimeout()
		}
		// The listener is closed here and now: the port is the scarce resource,
		// and the next sign-in must be able to bind it as soon as this one ends.
		if f.listener != nil {
			_ = f.listener.Close()
		}
		if f.server == nil {
			return
		}
		// Draining runs in the background because release is reached from inside
		// the callback handler itself, and Shutdown waits for handlers to return:
		// calling it here would stall every successful sign-in for the whole
		// shutdown timeout before the browser saw its "signed in" page.
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = f.server.Shutdown(ctx)
		}()
	})
}

// codexLoginManager owns the flows in flight.
//
// oauthConfig, exchange and persist are fields rather than direct calls so the
// HTTP surface can be tested end to end without an OAuth issuer: a test swaps in
// an exchange that returns a credential and a persist that records it.
type codexLoginManager struct {
	mu    sync.Mutex
	flows map[string]*codexLoginFlow

	oauthConfig func() auth.OAuthProviderConfig
	exchange    func(cfg auth.OAuthProviderConfig, code, verifier, redirectURI string) (*auth.AuthCredential, error)
	persist     func(*codexCredential) error
	// listen opens the callback port. A test overrides it to take an ephemeral
	// one, since the registered redirect port may well be busy on a dev machine.
	listen func(port int) (net.Listener, int, error)
}

func newCodexLoginManager() *codexLoginManager {
	return &codexLoginManager{
		flows:       make(map[string]*codexLoginFlow),
		oauthConfig: auth.OpenAIOAuthConfig,
		exchange:    auth.ExchangeCodeForTokens,
		persist:     saveCodexCredential,
		listen:      listenOnPort,
	}
}

// listenOnPort binds the OAuth callback port.
//
// The port is not negotiable: OpenAI validates the redirect URI against the one
// registered for the Codex client, so falling back to another port would only
// trade a clear error here for an opaque rejection at the issuer.
func listenOnPort(port int) (net.Listener, int, error) {
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return nil, 0, err
	}
	addr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		_ = l.Close()
		return nil, 0, fmt.Errorf("unexpected listener address type %T", l.Addr())
	}
	return l, addr.Port, nil
}

// codexFlows lazily builds the manager, so a webHub assembled as a struct
// literal (as the tests do) still serves the sign-in endpoints.
func (h *webHub) codexFlows() *codexLoginManager {
	h.codexMu.Lock()
	defer h.codexMu.Unlock()
	if h.codexLogin == nil {
		h.codexLogin = newCodexLoginManager()
	}
	return h.codexLogin
}

// start opens a sign-in. With a listener the operator only has to complete the
// browser prompt; without one (noLocalServer, or a busy port) they paste the
// redirect URL back into the panel.
func (m *codexLoginManager) start(noLocalServer bool) (*codexLoginFlow, error) {
	cfg := m.oauthConfig()
	pkce, err := auth.GeneratePKCE()
	if err != nil {
		return nil, fmt.Errorf("generating PKCE: %w", err)
	}
	state, err := auth.GenerateState()
	if err != nil {
		return nil, fmt.Errorf("generating state: %w", err)
	}
	id, err := newFlowID()
	if err != nil {
		return nil, err
	}

	flow := &codexLoginFlow{
		ID:        id,
		verifier:  pkce.CodeVerifier,
		state:     state,
		status:    codexLoginPending,
		createdAt: time.Now(),
	}

	port := cfg.Port
	if !noLocalServer {
		listener, actual, err := m.listen(cfg.Port)
		if err != nil {
			return nil, fmt.Errorf(
				"the ChatGPT sign-in needs port %d for the redirect, and it is busy (%w). "+
					"Close whatever holds it — another encli sign-in or the Codex CLI — or use the manual paste option",
				cfg.Port, err)
		}
		port = actual
		flow.listener = listener
		flow.server = &http.Server{Handler: m.callbackHandler(flow)}
		go func() {
			_ = flow.server.Serve(listener)
		}()
	}

	flow.RedirectURI = fmt.Sprintf("http://localhost:%d/auth/callback", port)
	flow.AuthorizeURL = auth.BuildAuthorizeURL(cfg, pkce, state, flow.RedirectURI)

	timer := time.AfterFunc(codexLoginTTL, func() {
		flow.finish("", fmt.Errorf("the ChatGPT sign-in was not completed within %s", codexLoginTTL))
		m.forget(flow.ID)
	})
	flow.cancelTimeout = func() { timer.Stop() }

	m.mu.Lock()
	m.sweepLocked()
	m.flows[flow.ID] = flow
	m.mu.Unlock()
	return flow, nil
}

func (m *codexLoginManager) callbackHandler(flow *codexLoginFlow) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("state") != flow.state {
			flow.finish("", errors.New("the sign-in redirect carried the wrong state"))
			http.Error(w, "State mismatch", http.StatusBadRequest)
			return
		}
		code := q.Get("code")
		if code == "" {
			msg := strings.TrimSpace(q.Get("error"))
			if msg == "" {
				msg = "no authorization code"
			}
			flow.finish("", fmt.Errorf("the sign-in redirect carried no code: %s", msg))
			http.Error(w, "No authorization code received", http.StatusBadRequest)
			return
		}
		err := m.complete(flow, code)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			fmt.Fprintf(w, "<html><body><h2>Вход не удался</h2><p>%s</p></body></html>", htmlEscape(err.Error()))
			return
		}
		fmt.Fprint(w, "<html><body><h2>Вход выполнен</h2><p>Можно закрыть эту вкладку и вернуться в encli.</p></body></html>")
	})
	return mux
}

// complete exchanges the authorization code and stores the credential.
func (m *codexLoginManager) complete(flow *codexLoginFlow, code string) error {
	if flow.settled() {
		return errors.New("this sign-in has already finished")
	}
	cred, err := m.exchange(m.oauthConfig(), code, flow.verifier, flow.RedirectURI)
	if err != nil {
		flow.finish("", err)
		return err
	}
	if cred == nil || strings.TrimSpace(cred.AccessToken) == "" {
		err := errors.New("the ChatGPT issuer returned no access token")
		flow.finish("", err)
		return err
	}
	if err := m.persist(codexCredentialFromAuth(cred)); err != nil {
		flow.finish("", err)
		return err
	}
	// A fresh sign-in must not keep serving the token cached for the old one.
	resetCodexTokenStores()
	flow.finish(cred.AccountID, nil)
	return nil
}

// submitCode finishes a flow from a pasted redirect URL or bare code, which is
// the only path available when the browser cannot reach this machine.
func (m *codexLoginManager) submitCode(id, input string) (*codexLoginFlow, error) {
	flow, ok := m.get(id)
	if !ok {
		return nil, errors.New("no such sign-in")
	}
	code, err := codeFromPastedInput(input, flow.state)
	if err != nil {
		return flow, err
	}
	return flow, m.complete(flow, code)
}

// codeFromPastedInput accepts either the whole redirect URL or the code alone.
// A pasted URL still has to carry the flow's state: it is the same CSRF check
// the listener makes, and skipping it here would leave a way around it.
func codeFromPastedInput(input, state string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", errors.New("paste the redirect URL or the code")
	}
	if !strings.Contains(input, "?") {
		return input, nil
	}
	u, err := url.Parse(input)
	if err != nil {
		return "", fmt.Errorf("that is not a redirect URL: %w", err)
	}
	q := u.Query()
	if got := q.Get("state"); got != "" && got != state {
		return "", errors.New("the pasted URL belongs to a different sign-in")
	}
	code := strings.TrimSpace(q.Get("code"))
	if code == "" {
		if msg := strings.TrimSpace(q.Get("error")); msg != "" {
			return "", fmt.Errorf("the sign-in was refused: %s", msg)
		}
		return "", errors.New("no authorization code in the pasted URL")
	}
	return code, nil
}

func (m *codexLoginManager) get(id string) (*codexLoginFlow, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, ok := m.flows[id]
	return f, ok
}

func (m *codexLoginManager) forget(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.flows, id)
}

// sweepLocked drops flows nothing will poll again. The caller holds m.mu.
func (m *codexLoginManager) sweepLocked() {
	now := time.Now()
	for id, f := range m.flows {
		age := now.Sub(f.createdAt)
		if age > codexLoginTTL+codexLoginRetention || (f.settled() && age > codexLoginRetention) {
			f.release()
			delete(m.flows, id)
		}
	}
}

func newFlowID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generating a sign-in id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

func htmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}

// --- HTTP handlers ---

type codexLoginRequest struct {
	// NoBrowser skips the local callback listener, for an operator whose browser
	// runs on another machine and who will paste the redirect back.
	NoBrowser bool `json:"no_browser"`
}

func (h *webHub) httpCodexLoginStart(w http.ResponseWriter, r *http.Request) {
	var req codexLoginRequest
	// The body is optional: a plain POST starts the browser flow.
	if r.ContentLength > 0 && !readJSONBody(w, r, &req) {
		return
	}
	flow, err := h.codexFlows().start(req.NoBrowser)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, flow.snapshot())
}

func (h *webHub) httpCodexLoginStatus(w http.ResponseWriter, r *http.Request) {
	flow, ok := h.codexFlows().get(r.PathValue("id"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such sign-in"})
		return
	}
	writeJSON(w, http.StatusOK, flow.snapshot())
}

type codexLoginCodeRequest struct {
	Code string `json:"code"`
}

func (h *webHub) httpCodexLoginCode(w http.ResponseWriter, r *http.Request) {
	var req codexLoginCodeRequest
	if !readJSONBody(w, r, &req) {
		return
	}
	flow, err := h.codexFlows().submitCode(r.PathValue("id"), req.Code)
	if flow == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, flow.snapshot())
}

func (h *webHub) httpCodexLoginCancel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	flows := h.codexFlows()
	flow, ok := flows.get(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such sign-in"})
		return
	}
	flow.finish("", errors.New("the sign-in was cancelled"))
	flows.forget(id)
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

func (h *webHub) httpCodexStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, codexStatusForWeb())
}

func (h *webHub) httpCodexLogout(w http.ResponseWriter, r *http.Request) {
	if err := deleteCodexCredential(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	resetCodexTokenStores()
	writeJSON(w, http.StatusOK, codexStatusForWeb())
}
