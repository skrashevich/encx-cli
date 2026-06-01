package encx_test

import (
	"encoding/json"
	"testing"

	"github.com/skrashevich/encx-cli/encx"
)

func TestSectorAnswerUnmarshal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "string", raw: `"КОТ"`, want: "КОТ"},
		{name: "empty", raw: `""`, want: ""},
		{name: "null", raw: `null`, want: ""},
		{name: "object answ", raw: `{"Answ":"КОТ","Login":"player"}`, want: "КОТ"},
		{name: "object answer", raw: `{"Answer":"ПЁС"}`, want: "ПЁС"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var sector encx.Sector
			payload := `{"SectorId":1,"Order":1,"Name":"S1","IsAnswered":true,"Answer":` + tt.raw + `}`
			if err := json.Unmarshal([]byte(payload), &sector); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if string(sector.Answer) != tt.want {
				t.Fatalf("Answer = %q, want %q", sector.Answer, tt.want)
			}
		})
	}
}

func TestGameModelSectorAnswerObject(t *testing.T) {
	t.Parallel()

	raw := `{
		"GameId": 1,
		"Level": {
			"LevelId": 3,
			"Number": 3,
			"Name": "Level 3",
			"Sectors": [
				{
					"SectorId": 1,
					"Order": 1,
					"Name": "Первый код",
					"IsAnswered": true,
					"Answer": {"Answ": "КОТ", "Login": "skrashevich"}
				}
			]
		}
	}`

	var model encx.GameModel
	if err := json.Unmarshal([]byte(raw), &model); err != nil {
		t.Fatalf("Unmarshal GameModel: %v", err)
	}
	if model.Level == nil || len(model.Level.Sectors) != 1 {
		t.Fatal("expected one sector on level")
	}
	if got := string(model.Level.Sectors[0].Answer); got != "КОТ" {
		t.Fatalf("sector answer = %q, want КОТ", got)
	}

	out, err := json.Marshal(model)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !json.Valid(out) {
		t.Fatal("marshaled JSON invalid")
	}
}
