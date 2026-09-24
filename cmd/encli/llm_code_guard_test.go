package main

import (
	"strings"
	"testing"
)

func TestCheckRequestedCodeSuffix(t *testing.T) {
	t.Parallel()
	userText := "дом1 кот2 год41 белка135"
	for _, tt := range []struct {
		name    string
		answers []string
		blocked bool
	}{
		{"Пиксель 1", []string{"дом"}, true},
		{"Безметка 41", []string{"год"}, true},
		{"Безметка 135", []string{"белка"}, true},
		{"Пиксель 1", []string{"дом1"}, false},
		{"Пиксель 2", []string{"кот2"}, false},
		{"Пиксель 3", []string{"лес"}, false},
		{"Пиксель 4", []string{"дом"}, false},
		{"Пиксель", []string{"дом"}, false},
	} {
		err := checkRequestedCodeSuffix(userText, tt.name, tt.answers)
		if (err != nil) != tt.blocked {
			t.Errorf("checkRequestedCodeSuffix(%q, %q) = %v, blocked = %v", tt.name, tt.answers, err, tt.blocked)
		}
	}
	if err := checkRequestedCodeSuffix("дом дом1", "Пиксель 1", []string{"дом"}); err != nil {
		t.Errorf("explicit unsuffixed source code was rejected: %v", err)
	}
}

func TestAgentRejectsCodeWithoutRequestedSuffixBeforeWrite(t *testing.T) {
	for _, tt := range []struct {
		tool, args, sourceCode string
	}{
		{"admin_create_sector", `{"game_id":81348,"level_number":2,"name":"Пиксель 1","answers":["дом"]}`, "дом1"},
		{"admin_create_bonus", `{"game_id":81348,"level_number":2,"level_id":1542162,"name":"Безметка 41","answers":["год"]}`, "год41"},
		{"admin_update_sector", `{"game_id":81348,"level_number":2,"sector_id":1,"name":"Пиксель 1","answers":["дом"]}`, "дом1"},
	} {
		result := executeToolCallSafe(t.Context(), &config{gameId: 81348}, nil,
			&llmSession{latestUserMessage: "дом1 кот2 год41"}, tt.tool, tt.args)
		if !strings.Contains(result, tt.sourceCode) || !strings.Contains(result, "drops the numeric suffix") {
			t.Errorf("%s mutation was not rejected with the source code: %s", tt.tool, result)
		}
	}
}
