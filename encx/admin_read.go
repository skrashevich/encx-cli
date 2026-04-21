package encx

import (
	"context"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Regex patterns for reading admin data from HTML.
var (
	adminInputDisabledRe = regexp.MustCompile(`(?i)disabled`)
	adminTextareaRe      = regexp.MustCompile(`(?i)<textarea[^>]*name="([^"]*)"[^>]*>([\s\S]*?)</textarea>`)
	adminTaskTextareaRe  = regexp.MustCompile(`(?i)<textarea[^>]*>([\s\S]*?)</textarea>`)
	adminCheckedLevelRe  = regexp.MustCompile(`(?i)<input[^>]*name="(level_\d+)"[^>]*checked`)
	adminRbAllLevelsRe   = regexp.MustCompile(`(?i)<input[^>]*name="rbAllLevels"[^>]*checked`)
)

// parseEnabledInputs extracts name/value pairs from enabled (not disabled) input elements.
func parseEnabledInputs(body string) map[string]string {
	result := make(map[string]string)
	// Find all input tags
	inputTagRe := regexp.MustCompile(`(?i)<input[^>]*>`)
	tags := inputTagRe.FindAllString(body, -1)
	nameRe := regexp.MustCompile(`(?i)name="([^"]*)"`)
	valueRe := regexp.MustCompile(`(?i)value="([^"]*)"`)

	for _, tag := range tags {
		// Skip disabled inputs
		if adminInputDisabledRe.MatchString(tag) {
			continue
		}
		nameM := nameRe.FindStringSubmatch(tag)
		valueM := valueRe.FindStringSubmatch(tag)
		if nameM != nil && valueM != nil {
			result[nameM[1]] = valueM[1]
		}
	}
	return result
}

// parseCheckedInputs returns names of checked (but not disabled) inputs.
func parseCheckedInputs(body string) map[string]bool {
	result := make(map[string]bool)
	inputTagRe := regexp.MustCompile(`(?i)<input[^>]*>`)
	tags := inputTagRe.FindAllString(body, -1)
	nameRe := regexp.MustCompile(`(?i)name="([^"]*)"`)
	checkedRe := regexp.MustCompile(`(?i)checked`)

	for _, tag := range tags {
		if adminInputDisabledRe.MatchString(tag) {
			continue
		}
		if !checkedRe.MatchString(tag) {
			continue
		}
		nameM := nameRe.FindStringSubmatch(tag)
		if nameM != nil {
			result[nameM[1]] = true
		}
	}
	return result
}

// AdminGetLevelSettings reads the level settings (autopass, answer block) from the admin panel.
func (c *Client) AdminGetLevelSettings(ctx context.Context, gameId, levelNum int) (*AdminLevelSettings, error) {
	u := fmt.Sprintf("%s/Administration/Games/LevelEditor.aspx?gid=%d&level=%d", c.baseURL(), gameId, levelNum)
	body, err := c.doGet(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("encx: admin get level settings: %w", err)
	}

	s := &AdminLevelSettings{}
	inputs := parseEnabledInputs(body)
	checked := parseCheckedInputs(body)

	// Try input fields first (may not exist if form is collapsed)
	s.AutopassHours, _ = strconv.Atoi(inputs["txtApHours"])
	s.AutopassMinutes, _ = strconv.Atoi(inputs["txtApMinutes"])
	s.AutopassSeconds, _ = strconv.Atoi(inputs["txtApSeconds"])
	s.PenaltyHours, _ = strconv.Atoi(inputs["txtApPenaltyHours"])
	s.PenaltyMinutes, _ = strconv.Atoi(inputs["txtApPenaltyMinutes"])
	s.PenaltySeconds, _ = strconv.Atoi(inputs["txtApPenaltySeconds"])

	// If no input fields, parse autopass from displayed text like "через 30 минут" or "1 час 30 минут, 15 минут штрафа"
	if s.AutopassHours == 0 && s.AutopassMinutes == 0 && s.AutopassSeconds == 0 {
		s.AutopassHours, s.AutopassMinutes, s.AutopassSeconds = parseAutopassText(body)
		// Check for penalty part (after comma)
		penaltyRe := regexp.MustCompile(`(?i)lnkAdjustAutopass[^>]*>[^<]*,\s*([^<]+)`)
		if m := penaltyRe.FindStringSubmatch(body); m != nil {
			ph, pm, ps := parseTimeText(m[1])
			if ph > 0 || pm > 0 || ps > 0 {
				s.TimeoutPenalty = true
				s.PenaltyHours = ph
				s.PenaltyMinutes = pm
				s.PenaltySeconds = ps
			}
		}
	}

	s.AttemptsNumber, _ = strconv.Atoi(inputs["txtAttemptsNumber"])
	s.AttemptsPeriodHours, _ = strconv.Atoi(inputs["txtAttemptsPeriodHours"])
	s.AttemptsPeriodMinutes, _ = strconv.Atoi(inputs["txtAttemptsPeriodMinutes"])
	s.AttemptsPeriodSeconds, _ = strconv.Atoi(inputs["txtAttemptsPeriodSeconds"])

	if checked["chkTimeoutPenalty"] {
		s.TimeoutPenalty = true
	}
	if checked["rbApplyForPlayer"] {
		s.ApplyForPlayer = 1
	}

	return s, nil
}

// parseAutopassText extracts autopass time from the lnkAdjustAutopass link text.
func parseAutopassText(body string) (h, m, s int) {
	linkRe := regexp.MustCompile(`(?i)id="lnkAdjustAutopass"[^>]*>([^<]+)`)
	match := linkRe.FindStringSubmatch(body)
	if match == nil {
		return 0, 0, 0
	}
	text := match[1]
	// Split on comma (first part = autopass, second part = penalty)
	parts := strings.SplitN(text, ",", 2)
	return parseTimeText(parts[0])
}

// parseTimeText extracts hours, minutes, seconds from text like "1 час 30 минут" or "30 минут".
func parseTimeText(text string) (h, m, s int) {
	hourRe := regexp.MustCompile(`(\d+)\s*час`)
	minRe := regexp.MustCompile(`(\d+)\s*мин`)
	secRe := regexp.MustCompile(`(\d+)\s*сек`)

	if match := hourRe.FindStringSubmatch(text); match != nil {
		h, _ = strconv.Atoi(match[1])
	}
	if match := minRe.FindStringSubmatch(text); match != nil {
		m, _ = strconv.Atoi(match[1])
	}
	if match := secRe.FindStringSubmatch(text); match != nil {
		s, _ = strconv.Atoi(match[1])
	}
	return
}

// AdminGetBonusIds returns the list of bonus IDs on a level.
func (c *Client) AdminGetBonusIds(ctx context.Context, gameId, levelNum int) ([]int, error) {
	u := fmt.Sprintf("%s/Administration/Games/LevelEditor.aspx?level=%d&gid=%d", c.baseURL(), levelNum, gameId)
	body, err := c.doGet(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("encx: admin get bonus ids: %w", err)
	}

	matches := adminBonusIdRe.FindAllStringSubmatch(body, -1)
	ids := make([]int, 0, len(matches))
	for _, m := range matches {
		id, _ := strconv.Atoi(m[1])
		if id > 0 {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// AdminGetBonus reads a bonus details from the admin panel.
func (c *Client) AdminGetBonus(ctx context.Context, gameId, levelNum, bonusId int) (*AdminBonus, error) {
	u := fmt.Sprintf("%s/Administration/Games/BonusEdit.aspx?gid=%d&level=%d&bonus=%d&action=edit",
		c.baseURL(), gameId, levelNum, bonusId)
	body, err := c.doGet(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("encx: admin get bonus: %w", err)
	}

	b := &AdminBonus{}
	inputs := parseEnabledInputs(body)
	checked := parseCheckedInputs(body)

	b.Name = inputs["txtBonusName"]
	b.AwardHours, _ = strconv.Atoi(inputs["txtHours"])
	b.AwardMinutes, _ = strconv.Atoi(inputs["txtMinutes"])
	b.AwardSeconds, _ = strconv.Atoi(inputs["txtSeconds"])

	// Only read time limits if their checkboxes are checked
	if checked["chkAbsoluteLimit"] {
		b.ValidFrom = inputs["txtValidFrom"]
		b.ValidTo = inputs["txtValidTo"]
	}
	if checked["chkDelay"] {
		b.DelayHours, _ = strconv.Atoi(inputs["txtDelayHours"])
		b.DelayMinutes, _ = strconv.Atoi(inputs["txtDelayMinutes"])
		b.DelaySeconds, _ = strconv.Atoi(inputs["txtDelaySeconds"])
	}
	if checked["chkRelativeLimit"] {
		b.WorkHours, _ = strconv.Atoi(inputs["txtValidHours"])
		b.WorkMinutes, _ = strconv.Atoi(inputs["txtValidMinutes"])
		b.WorkSeconds, _ = strconv.Atoi(inputs["txtValidSeconds"])
	}
	if checked["negative"] {
		b.Negative = true
	}

	// Answers
	for key, val := range inputs {
		if strings.Contains(key, "nswer_") && val != "" {
			b.Answers = append(b.Answers, val)
		}
	}

	// Which levels this bonus applies to
	// Check if rbAllLevels is checked (bonus for all levels)
	if adminRbAllLevelsRe.MatchString(body) {
		b.LevelID = -1 // sentinel: means "all levels"
	} else {
		// Find checked level
		if m := adminCheckedLevelRe.FindStringSubmatch(body); m != nil {
			idStr := strings.TrimPrefix(m[1], "level_")
			b.LevelID, _ = strconv.Atoi(idStr)
		}
	}

	// Textareas (task, help)
	textareas := adminTextareaRe.FindAllStringSubmatch(body, -1)
	for _, m := range textareas {
		switch m[1] {
		case "txtTask":
			b.Task = m[2]
		case "txtHelp":
			b.Hint = m[2]
		}
	}

	// BonusFor dropdown
	bonusForRe := regexp.MustCompile(`(?i)<select[^>]*id="ddlBonusFor"[^>]*>[\s\S]*?<option[^>]*selected[^>]*value="([^"]*)"`)
	if m := bonusForRe.FindStringSubmatch(body); m != nil {
		b.BonusFor = m[1]
	}

	return b, nil
}

// AdminGetHintIds returns the list of hint IDs on a level.
func (c *Client) AdminGetHintIds(ctx context.Context, gameId, levelNum int) ([]int, error) {
	u := fmt.Sprintf("%s/Administration/Games/LevelEditor.aspx?level=%d&gid=%d", c.baseURL(), levelNum, gameId)
	body, err := c.doGet(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("encx: admin get hint ids: %w", err)
	}

	matches := adminHintIdRe.FindAllStringSubmatch(body, -1)
	seen := map[int]bool{}
	ids := make([]int, 0, len(matches))
	for _, m := range matches {
		id, _ := strconv.Atoi(m[1])
		if id > 0 && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// AdminGetHint reads a hint's details from the admin panel.
// It tries without penalty=1 first, then retries with penalty=1 if text is empty (penalty hints require it).
func (c *Client) AdminGetHint(ctx context.Context, gameId, levelNum, hintId int) (*AdminHint, error) {
	h, err := c.adminGetHintFromURL(ctx, gameId, levelNum, hintId, false)
	if err != nil {
		return nil, err
	}
	// If no text found, it might be a penalty hint — retry with penalty=1
	if h.Text == "" {
		h2, err := c.adminGetHintFromURL(ctx, gameId, levelNum, hintId, true)
		if err == nil && h2.Text != "" {
			return h2, nil
		}
	}
	return h, nil
}

func (c *Client) adminGetHintFromURL(ctx context.Context, gameId, levelNum, hintId int, penalty bool) (*AdminHint, error) {
	var u string
	if penalty {
		u = fmt.Sprintf("%s/Administration/Games/PromptEdit.aspx?penalty=1&action=PromptEdit&gid=%d&level=%d&prid=%d",
			c.baseURL(), gameId, levelNum, hintId)
	} else {
		u = fmt.Sprintf("%s/Administration/Games/PromptEdit.aspx?action=PromptEdit&gid=%d&level=%d&prid=%d",
			c.baseURL(), gameId, levelNum, hintId)
	}
	body, err := c.doGet(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("encx: admin get hint: %w", err)
	}

	h := &AdminHint{}
	inputs := parseEnabledInputs(body)

	h.Days, _ = strconv.Atoi(inputs["NewPromptTimeoutDays"])
	h.Hours, _ = strconv.Atoi(inputs["NewPromptTimeoutHours"])
	h.Minutes, _ = strconv.Atoi(inputs["NewPromptTimeoutMinutes"])
	h.Seconds, _ = strconv.Atoi(inputs["NewPromptTimeoutSeconds"])
	h.PenaltyHours, _ = strconv.Atoi(inputs["PenaltyPromptHours"])
	h.PenaltyMinutes, _ = strconv.Atoi(inputs["PenaltyPromptMinutes"])
	h.PenaltySeconds, _ = strconv.Atoi(inputs["PenaltyPromptSeconds"])

	// Hint text from textarea
	textareas := adminTextareaRe.FindAllStringSubmatch(body, -1)
	for _, m := range textareas {
		if m[1] == "NewPrompt" {
			h.Text = m[2]
		}
		if m[1] == "txtPenaltyComment" {
			h.PenaltyComment = m[2]
		}
	}

	// Check if penalty hint
	if penalty || h.PenaltyHours > 0 || h.PenaltyMinutes > 0 || h.PenaltySeconds > 0 {
		h.IsPenalty = true
	}

	// Check chkRequestPenaltyConfirm
	checked := parseCheckedInputs(body)
	if checked["chkRequestPenaltyConfirm"] {
		h.RequestConfirm = true
		h.IsPenalty = true
	}

	// ForMemberID from select
	forMemberRe := regexp.MustCompile(`(?i)<select[^>]*name="ForMemberID"[^>]*>[\s\S]*?<option[^>]*selected[^>]*value="([^"]*)"`)
	if m := forMemberRe.FindStringSubmatch(body); m != nil && m[1] != "0" {
		h.ForMemberID = m[1]
	}

	return h, nil
}

// AdminGetTaskIds returns task IDs for a level from the admin panel.
func (c *Client) AdminGetTaskIds(ctx context.Context, gameId, levelNum int) ([]int, error) {
	u := fmt.Sprintf("%s/Administration/Games/LevelEditor.aspx?level=%d&gid=%d", c.baseURL(), levelNum, gameId)
	body, err := c.doGet(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("encx: admin get task ids: %w", err)
	}

	taskIdRe := regexp.MustCompile(`(?i)tid=(\d+)`)
	matches := taskIdRe.FindAllStringSubmatch(body, -1)
	seen := map[int]bool{}
	ids := make([]int, 0, len(matches))
	for _, m := range matches {
		id, _ := strconv.Atoi(m[1])
		if id > 0 && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// AdminGetTask reads task details from the admin panel.
func (c *Client) AdminGetTask(ctx context.Context, gameId, levelNum, taskId int) (*AdminTask, error) {
	u := fmt.Sprintf("%s/Administration/Games/TaskEdit.aspx?action=TaskEdit&gid=%d&level=%d&tid=%d",
		c.baseURL(), gameId, levelNum, taskId)
	body, err := c.doGet(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("encx: admin get task: %w", err)
	}

	t := &AdminTask{}

	// Task text from textarea (HTML-encoded in source, decode for round-tripping)
	if m := adminTaskTextareaRe.FindStringSubmatch(body); m != nil {
		t.Text = html.UnescapeString(m[1])
	}

	// ReplaceNlToBr checkbox
	checked := parseCheckedInputs(body)
	if checked["chkReplaceNlToBr"] {
		t.ReplaceNl = true
	}

	// ForMemberID from select
	forMemberRe := regexp.MustCompile(`(?i)<select[^>]*id="forMemberID"[^>]*>[\s\S]*?<option[^>]*selected[^>]*value="([^"]*)"`)
	if m := forMemberRe.FindStringSubmatch(body); m != nil && m[1] != "0" {
		t.ForMemberID = m[1]
	}

	return t, nil
}

// AdminGetComment reads the level name and comment from the admin panel.
func (c *Client) AdminGetComment(ctx context.Context, gameId, levelNum int) (name, comment string, err error) {
	u := fmt.Sprintf("%s/Administration/Games/NameCommentEdit.aspx?gid=%d&level=%d", c.baseURL(), gameId, levelNum)
	body, err := c.doGet(ctx, u)
	if err != nil {
		return "", "", fmt.Errorf("encx: admin get comment: %w", err)
	}

	textareas := adminTextareaRe.FindAllStringSubmatch(body, -1)
	for _, m := range textareas {
		switch m[1] {
		case "txtLevelComment":
			comment = m[2]
		}
	}

	// Level name from input
	inputs := parseEnabledInputs(body)
	if v, ok := inputs["txtLevelName"]; ok {
		name = v
	}

	return name, comment, nil
}

// AdminGetMessageIds returns message IDs for a level from the admin panel.
func (c *Client) AdminGetMessageIds(ctx context.Context, gameId, levelNum int) ([]int, error) {
	u := fmt.Sprintf("%s/Administration/Games/LevelEditor.aspx?level=%d&gid=%d", c.baseURL(), levelNum, gameId)
	body, err := c.doGet(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("encx: admin get message ids: %w", err)
	}

	matches := adminMessageIdRe.FindAllStringSubmatch(body, -1)
	seen := map[int]bool{}
	ids := make([]int, 0, len(matches))
	for _, m := range matches {
		id, _ := strconv.Atoi(m[1])
		if id > 0 && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// AdminGetMessage reads message details from the admin panel.
func (c *Client) AdminGetMessage(ctx context.Context, gameId, levelNum, messageId int) (*AdminGameMessage, error) {
	u := fmt.Sprintf("%s/Administration/Games/MessageEdit.aspx?gid=%d&level=%d&mid=%d", c.baseURL(), gameId, levelNum, messageId)
	body, err := c.doGet(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("encx: admin get message: %w", err)
	}

	msg := &AdminGameMessage{ID: messageId}
	inputs := parseEnabledInputs(body)
	checked := parseCheckedInputs(body)

	textareas := adminTextareaRe.FindAllStringSubmatch(body, -1)
	for _, m := range textareas {
		if m[1] == "txtMessage" {
			msg.Text = html.UnescapeString(m[2])
		}
	}

	if checked["chkReplaceNlToBr"] {
		msg.ReplaceNlToBr = true
	}
	if v := parseSelectedRadioOrValue(body, "rbShowOnLevels", inputs); v != "" {
		msg.ShowOnLevelsMode, _ = strconv.Atoi(v)
	}
	msg.RequiredPoints = inputs["txtRequiredPoints"]

	for key, on := range checked {
		if !on || !strings.HasPrefix(key, "lvl_") {
			continue
		}
		id, err := strconv.Atoi(strings.TrimPrefix(key, "lvl_"))
		if err == nil && id > 0 {
			msg.LevelIDs = append(msg.LevelIDs, id)
		}
	}

	return msg, nil
}

// AdminGetSectorAnswers reads sector answers from the ALoader endpoint.
func (c *Client) AdminGetSectorAnswers(ctx context.Context, gameId, levelNum int) ([]AdminSector, error) {
	u := fmt.Sprintf("%s/ALoader/LevelInfo.aspx?gid=%d&level=%d&object=3", c.baseURL(), gameId, levelNum)
	body, err := c.doGet(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("encx: admin get sectors: %w", err)
	}

	// Parse sector options
	optRe := regexp.MustCompile(`(?i)<option\s+value="([^"]*)"[^>]*>([^<]*)</option>`)
	opts := optRe.FindAllStringSubmatch(body, -1)

	if len(opts) == 0 {
		// No named sectors — try reading direct answers from object=2
		return c.adminGetDirectAnswers(ctx, gameId, levelNum)
	}

	sectors := make([]AdminSector, 0, len(opts))
	for _, opt := range opts {
		sectorVal := opt[1]
		sectorName := opt[2]
		sectorID, _ := strconv.Atoi(sectorVal)

		// Fetch answers for this sector
		ansURL := fmt.Sprintf("%s/ALoader/LevelInfo.aspx?gid=%d&level=%d&object=3&sector=%s",
			c.baseURL(), gameId, levelNum, sectorVal)
		ansBody, err := c.doGet(ctx, ansURL)
		if err != nil {
			continue
		}

		answers := parseAnswerInputs(ansBody)
		sectors = append(sectors, AdminSector{
			ID:      sectorID,
			Name:    sectorName,
			Answers: answers,
		})
	}

	return sectors, nil
}

func (c *Client) adminGetDirectAnswers(ctx context.Context, gameId, levelNum int) ([]AdminSector, error) {
	u := fmt.Sprintf("%s/ALoader/LevelInfo.aspx?gid=%d&level=%d&object=2", c.baseURL(), gameId, levelNum)
	body, err := c.doGet(ctx, u)
	if err != nil {
		return nil, err
	}

	// Find editanswers IDs from links on this page
	editRe := regexp.MustCompile(`(?i)editanswers=(\d+)`)
	editMatches := editRe.FindAllStringSubmatch(body, -1)

	if len(editMatches) == 0 {
		// Fallback: try to parse answers directly from the page
		answers := parseAnswerInputs(body)
		if len(answers) == 0 {
			return nil, nil
		}
		return []AdminSector{{Name: "", Answers: answers}}, nil
	}

	// For each answer group, fetch the edit page to get input values
	seen := map[string]bool{}
	var directSectors []AdminSector
	for _, m := range editMatches {
		editId := m[1]
		if seen[editId] {
			continue
		}
		seen[editId] = true

		editURL := fmt.Sprintf("%s/Administration/Games/LevelEditor.aspx?level=%d&gid=%d&swanswers=1&editanswers=%s",
			c.baseURL(), levelNum, gameId, editId)
		editBody, err := c.doGet(ctx, editURL)
		if err != nil {
			continue
		}
		answers := parseAnswerInputs(editBody)
		if len(answers) == 0 {
			continue
		}
		sectorID, _ := strconv.Atoi(editId)
		directSectors = append(directSectors, AdminSector{
			ID:      sectorID,
			Answers: answers,
		})
	}

	if len(directSectors) == 0 {
		return nil, nil
	}

	return directSectors, nil
}

func parseAnswerInputs(body string) []string {
	ansRe := regexp.MustCompile(`(?i)<input[^>]*name="txtAnswer[^"]*"[^>]*value="([^"]*)"`)
	matches := ansRe.FindAllStringSubmatch(body, -1)
	answers := make([]string, 0, len(matches))
	for _, m := range matches {
		if m[1] != "" {
			answers = append(answers, m[1])
		}
	}
	return answers
}

// adminDelay pauses between admin requests to avoid rate limiting.
func adminDelay() {
	time.Sleep(1200 * time.Millisecond)
}

// AdminWipeGame completely resets a game: removes all bonuses, hints, sectors, and levels.
func (c *Client) AdminWipeGame(ctx context.Context, gameId int, progress func(string)) error {
	if progress == nil {
		progress = func(string) {}
	}

	levels, err := c.AdminGetLevels(ctx, gameId)
	if err != nil {
		return fmt.Errorf("get levels: %w", err)
	}
	if len(levels) == 0 {
		progress("Game has no levels, nothing to wipe")
		return nil
	}
	progress(fmt.Sprintf("Wiping game: %d levels", len(levels)))

	// Delete bonuses from each level (game-wide bonuses appear on all levels)
	for _, lvl := range levels {
		bonusIds, err := c.AdminGetBonusIds(ctx, gameId, lvl.Number)
		if err == nil && len(bonusIds) > 0 {
			for _, bid := range bonusIds {
				c.AdminDeleteBonus(ctx, gameId, lvl.Number, bid)
				adminDelay()
			}
			progress(fmt.Sprintf("  Level %d: %d bonuses deleted", lvl.Number, len(bonusIds)))
		}
	}

	// Delete levels in reverse order
	for i := len(levels) - 1; i >= 0; i-- {
		c.AdminDeleteLevel(ctx, gameId, levels[i].Number)
		adminDelay()
	}
	progress(fmt.Sprintf("  %d levels deleted", len(levels)))

	// Delete corrections
	corrections, err := c.AdminGetCorrections(ctx, gameId)
	if err == nil && len(corrections) > 0 {
		for _, corr := range corrections {
			if corr.ID != "" {
				c.AdminDeleteCorrection(ctx, gameId, corr.ID)
				adminDelay()
			}
		}
		progress(fmt.Sprintf("  %d corrections deleted", len(corrections)))
	}

	progress("Wipe complete")
	return nil
}

// AdminCopyGame copies all levels, settings, bonuses, sectors, hints, tasks, and comments
// from one game to another. The target game must exist (can be empty).
func (c *Client) AdminCopyGame(ctx context.Context, srcGameId, dstGameId int, progress func(string)) error {
	if progress == nil {
		progress = func(string) {}
	}

	// 1. Get source levels
	srcLevels, err := c.AdminGetLevels(ctx, srcGameId)
	if err != nil {
		return fmt.Errorf("get source levels: %w", err)
	}
	if len(srcLevels) == 0 {
		return fmt.Errorf("source game has no levels")
	}
	progress(fmt.Sprintf("Source game has %d levels", len(srcLevels)))

	// 2. Get target levels, create if needed
	dstLevels, err := c.AdminGetLevels(ctx, dstGameId)
	if err != nil {
		return fmt.Errorf("get target levels: %w", err)
	}

	if len(dstLevels) < len(srcLevels) {
		need := len(srcLevels) - len(dstLevels)
		progress(fmt.Sprintf("Creating %d levels in target game", need))
		if err := c.AdminCreateLevels(ctx, dstGameId, need); err != nil {
			return fmt.Errorf("create target levels: %w", err)
		}
		dstLevels, err = c.AdminGetLevels(ctx, dstGameId)
		if err != nil {
			return fmt.Errorf("re-read target levels: %w", err)
		}
	}

	// 3. Rename target levels to match source
	names := make(map[int]string, len(srcLevels))
	for i, sl := range srcLevels {
		if i < len(dstLevels) {
			names[dstLevels[i].ID] = sl.Name
		}
	}
	if err := c.AdminRenameLevels(ctx, dstGameId, names); err != nil {
		return fmt.Errorf("rename levels: %w", err)
	}
	progress("Levels renamed")

	// Track which bonuses are "all levels" to avoid duplicating them
	copiedAllLevelBonuses := map[string]bool{}

	// 4. Copy each level's content
	for i, sl := range srcLevels {
		if i >= len(dstLevels) {
			break
		}
		lvlNum := sl.Number
		dstNum := dstLevels[i].Number
		progress(fmt.Sprintf("Copying level %d/%d: %s", lvlNum, len(srcLevels), sl.Name))

		// Copy level settings (autopass + answer block)
		settings, err := c.AdminGetLevelSettings(ctx, srcGameId, lvlNum)
		if err == nil && settings != nil {
			if settings.AutopassHours > 0 || settings.AutopassMinutes > 0 || settings.AutopassSeconds > 0 {
				c.AdminUpdateAutopass(ctx, dstGameId, dstNum, *settings)
				adminDelay()
			}
			if settings.AttemptsNumber > 0 {
				c.AdminUpdateAnswerBlock(ctx, dstGameId, dstNum, *settings)
				adminDelay()
			}
		}

		// Copy comment
		name, comment, err := c.AdminGetComment(ctx, srcGameId, lvlNum)
		if err == nil && (name != "" || comment != "") {
			c.AdminUpdateComment(ctx, dstGameId, dstNum, name, comment)
			adminDelay()
		}

		// Copy tasks
		taskIds, err := c.AdminGetTaskIds(ctx, srcGameId, lvlNum)
		if err == nil {
			copied := 0
			for _, tid := range taskIds {
				task, err := c.AdminGetTask(ctx, srcGameId, lvlNum, tid)
				if err != nil || task.Text == "" {
					continue
				}
				c.AdminCreateTask(ctx, dstGameId, dstNum, *task)
				adminDelay()
				copied++
			}
			if copied > 0 {
				progress(fmt.Sprintf("  %d task(s) copied", copied))
			}
		}

		// Copy sectors (answers)
		sectors, err := c.AdminGetSectorAnswers(ctx, srcGameId, lvlNum)
		if err == nil {
			for _, sec := range sectors {
				if len(sec.Answers) > 0 {
					c.AdminCreateSector(ctx, dstGameId, dstNum, sec)
					adminDelay()
				}
			}
			if len(sectors) > 0 {
				progress(fmt.Sprintf("  %d sector(s) copied", len(sectors)))
			}
		}

		// Copy bonuses
		bonusIds, err := c.AdminGetBonusIds(ctx, srcGameId, lvlNum)
		if err == nil {
			copied := 0
			for _, bid := range bonusIds {
				bonus, err := c.AdminGetBonus(ctx, srcGameId, lvlNum, bid)
				if err != nil {
					continue
				}

				// Handle "all levels" bonuses - only copy once
				if bonus.LevelID == -1 {
					key := bonus.Name + "|" + strings.Join(bonus.Answers, ",")
					if copiedAllLevelBonuses[key] {
						continue // already copied this bonus
					}
					copiedAllLevelBonuses[key] = true
					// Keep LevelID as -1 so AdminCreateBonus uses rbAllLevels=1
				} else {
					// Remap level ID to target
					bonus.LevelID = dstLevels[i].ID
				}
				c.AdminCreateBonus(ctx, dstGameId, dstNum, *bonus)
				adminDelay()
				copied++
			}
			if copied > 0 {
				progress(fmt.Sprintf("  %d bonus(es) copied", copied))
			}
		}

		// Copy hints
		hintIds, err := c.AdminGetHintIds(ctx, srcGameId, lvlNum)
		if err == nil {
			for _, hid := range hintIds {
				hint, err := c.AdminGetHint(ctx, srcGameId, lvlNum, hid)
				if err != nil {
					continue
				}
				c.AdminCreateHint(ctx, dstGameId, dstNum, *hint)
				adminDelay()
			}
			if len(hintIds) > 0 {
				progress(fmt.Sprintf("  %d hint(s) copied", len(hintIds)))
			}
		}
	}

	progress("Copy complete")
	return nil
}
