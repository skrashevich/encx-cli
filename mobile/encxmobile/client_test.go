package encxmobile

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
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

func TestSetGameRequestMinIntervalMillis(t *testing.T) {
	c := NewClient("tech.en.cx", true)
	if c.gameRequestMinInterval != defaultGameRequestMinInterval {
		t.Fatalf("gameRequestMinInterval = %v, want %v", c.gameRequestMinInterval, defaultGameRequestMinInterval)
	}

	c.SetGameRequestMinIntervalMillis(25)
	if c.gameRequestMinInterval != 25*time.Millisecond {
		t.Fatalf("gameRequestMinInterval = %v, want 25ms", c.gameRequestMinInterval)
	}

	c.SetGameRequestMinIntervalMillis(0)
	if c.gameRequestMinInterval != 0 {
		t.Fatalf("gameRequestMinInterval = %v, want disabled", c.gameRequestMinInterval)
	}
}

func TestGameRequestsArePacedAcrossConcurrentCalls(t *testing.T) {
	var mu sync.Mutex
	var starts []time.Time

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/gameengines/encounter/play/42" {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		starts = append(starts, time.Now())
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"GameId":42}`))
	}))
	defer srv.Close()

	c := NewClientWithOptions(strings.TrimPrefix(srv.URL, "http://"), false, true, 5, "")
	c.SetGameRequestMinIntervalMillis(50)

	ready := make(chan struct{})
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-ready
			_, err := c.GetGameModel(42)
			errs <- err
		}()
	}
	close(ready)

	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("GetGameModel: %v", err)
		}
	}

	mu.Lock()
	got := append([]time.Time(nil), starts...)
	mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("request count = %d, want 2", len(got))
	}
	sort.Slice(got, func(i, j int) bool { return got[i].Before(got[j]) })
	if gap := got[1].Sub(got[0]); gap < 45*time.Millisecond {
		t.Fatalf("game requests gap = %v, want at least 45ms", gap)
	}
}
