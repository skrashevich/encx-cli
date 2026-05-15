package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWebChatCRUD(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg := &config{}
	reg := NewAuthRegistry()
	store := NewChatStore()
	hub := &webHub{
		cfg:      cfg,
		registry: reg,
		store:    store,
		sse:      newSSEHub(),
		runTurn:  func(ctx context.Context, h *webHub, chatID string) {
			select {
			case <-time.After(20 * time.Millisecond):
			case <-ctx.Done():
			}
		},
	}
	srv := httptest.NewServer(hub.newMux())
	t.Cleanup(srv.Close)

	// create
	body := `{"domain":"tech.en.cx","game_id":7}`
	res, err := http.Post(srv.URL+"/api/v1/chats", "application/json", stringsReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create status %d", res.StatusCode)
	}
	var created ChatSnapshot
	if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Domain != "tech.en.cx" || created.GameID != 7 {
		t.Fatalf("unexpected create payload: %+v", created)
	}

	// list
	res, err = http.Get(srv.URL + "/api/v1/chats")
	if err != nil {
		t.Fatal(err)
	}
	var listWrap struct {
		Chats []chatListItem `json:"chats"`
	}
	if err := json.NewDecoder(res.Body).Decode(&listWrap); err != nil {
		res.Body.Close()
		t.Fatal(err)
	}
	res.Body.Close()
	if len(listWrap.Chats) != 1 || listWrap.Chats[0].ID != created.ID {
		t.Fatalf("list: %+v", listWrap.Chats)
	}

	// get
	res, err = http.Get(srv.URL + "/api/v1/chats/" + created.ID)
	if err != nil {
		t.Fatal(err)
	}
	var got ChatSnapshot
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		res.Body.Close()
		t.Fatal(err)
	}
	res.Body.Close()
	if got.ID != created.ID {
		t.Fatalf("get: %+v", got)
	}

	// patch
	patch := `{"title":"x","game_id":9}`
	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/api/v1/chats/"+created.ID, stringsReader(patch))
	req.Header.Set("Content-Type", "application/json")
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		res.Body.Close()
		t.Fatal(err)
	}
	res.Body.Close()
	if got.Title != "x" || got.GameID != 9 {
		t.Fatalf("patch: %+v", got)
	}

	// delete
	req, _ = http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/chats/"+created.ID, nil)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("delete status %d", res.StatusCode)
	}

	res, err = http.Get(srv.URL + "/api/v1/chats/" + created.ID)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("get after delete: %d", res.StatusCode)
	}
}

func stringsReader(s string) io.Reader {
	return bytes.NewBufferString(s)
}

func TestWebAuthStatus(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := &config{}
	reg := NewAuthRegistry()
	hub := &webHub{cfg: cfg, registry: reg, store: NewChatStore(), sse: newSSEHub()}
	srv := httptest.NewServer(hub.newMux())
	t.Cleanup(srv.Close)

	res, err := http.Get(srv.URL + "/api/v1/auth/status")
	if err != nil {
		t.Fatal(err)
	}
	var emptyWrap struct {
		Domains []authStatusItem `json:"domains"`
	}
	if err := json.NewDecoder(res.Body).Decode(&emptyWrap); err != nil {
		res.Body.Close()
		t.Fatal(err)
	}
	res.Body.Close()
	if len(emptyWrap.Domains) != 0 {
		t.Fatalf("expected no domains: %+v", emptyWrap.Domains)
	}

	domain := "stat.en.cx"
	if err := os.MkdirAll(sessionDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	path := sessionFile(&config{domain: domain})
	if err := os.WriteFile(path, []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}
	saveSessionMeta(&config{domain: domain, login: "tester"})

	res, err = http.Get(srv.URL + "/api/v1/auth/status")
	if err != nil {
		t.Fatal(err)
	}
	var statsWrap struct {
		Domains []authStatusItem `json:"domains"`
	}
	if err := json.NewDecoder(res.Body).Decode(&statsWrap); err != nil {
		res.Body.Close()
		t.Fatal(err)
	}
	res.Body.Close()
	found := false
	for _, s := range statsWrap.Domains {
		if s.Domain == domain && s.LoggedIn {
			found = true
			if s.SessionPath != path {
				t.Fatalf("path %q != %q", s.SessionPath, path)
			}
			if s.Login != "tester" {
				t.Fatalf("login %q, want tester", s.Login)
			}
		}
	}
	if !found {
		t.Fatalf("domain not in status: %+v", statsWrap.Domains)
	}

	// logout removes session file for domain
	logBody := `{"domain":` + jsonQuote(domain) + `}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/auth/logout", stringsReader(logBody))
	req.Header.Set("Content-Type", "application/json")
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("logout %d", res.StatusCode)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("session file should be removed: %v", err)
	}
}

func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestWebPostMessageAndConflict(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := &config{}
	started := make(chan string, 1)
	done := make(chan struct{})
	customTurn := func(ctx context.Context, h *webHub, chatID string) {
		started <- chatID
		<-ctx.Done()
		close(done)
	}
	hub := &webHub{
		cfg:      cfg,
		registry: NewAuthRegistry(),
		store:    NewChatStore(),
		sse:      newSSEHub(),
		runTurn:  customTurn,
	}
	srv := httptest.NewServer(hub.newMux())
	t.Cleanup(srv.Close)

	res, err := http.Post(srv.URL+"/api/v1/chats", "application/json", stringsReader(`{"domain":"d","game_id":1}`))
	if err != nil {
		t.Fatal(err)
	}
	var snap ChatSnapshot
	_ = json.NewDecoder(res.Body).Decode(&snap)
	res.Body.Close()
	id := snap.ID

	res, err = http.Post(srv.URL+"/api/v1/chats/"+id+"/messages", "application/json", stringsReader(`{"content":"hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("post message %d", res.StatusCode)
	}

	select {
	case got := <-started:
		if got != id {
			t.Fatalf("wrong id %s", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("agent did not start")
	}

	res, err = http.Post(srv.URL+"/api/v1/chats/"+id+"/messages", "application/json", stringsReader(`{"content":"again"}`))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 got %d", res.StatusCode)
	}

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/chats/"+id+"/cancel", nil)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("cancel %d", res.StatusCode)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("agent did not stop")
	}

	snap2, ok := hub.store.Get(id)
	if !ok || snap2.Running {
		t.Fatalf("expected not running: %+v ok=%v", snap2, ok)
	}
}

func TestWebCatalogDomainsEmpty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	hub := &webHub{cfg: &config{}, registry: NewAuthRegistry(), store: NewChatStore(), sse: newSSEHub()}
	srv := httptest.NewServer(hub.newMux())
	t.Cleanup(srv.Close)

	res, err := http.Get(srv.URL + "/api/v1/catalog/domains")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var wrap struct {
		Domains []map[string]string `json:"domains"`
	}
	if err := json.NewDecoder(res.Body).Decode(&wrap); err != nil {
		t.Fatal(err)
	}
	if len(wrap.Domains) != 0 {
		t.Fatalf("expected empty: %+v", wrap.Domains)
	}
}

func TestWebStaticRoot(t *testing.T) {
	hub := &webHub{cfg: &config{}, registry: NewAuthRegistry(), store: NewChatStore(), sse: newSSEHub()}
	srv := httptest.NewServer(hub.newMux())
	t.Cleanup(srv.Close)

	res, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d", res.StatusCode)
	}
	b, _ := io.ReadAll(res.Body)
	if !bytes.Contains(b, []byte("encli")) || !bytes.Contains(b, []byte("чат")) {
		t.Fatalf("unexpected body: %s", b)
	}
}

func TestWebAgentConfig(t *testing.T) {
	t.Setenv("LLM_MODEL", "test/model-xyz")
	t.Setenv("LLM_API_KEY", "sk-test")
	hub := &webHub{cfg: &config{}, registry: NewAuthRegistry(), store: NewChatStore(), sse: newSSEHub()}
	srv := httptest.NewServer(hub.newMux())
	t.Cleanup(srv.Close)

	res, err := http.Get(srv.URL + "/api/v1/agent/config")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var got map[string]string
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got["model"] != "test/model-xyz" {
		t.Fatalf("model: %+v", got)
	}
}

func TestAuthRegistryListStatusFilenameDomain(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	_ = os.MkdirAll(sessionDir(), 0o700)
	name := "weird_domain.io"
	path := filepath.Join(sessionDir(), name+".json")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	reg := NewAuthRegistry()
	stats := reg.ListStatus()
	found := false
	for _, s := range stats {
		if s.Domain == name {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("glob domain missing: %+v", stats)
	}
}
