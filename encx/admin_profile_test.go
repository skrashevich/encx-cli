package encx

import (
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
