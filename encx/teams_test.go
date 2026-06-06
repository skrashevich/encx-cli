package encx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseTeamManagementInfo(t *testing.T) {
	body := `
		<a id="lnkTeamName" href="/Teams/TeamDetails.aspx?tid=200714">svk team</a>
		id&nbsp;1408504 <a href="/UserDetails.aspx?uid=1408504">Santa</a>
		<a href="/Teams/TeamDetails.aspx?action=remove_invitation&uid=1408504&tid=200714">Удалить приглашение</a>
		<a href="/Teams/TeamDetails.aspx?tid=144561">PHGP</a>
		<a href="/Teams/TeamDetails.aspx?action=accept_invitation&tid=144561">Вступить</a>
		<a href="/Teams/TeamDetails.aspx?action=reject_invitation&tid=144561">Отказать</a>
	`
	info := ParseTeamManagementInfo(body, 200714)
	if info.TeamName != "svk team" {
		t.Fatalf("TeamName = %q", info.TeamName)
	}
	if len(info.PendingInvitations) != 1 || info.PendingInvitations[0].UserID != 1408504 || info.PendingInvitations[0].Login != "Santa" {
		t.Fatalf("pending invitations = %#v", info.PendingInvitations)
	}
	if info.Actions["accept_invitation"] != "" || info.Actions["reject_invitation"] != "" {
		t.Fatalf("actions = %#v", info.Actions)
	}
	if info.Actions["remove_invitation"] == "" {
		t.Fatalf("missing remove_invitation action: %#v", info.Actions)
	}
}

func TestParseTeamManagementInfoInfersTeamID(t *testing.T) {
	body := `<a id="lnkTeamName" href="/Teams/TeamDetails.aspx?tid=200714">svk team</a>`
	info := ParseTeamManagementInfo(body, 0)
	if info.TeamID != 200714 || info.TeamName != "svk team" {
		t.Fatalf("info = %#v", info)
	}
}

func TestParseTeamInvitations(t *testing.T) {
	body := `
		<a href="/Teams/TeamDetails.aspx?tid=144561">PHGP</a>
		<a href="/Teams/TeamDetails.aspx?action=accept_invitation&tid=144561">Вступить</a>
		<a href="/Teams/TeamDetails.aspx?action=reject_invitation&tid=144561">Отказать</a>
	`
	invitations := ParseTeamInvitations(body)
	if len(invitations) != 1 || invitations[0].TeamID != 144561 || invitations[0].Name != "PHGP" {
		t.Fatalf("invitations = %#v", invitations)
	}
}

func TestInviteTeamMemberPostsViewStateAndImageButton(t *testing.T) {
	var got string
	invited := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if invited {
				_, _ = w.Write([]byte(`
					id&nbsp;1408504 <a href="/UserDetails.aspx?uid=1408504">demo_user</a>
					<a href="/Teams/TeamDetails.aspx?action=remove_invitation&uid=1408504&tid=200714">Удалить приглашение</a>
				`))
				return
			}
			_, _ = w.Write([]byte(`
				<form id="aspnetForm" method="post">
					<input type="hidden" name="__VIEWSTATE" value="state">
					<input type="hidden" name="__VIEWSTATEGENERATOR" value="gen">
					<input type="text" name="NewMember">
					<input type="image" name="ctl06_content_ctl00_btnInvite">
				</form>
			`))
		case http.MethodPost:
			if err := r.ParseForm(); err != nil {
				t.Fatalf("ParseForm: %v", err)
			}
			got = r.Form.Encode()
			invited = true
			_, _ = w.Write([]byte("ok"))
		default:
			t.Fatalf("method = %s", r.Method)
		}
	}))
	defer server.Close()

	client := newContractTestClient(server.URL)
	if err := client.InviteTeamMember(t.Context(), 200714, "demo_user"); err != nil {
		t.Fatalf("InviteTeamMember: %v", err)
	}
	for _, want := range []string{"NewMember=demo_user", "__VIEWSTATE=state", "__VIEWSTATEGENERATOR=gen", "ctl06_content_ctl00_btnInvite.x=1", "ctl06_content_ctl00_btnInvite.y=1"} {
		if !strings.Contains(got, want) {
			t.Fatalf("posted form %q does not contain %q", got, want)
		}
	}
}

func TestRequestTeamMembershipPostsTeamName(t *testing.T) {
	var got string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if r.URL.Path != "/Teams/SendRequest.aspx" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		got = r.Form.Encode()
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	client := newContractTestClient(server.URL)
	if err := client.RequestTeamMembership(t.Context(), "svk team"); err != nil {
		t.Fatalf("RequestTeamMembership: %v", err)
	}
	for _, want := range []string{"TeamName=svk+team", "Submit.x=1", "Submit.y=1"} {
		if !strings.Contains(got, want) {
			t.Fatalf("posted form %q does not contain %q", got, want)
		}
	}
}

func TestAcceptTeamInvitationFailsWhenStateDoesNotChange(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/Teams/TeamDetails.aspx" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.URL.Query().Get("action") == "accept_invitation" {
			_, _ = w.Write([]byte(`<html><body>Вы являетесь капитаном своей команды и не можете перейти в другую команду.</body></html>`))
			return
		}
		_, _ = w.Write([]byte(`
			<a id="lnkTeamName" href="/Teams/TeamDetails.aspx?tid=1">Own Team</a>
			<a href="/Teams/TeamDetails.aspx?tid=2">Other Team</a>
			<a href="/Teams/TeamDetails.aspx?action=accept_invitation&tid=2">Вступить</a>
		`))
	}))
	defer server.Close()

	client := newContractTestClient(server.URL)
	err := client.AcceptTeamInvitation(t.Context(), 2)
	if err == nil {
		t.Fatal("AcceptTeamInvitation returned nil")
	}
	if !strings.Contains(err.Error(), "капитан") {
		t.Fatalf("error = %v", err)
	}
}

func TestAcceptTeamInvitationSucceedsWhenCurrentTeamChanges(t *testing.T) {
	accepted := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/Teams/TeamDetails.aspx" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.URL.Query().Get("action") == "accept_invitation" {
			accepted = true
			_, _ = w.Write([]byte(`<html><body>ok</body></html>`))
			return
		}
		if accepted {
			_, _ = w.Write([]byte(`<a id="lnkTeamName" href="/Teams/TeamDetails.aspx?tid=2">Other Team</a>`))
			return
		}
		_, _ = w.Write([]byte(`
			<a id="lnkTeamName" href="/Teams/TeamDetails.aspx?tid=1">Own Team</a>
			<a href="/Teams/TeamDetails.aspx?tid=2">Other Team</a>
			<a href="/Teams/TeamDetails.aspx?action=accept_invitation&tid=2">Вступить</a>
		`))
	}))
	defer server.Close()

	client := newContractTestClient(server.URL)
	if err := client.AcceptTeamInvitation(t.Context(), 2); err != nil {
		t.Fatalf("AcceptTeamInvitation: %v", err)
	}
}
