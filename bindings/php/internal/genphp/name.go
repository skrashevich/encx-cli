package genphp

import "unicode"

// MethodName converts a Go identifier into a PHP method name in lowerCamelCase.
//
// The leading run of capitals is lowered as a single word, the way Go itself
// reads an acronym: ExportHAR becomes exportHAR and Domain becomes domain. When
// the run is longer than one letter and a lowercase letter follows it, the last
// capital of the run starts the next word and stays capital, which is what
// turns APIBaseURL into apiBaseURL and HAREntryCount into harEntryCount.
func MethodName(goName string) string {
	runes := []rune(goName)

	run := 0
	for run < len(runes) && unicode.IsUpper(runes[run]) {
		run++
	}
	if run == 0 {
		return goName
	}

	lower := run
	if run > 1 && run < len(runes) && unicode.IsLower(runes[run]) {
		lower = run - 1
	}
	for i := range lower {
		runes[i] = unicode.ToLower(runes[i])
	}
	return string(runes)
}
