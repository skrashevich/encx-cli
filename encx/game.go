package encx

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// GetGameModel retrieves the current game state by posting to the game engine.
// Additional form values can be passed to perform actions (send codes, etc.).
func (c *Client) GetGameModel(ctx context.Context, gameId int, formValues ...url.Values) (*GameModel, error) {
	u, err := url.Parse(fmt.Sprintf("%s/gameengines/encounter/play/%d", c.baseURL(), gameId))
	if err != nil {
		return nil, fmt.Errorf("encx: parse game URL: %w", err)
	}

	q := u.Query()
	q.Set("json", "1")
	q.Set("lang", c.lang)
	u.RawQuery = q.Encode()

	merged := url.Values{}
	for _, fv := range formValues {
		for k, vs := range fv {
			for _, v := range vs {
				merged.Set(k, v)
			}
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), strings.NewReader(merged.Encode()))
	if err != nil {
		return nil, fmt.Errorf("encx: create game request: %w", err)
	}
	c.setHeaders(req)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("encx: game request: %w", err)
	}
	defer resp.Body.Close()

	var model GameModel
	if err := json.NewDecoder(resp.Body).Decode(&model); err != nil {
		return nil, fmt.Errorf("encx: decode game model: %w", err)
	}

	return &model, nil
}

// SendCode submits a level code answer.
func (c *Client) SendCode(ctx context.Context, gameId, levelId int, code string) (*GameModel, error) {
	form := url.Values{}
	form.Set("LevelId", strconv.Itoa(levelId))
	form.Set("LevelAction.Answer", code)
	return c.GetGameModel(ctx, gameId, form)
}

// SendBonusCode submits a bonus code answer.
func (c *Client) SendBonusCode(ctx context.Context, gameId, levelId int, code string) (*GameModel, error) {
	form := url.Values{}
	form.Set("LevelId", strconv.Itoa(levelId))
	form.Set("BonusAction.Answer", code)
	return c.GetGameModel(ctx, gameId, form)
}

// GetPenaltyHint requests a penalty hint by its ID.
// This uses a GET request with pid and pact=1 as query parameters.
func (c *Client) GetPenaltyHint(ctx context.Context, gameId, penaltyId int) (*GameModel, error) {
	u, err := url.Parse(fmt.Sprintf("%s/gameengines/encounter/play/%d", c.baseURL(), gameId))
	if err != nil {
		return nil, fmt.Errorf("encx: parse hint URL: %w", err)
	}

	q := u.Query()
	q.Set("json", "1")
	q.Set("lang", c.lang)
	q.Set("pid", strconv.Itoa(penaltyId))
	q.Set("pact", "1")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("encx: create hint request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("encx: hint request: %w", err)
	}
	defer resp.Body.Close()

	var model GameModel
	if err := json.NewDecoder(resp.Body).Decode(&model); err != nil {
		return nil, fmt.Errorf("encx: decode hint response: %w", err)
	}

	return &model, nil
}
