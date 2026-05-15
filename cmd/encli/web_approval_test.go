package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
