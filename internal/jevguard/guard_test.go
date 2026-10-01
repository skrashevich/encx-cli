package jevguard

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

const mutationIntent = `{"model":"jev-test","answers":{"intent":{"type":"choice","choice":"modify","confidence":0.95},"explicit_change":{"type":"noul","noul":0.95}},"usage":{"cost_rub":0.01}}`
const auditIntent = `{"model":"jev-test","answers":{"intent":{"type":"choice","choice":"audit","confidence":0.95},"explicit_change":{"type":"noul","noul":0.02}}}`
const aligned = `{"model":"jev-test","answers":{"alignment":{"type":"choice","choice":"within_scope","confidence":0.95},"unrequested_work":{"type":"noul","noul":0.01}}}`
const outside = `{"model":"jev-test","answers":{"alignment":{"type":"choice","choice":"outside_scope","confidence":0.95},"unrequested_work":{"type":"noul","noul":0.95}}}`
const unsure = `{"model":"jev-test","answers":{"alignment":{"type":"choice","choice":"unclear","confidence":0.3},"unrequested_work":{"type":"noul","noul":0.5}}}`

func testClient(t *testing.T, responses ...string) (*Client, *[]map[string]any) {
	t.Helper()
	requests := new([]map[string]any)
	c := New("test-secret-key")
	c.http.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != BaseURL+"/systemone" || r.Method != "POST" || r.Header.Get("Authorization") != "Bearer test-secret-key" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["model"] != "typesafe/jev" {
			t.Errorf("model=%v", body["model"])
		}
		*requests = append(*requests, body)
		if len(responses) == 0 {
			t.Fatal("unexpected JEV request")
		}
		value := responses[0]
		responses = responses[1:]
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(value)), Header: make(http.Header)}, nil
	})
	return c, requests
}

func TestEnabledOnlyResolvedPolzaAPI(t *testing.T) {
	for _, tc := range []struct {
		base, auth string
		want       bool
	}{
		{BaseURL, "", true}, {BaseURL + "/", "apikey", true},
		{BaseURL, "codex", false}, {BaseURL, "gigachat", false},
		{"https://openrouter.ai/api/v1", "", false}, {"http://127.0.0.1/v1", "", false},
		{"https://polza.ai.evil.test/api/v1", "", false}, {"https://polza.ai/api/v1?proxy=1", "", false},
	} {
		if got := Enabled(tc.base, tc.auth); got != tc.want {
			t.Errorf("Enabled(%q,%q)=%v", tc.base, tc.auth, got)
		}
	}
}

func TestAuditNeverExecutesContentMutationButAcceptsProposal(t *testing.T) {
	c, requests := testClient(t, auditIntent, aligned)
	turn, err := c.Start(t.Context(), []string{"Проверь бонусы, ничего не меняй"})
	if err != nil {
		t.Fatal(err)
	}
	call := Call{Name: "admin_update_bonus", Mutating: true, ContentMutation: true}
	if d := turn.Check(t.Context(), call); d.Action != "propose" {
		t.Fatalf("decision=%+v", d)
	}
	if len(*requests) != 1 {
		t.Fatal("audit mutation should be rejected before another API call")
	}
	if d := turn.Check(t.Context(), Call{Name: "propose_admin_fix", Mutating: true, Proposal: true}); d.Action != "allow" {
		t.Fatalf("proposal=%+v", d)
	}
	call.Approved = true
	if d := turn.Check(t.Context(), call); d.Action != "allow" {
		t.Fatalf("human-approved call=%+v", d)
	}
}

func TestToolScopeAndUncertainty(t *testing.T) {
	for _, tc := range []struct {
		answer   string
		mutating bool
		want     string
	}{
		{aligned, true, "allow"}, {outside, true, "deny"}, {outside, false, "deny"},
		{unsure, true, "confirm"}, {unsure, false, "allow"},
	} {
		c, _ := testClient(t, mutationIntent, tc.answer)
		turn, err := c.Start(t.Context(), []string{"Измени подсказку"})
		if err != nil {
			t.Fatal(err)
		}
		d := turn.Check(t.Context(), Call{Name: "admin_update_task", Mutating: tc.mutating, ContentMutation: tc.mutating})
		if d.Action != tc.want || d.Model != "jev-test" {
			t.Fatalf("decision=%+v, want %s", d, tc.want)
		}
	}
}

func TestContinuationAndEvidenceAreRedactedWithoutLosingCodes(t *testing.T) {
	c, requests := testClient(t, mutationIntent, aligned)
	turn, err := c.Start(t.Context(), []string{"Создай сектор с кодом дом123", "продолжай test-secret-key"})
	if err != nil {
		t.Fatal(err)
	}
	turn.Remember("admin_level_content", map[string]any{"game_id": 42}, `{"task":"Исходный текст","password":"hidden-password","api_token":"hidden-token"}`)
	turn.Check(t.Context(), Call{Name: "admin_create_sector", Mutating: true, Arguments: map[string]any{"answers": []any{"дом123"}, "password": "another-password", "nested": map[string]any{"api_key": "hidden-api-key"}}})
	body, _ := json.Marshal(*requests)
	for _, secret := range []string{"test-secret-key", "hidden-password", "hidden-token", "another-password", "hidden-api-key"} {
		if strings.Contains(string(body), secret) {
			t.Errorf("secret leaked: %s", secret)
		}
	}
	state := (*requests)[1]["state"].(map[string]any)
	users := state["user_messages"].([]any)
	if len(users) != 2 || !strings.Contains(users[0].(string), "дом123") || len(state["read_evidence"].([]any)) != 1 {
		t.Fatalf("state=%v", state)
	}
}

func TestInvalidResponsesRequireApprovalOfMutations(t *testing.T) {
	for _, invalid := range []string{
		`{}`, `{"model":"x","answers":{}}`,
		strings.Replace(aligned, `"confidence":0.95`, `"confidence":2`, 1),
		strings.Replace(aligned, `"choice":"within_scope"`, `"choice":"invented"`, 1),
		strings.Replace(aligned, `"noul":0.01`, `"noul":null`, 1),
	} {
		c, _ := testClient(t, mutationIntent, invalid)
		turn, err := c.Start(t.Context(), []string{"Измени"})
		if err != nil {
			t.Fatal(err)
		}
		if d := turn.Check(t.Context(), Call{Mutating: true}); d.Action != "confirm" {
			t.Fatalf("invalid response allowed mutation: %+v", d)
		}
	}
	c, requests := testClient(t, `{}`)
	turn, err := c.Start(t.Context(), []string{"Измени"})
	if err == nil || !turn.Unavailable {
		t.Fatal("invalid classification accepted")
	}
	if turn.Check(context.Background(), Call{Mutating: true}).Action != "confirm" || turn.Check(context.Background(), Call{}).Action != "allow" || len(*requests) != 1 {
		t.Fatal("unavailable behavior incorrect")
	}
}

func TestRequestBudgetDoesNotCutAuthorization(t *testing.T) {
	c, requests := testClient(t)
	turn, err := c.Start(t.Context(), []string{strings.Repeat("x", 90000)})
	if err == nil || !turn.Unavailable || len(*requests) != 0 {
		t.Fatal("oversized authorization was sent or accepted")
	}
}
