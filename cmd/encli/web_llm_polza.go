package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

const polzaBaseURL = "https://polza.ai/api/v1"

// Provider endpoints are private implementation details, never browser inputs.
type polzaManager struct {
	mu                                            sync.Mutex
	flows                                         map[string]*polzaFlow
	client                                        *http.Client
	authorizeURL, tokenURL, balanceURL, modelsURL string
	ttl, retention                                time.Duration
	persist                                       func(llmSettings) error
}

type polzaFlow struct {
	mu                                                    sync.Mutex
	id, state, verifier, model, authorizeURL, redirectURI string
	status, message                                       string
	claimed                                               bool
	ctx                                                   context.Context
	cancel                                                context.CancelFunc
	listener                                              net.Listener
	timer                                                 *time.Timer
}

func newPolzaManager() *polzaManager {
	return &polzaManager{flows: make(map[string]*polzaFlow), client: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, authorizeURL: "https://polza.ai/api/auth/authorize", tokenURL: "https://polza.ai/api/auth/token", balanceURL: "https://polza.ai/api/v2/balance", modelsURL: polzaBaseURL + "/models", ttl: 5 * time.Minute, retention: 2 * time.Minute, persist: saveLLMSettings}
}
func (h *webHub) polzaFlows() *polzaManager {
	h.polzaMu.Lock()
	defer h.polzaMu.Unlock()
	if h.polza == nil {
		h.polza = newPolzaManager()
	}
	return h.polza
}
func polzaModel(model string) (string, error) {
	model = strings.TrimSpace(model)
	if len(model) == 0 || len(model) > 200 || strings.ContainsAny(model, "\r\n\t ") {
		return "", errors.New("select a Polza model")
	}
	return model, nil
}
func polzaEndpoint(endpoint string) bool { return strings.TrimSuffix(endpoint, "/") == polzaBaseURL }
func polzaRandom() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
func (f *polzaFlow) snapshot() map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]any{"id": f.id, "status": f.status, "authorize_url": f.authorizeURL, "redirect_uri": f.redirectURI}
	if f.message != "" {
		out["error"] = f.message
	}
	return out
}

// Caller holds f.mu. Cancellation and persistence share this lock so cancelled
// flows cannot later overwrite the operator's settings.
func (m *polzaManager) finishLocked(f *polzaFlow, err error) {
	if f.status != "pending" {
		return
	}
	f.status = "success"
	if err != nil {
		f.status = "error"
		f.message = err.Error()
	}
	f.verifier = ""
	f.cancel()
	_ = f.listener.Close()
	if f.timer != nil {
		f.timer.Stop()
	}
	time.AfterFunc(m.retention, func() { m.mu.Lock(); delete(m.flows, f.id); m.mu.Unlock() })
}
func (m *polzaManager) start(model string) (*polzaFlow, error) {
	model, err := polzaModel(model)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.flows) >= 32 {
		return nil, errors.New("too many Polza sign-ins; wait for previous attempts to expire")
	}
	for _, previous := range m.flows {
		previous.mu.Lock()
		m.finishLocked(previous, errors.New("Polza sign-in replaced by a newer attempt"))
		previous.mu.Unlock()
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, errors.New("cannot open local Polza callback listener")
	}
	ctx, cancel := context.WithTimeout(context.Background(), m.ttl)
	f := &polzaFlow{id: polzaRandom(), state: polzaRandom(), verifier: polzaRandom(), model: model, status: "pending", ctx: ctx, cancel: cancel, listener: listener}
	f.redirectURI = "http://" + listener.Addr().String() + "/auth/polza/callback"
	challenge := sha256.Sum256([]byte(f.verifier))
	u, _ := url.Parse(m.authorizeURL)
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("callback_url", f.redirectURI)
	q.Set("code_challenge", base64.RawURLEncoding.EncodeToString(challenge[:]))
	q.Set("code_challenge_method", "S256")
	q.Set("state", f.state)
	q.Set("app_name", "encli")
	u.RawQuery = q.Encode()
	f.authorizeURL = u.String()
	m.flows[f.id] = f
	f.mu.Lock()
	f.timer = time.AfterFunc(m.ttl, func() { f.mu.Lock(); defer f.mu.Unlock(); m.finishLocked(f, errors.New("Polza sign-in timed out")) })
	f.mu.Unlock()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /auth/polza/callback", func(w http.ResponseWriter, r *http.Request) {
		err := m.submit(f, f.redirectURI+"?"+r.URL.RawQuery)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, "Вход выполнен. Вернитесь в encli и проверьте баланс.")
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 5 * time.Second}
	go func() { _ = server.Serve(listener) }()
	return f, nil
}
func (m *polzaManager) get(id string) *polzaFlow {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.flows[id]
}
func (m *polzaManager) submit(f *polzaFlow, input string) error {
	u, err := url.Parse(strings.TrimSpace(input))
	expected, _ := url.Parse(f.redirectURI)
	if err != nil || u.Scheme != expected.Scheme || u.Host != expected.Host || u.Path != expected.Path || u.User != nil || u.Fragment != "" {
		return errors.New("paste the full Polza callback URL")
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(q["state"]) != 1 || q.Get("state") != f.state {
		return errors.New("Polza callback state mismatch")
	}
	if q.Get("error") != "" {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.status != "pending" || f.claimed {
			return errors.New("Polza sign-in already used")
		}
		err := errors.New("Polza sign-in was declined")
		m.finishLocked(f, err)
		return err
	}
	if len(q["code"]) != 1 || q.Get("code") == "" || len(q.Get("code")) > 8192 {
		return errors.New("Polza callback has no valid authorization code")
	}
	f.mu.Lock()
	if f.status != "pending" || f.claimed {
		f.mu.Unlock()
		return errors.New("Polza sign-in already used")
	}
	f.claimed = true
	verifier := f.verifier
	f.mu.Unlock()
	var token struct {
		Key string `json:"key"`
	}
	err = m.request(f.ctx, http.MethodPost, m.tokenURL, "", map[string]string{"grant_type": "authorization_code", "code": q.Get("code"), "code_verifier": verifier, "callback_url": f.redirectURI}, &token)
	if err == nil && strings.TrimSpace(token.Key) == "" {
		err = errors.New("Polza returned no API key")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.status != "pending" || f.ctx.Err() != nil {
		return errors.New("Polza sign-in cancelled or expired")
	}
	if err == nil {
		if m.persist(llmSettings{AuthMethod: authMethodAPIKey, BaseURL: polzaBaseURL, APIKey: token.Key, Model: f.model}) != nil {
			err = errors.New("cannot save Polza settings")
		}
	}
	m.finishLocked(f, err)
	return err
}
func (m *polzaManager) request(ctx context.Context, method, endpoint, key string, payload, out any) error {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return errors.New("cannot encode Polza request")
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return errors.New("cannot create Polza request")
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	// Even injected clients cannot follow redirects with credentials or a code.
	client := *m.client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := client.Do(req)
	if err != nil {
		return errors.New("Polza request failed; check connectivity and try again")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("Polza request rejected (HTTP %d)", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return errors.New("cannot read Polza response")
	}
	if json.Unmarshal(data, out) != nil {
		return errors.New("invalid Polza response")
	}
	return nil
}

type polzaBalance struct {
	Amount    string `json:"amount"`
	Available string `json:"available"`
}

func (m *polzaManager) balance(ctx context.Context, key string) (polzaBalance, error) {
	var b polzaBalance
	if strings.TrimSpace(key) == "" {
		return b, errors.New("Polza API key required")
	}
	err := m.request(ctx, http.MethodGet, m.balanceURL, key, nil, &b)
	if err == nil && (!polzaDecimal(b.Amount) || !polzaDecimal(b.Available)) {
		err = errors.New("invalid Polza balance response")
	}
	return b, err
}
func polzaError(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}
func (h *webHub) httpPolzaLoginStart(w http.ResponseWriter, r *http.Request) {
	if err := h.polzaSetupAllowed(); err != nil {
		polzaError(w, 409, err)
		return
	}
	var req struct {
		Model string `json:"model"`
	}
	if !readJSONBody(w, r, &req) {
		return
	}
	f, err := h.polzaFlows().start(req.Model)
	if err != nil {
		polzaError(w, 400, err)
		return
	}
	writeJSON(w, 201, f.snapshot())
}
func (h *webHub) httpPolzaLoginStatus(w http.ResponseWriter, r *http.Request) {
	f := h.polzaFlows().get(r.PathValue("id"))
	if f == nil {
		polzaError(w, 404, errors.New("no such Polza sign-in"))
		return
	}
	writeJSON(w, 200, f.snapshot())
}
func (h *webHub) httpPolzaLoginCode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if !readJSONBody(w, r, &req) {
		return
	}
	m := h.polzaFlows()
	f := m.get(r.PathValue("id"))
	if f == nil {
		polzaError(w, 404, errors.New("no such Polza sign-in"))
		return
	}
	if err := m.submit(f, req.Code); err != nil {
		polzaError(w, 400, err)
		return
	}
	writeJSON(w, 200, f.snapshot())
}
func (h *webHub) httpPolzaLoginCancel(w http.ResponseWriter, r *http.Request) {
	m := h.polzaFlows()
	f := m.get(r.PathValue("id"))
	if f == nil {
		polzaError(w, 404, errors.New("no such Polza sign-in"))
		return
	}
	f.mu.Lock()
	m.finishLocked(f, errors.New("Polza sign-in cancelled"))
	f.mu.Unlock()
	writeJSON(w, 200, map[string]string{"status": "cancelled"})
}
func (h *webHub) httpPolzaConnect(w http.ResponseWriter, r *http.Request) {
	if err := h.polzaSetupAllowed(); err != nil {
		polzaError(w, 409, err)
		return
	}
	var req struct {
		APIKey string `json:"api_key"`
		Model  string `json:"model"`
	}
	if !readJSONBody(w, r, &req) {
		return
	}
	model, err := polzaModel(req.Model)
	if err != nil {
		polzaError(w, 400, err)
		return
	}
	key := strings.TrimSpace(req.APIKey)
	if key == "" {
		stored, err := loadLLMSettings()
		if err != nil || !polzaEndpoint(stored.BaseURL) || stored.AuthMethod != authMethodAPIKey {
			polzaError(w, 400, errors.New("enter a Polza API key; another provider's key cannot be reused"))
			return
		}
		key = stored.APIKey
	}
	m := h.polzaFlows()
	b, err := m.balance(r.Context(), key)
	if err != nil {
		polzaError(w, 502, err)
		return
	}
	if m.persist(llmSettings{AuthMethod: authMethodAPIKey, BaseURL: polzaBaseURL, APIKey: key, Model: model}) != nil {
		polzaError(w, 500, errors.New("cannot save Polza settings"))
		return
	}
	writeJSON(w, 200, map[string]any{"connected": true, "amount": b.Amount, "available": b.Available, "model": model})
}
func (h *webHub) httpPolzaCheck(w http.ResponseWriter, r *http.Request) {
	if _, keyEnv := firstEnv(llmAPIKeyEnvVars); keyEnv != "" {
		endpoint, _ := firstEnv(llmBaseURLEnvVars)
		if !polzaEndpoint(endpoint) {
			polzaError(w, 409, fmt.Errorf("%s overrides the saved key; explicitly configure LLM_BASE_URL for Polza before checking this environment key", keyEnv))
			return
		}
	}
	cfg, err := resolveAgentConfig(h.cfg)
	if err != nil || !polzaEndpoint(cfg.BaseURL) || (cfg.AuthMethod != "" && cfg.AuthMethod != authMethodAPIKey) {
		polzaError(w, 409, errors.New("effective LLM configuration is not Polza; check LLM_AUTH, LLM_BASE_URL, OPENROUTER_BASE_URL and command-line overrides"))
		return
	}
	b, err := h.polzaFlows().balance(r.Context(), cfg.APIKey)
	if err != nil {
		polzaError(w, 502, err)
		return
	}
	out := map[string]any{"connected": true, "amount": b.Amount, "available": b.Available, "model": cfg.Model}
	if _, name := firstEnv(llmAPIKeyEnvVars); name != "" {
		out["warning"] = "Using API key from " + name + " instead of saved settings"
	}
	writeJSON(w, 200, out)
}
func polzaDecimal(value string) bool {
	if value == "" || len(value) > 100 {
		return false
	}
	for _, c := range value {
		if (c < '0' || c > '9') && c != '.' && c != '-' {
			return false
		}
	}
	_, ok := new(big.Rat).SetString(value)
	return ok
}
func (m *polzaManager) close() {
	m.mu.Lock()
	flows := make([]*polzaFlow, 0, len(m.flows))
	for _, f := range m.flows {
		flows = append(flows, f)
	}
	m.mu.Unlock()
	for _, f := range flows {
		f.mu.Lock()
		m.finishLocked(f, errors.New("Polza sign-in cancelled because server stopped"))
		f.mu.Unlock()
	}
}
func (h *webHub) closePolza() {
	h.polzaMu.Lock()
	m := h.polza
	h.polzaMu.Unlock()
	if m != nil {
		m.close()
	}
}
func (h *webHub) httpPolzaModels(w http.ResponseWriter, r *http.Request) {
	var catalog struct {
		Data []struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Type        string `json:"type"`
			TopProvider struct {
				SupportedParameters []string `json:"supported_parameters"`
			} `json:"top_provider"`
		} `json:"data"`
	}
	if err := h.polzaFlows().request(r.Context(), http.MethodGet, h.polzaFlows().modelsURL, "", nil, &catalog); err != nil {
		polzaError(w, 502, err)
		return
	}
	models := make([]map[string]string, 0)
	def := ""
	for _, m := range catalog.Data {
		if m.Type == "chat" && slices.Contains(m.TopProvider.SupportedParameters, "tools") {
			models = append(models, map[string]string{"id": m.ID, "name": m.Name})
			if m.ID == "deepseek/deepseek-v4-flash-0731" {
				def = m.ID
			}
		}
	}
	if len(models) > 0 && def == "" {
		def = models[0]["id"]
	}
	writeJSON(w, 200, map[string]any{"models": models, "default_model": def})
}

// A web preset cannot take effect while ambient configuration shadows it. In
// particular an old environment key must never be paired with a newly saved
// provider endpoint and sent to that provider.
func (h *webHub) polzaSetupAllowed() error {
	if h.cfg != nil && strings.TrimSpace(h.cfg.llmAuth) != "" {
		return errors.New("command-line LLM auth overrides settings; remove it before connecting Polza")
	}
	for _, names := range [][]string{llmAuthEnvVars, llmAPIKeyEnvVars, llmBaseURLEnvVars, llmModelEnvVars} {
		if _, name := firstEnv(names); name != "" {
			return fmt.Errorf("%s overrides saved LLM settings; remove the override and restart encli before connecting Polza", name)
		}
	}
	return nil
}
