package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/skrashevich/encx-cli/encx"
)

func TestFetchURLSession(t *testing.T) {
	for _, tc := range []struct {
		name, site, target, redirect, session, cookie, auth string
	}{
		{name: "Encounter with cookies", site: "city.en.cx", target: "city.en.cx", session: `[{"name":"session","value":"secret","path":"/"}]`, cookie: "session=secret"},
		{name: "Encounter without auth", site: "city.en.cx", target: "city.en.cx"},
		{name: "shared Encounter cookie", site: "city.en.cx", target: "other.en.cx", session: `[{"name":"session","value":"secret","path":"/","domain":".en.cx"}]`, cookie: "session=secret"},
		{name: "custom site", site: "quest.example.co.uk", target: "quest.example.co.uk", session: `[{"name":"session","value":"secret","path":"/"}]`, cookie: "session=secret"},
		{name: "external URL", site: "quest.example.co.uk", target: "outside.example.co.uk", session: `[{"name":"session","value":"secret","path":"/","domain":".example.co.uk"}]`},
		{name: "Encounter redirect outside", site: "city.en.cx", target: "city.en.cx", redirect: "outside.test", session: `[{"name":"session","value":"secret","path":"/"}]`, cookie: "session=secret"},
		{name: "custom redirect to subdomain", site: "quest.example.co.uk", target: "quest.example.co.uk", redirect: "outside.quest.example.co.uk", session: `[{"name":"session","value":"secret","path":"/","domain":".quest.example.co.uk"}]`, cookie: "session=secret"},
		{name: "API bearer", site: "city.en.cx", target: "api.example.test", session: `{"apiToken":"secret"}`, auth: "Bearer secret"},
		{name: "API redirect to subdomain", site: "city.en.cx", target: "api.example.test", redirect: "outside.api.example.test", session: `{"apiToken":"secret"}`, auth: "Bearer secret"},
		{name: "API token stays off site", site: "city.en.cx", target: "city.en.cx", session: `{"apiToken":"secret"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hits := 0
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits++
				wantUA := "encounter-session-test"
				if tc.name == "external URL" || r.Host == tc.redirect {
					wantUA = agentUserAgent
				}
				if got := r.UserAgent(); got != wantUA {
					t.Errorf("%s User-Agent = %q, want %q", r.Host, got, wantUA)
				}
				wantCookie, wantAuth := tc.cookie, tc.auth
				if r.Host == tc.redirect {
					wantCookie, wantAuth = "", ""
				}
				if got := r.Header.Get("Cookie"); got != wantCookie {
					t.Errorf("%s Cookie = %q, want %q", r.Host, got, wantCookie)
				}
				if got := r.Header.Get("Authorization"); got != wantAuth {
					t.Errorf("%s Authorization = %q, want %q", r.Host, got, wantAuth)
				}
				if wantAuth == "" && r.Header.Get("X-En-Domain") != "" {
					t.Errorf("site header leaked to %s", r.Host)
				}
				if tc.redirect != "" && r.Host == tc.target {
					http.Redirect(w, r, "https://"+tc.redirect+"/final", http.StatusFound)
					return
				}
				w.Header().Set("Content-Type", "text/plain")
				fmt.Fprint(w, "private page")
			}))
			defer srv.Close()
			transport := srv.Client().Transport.(*http.Transport).Clone()
			transport.TLSClientConfig = transport.TLSClientConfig.Clone()
			transport.TLSClientConfig.ServerName = "example.com"
			transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
			}
			defer transport.CloseIdleConnections()
			previous := fetchHTTPClient
			clientCopy := *previous
			clientCopy.Transport = transport
			fetchHTTPClient = &clientCopy
			t.Cleanup(func() { fetchHTTPClient = previous })
			mode := encx.EngineLegacy
			if strings.Contains(tc.session, "apiToken") {
				mode = encx.EngineNew
			}
			client := encx.New(tc.site, encx.WithEngine(mode), encx.WithUserAgent("encounter-session-test"), encx.WithAPIBaseURL("https://api.example.test"))
			if tc.session != "" {
				if err := client.ImportCookies([]byte(tc.session)); err != nil {
					t.Fatal(err)
				}
			}
			raw := executeToolCallSafe(t.Context(), &config{}, client, &llmSession{securityMode: SecurityModeFull}, "fetch_url", fmt.Sprintf(`{"url":"https://%s/page"}`, tc.target))
			var result map[string]any
			if err := json.Unmarshal([]byte(raw), &result); err != nil {
				t.Fatal(err)
			}
			if result["content"] != "private page" {
				t.Fatalf("result = %s", raw)
			}
			wantHits := 1
			if tc.redirect != "" {
				wantHits = 2
				if !strings.Contains(result["url"].(string), tc.redirect) {
					t.Errorf("wrong final URL: %v", result["url"])
				}
			}
			if hits != wantHits {
				t.Errorf("requests = %d, want %d", hits, wantHits)
			}
		})
	}
}

// The authenticated path must retain fetch_url's dial-time SSRF protection.
func TestFetchURLSessionBlocksPrivateHost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("authenticated fetch reached a private address")
	}))
	defer srv.Close()
	client := encx.New(strings.TrimPrefix(srv.URL, "http://"), encx.WithHTTP(), encx.WithEngine(encx.EngineLegacy))
	if err := client.ImportCookies([]byte(`[{"name":"session","value":"secret","path":"/"}]`)); err != nil {
		t.Fatal(err)
	}
	raw := executeToolCallSafe(t.Context(), &config{}, client, &llmSession{securityMode: SecurityModeFull}, "fetch_url", fmt.Sprintf(`{"url":%q}`, srv.URL))
	if !strings.Contains(raw, "non-public address") {
		t.Fatalf("expected SSRF rejection, got %s", raw)
	}
}
