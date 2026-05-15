package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func withWikipediaServer(t *testing.T, handler http.HandlerFunc, fn func()) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	prev := wikipediaHTTPClient
	wikipediaHTTPClient = srv.Client()
	t.Cleanup(func() { wikipediaHTTPClient = prev })

	orig := wikipediaAPIBaseURL
	wikipediaAPIBaseURL = func(lang string) string {
		return srv.URL
	}
	t.Cleanup(func() {
		wikipediaAPIBaseURL = orig
	})
	fn()
}

func TestWikipediaSearch(t *testing.T) {
	withWikipediaServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("list") != "search" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"query": map[string]any{
				"search": []map[string]any{
					{"pageid": 42, "title": "Moscow", "snippet": "capital of <span>Russia</span>"},
				},
			},
		})
	}, func() {
		out := captureStdout(t, func() {
			toolWikipediaSearch(context.Background(), "Moscow", "en", 5)
		})
		if !strings.Contains(out, `"title": "Moscow"`) || !strings.Contains(out, "capital of Russia") {
			t.Fatalf("unexpected output: %s", out)
		}
	})
}

func TestWikipediaArticle(t *testing.T) {
	withWikipediaServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("prop") == "" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"query": map[string]any{
				"pages": map[string]any{
					"42": map[string]any{
						"pageid":   42,
						"title":    "Moscow",
						"extract":  "Moscow is the capital of Russia.",
						"fullurl":  "https://en.wikipedia.org/wiki/Moscow",
					},
				},
			},
		})
	}, func() {
		out := captureStdout(t, func() {
			toolWikipediaArticle(context.Background(), "Moscow", "en")
		})
		if !strings.Contains(out, "Moscow is the capital of Russia") {
			t.Fatalf("unexpected output: %s", out)
		}
	})
}

func TestNormalizeWikiLangDefault(t *testing.T) {
	if got := normalizeWikiLang(""); got != "ru" {
		t.Fatalf("expected ru, got %q", got)
	}
}

func TestWikipediaFetchUsesInjectedClient(t *testing.T) {
	withWikipediaServer(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"ok":true}`)
	}, func() {
		body, err := wikipediaFetch(context.Background(), "en", nil)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(body), `"ok"`) {
			t.Fatalf("unexpected body: %s", body)
		}
	})
}
