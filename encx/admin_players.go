package encx

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// AdminGamePlayer is a participation entry, not a user or team account.
// PlayerID is the engine's entry ID; zero means the page did not expose it.
// CanDelete reflects the current engine permission (started players may only
// be disqualified). Status is -1 when the legacy page does not expose it.
type AdminGamePlayer struct {
	PlayerID  int    `json:"player_id"`
	Name      string `json:"name"`
	UserID    int    `json:"user_id,omitempty"`
	TeamID    int    `json:"team_id,omitempty"`
	Status    int    `json:"status"`
	CanDelete bool   `json:"can_delete"`
	deleteURL string
}

func (c *Client) AdminGetPlayers(ctx context.Context, gameID int) ([]AdminGamePlayer, error) {
	if gameID <= 0 {
		return nil, fmt.Errorf("encx: game ID must be positive")
	}
	return c.engine(ctx).AdminGetPlayers(ctx, gameID)
}

// AdminDeletePlayer removes a single request/participant and reads the entire
// list back. It never disqualifies a player or deletes the game as a fallback.
func (c *Client) AdminDeletePlayer(ctx context.Context, gameID, playerID int) error {
	if gameID <= 0 || playerID <= 0 {
		return fmt.Errorf("encx: game and player IDs must be positive")
	}
	return c.engine(ctx).AdminDeletePlayer(ctx, gameID, playerID)
}

func playerToDelete(players []AdminGamePlayer, id int) (*AdminGamePlayer, error) {
	for _, p := range players {
		if p.PlayerID == id {
			if !p.CanDelete {
				return nil, fmt.Errorf("encx: participant %d cannot be deleted in the current game state", id)
			}
			return &p, nil
		}
	}
	return nil, fmt.Errorf("encx: participant %d not found in the complete game participant list", id)
}

func verifyPlayerDeleted(players []AdminGamePlayer, id int) error {
	for _, p := range players {
		// Losing management permission must not masquerade as disappearance.
		if p.PlayerID == 0 {
			return fmt.Errorf("encx: cannot verify deletion: participant IDs are no longer visible")
		}
		if p.PlayerID == id {
			return fmt.Errorf("encx: participant %d remains after deletion; the engine refused the change", id)
		}
	}
	return nil
}

func (e *legacyEngine) AdminGetPlayers(ctx context.Context, gameID int) ([]AdminGamePlayer, error) {
	c := e.c
	start := fmt.Sprintf("%s/GamePlayers.aspx?gid=%d&status=-1&sort=1&page=1", c.baseURL(), gameID)
	queue := []string{start}
	visited := map[string]bool{}
	seen := map[int]bool{}
	players := []AdminGamePlayer{}
	total := -1
	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]
		if visited[u] {
			continue
		}
		visited[u] = true
		if len(visited) > 1000 {
			return nil, fmt.Errorf("encx: participant pagination exceeds 1000 pages")
		}
		body, err := c.doGet(ctx, u)
		if err != nil {
			return nil, err
		}
		rows, links, count, err := parseAdminPlayers(body, c.baseURL(), gameID)
		if err != nil {
			return nil, err
		}
		if total >= 0 && count != total {
			return nil, fmt.Errorf("encx: participant list changed during pagination; retry reading it")
		}
		total = count
		for _, p := range rows {
			if p.PlayerID != 0 && seen[p.PlayerID] {
				continue
			}
			if p.PlayerID != 0 {
				seen[p.PlayerID] = true
			}
			players = append(players, p)
		}
		queue = append(queue, links...)
	}
	if len(players) != total {
		return nil, fmt.Errorf("encx: incomplete participant list: read %d of %d", len(players), total)
	}
	return players, nil
}

func (e *legacyEngine) AdminDeletePlayer(ctx context.Context, gameID, playerID int) error {
	players, err := e.AdminGetPlayers(ctx, gameID)
	if err != nil {
		return err
	}
	p, err := playerToDelete(players, playerID)
	if err != nil {
		return err
	}
	if _, err = e.c.doGet(ctx, p.deleteURL); err != nil {
		return err
	}
	players, err = e.AdminGetPlayers(ctx, gameID)
	if err != nil {
		return fmt.Errorf("encx: deletion sent, verification failed: %w", err)
	}
	return verifyPlayerDeleted(players, playerID)
}

// parseAdminPlayers accepts only the complete management view. A public or
// login page cannot be interpreted as an empty list. Action URLs are taken
// from the engine's own row, constrained to this origin and game.
func parseAdminPlayers(body, base string, gameID int) ([]AdminGamePlayer, []string, int, error) {
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return nil, nil, 0, err
	}
	total := -1
	all := false
	rows := []AdminGamePlayer{}
	pages := []string{}
	walkPlayerHTML(doc, func(n *html.Node) {
		id := playerAttr(n, "id")
		if id == "spanTotal" {
			if v, e := strconv.Atoi(strings.TrimSpace(playerText(n))); e == nil {
				total = v
			}
		}
		if id == "lnkPlayersAll" && playerAttr(n, "href") == "" {
			all = true
		}
		if n.Type == html.ElementNode && n.Data == "a" {
			u, ok := playerPageURL(base, playerAttr(n, "href"), gameID)
			if ok && u.Query().Get("action") == "" && u.Query().Get("player") == "" && u.Query().Get("status") == "-1" {
				if page, e := strconv.Atoi(u.Query().Get("page")); e == nil && page > 0 {
					pages = append(pages, u.String())
				}
			}
		}
		if n.Data != "tr" || id != "trPlayer" {
			return
		}
		p := AdminGamePlayer{Status: -1}
		walkPlayerHTML(n, func(a *html.Node) {
			if a.Data != "a" {
				return
			}
			href := playerAttr(a, "href")
			if playerAttr(a, "id") == "lnkPlayer" {
				p.Name = strings.TrimSpace(playerText(a))
				if u, e := url.Parse(href); e == nil {
					p.UserID, _ = strconv.Atoi(u.Query().Get("uid"))
					p.TeamID, _ = strconv.Atoi(u.Query().Get("tid"))
				}
			}
			u, ok := playerPageURL(base, href, gameID)
			if !ok {
				return
			}
			pid, _ := strconv.Atoi(u.Query().Get("player"))
			if pid <= 0 {
				return
			}
			p.PlayerID = pid
			if strings.EqualFold(u.Query().Get("action"), "delete") {
				p.CanDelete = true
				p.deleteURL = u.String()
			}
		})
		text := playerText(n)
		switch {
		case strings.Contains(text, "Дисквалифицирован"):
			p.Status = 2
		case strings.Contains(text, "Принят к участию"):
			p.Status = 1
		case strings.Contains(text, "Подтвердил участие"), strings.Contains(text, "Подана заявка"):
			p.Status = 0
		}
		rows = append(rows, p)
	})
	if !all || total < 0 {
		return nil, nil, 0, fmt.Errorf("encx: full participant management view unavailable; authenticate as a game organizer")
	}
	return rows, pages, total, nil
}

func playerPageURL(base, href string, gameID int) (*url.URL, bool) {
	if href == "" {
		return nil, false
	}
	b, err := url.Parse(base)
	if err != nil {
		return nil, false
	}
	u, err := b.Parse(href)
	if err != nil {
		return nil, false
	}
	if u.Scheme != b.Scheme || u.Host != b.Host || u.User != nil || !strings.EqualFold(u.Path, "/GamePlayers.aspx") || u.Query().Get("gid") != strconv.Itoa(gameID) {
		return nil, false
	}
	return u, true
}
func playerAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}
func walkPlayerHTML(n *html.Node, fn func(*html.Node)) {
	fn(n)
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walkPlayerHTML(c, fn)
	}
}
func playerText(n *html.Node) string {
	var b strings.Builder
	walkPlayerHTML(n, func(c *html.Node) {
		if c.Type == html.TextNode {
			b.WriteString(c.Data)
		}
	})
	return b.String()
}
