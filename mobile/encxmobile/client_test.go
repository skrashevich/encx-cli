package encxmobile

import (
	"encoding/json"
	"testing"
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

func TestNewClientWithOptions(t *testing.T) {
	c := NewClientWithOptions("tech.en.cx", true, false, 30, "en")
	if c.Domain() != "tech.en.cx" {
		t.Fatalf("domain = %q", c.Domain())
	}
}
