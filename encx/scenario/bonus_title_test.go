package scenario

import "testing"

// parseBonusTitle splits the bonus number from the quoted bonus name. The name
// is delimited by the LAST quote, not the first, because a name may itself
// contain quotes. These cases pin the boundaries of that split: a bare number,
// a name that carries its own quotes, and the two degenerate inputs where the
// opening quote is also the last one and there is no name to report.
func TestParseBonusTitle(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		wantNum  int
		wantName string
		wantOK   bool
	}{
		{"no bonus marker", `Уровень №3 "Тест"`, 0, "", false},
		{"number only", `Бонус №7`, 7, "", true},
		{"quoted name", `Бонус №7 "Ключ"`, 7, "Ключ", true},
		{"name containing quotes", `Бонус №7 "Он сказал "да""`, 7, `Он сказал "да"`, true},
		{"unquoted trailing text", `Бонус №7 просто текст`, 7, "", true},

		// The opening quote is the last quote, so there is no closing quote and
		// no name between them. Both must report the number and an empty name
		// rather than slicing backwards.
		{"lone quote", `Бонус №7 "`, 7, "", true},
		{"unterminated name", `Бонус №7 "Ключ`, 7, "", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			num, name, ok := parseBonusTitle(tc.in)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if num != tc.wantNum {
				t.Errorf("num = %d, want %d", num, tc.wantNum)
			}
			if name != tc.wantName {
				t.Errorf("name = %q, want %q", name, tc.wantName)
			}
		})
	}
}
