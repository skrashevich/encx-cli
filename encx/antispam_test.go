package encx

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestGuardAntiSpamRedirect(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusFound,
		Header:     http.Header{"Location": {"/NotHumanRequest.aspx?return=%2f"}},
	}
	err := guardAntiSpam("tech.en.cx", "https", resp, nil)
	if !IsAntiSpam(err) {
		t.Fatalf("expected anti-spam error, got %v", err)
	}
	url := AntiSpamURLFromError(err)
	if !strings.Contains(url, "tech.en.cx/NotHumanRequest.aspx") {
		t.Fatalf("unexpected URL %q", url)
	}
	if !strings.Contains(url, "return=%2f") {
		t.Fatalf("expected return param in URL %q", url)
	}
}

func TestGuardAntiSpamBody(t *testing.T) {
	resp := &http.Response{StatusCode: http.StatusOK}
	body := []byte(`<html><form action="/NotHumanRequest.aspx"></form></html>`)
	err := guardAntiSpam("world.en.cx", "https", resp, body)
	if !IsAntiSpam(err) {
		t.Fatalf("expected anti-spam error, got %v", err)
	}
}

func TestGuardAntiSpamNoMatch(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusFound,
		Header:     http.Header{"Location": {"/login/signin"}},
	}
	if err := guardAntiSpam("tech.en.cx", "https", resp, []byte(`{"ok":true}`)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAntiSpamErrorIs(t *testing.T) {
	err := &AntiSpamError{URL: "https://tech.en.cx/NotHumanRequest.aspx"}
	if !errors.Is(err, ErrAntiSpam) {
		t.Fatal("AntiSpamError should match ErrAntiSpam")
	}
}

func TestEnsureJSONBodyAntiSpam(t *testing.T) {
	c := &Client{domain: "tech.en.cx", scheme: "https"}
	body := []byte(`<html><a href="/NotHumanRequest.aspx?return=%2f">here</a></html>`)
	err := c.ensureJSONBody(body)
	if !IsAntiSpam(err) {
		t.Fatalf("expected anti-spam, got %v", err)
	}
}

func TestDecodeJSONEmptyBody(t *testing.T) {
	c := &Client{domain: "tech.en.cx", scheme: "https"}
	var out map[string]any
	err := c.decodeJSON(nil, &out, "game list")
	if err == nil {
		t.Fatal("expected error for empty body")
	}
	if !strings.Contains(err.Error(), "empty response") {
		t.Fatalf("expected empty response error, got %v", err)
	}
}

func TestExtractLoginURLFromNotHumanHTML(t *testing.T) {
	page := "https://tech.en.cx/NotHumanRequest.aspx?return=%2fhome%2f"
	body := []byte(`<html><body>
		<p>Please verify</p>
		<a href="/Login.aspx?return=%2fhome%2f">Войти</a>
		</body></html>`)
	got := ExtractLoginURLFromNotHumanHTML(page, body)
	want := "https://tech.en.cx/Login.aspx?return=%2fhome%2f"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestExtractLoginURLFromNotHumanHTMLKeepsSectorParam(t *testing.T) {
	page := "https://tech.en.cx/NotHumanRequest.aspx?return=%2fALoader%2fLevelInfo.aspx%3fgid%3d82443%26level%3d10%26object%3d3%26sector%3d3496478"
	body := []byte(`<html><body>
		<a href="/Login.aspx?return=/ALoader/LevelInfo.aspx?gid=82443&level=10&object=3&sector=3496478">Войти</a>
		</body></html>`)
	got := ExtractLoginURLFromNotHumanHTML(page, body)
	want := "https://tech.en.cx/Login.aspx?return=/ALoader/LevelInfo.aspx?gid=82443&level=10&object=3&sector=3496478"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if strings.Contains(got, "§or") {
		t.Fatalf("sector parameter was decoded as HTML entity: %q", got)
	}
}

func TestAntiSpamPageURL(t *testing.T) {
	got := AntiSpamPageURL("tech.en.cx", "https", "/home/")
	if !strings.HasPrefix(got, "https://tech.en.cx/NotHumanRequest.aspx") {
		t.Fatalf("unexpected URL %q", got)
	}
	if !strings.Contains(got, "return=%2Fhome%2F") && !strings.Contains(got, "return=%2fhome%2f") {
		t.Fatalf("expected encoded return path in %q", got)
	}
}
