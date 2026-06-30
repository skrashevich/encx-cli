package encx

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ErrSectorStarted means Encounter refused to delete a sector because participants started it.
var ErrSectorStarted = errors.New("encx: sector cannot be deleted because it has been started by participants")

var (
	sectorDeleteIDRE             = regexp.MustCompile(`(?i)[?&]delsector=(\d+)`)
	listSectorAnswerRE           = regexp.MustCompile(`(?is)divAnswersView_(\d+)[^>]*>[\s\S]*?nonLatinChar[^>]*>([^<]*)</span>([^<]*)`)
	sectorStartedDeleteMessageRE = regexp.MustCompile(`(?i)(?:Ошибка\.?\s*)?Сектор\s+не\s+может\s+быть\s+удален[^.]*начали\s+проходить\s+участник`)
	sectorDeleteAnswerRE         = regexp.MustCompile(`(?i)name="(chkDeleteAnswer_\d+)"[^>]*value="(\d+)"`)
	htmlInputTagRE               = regexp.MustCompile(`(?is)<input\b([^>/]*)(/?)>`)
	htmlInputTypeAttrRE          = regexp.MustCompile(`(?is)\btype\s*=\s*["']([^"']*)["']`)
	htmlInputNameAttrRE          = regexp.MustCompile(`(?is)\bname\s*=\s*["']([^"']*)["']`)
	htmlInputValueAttrRE         = regexp.MustCompile(`(?is)\bvalue\s*=\s*["']([^"']*)["']`)
	htmlFormOpenRE               = regexp.MustCompile(`(?is)<form\b[^>]*>`)
	htmlFormBlockRE              = regexp.MustCompile(`(?is)<form\b[\s\S]*?</form>`)
)

func parseListPageSectorAnswersMap(body string) map[int][]string {
	out := make(map[int][]string)
	for _, m := range listSectorAnswerRE.FindAllStringSubmatch(body, -1) {
		id, err := strconv.Atoi(m[1])
		if err != nil || id <= 0 {
			continue
		}
		if answer := strings.TrimSpace(html.UnescapeString(m[2] + m[3])); answer != "" {
			out[id] = []string{answer}
		}
	}
	return out
}

func parseSectorDeleteIDs(body string) []int {
	seen := map[int]struct{}{}
	var ids []int
	for _, m := range sectorDeleteIDRE.FindAllStringSubmatch(body, -1) {
		id, err := strconv.Atoi(m[1])
		if err != nil || id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(ids)))
	return ids
}

type sectorAnswerField struct {
	AnswerName string
	ForName    string
	Value      string
}

func parseSectorAnswerFields(body string) []sectorAnswerField {
	var fields []sectorAnswerField
	for _, m := range htmlInputTagRE.FindAllStringSubmatch(body, -1) {
		attrs := m[1]
		name := htmlAttrValue(attrs, "name")
		if !strings.HasPrefix(name, "txtAnswer") {
			continue
		}
		fields = append(fields, sectorAnswerField{
			AnswerName: name,
			ForName:    strings.Replace(name, "txtAnswer", "ddlAnswerFor", 1),
			Value:      htmlAttrValue(attrs, "value"),
		})
	}
	return fields
}

func htmlAttrValue(attrs, key string) string {
	var re *regexp.Regexp
	switch strings.ToLower(key) {
	case "type":
		re = htmlInputTypeAttrRE
	case "name":
		re = htmlInputNameAttrRE
	case "value":
		re = htmlInputValueAttrRE
	default:
		return ""
	}
	if m := re.FindStringSubmatch(attrs); len(m) >= 2 {
		return html.UnescapeString(m[1])
	}
	return ""
}

func parseHTMLHiddenFields(body string) url.Values {
	form := url.Values{}
	for _, m := range htmlInputTagRE.FindAllStringSubmatch(body, -1) {
		attrs := m[1]
		if !strings.EqualFold(htmlAttrValue(attrs, "type"), "hidden") {
			continue
		}
		if name := htmlAttrValue(attrs, "name"); name != "" {
			form.Set(name, htmlAttrValue(attrs, "value"))
		}
	}
	return form
}

func sectorEditFormBlock(body string, sectorID int) string {
	markerRe := regexp.MustCompile(fmt.Sprintf(`(?is)<input\b[^>]*\bname\s*=\s*["']updateanswers["'][^>]*\bvalue\s*=\s*["']%d["'][^>]*>`, sectorID))
	loc := markerRe.FindStringIndex(body)
	if loc == nil {
		return body
	}
	start := 0
	for _, formLoc := range htmlFormOpenRE.FindAllStringIndex(body[:loc[0]], -1) {
		start = formLoc[0]
	}
	end := len(body)
	if closeRel := strings.Index(strings.ToLower(body[loc[1]:]), "</form>"); closeRel >= 0 {
		end = loc[1] + closeRel + len("</form>")
	}
	return body[start:end]
}

func sectorAddAnswersFormBlock(body string, sectorID int) string {
	marker := fmt.Sprintf("divSectorsAddAnswersRows_%d", sectorID)
	for _, loc := range htmlFormBlockRE.FindAllStringIndex(body, -1) {
		block := body[loc[0]:loc[1]]
		if strings.Contains(block, marker) {
			return block
		}
	}
	return body
}

func firstImageSubmitName(body string) string {
	for _, m := range htmlInputTagRE.FindAllStringSubmatch(body, -1) {
		attrs := m[1]
		if !strings.EqualFold(htmlAttrValue(attrs, "type"), "image") {
			continue
		}
		if name := htmlAttrValue(attrs, "name"); name != "" {
			return name
		}
	}
	return ""
}

func (c *Client) adminReadSectorEditPage(ctx context.Context, gameID, levelNum, sectorID int) (string, error) {
	u := fmt.Sprintf("%s/Administration/Games/LevelEditor.aspx?level=%d&gid=%d&swanswers=1&editanswers=%d",
		c.baseURL(), levelNum, gameID, sectorID)
	body, err := c.doGet(ctx, u)
	if err != nil {
		return "", fmt.Errorf("encx: read sector %d editor: %w", sectorID, err)
	}
	return body, nil
}

func (c *Client) adminSaveSectorForm(ctx context.Context, gameID, levelNum, sectorID int, name string, answers []string) error {
	maxRounds := len(answers) + 2
	if maxRounds < 2 {
		maxRounds = 2
	}
	for round := 0; round < maxRounds; round++ {
		editBody, err := c.adminReadSectorEditPage(ctx, gameID, levelNum, sectorID)
		if err != nil {
			return err
		}
		editBody = sectorEditFormBlock(editBody, sectorID)
		fields := parseSectorAnswerFields(editBody)
		form := parseHTMLHiddenFields(editBody)
		form.Set("ddlSector", strconv.Itoa(sectorID))
		form.Set("updateanswers", strconv.Itoa(sectorID))
		form.Set("txtSectorName", name)
		form.Set("btnSaveSector.x", "1")
		form.Set("btnSaveSector.y", "1")

		for _, field := range fields {
			form.Set(field.AnswerName, "")
			form.Set(field.ForName, "0")
		}
		if len(answers) == 0 {
			for _, m := range sectorDeleteAnswerRE.FindAllStringSubmatch(editBody, -1) {
				form.Set(m[1], m[2])
			}
		} else {
			targets := make([]sectorAnswerField, 0, len(fields))
			for _, field := range fields {
				if strings.TrimSpace(field.Value) != "" {
					targets = append(targets, field)
				}
			}
			for _, field := range fields {
				if strings.TrimSpace(field.Value) == "" {
					targets = append(targets, field)
				}
			}
			for i, answer := range answers {
				if i >= len(targets) {
					break
				}
				form.Set(targets[i].AnswerName, answer)
			}
		}

		u := fmt.Sprintf("%s/Administration/Games/LevelEditor.aspx?gid=%d&level=%d&swanswers=1&editanswers=%d",
			c.baseURL(), gameID, levelNum, sectorID)
		if _, err := c.doPost(ctx, u, form); err != nil {
			return fmt.Errorf("encx: save sector %d form: %w", sectorID, err)
		}
		c.adminDelay()
		if len(answers) <= len(fields) {
			return nil
		}
	}
	return fmt.Errorf("encx: save sector %d form: could not allocate %d answer fields", sectorID, len(answers))
}

func (c *Client) adminReadSectorAnswersList(ctx context.Context, gameID, levelNum int) (string, error) {
	body, err := c.doGet(ctx, c.adminLevelEditorAnswersListURL(gameID, levelNum))
	if err != nil {
		return "", fmt.Errorf("encx: read sector answers list: %w", err)
	}
	return body, nil
}

func sectorPresent(body string, sectorID int) bool {
	id := strconv.Itoa(sectorID)
	return strings.Contains(body, "delsector="+id) ||
		strings.Contains(body, "divAnswersView_"+id) ||
		strings.Contains(body, "divSectorManage_"+id)
}

func (c *Client) adminDeleteSector(ctx context.Context, gameID, levelNum, sectorID int) error {
	deleteBody, err := c.adminDeleteSectorAndReadList(ctx, gameID, levelNum, sectorID)
	if err != nil {
		return err
	}
	if !sectorPresent(deleteBody, sectorID) {
		return nil
	}

	listBody, err := c.adminReadSectorAnswersList(ctx, gameID, levelNum)
	if err != nil {
		return err
	}
	if sectorPresent(listBody, sectorID) {
		return ErrSectorStarted
	}
	return nil
}

func (c *Client) adminDeleteSectorAndReadList(ctx context.Context, gameID, levelNum, sectorID int) (string, error) {
	listURL := c.adminLevelEditorAnswersListURL(gameID, levelNum)
	deleteBody, err := c.adminDeleteSectorURL(ctx, fmt.Sprintf("%s&delsector=%d", listURL, sectorID), listURL)
	if err != nil {
		return "", err
	}
	if sectorStartedDeleteMessageRE.MatchString(html.UnescapeString(deleteBody)) {
		return "", ErrSectorStarted
	}
	return deleteBody, nil
}

// AdminClearLevelSectors deletes all sectors exposed by the level editor.
func (c *Client) AdminClearLevelSectors(ctx context.Context, gameID, levelNum int) error {
	const maxRounds = 3
	body, err := c.adminReadSectorAnswersList(ctx, gameID, levelNum)
	if err != nil {
		return err
	}
	for range maxRounds {
		ids := parseSectorDeleteIDs(body)
		if len(ids) == 0 {
			return nil
		}

		deleted := 0
		var startedErr error
		for _, id := range ids {
			nextBody, err := c.adminDeleteSectorAndReadList(ctx, gameID, levelNum, id)
			if err != nil {
				if errors.Is(err, ErrSectorStarted) {
					startedErr = err
					continue
				}
				return fmt.Errorf("encx: delete sector %d: %w", id, err)
			}
			body = nextBody
			deleted++
		}
		if deleted == 0 {
			if startedErr != nil {
				return startedErr
			}
			return fmt.Errorf("encx: could not delete sectors on level %d", levelNum)
		}
	}
	return fmt.Errorf("encx: sector cleanup exceeded %d rounds on level %d", maxRounds, levelNum)
}
