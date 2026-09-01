package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mockHandlerForRouting builds the same handler main() serves with -engine both,
// so the routing tests see the production composition and not a subset of it.
func mockHandlerForRouting(t *testing.T) http.Handler {
	t.Helper()
	s := newMockServer(t)
	legacyMux := s.routes()
	apiMux := http.NewServeMux()
	s.registerNewAPIRoutes(apiMux, legacyMux)
	s.registerAdminAPIRoutes(apiMux)
	return newEngineFallbackMux(apiMux, legacyMux)
}

// TestLoginSigninAcceptsTrailingSlash is the regression that started this:
// enxbot posts to "/login/signin/", the live ASP.NET engine treats it as
// "/login/signin", and the mock used to fall through to the "GET /" catch-all
// and answer 405 — a failure the real engine never produces.
func TestLoginSigninAcceptsTrailingSlash(t *testing.T) {
	handler := mockHandlerForRouting(t)

	for _, path := range []string{"/login/signin", "/login/signin/", "/login/signin/?json=1"} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("Login=mock&Password=mock"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("POST %s: status = %d, want 200 (body %q)", path, rec.Code, rec.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("POST %s: body is not JSON: %v", path, err)
		}
		if code, _ := body["Error"].(float64); code != 0 {
			t.Fatalf("POST %s: Error = %v, want 0", path, body["Error"])
		}
		if len(rec.Result().Cookies()) == 0 {
			t.Fatalf("POST %s: no session cookies, so the handler did not really log in", path)
		}
	}
}

// TestRoutesAcceptTrailingSlash covers the rest of the surface: the engine is
// slash-tolerant everywhere, not only on the login route.
func TestRoutesAcceptTrailingSlash(t *testing.T) {
	handler := mockHandlerForRouting(t)

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/version/"},
		{http.MethodGet, "/auth/session/"},
		{http.MethodGet, "/games/active/"},
		{http.MethodGet, "/UserDetails.aspx/"},
		{http.MethodGet, "/admin/games/"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code == http.StatusMethodNotAllowed || rec.Code == http.StatusNotFound {
			t.Errorf("%s %s: status = %d, want the slashless route's answer", tc.method, tc.path, rec.Code)
		}
	}
}

// TestSubtreeRoutesStillMatch guards the other direction: registering the
// trailing-slash twins must not shadow the subtree patterns the game pages use.
// It asks the mux which pattern it picked rather than reading a status code, so
// a handler that answers 404 on its own (an unknown game id, say) cannot be
// mistaken for a routing miss.
func TestSubtreeRoutesStillMatch(t *testing.T) {
	legacyMux := newMockServer(t).routes()

	cases := []struct{ path, want string }{
		{"/home/", "GET /home/"},
		{"/gamestatistics/full/", "GET /gamestatistics/full/"},
		{"/gameengines/encounter/play/424242", "GET /gameengines/encounter/play/"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		if _, pattern := legacyMux.Handler(req); pattern != tc.want {
			t.Errorf("GET %s routed to %q, want %q", tc.path, pattern, tc.want)
		}
	}
}
