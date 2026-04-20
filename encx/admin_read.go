package encx

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Regex patterns for reading admin data from HTML.
var (
	adminInputValueRe = regexp.MustCompile(`(?i)<input[^>]*name="([^"]*)"[^>]*value="([^"]*)"`)
	adminInputCheckRe = regexp.MustCompile(`(?i)<input[^>]*name="([^"]*)"[^>]*checked`)
	adminTextareaRe   = regexp.MustCompile(`(?i)<textarea[^>]*name="([^"]*)"[^>]*>([\s\S]*?)</textarea>`)
	adminSelectValRe  = regexp.MustCompile(`(?i)<option[^>]*selected[^>]*value="([^"]*)"`)
	adminAnswerRe     = regexp.MustCompile(`(?i)<input[^>]*name="((?:answer_-?\d+|txtAnswer_?\d*))"[^>]*value="([^"]*)"`)
)

// AdminGetLevelSettings reads the level settings (autopass, answer block) from the admin panel.
func (c *Client) AdminGetLevelSettings(ctx context.Context, gameId, levelNum int) (*AdminLevelSettings, error) {
	u := fmt.Sprintf("%s/Administration/Games/LevelEditor.aspx?gid=%d&level=%d", c.baseURL(), gameId, levelNum)
	body, err := c.doGet(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("encx: admin get level settings: %w", err)
	}

	s := &AdminLevelSettings{}
	inputs := adminInputValueRe.FindAllStringSubmatch(body, -1)
	for _, m := range inputs {
		name, val := m[1], m[2]
		switch name {
		case "txtApHours":
			s.AutopassHours, _ = strconv.Atoi(val)
		case "txtApMinutes":
			s.AutopassMinutes, _ = strconv.Atoi(val)
		case "txtApSeconds":
			s.AutopassSeconds, _ = strconv.Atoi(val)
		case "txtApPenaltyHours":
			s.PenaltyHours, _ = strconv.Atoi(val)
		case "txtApPenaltyMinutes":
			s.PenaltyMinutes, _ = strconv.Atoi(val)
		case "txtApPenaltySeconds":
			s.PenaltySeconds, _ = strconv.Atoi(val)
		case "txtAttemptsNumber":
			s.AttemptsNumber, _ = strconv.Atoi(val)
		case "txtAttemptsPeriodHours":
			s.AttemptsPeriodHours, _ = strconv.Atoi(val)
		case "txtAttemptsPeriodMinutes":
			s.AttemptsPeriodMinutes, _ = strconv.Atoi(val)
		case "txtAttemptsPeriodSeconds":
			s.AttemptsPeriodSeconds, _ = strconv.Atoi(val)
		}
	}

	// Check for checked inputs
	checks := adminInputCheckRe.FindAllStringSubmatch(body, -1)
	for _, m := range checks {
		switch m[1] {
		case "chkTimeoutPenalty":
			s.TimeoutPenalty = true
		case "rbApplyForPlayer":
			s.ApplyForPlayer = 1
		}
	}

	return s, nil
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
	inputs := adminInputValueRe.FindAllStringSubmatch(body, -1)
	for _, m := range inputs {
		name, val := m[1], m[2]
		switch {
		case name == "txtBonusName":
			b.Name = val
		case name == "txtValidFrom":
			b.ValidFrom = val
		case name == "txtValidTo":
			b.ValidTo = val
		case name == "txtDelayHours":
			b.DelayHours, _ = strconv.Atoi(val)
		case name == "txtDelayMinutes":
			b.DelayMinutes, _ = strconv.Atoi(val)
		case name == "txtDelaySeconds":
			b.DelaySeconds, _ = strconv.Atoi(val)
		case name == "txtValidHours":
			b.WorkHours, _ = strconv.Atoi(val)
		case name == "txtValidMinutes":
			b.WorkMinutes, _ = strconv.Atoi(val)
		case name == "txtValidSeconds":
			b.WorkSeconds, _ = strconv.Atoi(val)
		case name == "txtHours":
			b.AwardHours, _ = strconv.Atoi(val)
		case name == "txtMinutes":
			b.AwardMinutes, _ = strconv.Atoi(val)
		case name == "txtSeconds":
			b.AwardSeconds, _ = strconv.Atoi(val)
		case strings.Contains(name, "nswer_"):
			b.Answers = append(b.Answers, val)
		case strings.HasPrefix(name, "level_"):
			id, _ := strconv.Atoi(name[6:])
			b.LevelID = id
		}
	}

	// Check negative
	negRe := regexp.MustCompile(`(?i)name="negative"[^>]*checked`)
	if negRe.MatchString(body) {
		b.Negative = true
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
func (c *Client) AdminGetHint(ctx context.Context, gameId, levelNum, hintId int) (*AdminHint, error) {
	u := fmt.Sprintf("%s/Administration/Games/PromptEdit.aspx?action=PromptEdit&gid=%d&level=%d&prid=%d",
		c.baseURL(), gameId, levelNum, hintId)
	body, err := c.doGet(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("encx: admin get hint: %w", err)
	}

	h := &AdminHint{}
	inputs := adminInputValueRe.FindAllStringSubmatch(body, -1)
	for _, m := range inputs {
		name, val := m[1], m[2]
		switch name {
		case "NewPromptTimeoutDays":
			h.Days, _ = strconv.Atoi(val)
		case "NewPromptTimeoutHours":
			h.Hours, _ = strconv.Atoi(val)
		case "NewPromptTimeoutMinutes":
			h.Minutes, _ = strconv.Atoi(val)
		case "NewPromptTimeoutSeconds":
			h.Seconds, _ = strconv.Atoi(val)
		case "PenaltyPromptHours":
			h.PenaltyHours, _ = strconv.Atoi(val)
		case "PenaltyPromptMinutes":
			h.PenaltyMinutes, _ = strconv.Atoi(val)
		case "PenaltyPromptSeconds":
			h.PenaltySeconds, _ = strconv.Atoi(val)
		}
	}

	// Textarea for hint text
	textareas := adminTextareaRe.FindAllStringSubmatch(body, -1)
	for _, m := range textareas {
		if m[1] == "NewPrompt" || m[1] == "" {
			h.Text = m[2]
		}
	}
	// Fallback: some versions use a plain textarea without name
	if h.Text == "" {
		plainRe := regexp.MustCompile(`(?i)<textarea[^>]*>([\s\S]*?)</textarea>`)
		if m := plainRe.FindStringSubmatch(body); m != nil {
			h.Text = m[1]
		}
	}

	// Check if penalty hint
	if strings.Contains(body, "penalty=1") || h.PenaltyHours > 0 || h.PenaltyMinutes > 0 || h.PenaltySeconds > 0 {
		h.IsPenalty = true
	}

	// ForMemberID
	forMemberRe := regexp.MustCompile(`(?i)<select[^>]*>[\s\S]*?<option[^>]*selected[^>]*value="([^"]*)"`)
	if m := forMemberRe.FindStringSubmatch(body); m != nil && m[1] != "0" {
		h.ForMemberID = m[1]
	}

	return h, nil
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
		case "txtLevelName":
			name = m[2]
		}
	}

	// Level name might be in an input field
	inputs := adminInputValueRe.FindAllStringSubmatch(body, -1)
	for _, m := range inputs {
		if m[1] == "txtLevelName" {
			name = m[2]
		}
	}

	return name, comment, nil
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

		// Fetch answers for this sector
		ansURL := fmt.Sprintf("%s/ALoader/LevelInfo.aspx?gid=%d&level=%d&object=3&sector=%s",
			c.baseURL(), gameId, levelNum, sectorVal)
		ansBody, err := c.doGet(ctx, ansURL)
		if err != nil {
			continue
		}

		answers := parseAnswerInputs(ansBody)
		sectors = append(sectors, AdminSector{
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

	answers := parseAnswerInputs(body)
	if len(answers) == 0 {
		return nil, nil
	}

	return []AdminSector{{Name: "", Answers: answers}}, nil
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

	// 4. Copy each level's content
	for i, sl := range srcLevels {
		if i >= len(dstLevels) {
			break
		}
		lvlNum := sl.Number
		dstNum := dstLevels[i].Number
		progress(fmt.Sprintf("Copying level %d/%d: %s", lvlNum, len(srcLevels), sl.Name))

		// Copy level settings
		settings, err := c.AdminGetLevelSettings(ctx, srcGameId, lvlNum)
		if err == nil && settings != nil {
			c.AdminUpdateAutopass(ctx, dstGameId, dstNum, *settings)
			c.AdminUpdateAnswerBlock(ctx, dstGameId, dstNum, *settings)
		}

		// Copy comment
		name, comment, err := c.AdminGetComment(ctx, srcGameId, lvlNum)
		if err == nil {
			c.AdminUpdateComment(ctx, dstGameId, dstNum, name, comment)
		}

		// Copy bonuses
		bonusIds, err := c.AdminGetBonusIds(ctx, srcGameId, lvlNum)
		if err == nil {
			for _, bid := range bonusIds {
				bonus, err := c.AdminGetBonus(ctx, srcGameId, lvlNum, bid)
				if err != nil {
					continue
				}
				// Remap level ID to target
				if i < len(dstLevels) {
					bonus.LevelID = dstLevels[i].ID
				}
				c.AdminCreateBonus(ctx, dstGameId, dstNum, *bonus)
			}
			if len(bonusIds) > 0 {
				progress(fmt.Sprintf("  %d bonuses copied", len(bonusIds)))
			}
		}

		// Copy sectors
		sectors, err := c.AdminGetSectorAnswers(ctx, srcGameId, lvlNum)
		if err == nil {
			for _, sec := range sectors {
				c.AdminCreateSector(ctx, dstGameId, dstNum, sec)
			}
			if len(sectors) > 0 {
				progress(fmt.Sprintf("  %d sectors copied", len(sectors)))
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
			}
			if len(hintIds) > 0 {
				progress(fmt.Sprintf("  %d hints copied", len(hintIds)))
			}
		}
	}

	progress("Copy complete")
	return nil
}
