package encx

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newEngineClient wires a Client that talks to srv as both the site and the
// API host, with the new engine forced on.
func newEngineClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	t.Setenv(EngineEnvVar, "legacy")
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")
	return New(host, WithHTTP(), WithAdminDelay(0), WithAPIBaseURL(srv.URL), WithEngine(EngineNew))
}

func TestNewEngineLoginStoresToken(t *testing.T) {
	c := newEngineClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/login" {
			t.Errorf("path = %q, want /login", r.URL.Path)
		}
		http.SetCookie(w, &http.Cookie{Name: "en_access", Value: "jwt-cookie", Path: "/"})
		_, _ = w.Write([]byte(`{"token":"jwt-token","message":"welcome","session_class":"automation"}`))
	})

	resp, err := c.Login(context.Background(), "user", "pass")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if resp.Error != 0 {
		t.Errorf("Error = %d, want 0", resp.Error)
	}
	if resp.Message != "welcome" {
		t.Errorf("Message = %q, want welcome", resp.Message)
	}
	if got := c.APIToken(); got != "jwt-token" {
		t.Errorf("APIToken = %q, want jwt-token", got)
	}
}

func TestNewEngineLoginMapsFailuresToLegacyCodes(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		body      string
		wantError int
		wantURL   func(*LoginResponse) *string
	}{
		{
			name: "invalid credentials", status: http.StatusUnauthorized,
			body: `{"error":"invalid credentials"}`, wantError: 2,
		},
		{
			name: "captcha required", status: http.StatusUnauthorized,
			body:      `{"error":"captcha required","captcha_token":"tok-1","captcha_url":"https://api.en.cx/captcha/tok-1"}`,
			wantError: 1,
			wantURL:   func(r *LoginResponse) *string { return r.CaptchaUrl },
		},
		{
			name: "email not confirmed", status: http.StatusForbidden,
			body:      `{"error":"email not confirmed","confirm_email_url":"https://tech.en.cx/confirm"}`,
			wantError: 10,
			wantURL:   func(r *LoginResponse) *string { return r.ConfirmEmailUrl },
		},
		{
			name: "brute force", status: http.StatusForbidden,
			body:      `{"error":"brute force detected","brute_force_unblock_url":"https://tech.en.cx/unblock"}`,
			wantError: 9,
			wantURL:   func(r *LoginResponse) *string { return r.BruteForceUnblockUrl },
		},
		{
			name: "ip release", status: http.StatusForbidden,
			body:      `{"error":"ip block","ip_unblock_url":"https://tech.en.cx/ip"}`,
			wantError: 4,
			wantURL:   func(r *LoginResponse) *string { return r.IpUnblockUrl },
		},
		{
			name: "plain text forbidden", status: http.StatusForbidden,
			body: `access denied`, wantError: 7,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newEngineClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})

			resp, err := c.Login(context.Background(), "encx_probe_nonexistent", "x")
			if err != nil {
				t.Fatalf("Login returned a transport error: %v", err)
			}
			if resp.Error != tc.wantError {
				t.Errorf("Error = %d (%s), want %d (%s)",
					resp.Error, LoginErrorText(resp.Error), tc.wantError, LoginErrorText(tc.wantError))
			}
			if resp.Message == "" {
				t.Error("Message is empty; callers show it to the user")
			}
			if tc.wantURL != nil {
				if got := tc.wantURL(resp); got == nil || *got == "" {
					t.Error("recovery URL from the API was dropped")
				}
			}
		})
	}
}

func TestNewEngineLoginRetriesWithCaptchaToken(t *testing.T) {
	var second loginRequest
	calls := 0
	c := newEngineClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"captcha required","captcha_token":"tok-42"}`))
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&second)
		_, _ = w.Write([]byte(`{"token":"jwt"}`))
	})

	if _, err := c.Login(context.Background(), "user", "pass"); err != nil {
		t.Fatalf("first Login: %v", err)
	}
	if _, err := c.Login(context.Background(), "user", "pass", LoginOptions{MagicNumbers: "1234"}); err != nil {
		t.Fatalf("second Login: %v", err)
	}
	if second.CaptchaCode != "1234" {
		t.Errorf("captcha_code = %q, want 1234", second.CaptchaCode)
	}
	if second.CaptchaToken != "tok-42" {
		t.Errorf("captcha_token = %q, want tok-42 from the challenge", second.CaptchaToken)
	}
}

func TestNewEngineLoginCompleteVerifiesSession(t *testing.T) {
	var paths []string
	c := newEngineClient(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/login":
			_, _ = w.Write([]byte(`{"token":"jwt"}`))
		case "/auth/session":
			_, _ = w.Write([]byte(`{"user_id":1,"login":"user"}`))
		}
	})

	if err := c.LoginComplete(context.Background(), "user", "pass"); err != nil {
		t.Fatalf("LoginComplete: %v", err)
	}
	want := []string{"/login", "/auth/session"}
	if len(paths) != len(want) || paths[0] != want[0] || paths[1] != want[1] {
		t.Errorf("paths = %v, want %v", paths, want)
	}
}

func TestNewEngineLoginCompleteRejectsBadCredentials(t *testing.T) {
	c := newEngineClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid credentials"}`))
	})

	err := c.LoginComplete(context.Background(), "encx_probe_nonexistent", "x")
	if err == nil {
		t.Fatal("LoginComplete accepted invalid credentials")
	}
	if !strings.Contains(err.Error(), LoginErrorText(2)) {
		t.Errorf("error = %v, want it to mention %q", err, LoginErrorText(2))
	}
}

func TestNewEngineVerifyAdminSessionReportsExpiry(t *testing.T) {
	c := newEngineClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized","message":"Authorization required","code":401}`))
	})

	err := c.VerifyAdminSession(context.Background())
	if err == nil {
		t.Fatal("VerifyAdminSession accepted an unauthorized session")
	}
	if !strings.Contains(err.Error(), "session expired") {
		t.Errorf("error = %v, want it to name an expired session", err)
	}
}

func TestSessionRoundTripCarriesAPIToken(t *testing.T) {
	c := newEngineClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "en_access", Value: "cookie-jwt", Path: "/"})
		_, _ = w.Write([]byte(`{"token":"jwt-token"}`))
	})
	if _, err := c.Login(context.Background(), "user", "pass"); err != nil {
		t.Fatalf("Login: %v", err)
	}

	data, err := c.ExportCookies()
	if err != nil {
		t.Fatalf("ExportCookies: %v", err)
	}
	if !strings.Contains(string(data), "jwt-token") {
		t.Fatalf("export dropped the token: %s", data)
	}

	restored := New(c.domain, WithHTTP(), WithAPIBaseURL(c.APIBaseURL()), WithEngine(EngineNew))
	if err := restored.ImportCookies(data); err != nil {
		t.Fatalf("ImportCookies: %v", err)
	}
	if got := restored.APIToken(); got != "jwt-token" {
		t.Errorf("restored APIToken = %q, want jwt-token", got)
	}
}

func TestLegacySessionExportStaysACookieArray(t *testing.T) {
	t.Setenv(EngineEnvVar, "legacy")
	c := New("tech.en.cx")
	data, err := c.ExportCookies()
	if err != nil {
		t.Fatalf("ExportCookies: %v", err)
	}
	if len(data) == 0 || data[0] != '[' {
		t.Fatalf("legacy export = %s, want a JSON array for backward compatibility", data)
	}

	restored := New("tech.en.cx")
	if err := restored.ImportCookies(data); err != nil {
		t.Fatalf("ImportCookies on the legacy format: %v", err)
	}
}

// TestNewEngineLoginAsksForACookielessSession pins behaviour a live run on
// demo.en.cx exposed: signing in inside the browser's session class evicts the
// user's own browser session ("session_superseded"), so encx asks for a
// cookie-less session of its own class instead.
func TestNewEngineLoginAsksForACookielessSession(t *testing.T) {
	var body loginRequest
	var clientClass string
	c := newEngineClient(t, func(w http.ResponseWriter, r *http.Request) {
		clientClass = r.Header.Get(clientClassHeader)
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"token":"jwt","session_class":"mobile"}`))
	})

	if _, err := c.Login(context.Background(), "user", "pass"); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if !body.NoCookie {
		t.Error("no_cookie = false; the sign-in would evict the user's browser session")
	}
	if body.Client != automationClientClass {
		t.Errorf("client = %q, want %q", body.Client, automationClientClass)
	}
	if clientClass != automationClientClass {
		t.Errorf("%s = %q, want %q", clientClassHeader, clientClass, automationClientClass)
	}
}

// TestNewEngineLoginDoesNotMatchTheWholeBody pins that the reason is read from
// the fields that name it: matching substrings against the raw body let an
// unrelated word — "recipient" contains "ip" — turn wrong credentials into an
// IP block, and callers branch on that code.
func TestNewEngineLoginDoesNotMatchTheWholeBody(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"body mentions ip inside another word", `{"message":"Неверный recipient или пароль"}`},
		{"body mentions email inside a hint", `{"message":"Проверьте email при входе"}`},
		{"body is plain text", `Unauthorized: description`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newEngineClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(tc.body))
			})
			resp, err := c.Login(context.Background(), "encx_probe_nonexistent", "x")
			if err != nil {
				t.Fatalf("Login: %v", err)
			}
			if resp.Error != 2 {
				t.Errorf("Error = %d (%s), want 2 (%s)",
					resp.Error, LoginErrorText(resp.Error), LoginErrorText(2))
			}
		})
	}
}
