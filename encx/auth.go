package encx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// Login authenticates the user on the Encounter domain.
// On success (Error == 0), session cookies are stored in the client's cookie jar
// and used for subsequent requests.
//
// Optional LoginOptions can be passed to specify network or CAPTCHA digits.
func (c *Client) Login(ctx context.Context, login, password string, opts ...LoginOptions) (*LoginResponse, error) {
	u, err := url.Parse(c.baseURL() + "/login/signin")
	if err != nil {
		return nil, fmt.Errorf("encx: parse login URL: %w", err)
	}

	q := u.Query()
	q.Set("json", "1")
	q.Set("lang", c.lang)
	u.RawQuery = q.Encode()

	payload := map[string]string{
		"Login":    login,
		"Password": password,
	}

	if len(opts) > 0 {
		opt := opts[0]
		if opt.Network > 0 {
			payload["ddlNetwork"] = strconv.Itoa(opt.Network)
		}
		if opt.MagicNumbers != "" {
			payload["MagicNumbers"] = opt.MagicNumbers
		}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encx: marshal login body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("encx: create login request: %w", err)
	}
	c.setHeaders(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("encx: login request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("encx: login request failed with HTTP %d", resp.StatusCode)
	}

	var result LoginResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("encx: decode login response: %w", err)
	}

	return &result, nil
}
