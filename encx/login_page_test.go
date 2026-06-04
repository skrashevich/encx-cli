package encx

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseLoginPageForm(t *testing.T) {
	page := "https://tech.en.cx/Login.aspx?return=%2fhome%2f"
	html := []byte(`<form id="formMain" method="post" action="/Login.aspx?return=%2fhome%2f">
	<input name="Login" type="text"/>
	<input name="Password" type="password"/>
	<select name="ddlNetwork"><option value="1" selected="selected">Encounter</option></select>
	</form>`)
	form, err := parseLoginPageForm(page, html)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(form.PostURL, "/Login.aspx?return=%2fhome%2f") {
		t.Fatalf("post URL: %s", form.PostURL)
	}
	if form.Network != "1" {
		t.Fatalf("network: %s", form.Network)
	}
}

func TestIsLoginCheckCookieRedirect(t *testing.T) {
	if !isLoginCheckCookieRedirect("/Login.aspx?return=%2f&checkcookie=1") {
		t.Fatal("expected checkcookie redirect")
	}
	if isLoginFailureRedirect("/Login.aspx?return=%2f&checkcookie=1") {
		t.Fatal("checkcookie must not be a failure redirect")
	}
	if !isLoginFailureRedirect("/Login.aspx?return=%2f") {
		t.Fatal("plain login redirect should be failure")
	}
}

func TestLoginViaLoginPageCheckCookie(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/Login.aspx" && r.URL.Query().Get("checkcookie") == "1":
			http.Redirect(w, r, "/home/", http.StatusFound)
		case r.Method == http.MethodGet && r.URL.Path == "/Login.aspx":
			_, _ = fmt.Fprintf(w, `<form id="formMain" method="post" action="/Login.aspx?return=%%2f">
				<input name="Login"/><input name="Password"/></form>`)
		case r.Method == http.MethodPost && r.URL.Path == "/Login.aspx":
			http.Redirect(w, r, "/Login.aspx?return=%2f&checkcookie=1", http.StatusFound)
		case r.URL.Path == "/home/" || r.URL.Path == "/":
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	host := srv.Listener.Addr().String()
	client := New(host, WithHTTP())
	err := client.LoginViaLoginPage(context.Background(), "http://"+host+"/Login.aspx?return=%2f", "user", "pass")
	if err != nil {
		t.Fatalf("LoginViaLoginPage: %v", err)
	}
}

func TestLoginViaLoginPageSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/Login.aspx":
			w.Header().Set("Content-Type", "text/html")
			_, _ = fmt.Fprintf(w, `<form id="formMain" method="post" action="/Login.aspx?return=%%2fhome%%2f">
				<input name="Login"/><input name="Password"/>
				<select name="ddlNetwork"><option value="1" selected>Encounter</option></select></form>`)
		case r.Method == http.MethodPost && r.URL.Path == "/Login.aspx":
			if r.FormValue("Login") != "user" || r.FormValue("Password") != "pass" {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`<span id="lblDBMessage">Incorrect login or password</span><input name="Login"/>`))
				return
			}
			http.Redirect(w, r, "/home/", http.StatusFound)
		case r.Method == http.MethodGet && r.URL.Path == "/home/":
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	host := srv.Listener.Addr().String()
	client := New(host, WithHTTP())
	err := client.LoginViaLoginPage(context.Background(), "http://"+host+"/Login.aspx?return=%2fhome%2f", "user", "pass")
	if err != nil {
		t.Fatalf("LoginViaLoginPage: %v", err)
	}
}
