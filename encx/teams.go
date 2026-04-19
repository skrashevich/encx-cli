package encx

import (
	"context"
	"fmt"
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

// GetTeamDetails fetches the team details page and returns raw HTML.
func (c *Client) GetTeamDetails(ctx context.Context, teamId int) (string, error) {
	return c.doGet(ctx, fmt.Sprintf("%s/Teams/TeamDetails.aspx?tid=%d", c.baseURL(), teamId))
}

// AcceptTeamInvitation accepts a team invitation by team ID.
func (c *Client) AcceptTeamInvitation(ctx context.Context, teamId int) error {
	_, err := c.doGet(ctx, fmt.Sprintf("%s/Teams/TeamDetails.aspx?action=accept_invitation&tid=%d", c.baseURL(), teamId))
	return err
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
