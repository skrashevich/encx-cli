package encx

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// legacyGetGameStatistics fetches full game statistics from the ASP.NET
// engine via GET /gamestatistics/full/{gameId}?json=1.
func (c *Client) legacyGetGameStatistics(ctx context.Context, gameId int) (*GameStatisticsResponse, error) {
	u, err := url.Parse(fmt.Sprintf("%s/gamestatistics/full/%d", c.baseURL(), gameId))
	if err != nil {
		return nil, fmt.Errorf("encx: parse game statistics URL: %w", err)
	}

	q := u.Query()
	q.Set("json", "1")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("encx: create game statistics request: %w", err)
	}
	c.setHeaders(req)

	_, _, body, err := c.doRequestAndRead(req)
	if err != nil {
		return nil, fmt.Errorf("encx: game statistics request: %w", err)
	}

	var result GameStatisticsResponse
	if err := c.decodeJSON(body, &result, "game statistics"); err != nil {
		return nil, err
	}

	return &result, nil
}
