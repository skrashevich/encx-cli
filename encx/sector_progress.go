package encx

// SectorProgress is a normalized summary of a level's answer requirements.
// Remaining is nil when no sector or level-answer requirement is known.
type SectorProgress struct {
	Total     int  `json:"total"`
	Done      int  `json:"done"`
	Required  int  `json:"required"`
	Remaining *int `json:"remaining"`
}

// SectorProgress summarizes explicit sectors and implicit level-answer codes
// without modifying the model. Positive aggregate counters take precedence over
// sector flags; zero counters may be absent in older payloads and fall back to
// the explicit sectors. Total is a lower bound when the sector list is hidden:
// it includes at least the required answers and the reported done + remaining.
// Passed or dismissed levels with a known answer requirement have zero
// remaining answers.
func (l *Level) SectorProgress() SectorProgress {
	if l == nil {
		return SectorProgress{}
	}
	done := max(0, l.PassedSectorsCount)
	if done == 0 {
		for _, sector := range l.Sectors {
			if sector.IsAnswered {
				done++
			}
		}
	}
	total := max(len(l.Sectors), l.RequiredSectorsCount, done+max(0, l.SectorsLeftToClose))
	if total == 0 {
		return SectorProgress{}
	}
	required := l.RequiredSectorsCount
	if required <= 0 {
		required = total
	}
	remaining := max(0, required-done)
	if l.SectorsLeftToClose > 0 {
		remaining = l.SectorsLeftToClose
	}
	if l.IsPassed || l.Dismissed {
		remaining = 0
	}
	return SectorProgress{Total: total, Done: done, Required: required, Remaining: new(remaining)}
}
