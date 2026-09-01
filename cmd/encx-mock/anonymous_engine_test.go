package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/skrashevich/encx-cli/encx"
	"github.com/skrashevich/encx-cli/encx/enapi"
)

// The two engines answer an anonymous game engine call differently, and the
// mock has to reproduce both. Measured on 2026-09-01:
//
//	GET https://svk.en.cx/gameengines/encounter/play/82448?json=1&lang=ru
//	  302, Location: /login.aspx?return=%2fgameengines%2f...%3fjson%3d1%26lang%3dru
//	GET https://api.en.cx/gameengines/encounter/play/27053?json=1&lang=ru
//	    (X-En-Domain: demo.en.cx)
//	  401, {"error":"unauthorized","message":"unauthorized","code":401}
//
// Both hold with json=1, without it, and for a game id that does not exist:
// each engine checks the session before it looks at anything else.

func TestLegacyEnginePlayBouncesAnonymousToLoginPage(t *testing.T) {
	legacyMux := newMockServer(t).routes()

	for _, path := range []string{
		"/gameengines/encounter/play/424242?json=1&lang=ru",
		"/gameengines/encounter/play/424242",
		"/gameengines/encounter/play/999999999?json=1&lang=ru",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		legacyMux.ServeHTTP(rec, req)

		if rec.Code != http.StatusFound {
			t.Errorf("GET %s: status = %d, want 302", path, rec.Code)
			continue
		}
		wantLocation := "/login.aspx?return=" + lowerHexEscapes(escapeForTest(path))
		if got := rec.Header().Get("Location"); got != wantLocation {
			t.Errorf("GET %s: Location = %q, want %q", path, got, wantLocation)
		}
		if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
			t.Errorf("GET %s: Content-Type = %q, want text/html; charset=utf-8", path, got)
		}
		if !strings.Contains(rec.Body.String(), "Object moved") {
			t.Errorf("GET %s: body = %q, want the engine's \"Object moved\" page", path, rec.Body.String())
		}
	}
}

// TestLoginPageCarriesTheMarkersEncxReads guards the page the redirect lands on:
// encx tells an expired session apart from the anti-spam wall by the sign-in
// form's own field names, so a stub without them would change the diagnosis.
func TestLoginPageCarriesTheMarkersEncxReads(t *testing.T) {
	legacyMux := newMockServer(t).routes()

	req := httptest.NewRequest(http.MethodGet, "/login.aspx?return=%2fgameengines%2fencounter%2fplay%2f424242", nil)
	rec := httptest.NewRecorder()
	legacyMux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	page := rec.Body.String()
	for _, marker := range []string{"txtLogin", "txtPassword", "/login.aspx?return="} {
		if !strings.Contains(page, marker) {
			t.Errorf("the sign-in page is missing %q:\n%s", marker, page)
		}
	}
}

func TestNewEnginePlayAnswersUnauthorizedForAnonymous(t *testing.T) {
	s := newMockServer(t)
	legacyMux := s.routes()
	apiMux := http.NewServeMux()
	s.registerNewAPIRoutes(apiMux, legacyMux)
	handler := newEngineFallbackMux(apiMux, legacyMux)

	for _, path := range []string{
		"/gameengines/encounter/play/424242?json=1&lang=ru",
		"/gameengines/encounter/play/424242",
		"/gameengines/encounter/play/999999999?json=1&lang=ru",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("GET %s: status = %d, want 401 (body %q)", path, rec.Code, rec.Body.String())
			continue
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Errorf("GET %s: body is not JSON: %v", path, err)
			continue
		}
		want := map[string]any{"error": "unauthorized", "message": "unauthorized", "code": float64(401)}
		for key, value := range want {
			if body[key] != value {
				t.Errorf("GET %s: %s = %v, want %v (body %v)", path, key, body[key], value, body)
			}
		}
	}
}

// TestAnonymousPlayIsAnErrorOnBothEngines is the one that matters. The mock used
// to answer the legacy route with 401 and a JSON body; encx decodes the engine's
// body without gating on the status, so that body parsed into an empty GameModel
// and the call reported success. A mock that turns a lost session into a silent
// empty game hides the very failure it exists to surface.
func TestAnonymousPlayIsAnErrorOnBothEngines(t *testing.T) {
	legacy, api := newMockServers(t)
	ctx := context.Background()

	t.Run("legacy", func(t *testing.T) {
		c := mockClient(t, legacy, encx.EngineLegacy)
		model, err := c.GetGameModel(ctx, mockGameID)
		if err == nil {
			t.Fatalf("GetGameModel without a session returned no error, model = %+v", model)
		}
		if !strings.Contains(err.Error(), "session expired") {
			t.Errorf("error = %q, want the expired-session diagnosis the live engine produces", err)
		}
	})

	t.Run("new", func(t *testing.T) {
		c := mockClient(t, api, encx.EngineNew)
		model, err := c.GetGameModel(ctx, mockGameID)
		if err == nil {
			t.Fatalf("GetGameModel without a session returned no error, model = %+v", model)
		}
		if !enapi.IsUnauthorized(err) {
			t.Errorf("error = %q, want an enapi 401", err)
		}
	})
}

// TestPublicLegacyPagesNeedNoSession pins the pages Encounter serves to anyone.
// Measured on svk.en.cx (2026-09-01) with no cookies at all: /home/?json=1 and
// /gamestatistics/full/{id}?json=1 answer 200 with the full document, and the
// game card answers 200 with or without json=1.
func TestPublicLegacyPagesNeedNoSession(t *testing.T) {
	legacyMux := newMockServer(t).routes()

	cases := []struct {
		path      string
		wantJSON  bool
		mustCarry string
	}{
		{path: "/home/?json=1", wantJSON: true, mustCarry: "ActiveGames"},
		{path: "/gamestatistics/full/424242?json=1", wantJSON: true, mustCarry: "Levels"},
		{path: "/GameDetails.aspx?gid=424242&json=1"},
		{path: "/GameDetails.aspx?gid=424242"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		rec := httptest.NewRecorder()
		legacyMux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("GET %s: status = %d, want 200 (body %q)", tc.path, rec.Code, rec.Body.String())
			continue
		}
		if !tc.wantJSON {
			continue
		}
		var doc map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
			t.Errorf("GET %s: body is not JSON: %v", tc.path, err)
			continue
		}
		if _, ok := doc[tc.mustCarry]; !ok {
			t.Errorf("GET %s: the document has no %q, so it is not the public answer", tc.path, tc.mustCarry)
		}
	}
}

// TestMemberOnlyLegacyPagesAnswerForbidden pins the other half: the profile and
// team pages answer 403 with the platform's sign-in interstitial, on svk.en.cx
// and on kharkov.en.cx alike, and the fee page bounces to /Login.aspx.
func TestMemberOnlyLegacyPagesAnswerForbidden(t *testing.T) {
	legacyMux := newMockServer(t).routes()

	for _, path := range []string{"/UserDetails.aspx?uid=1&json=1", "/Teams/TeamDetails.aspx?tid=1&json=1"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		legacyMux.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("GET %s: status = %d, want 403", path, rec.Code)
			continue
		}
		if !strings.Contains(rec.Body.String(), makeFeeLoginPath) {
			t.Errorf("GET %s: the interstitial does not link to %s, so encx cannot recognize it",
				path, makeFeeLoginPath)
		}
	}

	feePath := "/MakeGameFee.aspx?gid=424242&json=1"
	req := httptest.NewRequest(http.MethodGet, feePath, nil)
	rec := httptest.NewRecorder()
	legacyMux.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("GET %s: status = %d, want 302", feePath, rec.Code)
	}
	want := makeFeeLoginPath + "?return=" + lowerHexEscapes(escapeForTest(feePath))
	if got := rec.Header().Get("Location"); got != want {
		t.Errorf("GET %s: Location = %q, want %q", feePath, got, want)
	}
}

// TestAdminSurfaceRequiresASession covers the group as a whole rather than a
// sample of it: every route the admin mux serves must answer 401 to an
// anonymous caller, except the one the live API publishes.
func TestAdminSurfaceRequiresASession(t *testing.T) {
	s := newMockServer(t)
	legacyMux := s.routes()
	apiMux := http.NewServeMux()
	s.registerNewAPIRoutes(apiMux, legacyMux)
	s.registerAdminAPIRoutes(apiMux)

	cases := []struct {
		method, path string
		wantStatus   int
	}{
		{http.MethodGet, "/admin/games", http.StatusUnauthorized},
		{http.MethodGet, "/admin/games/424242", http.StatusUnauthorized},
		{http.MethodGet, "/admin/games/424242/levels", http.StatusUnauthorized},
		{http.MethodGet, "/admin/games/424242/lifecycle", http.StatusUnauthorized},
		{http.MethodPatch, "/admin/games/424242", http.StatusUnauthorized},
		{http.MethodPost, "/admin/games/424242/levels", http.StatusUnauthorized},
		{http.MethodDelete, "/admin/games/424242/levels/1", http.StatusUnauthorized},
		{http.MethodGet, "/games/424242/monitoring", http.StatusUnauthorized},
		// The live API publishes the corrections feed to anyone. The mock still
		// refuses a game that has not started, which is its own measured rule
		// (TestMockAdminCorrectionsOnAGameThatHasNotStarted), so what this case
		// asserts is only that the route is not behind the session guard.
		{http.MethodGet, "/games/424242/corrections", 0},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		rec := httptest.NewRecorder()
		apiMux.ServeHTTP(rec, req)
		if tc.wantStatus == 0 {
			if rec.Code == http.StatusUnauthorized {
				t.Errorf("%s %s: status = 401, but the live API serves it to anyone", tc.method, tc.path)
			}
			continue
		}
		if rec.Code != tc.wantStatus {
			t.Errorf("%s %s: status = %d, want %d (body %q)",
				tc.method, tc.path, rec.Code, tc.wantStatus, rec.Body.String())
			continue
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Errorf("%s %s: body is not JSON: %v", tc.method, tc.path, err)
			continue
		}
		want := map[string]any{"error": "unauthorized", "message": "Authorization required", "code": float64(401)}
		for key, value := range want {
			if body[key] != value {
				t.Errorf("%s %s: %s = %v, want %v", tc.method, tc.path, key, body[key], value)
			}
		}
	}
}

// TestBearerTokenAuthenticatesTheAdminSurface covers the mobile session, which
// carries no cookie: the token the login route hands out has to be accepted on
// its own, or the mock would only ever work for browser-shaped clients.
func TestBearerTokenAuthenticatesTheAdminSurface(t *testing.T) {
	_, api := newMockServers(t)
	token := mockSessionToken(t, api)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, api.URL+"/admin/games", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := api.Client().Do(req)
	if err != nil {
		t.Fatalf("GET /admin/games: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /admin/games with a bearer token: HTTP %d", resp.StatusCode)
	}
}

// escapeForTest mirrors url.QueryEscape for the paths above without importing
// net/url into the expectation, so the test states the escaping it wants.
func escapeForTest(path string) string {
	replacer := strings.NewReplacer("/", "%2F", "?", "%3F", "=", "%3D", "&", "%26")
	return replacer.Replace(path)
}
