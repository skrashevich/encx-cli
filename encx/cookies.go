package encx

import (
	"encoding/json"
	"net/http"
	"net/url"
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

// ExportCookies serializes the client's cookies for the configured domain to JSON.
func (c *Client) ExportCookies() ([]byte, error) {
	u, _ := url.Parse(c.baseURL())
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

	return json.Marshal(saved)
}

// ImportCookies loads cookies from JSON data into the client's cookie jar.
func (c *Client) ImportCookies(data []byte) error {
	var saved []savedCookie
	if err := json.Unmarshal(data, &saved); err != nil {
		return err
	}

	u, _ := url.Parse(c.baseURL())
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
	return nil
}
