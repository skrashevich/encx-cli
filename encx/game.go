package encx

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// GetGameModel retrieves the current game state with the documented GET endpoint.
// Passing form values is retained for backward compatibility and performs a
// POST action request; prefer SendCode, SendBonusCode, or GetPenaltyHint.
func (c *Client) GetGameModel(ctx context.Context, gameId int, formValues ...url.Values) (*GameModel, error) {
	if len(formValues) > 0 {
		return c.postGameModel(ctx, gameId, formValues...)
	}
	return c.getGameModel(ctx, gameId, nil)
}

// GetGameModelLevel retrieves the state for a specific level number. This is
// used by storm sequence games where the API accepts a level query parameter.
func (c *Client) GetGameModelLevel(ctx context.Context, gameId, levelNumber int) (*GameModel, error) {
	q := url.Values{}
	if levelNumber > 0 {
		q.Set("level", strconv.Itoa(levelNumber))
	}
	return c.getGameModel(ctx, gameId, q)
}

func (c *Client) getGameModel(ctx context.Context, gameId int, extraQuery url.Values) (*GameModel, error) {
	u, err := url.Parse(fmt.Sprintf("%s/gameengines/encounter/play/%d", c.baseURL(), gameId))
	if err != nil {
		return nil, fmt.Errorf("encx: parse game URL: %w", err)
	}

	q := u.Query()
	q.Set("json", "1")
	q.Set("lang", c.lang)
	for key, values := range extraQuery {
		for _, value := range values {
			q.Add(key, value)
		}
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("encx: create game request: %w", err)
	}
	c.setHeaders(req)

	_, _, body, err := c.doRequestAndRead(req)
	if err != nil {
		return nil, fmt.Errorf("encx: game request: %w", err)
	}

	return decodeGameModelJSON(body, "game model")
}

func (c *Client) postGameModel(ctx context.Context, gameId int, formValues ...url.Values) (*GameModel, error) {
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
		maps.Copy(merged, fv)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), strings.NewReader(merged.Encode()))
	if err != nil {
		return nil, fmt.Errorf("encx: create game request: %w", err)
	}
	c.setHeaders(req)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	_, _, body, err := c.doRequestAndRead(req)
	if err != nil {
		return nil, fmt.Errorf("encx: game request: %w", err)
	}

	return decodeGameModelJSON(body, "game model")
}

func decodeGameModelJSON(body []byte, context string) (*GameModel, error) {
	if len(body) > 0 && body[0] == '<' {
		return nil, fmt.Errorf("encx: session expired or access denied (server returned HTML instead of JSON; try re-login)")
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("encx: empty response (%s)", context)
	}
	var model GameModel
	if err := json.Unmarshal(body, &model); err != nil {
		return nil, fmt.Errorf("encx: decode %s: %w", context, err)
	}
	return &model, nil
}

// SendCode submits an answer via LevelAction.Answer (level, sectors, bonuses
// when the level has no active answer block rule).
func (c *Client) SendCode(ctx context.Context, gameId, levelId, levelNumber int, code string) (*GameModel, error) {
	form := url.Values{}
	form.Set("LevelId", strconv.Itoa(levelId))
	form.Set("LevelNumber", strconv.Itoa(levelNumber))
	form.Set("LevelAction.Answer", code)
	return c.postGameModel(ctx, gameId, form)
}

// SendBonusCode submits a bonus answer via BonusAction.Answer. The Encounter API
// requires this separate action when level answers are blocked.
func (c *Client) SendBonusCode(ctx context.Context, gameId, levelId, levelNumber int, code string) (*GameModel, error) {
	form := url.Values{}
	form.Set("LevelId", strconv.Itoa(levelId))
	form.Set("LevelNumber", strconv.Itoa(levelNumber))
	form.Set("BonusAction.Answer", code)
	return c.postGameModel(ctx, gameId, form)
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

	_, _, body, err := c.doRequestAndRead(req)
	if err != nil {
		return nil, fmt.Errorf("encx: hint request: %w", err)
	}

	return decodeGameModelJSON(body, "hint response")
}
