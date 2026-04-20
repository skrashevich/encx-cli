package encx

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Regex patterns for parsing admin HTML responses.
var (
	// LevelManager: <input name="txtLevelName_12345" ... value="Level Name" ...>
	adminLevelRe = regexp.MustCompile(`(?i)<input[^>]*name="txtLevelName_(\d+)"[^>]*value="([^"]*)"`)
	// Teams dropdown: <option value="123">Team Name</option>
	adminTeamOptionRe = regexp.MustCompile(`(?i)<option\s+value="([^"]*)"[^>]*>([^<]+)</option>`)
	// Bonus link: data-bonusid="123"
	adminBonusIdRe = regexp.MustCompile(`(?i)data-bonusid="(\d+)"`)
	// Hint link: prid=123
	adminHintIdRe = regexp.MustCompile(`(?i)prid=(\d+)`)
	// Sector div: id="...SectorEdit_123..."
	adminSectorIdRe = regexp.MustCompile(`(?i)id="[^"]*SectorEdit_(\d+)"`)
	// Correction row parsing
	adminCorrectionRe = regexp.MustCompile(`(?i)<tr\s+class="toWinnerItem"[^>]*>(.*?)</tr>`)
	adminTdRe         = regexp.MustCompile(`(?i)<td[^>]*>(.*?)</td>`)
	adminHrefRe       = regexp.MustCompile(`(?i)href="([^"]*)"`)
	adminATextRe      = regexp.MustCompile(`(?i)<a[^>]*>([^<]*)</a>`)
)

// doPost performs a POST request with form-encoded payload and returns the response body.
func (c *Client) doPost(ctx context.Context, rawURL string, form url.Values) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("encx: create POST request: %w", err)
	}
	c.setHeaders(req)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("encx: POST %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("encx: read POST response: %w", err)
	}

	return string(body), nil
}

// --- Level Management ---

// AdminGetLevels fetches the list of levels for a game from the admin panel.
func (c *Client) AdminGetLevels(ctx context.Context, gameId int) ([]AdminLevel, error) {
	u := fmt.Sprintf("%s/Administration/Games/LevelManager.aspx?gid=%d", c.baseURL(), gameId)
	body, err := c.doGet(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("encx: admin get levels: %w", err)
	}

	matches := adminLevelRe.FindAllStringSubmatch(body, -1)
	levels := make([]AdminLevel, 0, len(matches))
	for i, m := range matches {
		id, _ := strconv.Atoi(m[1])
		levels = append(levels, AdminLevel{
			Number: i + 1,
			Name:   m[2],
			ID:     id,
		})
	}

	return levels, nil
}

// AdminCreateLevels creates the specified number of new levels in the game.
func (c *Client) AdminCreateLevels(ctx context.Context, gameId, count int) error {
	u := fmt.Sprintf("%s/Administration/Games/LevelManager.aspx?gid=%d&levels=create&ddlCreateLevelsNum=%d",
		c.baseURL(), gameId, count)
	_, err := c.doGet(ctx, u)
	if err != nil {
		return fmt.Errorf("encx: admin create levels: %w", err)
	}
	return nil
}

// AdminDeleteLevel deletes a level by its number.
func (c *Client) AdminDeleteLevel(ctx context.Context, gameId, levelNum int) error {
	u := fmt.Sprintf("%s/Administration/Games/LevelManager.aspx?gid=%d&levels=delete&ddlDeleteLevels=%d",
		c.baseURL(), gameId, levelNum)
	_, err := c.doGet(ctx, u)
	if err != nil {
		return fmt.Errorf("encx: admin delete level: %w", err)
	}
	return nil
}

// AdminRenameLevels renames levels. The map key is the level ID, value is the new name.
func (c *Client) AdminRenameLevels(ctx context.Context, gameId int, names map[int]string) error {
	u := fmt.Sprintf("%s/Administration/Games/LevelManager.aspx?gid=%d&level_names=update", c.baseURL(), gameId)

	form := url.Values{}
	for id, name := range names {
		form.Set(fmt.Sprintf("txtLevelName_%d", id), name)
	}

	_, err := c.doPost(ctx, u, form)
	if err != nil {
		return fmt.Errorf("encx: admin rename levels: %w", err)
	}
	return nil
}

// AdminUpdateAutopass updates the autopass settings for a level.
func (c *Client) AdminUpdateAutopass(ctx context.Context, gameId, levelNum int, s AdminLevelSettings) error {
	u := fmt.Sprintf("%s/Administration/Games/LevelEditor.aspx?gid=%d&level=%d", c.baseURL(), gameId, levelNum)

	form := url.Values{}
	form.Set("txtApHours", strconv.Itoa(s.AutopassHours))
	form.Set("txtApMinutes", strconv.Itoa(s.AutopassMinutes))
	form.Set("txtApSeconds", strconv.Itoa(s.AutopassSeconds))
	form.Set("txtApPenaltyHours", strconv.Itoa(s.PenaltyHours))
	form.Set("txtApPenaltyMinutes", strconv.Itoa(s.PenaltyMinutes))
	form.Set("txtApPenaltySeconds", strconv.Itoa(s.PenaltySeconds))
	form.Set("updateautopass", " ")

	if s.TimeoutPenalty {
		form.Set("chkTimeoutPenalty", "on")
	}

	_, err := c.doPost(ctx, u, form)
	if err != nil {
		return fmt.Errorf("encx: admin update autopass: %w", err)
	}
	return nil
}

// AdminUpdateAnswerBlock updates the answer block settings for a level.
func (c *Client) AdminUpdateAnswerBlock(ctx context.Context, gameId, levelNum int, s AdminLevelSettings) error {
	u := fmt.Sprintf("%s/Administration/Games/LevelEditor.aspx?gid=%d&level=%d", c.baseURL(), gameId, levelNum)

	form := url.Values{}
	form.Set("txtAttemptsNumber", strconv.Itoa(s.AttemptsNumber))
	form.Set("txtAttemptsPeriodHours", strconv.Itoa(s.AttemptsPeriodHours))
	form.Set("txtAttemptsPeriodMinutes", strconv.Itoa(s.AttemptsPeriodMinutes))
	form.Set("txtAttemptsPeriodSeconds", strconv.Itoa(s.AttemptsPeriodSeconds))
	form.Set("rbApplyForPlayer", strconv.Itoa(s.ApplyForPlayer))
	form.Set("action", "upansblock")

	_, err := c.doPost(ctx, u, form)
	if err != nil {
		return fmt.Errorf("encx: admin update answer block: %w", err)
	}
	return nil
}

// --- Bonus Management ---

// AdminCreateBonus creates a new bonus on the specified level.
func (c *Client) AdminCreateBonus(ctx context.Context, gameId, levelNum int, b AdminBonus) error {
	u := fmt.Sprintf("%s/Administration/Games/BonusEdit.aspx?gid=%d&level=%d&bonus=0&action=save",
		c.baseURL(), gameId, levelNum)

	form := url.Values{}
	form.Set("txtBonusName", b.Name)
	form.Set("txtTask", b.Task)
	form.Set("txtHelp", b.Hint)
	form.Set("ddlBonusFor", b.BonusFor)

	if b.LevelID == -1 || b.LevelID == 0 {
		// Bonus for all levels
		form.Set("rbAllLevels", "1")
	} else {
		// Bonus for specific level
		form.Set("rbAllLevels", "0")
		form.Set(fmt.Sprintf("level_%d", b.LevelID), "on")
	}

	for i, ans := range b.Answers {
		form.Set(fmt.Sprintf("answer_-%d", i+1), ans)
	}

	form.Set("txtHours", strconv.Itoa(b.AwardHours))
	form.Set("txtMinutes", strconv.Itoa(b.AwardMinutes))
	form.Set("txtSeconds", strconv.Itoa(b.AwardSeconds))

	if b.Negative {
		form.Set("negative", "on")
	}

	if b.ValidFrom != "" || b.ValidTo != "" {
		form.Set("chkAbsoluteLimit", "on")
		form.Set("txtValidFrom", b.ValidFrom)
		form.Set("txtValidTo", b.ValidTo)
	}

	if b.DelayHours > 0 || b.DelayMinutes > 0 || b.DelaySeconds > 0 {
		form.Set("chkDelay", "on")
		form.Set("txtDelayHours", strconv.Itoa(b.DelayHours))
		form.Set("txtDelayMinutes", strconv.Itoa(b.DelayMinutes))
		form.Set("txtDelaySeconds", strconv.Itoa(b.DelaySeconds))
	}

	if b.WorkHours > 0 || b.WorkMinutes > 0 || b.WorkSeconds > 0 {
		form.Set("chkRelativeLimit", "on")
		form.Set("txtValidHours", strconv.Itoa(b.WorkHours))
		form.Set("txtValidMinutes", strconv.Itoa(b.WorkMinutes))
		form.Set("txtValidSeconds", strconv.Itoa(b.WorkSeconds))
	}

	_, err := c.doPost(ctx, u, form)
	if err != nil {
		return fmt.Errorf("encx: admin create bonus: %w", err)
	}
	return nil
}

// AdminDeleteBonus deletes a bonus by its ID.
func (c *Client) AdminDeleteBonus(ctx context.Context, gameId, levelNum, bonusId int) error {
	u := fmt.Sprintf("%s/Administration/Games/BonusEdit.aspx?gid=%d&level=%d&bonus=%d&action=delete",
		c.baseURL(), gameId, levelNum, bonusId)
	_, err := c.doGet(ctx, u)
	if err != nil {
		return fmt.Errorf("encx: admin delete bonus: %w", err)
	}
	return nil
}

// --- Sector Management ---

// AdminCreateSector creates a new sector on the specified level.
func (c *Client) AdminCreateSector(ctx context.Context, gameId, levelNum int, s AdminSector) error {
	u := fmt.Sprintf("%s/Administration/Games/LevelEditor.aspx?gid=%d&level=%d", c.baseURL(), gameId, levelNum)

	form := url.Values{}
	form.Set("txtSectorName", s.Name)
	form.Set("savesector", " ")

	for i, ans := range s.Answers {
		form.Set(fmt.Sprintf("txtAnswer_%d", i), ans)
		memberID := "0"
		if s.ForMemberID != "" {
			memberID = s.ForMemberID
		}
		form.Set(fmt.Sprintf("ddlAnswerFor_%d", i), memberID)
	}

	_, err := c.doPost(ctx, u, form)
	if err != nil {
		return fmt.Errorf("encx: admin create sector: %w", err)
	}
	return nil
}

// AdminDeleteSector deletes a sector by its ID.
func (c *Client) AdminDeleteSector(ctx context.Context, gameId, levelNum, sectorId int) error {
	u := fmt.Sprintf("%s/Administration/Games/LevelEditor.aspx?gid=%d&level=%d&swanswers=1&delsector=%d",
		c.baseURL(), gameId, levelNum, sectorId)
	_, err := c.doGet(ctx, u)
	if err != nil {
		return fmt.Errorf("encx: admin delete sector: %w", err)
	}
	return nil
}

// --- Hint Management ---

// AdminCreateHint creates a new hint (regular or penalty) on the specified level.
func (c *Client) AdminCreateHint(ctx context.Context, gameId, levelNum int, h AdminHint) error {
	var u string
	if h.IsPenalty || h.RequestConfirm {
		u = fmt.Sprintf("%s/Administration/Games/PromptEdit.aspx?penalty=1&gid=%d&level=%d",
			c.baseURL(), gameId, levelNum)
	} else {
		u = fmt.Sprintf("%s/Administration/Games/PromptEdit.aspx?gid=%d&level=%d",
			c.baseURL(), gameId, levelNum)
	}

	form := url.Values{}
	form.Set("NewPrompt", h.Text)
	form.Set("NewPromptTimeoutDays", strconv.Itoa(h.Days))
	form.Set("NewPromptTimeoutHours", strconv.Itoa(h.Hours))
	form.Set("NewPromptTimeoutMinutes", strconv.Itoa(h.Minutes))
	form.Set("NewPromptTimeoutSeconds", strconv.Itoa(h.Seconds))

	if h.PenaltyHours > 0 || h.PenaltyMinutes > 0 || h.PenaltySeconds > 0 {
		form.Set("PenaltyPromptHours", strconv.Itoa(h.PenaltyHours))
		form.Set("PenaltyPromptMinutes", strconv.Itoa(h.PenaltyMinutes))
		form.Set("PenaltyPromptSeconds", strconv.Itoa(h.PenaltySeconds))
	}

	if h.ForMemberID != "" {
		form.Set("ForMemberID", h.ForMemberID)
	}

	if h.PenaltyComment != "" {
		form.Set("txtPenaltyComment", h.PenaltyComment)
	}

	if h.RequestConfirm {
		form.Set("chkRequestPenaltyConfirm", "on")
	}

	_, err := c.doPost(ctx, u, form)
	if err != nil {
		return fmt.Errorf("encx: admin create hint: %w", err)
	}
	return nil
}

// AdminDeleteHint deletes a hint by its ID.
func (c *Client) AdminDeleteHint(ctx context.Context, gameId, levelNum, hintId int) error {
	u := fmt.Sprintf("%s/Administration/Games/PromptEdit.aspx?gid=%d&level=%d&prid=%d&action=PromptDelete",
		c.baseURL(), gameId, levelNum, hintId)
	_, err := c.doGet(ctx, u)
	if err != nil {
		return fmt.Errorf("encx: admin delete hint: %w", err)
	}
	return nil
}

// --- Task Management ---

// AdminCreateTask creates a new task on the specified level.
func (c *Client) AdminCreateTask(ctx context.Context, gameId, levelNum int, t AdminTask) error {
	u := fmt.Sprintf("%s/Administration/Games/TaskEdit.aspx?gid=%d&level=%d", c.baseURL(), gameId, levelNum)

	form := url.Values{}
	form.Set("inputTask", t.Text)
	form.Set("forMemberID", t.ForMemberID)

	if t.ReplaceNl {
		form.Set("chkReplaceNlToBr", "on")
	}

	_, err := c.doPost(ctx, u, form)
	if err != nil {
		return fmt.Errorf("encx: admin create task: %w", err)
	}
	return nil
}

// --- Comment Management ---

// AdminUpdateComment updates the level name and comment.
func (c *Client) AdminUpdateComment(ctx context.Context, gameId, levelNum int, name, comment string) error {
	u := fmt.Sprintf("%s/Administration/Games/NameCommentEdit.aspx?gid=%d&level=%d", c.baseURL(), gameId, levelNum)

	form := url.Values{}
	form.Set("txtLevelName", name)
	form.Set("txtLevelComment", comment)

	_, err := c.doPost(ctx, u, form)
	if err != nil {
		return fmt.Errorf("encx: admin update comment: %w", err)
	}
	return nil
}

// --- Team Management ---

// AdminGetTeams fetches the list of teams registered for the game.
func (c *Client) AdminGetTeams(ctx context.Context, gameId, levelNum int) ([]AdminTeam, error) {
	u := fmt.Sprintf("%s/Administration/Games/TaskEdit.aspx?gid=%d&level=%d", c.baseURL(), gameId, levelNum)
	body, err := c.doGet(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("encx: admin get teams: %w", err)
	}

	// Extract the forMemberID select block
	selectStart := strings.Index(body, `id="forMemberID"`)
	if selectStart < 0 {
		selectStart = strings.Index(body, `name="forMemberID"`)
	}
	if selectStart < 0 {
		return nil, nil
	}
	selectEnd := strings.Index(body[selectStart:], "</select>")
	if selectEnd < 0 {
		return nil, nil
	}
	selectBlock := body[selectStart : selectStart+selectEnd]

	matches := adminTeamOptionRe.FindAllStringSubmatch(selectBlock, -1)
	teams := make([]AdminTeam, 0, len(matches))
	for _, m := range matches {
		teams = append(teams, AdminTeam{
			ID:   m[1],
			Name: m[2],
		})
	}

	return teams, nil
}

// --- Bonus/Penalty Time Corrections ---

// AdminGetCorrections fetches the list of bonus/penalty time corrections for a game.
func (c *Client) AdminGetCorrections(ctx context.Context, gameId int) ([]AdminCorrection, error) {
	u := fmt.Sprintf("%s/GameBonusPenaltyTime.aspx?gid=%d&lang=ru", c.baseURL(), gameId)
	body, err := c.doGet(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("encx: admin get corrections: %w", err)
	}

	rows := adminCorrectionRe.FindAllStringSubmatch(body, -1)
	corrections := make([]AdminCorrection, 0, len(rows))
	for _, row := range rows {
		tds := adminTdRe.FindAllStringSubmatch(row[1], -1)
		if len(tds) < 6 {
			continue
		}

		// Extract correction ID from the link href
		id := ""
		if href := adminHrefRe.FindStringSubmatch(row[1]); href != nil {
			if idx := strings.Index(href[1], "correct="); idx >= 0 {
				val := href[1][idx+8:]
				if ampIdx := strings.Index(val, "&"); ampIdx >= 0 {
					val = val[:ampIdx]
				}
				id = val
			}
		}

		// Extract text from td cells, stripping HTML
		getText := func(s string) string {
			if m := adminATextRe.FindStringSubmatch(s); m != nil {
				return strings.TrimSpace(m[1])
			}
			return strings.TrimSpace(stripTags(s))
		}

		corrections = append(corrections, AdminCorrection{
			ID:       id,
			DateTime: getText(tds[0][1]),
			Team:     getText(tds[1][1]),
			Level:    getText(tds[2][1]),
			Reason:   getText(tds[3][1]),
			Time:     getText(tds[4][1]),
			Comment:  getText(tds[5][1]),
		})
	}

	return corrections, nil
}

// AdminAddCorrection adds a new bonus/penalty time correction.
func (c *Client) AdminAddCorrection(ctx context.Context, gameId int, corr AdminCorrectionAdd) error {
	// First get the form to resolve team/level names to IDs
	formURL := fmt.Sprintf("%s/GameBonusPenaltyTime.aspx?gid=%d&action=add", c.baseURL(), gameId)
	body, err := c.doGet(ctx, formURL)
	if err != nil {
		return fmt.Errorf("encx: admin get correction form: %w", err)
	}

	// Find team ID by name
	teamID := resolveOptionValue(body, "ddlEditCorrectionPlayers", corr.TeamName)
	if teamID == "" {
		return fmt.Errorf("encx: team %q not found", corr.TeamName)
	}

	// Find level ID by name
	levelID := "0"
	if corr.LevelName != "0" && corr.LevelName != "" {
		levelID = resolveOptionValue(body, "ddlEditCorrectionLevels", corr.LevelName)
		if levelID == "" {
			return fmt.Errorf("encx: level %q not found", corr.LevelName)
		}
	}

	submitURL := fmt.Sprintf("%s/GameBonusPenaltyTime.aspx?gid=%d&action=save", c.baseURL(), gameId)
	form := url.Values{}
	form.Set("radioCorrectionType", corr.CorrectionType)
	form.Set("ddlEditCorrectionPlayers", teamID)
	form.Set("ddlEditCorrectionLevels", levelID)
	form.Set("DaysList", corr.Days)
	form.Set("HoursList", corr.Hours)
	form.Set("MinutesList", corr.Minutes)
	form.Set("SecondsList", corr.Seconds)
	form.Set("txtEditCorrectionComment", corr.Comment)

	_, err = c.doPost(ctx, submitURL, form)
	if err != nil {
		return fmt.Errorf("encx: admin add correction: %w", err)
	}
	return nil
}

// AdminDeleteCorrection deletes a bonus/penalty time correction by its ID.
func (c *Client) AdminDeleteCorrection(ctx context.Context, gameId int, correctionId string) error {
	u := fmt.Sprintf("%s/GameBonusPenaltyTime.aspx?gid=%d&action=delete&correct=%s",
		c.baseURL(), gameId, correctionId)
	_, err := c.doGet(ctx, u)
	if err != nil {
		return fmt.Errorf("encx: admin delete correction: %w", err)
	}
	return nil
}

// --- Helpers ---

// stripTags removes HTML tags from a string.
func stripTags(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// resolveOptionValue finds the value attribute for an option with matching text
// inside a select element with the given name.
func resolveOptionValue(html, selectName, optionText string) string {
	// Find the select block
	nameAttr := fmt.Sprintf(`name="%s"`, selectName)
	idx := strings.Index(html, nameAttr)
	if idx < 0 {
		return ""
	}
	endIdx := strings.Index(html[idx:], "</select>")
	if endIdx < 0 {
		return ""
	}
	block := html[idx : idx+endIdx]

	// Find matching option
	optRe := regexp.MustCompile(`<option\s+value="([^"]*)"[^>]*>` + regexp.QuoteMeta(optionText) + `</option>`)
	if m := optRe.FindStringSubmatch(block); m != nil {
		return m[1]
	}
	return ""
}
