package encx

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type savedCookie struct {
	Name     string    `json:"name"`
	Value    string    `json:"value"`
	Path     string    `json:"path"`
	Domain   string    `json:"domain"`
	Expires  time.Time `json:"expires"`
	Secure   bool      `json:"secure"`
	HttpOnly bool      `json:"httpOnly"`
}

// savedSession is the export format used once the new engine is involved: the
// JWT lives outside the cookie jar, and the en_access cookie belongs to the API
// host rather than the site domain, so neither survives a bare cookie array.
type savedSession struct {
	Cookies    []savedCookie `json:"cookies"`
	APICookies []savedCookie `json:"apiCookies,omitempty"`
	APIToken   string        `json:"apiToken,omitempty"`
}

// ExportCookies serializes the client's session.
//
// With no new-engine state to carry it emits the historical cookie array, so
// sessions stay readable by older builds; otherwise it emits a session object.
func (c *Client) ExportCookies() ([]byte, error) {
	siteCookies := c.cookiesFor(c.baseURL())
	apiCookies := c.cookiesFor(c.APIBaseURL())
	token := c.apiToken()

	if token == "" && len(apiCookies) == 0 {
		return json.Marshal(siteCookies)
	}
	return json.Marshal(savedSession{
		Cookies:    siteCookies,
		APICookies: apiCookies,
		APIToken:   token,
	})
}

// ImportCookies restores a session produced by ExportCookies. Both the legacy
// cookie array and the session object are accepted.
func (c *Client) ImportCookies(data []byte) error {
	trimmed := bytes.TrimLeft(data, " \t\r\n")
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var saved []savedCookie
		if err := json.Unmarshal(data, &saved); err != nil {
			return err
		}
		c.restoreCookies(c.baseURL(), saved)
		return nil
	}

	var session savedSession
	if err := json.Unmarshal(data, &session); err != nil {
		return err
	}
	c.restoreCookies(c.baseURL(), session.Cookies)
	c.restoreCookies(c.APIBaseURL(), session.APICookies)
	if session.APIToken != "" {
		c.api().SetToken(session.APIToken)
	}
	return nil
}

// APIToken returns the new engine's bearer token, empty on the legacy engine.
func (c *Client) APIToken() string { return c.apiToken() }

// SetAPIToken installs a previously obtained new-engine bearer token.
func (c *Client) SetAPIToken(token string) { c.api().SetToken(token) }

func (c *Client) apiToken() string {
	c.apiMu.RLock()
	created := c.apiClient != nil
	c.apiMu.RUnlock()
	if !created {
		return ""
	}
	return c.api().Token()
}

func (c *Client) cookiesFor(rawURL string) []savedCookie {
	if strings.TrimSpace(rawURL) == "" {
		return nil
	}
	u, err := url.Parse(rawURL)
	if err != nil || c.httpClient.Jar == nil {
		return nil
	}
	cookies := c.httpClient.Jar.Cookies(u)
	saved := make([]savedCookie, len(cookies))
	for i, ck := range cookies {
		saved[i] = savedCookie{
			Name:     ck.Name,
			Value:    ck.Value,
			Path:     ck.Path,
			Domain:   ck.Domain,
			Expires:  ck.Expires,
			Secure:   ck.Secure,
			HttpOnly: ck.HttpOnly,
		}
	}
	return saved
}

func (c *Client) restoreCookies(rawURL string, saved []savedCookie) {
	if len(saved) == 0 || c.httpClient.Jar == nil || strings.TrimSpace(rawURL) == "" {
		return
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return
	}
	cookies := make([]*http.Cookie, len(saved))
	for i, s := range saved {
		cookies[i] = &http.Cookie{
			Name:     s.Name,
			Value:    s.Value,
			Path:     s.Path,
			Domain:   s.Domain,
			Expires:  s.Expires,
			Secure:   s.Secure,
			HttpOnly: s.HttpOnly,
		}
	}
	c.httpClient.Jar.SetCookies(u, cookies)
}
