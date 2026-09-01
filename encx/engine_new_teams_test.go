package encx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
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

// teamWorld is the state the fake team backend keeps. The team methods verify
// what they wrote — the legacy pages did the same before reporting success — so
// a responder that always replays one fixture could not tell an applied change
// from an ignored one.
type teamWorld struct {
	name, site, forum string
	currentTeam       int
	invitations       map[int]string
	// frozen accepts every write and changes nothing, the way the deployed
	// backend was seen answering 204 to a route that did nothing.
	frozen bool
	// staleSession serves /auth/session from the team the account had before
	// the change, which is how the route behaves if it is rebuilt from the
	// bearer token the client is still holding. The team verifications must not
	// depend on it.
	staleSession bool
	sessionTeam  int
}

func newTeamWorld() *teamWorld {
	return &teamWorld{
		name:        "svk team",
		site:        "https://team.example",
		forum:       "https://forum.example",
		currentTeam: 7324,
		sessionTeam: 7324,
		invitations: map[int]string{7328: "svk team 2"},
	}
}

func (w *teamWorld) team() string {
	return fmt.Sprintf(`{"id":7324,"name":%q,"captain_id":156988,"points":0,
	  "web_site":%q,"forum_link":%q}`, w.name, w.site, w.forum)
}

func (w *teamWorld) session() string {
	team := w.currentTeam
	if w.staleSession {
		team = w.sessionTeam
	}
	if team == 0 {
		return `{"user":{"id":156988,"login":"svk"}}`
	}
	return fmt.Sprintf(`{"user":{"id":156988,"login":"svk","team_id":%d,
	  "team":{"id":%d,"name":%q}}}`, team, team, w.name)
}

// members renders GET /teams/{id}/members, the live record the team
// verifications read instead of the possibly token-derived session.
func (w *teamWorld) members(teamID int) string {
	if w.currentTeam != teamID {
		return "[]"
	}
	// The key spelling is the deployed API's, taken from a live response, not the
	// Go struct's: a fake that mirrors the client's own assumption would agree
	// with it no matter what the server sends.
	return fmt.Sprintf(`[{"user_id":156988,"team_id":%d,"login":"svk","avatar":"",
	  "approved_by_captain":true,"approved_by_user":true,"is_active":true,
	  "points":0,"gender_id":0,"rank_id":1,"rank_sentence_key":"Private soldier"}]`, teamID)
}

func (w *teamWorld) invitationList() string {
	ids := make([]int, 0, len(w.invitations))
	for id := range w.invitations {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	items := make([]string, 0, len(ids))
	for _, id := range ids {
		items = append(items, fmt.Sprintf(`{"id":%d,"name":%q,"captain_id":159013}`, id, w.invitations[id]))
	}
	return "[" + strings.Join(items, ",") + "]"
}

func (w *teamWorld) handle(r *http.Request, body []byte) string {
	if w.frozen {
		return w.read(r.URL.Path)
	}
	switch {
	case r.Method == http.MethodPut && r.URL.Path == "/teams/7324":
		var update struct {
			Name      string `json:"name"`
			WebSite   string `json:"web_site"`
			ForumLink string `json:"forum_link"`
		}
		_ = json.Unmarshal(body, &update)
		w.name, w.site, w.forum = update.Name, update.WebSite, update.ForumLink
	case r.Method == http.MethodPost && r.URL.Path == "/teams/7328/invitations/respond":
		var respond struct {
			Accept bool `json:"accept"`
		}
		_ = json.Unmarshal(body, &respond)
		delete(w.invitations, 7328)
		if respond.Accept {
			w.currentTeam = 7328
		}
	case r.Method == http.MethodPost && r.URL.Path == "/teams/7324/leave":
		w.currentTeam = 0
	}
	return w.read(r.URL.Path)
}

func (w *teamWorld) read(path string) string {
	switch path {
	case "/teams/7324/members":
		return w.members(7324)
	case "/teams/7328/members":
		return w.members(7328)
	case "/teams/7324", "/teams/7328":
		return w.team()
	case "/auth/session":
		return w.session()
	case "/users/me/team-invitations":
		return w.invitationList()
	case "/teams":
		return `{"items":[{"id":7328,"name":"svk team 2"},{"id":7324,"name":"svk team"}],"total_count":2}`
	default:
		return `{}`
	}
}

func newTeamWorldClient(t *testing.T, calls *[]teamCall) (*Client, *teamWorld) {
	t.Helper()
	world := newTeamWorld()
	client := newEngineClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		*calls = append(*calls, teamCall{
			method: r.Method, path: r.URL.Path, query: r.URL.RawQuery, body: string(body),
		})
		_, _ = w.Write([]byte(world.handle(r, body)))
	})
	return client, world
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
			c, _ := newTeamWorldClient(t, &calls)
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
	c, _ := newTeamWorldClient(t, &calls)

	if err := c.LeaveTeam(context.Background(), 7324); err != nil {
		t.Fatalf("LeaveTeam: %v", err)
	}
	if calls[0].method != http.MethodPost || calls[0].path != "/teams/7324/leave" {
		t.Errorf("request = %s %s", calls[0].method, calls[0].path)
	}
}

// The membership verifications read the team's member list, not /auth/session.
// If that route is rebuilt from the bearer token this client is still holding,
// it names the team the account had before the change — and a verification
// resting on it would turn every successful accept and leave into a hard error.
func TestNewEngineTeamMembershipDoesNotRestOnTheSession(t *testing.T) {
	t.Run("accept", func(t *testing.T) {
		var calls []teamCall
		c, world := newTeamWorldClient(t, &calls)
		world.staleSession = true

		if err := c.AcceptTeamInvitation(context.Background(), 7328); err != nil {
			t.Errorf("AcceptTeamInvitation: %v", err)
		}
	})

	t.Run("leave", func(t *testing.T) {
		var calls []teamCall
		c, world := newTeamWorldClient(t, &calls)
		world.staleSession = true

		if err := c.LeaveTeam(context.Background(), 7324); err != nil {
			t.Errorf("LeaveTeam: %v", err)
		}
	})
}

// The member list is what both membership verifications rest on, so the key it
// is read by matters. The deployed API sends user_id and no id at all, while the
// specification describes the route as an array of models.User, whose key is id;
// neither spelling may be assumed away.
func TestNewEngineTeamMemberIDReadsEitherSpelling(t *testing.T) {
	cases := []struct {
		name string
		row  string
		want bool
	}{
		{"deployed shape", `{"user_id":156988,"team_id":7324,"login":"svk","is_active":true}`, true},
		{"specification shape", `{"id":156988,"team_id":7324,"login":"svk"}`, true},
		{"somebody else", `{"user_id":159001,"team_id":7324,"login":"enxbot"}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			left := false
			c := newEngineClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/teams/7324/leave":
					left = true
					w.WriteHeader(http.StatusNoContent)
				case r.URL.Path == "/teams/7324/members":
					if left {
						// The account is still listed, so the leave did nothing.
						_, _ = w.Write([]byte("[" + tc.row + "]"))
						return
					}
					_, _ = w.Write([]byte("[" + tc.row + "]"))
				default:
					_, _ = w.Write([]byte(`{"user":{"id":156988,"login":"svk","team_id":7324}}`))
				}
			})

			err := c.LeaveTeam(context.Background(), 7324)
			stillListed := err != nil
			if stillListed != tc.want {
				t.Errorf("row %s: recognised as this account = %v, want %v (err = %v)",
					tc.row, stillListed, tc.want, err)
			}
		})
	}
}

// A member list that names nobody recognizably must be an error, not "not a
// member". The two callers read a false differently — an accept fails loudly,
// but a leave succeeds — so a payload that stopped carrying an identity key
// would silently turn the leave verification back into a no-op.
func TestNewEngineTeamMembershipRefusesAnUnreadableList(t *testing.T) {
	for _, tc := range []struct {
		name    string
		members string
		wantErr bool
	}{
		{"no identity key", `[{"team_id":7324,"login":"svk","is_active":true}]`, true},
		// Rows that all name another team do not describe this team's membership
		// either, so they cannot support "the account has left".
		{"another team's rows", `[{"user_id":156988,"team_id":9999,"login":"svk"}]`, true},
		{"nobody at all", `[]`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newEngineClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/teams/7324/leave":
					w.WriteHeader(http.StatusNoContent)
				case r.URL.Path == "/teams/7324/members":
					_, _ = w.Write([]byte(tc.members))
				default:
					_, _ = w.Write([]byte(`{"user":{"id":156988,"login":"svk","team_id":7324}}`))
				}
			})

			err := c.LeaveTeam(context.Background(), 7324)
			if tc.wantErr {
				if err == nil {
					t.Fatal("LeaveTeam reported success over a list it could not read")
				}
				if !strings.Contains(err.Error(), "verify state") {
					t.Errorf("error = %v, want it to say the verification failed", err)
				}
				return
			}
			if err != nil {
				t.Errorf("LeaveTeam: %v", err)
			}
		})
	}
}

// The site and forum fields are links, and servers rewrite links: they add a
// scheme, drop a trailing slash, lowercase a host. Demanding the exact string
// back would call a successful write a failure — but accepting "the value
// changed" would call a rejected link a success.
func TestNewEngineTeamURLVerificationToleratesNormalizationOnly(t *testing.T) {
	cases := []struct {
		name   string
		want   string
		stored string
		ok     bool
	}{
		{"stored verbatim", "https://new.example", "https://new.example", true},
		{"scheme added", "new.example", "https://new.example", true},
		{"trailing slash added", "https://new.example", "https://new.example/", true},
		{"host lowercased", "https://New.Example", "https://new.example", true},
		{"already normalized to what was stored", "team.example", "https://team.example", true},
		{"path appended", "https://new.example", "https://new.example/team", true},
		{"subdomain prepended", "new.example", "https://team.new.example", true},
		{"rejected and blanked", "javascript:alert(1)", "", false},
		{"replaced by something else", "https://new.example", "https://elsewhere.example", false},
		// A host that merely ends with what was asked for is a different host.
		{"lookalike suffix", "team.example", "https://evil-team.example", false},
		{"lookalike prefix", "team.example", "https://team.example.attacker.test", false},
		{"cleared on purpose", "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls []teamCall
			stored := "https://team.example"
			c := newEngineClient(t, func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				calls = append(calls, teamCall{method: r.Method, path: r.URL.Path, body: string(body)})
				if r.Method == http.MethodPut {
					stored = tc.stored
					w.WriteHeader(http.StatusNoContent)
					return
				}
				_, _ = w.Write([]byte(fmt.Sprintf(
					`{"id":7324,"name":"svk team","web_site":%q,"forum_link":""}`, stored)))
			})

			err := c.SetTeamSite(context.Background(), 7324, tc.want)
			if tc.ok && err != nil {
				t.Errorf("SetTeamSite(%q) stored as %q was reported as failed: %v", tc.want, tc.stored, err)
			}
			if !tc.ok && err == nil {
				t.Errorf("SetTeamSite(%q) stored as %q was reported as successful", tc.want, tc.stored)
			}
		})
	}
}

// currentTeamID fails for three unrelated reasons and only one of them — the
// account is in no team — means a leave worked. A session read that 401s says
// nothing about whether the team was left, and must not pass for success.
func TestNewEngineLeaveTeamDoesNotTrustAFailedVerification(t *testing.T) {
	left := false
	c := newEngineClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/teams/7324/leave":
			left = true
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/auth/session" && left:
			// The session read fails after the write, which is exactly when the
			// caller most needs to be told the outcome is unknown.
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"Invalid token"}`))
		default:
			_, _ = w.Write([]byte(`{"user":{"id":156988,"login":"svk","team_id":7324}}`))
		}
	})

	err := c.LeaveTeam(context.Background(), 7324)
	if err == nil {
		t.Fatal("LeaveTeam reported success although it could not read the session back")
	}
	if !strings.Contains(err.Error(), "verify state") {
		t.Errorf("error = %v, want it to say the verification failed", err)
	}
}

// Every team mutation is verified against the state afterwards, the way the
// legacy pages were: a backend that accepts the call and changes nothing must
// not be reported as success. The deployed engine has already been observed
// doing exactly that on another route.
func TestNewEngineTeamMutationsRejectSilentNoOps(t *testing.T) {
	cases := []struct {
		name string
		call func(*Client) error
	}{
		{"accept invitation", func(c *Client) error { return c.AcceptTeamInvitation(context.Background(), 7328) }},
		{"reject invitation", func(c *Client) error { return c.RejectTeamInvitation(context.Background(), 7328) }},
		{"leave", func(c *Client) error { return c.LeaveTeam(context.Background(), 7324) }},
		{"rename", func(c *Client) error { return c.RenameTeam(context.Background(), 7324, "другое имя") }},
		{"set site", func(c *Client) error { return c.SetTeamSite(context.Background(), 7324, "https://other.example") }},
		{"set forum", func(c *Client) error { return c.SetTeamForum(context.Background(), 7324, "https://other.forum") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls []teamCall
			c, world := newTeamWorldClient(t, &calls)
			world.frozen = true

			err := tc.call(c)
			if err == nil {
				t.Fatalf("%s reported success although nothing changed", tc.name)
			}
			var actionErr *TeamActionError
			if !errors.As(err, &actionErr) {
				t.Fatalf("error is not *TeamActionError: %v", err)
			}
		})
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
			c, _ := newTeamWorldClient(t, &calls)
			if err := tc.call(c); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			// Read the team, write it back whole, then read it again to confirm
			// the change landed.
			if len(calls) != 3 {
				t.Fatalf("calls = %v, want a read, a write and a verifying read", callPaths(calls))
			}
			if calls[0].method != http.MethodGet || calls[1].method != http.MethodPut ||
				calls[2].method != http.MethodGet {
				t.Fatalf("methods = %s, %s, %s; want GET, PUT, GET",
					calls[0].method, calls[1].method, calls[2].method)
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
