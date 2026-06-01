package encx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEnterGame_MakeGameFee(t *testing.T) {
	t.Parallel()

	const gameID = 42
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/gameengines/encounter/makefee/Login.aspx":
			http.NotFound(w, r)
		case r.Method == http.MethodGet && r.URL.Path == "/MakeGameFee.aspx":
			q := r.URL.Query()
			if q.Get("gid") != "42" || q.Get("confirm") != "yes" || q.Get("lang") != "" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte("<html><body>Team accepted to the game.</body></html>"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "http://")
	client := New(host, WithHTTP())

	body, err := client.EnterGame(context.Background(), gameID)
	if err != nil {
		t.Fatalf("EnterGame: %v", err)
	}
	if !strings.Contains(body, "Team accepted") {
		t.Fatalf("unexpected body: %q", body)
	}
}

func TestEnterGame_EngineFallback(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/MakeGameFee.aspx":
			http.NotFound(w, r)
		case r.Method == http.MethodPost && r.URL.Path == "/gameengines/encounter/makefee/Login.aspx":
			_ = r.ParseForm()
			if r.Form.Get("gid") != "7" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte("legacy ok"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "http://")
	client := New(host, WithHTTP())

	body, err := client.EnterGame(context.Background(), 7)
	if err != nil {
		t.Fatalf("EnterGame: %v", err)
	}
	if body != "legacy ok" {
		t.Fatalf("body = %q", body)
	}
}

func TestEnterGame_LoginRedirect(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/MakeGameFee.aspx" {
			http.Redirect(w, r, "/Login.aspx?return=%2fMakeGameFee.aspx", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "http://")
	client := New(host, WithHTTP())

	_, err := client.EnterGame(context.Background(), 1)
	if err == nil || !strings.Contains(err.Error(), "login") {
		t.Fatalf("expected login redirect error, got %v", err)
	}
}
