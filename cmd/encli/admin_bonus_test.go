package main

import (
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/skrashevich/encx-cli/encx"
)

func TestBonusCLIAndLLMWrite(t *testing.T) {
	// The execution wrapper captures stdout and uses process globals.
	t.Setenv("HOME", t.TempDir())
	var saved url.Values
	var saves int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/Administration/Games/LevelManager.aspx":
			fmt.Fprint(w, "admin")
		case "/Administration/Games/BonusEdit.aspx":
			switch r.URL.Query().Get("action") {
			case "edit":
				fmt.Fprint(w, `<input name="txtBonusName" value="Б 1">
<input name="txtHours" value="1"><input name="txtMinutes" value="2"><input name="txtSeconds" value="30">
<input name="answer_1" value="дом1"><input name="negative" checked="checked" type="checkbox">
<input name="rbAllLevels" value="1" checked="checked" type="radio">
<input name="chkDelay" checked="checked" type="checkbox"><input name="txtDelaySeconds" value="15">
<textarea name="txtTask">task</textarea><textarea name="txtHelp">hint</textarea>`)
			case "save", "update":
				if r.URL.Query().Get("bonus") == "42" && r.URL.Query().Get("action") != "update" {
					t.Error("updating an existing bonus must not use the create action")
				}
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				saved = r.PostForm
				saves++
				fmt.Fprint(w, "ok")
			default:
				t.Errorf("unexpected bonus action: %s", r.URL)
			}
		default:
			t.Errorf("unexpected request: %s", r.URL)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	cfg := &config{domain: strings.TrimPrefix(srv.URL, "http://"), gameId: 1, jsonOutput: true}
	client := encx.New(cfg.domain, encx.WithHTTP(), encx.WithEngine(encx.EngineLegacy), encx.WithAdminDelay(0))
	saveSession(cfg, client)

	for _, minutes := range []int{3, 5, 10} {
		for _, via := range []string{"cli", "llm"} {
			t.Run(fmt.Sprintf("%s_%d", via, minutes), func(t *testing.T) {
				before := saves
				if via == "cli" {
					captureStdout(t, func() {
						cmdAdminUpdateBonus(t.Context(), cfg, client, []string{"2", "42", "award_hours=0", fmt.Sprintf("award_minutes=%d", minutes), "award_seconds=0", "negative=false"})
					})
				} else {
					out := executeToolCallSafe(t.Context(), cfg, client, nil, "admin_update_bonus",
						fmt.Sprintf(`{"game_id":1,"level_number":2,"bonus_id":42,"award_hours":0,"award_minutes":%d,"award_seconds":0,"negative":false}`, minutes))
					if strings.Contains(out, "error") {
						t.Fatal(out)
					}
				}
				if saves != before+1 {
					t.Fatalf("writes = %d, before = %d", saves, before)
				}
				for key, want := range map[string]string{
					"txtHours": "0", "txtMinutes": fmt.Sprint(minutes), "txtSeconds": "0",
					"txtBonusName": "Б 1", "txtTask": "task", "txtHelp": "hint", "answer_1": "дом1",
					"rbAllLevels": "1", "chkDelay": "on", "txtDelaySeconds": "15", "negative": "",
				} {
					if got := saved.Get(key); got != want {
						t.Errorf("%s = %q, want %q", key, got, want)
					}
				}
			})
		}
	}
	t.Run("partial_preserves_time", func(t *testing.T) {
		out := executeToolCallSafe(t.Context(), cfg, client, nil, "admin_update_bonus", `{"level_number":2,"bonus_id":42,"award_minutes":5}`)
		if strings.Contains(out, "error") {
			t.Fatal(out)
		}
		if saved.Get("txtHours") != "1" || saved.Get("txtSeconds") != "30" || saved.Get("negative") != "on" {
			t.Fatal(saved)
		}
	})
	t.Run("create_cli", func(t *testing.T) {
		captureStdout(t, func() {
			cmdAdminCreateBonus(t.Context(), cfg, client, []string{"2", "22", "Б 1", "a=b", "--", "award_minutes=3", "negative=true"})
		})
		if saved.Get("txtMinutes") != "3" || saved.Get("negative") != "on" || saved.Get("answer_-1") != "a=b" {
			t.Fatal(saved)
		}
	})
	t.Run("create_llm_exact_answers", func(t *testing.T) {
		out := executeToolCallSafe(t.Context(), cfg, client, nil, "admin_create_bonus", `{"level_number":2,"level_id":22,"name":"Б 1","answers":["a,b","дом1"],"award_minutes":5,"task":"task","hint":"hint"}`)
		if strings.Contains(out, "error") {
			t.Fatal(out)
		}
		if saved.Get("txtMinutes") != "5" || saved.Get("answer_-1") != "a,b" || saved.Get("answer_-2") != "дом1" {
			t.Fatal(saved)
		}
	})
	t.Run("guard_uses_saved_name", func(t *testing.T) {
		before := saves
		out := executeToolCallSafe(t.Context(), cfg, client, &llmSession{latestUserMessage: "дом1"}, "admin_update_bonus", `{"level_number":2,"bonus_id":42,"answers":["дом"]}`)
		if !strings.Contains(out, "drops the numeric suffix") || saves != before {
			t.Fatal(out)
		}
	})
}

func TestBonusPatchValidationAndZero(t *testing.T) {
	for _, arg := range []string{"award_seconds=-1", "award_minutes=oops", "award_hours=1.5", "negative=maybe", "unknown=1", "award_minutes"} {
		if _, err := parseAdminBonusPatch([]string{arg}); err == nil {
			t.Errorf("accepted %s", arg)
		}
	}
	before := encx.AdminBonus{Name: "keep", Answers: []string{"a,b"}, AwardHours: 1, AwardMinutes: 2, AwardSeconds: 30, Negative: true, DelaySeconds: 15}
	p, err := parseAdminBonusPatch([]string{"award_hours=0", "award_minutes=0", "award_seconds=0", "negative=false"})
	if err != nil {
		t.Fatal(err)
	}
	got := before
	if err := p.apply(&got); err != nil {
		t.Fatal(err)
	}
	want := before
	want.AwardHours, want.AwardMinutes, want.AwardSeconds, want.Negative = 0, 0, 0, false
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for _, raw := range []string{`{"award_minutes":1.5}`, `{"award_minutes":-1}`, `{"award_seconds":"oops"}`} {
		out := executeToolCallSafe(t.Context(), &config{}, nil, nil, "admin_update_bonus", raw)
		if !strings.Contains(out, "Invalid bonus fields") {
			t.Fatal(out)
		}
	}
}

func TestBonusToolRegistrationAndApproval(t *testing.T) {
	for _, name := range []string{"admin_create_bonus", "admin_update_bonus"} {
		found := false
		for _, tool := range getTools() {
			if tool.Function.Name != name {
				continue
			}
			found = true
			var schema struct {
				Properties map[string]any `json:"properties"`
			}
			if err := json.Unmarshal(tool.Function.Parameters, &schema); err != nil {
				t.Fatal(err)
			}
			for _, field := range []string{"award_hours", "award_minutes", "award_seconds", "negative", "answers"} {
				if _, ok := schema.Properties[field]; !ok {
					t.Errorf("%s missing %s", name, field)
				}
			}
		}
		if !found || !isAdminMutationTool(name) || !isProposalMutationTool(name) {
			t.Errorf("missing registration: %s", name)
		}
	}
	out := executeToolCallSafe(t.Context(), &config{}, nil, &llmSession{securityMode: SecurityModeReadonly}, "admin_update_bonus", `{}`)
	if !strings.Contains(out, "read-only") {
		t.Fatal(out)
	}
	details := strings.Join(formatToolApprovalDetails(nil, "admin_update_bonus", `{"bonus_id":42,"award_minutes":3,"award_seconds":0,"negative":false}`), "\n")
	for _, want := range []string{"#42", "award_minutes: 3", "award_seconds: 0", "negative: false"} {
		if !strings.Contains(details, want) {
			t.Errorf("missing %q in %s", want, details)
		}
	}
}

func TestBonusLLMNewEngine(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var saved map[string]any
	var method string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/auth/session":
			fmt.Fprint(w, "{}")
		case "/admin/games/1/levels":
			fmt.Fprint(w, `{"levels":[{"level_id":22,"level_number":2}]}`)
		case "/admin/games/1/levels/22/editor":
			fmt.Fprint(w, `{"bonuses":[{"bonus_id":42,"bonus_name":"Б 1","task":"task","bonus_help":"hint","answers":["дом1","a,b"],"bonus_time":30,"all_levels":true,"negative":true,"has_delay":true,"delay_sec":15,"has_relative_limit":true,"life_time_sec":120}]}`)
		case "/admin/games/1/levels/22/bonuses", "/admin/games/1/levels/22/bonuses/42":
			method = r.Method
			if err := json.UnmarshalRead(r.Body, &saved); err != nil {
				t.Error(err)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request: %s", r.URL)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	cfg := &config{domain: strings.TrimPrefix(srv.URL, "http://"), gameId: 1, jsonOutput: true}
	client := encx.New(cfg.domain, encx.WithHTTP(), encx.WithEngine(encx.EngineNew), encx.WithAPIBaseURL(srv.URL), encx.WithAdminDelay(0))
	saveSession(cfg, client)
	for _, minutes := range []int{3, 5, 10} {
		saved = nil
		out := executeToolCallSafe(t.Context(), cfg, client, nil, "admin_update_bonus",
			fmt.Sprintf(`{"level_number":2,"bonus_id":42,"award_hours":0,"award_minutes":%d,"award_seconds":0,"negative":false}`, minutes))
		if strings.Contains(out, "error") {
			t.Fatal(out)
		}
		if method != http.MethodPut {
			t.Fatalf("method = %s", method)
		}
		for key, want := range map[string]any{"bonus_time": float64(minutes * 60), "negative": false, "all_levels": true, "task": "task", "bonus_help": "hint", "delay_sec": float64(15), "life_time_sec": float64(120)} {
			if saved[key] != want {
				t.Errorf("%s = %v, want %v", key, saved[key], want)
			}
		}
		if !reflect.DeepEqual(saved["answers"], []any{"дом1", "a,b"}) {
			t.Fatal(saved)
		}
	}
	saved = nil
	out := executeToolCallSafe(t.Context(), cfg, client, nil, "admin_create_bonus",
		`{"level_number":2,"level_id":22,"name":"Б 1","answers":["дом1"],"award_minutes":3}`)
	if strings.Contains(out, "error") {
		t.Fatal(out)
	}
	if method != http.MethodPost || saved["bonus_time"] != float64(180) {
		t.Fatal(method, saved)
	}
}
