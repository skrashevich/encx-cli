package encx

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// teamCall records one REST call the new team backend made.
type teamCall struct {
	method string
	path   string
	query  string
	body   string
}

func newTeamClient(t *testing.T, calls *[]teamCall, respond func(path string) string) *Client {
	t.Helper()
	return newEngineClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		*calls = append(*calls, teamCall{
			method: r.Method, path: r.URL.Path, query: r.URL.RawQuery, body: string(body),
		})
		_, _ = w.Write([]byte(respond(r.URL.Path)))
	})
}

const teamFixture = `{"id":7324,"name":"svk team","captain_id":156988,"points":0,
  "web_site":"https://team.example","forum_link":"https://forum.example"}`

func teamRoutes(path string) string {
	switch {
	case path == "/teams/7324":
		return teamFixture
	case path == "/auth/session":
		return `{"user":{"id":156988,"login":"svk","team_id":7324,"team":{"id":7324,"name":"svk team"}}}`
	case path == "/users/me/team-invitations":
		return `[{"id":7328,"name":"svk team 2","captain_id":159013}]`
	case path == "/teams":
		return `{"items":[{"id":7328,"name":"svk team 2"},{"id":7324,"name":"svk team"}],"total_count":2}`
	default:
		return `{}`
	}
}

func TestNewEngineTeamDetails(t *testing.T) {
	var calls []teamCall
	c := newTeamClient(t, &calls, teamRoutes)

	body, err := c.GetTeamDetails(context.Background(), 7324)
	if err != nil {
		t.Fatalf("GetTeamDetails: %v", err)
	}
	if calls[0].path != "/teams/7324" || calls[0].method != http.MethodGet {
		t.Errorf("request = %s %s", calls[0].method, calls[0].path)
	}
	var team struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(body), &team); err != nil {
		t.Fatalf("team details are not JSON: %v", err)
	}
	if team.ID != 7324 || team.Name != "svk team" {
		t.Errorf("team = %+v", team)
	}
}

func TestNewEngineMyTeamDetailsResolvesTeamFromSession(t *testing.T) {
	var calls []teamCall
	c := newTeamClient(t, &calls, teamRoutes)

	if _, err := c.GetMyTeamDetails(context.Background()); err != nil {
		t.Fatalf("GetMyTeamDetails: %v", err)
	}
	want := []string{"/auth/session", "/teams/7324"}
	if len(calls) != 2 || calls[0].path != want[0] || calls[1].path != want[1] {
		t.Errorf("paths = %v, want %v", callPaths(calls), want)
	}
}

func TestNewEngineMyTeamDetailsReportsNoTeam(t *testing.T) {
	var calls []teamCall
	c := newTeamClient(t, &calls, func(string) string { return `{"user":{"id":1,"login":"solo"}}` })

	_, err := c.GetMyTeamDetails(context.Background())
	if err == nil {
		t.Fatal("GetMyTeamDetails invented a team")
	}
	if !strings.Contains(err.Error(), "not in a team") {
		t.Errorf("error = %v", err)
	}
}

func TestNewEngineTeamManagementInfo(t *testing.T) {
	var calls []teamCall
	c := newTeamClient(t, &calls, teamRoutes)

	info, err := c.GetTeamManagementInfo(context.Background(), 7324)
	if err != nil {
		t.Fatalf("GetTeamManagementInfo: %v", err)
	}
	if info.TeamID != 7324 || info.TeamName != "svk team" {
		t.Errorf("info = %+v", info)
	}
	// The new engine publishes no list of outstanding invitations and exposes
	// operations as routes rather than page links.
	if info.PendingInvitations != nil || info.Actions != nil {
		t.Errorf("info = %+v, want no HTML-page leftovers", info)
	}
}

func TestNewEngineTeamInvitations(t *testing.T) {
	var calls []teamCall
	c := newTeamClient(t, &calls, teamRoutes)

	invitations, err := c.GetTeamInvitations(context.Background())
	if err != nil {
		t.Fatalf("GetTeamInvitations: %v", err)
	}
	if calls[0].path != "/users/me/team-invitations" {
		t.Errorf("path = %q", calls[0].path)
	}
	if len(invitations) != 1 || invitations[0].TeamID != 7328 || invitations[0].Name != "svk team 2" {
		t.Errorf("invitations = %+v", invitations)
	}
}

func TestNewEngineInvitationResponses(t *testing.T) {
	cases := []struct {
		name       string
		call       func(*Client) error
		wantAccept bool
	}{
		{"accept", func(c *Client) error { return c.AcceptTeamInvitation(context.Background(), 7328) }, true},
		{"reject", func(c *Client) error { return c.RejectTeamInvitation(context.Background(), 7328) }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls []teamCall
			c := newTeamClient(t, &calls, teamRoutes)
			if err := tc.call(c); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if calls[0].method != http.MethodPost || calls[0].path != "/teams/7328/invitations/respond" {
				t.Errorf("request = %s %s", calls[0].method, calls[0].path)
			}
			var body struct {
				Accept bool `json:"accept"`
			}
			if err := json.Unmarshal([]byte(calls[0].body), &body); err != nil {
				t.Fatalf("body is not JSON: %q", calls[0].body)
			}
			if body.Accept != tc.wantAccept {
				t.Errorf("accept = %v, want %v", body.Accept, tc.wantAccept)
			}
		})
	}
}

func TestNewEngineInviteAndRemoveMember(t *testing.T) {
	var calls []teamCall
	c := newTeamClient(t, &calls, teamRoutes)
	ctx := context.Background()

	if err := c.InviteTeamMember(ctx, 7324, " newbie "); err != nil {
		t.Fatalf("InviteTeamMember: %v", err)
	}
	if calls[0].method != http.MethodPost || calls[0].path != "/teams/7324/invitations" {
		t.Errorf("request = %s %s", calls[0].method, calls[0].path)
	}
	var invite struct {
		Login string `json:"login"`
	}
	if err := json.Unmarshal([]byte(calls[0].body), &invite); err != nil {
		t.Fatalf("body is not JSON: %q", calls[0].body)
	}
	if invite.Login != "newbie" {
		t.Errorf("login = %q, want the trimmed login", invite.Login)
	}

	if err := c.RemoveTeamInvitation(ctx, 7324, 900); err != nil {
		t.Fatalf("RemoveTeamInvitation: %v", err)
	}
	if calls[1].method != http.MethodDelete || calls[1].path != "/teams/7324/invitations/900" {
		t.Errorf("request = %s %s", calls[1].method, calls[1].path)
	}

	if err := c.InviteTeamMember(ctx, 7324, "   "); err == nil {
		t.Error("InviteTeamMember accepted an empty login")
	}
}

func TestNewEngineRequestMembershipResolvesTeamName(t *testing.T) {
	var calls []teamCall
	c := newTeamClient(t, &calls, teamRoutes)

	if err := c.RequestTeamMembership(context.Background(), "svk team"); err != nil {
		t.Fatalf("RequestTeamMembership: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("calls = %v, want a search and a request", callPaths(calls))
	}
	if calls[0].path != "/teams" || !containsQueryPair(calls[0].query, "q", "svk team") {
		t.Errorf("search = %s?%s", calls[0].path, calls[0].query)
	}
	// The search returns "svk team 2" first; the exact name must win.
	if calls[1].method != http.MethodPost || calls[1].path != "/teams/7324/requests" {
		t.Errorf("request = %s %s", calls[1].method, calls[1].path)
	}
}

func TestNewEngineRequestMembershipReportsUnknownTeam(t *testing.T) {
	var calls []teamCall
	c := newTeamClient(t, &calls, func(string) string { return `{"items":[],"total_count":0}` })

	err := c.RequestTeamMembership(context.Background(), "нет такой")
	if err == nil {
		t.Fatal("RequestTeamMembership accepted an unknown team")
	}
	var actionErr *TeamActionError
	if !errors.As(err, &actionErr) {
		t.Fatalf("error is not *TeamActionError: %v", err)
	}
	if !strings.Contains(actionErr.Message, "не найдена") {
		t.Errorf("message = %q", actionErr.Message)
	}
}

func TestNewEngineLeaveTeam(t *testing.T) {
	var calls []teamCall
	c := newTeamClient(t, &calls, teamRoutes)

	if err := c.LeaveTeam(context.Background(), 7324); err != nil {
		t.Fatalf("LeaveTeam: %v", err)
	}
	if calls[0].method != http.MethodPost || calls[0].path != "/teams/7324/leave" {
		t.Errorf("request = %s %s", calls[0].method, calls[0].path)
	}
}

// TestNewEngineTeamUpdatesPreserveOtherFields pins that a partial edit does not
// blank the rest: PUT /teams/{id} replaces name, web_site and forum_link.
func TestNewEngineTeamUpdatesPreserveOtherFields(t *testing.T) {
	cases := []struct {
		name      string
		call      func(*Client) error
		wantName  string
		wantSite  string
		wantForum string
	}{
		{
			name:     "rename",
			call:     func(c *Client) error { return c.RenameTeam(context.Background(), 7324, "новое имя") },
			wantName: "новое имя", wantSite: "https://team.example", wantForum: "https://forum.example",
		},
		{
			name:     "set site",
			call:     func(c *Client) error { return c.SetTeamSite(context.Background(), 7324, "https://new.example") },
			wantName: "svk team", wantSite: "https://new.example", wantForum: "https://forum.example",
		},
		{
			name:     "set forum",
			call:     func(c *Client) error { return c.SetTeamForum(context.Background(), 7324, "https://new.forum") },
			wantName: "svk team", wantSite: "https://team.example", wantForum: "https://new.forum",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls []teamCall
			c := newTeamClient(t, &calls, teamRoutes)
			if err := tc.call(c); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if len(calls) != 2 {
				t.Fatalf("calls = %v, want a read then a write", callPaths(calls))
			}
			if calls[0].method != http.MethodGet || calls[1].method != http.MethodPut {
				t.Fatalf("methods = %s then %s, want GET then PUT", calls[0].method, calls[1].method)
			}
			var update struct {
				Name      string `json:"name"`
				WebSite   string `json:"web_site"`
				ForumLink string `json:"forum_link"`
			}
			if err := json.Unmarshal([]byte(calls[1].body), &update); err != nil {
				t.Fatalf("body is not JSON: %q", calls[1].body)
			}
			if update.Name != tc.wantName || update.WebSite != tc.wantSite || update.ForumLink != tc.wantForum {
				t.Errorf("update = %+v, want %q/%q/%q", update, tc.wantName, tc.wantSite, tc.wantForum)
			}
		})
	}
}

func TestNewEngineTeamFailuresStayTeamActionErrors(t *testing.T) {
	c := newEngineClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"not_captain","message":"Только капитан"}`))
	})

	err := c.LeaveTeam(context.Background(), 7324)
	var actionErr *TeamActionError
	if !errors.As(err, &actionErr) {
		t.Fatalf("error is not *TeamActionError: %v", err)
	}
	if actionErr.Operation != "leave" || actionErr.Message != "Только капитан" {
		t.Errorf("error = %+v", actionErr)
	}
}

func callPaths(calls []teamCall) []string {
	paths := make([]string, 0, len(calls))
	for _, call := range calls {
		paths = append(paths, call.path)
	}
	return paths
}
