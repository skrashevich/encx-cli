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
	path := fmt.Sprintf("/teams/%d/invitations/respond", teamID)
	body := enapi.InvitationResponseRequest{Accept: accept}
	if err := e.c.api().PostJSON(ctx, path, body, nil); err != nil {
		return teamActionErrorFrom(invitationOperation(accept), err)
	}
	return nil
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
	return nil
}

func (e *newEngine) RenameTeam(ctx context.Context, teamID int, name string) error {
	return e.updateTeam(ctx, teamID, "rename", func(update *enapi.TeamUpdateRequest) {
		update.Name = name
	})
}

func (e *newEngine) SetTeamSite(ctx context.Context, teamID int, site string) error {
	return e.updateTeam(ctx, teamID, "set site", func(update *enapi.TeamUpdateRequest) {
		update.WebSite = site
	})
}

func (e *newEngine) SetTeamForum(ctx context.Context, teamID int, forum string) error {
	return e.updateTeam(ctx, teamID, "set forum", func(update *enapi.TeamUpdateRequest) {
		update.ForumLink = forum
	})
}

// updateTeam reads the team before writing it back: PUT /teams/{id} replaces all
// three editable fields, so changing one without the others would blank them.
func (e *newEngine) updateTeam(ctx context.Context, teamID int, operation string, apply func(*enapi.TeamUpdateRequest)) error {
	var team enapi.Team
	if err := e.c.api().GetJSON(ctx, fmt.Sprintf("/teams/%d", teamID), nil, &team); err != nil {
		return err
	}
	update := enapi.TeamUpdateRequest{
		Name:      team.Name,
		WebSite:   team.WebSite,
		ForumLink: team.ForumLink,
	}
	apply(&update)

	if err := e.c.api().PutJSON(ctx, fmt.Sprintf("/teams/%d", teamID), update, nil); err != nil {
		return teamActionErrorFrom(operation, err)
	}
	return nil
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
