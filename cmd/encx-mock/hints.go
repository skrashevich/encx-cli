package main

import (
	"time"

	"github.com/skrashevich/encx-cli/encx/scenario"
)

func scenarioHelps(st *sessionState, levelIdx int, lvl scenario.Level, now time.Time) []map[string]any {
	started := time.Time{}
	if levelIdx < len(st.LevelStartedAt) {
		started = st.LevelStartedAt[levelIdx]
	}
	if started.IsZero() {
		started = now
	}
	elapsed := int(now.Sub(started).Seconds())
	if elapsed < 0 {
		elapsed = 0
	}

	helps := make([]map[string]any, 0, len(lvl.Hints))
	for i, hint := range lvl.Hints {
		remain := hint.DelaySeconds - elapsed
		if remain < 0 {
			remain = 0
		}
		var helpText any
		if remain == 0 {
			helpText = hint.Text
		}
		helps = append(helps, map[string]any{
			"HelpId":           1000 + levelIdx*100 + i + 1,
			"Number":           i + 1,
			"HelpText":         helpText,
			"IsPenalty":        false,
			"Penalty":          0,
			"PenaltyComment":   nil,
			"RequestConfirm":   false,
			"PenaltyHelpState": 0,
			"RemainSeconds":    remain,
			"PenaltyMessage":   nil,
		})
	}
	return helps
}
