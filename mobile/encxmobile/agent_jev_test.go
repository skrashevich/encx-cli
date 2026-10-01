package encxmobile

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/skrashevich/encx-cli/agenttools"
	"github.com/skrashevich/encx-cli/internal/jevguard"
)

type jevTransport func(*http.Request) (*http.Response, error)

func (f jevTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMobileJEVOnlyForResolvedPolza(t *testing.T) {
	for _, tc := range []struct {
		provider, base, auth string
		enabled              bool
	}{
		{providerPolza, "", "", true}, {providerPolza, jevguard.BaseURL + "/", "apikey", true},
		{providerOpenAI, "", "", false}, {providerPolza, "https://gateway.test/v1", "", false},
		{providerPolza, jevguard.BaseURL, AuthMethodCodex, false},
	} {
		s, err := newAgentSession(newFakeEngine(), agentConfig{Provider: tc.provider, APIBase: tc.base, AuthMethod: tc.auth, Model: "test", APIKey: "test"}, agenttools.PolicyFull, &scriptedProvider{})
		if err != nil {
			t.Fatal(err)
		}
		if (s.jevClient != nil) != tc.enabled {
			t.Errorf("provider=%s base=%s auth=%s enabled=%v", tc.provider, tc.base, tc.auth, s.jevClient != nil)
		}
	}
}

func TestMobileJEVGuardsEngineCallsAndPreservesApproval(t *testing.T) {
	for _, tc := range []struct {
		name, classify, scope string
		policy                agenttools.Policy
		approve, executed     bool
		confirmations         int
	}{
		{"audit", `"audit"`, `"within_scope"`, agenttools.PolicyFull, true, false, 0},
		{"outside_scope", `"act"`, `"outside_scope"`, agenttools.PolicyFull, true, false, 0},
		{"existing_approval", `"act"`, `"within_scope"`, agenttools.PolicyApprove, true, true, 1},
		{"uncertain", `"act"`, `"unclear"`, agenttools.PolicyFull, false, false, 1},
		{"unavailable", `"invalid"`, `"within_scope"`, agenttools.PolicyFull, false, false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = original })
			var mu sync.Mutex
			requests := 0
			http.DefaultTransport = jevTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() != jevguard.BaseURL+"/systemone" {
					t.Errorf("unexpected URL %s", r.URL)
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					return nil, err
				}
				mu.Lock()
				requests++
				mu.Unlock()
				content := `{"model":"jev-test","answers":{"alignment":{"type":"choice","choice":` + tc.scope + `,"confidence":0.95},"unrequested_work":{"type":"noul","noul":0.01}}}`
				if _, exists := body["questions"].(map[string]any)["intent"]; exists {
					explicit := "0.95"
					if tc.name == "audit" {
						explicit = "0.01"
					}
					content = `{"model":"jev-test","answers":{"intent":{"type":"choice","choice":` + tc.classify + `,"confidence":0.95},"explicit_change":{"type":"noul","noul":` + explicit + `}}}`
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(content))}, nil
			})
			engine := newFakeEngine()
			provider := &scriptedProvider{responses: []*providers.LLMResponse{toolCallResponse("enc_send_code", `{"game_id":42,"code":"дом123"}`), {Content: "Ответ"}}}
			s, err := newAgentSession(engine, agentConfig{Provider: providerPolza, Model: "test", APIKey: "test", MaxIterations: 3}, tc.policy, provider)
			if err != nil {
				t.Fatal(err)
			}
			delegate := &recordingDelegate{session: s, approve: tc.approve}
			s.SetDelegate(delegate)
			if _, err = s.SendMessage("Проверь уровень / отправь указанный код"); err != nil {
				t.Fatal(err)
			}
			if engine.called("SendCode") != tc.executed {
				t.Fatalf("engine executed=%v want=%v", engine.called("SendCode"), tc.executed)
			}
			if len(delegate.confirmations) != tc.confirmations {
				t.Fatalf("confirmations=%d want=%d", len(delegate.confirmations), tc.confirmations)
			}
			if requests == 0 {
				t.Fatal("JEV not called")
			}
		})
	}
}
