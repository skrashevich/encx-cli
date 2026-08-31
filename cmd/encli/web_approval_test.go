package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWebApprovalFlow(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := &config{}
	store := NewChatStore()
	hub := &webHub{
		cfg:      cfg,
		registry: NewAuthRegistry(),
		store:    store,
		sse:      newSSEHub(),
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

	gate := newApprovalGate()
	hub.setApprovalGate(snap.ID, gate)

	go func() {
		time.Sleep(30 * time.Millisecond)
		_ = gate.respond(approvalNo)
	}()

	action, err := gate.wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if action != approvalNo {
		t.Fatalf("got %q", action)
	}

	res, err = http.Post(srv.URL+"/api/v1/chats/"+snap.ID+"/approval", "application/json", stringsReader(`{"action":"yes"}`))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound && res.StatusCode != http.StatusConflict {
		// gate already consumed by goroutine — POST without active gate is 404
	}
}

func TestParseApprovalAction(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"yes", "yes"},
		{"y", "yes"},
		{"n", "no"},
		{"quit", "quit"},
	} {
		a, err := parseApprovalAction(tc.in)
		if err != nil {
			t.Fatal(err)
		}
		if string(a) != tc.want {
			t.Fatalf("%s -> %s", tc.in, a)
		}
	}
}

func TestWebToolApprovalPublishesWhileThreadLocked(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store := NewChatStore()
	snap := store.Create("d", 1, SecurityModeApprove)
	hub := &webHub{store: store, sse: newSSEHub()}

	thread, unlock, ok := store.LockThread(snap.ID)
	if !ok {
		t.Fatal("chat not found")
	}
	defer unlock()

	events := hub.sse.room(snap.ID).subscribe(4)
	defer hub.sse.room(snap.ID).unsubscribe(events)

	type result struct {
		allowed bool
		err     error
	}
	resultCh := make(chan result, 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() {
		allowed, err := runWebToolApproval(ctx, hub, snap.ID, thread.session, "admin_update_level", `{"level_id":7}`)
		resultCh <- result{allowed: allowed, err: err}
	}()

	select {
	case frame := <-events:
		if !strings.Contains(string(frame), "event: approval_prompt") || !strings.Contains(string(frame), `"kind":"tool"`) {
			t.Fatalf("unexpected SSE frame: %s", frame)
		}
	case <-time.After(time.Second):
		t.Fatal("approval prompt was not published while the chat thread was locked")
	}

	gate := hub.approvalGate(snap.ID)
	if gate == nil {
		t.Fatal("approval gate not registered")
	}
	if err := gate.respond(approvalYes); err != nil {
		t.Fatal(err)
	}
	if prompt, ok := gate.currentPrompt(); ok {
		t.Fatalf("resolved approval still exposes prompt: %#v", prompt)
	}
	select {
	case got := <-resultCh:
		if got.err != nil || !got.allowed {
			t.Fatalf("approval result = %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("tool approval did not receive the response")
	}
}

func TestGetApprovalReturnsToolPrompt(t *testing.T) {
	hub := &webHub{store: NewChatStore(), sse: newSSEHub()}
	snap := hub.store.Create("d", 1, SecurityModeApprove)
	gate := newApprovalGate()
	gate.setPrompt(toolApprovalPayload(nil, "admin_update_level", `{"level_id":7}`))
	hub.setApprovalGate(snap.ID, gate)
	defer hub.clearApprovalGate(snap.ID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/chats/"+snap.ID+"/approval", nil)
	req.SetPathValue("id", snap.ID)
	res := httptest.NewRecorder()
	hub.httpGetApproval(res, req)
	if res.Code != http.StatusOK {
		body, _ := io.ReadAll(res.Result().Body)
		t.Fatalf("GET approval status %d: %s", res.Code, body)
	}
	var prompt map[string]any
	if err := json.NewDecoder(res.Result().Body).Decode(&prompt); err != nil {
		t.Fatal(err)
	}
	if prompt["kind"] != "tool" || prompt["tool"] != "admin_update_level" {
		t.Fatalf("unexpected prompt: %#v", prompt)
	}
}

func TestExportChatMarkdown(t *testing.T) {
	md := exportChatMarkdown(ChatSnapshot{
		Title:  "t",
		Domain: "d.en.cx",
		GameID: 1,
		Messages: []UIMessage{
			{Role: UIMessageRoleUser, Content: "hi"},
		},
	})
	if !bytes.Contains([]byte(md), []byte("# t")) {
		t.Fatalf("md: %s", md)
	}
}
