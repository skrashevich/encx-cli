package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCodexModelCatalog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer private-token" || r.Header.Get("ChatGPT-Account-ID") != "account" || r.Header.Get("originator") != "codex_cli_rs" {
			t.Error("incorrect subscription authentication")
		}
		io.WriteString(w, `{"models":[{"slug":"gpt-second","visibility":"list","priority":2},{"slug":"gpt-first","display_name":"First","visibility":"list","priority":1},{"slug":"gpt-hidden","visibility":"hide"},{"slug":"claude-invalid","visibility":"list"},{"slug":"gpt-first","visibility":"list","priority":3}]}`)
	}))
	defer server.Close()
	c, err := fetchCodexModels(t.Context(), server.Client(), server.URL, "private-token", "account")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Models) != 2 || c.Models[0].Name != "First" || c.DefaultModel != "gpt-first" {
		t.Fatalf("unexpected catalog: %+v", c)
	}
}

func TestCodexModelCatalogFailures(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status int
		body   string
	}{
		{"unauthorized", 401, "private-token"}, {"rate limited", 429, "private-token"}, {"invalid json", 200, "private-token"}, {"empty", 200, `{"models":[]}`}, {"oversized", 200, strings.Repeat("x", (2<<20)+1)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tt.status); io.WriteString(w, tt.body) }))
			defer s.Close()
			_, err := fetchCodexModels(t.Context(), s.Client(), s.URL, "private-token", "")
			if err == nil || strings.Contains(err.Error(), "private-token") {
				t.Fatalf("expected sanitized error: %v", err)
			}
		})
	}
}

func TestCodexModelCatalogRejectsRedirect(t *testing.T) {
	reached := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))
	defer target.Close()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer s.Close()
	_, err := fetchCodexModels(t.Context(), s.Client(), s.URL, "private-token", "")
	if err == nil || reached {
		t.Fatal("catalog followed redirect")
	}
}

func TestCodexModelsEndpointRequiresCredential(t *testing.T) {
	server := newLLMSettingsTestServer(t)
	res, err := http.Get(server.URL + "/api/v1/llm/codex/models")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized || res.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("status: %d", res.StatusCode)
	}
}
