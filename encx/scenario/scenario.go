package scenario

import (
	"encoding/base64"
	"fmt"
	"html"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Hint is a parsed level hint from a GameScenario export.
type Hint struct {
	Title        string `json:"title,omitempty"`
	Text         string `json:"text"`
	DelaySeconds int    `json:"delay_seconds"`
}

// PenaltyHint is a parsed penalty hint (штрафная подсказка) from a level block.
type PenaltyHint struct {
	Title          string `json:"title,omitempty"`
	Text           string `json:"text"`
	DelaySeconds   int    `json:"delay_seconds"`
	PenaltySeconds int    `json:"penalty_seconds,omitempty"`
	RequestConfirm bool   `json:"request_confirm,omitempty"`
	Comment        string `json:"comment,omitempty"`
}

// Bonus is a parsed bonus block from a GameScenario export.
type Bonus struct {
	Number       int      `json:"number"`
	Name         string   `json:"name,omitempty"`
	AwardSeconds int      `json:"award_seconds,omitempty"`
	Negative     bool     `json:"negative,omitempty"`
	Task         string   `json:"task,omitempty"`
	Hint         string   `json:"hint,omitempty"`
	Answers      []string `json:"answers,omitempty"`
}

// Sector is a parsed answer sector from a GameScenario export.
type Sector struct {
	Name    string   `json:"name,omitempty"`
	Answers []string `json:"answers,omitempty"`
}

// Level is one level block from a GameScenario export.
type Level struct {
	Number                int           `json:"number"`
	Name                  string        `json:"name"`
	AutopassSecond        int           `json:"autopass_seconds,omitempty"`
	AutopassPenaltySecond int           `json:"autopass_penalty_seconds,omitempty"`
	RequiredSectorsCount  int           `json:"required_sectors_count,omitempty"`
	Comment               string        `json:"comment,omitempty"`
	Tasks                 []string      `json:"tasks,omitempty"`
	Hints                 []Hint        `json:"hints,omitempty"`
	PenaltyHints          []PenaltyHint `json:"penalty_hints,omitempty"`
	Sectors               []Sector      `json:"sectors,omitempty"`
	SectorAnswers         [][]string    `json:"sector_answers,omitempty"`
	Bonuses               []Bonus       `json:"bonuses,omitempty"`
}

// Document is a parsed GameScenario.aspx HTML export.
type Document struct {
	SourcePath     string   `json:"source_path"`
	GameID         int      `json:"game_id,omitempty"`
	GameNum        int      `json:"game_num,omitempty"`
	GameTitle      string   `json:"game_title,omitempty"`
	Levels         []Level  `json:"levels"`
	EmbeddedAssets int      `json:"embedded_assets"`
	MissingAssets  []string `json:"missing_assets,omitempty"`
}

type assetRewriteState struct {
	baseDir       string
	cache         map[string]string
	embeddedCount int
	missingSet    map[string]struct{}
}

var (
	levelAnchorRe      = regexp.MustCompile(`(?is)<a id="LevelsScenarioRepeater_ctl\d+_lnkLevelAnchorPoint" name="\d+"></a>`)
	levelTitleRe       = regexp.MustCompile(`(?is)Уровень №\s*(\d+)\s*(?:"([^"]*)")?`)
	autopassRe         = regexp.MustCompile(`(?is)Автопереход:\s*через\s*([^<]+)`)
	taskOpenRe         = regexp.MustCompile(`(?is)<span[^>]*id="LevelsScenarioRepeater_ctl\d+_LevelTasksRepeater_ctl\d+_lblLevelTask"[^>]*>`)
	hintTitleOpenRe    = regexp.MustCompile(`(?is)<span[^>]*id="LevelsScenarioRepeater_ctl(\d+)_LevelHelpsRepeater_ctl(\d+)_lblLevelHelpTitle"[^>]*>`)
	hintTextOpenRe     = regexp.MustCompile(`(?is)<span[^>]*id="LevelsScenarioRepeater_ctl(\d+)_LevelHelpsRepeater_ctl(\d+)_lblLevelHelp"[^>]*>`)
	penaltyTitleOpenRe = regexp.MustCompile(`(?is)<span[^>]*id="LevelsScenarioRepeater_ctl(\d+)_LevelPenaltyHelpsRepeater_ctl(\d+)_lblLevelHelpTitle"[^>]*>`)
	penaltyTextOpenRe  = regexp.MustCompile(`(?is)<span[^>]*id="LevelsScenarioRepeater_ctl(\d+)_LevelPenaltyHelpsRepeater_ctl(\d+)_lblLevelHelp"[^>]*>`)
	penaltyTimeRe      = regexp.MustCompile(`(?is)Штрафное\s+время:\s*</span>\s*<span[^>]*>([^<]*)</span>`)
	penaltyConfirmRe   = regexp.MustCompile(`(?is)запрашивать\s+дополнительное\s+подтверждение:\s*</span>\s*<span[^>]*>([^<]*)</span>`)
	penaltyCommentRe   = regexp.MustCompile(`(?is)Описание:\s*<span[^>]*>`)
	requiredSectorsRe  = regexp.MustCompile(`(?is)для\s+прохождения\s+задания\s+необходимо\s+выполнить\s+(все|\d+)\s+сектор`)
	sectorNameOpenRe   = regexp.MustCompile(`(?is)<div[^>]*id="LevelsScenarioRepeater_ctl\d+_SectorsRepeater_ctl(\d+)_divSectorName"[^>]*>`)
	answerOpenRe       = regexp.MustCompile(`(?is)<span[^>]*id="LevelsScenarioRepeater_ctl\d+_SectorsRepeater_ctl(\d+)_LevelAnswersRepeater_ctl\d+_lblLevelAnswer"[^>]*>`)
	answerForRe        = regexp.MustCompile(`(?is)^\s*-\s*<span[^>]*id="LevelsScenarioRepeater_ctl\d+_SectorsRepeater_ctl\d+_LevelAnswersRepeater_ctl\d+_lblAnswerFor"`)
	commentOpenRe      = regexp.MustCompile(`(?is)<span[^>]*id="LevelsScenarioRepeater_ctl\d+_lblLevelComment"[^>]*>`)
	bonusHeaderOpenRe  = regexp.MustCompile(`(?is)<span[^>]*id="LevelsScenarioRepeater_ctl\d+_LevelBonusesRepeater_ctl\d+_lblBonusNum"[^>]*>`)
	bonusAwardRe       = regexp.MustCompile(`(?is)(Бонусное|Штрафное)\s+время:\s*([^<]+)`)
	bonusAnswerOpenRe  = regexp.MustCompile(`(?is)<span[^>]*id="LevelsScenarioRepeater_ctl\d+_LevelBonusesRepeater_ctl\d+_BonusAnswersRepeater_ctl\d+_lblBonusAnswer"[^>]*>`)
	bonusTitleNumberRe = regexp.MustCompile(`(?is)Бонус\s*№\s*(\d+)`)
	hintDelayRe        = regexp.MustCompile(`\(([^()]*)\)\s*$`)
	durationRe         = regexp.MustCompile(`(?i)(\d+)\s*(день|дня|дней|час(?:а|ов)?|минут(?:а|ы)?|секунд(?:а|ы)?)`)
	assetAttrRe        = regexp.MustCompile(`(?i)\b(src|href)\s*=\s*"([^"]+)"`)
	gameTitleRe        = regexp.MustCompile(`(?is)id="lnkGameInfo"[^>]*>([^<]+)</a>`)
	gameIDRe           = regexp.MustCompile(`(?is)id="lblGameId"[^>]*>(\d+)`)
	gameNumRe          = regexp.MustCompile(`(?is)id="lblGameNumber"[^>]*>(\d+)`)
)

// ParseFile reads and parses a GameScenario HTML export.
func ParseFile(path string) (*Document, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}

	state := &assetRewriteState{
		baseDir:    filepath.Dir(absPath),
		cache:      make(map[string]string),
		missingSet: make(map[string]struct{}),
	}

	return parseDocument(string(raw), absPath, state)
}

// ParseString parses a GameScenario HTML export already loaded in memory.
func ParseString(raw, sourcePath string) (*Document, error) {
	state := &assetRewriteState{
		baseDir:    "",
		cache:      make(map[string]string),
		missingSet: make(map[string]struct{}),
	}
	return parseDocument(raw, sourcePath, state)
}

func parseDocument(raw, sourcePath string, state *assetRewriteState) (*Document, error) {
	levels, err := parseLevels(raw, state)
	if err != nil {
		return nil, err
	}
	missing := make([]string, 0, len(state.missingSet))
	for p := range state.missingSet {
		missing = append(missing, p)
	}
	sort.Strings(missing)

	doc := &Document{
		SourcePath:     sourcePath,
		Levels:         levels,
		EmbeddedAssets: state.embeddedCount,
		MissingAssets:  missing,
	}
	if m := gameTitleRe.FindStringSubmatch(raw); len(m) >= 2 {
		doc.GameTitle = strings.TrimSpace(html.UnescapeString(m[1]))
	}
	if m := gameIDRe.FindStringSubmatch(raw); len(m) >= 2 {
		doc.GameID, _ = strconv.Atoi(m[1])
	}
	if m := gameNumRe.FindStringSubmatch(raw); len(m) >= 2 {
		doc.GameNum, _ = strconv.Atoi(m[1])
	}
	return doc, nil
}

// ParseRuDuration parses Russian duration phrases like "1 час 5 минут".
func ParseRuDuration(s string) int {
	total := 0
	for _, match := range durationRe.FindAllStringSubmatch(strings.ToLower(s), -1) {
		if len(match) < 3 {
			continue
		}
		value, err := strconv.Atoi(match[1])
		if err != nil {
			continue
		}
		unit := match[2]
		switch {
		case unit == "день" || unit == "дня" || unit == "дней":
			total += value * 24 * 3600
		case strings.HasPrefix(unit, "час"):
			total += value * 3600
		case strings.HasPrefix(unit, "минут"):
			total += value * 60
		case strings.HasPrefix(unit, "секунд"):
			total += value
		}
	}
	return total
}

// NormalizeComparableText normalizes text for answer/content comparison.
func NormalizeComparableText(s string) string {
	s = html.UnescapeString(s)
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.Join(strings.Fields(s), " ")
}

// MatchAnswer reports whether submitted matches any accepted sector answer.
// Comparison is case-insensitive and ignores insignificant whitespace differences.
func MatchAnswer(submitted string, accepted []string) bool {
	submittedNorm := NormalizeComparableText(submitted)
	if submittedNorm == "" {
		return false
	}
	for _, candidate := range accepted {
		candidateNorm := NormalizeComparableText(candidate)
		if candidateNorm != "" && strings.EqualFold(candidateNorm, submittedNorm) {
			return true
		}
	}
	return false
}

// SectorCount returns the number of sectors on a level (at least 1 when answers exist).
func (l Level) SectorCount() int {
	if len(l.SectorAnswers) > 0 {
		return len(l.SectorAnswers)
	}
	return 1
}

// element is one HTML element located by its opening tag: the submatches of the
// opening-tag pattern plus the element's inner HTML.
type element struct {
	groups []string
	inner  string
	start  int
	end    int
}

// indexKey identifies a repeater field by its outer and inner ctl indices.
func (e element) indexKey() string {
	if len(e.groups) < 3 {
		return ""
	}
	return e.groups[1] + "|" + e.groups[2]
}

// findElements locates every element in block whose opening tag matches openRe
// and extracts its inner HTML honouring nesting of the same tag name. The
// non-greedy `(.*?)</tag>` shape used previously truncated content at the first
// nested closing tag, which silently dropped most of a task or hint body.
func findElements(block string, openRe *regexp.Regexp, tag string) []element {
	locs := openRe.FindAllStringSubmatchIndex(block, -1)
	if len(locs) == 0 {
		return nil
	}
	out := make([]element, 0, len(locs))
	for _, loc := range locs {
		inner, end, ok := extractBalanced(block, loc[1], tag)
		if !ok {
			continue
		}
		groups := make([]string, len(loc)/2)
		for i := range groups {
			if loc[2*i] >= 0 {
				groups[i] = block[loc[2*i]:loc[2*i+1]]
			}
		}
		out = append(out, element{groups: groups, inner: inner, start: loc[0], end: end})
	}
	return out
}

// extractBalanced returns the inner HTML of an element whose opening tag ends at
// start and the offset just past the element. Nested elements with the same tag
// name are counted.
//
// Level content is authored by game masters and is not guaranteed to be well
// formed, so two fallback boundaries are remembered while scanning: the start of
// the next repeater field and the end of the enclosing block. They are only
// candidates — the element's own closing tag always wins, so a nested repeater
// id or a stray </div> inside well-formed content is harmless. When the element
// turns out to be unclosed, the earliest candidate bounds it, which keeps its
// content without letting it swallow a neighbouring scenario field.
func extractBalanced(s string, start int, tag string) (string, int, bool) {
	openPrefix := "<" + tag
	closePrefix := "</" + tag
	trackBlockEnd := tag != "div"
	nextField, blockEnd := -1, -1
	guardDepth := 0
	depth := 1
	pos := start
	for pos < len(s) {
		rel := strings.IndexByte(s[pos:], '<')
		if rel < 0 {
			break
		}
		idx := pos + rel
		rest := s[idx:]
		gt := strings.IndexByte(rest, '>')
		if gt < 0 {
			break
		}
		if nextField < 0 && strings.Contains(rest[:gt], repeaterIDMarker) {
			nextField = idx
		}
		switch {
		case hasTagPrefix(rest, closePrefix):
			depth--
			if depth == 0 {
				return s[start:idx], idx + gt + 1, true
			}
		case hasTagPrefix(rest, openPrefix):
			if !strings.HasSuffix(strings.TrimSpace(rest[:gt]), "/") {
				depth++
			}
		case trackBlockEnd && hasTagPrefix(rest, "<div"):
			if !strings.HasSuffix(strings.TrimSpace(rest[:gt]), "/") {
				guardDepth++
			}
		case trackBlockEnd && hasTagPrefix(rest, "</div"):
			if guardDepth == 0 {
				if blockEnd < 0 {
					blockEnd = idx
				}
			} else {
				guardDepth--
			}
		default:
			pos = idx + 1
			continue
		}
		pos = idx + gt + 1
	}
	if end := earliestBoundary(nextField, blockEnd); end >= 0 {
		return s[start:end], end, true
	}
	// No boundary at all: keep the remainder rather than dropping the field
	// silently, which is how the truncation bug stayed invisible.
	return s[start:], len(s), true
}

// earliestBoundary picks the nearer of two optional offsets; -1 means absent.
func earliestBoundary(a, b int) int {
	if a < 0 {
		return b
	}
	if b < 0 || a < b {
		return a
	}
	return b
}

// repeaterIDMarker prefixes the id of every field the scenario export renders.
const repeaterIDMarker = `id="LevelsScenarioRepeater_`

func hasTagPrefix(s, prefix string) bool {
	if len(s) < len(prefix) || !strings.EqualFold(s[:len(prefix)], prefix) {
		return false
	}
	if len(s) == len(prefix) {
		return true
	}
	switch s[len(prefix)] {
	case '>', '/', ' ', '\t', '\n', '\r':
		return true
	}
	return false
}

func parseLevels(raw string, state *assetRewriteState) ([]Level, error) {
	anchors := levelAnchorRe.FindAllStringIndex(raw, -1)
	if len(anchors) == 0 {
		return nil, fmt.Errorf("no level anchors found (LevelsScenarioRepeater)")
	}

	levels := make([]Level, 0, len(anchors))
	for i, anchor := range anchors {
		start := anchor[0]
		end := len(raw)
		if i+1 < len(anchors) {
			end = anchors[i+1][0]
		}
		block := raw[start:end]
		level, ok := parseLevelBlock(block, state)
		if !ok {
			continue
		}
		levels = append(levels, level)
	}

	sort.Slice(levels, func(i, j int) bool { return levels[i].Number < levels[j].Number })
	return levels, nil
}

func parseLevelBlock(block string, state *assetRewriteState) (Level, bool) {
	title := levelTitleRe.FindStringSubmatch(block)
	if len(title) < 2 {
		return Level{}, false
	}
	levelNum, _ := strconv.Atoi(strings.TrimSpace(title[1]))
	levelName := ""
	if len(title) >= 3 {
		// The export quotes the name verbatim; trimming here would silently
		// rewrite names whose trailing spaces are part of the level name.
		levelName = html.UnescapeString(title[2])
	}
	level := Level{
		Number: levelNum,
		Name:   levelName,
	}

	if m := autopassRe.FindStringSubmatch(block); len(m) >= 2 {
		level.AutopassSecond, level.AutopassPenaltySecond = parseAutopassParts(m[1])
	}

	if m := requiredSectorsRe.FindStringSubmatch(block); len(m) >= 2 {
		if !strings.EqualFold(strings.TrimSpace(m[1]), "все") {
			level.RequiredSectorsCount, _ = strconv.Atoi(strings.TrimSpace(m[1]))
		}
	}

	if comments := findElements(block, commentOpenRe, "span"); len(comments) > 0 {
		level.Comment = normalizeHTMLFragment(comments[0].inner, state)
	}

	for _, task := range findElements(block, taskOpenRe, "span") {
		taskHTML := normalizeHTMLFragment(task.inner, state)
		if taskHTML != "" {
			level.Tasks = append(level.Tasks, taskHTML)
		}
	}

	level.Hints = parseHints(block, state)
	level.PenaltyHints = parsePenaltyHints(block, state)

	sectorNames := map[int]string{}
	for _, nameEl := range findElements(block, sectorNameOpenRe, "div") {
		sectorIdx, err := strconv.Atoi(nameEl.groups[1])
		if err != nil {
			continue
		}
		// Sector names are stored verbatim; collapsing runs of spaces here
		// would make the re-exported scenario differ from the source.
		if name := inlineText(nameEl.inner); strings.TrimSpace(name) != "" {
			sectorNames[sectorIdx] = name
		}
	}

	answersBySector := map[int][]string{}
	for _, answerEl := range findElements(block, answerOpenRe, "span") {
		sectorIdx, err := strconv.Atoi(answerEl.groups[1])
		if err != nil {
			continue
		}
		if !answerForRe.MatchString(block[answerEl.end:]) {
			continue
		}
		answer := cleanInlineText(answerEl.inner)
		if answer == "" {
			continue
		}
		answersBySector[sectorIdx] = append(answersBySector[sectorIdx], answer)
	}
	if len(answersBySector) > 0 || len(sectorNames) > 0 {
		keySet := map[int]struct{}{}
		for key := range answersBySector {
			keySet[key] = struct{}{}
		}
		for key := range sectorNames {
			keySet[key] = struct{}{}
		}
		keys := make([]int, 0, len(keySet))
		for key := range keySet {
			keys = append(keys, key)
		}
		sort.Ints(keys)
		level.SectorAnswers = make([][]string, 0, len(keys))
		level.Sectors = make([]Sector, 0, len(keys))
		for _, key := range keys {
			answers := dedupeKeepOrder(answersBySector[key])
			level.SectorAnswers = append(level.SectorAnswers, answers)
			level.Sectors = append(level.Sectors, Sector{
				Name:    sectorNames[key],
				Answers: answers,
			})
		}
	}

	level.Bonuses = parseBonuses(block, state)

	return level, true
}

// parseHints pairs each hint title with the hint body carrying the same
// repeater index. Penalty hints live in a separate repeater and are excluded.
func parseHints(block string, state *assetRewriteState) []Hint {
	texts := elementsByIndex(block, hintTextOpenRe)
	var hints []Hint
	for _, titleEl := range findElements(block, hintTitleOpenRe, "span") {
		textEl, ok := texts[titleEl.indexKey()]
		if !ok || textEl.start < titleEl.end {
			continue
		}
		titleText := cleanInlineText(titleEl.inner)
		hintHTML := normalizeHTMLFragment(textEl.inner, state)
		if hintHTML == "" {
			continue
		}
		hints = append(hints, Hint{
			Title:        titleText,
			Text:         hintHTML,
			DelaySeconds: parseHintDelay(titleText),
		})
	}
	return hints
}

// parsePenaltyHints reads the LevelPenaltyHelpsRepeater block, which carries the
// penalty time, the confirmation flag and the admin-side description in between
// the hint title and the hint body.
func parsePenaltyHints(block string, state *assetRewriteState) []PenaltyHint {
	texts := elementsByIndex(block, penaltyTextOpenRe)
	var hints []PenaltyHint
	for _, titleEl := range findElements(block, penaltyTitleOpenRe, "span") {
		textEl, ok := texts[titleEl.indexKey()]
		// The body always follows its title; anything else means the title
		// consumed the body (unclosed markup) and the pair is unusable.
		if !ok || textEl.start < titleEl.end {
			continue
		}
		titleText := cleanInlineText(titleEl.inner)
		hint := PenaltyHint{
			Title:        titleText,
			Text:         normalizeHTMLFragment(textEl.inner, state),
			DelaySeconds: parseHintDelay(titleText),
		}
		if body := block[titleEl.end:textEl.start]; body != "" {
			if m := penaltyTimeRe.FindStringSubmatch(body); len(m) >= 2 {
				hint.PenaltySeconds = ParseRuDuration(html.UnescapeString(m[1]))
			}
			if m := penaltyConfirmRe.FindStringSubmatch(body); len(m) >= 2 {
				hint.RequestConfirm = strings.EqualFold(strings.TrimSpace(m[1]), "Да")
			}
			if els := findElements(body, penaltyCommentRe, "span"); len(els) > 0 {
				hint.Comment = cleanInlineText(els[0].inner)
			}
		}
		if hint.Text == "" && hint.Comment == "" {
			continue
		}
		hints = append(hints, hint)
	}
	return hints
}

// elementsByIndex maps the outer and inner repeater indices of openRe to the
// matched element. Both indices are part of the key: a block delimited by level
// anchors can still contain more than one outer index, and keying on the inner
// one alone would pair a title with another level's body.
func elementsByIndex(block string, openRe *regexp.Regexp) map[string]element {
	out := map[string]element{}
	for _, el := range findElements(block, openRe, "span") {
		key := el.indexKey()
		if key == "" {
			continue
		}
		if _, seen := out[key]; !seen {
			out[key] = el
		}
	}
	return out
}

func parseAutopassParts(text string) (autopassSecond, penaltySecond int) {
	text = strings.TrimSpace(html.UnescapeString(text))
	parts := strings.SplitN(text, ",", 2)
	autopassSecond = ParseRuDuration(parts[0])
	if len(parts) == 2 {
		penaltyText := strings.TrimSpace(parts[1])
		penaltyText = regexp.MustCompile(`(?i)\bштраф\w*:?\s*`).ReplaceAllString(penaltyText, "")
		penaltySecond = ParseRuDuration(penaltyText)
	}
	return autopassSecond, penaltySecond
}

func parseBonuses(block string, state *assetRewriteState) []Bonus {
	headers := findElements(block, bonusHeaderOpenRe, "span")
	if len(headers) == 0 {
		return nil
	}
	bonuses := make([]Bonus, 0, len(headers))
	for i, header := range headers {
		bodyEnd := len(block)
		if i+1 < len(headers) {
			bodyEnd = headers[i+1].start
		}
		body := block[header.end:bodyEnd]

		titleText := cleanInlineText(header.inner)
		num, name, ok := parseBonusTitle(titleText)
		if !ok {
			continue
		}

		award := 0
		negative := false
		if am := bonusAwardRe.FindStringSubmatch(body); len(am) >= 3 {
			negative = strings.EqualFold(strings.TrimSpace(am[1]), "Штрафное")
			award = ParseRuDuration(html.UnescapeString(strings.TrimSpace(am[2])))
		}

		task := extractBonusWhiteField(body, "Задание", state)
		hint := extractBonusWhiteField(body, "Подсказка", state)

		bonuses = append(bonuses, Bonus{
			Number:       num,
			Name:         name,
			AwardSeconds: award,
			Negative:     negative,
			Task:         task,
			Hint:         hint,
			Answers:      dedupeKeepOrder(parseBonusAnswers(body)),
		})
	}
	sort.Slice(bonuses, func(i, j int) bool { return bonuses[i].Number < bonuses[j].Number })
	return bonuses
}

func parseBonusTitle(titleText string) (int, string, bool) {
	m := bonusTitleNumberRe.FindStringSubmatchIndex(titleText)
	if len(m) < 4 {
		return 0, "", false
	}
	num, err := strconv.Atoi(strings.TrimSpace(titleText[m[2]:m[3]]))
	if err != nil {
		return 0, "", false
	}
	rest := strings.TrimSpace(titleText[m[1]:])
	if !strings.HasPrefix(rest, `"`) {
		return num, "", true
	}
	end := strings.LastIndex(rest, `"`)
	if end <= 0 {
		return num, "", true
	}
	return num, strings.TrimSpace(rest[1:end]), true
}

func extractBonusWhiteField(body, label string, state *assetRewriteState) string {
	re := regexp.MustCompile(`(?is)<span\s+class="green">\s*` + regexp.QuoteMeta(label) + `\s*</span>\s*<br\s*/?>\s*<span\s+class="white"\s*>`)
	els := findElements(body, re, "span")
	if len(els) == 0 {
		return ""
	}
	return normalizeHTMLFragment(els[0].inner, state)
}

func parseBonusAnswers(body string) []string {
	answers := make([]string, 0)
	for _, el := range findElements(body, bonusAnswerOpenRe, "span") {
		if answer := cleanInlineText(el.inner); answer != "" {
			answers = append(answers, answer)
		}
	}
	return answers
}

func normalizeHTMLFragment(fragment string, state *assetRewriteState) string {
	out := strings.TrimSpace(fragment)
	out = strings.ReplaceAll(out, "\r\n", "\n")
	out = strings.ReplaceAll(out, "\r", "\n")
	out = normalizeBRAdjacentNewlines(out)
	out = embedLocalAssets(out, state)
	return strings.TrimSpace(out)
}

func normalizeBRAdjacentNewlines(fragment string) string {
	beforeBR := regexp.MustCompile(`(?is)\n[ \t]*(<br\s*/?>)`)
	afterBR := regexp.MustCompile(`(?is)(<br\s*/?>)\s*\n\s*`)
	fragment = beforeBR.ReplaceAllString(fragment, "$1")
	fragment = afterBR.ReplaceAllString(fragment, "$1")
	return fragment
}

func cleanInlineText(v string) string {
	return strings.Join(strings.Fields(inlineText(v)), " ")
}

// inlineText strips markup and unescapes entities while keeping the original
// spacing, so verbatim fields (sector names) survive a scenario round-trip.
func inlineText(v string) string {
	return html.UnescapeString(stripHTML(v))
}

func stripHTML(v string) string {
	v = regexp.MustCompile(`(?is)<br\s*/?>`).ReplaceAllString(v, "\n")
	v = regexp.MustCompile(`(?is)<[^>]+>`).ReplaceAllString(v, "")
	return v
}

func parseHintDelay(title string) int {
	match := hintDelayRe.FindStringSubmatch(title)
	if len(match) < 2 {
		return 0
	}
	return ParseRuDuration(strings.TrimSpace(match[1]))
}

func dedupeKeepOrder(items []string) []string {
	out := make([]string, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func embedLocalAssets(fragment string, state *assetRewriteState) string {
	if state == nil {
		return fragment
	}
	return assetAttrRe.ReplaceAllStringFunc(fragment, func(attr string) string {
		m := assetAttrRe.FindStringSubmatch(attr)
		if len(m) < 3 {
			return attr
		}
		pathValue := strings.TrimSpace(m[2])
		if skipAssetEmbedding(pathValue) {
			return attr
		}
		localPath := resolveAssetPath(state.baseDir, pathValue)
		if localPath == "" {
			return attr
		}
		dataURI, ok := state.cache[localPath]
		if !ok {
			payload, err := os.ReadFile(localPath)
			if err != nil {
				state.missingSet[pathValue] = struct{}{}
				return attr
			}
			mimeType := mime.TypeByExtension(strings.ToLower(filepath.Ext(localPath)))
			if mimeType == "" {
				mimeType = http.DetectContentType(payload)
			}
			if mimeType == "" {
				mimeType = "application/octet-stream"
			}
			dataURI = "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(payload)
			state.cache[localPath] = dataURI
			state.embeddedCount++
		}
		return fmt.Sprintf(`%s="%s"`, m[1], dataURI)
	})
}

func skipAssetEmbedding(pathValue string) bool {
	lower := strings.ToLower(pathValue)
	return strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "data:") ||
		strings.HasPrefix(lower, "geo:") ||
		strings.HasPrefix(lower, "mailto:") ||
		strings.HasPrefix(lower, "tel:") ||
		strings.HasPrefix(lower, "#") ||
		strings.HasPrefix(lower, "//")
}

func resolveAssetPath(baseDir, pathValue string) string {
	if baseDir == "" || pathValue == "" {
		return ""
	}
	clean := strings.ReplaceAll(pathValue, "\\", "/")
	if strings.HasPrefix(clean, "/") {
		return ""
	}
	local := filepath.Clean(filepath.Join(baseDir, filepath.FromSlash(clean)))
	baseAbs, err := filepath.Abs(baseDir)
	if err != nil {
		return ""
	}
	localAbs, err := filepath.Abs(local)
	if err != nil {
		return ""
	}
	baseWithSep := baseAbs + string(os.PathSeparator)
	if localAbs != baseAbs && !strings.HasPrefix(localAbs, baseWithSep) {
		return ""
	}
	return localAbs
}
