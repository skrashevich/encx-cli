package main

import "testing"

// A game time is typed the way the editor prints it, minus the seconds nobody
// writes: "start=07.06.2027 18:30". That value parsed nowhere, so the date the
// engine accepts ratings from was never moved with the start, and the whole
// update was refused over a field the caller had not mentioned:
// "Дата начала приема оценок за игру не должна быть раньше даты начала игры".
func TestParseGameDateTimeAcceptsMinutePrecision(t *testing.T) {
	t.Parallel()
	for _, value := range []string{
		"07.06.2027 18:30",
		"07.06.2027 18:30:00",
		"2027-06-07 18:30",
		"2027-06-07T18:30",
		"2027-06-07T18:30:00",
		"2027-06-07T18:30:00+03:00",
	} {
		parsed, ok := parseGameDateTime(value)
		if !ok {
			t.Errorf("%q is a game time and must parse", value)
			continue
		}
		if parsed.Year() != 2027 || parsed.Month() != 6 || parsed.Day() != 7 ||
			parsed.Hour() != 18 || parsed.Minute() != 30 {
			t.Errorf("%q parsed as %s", value, parsed)
		}
	}
	if _, ok := parseGameDateTime("седьмого июня"); ok {
		t.Error("an unparseable value must say so rather than guess")
	}
}

// The auto-advance that this feeds only fires for a start it could read, so the
// two minute-precision spellings have to agree about the same moment.
func TestParseGameDateTimeComparesAcrossSpellings(t *testing.T) {
	t.Parallel()
	typed, ok := parseGameDateTime("07.06.2027 18:30")
	if !ok {
		t.Fatal("the typed spelling must parse")
	}
	stored, ok := parseGameDateTime("07.06.2027 18:30:00")
	if !ok {
		t.Fatal("the editor spelling must parse")
	}
	if !typed.Equal(stored) {
		t.Fatalf("%s and %s are the same moment", typed, stored)
	}
}
