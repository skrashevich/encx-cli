package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestWebPatchRunningChatDoesNotBlockStore(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store := NewChatStore()
	chat := store.Create("test.en.cx", 82902, SecurityModeApprove)
	canceled := false
	store.WithRunning(chat.ID, true, func() { canceled = true })
	_, unlock, ok := store.LockThread(chat.ID)
	if !ok {
		t.Fatal("chat missing")
	}
	release := sync.OnceFunc(unlock)
	defer release()
	hub := &webHub{store: store, sse: newSSEHub()}
	mux := hub.newMux()
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodPatch,
			"/api/v1/chats/"+chat.ID, stringsReader(`{"security_mode":"full","game_id":7,"title":"changed"}`)))
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		release()
		<-done
		t.Fatal("settings update blocked behind the running agent")
	}
	if response.Code != http.StatusConflict {
		t.Fatalf("patch status = %d, want 409: %s", response.Code, response.Body.String())
	}
	// An active turn must still be able to save, and the UI must be able to
	// read and cancel it while the agent holds the thread lock.
	store.Persist(chat.ID)
	got, ok := store.Get(chat.ID)
	if !ok || got.SecurityMode != SecurityModeApprove || got.GameID != 82902 || got.Title != chat.Title {
		t.Fatalf("rejected patch changed chat: %+v", got)
	}
	if len(store.List()) != 1 {
		t.Fatal("chat list unavailable")
	}
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/chats/"+chat.ID+"/cancel", nil))
	if response.Code != http.StatusOK || !canceled {
		t.Fatalf("cancel failed: status=%d canceled=%v", response.Code, canceled)
	}
	release()
	store.WithRunning(chat.ID, false, nil)
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPatch,
		"/api/v1/chats/"+chat.ID, stringsReader(`{"security_mode":"full"}`)))
	if response.Code != http.StatusOK {
		t.Fatalf("idle patch status = %d", response.Code)
	}
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.SecurityMode != SecurityModeFull {
		t.Fatalf("idle mode = %s", got.SecurityMode)
	}
	reloaded := NewChatStore()
	if err := reloaded.LoadFromDisk(); err != nil {
		t.Fatal(err)
	}
	if got, ok := reloaded.Get(chat.ID); !ok || got.SecurityMode != SecurityModeFull {
		t.Fatalf("mode was not persisted: %+v", got)
	}
}
