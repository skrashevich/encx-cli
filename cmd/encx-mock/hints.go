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

	remaining := func(delaySeconds int) int {
		if remain := delaySeconds - elapsed; remain > 0 {
			return remain
		}
		return 0
	}

	helps := make([]map[string]any, 0, len(lvl.Hints)+len(lvl.PenaltyHints))
	for i, hint := range lvl.Hints {
		remain := remaining(hint.DelaySeconds)
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
	for i, hint := range lvl.PenaltyHints {
		remain := remaining(hint.DelaySeconds)
		var helpText any
		if remain == 0 {
			helpText = hint.Text
		}
		var comment any
		if hint.Comment != "" {
			comment = hint.Comment
		}
		helps = append(helps, map[string]any{
			"HelpId":           2000 + levelIdx*100 + i + 1,
			"Number":           i + 1,
			"HelpText":         helpText,
			"IsPenalty":        true,
			"Penalty":          hint.PenaltySeconds,
			"PenaltyComment":   comment,
			"RequestConfirm":   hint.RequestConfirm,
			"PenaltyHelpState": 0,
			"RemainSeconds":    remain,
			"PenaltyMessage":   nil,
		})
	}
	return helps
}
