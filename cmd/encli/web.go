package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/skrashevich/encx-cli/encx"
)

//go:embed webui/*
var webUIFiles embed.FS

const defaultWebAddr = "127.0.0.1:8787"

// RunChatTurnFn executes one agent turn for a chat.
type RunChatTurnFn func(ctx context.Context, hub *webHub, chatID string)

// webHub wires HTTP handlers to auth, chat storage, and SSE.
type webHub struct {
	cfg        *config
	registry   *AuthRegistry
	store      *ChatStore
	sse        *sseHub
	runTurn    RunChatTurnFn
	approvalMu sync.Mutex
	approvals  map[string]*approvalGate
}

func (h *webHub) publishSSE(chatID, eventType string, payload any) {
	h.sse.room(chatID).broadcast(formatSSE(eventType, payload))
}

func encOptsFromConfig(cfg *config) []encx.Option {
	var opts []encx.Option
	if cfg.insecure {
		opts = append(opts, encx.WithInsecureTLS())
	}
	if cfg.useHTTP {
		opts = append(opts, encx.WithHTTP())
	}
	if cfg.debug {
		opts = append(opts, encx.WithDebugLogger(debugf))
	}
	return opts
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func readJSONBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close()
	data, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "read body"})
		return false
	}
	if len(data) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "empty body"})
		return false
	}
	if err := json.Unmarshal(data, dst); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return false
	}
	return true
}

func (h *webHub) mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/chats", h.httpListChats)
	mux.HandleFunc("POST /api/v1/chats", h.httpCreateChat)
	mux.HandleFunc("GET /api/v1/chats/{id}", h.httpGetChat)
	mux.HandleFunc("PATCH /api/v1/chats/{id}", h.httpPatchChat)
	mux.HandleFunc("DELETE /api/v1/chats/{id}", h.httpDeleteChat)
	mux.HandleFunc("POST /api/v1/chats/{id}/messages", h.httpPostMessage)
	mux.HandleFunc("GET /api/v1/chats/{id}/events", h.httpSSE)
	mux.HandleFunc("POST /api/v1/chats/{id}/cancel", h.httpCancelChat)
	mux.HandleFunc("GET /api/v1/chats/{id}/approval", h.httpGetApproval)
	mux.HandleFunc("POST /api/v1/chats/{id}/approval", h.httpPostApproval)
	mux.HandleFunc("GET /api/v1/chats/{id}/export", h.httpExportChat)

	mux.HandleFunc("POST /api/v1/auth/login", h.httpAuthLogin)
	mux.HandleFunc("POST /api/v1/auth/logout", h.httpAuthLogout)
	mux.HandleFunc("GET /api/v1/auth/status", h.httpAuthStatus)
	mux.HandleFunc("GET /api/v1/catalog/domains", h.httpCatalogDomains)
	mux.HandleFunc("GET /api/v1/catalog/games", h.httpCatalogGames)
	mux.HandleFunc("GET /api/v1/agent/config", h.httpAgentConfig)

	sub, err := fs.Sub(webUIFiles, "webui")
	if err != nil {
		panic("webui embed: " + err.Error())
	}
	mux.Handle("/", http.FileServer(http.FS(sub)))
}

func (h *webHub) newMux() *http.ServeMux {
	mux := http.NewServeMux()
	h.mount(mux)
	return mux
}

func cmdWeb(ctx context.Context, cfg *config, addr string) error {
	if addr == "" {
		addr = defaultWebAddr
	}
	registry := NewAuthRegistry()
	store := NewChatStore()
	if err := store.LoadFromDisk(); err != nil {
		debugf("load chats from disk: %v", err)
	}
	url := "http://" + addr
	if err := openBrowser(url); err != nil {
		debugf("open browser: %v", err)
	}
	fmt.Fprintf(os.Stderr, "encli web UI at %s (Ctrl+C to stop)\n", url)
	return startWebServer(ctx, cfg, addr, registry, store)
}

func startWebServer(ctx context.Context, cfg *config, addr string, registry *AuthRegistry, store *ChatStore) error {
	if addr == "" {
		addr = defaultWebAddr
	}
	hub := &webHub{
		cfg:      cfg,
		registry: registry,
		store:    store,
		sse:      newSSEHub(),
		runTurn:  runWebChatTurn,
	}
	srv := &http.Server{
		Addr:    addr,
		Handler: hub.newMux(),
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return ctx.Err()
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

// openBrowser opens url in the system default browser where supported.
func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}

func (h *webHub) httpListChats(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	snaps := h.store.List()
	items := make([]chatListItem, 0, len(snaps))
	for _, s := range snaps {
		if q != "" && !chatMatchesQuery(s, q) {
			continue
		}
		items = append(items, chatListItem{
			ID:        s.ID,
			Title:     s.Title,
			Domain:    s.Domain,
			GameID:    s.GameID,
			UpdatedAt: s.UpdatedAt,
			Running:   s.Running,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"chats": items})
}

func chatMatchesQuery(s ChatSnapshot, q string) bool {
	q = strings.ToLower(q)
	if strings.Contains(strings.ToLower(s.Title), q) {
		return true
	}
	if strings.Contains(strings.ToLower(s.Domain), q) {
		return true
	}
	for _, m := range s.Messages {
		if strings.Contains(strings.ToLower(m.Content), q) {
			return true
		}
	}
	return false
}

type chatListItem struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Domain    string    `json:"domain"`
	GameID    int       `json:"game_id"`
	UpdatedAt time.Time `json:"updated_at"`
	Running   bool      `json:"running"`
}

type createChatRequest struct {
	Domain string `json:"domain"`
	GameID int    `json:"game_id"`
}

func (h *webHub) httpCreateChat(w http.ResponseWriter, r *http.Request) {
	var req createChatRequest
	if !readJSONBody(w, r, &req) {
		return
	}
	domain := req.Domain
	if domain == "" {
		domain = h.cfg.domain
	}
	defaultSec := h.cfg.agentSecurity.effective()
	snap := h.store.Create(domain, req.GameID, defaultSec)
	h.store.Persist(snap.ID)
	writeJSON(w, http.StatusCreated, snap)
}

func (h *webHub) httpGetChat(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	snap, ok := h.store.Get(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "chat not found"})
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

func (h *webHub) httpPatchChat(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	data, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	r.Body.Close()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "read body"})
		return
	}
	snap, ok := h.store.Update(id, data)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "chat not found"})
		return
	}
	h.store.Persist(id)
	writeJSON(w, http.StatusOK, snap)
}

func (h *webHub) httpDeleteChat(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ok := h.store.Delete(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "chat not found"})
		return
	}
	h.store.RemovePersisted(id)
	h.sse.removeChat(id)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

type postMessageRequest struct {
	Content string `json:"content"`
}

func (h *webHub) httpPostMessage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req postMessageRequest
	if !readJSONBody(w, r, &req) {
		return
	}
	if req.Content == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "content required"})
		return
	}
	msg, ok, busy := h.store.AppendUserMessageUnlessRunning(id, req.Content)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "chat not found"})
		return
	}
	if busy {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "agent run in progress"})
		return
	}

	agentCtx, cancel := context.WithCancel(context.Background())
	_ = h.store.WithRunning(id, true, cancel)

	run := h.runTurn
	if run == nil {
		run = runWebChatTurn
	}
	go func(chatID string) {
		defer func() {
			_ = h.store.WithRunning(chatID, false, nil)
			h.publishSSE(chatID, agentEventDone, map[string]any{"chat_id": chatID})
		}()
		run(agentCtx, h, chatID)
	}(id)

	h.store.Persist(id)
	writeJSON(w, http.StatusAccepted, map[string]any{"message": msg, "chat_id": id})
}

type approvalPostBody struct {
	Action string `json:"action"`
}

func (h *webHub) httpGetApproval(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	gate := h.approvalGate(id)
	if gate == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no pending approval"})
		return
	}
	pending, ok := h.store.PendingFixes(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no pending approval"})
		return
	}
	writeJSON(w, http.StatusOK, fixProposalPayload(nil, 1, len(pending), pending[0]))
}

func (h *webHub) httpPostApproval(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body approvalPostBody
	if !readJSONBody(w, r, &body) {
		return
	}
	action, err := parseApprovalAction(body.Action)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	gate := h.approvalGate(id)
	if gate == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no pending approval"})
		return
	}
	if err := gate.respond(action); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "action": string(action)})
}

func (h *webHub) httpExportChat(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	snap, ok := h.store.Get(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "chat not found"})
		return
	}
	format := strings.ToLower(r.URL.Query().Get("format"))
	if format == "" {
		format = "markdown"
	}
	switch format {
	case "json":
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="chat-`+id+`.json"`)
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(snap)
	case "markdown", "md":
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="chat-`+id+`.md"`)
		_, _ = w.Write([]byte(exportChatMarkdown(snap)))
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "format must be json or markdown"})
	}
}

func exportChatMarkdown(snap ChatSnapshot) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", snap.Title)
	fmt.Fprintf(&b, "- Domain: %s\n- Game ID: %d\n- Updated: %s\n\n", snap.Domain, snap.GameID, snap.UpdatedAt.Format(time.RFC3339))
	for _, m := range snap.Messages {
		role := string(m.Role)
		if m.ToolName != "" {
			role = "tool:" + m.ToolName
		}
		fmt.Fprintf(&b, "## %s\n\n%s\n\n", role, m.Content)
	}
	return b.String()
}

func (h *webHub) httpAgentConfig(w http.ResponseWriter, r *http.Request) {
	agentCfg, err := resolveAgentConfig()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"model":    "",
			"base_url": "",
			"error":    err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"model":    agentCfg.Model,
		"base_url": agentCfg.BaseURL,
	})
}

func (h *webHub) httpSSE(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, ok := h.store.Get(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "chat not found"})
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming unsupported"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	room := h.sse.room(id)
	ch := room.subscribe(32)
	defer room.unsubscribe(ch)

	done := r.Context().Done()
	for {
		select {
		case <-done:
			return
		case frame, ok := <-ch:
			if !ok {
				return
			}
			if _, err := w.Write(frame); err != nil {
				return
			}
			fl.Flush()
		}
	}
}

func (h *webHub) httpCancelChat(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	h.store.mu.Lock()
	t, ok := h.store.chats[id]
	var cancel func()
	if ok && t.cancel != nil {
		cancel = t.cancel
	}
	h.store.mu.Unlock()
	if cancel == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "not running"})
		return
	}
	cancel()
	writeJSON(w, http.StatusOK, map[string]string{"status": "canceled"})
}

type authLoginBody struct {
	Domain   string `json:"domain"`
	Login    string `json:"login"`
	Password string `json:"password"`
}

func (h *webHub) httpAuthLogin(w http.ResponseWriter, r *http.Request) {
	var body authLoginBody
	if !readJSONBody(w, r, &body) {
		return
	}
	if body.Domain == "" || body.Login == "" || body.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "domain, login, password required"})
		return
	}
	opts := encOptsFromConfig(h.cfg)
	err := h.registry.Login(r.Context(), body.Domain, body.Login, body.Password, opts)
	if err != nil {
		var le EncxLoginError
		if errors.As(err, &le) {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": le.Error(), "code": le.Code})
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success":      true,
		"session_file": sessionFile(&config{domain: body.Domain}),
	})
}

type authLogoutBody struct {
	Domain string `json:"domain"`
}

func (h *webHub) httpAuthLogout(w http.ResponseWriter, r *http.Request) {
	var body authLogoutBody
	if !readJSONBody(w, r, &body) {
		return
	}
	if body.Domain == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "domain required"})
		return
	}
	h.registry.Logout(body.Domain)
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (h *webHub) httpCatalogDomains(w http.ResponseWriter, r *http.Request) {
	domains := fetchAuthorizedDomains(h.registry)
	items := make([]map[string]string, len(domains))
	for i, d := range domains {
		items[i] = map[string]string{"domain": d}
	}
	writeJSON(w, http.StatusOK, map[string]any{"domains": items})
}

func (h *webHub) httpCatalogGames(w http.ResponseWriter, r *http.Request) {
	domain := strings.TrimSpace(r.URL.Query().Get("domain"))
	if domain == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "domain query required"})
		return
	}
	games, err := fetchWebGames(r.Context(), h.registry, h.cfg, domain)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"games": games})
}

func (h *webHub) httpAuthStatus(w http.ResponseWriter, r *http.Request) {
	opts := encOptsFromConfig(h.cfg)
	stats := h.registry.ListStatus()
	domains := make([]authStatusItem, len(stats))
	for i, s := range stats {
		login := s.Login
		if s.HasSession && login == "" {
			login = h.registry.ResolveLogin(r.Context(), s.Domain, opts)
		}
		domains[i] = authStatusItem{
			Domain:      s.Domain,
			Login:       login,
			LoggedIn:    s.HasSession,
			HasSession:  s.HasSession,
			SessionPath: s.SessionPath,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"domains": domains})
}

type authStatusItem struct {
	Domain      string `json:"domain"`
	Login       string `json:"login,omitempty"`
	LoggedIn    bool   `json:"logged_in"`
	HasSession  bool   `json:"has_session"`
	SessionPath string `json:"session_path,omitempty"`
}
