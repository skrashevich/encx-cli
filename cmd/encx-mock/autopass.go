package main

import (
	"time"

	"github.com/skrashevich/encx-cli/encx/scenario"
)

func (s *server) ensureLevelStarted(st *sessionState, levelIdx int, now time.Time) {
	if levelIdx < 0 || levelIdx >= len(st.LevelStartedAt) {
		return
	}
	if st.LevelStartedAt[levelIdx].IsZero() {
		st.LevelStartedAt[levelIdx] = now
	}
}

func (s *server) applyScenarioAutopass(st *sessionState, now time.Time) {
	if s.scenario == nil || st.Completed {
		return
	}
	s.ensureLevelStarted(st, st.CurrentIdx, now)
	for st.CurrentIdx < len(s.scenario.Levels) && !st.Completed {
		idx := st.CurrentIdx
		if idx >= len(st.Passed) || st.Passed[idx] {
			return
		}
		lvl := s.scenario.Levels[idx]
		if lvl.AutopassSecond <= 0 {
			return
		}
		started := st.LevelStartedAt[idx]
		if started.IsZero() {
			return
		}
		if int(now.Sub(started).Seconds()) < lvl.AutopassSecond {
			return
		}
		s.autopassLevel(st, idx, now)
	}
}

func (s *server) autopassLevel(st *sessionState, levelIdx int, now time.Time) {
	if levelIdx < len(st.SectorPassed) {
		for i := range st.SectorPassed[levelIdx] {
			st.SectorPassed[levelIdx][i] = true
		}
	}
	if levelIdx < len(st.Passed) {
		st.Passed[levelIdx] = true
	}
	if levelIdx < s.levelCount()-1 {
		st.CurrentIdx = levelIdx + 1
		s.ensureLevelStarted(st, st.CurrentIdx, now)
		st.LastAction = nil
	} else {
		st.Completed = true
	}
	st.UpdatedAt = now
}

func scenarioLevelTimeout(lvl scenario.Level, started, now time.Time) (timeout, remain, award int) {
	if lvl.AutopassSecond <= 0 || started.IsZero() {
		return 0, 0, 0
	}
	timeout = lvl.AutopassSecond
	elapsed := int(now.Sub(started).Seconds())
	remain = timeout - elapsed
	if remain < 0 {
		remain = 0
	}
	return timeout, remain, 0
}
