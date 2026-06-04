package encx

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGuardHTTPLoginRedirect(t *testing.T) {
	err := guardHTTPLoginRedirect(http.StatusFound, http.Header{"Location": {"/Login.aspx?return=%2f"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "redirect to login") {
		t.Fatalf("expected login redirect error, got %v", err)
	}
}

func TestDoGetRejectsAdminLoginRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/Login.aspx?return=%2fAdministration%2f", http.StatusFound)
	}))
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "http://")
	client := New(host, WithHTTP())
	_, err := client.doGet(context.Background(), "http://"+host+"/Administration/Games/LevelManager.aspx")
	if err == nil || !strings.Contains(err.Error(), "redirect to login") {
		t.Fatalf("expected login redirect error, got %v", err)
	}
}

func TestLoginCompleteUsesLoginPage(t *testing.T) {
	var formPosts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/Login.aspx" && r.URL.Query().Get("checkcookie") == "1":
			http.Redirect(w, r, "/home/", http.StatusFound)
		case r.Method == http.MethodGet && r.URL.Path == "/Login.aspx":
			_, _ = fmt.Fprintf(w, `<form id="formMain" method="post" action="/Login.aspx?return=%%2f">
				<input name="Login"><input name="Password"></form>`)
		case r.Method == http.MethodPost && r.URL.Path == "/Login.aspx":
			formPosts++
			http.Redirect(w, r, "/Login.aspx?return=%2f&checkcookie=1", http.StatusFound)
		case r.URL.Path == "/home/" || r.URL.Path == "/":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		case strings.Contains(r.URL.Path, "LevelManager.aspx"):
			_, _ = fmt.Fprintf(w, `<input name="txtLevelName_1" value="L1">`)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "http://")
	client := New(host, WithHTTP())
	if err := client.LoginComplete(context.Background(), "user", "pass"); err != nil {
		t.Fatalf("LoginComplete: %v", err)
	}
	if formPosts != 1 {
		t.Fatalf("expected 1 login form POST, got %d", formPosts)
	}
}
