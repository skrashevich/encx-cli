package encx_test

import (
	"encoding/json"
	"fmt"
	"reflect"
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
		{name: "bool", raw: `true`, want: "true"},
		{name: "number", raw: `42.5`, want: "42.5"},
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

func TestFlexStringRemainsStringCompatible(t *testing.T) {
	t.Parallel()

	var sector encx.Sector
	if err := json.Unmarshal([]byte(`{"Answer":"synthetic"}`), &sector); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if string(sector.Answer) != "synthetic" {
		t.Fatalf("string(Answer) = %q, want synthetic", sector.Answer)
	}
	if sector.Answer != encx.FlexString("synthetic") {
		t.Fatalf("Answer = %q, want synthetic", sector.Answer)
	}
	if got := fmt.Sprintf("%s", sector.Answer); got != "synthetic" {
		t.Fatalf("%%s Answer = %q, want synthetic", got)
	}
}

func TestSectorAndBonusAnswerRoundTripAndRawAccessors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		newTarget func() rawAnswerTarget
	}{
		{
			name:  "sector object",
			input: `{"SectorId":1,"Answer":{"Answer":"synthetic","Login":"redacted"}}`,
			newTarget: func() rawAnswerTarget {
				return &sectorAnswerTarget{}
			},
		},
		{
			name:  "sector string",
			input: `{"SectorId":1,"Answer":"synthetic"}`,
			newTarget: func() rawAnswerTarget {
				return &sectorAnswerTarget{}
			},
		},
		{
			name:  "sector null",
			input: `{"SectorId":1,"Answer":null}`,
			newTarget: func() rawAnswerTarget {
				return &sectorAnswerTarget{}
			},
		},
		{
			name:  "bonus object",
			input: `{"BonusId":2,"Answer":{"Answer":"synthetic","Login":"redacted"}}`,
			newTarget: func() rawAnswerTarget {
				return &bonusAnswerTarget{}
			},
		},
		{
			name:  "bonus string",
			input: `{"BonusId":2,"Answer":"synthetic"}`,
			newTarget: func() rawAnswerTarget {
				return &bonusAnswerTarget{}
			},
		},
		{
			name:  "bonus null",
			input: `{"BonusId":2,"Answer":null}`,
			newTarget: func() rawAnswerTarget {
				return &bonusAnswerTarget{}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := tt.newTarget()
			if err := json.Unmarshal([]byte(tt.input), target); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			want := answerField([]byte(tt.input))
			raw := target.rawAnswerJSON()
			if !jsonEqual(raw, want) {
				t.Fatalf("RawAnswerJSON() = %s, want %s", raw, want)
			}
			if got := target.answerJSON(); !jsonEqual(got, want) {
				t.Fatalf("marshaled Answer = %s, want %s", got, want)
			}
			raw[0] = '!'
			if !jsonEqual(target.rawAnswerJSON(), want) {
				t.Fatal("RawAnswerJSON() returned mutable sidecar")
			}
		})
	}
}

func TestSectorMarshalUsesMutatedAnswer(t *testing.T) {
	t.Parallel()

	var sector encx.Sector
	if err := json.Unmarshal([]byte(`{"SectorId":1,"Answer":{"Answer":"old","Login":"redacted"}}`), &sector); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	sector.Answer = "new"

	if got := rawAnswerField(sector); string(got) != `"new"` {
		t.Fatalf("marshaled Answer = %s, want %q", got, "new")
	}
}

func TestBonusMarshalUsesMutatedAnswer(t *testing.T) {
	t.Parallel()

	var bonus encx.Bonus
	if err := json.Unmarshal([]byte(`{"BonusId":2,"Answer":{"Answer":"old","Login":"redacted"}}`), &bonus); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	bonus.Answer = "new"

	if got := rawAnswerField(bonus); string(got) != `"new"` {
		t.Fatalf("marshaled Answer = %s, want %q", got, "new")
	}
}

func TestRawSidecarPublicStructsRemainComparable(t *testing.T) {
	t.Parallel()

	if (encx.Sector{}) != (encx.Sector{}) {
		t.Fatal("zero Sector values must compare equal")
	}
	if (encx.Bonus{}) != (encx.Bonus{}) {
		t.Fatal("zero Bonus values must compare equal")
	}
	if (encx.CodeAction{}) != (encx.CodeAction{}) {
		t.Fatal("zero CodeAction values must compare equal")
	}
	if (encx.GameInfo{}) != (encx.GameInfo{}) {
		t.Fatal("zero GameInfo values must compare equal")
	}
}

type rawAnswerTarget interface {
	json.Unmarshaler
	json.Marshaler
	rawAnswerJSON() json.RawMessage
	answerJSON() json.RawMessage
}

type sectorAnswerTarget struct{ encx.Sector }

func (s *sectorAnswerTarget) rawAnswerJSON() json.RawMessage { return s.RawAnswerJSON() }
func (s *sectorAnswerTarget) answerJSON() json.RawMessage {
	return rawAnswerField(s.Sector)
}

type bonusAnswerTarget struct{ encx.Bonus }

func (b *bonusAnswerTarget) rawAnswerJSON() json.RawMessage { return b.RawAnswerJSON() }
func (b *bonusAnswerTarget) answerJSON() json.RawMessage {
	return rawAnswerField(b.Bonus)
}

func rawAnswerField(value any) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return answerField(encoded)
}

func answerField(encoded []byte) json.RawMessage {
	var object struct {
		Answer json.RawMessage `json:"Answer"`
	}
	if err := json.Unmarshal(encoded, &object); err != nil {
		panic(err)
	}
	return object.Answer
}

func jsonEqual(left, right []byte) bool {
	var a, b any
	return json.Unmarshal(left, &a) == nil && json.Unmarshal(right, &b) == nil && reflect.DeepEqual(a, b)
}
