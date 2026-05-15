package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestChatPersistRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	store := NewChatStore()
	snap := store.Create("tech.en.cx", 42, SecurityModeFull)
	id := snap.ID
	_, ok, _ := store.AppendUserMessageUnlessRunning(id, "проверка персистентности")
	if !ok {
		t.Fatal("append failed")
	}
	store.Persist(id)

	store2 := NewChatStore()
	if err := store2.LoadFromDisk(); err != nil {
		t.Fatal(err)
	}
	got, ok := store2.Get(id)
	if !ok {
		t.Fatal("not loaded")
	}
	if got.Domain != "tech.en.cx" || got.GameID != 42 {
		t.Fatalf("meta: %+v", got)
	}
	if len(got.Messages) != 1 || got.Messages[0].Content != "проверка персистентности" {
		t.Fatalf("messages: %+v", got.Messages)
	}
	path := filepath.Join(webChatsDir(), id+".json")
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}

	store2.Delete(id)
	store2.RemovePersisted(id)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("file should be gone: %v", err)
	}
}

func TestAutoTitleFromMessage(t *testing.T) {
	got := autoTitleFromMessage("tech.en.cx", "tech.en.cx", "Создай три уровня про космос")
	if got == "tech.en.cx" || got == "" {
		t.Fatalf("expected derived title, got %q", got)
	}
}

func TestChatMatchesQuery(t *testing.T) {
	s := ChatSnapshot{
		Title: "mars quest",
		Messages: []UIMessage{{Content: "orbit"}},
	}
	if !chatMatchesQuery(s, "orbit") {
		t.Fatal("should match message body")
	}
	if chatMatchesQuery(s, "venus") {
		t.Fatal("should not match")
	}
}
