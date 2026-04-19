package encx

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

var teamLinkRe = regexp.MustCompile(`(?i)TeamDetails\.aspx\?tid=(\d+)[^>]*>([^<]+)</a>`)

// TeamInfo represents basic team information parsed from HTML.
type TeamInfo struct {
	TeamId int    `json:"teamId"`
	Name   string `json:"name"`
}

// GetTeamDetails fetches the team details page and parses team information.
func (c *Client) GetTeamDetails(ctx context.Context, teamId int) (string, error) {
	u := fmt.Sprintf("%s/Teams/TeamDetails.aspx?tid=%d", c.baseURL(), teamId)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", fmt.Errorf("encx: create team details request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("encx: team details request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("encx: read team details body: %w", err)
	}

	return string(body), nil
}

// AcceptTeamInvitation accepts a team invitation by team ID.
func (c *Client) AcceptTeamInvitation(ctx context.Context, teamId int) error {
	u := fmt.Sprintf("%s/Teams/TeamDetails.aspx?action=accept_invitation&tid=%d", c.baseURL(), teamId)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("encx: create accept invitation request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("encx: accept invitation request: %w", err)
	}
	defer resp.Body.Close()

	return nil
}

// ParseTeamLinks extracts team IDs and names from an HTML page.
func ParseTeamLinks(html string) []TeamInfo {
	matches := teamLinkRe.FindAllStringSubmatch(html, -1)
	seen := map[int]bool{}
	teams := make([]TeamInfo, 0, len(matches))
	for _, m := range matches {
		id, err := strconv.Atoi(m[1])
		if err != nil || seen[id] {
			continue
		}
		seen[id] = true
		teams = append(teams, TeamInfo{
			TeamId: id,
			Name:   strings.TrimSpace(m[2]),
		})
	}
	return teams
}
