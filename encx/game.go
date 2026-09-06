package encx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// legacyGetGameModel retrieves the current game state from the ASP.NET engine.
func (c *Client) legacyGetGameModel(ctx context.Context, gameId int, formValues ...url.Values) (*GameModel, error) {
	if len(formValues) > 0 {
		return c.postGameModel(ctx, gameId, formValues...)
	}
	return c.getGameModel(ctx, gameId, nil)
}

// legacyGetGameModelLevel retrieves the state for a specific level number on
// the ASP.NET engine. Storm sequence games pass the level as a query parameter.
func (c *Client) legacyGetGameModelLevel(ctx context.Context, gameId, levelNumber int) (*GameModel, error) {
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

	status, headers, body, err := c.doRequestAndRead(req)
	if err != nil {
		return nil, fmt.Errorf("encx: game request: %w", err)
	}
	if err := c.gameNotFoundFromRedirect(gameId, status, headers); err != nil {
		return nil, err
	}

	return c.decodeGameModelJSON(body, status, "game model")
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

	status, headers, body, err := c.doRequestAndRead(req)
	if err != nil {
		return nil, fmt.Errorf("encx: game request: %w", err)
	}
	if err := c.gameNotFoundFromRedirect(gameId, status, headers); err != nil {
		return nil, err
	}

	return c.decodeGameModelJSON(body, status, "game model")
}

func (c *Client) decodeGameModelJSON(body []byte, statusCode int, context string) (*GameModel, error) {
	if trimmed := bytes.TrimLeft(body, " \t\r\n\uFEFF"); len(trimmed) > 0 && trimmed[0] == '<' {
		return nil, c.classifyHTMLGameResponse(trimmed, context)
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("encx: empty response (%s)", context)
	}
	var model GameModel
	if err := json.Unmarshal(body, &model); err != nil {
		// Carries the HTTP status so callers can tell an unreadable reply from a live engine
		// (2xx) apart from a proxy/gateway error page (non-2xx).
		return nil, &UndecodableResponseError{StatusCode: statusCode, Context: context, Err: err}
	}
	return &model, nil
}

// classifyHTMLGameResponse explains an HTML body where JSON was expected.
//
// The engine answers with HTML for several unrelated reasons, and they need
// different responses from the caller. Reporting all of them as an expired
// session sends players to re-login when their session is perfectly valid — the
// usual cause is the anti-spam wall after a burst of requests.
func (c *Client) classifyHTMLGameResponse(body []byte, context string) error {
	page := string(body)
	if isNotHumanRequest(page) {
		return newAntiSpamError(c.domain, c.scheme, "")
	}
	if looksLikeLoginPage(page) {
		return fmt.Errorf("encx: session expired or access denied (login page returned; try re-login)")
	}
	return fmt.Errorf(
		"encx: engine returned an HTML page instead of JSON (%s); the session may still be valid, retry shortly",
		context,
	)
}

// looksLikeLoginPage recognizes the Encounter sign-in form. The markers are the
// login form's own field names, which no game page carries.
func looksLikeLoginPage(page string) bool {
	lower := strings.ToLower(page)
	if strings.Contains(lower, "login.aspx") {
		return true
	}
	return strings.Contains(lower, "txtlogin") && strings.Contains(lower, "txtpassword")
}

// legacySendCode submits an answer via LevelAction.Answer (level, sectors,
// bonuses when the level has no active answer block rule).
func (c *Client) legacySendCode(ctx context.Context, gameId, levelId, levelNumber int, code string) (*GameModel, error) {
	form := url.Values{}
	form.Set("LevelId", strconv.Itoa(levelId))
	form.Set("LevelNumber", strconv.Itoa(levelNumber))
	form.Set("LevelAction.Answer", code)
	return c.postGameModel(ctx, gameId, form)
}

// legacySendBonusCode submits a bonus answer via BonusAction.Answer. The
// ASP.NET API requires this separate action when level answers are blocked.
func (c *Client) legacySendBonusCode(ctx context.Context, gameId, levelId, levelNumber int, code string) (*GameModel, error) {
	form := url.Values{}
	form.Set("LevelId", strconv.Itoa(levelId))
	form.Set("LevelNumber", strconv.Itoa(levelNumber))
	form.Set("BonusAction.Answer", code)
	return c.postGameModel(ctx, gameId, form)
}

// legacyGetPenaltyHint requests a penalty hint by its ID via a GET request
// with pid and pact=1 as query parameters.
func (c *Client) legacyGetPenaltyHint(ctx context.Context, gameId, penaltyId int) (*GameModel, error) {
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

	status, headers, body, err := c.doRequestAndRead(req)
	if err != nil {
		return nil, fmt.Errorf("encx: hint request: %w", err)
	}
	if err := c.gameNotFoundFromRedirect(gameId, status, headers); err != nil {
		return nil, err
	}

	return c.decodeGameModelJSON(body, status, "hint response")
}
