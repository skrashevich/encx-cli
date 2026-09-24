package encx

import (
	"net/http"
	"net/url"
	"strings"
)

// SessionHTTPClient reuses the current Encounter session with the caller's
// transport, timeout and redirect policy. It does not log in or probe an engine.
// Cookies retain their jar scope; bearer credentials are sent only to the exact
// API origin. Unrelated hosts receive no session credentials, including after
// redirects. The supplied client is not modified.
func (c *Client) SessionHTTPClient(base *http.Client) *http.Client {
	client := *base
	client.Jar = &encounterSessionJar{client: c}
	transport := base.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	client.Transport = &encounterSessionTransport{client: c, base: transport}
	return &client
}

func (c *Client) sessionURL(u *url.URL) bool {
	if u == nil || (u.Scheme != "https" && u.Scheme != "http") {
		return false
	}
	site, err := url.Parse(c.baseURL())
	if err == nil && strings.EqualFold(u.Host, site.Host) {
		return true
	}
	// Use the engine's known zones, never a guessed registrable parent of a
	// custom domain: a sibling (or delegated subdomain) may belong to anyone.
	if encounterZoneOf(u.Hostname()) != "" {
		return true
	}
	return c.sessionAPIURL(u)
}

func (c *Client) sessionAPIURL(u *url.URL) bool {
	api, err := url.Parse(c.APIBaseURL())
	return err == nil && api.Host != "" && u.Scheme == api.Scheme && strings.EqualFold(u.Host, api.Host)
}

type encounterSessionJar struct{ client *Client }

func (j *encounterSessionJar) Cookies(u *url.URL) []*http.Cookie {
	if j.client.httpClient.Jar == nil || !j.client.sessionURL(u) {
		return nil
	}
	return j.client.httpClient.Jar.Cookies(u)
}

func (j *encounterSessionJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	if j.client.httpClient.Jar != nil && j.client.sessionURL(u) {
		j.client.httpClient.Jar.SetCookies(u, cookies)
	}
}

type encounterSessionTransport struct {
	client *Client
	base   http.RoundTripper
}

func (t *encounterSessionTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	// Recompute credentials for every hop. net/http otherwise forwards sensitive
	// headers to subdomains, which need not be part of the configured engine.
	req.Header.Del("Authorization")
	req.Header.Del("Proxy-Authorization")
	req.Header.Del("X-En-Domain")
	if t.client.sessionURL(req.URL) {
		// Legacy Encounter sessions are bound to the login User-Agent.
		// Reuse the client's headers along with its cookies on every hop.
		t.client.setHeaders(req)
	} else {
		req.Header.Del("Cookie")
	}
	if t.client.sessionAPIURL(req.URL) && t.client.apiToken() != "" {
		t.client.api().SetSessionHeaders(req)
	}
	return t.base.RoundTrip(req)
}
