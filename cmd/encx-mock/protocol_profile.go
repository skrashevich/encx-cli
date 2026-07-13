package main

import "time"

type protocolProfile struct {
	Records             []protocolRecord
	Levels              []levelTopology
	TransitionEvents    []int
	HasAntiBotRedirect  bool
	HasTransportFailure bool
}

func (p *protocolProfile) LevelCount() int {
	if p == nil {
		return 0
	}
	return len(p.Levels)
}

type protocolRecord struct {
	Kind      string
	Variant   string
	Method    string
	Status    int
	Event     int
	StartedAt time.Time
	Count     int
}

type levelTopology struct {
	Number              int
	SectorCount         int
	RequiredSectorCount int
	BonusCount          int
	HintCount           int
	MessageCount        int
	Dismissed           bool
	Passed              bool
}
