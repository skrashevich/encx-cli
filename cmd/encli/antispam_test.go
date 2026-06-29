package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/skrashevich/encx-cli/encx"
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

func TestHandleAntiSpamDoesNotReportAutoSignInWhenCredentialsMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/NotHumanRequest.aspx":
			_, _ = w.Write([]byte(`<html><a href="/Login.aspx?return=/ALoader/LevelInfo.aspx?gid=82443&level=10&object=3&sector=3496478">Войти</a></html>`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	host := srv.Listener.Addr().String()
	client := encx.New(host, encx.WithHTTP())
	cfg := &config{domain: host}
	guard := newAntiSpamGuard()

	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdinR.Close()
	if _, err := stdinW.Write([]byte("\n")); err != nil {
		t.Fatal(err)
	}
	_ = stdinW.Close()

	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stderrR.Close()

	oldStdin := os.Stdin
	oldStderr := os.Stderr
	os.Stdin = stdinR
	os.Stderr = stderrW
	defer func() {
		os.Stdin = oldStdin
		os.Stderr = oldStderr
	}()

	err = handleAntiSpam(t.Context(), cfg, client, guard, srv.URL+"/NotHumanRequest.aspx")
	_ = stderrW.Close()
	var stderr bytes.Buffer
	_, _ = io.Copy(&stderr, stderrR)
	if err != nil {
		t.Fatalf("handleAntiSpam: %v", err)
	}
	out := stderr.String()
	if strings.Contains(out, "Automatic sign-in did not clear anti-spam") {
		t.Fatalf("unexpected auto sign-in failure message: %s", out)
	}
	if !strings.Contains(out, "Automatic sign-in skipped:") {
		t.Fatalf("expected auto sign-in skipped message: %s", out)
	}
	if strings.Contains(out, "§or") {
		t.Fatalf("sector parameter was decoded as HTML entity: %s", out)
	}
}
