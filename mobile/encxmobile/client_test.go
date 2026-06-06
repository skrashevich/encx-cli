package encxmobile

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	c := NewClient("tech.en.cx", true)
	if c == nil {
		t.Fatal("NewClient returned nil")
	}
	if c.Domain() != "tech.en.cx" {
		t.Fatalf("domain = %q, want tech.en.cx", c.Domain())
	}
}

func TestLoginErrorText(t *testing.T) {
	if LoginErrorText(0) == "" {
		t.Fatal("LoginErrorText(0) empty")
	}
	if LoginErrorText(999) == "" {
		t.Fatal("LoginErrorText(999) empty")
	}
}

func TestEventText(t *testing.T) {
	if EventText(0) == "" {
		t.Fatal("EventText(0) empty")
	}
}

func TestCookieRoundTrip(t *testing.T) {
	c := NewClient("tech.en.cx", true)
	data, err := c.ExportCookies()
	if err != nil {
		t.Fatalf("ExportCookies: %v", err)
	}
	c2 := NewClient("tech.en.cx", true)
	if err := c2.ImportCookies(data); err != nil {
		t.Fatalf("ImportCookies: %v", err)
	}
}

func TestParseTeamLinks(t *testing.T) {
	html := `<a href="/Teams/TeamDetails.aspx?tid=42">Alpha Team</a>`
	out, err := ParseTeamLinks(html)
	if err != nil {
		t.Fatalf("ParseTeamLinks: %v", err)
	}
	var teams []struct {
		TeamId int    `json:"teamId"`
		Name   string `json:"name"`
	}
	if err := json.Unmarshal([]byte(out), &teams); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(teams) != 1 || teams[0].TeamId != 42 || teams[0].Name != "Alpha Team" {
		t.Fatalf("unexpected teams: %+v", teams)
	}
}

func TestParseTeamManagementInfo(t *testing.T) {
	html := `
		<a id="lnkTeamName" href="/Teams/TeamDetails.aspx?tid=200714">svk team</a>
		id&nbsp;1408504 <a href="/UserDetails.aspx?uid=1408504">Santa</a>
		<a href="/Teams/TeamDetails.aspx?action=remove_invitation&uid=1408504&tid=200714">Удалить приглашение</a>
	`
	out, err := ParseTeamManagementInfo(html, 200714)
	if err != nil {
		t.Fatalf("ParseTeamManagementInfo: %v", err)
	}
	var info struct {
		TeamID             int `json:"team_id"`
		PendingInvitations []struct {
			UserID int    `json:"user_id"`
			Login  string `json:"login"`
		} `json:"pending_invitations"`
	}
	if err := json.Unmarshal([]byte(out), &info); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if info.TeamID != 200714 || len(info.PendingInvitations) != 1 || info.PendingInvitations[0].Login != "Santa" {
		t.Fatalf("unexpected info: %+v", info)
	}
}

func TestParseTeamInvitations(t *testing.T) {
	html := `
		<a href="/Teams/TeamDetails.aspx?tid=144561">PHGP</a>
		<a href="/Teams/TeamDetails.aspx?action=accept_invitation&tid=144561">Вступить</a>
	`
	out, err := ParseTeamInvitations(html)
	if err != nil {
		t.Fatalf("ParseTeamInvitations: %v", err)
	}
	var invitations []struct {
		TeamID int    `json:"team_id"`
		Name   string `json:"name"`
	}
	if err := json.Unmarshal([]byte(out), &invitations); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(invitations) != 1 || invitations[0].TeamID != 144561 || invitations[0].Name != "PHGP" {
		t.Fatalf("unexpected invitations: %+v", invitations)
	}
}

func TestNewClientWithOptions(t *testing.T) {
	c := NewClientWithOptions("tech.en.cx", true, false, 30, "en")
	if c.Domain() != "tech.en.cx" {
		t.Fatalf("domain = %q", c.Domain())
	}
}

func TestCodeSendTimeoutDefault(t *testing.T) {
	c := NewClient("tech.en.cx", true)
	if c.codeSendTimeout != defaultCodeSendTimeout {
		t.Fatalf("codeSendTimeout = %v, want %v", c.codeSendTimeout, defaultCodeSendTimeout)
	}
}

func TestSetCodeSendTimeoutSeconds(t *testing.T) {
	c := NewClient("tech.en.cx", true)
	c.SetCodeSendTimeoutSeconds(3)
	if c.codeSendTimeout != 3*time.Second {
		t.Fatalf("codeSendTimeout = %v, want 3s", c.codeSendTimeout)
	}
	c.SetCodeSendTimeoutSeconds(0)
	if c.codeSendTimeout != defaultCodeSendTimeout {
		t.Fatalf("reset codeSendTimeout = %v, want default", c.codeSendTimeout)
	}
}
