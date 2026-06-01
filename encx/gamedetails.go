package encx

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// EnterGame registers the player in a game (application / fee confirmation).
// Primary: GET /MakeGameFee.aspx?gid={id}&confirm=yes (e.g. tech.en.cx/MakeGameFee.aspx?gid=81793&confirm=yes).
// Fallback: POST /gameengines/encounter/makefee/Login.aspx on legacy hosts.
func (c *Client) EnterGame(ctx context.Context, gameId int) (string, error) {
	body, err := c.enterGameViaMakeGameFee(ctx, gameId)
	if err == nil {
		return body, nil
	}
	if !isEnterGameHTTP404(err) {
		return "", err
	}
	return c.enterGameViaEngineMakefee(ctx, gameId)
}

func (c *Client) enterGameViaMakeGameFee(ctx context.Context, gameId int) (string, error) {
	rawURL, err := c.makeGameFeeURL(gameId, true)
	if err != nil {
		return "", err
	}
	return c.enterGameFeeRequest(ctx, http.MethodGet, rawURL, "")
}

func (c *Client) makeGameFeeURL(gameId int, confirm bool) (string, error) {
	u, err := url.Parse(c.baseURL() + "/MakeGameFee.aspx")
	if err != nil {
		return "", fmt.Errorf("encx: parse MakeGameFee URL: %w", err)
	}
	q := u.Query()
	q.Set("gid", strconv.Itoa(gameId))
	if confirm {
		q.Set("confirm", "yes")
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (c *Client) enterGameViaEngineMakefee(ctx context.Context, gameId int) (string, error) {
	u, err := url.Parse(c.baseURL() + "/gameengines/encounter/makefee/Login.aspx")
	if err != nil {
		return "", fmt.Errorf("encx: parse enter game URL: %w", err)
	}

	q := u.Query()
	q.Set("json", "1")
	q.Set("lang", c.lang)
	u.RawQuery = q.Encode()

	form := url.Values{}
	form.Set("gid", strconv.Itoa(gameId))

	return c.enterGameFeeRequest(ctx, http.MethodPost, u.String(), form.Encode())
}

func (c *Client) enterGameFeeRequest(ctx context.Context, method, rawURL, formBody string) (string, error) {
	const maxRedirects = 5
	currentURL := rawURL
	postBody := formBody

	for attempt := 0; attempt < maxRedirects; attempt++ {
		var reader io.Reader
		if method == http.MethodPost && postBody != "" {
			reader = strings.NewReader(postBody)
		}

		req, err := http.NewRequestWithContext(ctx, method, currentURL, reader)
		if err != nil {
			return "", fmt.Errorf("encx: create enter game request: %w", err)
		}
		c.setHeaders(req)
		if method == http.MethodPost && postBody != "" {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return "", fmt.Errorf("encx: enter game request: %w", err)
		}

		respBody, readErr := c.readResponseBody(resp)
		resp.Body.Close()
		if readErr != nil {
			return "", readErr
		}

		if isRedirectStatus(resp.StatusCode) {
			location := resp.Header.Get("Location")
			if isLoginRedirect(location) {
				return "", fmt.Errorf("encx: session expired or access denied (redirect to login)")
			}
			if location == "" {
				return "", fmt.Errorf("encx: enter game redirect without Location (HTTP %d)", resp.StatusCode)
			}
			nextURL, err := resolveAgainstBase(c.baseURL(), location)
			if err != nil {
				return "", fmt.Errorf("encx: resolve enter game redirect: %w", err)
			}
			currentURL = nextURL
			method = http.MethodGet
			postBody = ""
			continue
		}

		if resp.StatusCode == http.StatusNotFound {
			return "", fmt.Errorf("encx: enter game failed with HTTP 404 (endpoint missing on %s)", c.domain)
		}
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("encx: enter game failed with HTTP %d", resp.StatusCode)
		}

		return string(respBody), nil
	}

	return "", fmt.Errorf("encx: enter game: too many redirects")
}

func isEnterGameHTTP404(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "HTTP 404")
}

func isLoginRedirect(location string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(location)), "login.aspx")
}

func resolveAgainstBase(baseURL, location string) (string, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	ref, err := url.Parse(location)
	if err != nil {
		return "", err
	}
	return base.ResolveReference(ref).String(), nil
}

// GetGameDetails fetches the game details/statistics page.
// Returns raw HTML that can be parsed for game information and stats.
func (c *Client) GetGameDetails(ctx context.Context, gameId int) (string, error) {
	return c.doGet(ctx, fmt.Sprintf("%s/GameDetails.aspx?gid=%d", c.baseURL(), gameId))
}
