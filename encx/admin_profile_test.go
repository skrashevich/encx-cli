package encx

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestIsPlausibleEncounterLogin(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"skrushovich", true},
		{"-", false},
		{"—", false},
		{"", false},
		{"a", false},
		{"John Doe", false},
	}
	for _, tt := range tests {
		if got := IsPlausibleEncounterLogin(tt.in); got != tt.want {
			t.Errorf("IsPlausibleEncounterLogin(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestProfileLoginSkipsDashLink(t *testing.T) {
	body := `<a href="/UserDetails.aspx">-</a><a href="/UserDetails.aspx">demo_user</a>`
	loginRe := regexp.MustCompile(`(?i)<a[^>]*href="/UserDetails\.aspx"[^>]*>([^<]+)</a>`)
	var login string
	for _, m := range loginRe.FindAllStringSubmatch(body, 8) {
		candidate := strings.TrimSpace(m[1])
		if IsPlausibleEncounterLogin(candidate) {
			login = candidate
			break
		}
	}
	if login != "demo_user" {
		t.Fatalf("login = %q, want demo_user", login)
	}
}

func TestGetProfileParsesBoldRank(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/UserDetails.aspx" {
			t.Fatalf("path = %q, want /UserDetails.aspx", r.URL.Path)
		}
		_, _ = w.Write([]byte(`
			<a href="/UserDetails.aspx" class="white bold no_decoration">skrashevich</a>
			(id&nbsp;<span class="white">1516219</span>)<br/>
			<b><a href="/UserDetails.aspx" id="lnkUserName">Сергей Крашевич</a></b><br/>
			<span class="h9">232,66 / <b>Лейтенант</b></span><br/>
			<a href="/Teams/TeamDetails.aspx?tid=200714">svk team</a>
		`))
	}))
	defer server.Close()

	client := newContractTestClient(server.URL)
	profile, err := client.GetProfile(t.Context())
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	if profile.Rank != "Лейтенант" {
		t.Fatalf("Rank = %q, want Лейтенант", profile.Rank)
	}
	if profile.Points != "232,66" {
		t.Fatalf("Points = %q, want 232,66", profile.Points)
	}
}
