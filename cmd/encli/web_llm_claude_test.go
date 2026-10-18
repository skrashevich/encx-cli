package main

import (
	"context"
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClaudeModelCatalogPagination(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer private-token" || r.Header.Get("anthropic-version") != "2023-06-01" || r.Header.Get("anthropic-beta") != "oauth-2025-04-20" || r.Header.Get("X-Api-Key") != "" {
			t.Error("incorrect catalog authentication")
		}
		if r.URL.Query().Get("limit") != "1000" {
			t.Error("missing page limit")
		}
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			io.WriteString(w, `{"data":[{"id":"claude-sonnet-5-5","display_name":"Claude Sonnet 5.5"},{"id":"claude-retired","lifecycle":"retired"}],"has_more":true,"last_id":"cursor+one"}`)
		} else {
			if r.URL.Query().Get("after_id") != "cursor+one" {
				t.Error("incorrect cursor")
			}
			io.WriteString(w, `{"data":[{"id":"claude-sonnet-5-5"},{"id":"claude-sonnet-4-6","display_name":"Claude Sonnet 4.6"}],"has_more":false}`)
		}
	}))
	defer server.Close()
	catalog, err := fetchClaudeModels(t.Context(), server.Client(), server.URL, "private-token")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(catalog.Models) != 2 || catalog.DefaultModel != defaultClaudeModel || catalog.Models[0].Name != "Claude Sonnet 5.5" {
		t.Fatalf("catalog: %+v, calls: %d", catalog, calls)
	}
}

func TestClaudeModelCatalogFailures(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status int
		body   string
	}{
		{"rate limit", 429, `private-token`},
		{"invalid json", 200, `private-token`},
		{"empty", 200, `{"data":[]}`},
		{"stalled cursor", 200, `{"data":[{"id":"claude-test"}],"has_more":true,"last_id":"same"}`},
		{"oversized", 200, strings.Repeat("x", (2<<20)+1)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tt.status); io.WriteString(w, tt.body) }))
			defer server.Close()
			_, err := fetchClaudeModels(t.Context(), server.Client(), server.URL, "private-token")
			if err == nil || strings.Contains(err.Error(), "private-token") {
				t.Fatalf("error must be sanitized: %v", err)
			}
		})
	}
}

func TestClaudeModelCatalogRejectsRedirectAndHonorsCancellation(t *testing.T) {
	received := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { received = true }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer redirect.Close()
	if _, err := fetchClaudeModels(t.Context(), redirect.Client(), redirect.URL, "private-token"); err == nil {
		t.Fatal("redirect accepted")
	}
	if received {
		t.Fatal("followed credential-bearing redirect")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := fetchClaudeModels(ctx, redirect.Client(), redirect.URL, "private-token"); err == nil {
		t.Fatal("canceled request succeeded")
	}
}

func TestClaudeModelsEndpointRequiresCredential(t *testing.T) {
	server := newLLMSettingsTestServer(t)
	res, err := http.Get(server.URL + "/api/v1/llm/claude/models")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized || res.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("status: %d", res.StatusCode)
	}
}

func TestClaudeCatalogDefaultWhenOldModelUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"data":[{"id":"claude-new","display_name":"New"}]}`)
	}))
	defer server.Close()
	catalog, err := fetchClaudeModels(t.Context(), server.Client(), server.URL, "secret")
	if err != nil {
		t.Fatal(err)
	}
	if catalog.DefaultModel != "claude-new" {
		t.Fatal("unavailable default offered")
	}
	raw, _ := json.Marshal(catalog)
	if strings.Contains(string(raw), "secret") {
		t.Fatal("token leaked")
	}
}
