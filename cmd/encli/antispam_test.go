package main

import (
	"testing"
)

func TestAntiSpamCredentials(t *testing.T) {
	cfg := &config{domain: "tech.en.cx", login: "user", password: "secret"}
	login, pass, ok := antiSpamCredentials(cfg)
	if !ok || login != "user" || pass != "secret" {
		t.Fatalf("got login=%q pass=%q ok=%v", login, pass, ok)
	}

	cfg = &config{domain: "tech.en.cx", login: "user"}
	if _, _, ok := antiSpamCredentials(cfg); ok {
		t.Fatal("expected missing password")
	}

	cfg = &config{domain: "no-such-domain.example", password: "secret"}
	if _, _, ok := antiSpamCredentials(cfg); ok {
		t.Fatal("expected missing login")
	}
}
