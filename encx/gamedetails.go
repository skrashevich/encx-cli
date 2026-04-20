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

// EnterGame registers the player in a game (fee/confirmation step).
// This corresponds to the /gameengines/encounter/makefee/Login.aspx endpoint.
func (c *Client) EnterGame(ctx context.Context, gameId int) (string, error) {
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

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("encx: create enter game request: %w", err)
	}
	c.setHeaders(req)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("encx: enter game request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("encx: enter game failed with HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("encx: read enter game body: %w", err)
	}

	return string(body), nil
}

// GetGameDetails fetches the game details/statistics page.
// Returns raw HTML that can be parsed for game information and stats.
func (c *Client) GetGameDetails(ctx context.Context, gameId int) (string, error) {
	return c.doGet(ctx, fmt.Sprintf("%s/GameDetails.aspx?gid=%d", c.baseURL(), gameId))
}
