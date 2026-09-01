package encx

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/skrashevich/encx-cli/encx/enapi"
)

func (e *newEngine) GetTeamDetails(ctx context.Context, teamId int) (string, error) {
	var team enapi.Team
	if err := e.c.api().GetJSON(ctx, fmt.Sprintf("/teams/%d", teamId), nil, &team); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(team)
	if err != nil {
		return "", fmt.Errorf("encx: encode team details: %w", err)
	}
	return string(encoded), nil
}

func (e *newEngine) GetMyTeamDetails(ctx context.Context) (string, error) {
	teamID, err := e.currentTeamID(ctx)
	if err != nil {
		return "", err
	}
	return e.GetTeamDetails(ctx, teamID)
}

// currentTeamID reads the signed-in user's team from the session.
//
// It answers "which team does this account belong to", which is a property of
// the account. "Did a team's membership just change" is a different question and
// is asked of the team's member list instead: a session rebuilt from the bearer
// token would still name the team the account had before the change.
func (e *newEngine) currentTeamID(ctx context.Context) (int, error) {
	var session enapi.Session
	if err := e.c.api().GetJSON(ctx, "/auth/session", nil, &session); err != nil {
		return 0, err
	}
	if session.User != nil {
		if session.User.TeamID > 0 {
			return session.User.TeamID, nil
		}
		if session.User.Team != nil && session.User.Team.ID > 0 {
			return session.User.Team.ID, nil
		}
	}
	return 0, fmt.Errorf("encx: the signed-in user is not in a team")
}

func (e *newEngine) GetTeamManagementInfo(ctx context.Context, teamID int) (*TeamManagementInfo, error) {
	var team enapi.Team
	if err := e.c.api().GetJSON(ctx, fmt.Sprintf("/teams/%d", teamID), nil, &team); err != nil {
		return nil, err
	}
	// Actions and PendingInvitations describe the legacy HTML page: the new
	// engine exposes operations as REST routes and publishes no list of
	// outstanding invitations, so both stay empty here.
	return &TeamManagementInfo{TeamID: team.ID, TeamName: team.Name}, nil
}

func (e *newEngine) GetTeamInvitations(ctx context.Context) ([]TeamInvitation, error) {
	var previews []enapi.TeamPreview
	if err := e.c.api().GetJSON(ctx, "/users/me/team-invitations", nil, &previews); err != nil {
		return nil, err
	}
	if len(previews) == 0 {
		return nil, nil
	}
	invitations := make([]TeamInvitation, 0, len(previews))
	for _, preview := range previews {
		invitations = append(invitations, TeamInvitation{TeamID: preview.ID, Name: preview.Name})
	}
	return invitations, nil
}

func (e *newEngine) AcceptTeamInvitation(ctx context.Context, teamId int) error {
	return e.respondToInvitation(ctx, teamId, true)
}

func (e *newEngine) RejectTeamInvitation(ctx context.Context, teamID int) error {
	return e.respondToInvitation(ctx, teamID, false)
}

func (e *newEngine) respondToInvitation(ctx context.Context, teamID int, accept bool) error {
	operation := invitationOperation(accept)
	path := fmt.Sprintf("/teams/%d/invitations/respond", teamID)
	body := enapi.InvitationResponseRequest{Accept: accept}
	if err := e.c.api().PostJSON(ctx, path, body, nil); err != nil {
		return teamActionErrorFrom(operation, err)
	}

	// The legacy engine re-read the team page and only then called the action
	// done. A 2xx here is not that: the deployed backend has already been seen
	// answering 204 to a route that changed nothing, and a team action that
	// silently did not happen is exactly the kind of failure a caller cannot
	// notice on its own.
	invitations, err := e.GetTeamInvitations(ctx)
	if err != nil {
		return fmt.Errorf("encx: team %s: verify state: %w", operation, err)
	}
	for _, invitation := range invitations {
		if invitation.TeamID == teamID {
			return &TeamActionError{Operation: operation, Message: "приглашение осталось в списке"}
		}
	}
	if !accept {
		return nil
	}
	// Membership is read from the team's own member list, not from
	// /auth/session: the session may be reconstructed from the bearer token this
	// client is still holding from before the change, in which case it would
	// report the old team and turn every successful accept into a hard error.
	member, err := e.isTeamMember(ctx, teamID)
	if err != nil {
		return fmt.Errorf("encx: team %s: verify state: %w", operation, err)
	}
	if member == nil {
		return &TeamActionError{Operation: operation, Message: "команда не сменилась"}
	}
	return nil
}

// isTeamMember reports whether the signed-in account is listed in a team.
//
// A list that names nobody recognizably is reported as an error rather than as
// "not a member". The difference matters because the two callers read a false
// differently: an accept would fail loudly, but a leave would quietly succeed —
// so a payload that stopped carrying an identity key would silently turn the
// leave verification back into the no-op it used to be.
func (e *newEngine) isTeamMember(ctx context.Context, teamID int) (*enapi.TeamMember, error) {
	userID, err := e.currentUserID(ctx)
	if err != nil {
		return nil, err
	}
	var members []enapi.TeamMember
	if err := e.c.api().GetJSON(ctx, fmt.Sprintf("/teams/%d/members", teamID), nil, &members); err != nil {
		return nil, err
	}

	identified := false
	for i, member := range members {
		// A row naming another team is not this team's membership, whatever else
		// it says.
		if member.TeamID != 0 && member.TeamID != teamID {
			continue
		}
		if member.MemberID() == 0 {
			continue
		}
		identified = true
		if member.MemberID() == userID {
			return &members[i], nil
		}
	}
	// Rows arrived but none of them could be read as this team's membership —
	// either they name no user or they name another team. Either way the answer
	// does not support the conclusion "the account is not in this team", and
	// saying so is the whole point of asking.
	if len(members) > 0 && !identified {
		return nil, fmt.Errorf(
			"encx: ответ со списком участников команды %d не читается как её состав (%d строк)",
			teamID, len(members))
	}
	return nil, nil
}

// currentUserID reads the signed-in account's id. Unlike the team, the identity
// is exactly what a token legitimately carries, so the session is the right
// place to ask.
func (e *newEngine) currentUserID(ctx context.Context) (int, error) {
	var session enapi.Session
	if err := e.c.api().GetJSON(ctx, "/auth/session", nil, &session); err != nil {
		return 0, err
	}
	if id := session.CurrentUserID(); id != 0 {
		return id, nil
	}
	return 0, fmt.Errorf("encx: the session does not name a user")
}

func invitationOperation(accept bool) string {
	if accept {
		return "accept invitation"
	}
	return "reject invitation"
}

func (e *newEngine) InviteTeamMember(ctx context.Context, teamID int, login string) error {
	login = strings.TrimSpace(login)
	if login == "" {
		return fmt.Errorf("encx: team invite: login is empty")
	}
	path := fmt.Sprintf("/teams/%d/invitations", teamID)
	body := enapi.TeamInviteRequest{Login: login}
	if err := e.c.api().PostJSON(ctx, path, body, nil); err != nil {
		return teamActionErrorFrom("invite member", err)
	}
	return nil
}

func (e *newEngine) RemoveTeamInvitation(ctx context.Context, teamID, userID int) error {
	path := fmt.Sprintf("/teams/%d/invitations/%d", teamID, userID)
	if err := e.c.api().Delete(ctx, path, nil, nil); err != nil {
		return teamActionErrorFrom("remove invitation", err)
	}
	return nil
}

func (e *newEngine) RequestTeamMembership(ctx context.Context, teamName string) error {
	// The legacy form took a team name; the REST route takes an ID, so the name
	// is resolved through the team search first.
	teamID, err := e.findTeamByName(ctx, teamName)
	if err != nil {
		return err
	}
	if err := e.c.api().PostJSON(ctx, fmt.Sprintf("/teams/%d/requests", teamID), nil, nil); err != nil {
		return teamActionErrorFrom("request membership", err)
	}
	return nil
}

func (e *newEngine) findTeamByName(ctx context.Context, teamName string) (int, error) {
	name := strings.TrimSpace(teamName)
	if name == "" {
		return 0, fmt.Errorf("encx: team request: team name is empty")
	}

	var list enapi.TeamsListResponse
	q := url.Values{"q": {name}, "pageSize": {"50"}}
	if err := e.c.api().GetJSON(ctx, "/teams", q, &list); err != nil {
		return 0, err
	}
	for _, item := range list.Items {
		if strings.EqualFold(strings.TrimSpace(item.Name), name) {
			return item.ID, nil
		}
	}
	return 0, &TeamActionError{
		Operation: "request membership",
		Message:   fmt.Sprintf("команда %q не найдена", name),
	}
}

func (e *newEngine) LeaveTeam(ctx context.Context, teamID int) error {
	if err := e.c.api().PostJSON(ctx, fmt.Sprintf("/teams/%d/leave", teamID), nil, nil); err != nil {
		return teamActionErrorFrom("leave", err)
	}
	// Verified the way legacy verified it: the account must no longer be listed
	// in the team it just left. The member list is asked rather than the session
	// for the same reason the accept path asks it — a token-derived session
	// could still name the old team. A verification that cannot be performed is
	// an error, not a success: a 401 here says nothing about whether the leave
	// went through.
	member, err := e.isTeamMember(ctx, teamID)
	if err != nil {
		return fmt.Errorf("encx: team leave: verify state: %w", err)
	}
	if member != nil {
		// The surviving row's flags are the first thing worth knowing when a
		// leave did not take.
		return &TeamActionError{
			Operation: "leave",
			Message: fmt.Sprintf("команда не покинута: is_active=%v approved_by_captain=%v approved_by_user=%v",
				member.IsActive, member.ApprovedByCaptain, member.ApprovedByUser),
		}
	}
	return nil
}

func (e *newEngine) RenameTeam(ctx context.Context, teamID int, name string) error {
	return e.updateTeam(ctx, teamID, "rename", name, exactMatch,
		func(update *enapi.TeamUpdateRequest) { update.Name = name },
		func(team *enapi.Team) string { return team.Name })
}

func (e *newEngine) SetTeamSite(ctx context.Context, teamID int, site string) error {
	return e.updateTeam(ctx, teamID, "set site", site, urlMatch,
		func(update *enapi.TeamUpdateRequest) { update.WebSite = site },
		func(team *enapi.Team) string { return team.WebSite })
}

func (e *newEngine) SetTeamForum(ctx context.Context, teamID int, forum string) error {
	return e.updateTeam(ctx, teamID, "set forum", forum, urlMatch,
		func(update *enapi.TeamUpdateRequest) { update.ForumLink = forum },
		func(team *enapi.Team) string { return team.ForumLink })
}

// accepted decides whether a write landed, given what was asked for and what the
// field held before and after.
type accepted func(want, before, after string) bool

func exactMatch(want, _, after string) bool {
	return strings.EqualFold(strings.TrimSpace(after), strings.TrimSpace(want))
}

// urlMatch is the same check relaxed for the two URL fields: servers normalize
// links — adding a scheme, dropping a trailing slash, lowercasing a host — so
// demanding the exact string back would report a successful write as a failure.
//
// It compares normalized forms rather than merely asking whether the value
// moved. "It changed" would accept a server that rejected the link and stored
// something else entirely, and it would call a concurrent edit by somebody else
// this write's success.
func urlMatch(want, _, after string) bool {
	wanted, stored := normalizeURL(want), normalizeURL(after)
	if wanted == "" {
		return stored == ""
	}
	if stored == "" {
		// The link was asked for and nothing is stored: the server refused it.
		return false
	}
	if stored == wanted {
		return true
	}
	// The two rewrites that add something rather than reshape it. Both are
	// anchored on a separator: an unanchored suffix match would accept
	// evil-team.example for team.example, which is the substitution this check
	// exists to catch.
	return strings.HasPrefix(stored, wanted+"/") || strings.HasSuffix(stored, "."+wanted)
}

// normalizeURL reduces a link to what two spellings of the same address share:
// case, the scheme and a trailing slash are what servers rewrite.
func normalizeURL(value string) string {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	// Scheme first: trimming the slash first would leave "https://" as "https:/"
	// instead of the empty string it means.
	for _, scheme := range []string{"https://", "http://"} {
		trimmed = strings.TrimPrefix(trimmed, scheme)
	}
	return strings.TrimSuffix(trimmed, "/")
}

// updateTeam reads the team before writing it back: PUT /teams/{id} replaces all
// three editable fields, so changing one without the others would blank them.
//
// It then reads the team once more and checks that the field actually changed,
// which is what the legacy pages did before reporting success.
func (e *newEngine) updateTeam(
	ctx context.Context,
	teamID int,
	operation, want string,
	landed accepted,
	apply func(*enapi.TeamUpdateRequest),
	read func(*enapi.Team) string,
) error {
	team, err := e.team(ctx, teamID)
	if err != nil {
		return err
	}
	before := read(team)
	update := enapi.TeamUpdateRequest{
		Name:      team.Name,
		WebSite:   team.WebSite,
		ForumLink: team.ForumLink,
	}
	apply(&update)

	if err := e.c.api().PutJSON(ctx, fmt.Sprintf("/teams/%d", teamID), update, nil); err != nil {
		return teamActionErrorFrom(operation, err)
	}

	after, err := e.team(ctx, teamID)
	if err != nil {
		return fmt.Errorf("encx: team %s: verify state: %w", operation, err)
	}
	if landed(want, before, read(after)) {
		return nil
	}
	return &TeamActionError{
		Operation: operation,
		Message:   fmt.Sprintf("значение не изменилось: осталось %q", read(after)),
	}
}

func (e *newEngine) team(ctx context.Context, teamID int) (*enapi.Team, error) {
	var team enapi.Team
	if err := e.c.api().GetJSON(ctx, fmt.Sprintf("/teams/%d", teamID), nil, &team); err != nil {
		return nil, err
	}
	return &team, nil
}

// teamActionErrorFrom keeps team failures reported as TeamActionError on both
// engines, so callers can keep branching on the type.
func teamActionErrorFrom(operation string, err error) error {
	apiErr, ok := enapi.AsAPIError(err)
	if !ok {
		return err
	}
	message := firstNonEmpty(apiErr.Message, apiErr.Err, apiErr.SentenceKey, apiErr.Body)
	return &TeamActionError{Operation: operation, Message: message}
}
