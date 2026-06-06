package encx

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var teamLinkRe = regexp.MustCompile(`(?i)TeamDetails\.aspx\?tid=(\d+)[^>]*>([^<]+)</a>`)
var teamActionLinkRe = regexp.MustCompile(`(?is)<a[^>]*href="([^"]*TeamDetails\.aspx\?[^"]*action=([^"&]+)[^"]*)"[^>]*>(.*?)</a>`)

// TeamInvitation represents an invitation to join a team.
type TeamInvitation struct {
	TeamID int    `json:"team_id"`
	Name   string `json:"name"`
	Action string `json:"action,omitempty"`
}

// TeamPendingInvitation represents a user invited to the current team.
type TeamPendingInvitation struct {
	UserID int    `json:"user_id"`
	Login  string `json:"login"`
}

// TeamManagementInfo contains team management actions parsed from TeamDetails.aspx.
type TeamManagementInfo struct {
	TeamID             int                     `json:"team_id"`
	TeamName           string                  `json:"team_name,omitempty"`
	PendingInvitations []TeamPendingInvitation `json:"pending_invitations,omitempty"`
	Actions            map[string]string       `json:"actions,omitempty"`
}

// TeamInfo represents basic team information parsed from HTML.
type TeamInfo struct {
	TeamId int    `json:"teamId"`
	Name   string `json:"name"`
}

// TeamActionError reports an Encounter team-management action that returned HTTP OK
// but did not change the page state as requested.
type TeamActionError struct {
	Operation string
	Message   string
}

func (e *TeamActionError) Error() string {
	if strings.TrimSpace(e.Message) != "" {
		return fmt.Sprintf("encx: team %s: %s", e.Operation, e.Message)
	}
	return fmt.Sprintf("encx: team %s: action did not change team state", e.Operation)
}

// GetTeamDetails fetches the team details page and returns raw HTML.
func (c *Client) GetTeamDetails(ctx context.Context, teamId int) (string, error) {
	return c.doGet(ctx, fmt.Sprintf("%s/Teams/TeamDetails.aspx?tid=%d", c.baseURL(), teamId))
}

// GetMyTeamDetails fetches the current user's team page.
func (c *Client) GetMyTeamDetails(ctx context.Context) (string, error) {
	return c.doGet(ctx, fmt.Sprintf("%s/Teams/TeamDetails.aspx", c.baseURL()))
}

func (c *Client) absURL(href string) string {
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		return href
	}
	if strings.HasPrefix(href, "/") {
		return c.baseURL() + href
	}
	return c.baseURL() + "/" + href
}

// GetTeamManagementInfo fetches and parses management links and invitations for a team.
func (c *Client) GetTeamManagementInfo(ctx context.Context, teamID int) (*TeamManagementInfo, error) {
	body, err := c.GetTeamDetails(ctx, teamID)
	if err != nil {
		return nil, err
	}
	return ParseTeamManagementInfo(body, teamID), nil
}

// GetTeamInvitations fetches team invitations addressed to the current user.
func (c *Client) GetTeamInvitations(ctx context.Context) ([]TeamInvitation, error) {
	body, err := c.GetMyTeamDetails(ctx)
	if err != nil {
		return nil, err
	}
	return ParseTeamInvitations(body), nil
}

// AcceptTeamInvitation accepts a team invitation by team ID.
func (c *Client) AcceptTeamInvitation(ctx context.Context, teamId int) error {
	body, err := c.doGet(ctx, fmt.Sprintf("%s/Teams/TeamDetails.aspx?action=accept_invitation&tid=%d", c.baseURL(), teamId))
	if err != nil {
		return err
	}
	state, err := c.getCurrentTeamState(ctx)
	if err != nil {
		return fmt.Errorf("encx: team accept invitation: verify state: %w", err)
	}
	if state.currentTeamID == teamId && !state.hasInvitation(teamId) {
		return nil
	}
	return &TeamActionError{
		Operation: "accept invitation",
		Message:   teamActionFailureMessage(body, "приглашение не принято"),
	}
}

// RejectTeamInvitation rejects a team invitation by team ID.
func (c *Client) RejectTeamInvitation(ctx context.Context, teamID int) error {
	body, err := c.doGet(ctx, fmt.Sprintf("%s/Teams/TeamDetails.aspx?action=reject_invitation&tid=%d", c.baseURL(), teamID))
	if err != nil {
		return err
	}
	state, err := c.getCurrentTeamState(ctx)
	if err != nil {
		return fmt.Errorf("encx: team reject invitation: verify state: %w", err)
	}
	if !state.hasInvitation(teamID) {
		return nil
	}
	return &TeamActionError{
		Operation: "reject invitation",
		Message:   teamActionFailureMessage(body, "приглашение не отклонено"),
	}
}

// RequestTeamMembership sends a request to join the named team.
func (c *Client) RequestTeamMembership(ctx context.Context, teamName string) error {
	u := fmt.Sprintf("%s/Teams/SendRequest.aspx", c.baseURL())
	form := url.Values{}
	form.Set("TeamName", teamName)
	form.Set("Submit.x", "1")
	form.Set("Submit.y", "1")
	body, err := c.doPost(ctx, u, form)
	if err != nil {
		return err
	}
	if message := explicitTeamActionFailure(body); message != "" {
		return &TeamActionError{Operation: "request membership", Message: message}
	}
	return nil
}

// InviteTeamMember invites a user login into the specified team.
func (c *Client) InviteTeamMember(ctx context.Context, teamID int, login string) error {
	u := fmt.Sprintf("%s/Teams/TeamDetails.aspx?tid=%d", c.baseURL(), teamID)
	body, err := c.doGet(ctx, u)
	if err != nil {
		return err
	}
	form := parseEnabledInputs(body)
	formVals := url.Values{}
	for k, v := range form {
		formVals.Set(k, html.UnescapeString(v))
	}
	formVals.Set("NewMember", login)
	formVals.Set("ctl06_content_ctl00_btnInvite.x", "1")
	formVals.Set("ctl06_content_ctl00_btnInvite.y", "1")
	body, err = c.doPost(ctx, u, formVals)
	if err != nil {
		return err
	}
	info, err := c.GetTeamManagementInfo(ctx, teamID)
	if err != nil {
		return fmt.Errorf("encx: team invite member: verify state: %w", err)
	}
	for _, pending := range info.PendingInvitations {
		if strings.EqualFold(pending.Login, strings.TrimSpace(login)) {
			return nil
		}
	}
	return &TeamActionError{
		Operation: "invite member",
		Message:   teamActionFailureMessage(body, "приглашение не отправлено"),
	}
}

// RemoveTeamInvitation removes a pending invitation for a user from a team.
func (c *Client) RemoveTeamInvitation(ctx context.Context, teamID, userID int) error {
	u := fmt.Sprintf("%s/Teams/TeamDetails.aspx?action=remove_invitation&uid=%d&tid=%d", c.baseURL(), userID, teamID)
	body, err := c.doGet(ctx, u)
	if err != nil {
		return err
	}
	info, err := c.GetTeamManagementInfo(ctx, teamID)
	if err != nil {
		return fmt.Errorf("encx: team remove invitation: verify state: %w", err)
	}
	for _, pending := range info.PendingInvitations {
		if pending.UserID == userID {
			return &TeamActionError{
				Operation: "remove invitation",
				Message:   teamActionFailureMessage(body, "приглашение не удалено"),
			}
		}
	}
	return nil
}

// LeaveTeam leaves a team by following the leave action link exposed by the page.
func (c *Client) LeaveTeam(ctx context.Context, teamID int) error {
	body, err := c.GetTeamDetails(ctx, teamID)
	if err != nil {
		return err
	}
	info := ParseTeamManagementInfo(body, teamID)
	for action, href := range info.Actions {
		if strings.Contains(action, "leave") || strings.Contains(action, "quit") || strings.Contains(action, "exit") {
			body, err := c.doGet(ctx, c.absURL(href))
			if err != nil {
				return err
			}
			state, err := c.getCurrentTeamState(ctx)
			if err != nil {
				return fmt.Errorf("encx: team leave: verify state: %w", err)
			}
			if state.currentTeamID != teamID {
				return nil
			}
			return &TeamActionError{
				Operation: "leave",
				Message:   teamActionFailureMessage(body, "выход из команды не выполнен"),
			}
		}
	}
	return fmt.Errorf("encx: leave team: no leave action found for team %d", teamID)
}

// RenameTeam renames a team.
func (c *Client) RenameTeam(ctx context.Context, teamID int, name string) error {
	form := url.Values{}
	form.Set("txtTeamName", name)
	form.Set("Submit.x", "1")
	form.Set("Submit.y", "1")
	body, err := c.doPost(ctx, fmt.Sprintf("%s/Teams/RenameTeam.aspx?tid=%d", c.baseURL(), teamID), form)
	if err != nil {
		return err
	}
	info, err := c.GetTeamManagementInfo(ctx, teamID)
	if err != nil {
		return fmt.Errorf("encx: team rename: verify state: %w", err)
	}
	if strings.EqualFold(strings.TrimSpace(info.TeamName), strings.TrimSpace(name)) {
		return nil
	}
	return &TeamActionError{
		Operation: "rename",
		Message:   teamActionFailureMessage(body, "команда не переименована"),
	}
}

// SetTeamSite updates the team website URL.
func (c *Client) SetTeamSite(ctx context.Context, teamID int, site string) error {
	form := url.Values{}
	form.Set("entbTeamSite", site)
	form.Set("btnSubmit.x", "1")
	form.Set("btnSubmit.y", "1")
	_, err := c.doPost(ctx, fmt.Sprintf("%s/Teams/EditTeamSite.aspx?tid=%d", c.baseURL(), teamID), form)
	return err
}

// SetTeamForum updates the team external forum URL.
func (c *Client) SetTeamForum(ctx context.Context, teamID int, forum string) error {
	form := url.Values{}
	form.Set("txtTeamForum", forum)
	form.Set("btnSubmit.x", "1")
	form.Set("btnSubmit.y", "1")
	_, err := c.doPost(ctx, fmt.Sprintf("%s/Teams/EditTeamForum.aspx?tid=%d", c.baseURL(), teamID), form)
	return err
}

// ParseTeamLinks extracts team IDs and names from an HTML page.
func ParseTeamLinks(body string) []TeamInfo {
	matches := teamLinkRe.FindAllStringSubmatch(body, -1)
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

// ParseTeamManagementInfo extracts known team management actions and invitations from a team page.
func ParseTeamManagementInfo(body string, teamID int) *TeamManagementInfo {
	info := &TeamManagementInfo{
		TeamID:  teamID,
		Actions: map[string]string{},
	}
	if m := regexp.MustCompile(`(?is)id="lnkTeamName"[^>]*href="/Teams/TeamDetails\.aspx\?tid=(\d+)"[^>]*>(.*?)</a>`).FindStringSubmatch(body); m != nil {
		if info.TeamID == 0 {
			info.TeamID, _ = strconv.Atoi(m[1])
		}
		info.TeamName = cleanHTMLText(m[2])
	} else if m := regexp.MustCompile(`(?is)id="lnkTeamName"[^>]*>(.*?)</a>`).FindStringSubmatch(body); m != nil {
		info.TeamName = cleanHTMLText(m[1])
	}
	for _, m := range teamActionLinkRe.FindAllStringSubmatch(body, -1) {
		href := html.UnescapeString(m[1])
		action := strings.ToLower(strings.TrimSpace(m[2]))
		if action == "accept_invitation" || action == "reject_invitation" {
			continue
		}
		info.Actions[action] = href
	}
	for _, m := range regexp.MustCompile(`(?is)href="/Teams/TeamDetails\.aspx\?action=remove_invitation&uid=(\d+)&tid=(\d+)"[^>]*>.*?</a>`).FindAllStringSubmatch(body, -1) {
		uid, _ := strconv.Atoi(m[1])
		tid, _ := strconv.Atoi(m[2])
		if tid != teamID {
			continue
		}
		info.PendingInvitations = append(info.PendingInvitations, TeamPendingInvitation{UserID: uid, Login: pendingInviteLogin(body, uid)})
	}
	return info
}

// ParseTeamInvitations extracts invitations addressed to the current user.
func ParseTeamInvitations(body string) []TeamInvitation {
	var invitations []TeamInvitation
	for _, m := range teamActionLinkRe.FindAllStringSubmatch(body, -1) {
		href := html.UnescapeString(m[1])
		action := strings.ToLower(strings.TrimSpace(m[2]))
		if action != "accept_invitation" {
			continue
		}
		if id := firstIntParam(href, "tid"); id > 0 {
			invitations = append(invitations, TeamInvitation{
				TeamID: id,
				Name:   previousTeamName(body, href),
				Action: cleanHTMLText(m[3]),
			})
		}
	}
	return invitations
}

func firstIntParam(rawURL, key string) int {
	u, err := url.Parse(html.UnescapeString(rawURL))
	if err != nil {
		return 0
	}
	v, _ := strconv.Atoi(u.Query().Get(key))
	return v
}

func pendingInviteLogin(body string, uid int) string {
	re := regexp.MustCompile(fmt.Sprintf(`(?is)id&nbsp;(?:</span>)?\s*%d.*?<a[^>]*href="/UserDetails\.aspx\?uid=%d"[^>]*>([^<]+)</a>`, uid, uid))
	if m := re.FindStringSubmatch(body); m != nil {
		return cleanHTMLText(m[1])
	}
	return ""
}

func previousTeamName(body, acceptHref string) string {
	tid := firstIntParam(acceptHref, "tid")
	if tid == 0 {
		return ""
	}
	re := regexp.MustCompile(fmt.Sprintf(`(?is)<a[^>]*href="/Teams/TeamDetails\.aspx\?tid=%d"[^>]*>(.*?)</a>`, tid))
	if m := re.FindStringSubmatch(body); m != nil {
		return cleanHTMLText(m[1])
	}
	return ""
}

func cleanHTMLText(s string) string {
	s = regexp.MustCompile(`(?is)<[^>]+>`).ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	return strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
}

type teamState struct {
	currentTeamID int
	invitations   []TeamInvitation
}

func (s teamState) hasInvitation(teamID int) bool {
	for _, invitation := range s.invitations {
		if invitation.TeamID == teamID {
			return true
		}
	}
	return false
}

func (c *Client) getCurrentTeamState(ctx context.Context) (teamState, error) {
	body, err := c.GetMyTeamDetails(ctx)
	if err != nil {
		return teamState{}, err
	}
	info := ParseTeamManagementInfo(body, 0)
	return teamState{
		currentTeamID: info.TeamID,
		invitations:   ParseTeamInvitations(body),
	}, nil
}

func explicitTeamActionFailure(body string) string {
	text := cleanHTMLText(body)
	normalized := strings.ToLower(text)
	for _, marker := range []string{
		"ошибка",
		"невозможно",
		"нельзя",
		"не можете",
		"not allowed",
		"cannot",
		"can't",
		"error",
	} {
		if strings.Contains(normalized, marker) {
			return summarizeTeamActionText(text)
		}
	}
	return ""
}

func teamActionFailureMessage(body, fallback string) string {
	if message := explicitTeamActionFailure(body); message != "" {
		return message
	}
	return fallback
}

func summarizeTeamActionText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if len(text) > 240 {
		return strings.TrimSpace(text[:240]) + "..."
	}
	return text
}
