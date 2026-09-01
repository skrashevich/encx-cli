package encx

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/skrashevich/encx-cli/encx/enapi"
)

func TestParseEngineMode(t *testing.T) {
	cases := []struct {
		in   string
		want EngineMode
		ok   bool
	}{
		{"legacy", EngineLegacy, true},
		{"new", EngineNew, true},
		{"auto", EngineAuto, true},
		{"  NEW  ", EngineNew, true},
		{"", DefaultEngineMode, false},
		{"nonsense", DefaultEngineMode, false},
	}
	for _, tc := range cases {
		got, ok := ParseEngineMode(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("ParseEngineMode(%q) = %v,%v; want %v,%v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestDefaultAPIBaseURLDerivesTheZoneHost(t *testing.T) {
	cases := map[string]string{
		"tech.en.cx":           "https://api.en.cx",
		"moscow.en.cx":         "https://api.en.cx",
		"en.cx":                "https://api.en.cx",
		"demo.encounter.cx":    "https://api.encounter.cx",
		"demo.encounter.ru":    "https://api.encounter.ru",
		"demo.en-world.org":    "https://api.en-world.org",
		"kharkovquestua.en.cx": "https://api.en.cx",
		"tech.en.cx:8443":      "https://api.en.cx",
		"TECH.EN.CX":           "https://api.en.cx",
	}
	for domain, want := range cases {
		if got := defaultAPIBaseURL("https", domain); got != want {
			t.Errorf("defaultAPIBaseURL(%q) = %q, want %q", domain, got, want)
		}
	}
}

// TestDefaultAPIBaseURLRefusesForeignZones is the guard against sending the site
// header and the sign-in to a host that never claimed the domain: stripping a
// label off an arbitrary domain would turn quest.example.com into
// api.example.com, and falling back to Encounter's own host would hand that
// domain's password to a backend which does not serve it.
func TestDefaultAPIBaseURLRefusesForeignZones(t *testing.T) {
	for _, domain := range []string{
		"quest.example.com", "game.github.io", "quest.herokuapp.com",
		"evil.com", "", "127.0.0.1:8080",
	} {
		if got := defaultAPIBaseURL("https", domain); got != "" {
			t.Errorf("defaultAPIBaseURL(%q) = %q, want no host at all", domain, got)
		}
	}
}

func TestEngineAutoDoesNotProbeOutsideEncounterZones(t *testing.T) {
	t.Setenv(EngineEnvVar, "auto")
	for _, domain := range []string{"quest.example.com", "game.github.io", "evil.com"} {
		c := New(domain)
		if c.canProbeNewEngine() {
			t.Errorf("%q: the client would probe a host derived from a foreign zone", domain)
		}
		if got := c.Engine(); got != EngineLegacy {
			t.Errorf("Engine(%q) = %q, want legacy", domain, got)
		}
	}
}

// TestEngineAutoRequiresTheSiteToClaimTheDomain closes the case where a host
// answers 200 with any non-zero id: without checking that the site names our
// domain, that answer alone would move the session and the sign-in onto it.
func TestEngineAutoRequiresTheSiteToClaimTheDomain(t *testing.T) {
	t.Setenv(EngineEnvVar, "auto")
	cases := []struct {
		name string
		body string
		want EngineMode
	}{
		{"claims another domain", `{"id":1,"name":"foreign","primary_domain":"other.en.cx"}`, EngineLegacy},
		{"names no domain at all", `{"id":1,"name":"anything"}`, EngineLegacy},
		{"claims ours as primary", `{"id":135,"primary_domain":"demo.en.cx"}`, EngineNew},
		{"claims ours as an alias", `{"id":135,"primary_domain":"demo.encounter.cx",
		  "domains":[{"domain":"demo.encounter.cx","is_primary":true},{"domain":"demo.en.cx"}]}`, EngineNew},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(tc.body))
			}))
			srv.Start()

			// No WithAPIBaseURL: the host was derived, so it has to prove itself.
			c := New("demo.en.cx", WithHTTP())
			c.apiBaseURL = srv.URL
			if got := c.Engine(); got != tc.want {
				t.Errorf("Engine = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestEngineAutoDoesNotCacheATransportFailure keeps a long-lived client from
// being pinned to the legacy engine by one network blip.
func TestEngineAutoDoesNotCacheATransportFailure(t *testing.T) {
	t.Setenv(EngineEnvVar, "auto")
	// The flag is shared with the server goroutine.
	var reachable atomic.Bool
	srv := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !reachable.Load() {
			// Close the connection the way a network failure would.
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Fatal("the test server cannot simulate a dropped connection")
			}
			conn, _, err := hj.Hijack()
			if err != nil {
				t.Fatalf("hijack: %v", err)
			}
			_ = conn.Close()
			return
		}
		_, _ = w.Write([]byte(`{"id":135,"primary_domain":"demo.en.cx"}`))
	}))
	srv.Start()

	c := New("demo.en.cx", WithHTTP())
	c.apiBaseURL = srv.URL
	if got := c.Engine(); got != EngineLegacy {
		t.Fatalf("Engine = %q, want legacy while the API host is unreachable", got)
	}

	reachable.Store(true)
	if got := c.Engine(); got != EngineNew {
		t.Errorf("Engine = %q, want new once the host answers: a transport failure "+
			"must not be cached as a verdict", got)
	}
}

// TestEngineAutoCachesADefinitiveAnswer is the other half: a 404 is the backend
// saying the domain is still on the legacy engine, and that does not change.
func TestEngineAutoCachesADefinitiveAnswer(t *testing.T) {
	t.Setenv(EngineEnvVar, "auto")
	var probes atomic.Int32
	srv := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probes.Add(1)
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"domain_unregistered","code":404}`))
	}))
	srv.Start()

	c := New("demo.en.cx", WithHTTP())
	c.apiBaseURL = srv.URL
	for i := 0; i < 3; i++ {
		if got := c.Engine(); got != EngineLegacy {
			t.Fatalf("Engine = %q, want legacy", got)
		}
	}
	if got := probes.Load(); got != 1 {
		t.Errorf("probes = %d, want 1: a 404 is final", got)
	}
}

// TestEngineDefaultsToAuto pins the default: with nothing configured the client
// asks whether the domain has moved rather than assuming either engine.
func TestEngineDefaultsToAuto(t *testing.T) {
	t.Setenv(EngineEnvVar, "")
	c := New("tech.en.cx")
	if got := c.EngineMode(); got != EngineAuto {
		t.Errorf("EngineMode = %q, want auto", got)
	}
}

// TestEngineAutoSkipsProbeWithoutARealDomain keeps local and test setups off the
// network: the API host is derived from the domain, so an IP or a bare hostname
// has nothing to ask.
func TestEngineAutoSkipsProbeWithoutARealDomain(t *testing.T) {
	t.Setenv(EngineEnvVar, "auto")
	for _, domain := range []string{"127.0.0.1:8080", "localhost", "::1", ""} {
		c := New(domain)
		if got := c.Engine(); got != EngineLegacy {
			t.Errorf("Engine(%q) = %q, want legacy without a probe", domain, got)
		}
	}
}

func TestEngineReadsEnvVar(t *testing.T) {
	t.Setenv(EngineEnvVar, "new")
	c := New("tech.en.cx")
	if got := c.Engine(); got != EngineNew {
		t.Errorf("Engine = %q, want new", got)
	}
}

func TestWithEngineOverridesEnvVar(t *testing.T) {
	t.Setenv(EngineEnvVar, "new")
	c := New("tech.en.cx", WithEngine(EngineLegacy))
	if got := c.Engine(); got != EngineLegacy {
		t.Errorf("Engine = %q, want legacy", got)
	}
}

// engineRouteRecorder answers both engines from one server and records the
// paths each backend asked for.
type engineRouteRecorder struct {
	paths  []string
	bodies []string
	// domainUnregistered makes the API host answer as it does for a domain
	// still served by the ASP.NET engine.
	domainUnregistered bool
}

func (rec *engineRouteRecorder) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rec.paths = append(rec.paths, r.URL.Path)
		rec.bodies = append(rec.bodies, string(body))
		switch {
		case strings.HasPrefix(r.URL.Path, "/sites/domain/"):
			if rec.domainUnregistered {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":"domain_unregistered","key":"Domain name is unregistered","code":404}`))
				return
			}
			_, _ = w.Write([]byte(`{"id":135,"name":"Демо город"}`))
		case r.URL.Path == "/login/signin":
			_, _ = w.Write([]byte(`{"Error":0,"Message":"ok"}`))
		case r.URL.Path == "/login":
			_, _ = w.Write([]byte(`{"token":"jwt","message":"ok"}`))
		case r.URL.Path == "/auth/session":
			_, _ = w.Write([]byte(`{"user_id":1}`))
		case r.URL.Path == "/version":
			_, _ = w.Write([]byte(`{"site_version":"dev"}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}
}

func newRoutedClient(t *testing.T, rec *engineRouteRecorder, opts ...Option) *Client {
	t.Helper()
	srv := httptest.NewTestServer(t, rec.handler(t))
	srv.Start()
	host := strings.TrimPrefix(srv.URL, "http://")
	opts = append([]Option{WithHTTP(), WithAdminDelay(0), WithAPIBaseURL(srv.URL)}, opts...)
	return New(host, opts...)
}

func TestLegacyEngineUsesASPRoutes(t *testing.T) {
	t.Setenv(EngineEnvVar, "legacy")
	rec := &engineRouteRecorder{}
	c := newRoutedClient(t, rec)

	if _, err := c.Login(context.Background(), "user", "pass"); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if err := c.VerifyAdminSession(context.Background()); err != nil {
		t.Fatalf("VerifyAdminSession: %v", err)
	}

	want := []string{"/login/signin", "/Administration/Games/LevelManager.aspx"}
	if len(rec.paths) != len(want) {
		t.Fatalf("paths = %v, want %v", rec.paths, want)
	}
	for i, path := range want {
		if rec.paths[i] != path {
			t.Errorf("paths[%d] = %q, want %q", i, rec.paths[i], path)
		}
	}
	if !strings.Contains(rec.bodies[0], "Login=user") {
		t.Errorf("legacy login body = %q, want a form encoding", rec.bodies[0])
	}
}

func TestNewEngineUsesRESTRoutes(t *testing.T) {
	t.Setenv(EngineEnvVar, "legacy")
	rec := &engineRouteRecorder{}
	c := newRoutedClient(t, rec, WithEngine(EngineNew))

	resp, err := c.Login(context.Background(), "user", "pass")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if resp.Error != 0 {
		t.Errorf("LoginResponse.Error = %d, want 0", resp.Error)
	}
	if err := c.VerifyAdminSession(context.Background()); err != nil {
		t.Fatalf("VerifyAdminSession: %v", err)
	}

	want := []string{"/login", "/auth/session"}
	if len(rec.paths) != len(want) {
		t.Fatalf("paths = %v, want %v", rec.paths, want)
	}
	for i, path := range want {
		if rec.paths[i] != path {
			t.Errorf("paths[%d] = %q, want %q", i, rec.paths[i], path)
		}
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(rec.bodies[0]), &body); err != nil {
		t.Fatalf("new login body is not JSON: %q", rec.bodies[0])
	}
	if body["login"] != "user" || body["password"] != "pass" {
		t.Errorf("new login body = %v", body)
	}
	if got := c.api().Token(); got != "jwt" {
		t.Errorf("token = %q, want jwt", got)
	}
}

func TestEngineAutoProbesAPIHostOnce(t *testing.T) {
	t.Setenv(EngineEnvVar, "auto")
	rec := &engineRouteRecorder{}
	c := newRoutedClient(t, rec)

	if got := c.Engine(); got != EngineNew {
		t.Fatalf("Engine = %q, want new (probe succeeded)", got)
	}
	if got := c.Engine(); got != EngineNew {
		t.Fatalf("second Engine = %q, want new", got)
	}
	probes := 0
	for _, path := range rec.paths {
		if strings.HasPrefix(path, "/sites/domain/") {
			probes++
		}
	}
	if probes != 1 {
		t.Errorf("site probes = %d, want 1 (verdict must be cached)", probes)
	}
}

func TestEngineAutoFallsBackToLegacy(t *testing.T) {
	t.Setenv(EngineEnvVar, "auto")
	srv := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	srv.Start()

	host := strings.TrimPrefix(srv.URL, "http://")
	c := New(host, WithHTTP(), WithAPIBaseURL(srv.URL))
	if got := c.Engine(); got != EngineLegacy {
		t.Errorf("Engine = %q, want legacy when the API host does not answer", got)
	}
}

// TestEngineAutoAsksAboutTheDomainNotTheHost pins the failure this probe was
// rewritten for: one API host fronts the whole zone and answers for every
// request, so a liveness check declared domains still on the ASP.NET engine
// migrated and their game lists came back empty.
func TestEngineAutoAsksAboutTheDomainNotTheHost(t *testing.T) {
	t.Setenv(EngineEnvVar, "auto")
	rec := &engineRouteRecorder{domainUnregistered: true}
	c := newRoutedClient(t, rec)

	if got := c.Engine(); got != EngineLegacy {
		t.Errorf("Engine = %q, want legacy: the host is alive but does not own this domain", got)
	}
	for _, path := range rec.paths {
		if path == "/version" {
			t.Error("the probe asked about the host instead of the domain")
		}
	}
}

func TestEngineAutoProbeAsksForTheClientDomain(t *testing.T) {
	t.Setenv(EngineEnvVar, "auto")
	var probed string
	srv := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probed = r.URL.Path
		_, _ = w.Write([]byte(`{"id":135,"name":"Демо город"}`))
	}))
	srv.Start()

	c := New("demo.en.cx", WithHTTP(), WithAPIBaseURL(srv.URL))
	if got := c.Engine(); got != EngineNew {
		t.Fatalf("Engine = %q, want new", got)
	}
	if probed != "/sites/domain/demo.en.cx" {
		t.Errorf("probe path = %q, want the client's own domain", probed)
	}
}

// TestEngineAutoIgnoresASiteWithoutAnID guards against a host that answers 200
// with an empty body being read as a migrated domain.
func TestEngineAutoIgnoresASiteWithoutAnID(t *testing.T) {
	t.Setenv(EngineEnvVar, "auto")
	srv := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	srv.Start()

	c := New("demo.en.cx", WithHTTP(), WithAPIBaseURL(srv.URL))
	if got := c.Engine(); got != EngineLegacy {
		t.Errorf("Engine = %q, want legacy when no site was identified", got)
	}
}

func TestSetEngineResetsAutoVerdict(t *testing.T) {
	t.Setenv(EngineEnvVar, "auto")
	rec := &engineRouteRecorder{}
	c := newRoutedClient(t, rec)

	if got := c.Engine(); got != EngineNew {
		t.Fatalf("Engine = %q, want new", got)
	}
	c.SetEngine(EngineLegacy)
	if got := c.Engine(); got != EngineLegacy {
		t.Errorf("Engine after SetEngine = %q, want legacy", got)
	}
	c.SetEngine(EngineAuto)
	if got := c.Engine(); got != EngineNew {
		t.Errorf("Engine after re-probe = %q, want new", got)
	}
}

// TestNewEngineRefusesADomainWithoutAHost pins the last path that bypassed the
// checks: an explicit "new" skips the auto probe entirely, so without a host of
// its own the client used to fall back to Encounter's default one and post the
// sign-in there under a foreign X-En-Domain.
func TestNewEngineRefusesADomainWithoutAHost(t *testing.T) {
	t.Setenv(EngineEnvVar, "")
	c := New("quest.example.com", WithEngine(EngineNew))

	if got := c.APIBaseURL(); got != "" {
		t.Errorf("APIBaseURL = %q, want empty for a domain outside the Encounter zones", got)
	}

	_, err := c.Login(context.Background(), "user", "pass")
	if err == nil {
		t.Fatal("Login was sent to a host that does not serve this domain")
	}
	if !enapi.IsMissingHost(err) {
		t.Fatalf("error = %v, want a MissingHostError", err)
	}
	if !strings.Contains(err.Error(), "quest.example.com") ||
		!strings.Contains(err.Error(), "ENCX_API_BASE_URL") {
		t.Errorf("error = %v, want it to name the domain and the remedy", err)
	}

	// Naming the host is the caller vouching for it, and unblocks the engine.
	withHost := New("quest.example.com", WithEngine(EngineNew),
		WithAPIBaseURL("https://api.quest.example.com"))
	if got := withHost.APIBaseURL(); got != "https://api.quest.example.com" {
		t.Errorf("APIBaseURL = %q, want the host the caller named", got)
	}
}

// TestSessionExportSurvivesAMissingAPIHost keeps the session round trip working
// for a domain that has no API host: there is simply nothing to store for it.
func TestSessionExportSurvivesAMissingAPIHost(t *testing.T) {
	t.Setenv(EngineEnvVar, "")
	c := New("quest.example.com", WithEngine(EngineNew))

	data, err := c.ExportCookies()
	if err != nil {
		t.Fatalf("ExportCookies: %v", err)
	}
	restored := New("quest.example.com", WithEngine(EngineNew))
	if err := restored.ImportCookies(data); err != nil {
		t.Fatalf("ImportCookies: %v", err)
	}
}
