package main

import "strings"

// Quirks reproduce behaviour the deployed backend actually has but that a
// correct engine would not. They are off by default, so the mock is a reference
// implementation of the protocol; turning one on lets a client's handling of the
// real server's misbehaviour be exercised offline.
//
// Available quirks:
//
//	reorder-noop  POST /admin/games/{id}/levels/exchange and .../put answer
//	              204 and leave the level order untouched, which is what
//	              api.en.cx does today. encx verifies the outcome and reports a
//	              failure; this is how that path is tested without the server.
var enabledQuirks = map[string]bool{}

func setQuirks(list string) {
	enabledQuirks = map[string]bool{}
	for _, name := range strings.Split(list, ",") {
		if name = strings.ToLower(strings.TrimSpace(name)); name != "" {
			enabledQuirks[name] = true
		}
	}
}

func mockQuirkEnabled(name string) bool { return enabledQuirks[name] }
