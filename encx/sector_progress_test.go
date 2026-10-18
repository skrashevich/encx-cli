package encx

import (
	"reflect"
	"testing"
)

func TestSectorProgressNormalizesEngineCountersAndImplicitLevelCode(t *testing.T) {
	for _, tt := range []struct {
		name  string
		level *Level
		want  SectorProgress
	}{
		{"unknown level", nil, SectorProgress{}},
		{"bonus only", &Level{Bonuses: []Bonus{{BonusId: 1}}}, SectorProgress{}},
		{"implicit level code", &Level{RequiredSectorsCount: 1, SectorsLeftToClose: 1}, SectorProgress{Total: 1, Required: 1, Remaining: new(1)}},
		{"implicit code answered", &Level{RequiredSectorsCount: 1, PassedSectorsCount: 1}, SectorProgress{Total: 1, Done: 1, Required: 1, Remaining: new(0)}},
		{"implicit code passed", &Level{RequiredSectorsCount: 1, IsPassed: true}, SectorProgress{Total: 1, Required: 1, Remaining: new(0)}},
		{"partial threshold", &Level{RequiredSectorsCount: 2, Sectors: []Sector{{IsAnswered: true}, {}, {}}}, SectorProgress{Total: 3, Done: 1, Required: 2, Remaining: new(1)}},
		{"missing counters", &Level{Sectors: []Sector{{IsAnswered: true}, {}}}, SectorProgress{Total: 2, Done: 1, Required: 2, Remaining: new(1)}},
		{"aggregate counts", &Level{RequiredSectorsCount: 3, PassedSectorsCount: 1, SectorsLeftToClose: 2}, SectorProgress{Total: 3, Done: 1, Required: 3, Remaining: new(2)}},
		{"engine remainder", &Level{RequiredSectorsCount: 3, PassedSectorsCount: 1, SectorsLeftToClose: 1, Sectors: []Sector{{}, {}, {}}}, SectorProgress{Total: 3, Done: 1, Required: 3, Remaining: new(1)}},
		{"threshold exceeded", &Level{RequiredSectorsCount: 1, PassedSectorsCount: 2, Sectors: []Sector{{IsAnswered: true}, {IsAnswered: true}, {}}}, SectorProgress{Total: 3, Done: 2, Required: 1, Remaining: new(0)}},
		{"dismissed", &Level{RequiredSectorsCount: 1, SectorsLeftToClose: 1, Dismissed: true}, SectorProgress{Total: 1, Required: 1, Remaining: new(0)}},
		{"negative counters", &Level{RequiredSectorsCount: -1, PassedSectorsCount: -1, SectorsLeftToClose: -1, Sectors: []Sector{{}}}, SectorProgress{Total: 1, Required: 1, Remaining: new(1)}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.level.SectorProgress(); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("SectorProgress() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
