package enapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func newTestClient(t *testing.T, handler http.HandlerFunc, opts ...Option) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return New(srv.Client(), srv.URL, "tech.en.cx", opts...)
}

func TestClientSendsDomainAndBearerHeaders(t *testing.T) {
	var gotDomain, gotAuth, gotUA, gotAccept string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotDomain = r.Header.Get(DomainHeader)
		gotAuth = r.Header.Get("Authorization")
		gotUA = r.Header.Get("User-Agent")
		gotAccept = r.Header.Get("Accept")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}, WithUserAgent("encx-test"))
	c.SetToken("jwt-token")

	var out struct {
		OK bool `json:"ok"`
	}
	if err := c.GetJSON(context.Background(), "/version", nil, &out); err != nil {
		t.Fatalf("GetJSON: %v", err)
	}
	if !out.OK {
		t.Error("response was not decoded")
	}
	if gotDomain != "tech.en.cx" {
		t.Errorf("%s = %q, want tech.en.cx", DomainHeader, gotDomain)
	}
	if gotAuth != "Bearer jwt-token" {
		t.Errorf("Authorization = %q, want Bearer jwt-token", gotAuth)
	}
	if gotUA != "encx-test" {
		t.Errorf("User-Agent = %q, want encx-test", gotUA)
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept = %q, want application/json", gotAccept)
	}
}

// The new engine renders rank names, correction durations and other sentences
// server-side and picks the language from Accept-Language; only a handful of
// routes take a lang query parameter. Without the header the whole API answers
// in English where the legacy engine served the site language.
func TestClientSendsAcceptLanguage(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts []Option
		want string
	}{
		{"default", nil, "ru"},
		{"configured", []Option{WithLang("en")}, "en"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				got = r.Header.Get("Accept-Language")
				_, _ = w.Write([]byte(`{}`))
			}, tc.opts...)
			if err := c.GetJSON(context.Background(), "/games/1/corrections", nil, nil); err != nil {
				t.Fatalf("GetJSON: %v", err)
			}
			if got != tc.want {
				t.Errorf("Accept-Language = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClientOmitsAuthorizationWithoutToken(t *testing.T) {
	var hasAuth bool
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, hasAuth = r.Header["Authorization"]
		_, _ = w.Write([]byte(`{}`))
	})
	if err := c.GetJSON(context.Background(), "/games/home", nil, nil); err != nil {
		t.Fatalf("GetJSON: %v", err)
	}
	if hasAuth {
		t.Error("Authorization header sent without a token")
	}
}

func TestClientPostsJSONBody(t *testing.T) {
	var gotBody map[string]any
	var gotMethod, gotContentType, gotPath, gotQuery string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		_, _ = w.Write([]byte(`{"token":"abc"}`))
	})

	var out struct {
		Token string `json:"token"`
	}
	err := c.Do(context.Background(), Request{
		Method: http.MethodPost,
		Path:   "/login",
		Query:  url.Values{"lang": {"ru"}},
		Body:   map[string]string{"login": "user", "password": "secret"},
		Out:    &out,
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/login" {
		t.Errorf("path = %q, want /login", gotPath)
	}
	if gotQuery != "lang=ru" {
		t.Errorf("query = %q, want lang=ru", gotQuery)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	if gotBody["login"] != "user" || gotBody["password"] != "secret" {
		t.Errorf("body = %v, want login/password", gotBody)
	}
	if out.Token != "abc" {
		t.Errorf("token = %q, want abc", out.Token)
	}
}

func TestClientDecodesJSONAPIError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized","message":"Authorization required","code":401}`))
	})

	err := c.GetJSON(context.Background(), "/auth/session", nil, nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	apiErr, ok := AsAPIError(err)
	if !ok {
		t.Fatalf("error is not *APIError: %v", err)
	}
	if apiErr.Status != http.StatusUnauthorized {
		t.Errorf("Status = %d, want 401", apiErr.Status)
	}
	if apiErr.Code != 401 {
		t.Errorf("Code = %d, want 401", apiErr.Code)
	}
	if apiErr.Err != "unauthorized" {
		t.Errorf("Err = %q, want unauthorized", apiErr.Err)
	}
	if apiErr.Message != "Authorization required" {
		t.Errorf("Message = %q", apiErr.Message)
	}
	if !apiErr.Unauthorized() || !IsUnauthorized(err) {
		t.Error("401 was not recognized as unauthorized")
	}
}

func TestClientDecodesSentenceKeyError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"sentence_key":"WrongAnswer","format_args":["3"]}`))
	})

	err := c.PostJSON(context.Background(), "/games/1/engine", map[string]string{}, nil)
	apiErr, ok := AsAPIError(err)
	if !ok {
		t.Fatalf("error is not *APIError: %v", err)
	}
	if apiErr.SentenceKey != "WrongAnswer" {
		t.Errorf("SentenceKey = %q, want WrongAnswer", apiErr.SentenceKey)
	}
	if len(apiErr.FormatArgs) != 1 || apiErr.FormatArgs[0] != "3" {
		t.Errorf("FormatArgs = %v, want [3]", apiErr.FormatArgs)
	}
	if !IsStatus(err, http.StatusBadRequest) {
		t.Error("400 was not recognized")
	}
}

func TestClientKeepsPlainTextError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("Game not found"))
	})

	err := c.GetJSON(context.Background(), "/games/999/details", nil, nil)
	apiErr, ok := AsAPIError(err)
	if !ok {
		t.Fatalf("error is not *APIError: %v", err)
	}
	if apiErr.Body != "Game not found" {
		t.Errorf("Body = %q, want Game not found", apiErr.Body)
	}
	if !IsNotFound(err) {
		t.Error("404 was not recognized")
	}
	if got := apiErr.Error(); got == "" {
		t.Error("Error() is empty")
	}
}

func TestClientGetBytesReturnsRawBody(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte{0x89, 'P', 'N', 'G'})
	})

	body, header, err := c.GetBytes(context.Background(), "/media/avatars/1.png", nil)
	if err != nil {
		t.Fatalf("GetBytes: %v", err)
	}
	if string(body) != "\x89PNG" {
		t.Errorf("body = %q", body)
	}
	if got := header.Get("Content-Type"); got != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", got)
	}
}

func TestClientURLBuildsAbsolutePaths(t *testing.T) {
	c := New(nil, "https://api.en.cx/", "tech.en.cx")
	if got := c.URL("version", nil); got != "https://api.en.cx/version" {
		t.Errorf("URL = %q", got)
	}
	if got := c.URL("/games", url.Values{"page": {"2"}}); got != "https://api.en.cx/games?page=2" {
		t.Errorf("URL = %q", got)
	}
	if c.BaseURL() != "https://api.en.cx" {
		t.Errorf("BaseURL = %q", c.BaseURL())
	}
	if c.Domain() != "tech.en.cx" || c.Lang() != "ru" {
		t.Errorf("Domain = %q, Lang = %q", c.Domain(), c.Lang())
	}
}
