package main

import (
	"fmt"
	"strings"
	"unicode"
)

// checkRequestedCodeSuffix catches an agent dropping the numeric suffix from a
// code when the destination item's name and the user's source token share it.
// It rejects the write so the agent can submit the exact source code instead.
func checkRequestedCodeSuffix(userText, itemName string, answers []string) error {
	if userText == "" || itemName == "" || len(answers) == 0 {
		return nil
	}
	nameParts := strings.Fields(itemName)
	if len(nameParts) == 0 {
		return nil
	}
	number := nameParts[len(nameParts)-1]
	if number == "" || strings.Trim(number, "0123456789") != "" {
		return nil
	}
	tokens := strings.FieldsFunc(userText, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	sourceCodes := make(map[string]struct{}, len(tokens))
	for _, token := range tokens {
		sourceCodes[strings.ToLower(token)] = struct{}{}
	}
	for _, answer := range answers {
		if answer == "" {
			continue
		}
		if _, exact := sourceCodes[strings.ToLower(answer)]; exact {
			continue
		}
		want := answer + number
		if _, truncated := sourceCodes[strings.ToLower(want)]; truncated {
			return fmt.Errorf("answer %q for %q drops the numeric suffix from user code %q; submit the exact code", answer, itemName, want)
		}
	}
	return nil
}
