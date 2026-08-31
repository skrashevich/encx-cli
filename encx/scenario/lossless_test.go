package scenario

import (
	"fmt"
	"html"
	"regexp"
	"strings"
)

var (
	htmlCommentRe   = regexp.MustCompile(`(?s)<!--.*?-->`)
	scriptOrStyleRe = regexp.MustCompile(`(?is)<(script|style)\b[^>]*>.*?</(script|style)>`)
	blockTagRe      = regexp.MustCompile(`(?is)</?(br|div|p|tr|table|tbody|li|ul|ol|details|summary|h\d)\b[^>]*>`)
	anyTagRe        = regexp.MustCompile(`(?s)<[^>]*>`)
	answerForRefRe  = regexp.MustCompile(`(?is)\s*-\s*<span[^>]*lblAnswerFor[^>]*>.*?</span>`)

	// Labels the export renders around the data; they carry no scenario content
	// of their own and are therefore absent from the parsed model.
	exportChromeRe = regexp.MustCompile(`(?is)^(` + strings.Join([]string{
		`Уровень №\d+.*$`, `Комментарий к уровню .*$`, `Задание для .*$`,
		`Подсказка №\d+ .*$`, `Штрафная подсказка №\d+ .*$`,
		`Штрафное время:.*$`, `Бонусное время:.*$`,
		`запрашивать дополнительное подтверждение:.*$`, `Описание:.*$`,
		`Автопереход:.*$`, `Бонус №\d+.*$`, `для всех$`, `Да$`, `Нет$`,
		`Задание$`, `Подсказка$`, `Ответы:?$`, `Сектор №?\d*$`,
		`Уровень (?:снят|пройден)\S*$`, `\(для прохождения[^)]*\)$`, `[-—]$`,
	}, "|") + `)$`)
)

// visibleLines reduces an HTML fragment to the lines a reader would see.
func visibleLines(fragment string) []string {
	fragment = htmlCommentRe.ReplaceAllString(fragment, "")
	fragment = scriptOrStyleRe.ReplaceAllString(fragment, "")
	fragment = blockTagRe.ReplaceAllString(fragment, "\n")
	fragment = anyTagRe.ReplaceAllString(fragment, "")
	fragment = html.UnescapeString(fragment)
	var out []string
	for _, line := range strings.Split(fragment, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

// missingScenarioText reports visible lines of each level block that did not
// survive into doc. It is the regression net for the non-greedy extraction bug,
// which silently dropped everything after the first nested closing tag.
func missingScenarioText(raw string, doc *Document) []string {
	anchors := levelAnchorRe.FindAllStringIndex(raw, -1)
	var missing []string
	for i, anchor := range anchors {
		if i >= len(doc.Levels) {
			break
		}
		end := len(raw)
		if i+1 < len(anchors) {
			end = anchors[i+1][0]
		}
		block := answerForRefRe.ReplaceAllString(raw[anchor[0]:end], "")

		known := map[string]struct{}{}
		for _, line := range levelVisibleLines(doc.Levels[i]) {
			known[line] = struct{}{}
		}
		for _, line := range visibleLines(block) {
			if _, ok := known[line]; ok || exportChromeRe.MatchString(line) {
				continue
			}
			missing = append(missing, fmt.Sprintf("level %d: %s", doc.Levels[i].Number, line))
		}
	}
	return missing
}

func levelVisibleLines(lvl Level) []string {
	fields := []string{lvl.Name, lvl.Comment}
	fields = append(fields, lvl.Tasks...)
	for _, h := range lvl.Hints {
		fields = append(fields, h.Title, h.Text)
	}
	for _, h := range lvl.PenaltyHints {
		fields = append(fields, h.Title, h.Text, h.Comment)
	}
	for _, s := range lvl.Sectors {
		fields = append(fields, s.Name)
		fields = append(fields, s.Answers...)
	}
	for _, b := range lvl.Bonuses {
		fields = append(fields, b.Name, b.Task, b.Hint)
		fields = append(fields, b.Answers...)
	}
	var out []string
	for _, field := range fields {
		out = append(out, visibleLines(field)...)
	}
	return out
}
