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

// Bonus is a parsed bonus block from a GameScenario export.
type Bonus struct {
	Number       int      `json:"number"`
	Name         string   `json:"name,omitempty"`
	AwardSeconds int      `json:"award_seconds,omitempty"`
	Task         string   `json:"task,omitempty"`
	Answers      []string `json:"answers,omitempty"`
}

// Level is one level block from a GameScenario export.
type Level struct {
	Number         int        `json:"number"`
	Name           string     `json:"name"`
	AutopassSecond int        `json:"autopass_seconds,omitempty"`
	Tasks          []string   `json:"tasks,omitempty"`
	Hints          []Hint     `json:"hints,omitempty"`
	SectorAnswers  [][]string `json:"sector_answers,omitempty"`
	Bonuses        []Bonus    `json:"bonuses,omitempty"`
}

// Document is a parsed GameScenario.aspx HTML export.
type Document struct {
	SourcePath     string  `json:"source_path"`
	GameID         int     `json:"game_id,omitempty"`
	GameNum        int     `json:"game_num,omitempty"`
	GameTitle      string  `json:"game_title,omitempty"`
	Levels         []Level `json:"levels"`
	EmbeddedAssets int     `json:"embedded_assets"`
	MissingAssets  []string `json:"missing_assets,omitempty"`
}

type assetRewriteState struct {
	baseDir       string
	cache         map[string]string
	embeddedCount int
	missingSet    map[string]struct{}
}

var (
	levelAnchorRe = regexp.MustCompile(`(?is)<a id="LevelsScenarioRepeater_ctl\d+_lnkLevelAnchorPoint" name="\d+"></a>`)
	levelTitleRe  = regexp.MustCompile(`(?is)Уровень №\s*(\d+)\s*(?:"([^"]*)")?`)
	autopassRe    = regexp.MustCompile(`(?is)Автопереход:\s*через\s*([^<]+)`)
	taskRe        = regexp.MustCompile(`(?is)<span[^>]*id="LevelsScenarioRepeater_ctl\d+_LevelTasksRepeater_ctl\d+_lblLevelTask"[^>]*>(.*?)</span>`)
	hintPairRe    = regexp.MustCompile(`(?is)<span[^>]*id="LevelsScenarioRepeater_ctl\d+_LevelHelpsRepeater_ctl\d+_lblLevelHelpTitle"[^>]*>(.*?)</span>\s*<br>\s*<span[^>]*id="LevelsScenarioRepeater_ctl\d+_LevelHelpsRepeater_ctl\d+_lblLevelHelp"[^>]*>(.*?)</span>`)
	answerRe      = regexp.MustCompile(`(?is)<span[^>]*id="LevelsScenarioRepeater_ctl\d+_SectorsRepeater_ctl(\d+)_LevelAnswersRepeater_ctl\d+_lblLevelAnswer"[^>]*>(.*?)</span>\s*-\s*<span[^>]*id="LevelsScenarioRepeater_ctl\d+_SectorsRepeater_ctl\d+_LevelAnswersRepeater_ctl\d+_lblAnswerFor"`)
	bonusHeaderRe = regexp.MustCompile(`(?is)<span[^>]*id="LevelsScenarioRepeater_ctl\d+_LevelBonusesRepeater_ctl\d+_lblBonusNum"[^>]*>(.*?)</span>`)
	bonusTitleRe  = regexp.MustCompile(`(?is)Бонус\s*№\s*(\d+)\s*"([^"]*)"`)
	bonusAwardRe  = regexp.MustCompile(`(?is)Бонусное\s+время:\s*([^<]+)`)
	bonusTaskRe   = regexp.MustCompile(`(?is)<span\s+class="green">Задание</span><br>\s*(.*?)\s*<br><br>\s*<span\s+class="green">Ответы</span>`)
	bonusAnswerOpenRe = regexp.MustCompile(`(?is)<span[^>]*id="LevelsScenarioRepeater_ctl\d+_LevelBonusesRepeater_ctl\d+_BonusAnswersRepeater_ctl\d+_lblBonusAnswer"[^>]*>`)
	bonusAnswerTailRe = regexp.MustCompile(`(?is)^\s*(?:<br|</div>|<span[^>]*BonusAnswersRepeater_ctl\d+_lblBonusAnswer|<span[^>]*lblBonusNum|$)`)
	hintDelayRe   = regexp.MustCompile(`\(([^()]*)\)\s*$`)
	durationRe    = regexp.MustCompile(`(?i)(\d+)\s*(день|дня|дней|час(?:а|ов)?|минут(?:а|ы)?|секунд(?:а|ы)?)`)
	assetAttrRe   = regexp.MustCompile(`(?i)\b(src|href)\s*=\s*"([^"]+)"`)
	gameTitleRe   = regexp.MustCompile(`(?is)id="lnkGameInfo"[^>]*>([^<]+)</a>`)
	gameIDRe      = regexp.MustCompile(`(?is)id="lblGameId"[^>]*>(\d+)`)
	gameNumRe     = regexp.MustCompile(`(?is)id="lblGameNumber"[^>]*>(\d+)`)
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

	levels, err := parseLevels(string(raw), state)
	if err != nil {
		return nil, err
	}
	missing := make([]string, 0, len(state.missingSet))
	for p := range state.missingSet {
		missing = append(missing, p)
	}
	sort.Strings(missing)

	doc := &Document{
		SourcePath:     absPath,
		Levels:         levels,
		EmbeddedAssets: state.embeddedCount,
		MissingAssets:  missing,
	}
	if m := gameTitleRe.FindStringSubmatch(string(raw)); len(m) >= 2 {
		doc.GameTitle = strings.TrimSpace(html.UnescapeString(m[1]))
	}
	if m := gameIDRe.FindStringSubmatch(string(raw)); len(m) >= 2 {
		doc.GameID, _ = strconv.Atoi(m[1])
	}
	if m := gameNumRe.FindStringSubmatch(string(raw)); len(m) >= 2 {
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
		levelName = strings.TrimSpace(html.UnescapeString(title[2]))
	}
	level := Level{
		Number: levelNum,
		Name:   levelName,
	}

	if m := autopassRe.FindStringSubmatch(block); len(m) >= 2 {
		level.AutopassSecond = ParseRuDuration(strings.TrimSpace(html.UnescapeString(m[1])))
	}

	for _, taskMatch := range taskRe.FindAllStringSubmatch(block, -1) {
		if len(taskMatch) < 2 {
			continue
		}
		taskHTML := normalizeHTMLFragment(taskMatch[1], state)
		if taskHTML != "" {
			level.Tasks = append(level.Tasks, taskHTML)
		}
	}

	for _, hintMatch := range hintPairRe.FindAllStringSubmatch(block, -1) {
		if len(hintMatch) < 3 {
			continue
		}
		titleText := cleanInlineText(hintMatch[1])
		hintHTML := normalizeHTMLFragment(hintMatch[2], state)
		if hintHTML == "" {
			continue
		}
		level.Hints = append(level.Hints, Hint{
			Title:        titleText,
			Text:         hintHTML,
			DelaySeconds: parseHintDelay(titleText),
		})
	}

	answersBySector := map[int][]string{}
	for _, answerMatch := range answerRe.FindAllStringSubmatch(block, -1) {
		if len(answerMatch) < 3 {
			continue
		}
		sectorIdx, err := strconv.Atoi(answerMatch[1])
		if err != nil {
			continue
		}
		answer := cleanInlineText(answerMatch[2])
		if answer == "" {
			continue
		}
		answersBySector[sectorIdx] = append(answersBySector[sectorIdx], answer)
	}
	if len(answersBySector) > 0 {
		keys := make([]int, 0, len(answersBySector))
		for key := range answersBySector {
			keys = append(keys, key)
		}
		sort.Ints(keys)
		level.SectorAnswers = make([][]string, 0, len(keys))
		for _, key := range keys {
			level.SectorAnswers = append(level.SectorAnswers, dedupeKeepOrder(answersBySector[key]))
		}
	}

	level.Bonuses = parseBonuses(block, state)

	return level, true
}

func parseBonuses(block string, state *assetRewriteState) []Bonus {
	headers := bonusHeaderRe.FindAllStringSubmatchIndex(block, -1)
	if len(headers) == 0 {
		return nil
	}
	bonuses := make([]Bonus, 0, len(headers))
	for i, loc := range headers {
		titleHTML := block[loc[2]:loc[3]]
		bodyStart := loc[1]
		bodyEnd := len(block)
		if i+1 < len(headers) {
			bodyEnd = headers[i+1][0]
		}
		body := block[bodyStart:bodyEnd]

		titleText := cleanInlineText(titleHTML)
		m := bonusTitleRe.FindStringSubmatch(titleText)
		if len(m) < 3 {
			continue
		}
		num, _ := strconv.Atoi(strings.TrimSpace(m[1]))
		name := strings.TrimSpace(m[2])

		award := 0
		if am := bonusAwardRe.FindStringSubmatch(body); len(am) >= 2 {
			award = ParseRuDuration(html.UnescapeString(strings.TrimSpace(am[1])))
		}

		task := ""
		if tm := bonusTaskRe.FindStringSubmatch(body); len(tm) >= 2 {
			task = normalizeHTMLFragment(tm[1], state)
		}

		bonuses = append(bonuses, Bonus{
			Number:       num,
			Name:         name,
			AwardSeconds: award,
			Task:         task,
			Answers:      dedupeKeepOrder(parseBonusAnswers(body)),
		})
	}
	sort.Slice(bonuses, func(i, j int) bool { return bonuses[i].Number < bonuses[j].Number })
	return bonuses
}

func parseBonusAnswers(body string) []string {
	answers := make([]string, 0)
	for _, open := range bonusAnswerOpenRe.FindAllStringIndex(body, -1) {
		start := open[1]
		pos := start
		for pos < len(body) {
			closeRel := strings.Index(body[pos:], "</span>")
			if closeRel < 0 {
				break
			}
			closeEnd := pos + closeRel + len("</span>")
			if bonusAnswerTailRe.MatchString(body[closeEnd:]) {
				if answer := cleanInlineText(body[start : pos+closeRel]); answer != "" {
					answers = append(answers, answer)
				}
				break
			}
			pos = closeEnd
		}
	}
	return answers
}

func normalizeHTMLFragment(fragment string, state *assetRewriteState) string {
	out := strings.TrimSpace(fragment)
	out = strings.ReplaceAll(out, "\r\n", "\n")
	out = strings.ReplaceAll(out, "\r", "\n")
	out = embedLocalAssets(out, state)
	return strings.TrimSpace(out)
}

func cleanInlineText(v string) string {
	text := stripHTML(v)
	text = html.UnescapeString(text)
	return strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
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
		strings.HasPrefix(lower, "mailto:") ||
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
