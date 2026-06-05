package encx

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var (
	// Mobile format: <h1 class="gametitle"><a href="...details/{id}...">Title</a></h1>
	gameTitleMobileRe = regexp.MustCompile(`(?i)<h1[^>]*class="gametitle"[^>]*>\s*<a[^>]*href="[^"]*details/(\d+)[^"]*"[^>]*>([^<]+)</a>`)
	// Desktop format: <a ... id="lnkGameTitle" href="/GameDetails.aspx?gid=12345">Title</a>
	gameTitleDesktopRe = regexp.MustCompile(`(?i)<a[^>]*\bid="lnkGameTitle"[^>]*href="[^"]*gid=(\d+)[^"]*"[^>]*>([^<]+)</a>`)
	// Matches "StartCounter":12345, in HTML/JSON response
	startCounterRe = regexp.MustCompile(`"StartCounter"\s*:\s*(\d+)`)
)

// GetDomainGames fetches the domain's list of available games.
// It prefers the JSON endpoint (/home/?json=1), then mobile HTML, then desktop HTML.
func (c *Client) GetDomainGames(ctx context.Context) ([]DomainGame, error) {
	c.debugf("encx games: trying JSON catalog at %s/home/?json=1", c.baseURL())
	list, err := c.GetGameList(ctx)
	if err == nil {
		games := domainGamesFromList(list)
		if len(games) > 0 {
			c.debugf("encx games: JSON catalog returned %d game(s)", len(games))
			return games, nil
		}
		c.debugf("encx games: JSON catalog returned 0 game(s), falling back to HTML")
	} else {
		c.debugf("encx games: JSON catalog failed: %v", err)
	}

	// Try mobile HTML, fall back to desktop HTML.
	c.debugf("encx games: trying mobile catalog at %s/", c.mobileBaseURL())
	games, err := c.fetchGames(ctx, c.mobileBaseURL()+"/", gameTitleMobileRe)
	if err == nil && len(games) > 0 {
		c.debugf("encx games: mobile catalog returned %d game(s)", len(games))
		return games, nil
	}
	if err != nil {
		c.debugf("encx games: mobile catalog failed: %v", err)
	} else {
		c.debugf("encx games: mobile catalog returned 0 game(s), falling back to desktop")
	}

	c.debugf("encx games: trying desktop catalog at %s/", c.baseURL())
	return c.fetchGames(ctx, c.baseURL()+"/", gameTitleDesktopRe)
}

func domainGamesFromList(list *GameListResponse) []DomainGame {
	if list == nil {
		return nil
	}
	seen := map[int]bool{}
	games := make([]DomainGame, 0, len(list.ActiveGames)+len(list.ComingGames))
	appendGames := func(infos []GameInfo) {
		for _, g := range infos {
			if g.GameID == 0 || seen[g.GameID] {
				continue
			}
			seen[g.GameID] = true
			games = append(games, DomainGame{
				Title:  strings.TrimSpace(g.Title),
				GameId: g.GameID,
			})
		}
	}
	appendGames(list.ActiveGames)
	appendGames(list.ComingGames)
	return games
}

func (c *Client) fetchGames(ctx context.Context, u string, re *regexp.Regexp) ([]DomainGame, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("encx: create domain games request: %w", err)
	}
	c.setHeaders(req)

	_, _, body, err := c.doRequestAndRead(req)
	if err != nil {
		return nil, fmt.Errorf("encx: domain games request: %w", err)
	}

	matches := re.FindAllSubmatch(body, -1)
	seen := map[int]bool{}
	games := make([]DomainGame, 0, len(matches))
	for _, m := range matches {
		id, err := strconv.Atoi(string(m[1]))
		if err != nil || seen[id] {
			continue
		}
		seen[id] = true
		title := strings.TrimSpace(string(m[2]))
		games = append(games, DomainGame{Title: title, GameId: id})
	}

	return games, nil
}

// GetGameList fetches the full game list via the JSON endpoint GET /home/?json=1.
// Returns structured data with ComingGames and ActiveGames.
// An optional page number can be passed for pagination (1-based).
func (c *Client) GetGameList(ctx context.Context, page ...int) (*GameListResponse, error) {
	pageNum := 1
	if len(page) > 0 && page[0] > 0 {
		pageNum = page[0]
	}
	c.debugf("encx game-list: requesting page=%d", pageNum)

	u, err := url.Parse(c.baseURL() + "/home/")
	if err != nil {
		return nil, fmt.Errorf("encx: parse game list URL: %w", err)
	}

	q := u.Query()
	q.Set("json", "1")
	if len(page) > 0 && page[0] > 0 {
		q.Set("page", strconv.Itoa(page[0]))
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("encx: create game list request: %w", err)
	}
	c.setHeaders(req)

	_, _, body, err := c.doRequestAndRead(req)
	if err != nil {
		return nil, fmt.Errorf("encx: game list request: %w", err)
	}

	var result GameListResponse
	if err := c.decodeJSON(body, &result, "game list"); err != nil {
		return nil, err
	}

	return &result, nil
}

// GetTimeoutToGame fetches the game page (HTML) and extracts the StartCounter value,
// which indicates seconds until the game starts. Returns nil if no counter is found.
func (c *Client) GetTimeoutToGame(ctx context.Context, gameId int) (*int, error) {
	u, err := url.Parse(fmt.Sprintf("%s/gameengines/encounter/play/%d", c.mobileBaseURL(), gameId))
	if err != nil {
		return nil, fmt.Errorf("encx: parse timeout URL: %w", err)
	}

	q := u.Query()
	q.Set("lang", c.lang)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("encx: create timeout request: %w", err)
	}
	c.setHeaders(req)

	_, _, body, err := c.doRequestAndRead(req)
	if err != nil {
		return nil, fmt.Errorf("encx: timeout request: %w", err)
	}

	match := startCounterRe.FindSubmatch(body)
	if match == nil {
		return nil, nil
	}

	val, err := strconv.Atoi(string(match[1]))
	if err != nil {
		return nil, nil
	}

	return &val, nil
}
