package encx

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
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
	// Message link: mid=123
	adminMessageIdRe = regexp.MustCompile(`(?i)mid=(\d+)`)
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

	_, _, body, err := c.doRequestAndRead(req)
	if err != nil {
		return "", fmt.Errorf("encx: POST %s: %w", rawURL, err)
	}

	return string(body), nil
}

// IsPlausibleEncounterLogin reports whether s looks like an Encounter username.
func IsPlausibleEncounterLogin(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) < 2 || len(s) > 48 {
		return false
	}
	switch s {
	case "-", "—", "–", ".", "…", "...", "&nbsp;":
		return false
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}

// Profile represents user profile data parsed from the profile page.
type Profile struct {
	ID       int    `json:"id"`
	Login    string `json:"login"`
	Name     string `json:"name"`
	Rank     string `json:"rank"`
	Team     string `json:"team"`
	TeamID   int    `json:"team_id,omitempty"`
	Domain   string `json:"domain"`
	Points   string `json:"points"`
	Location string `json:"location,omitempty"`
}

// legacyGetProfile fetches the current user's profile from /UserDetails.aspx.
func (c *Client) legacyGetProfile(ctx context.Context) (*Profile, error) {
	u := fmt.Sprintf("%s/UserDetails.aspx", c.baseURL())
	body, err := c.doGet(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("encx: get profile: %w", err)
	}

	p := &Profile{}

	// User ID from uid= in page links
	uidRe := regexp.MustCompile(`(?i)uid=(\d+)`)
	if m := uidRe.FindStringSubmatch(body); m != nil {
		p.ID, _ = strconv.Atoi(m[1])
	}

	// Login from header link: <a ... href="/UserDetails.aspx">login</a>
	loginRe := regexp.MustCompile(`(?i)<a[^>]*href="/UserDetails\.aspx"[^>]*>([^<]+)</a>`)
	nameMatches := loginRe.FindAllStringSubmatch(body, 8)
	for _, m := range nameMatches {
		candidate := strings.TrimSpace(m[1])
		if IsPlausibleEncounterLogin(candidate) {
			p.Login = candidate
			break
		}
	}

	// Full name (usually the first non-login UserDetails link with spaces).
	for _, m := range nameMatches {
		candidate := strings.TrimSpace(m[1])
		if candidate != p.Login && strings.Contains(candidate, " ") {
			p.Name = candidate
			break
		}
	}

	// Rank appears near points, e.g. "points / <b>Rank</b>" or "points / <span>Rank</span>".
	rankRe := regexp.MustCompile(`(?i)\d+[,\.]\d+\s*/\s*(?:<[^>]*>)*\s*<(?:span|b)[^>]*>\s*([^<]+?)\s*</(?:span|b)>`)
	if m := rankRe.FindStringSubmatch(body); m != nil {
		p.Rank = strings.TrimSpace(m[1])
	}

	// Team
	teamRe := regexp.MustCompile(`(?i)<a[^>]*href="/Teams/TeamDetails\.aspx\?tid=(\d+)"[^>]*>([^<]+)</a>`)
	if m := teamRe.FindStringSubmatch(body); m != nil {
		p.TeamID, _ = strconv.Atoi(m[1])
		p.Team = strings.TrimSpace(m[2])
	}

	// Domain - look for link like href="http://moscow.en.cx/">moscow.en.cx</a>
	domainRe := regexp.MustCompile(`(?i)<a[^>]*href="https?://([^/"]*\.en\.cx)"[^>]*>[^<]*</a>`)
	if m := domainRe.FindStringSubmatch(body); m != nil {
		p.Domain = m[1]
	}

	// Points - pattern: ">132,51 /" in the user info block (near uid link)
	pointsRe := regexp.MustCompile(`(?i)uid=\d+[^>]*>.*?(\d+[,\.]\d+)\s*/`)
	if m := pointsRe.FindStringSubmatch(body); m != nil {
		p.Points = m[1]
	} else {
		// Fallback: first occurrence of points pattern after the login
		fallbackRe := regexp.MustCompile(`(?i)>(\d{2,}[,\.]\d+)\s*/`)
		if m := fallbackRe.FindStringSubmatch(body); m != nil {
			p.Points = m[1]
		}
	}

	return p, nil
}

// AdminGame represents a game in the admin game manager.
type AdminGame struct {
	ID     int    `json:"id"`
	Number int    `json:"number"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

// AdminGetGames fetches the list of games the user has admin access to.
func (c *Client) legacyAdminGetGames(ctx context.Context) ([]AdminGame, error) {
	u := fmt.Sprintf("%s/Administration/GamesManager.aspx", c.baseURL())
	body, err := c.doGet(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("encx: admin get games: %w", err)
	}

	// Match game links: href="/GameDetails.aspx?gid=82033">Title</a>
	gameRe := regexp.MustCompile(`(?i)<a[^>]*href="/GameDetails\.aspx\?gid=(\d+)"[^>]*>([^<]+)</a>`)
	matches := gameRe.FindAllStringSubmatch(body, -1)

	seen := map[int]bool{}
	games := make([]AdminGame, 0, len(matches))
	for _, m := range matches {
		id, _ := strconv.Atoi(m[1])
		if id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		games = append(games, AdminGame{
			ID:    id,
			Title: strings.TrimSpace(m[2]),
		})
	}

	return games, nil
}

// --- Level Management ---

// AdminGetLevels fetches the list of levels for a game from the admin panel.
func (c *Client) legacyAdminGetLevels(ctx context.Context, gameId int) ([]AdminLevel, error) {
	u := fmt.Sprintf("%s/Administration/Games/LevelManager.aspx?gid=%d", c.baseURL(), gameId)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("encx: create admin get levels request: %w", err)
	}
	c.setHeaders(req)
	statusCode, headers, bodyBytes, err := c.doRequestAndRead(req)
	if err != nil {
		return nil, fmt.Errorf("encx: admin get levels: %w", err)
	}
	if isRedirectStatus(statusCode) {
		location := strings.TrimSpace(headers.Get("Location"))
		if isLoginRedirect(location) {
			return nil, fmt.Errorf("encx: admin get levels: session expired or access denied (redirect to login)")
		}
		return nil, fmt.Errorf("encx: admin get levels: unexpected redirect to %s (check game id and editor permissions)", location)
	}
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("encx: admin get levels: HTTP %d", statusCode)
	}

	body := string(bodyBytes)
	if err := guardAdminHTMLRequiresLogin(bodyBytes); err != nil {
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
	if len(levels) == 0 && !looksLikeLevelManagerPage(body) {
		return nil, fmt.Errorf("encx: admin get levels: level manager form not found (check game id and editor permissions)")
	}

	return levels, nil
}

func looksLikeLevelManagerPage(body string) bool {
	return strings.Contains(body, "txtLevelName_") ||
		strings.Contains(body, "ddlCreateLevelsNum") ||
		strings.Contains(body, "levels=create") ||
		strings.Contains(body, "level_names=update")
}

// AdminCreateLevels creates the specified number of new levels in the game.
func (c *Client) legacyAdminCreateLevels(ctx context.Context, gameId, count int) error {
	u := fmt.Sprintf("%s/Administration/Games/LevelManager.aspx?gid=%d&levels=create&ddlCreateLevelsNum=%d",
		c.baseURL(), gameId, count)
	_, err := c.doGet(ctx, u)
	if err != nil {
		return fmt.Errorf("encx: admin create levels: %w", err)
	}
	return nil
}

// AdminDeleteLevel deletes a level by its number.
func (c *Client) legacyAdminDeleteLevel(ctx context.Context, gameId, levelNum int) error {
	u := fmt.Sprintf("%s/Administration/Games/LevelManager.aspx?gid=%d&levels=delete&ddlDeleteLevels=%d",
		c.baseURL(), gameId, levelNum)
	_, err := c.doGet(ctx, u)
	if err != nil {
		return fmt.Errorf("encx: admin delete level: %w", err)
	}
	return nil
}

// AdminRenameLevels renames levels. The map key is the level ID, value is the new name.
func (c *Client) legacyAdminRenameLevels(ctx context.Context, gameId int, names map[int]string) error {
	u := fmt.Sprintf("%s/Administration/Games/LevelManager.aspx?gid=%d&level_names=update", c.baseURL(), gameId)

	form := url.Values{}
	for id, name := range names {
		form.Set(fmt.Sprintf("txtLevelName_%d", id), name)
	}

	_, err := c.doPost(ctx, u, form)
	if err != nil {
		return fmt.Errorf("encx: admin rename levels: %w", err)
	}
	c.adminDelay()
	return nil
}

// AdminUpdateAutopass updates the autopass settings for a level.
func (c *Client) legacyAdminUpdateAutopass(ctx context.Context, gameId, levelNum int, s AdminLevelSettings) error {
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
	c.adminDelay()
	return nil
}

// AdminUpdateAnswerBlock updates the answer block settings for a level.
func (c *Client) legacyAdminUpdateAnswerBlock(ctx context.Context, gameId, levelNum int, s AdminLevelSettings) error {
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
	c.adminDelay()
	return nil
}

// AdminUpdateSectorCompletion updates how many sectors are required to pass a level.
// requiredCount <= 0 means all sectors are required.
func (c *Client) legacyAdminUpdateSectorCompletion(ctx context.Context, gameId, levelNum, requiredCount int) error {
	u := fmt.Sprintf("%s/Administration/Games/LevelEditor.aspx?gid=%d&level=%d", c.baseURL(), gameId, levelNum)

	form := url.Values{}
	form.Set("action", "upsecsett")
	if requiredCount > 0 {
		form.Set("rbSectorCompleteType", "2")
		form.Set("txtRequiredSectorsCount", strconv.Itoa(requiredCount))
	} else {
		form.Set("rbSectorCompleteType", "1")
		form.Set("txtRequiredSectorsCount", "0")
	}
	form.Set("pnlSettings_SectorsCompletionSettings_ctl00_btnSave.x", "1")
	form.Set("pnlSettings_SectorsCompletionSettings_ctl00_btnSave.y", "1")

	_, err := c.doPost(ctx, u, form)
	if err != nil {
		return fmt.Errorf("encx: admin update sector completion: %w", err)
	}
	c.adminDelay()
	return nil
}

// --- Bonus Management ---

// AdminCreateBonus creates a new bonus on the specified level.
func (c *Client) legacyAdminCreateBonus(ctx context.Context, gameId, levelNum int, b AdminBonus) error {
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
	c.adminDelay()
	return nil
}

// AdminDeleteBonus deletes a bonus by its ID.
func (c *Client) legacyAdminDeleteBonus(ctx context.Context, gameId, levelNum, bonusId int) error {
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
func (c *Client) legacyAdminCreateSector(ctx context.Context, gameId, levelNum int, s AdminSector) error {
	u := fmt.Sprintf("%s/Administration/Games/LevelEditor.aspx?gid=%d&level=%d", c.baseURL(), gameId, levelNum)
	_, err := c.doPost(ctx, u, adminSectorForm(s))
	if err != nil {
		return fmt.Errorf("encx: admin create sector: %w", err)
	}
	c.adminDelay()
	return nil
}

// AdminAddSectorAnswers appends answers to an existing sector without creating a new sector.
func (c *Client) legacyAdminAddSectorAnswers(ctx context.Context, gameId, levelNum, sectorId int, answers []string) error {
	const maxAnswersPerRequest = 10
	for start := 0; start < len(answers); start += maxAnswersPerRequest {
		end := start + maxAnswersPerRequest
		if end > len(answers) {
			end = len(answers)
		}
		editBody, err := c.adminReadSectorEditPage(ctx, gameId, levelNum, sectorId)
		if err != nil {
			return fmt.Errorf("encx: admin add sector answers: %w", err)
		}
		form := adminAddSectorAnswersForm(editBody, sectorId, answers[start:end])
		if len(form) == 0 {
			continue
		}
		u := fmt.Sprintf("%s/Administration/Games/LevelEditor.aspx?gid=%d&level=%d", c.baseURL(), gameId, levelNum)
		if _, err := c.doPost(ctx, u, form); err != nil {
			return fmt.Errorf("encx: admin add sector answers: %w", err)
		}
		c.adminDelay()
	}
	return nil
}

// AdminUpdateSector updates an existing sector by its ID.
func (c *Client) legacyAdminUpdateSector(ctx context.Context, gameId, levelNum, sectorId int, s AdminSector) error {
	name := strings.TrimSpace(s.Name)
	if name == "" {
		name = fmt.Sprintf("Сектор %d", sectorId)
	}
	if err := c.adminSaveSectorForm(ctx, gameId, levelNum, sectorId, name, s.Answers); err != nil {
		return fmt.Errorf("encx: admin update sector: %w", err)
	}
	return nil
}

func adminSectorForm(s AdminSector) url.Values {
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
	return form
}

func adminAddSectorAnswersForm(body string, sectorId int, answers []string) url.Values {
	form := url.Values{}
	if sectorId <= 0 {
		return form
	}
	block := sectorAddAnswersFormBlock(body, sectorId)
	form = parseHTMLHiddenFields(block)
	form.Set("ddlSector", strconv.Itoa(sectorId))
	form.Set("saveanswers", "1")
	if submitName := firstImageSubmitName(block); submitName != "" {
		form.Set(submitName+".x", "1")
		form.Set(submitName+".y", "1")
	}

	next := 0
	for _, ans := range answers {
		ans = strings.TrimSpace(ans)
		if ans == "" {
			continue
		}
		form.Set(fmt.Sprintf("txtAnswer_%d", next), ans)
		form.Set(fmt.Sprintf("ddlAnswerFor_%d", next), "0")
		next++
	}
	if next == 0 {
		return url.Values{}
	}
	return form
}

// AdminDeleteSector deletes a sector by its ID (delsector= from LevelEditor).
func (c *Client) legacyAdminDeleteSector(ctx context.Context, gameId, levelNum, sectorId int) error {
	return c.adminDeleteSector(ctx, gameId, levelNum, sectorId)
}

func (c *Client) adminLevelEditorAnswersListURL(gameId, levelNum int) string {
	return fmt.Sprintf("%s/Administration/Games/LevelEditor.aspx?level=%d&gid=%d&swanswers=1",
		c.baseURL(), levelNum, gameId)
}

func (c *Client) adminDeleteSectorURL(ctx context.Context, rawURL, referer string) (string, error) {
	body, err := c.doGetFollowRedirects(ctx, rawURL, referer)
	if err != nil {
		return "", err
	}
	lower := strings.ToLower(body)
	if strings.Contains(lower, "object moved") && strings.Contains(lower, "login.aspx") {
		return body, fmt.Errorf("session expired")
	}
	return body, nil
}

// doGetFollowRedirects performs GET and follows redirects (LevelEditor delete often returns 302).
func (c *Client) doGetFollowRedirects(ctx context.Context, rawURL, referer string) (string, error) {
	const maxRedirects = 5
	currentURL := rawURL

	for attempt := 0; attempt < maxRedirects; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, currentURL, nil)
		if err != nil {
			return "", fmt.Errorf("encx: create GET request: %w", err)
		}
		c.setHeaders(req)
		if referer != "" {
			req.Header.Set("Referer", referer)
		}

		statusCode, headers, respBody, err := c.doRequestAndRead(req)
		if err != nil {
			return "", err
		}

		if isRedirectStatus(statusCode) {
			location := headers.Get("Location")
			if isLoginRedirect(location) {
				return "", fmt.Errorf("encx: session expired or access denied (redirect to login)")
			}
			if location == "" {
				return "", fmt.Errorf("encx: redirect without Location (HTTP %d)", statusCode)
			}
			nextURL, err := resolveAgainstBase(c.baseURL(), location)
			if err != nil {
				return "", fmt.Errorf("encx: resolve redirect: %w", err)
			}
			currentURL = nextURL
			continue
		}

		if statusCode != http.StatusOK {
			return "", fmt.Errorf("encx: GET %s: HTTP %d", currentURL, statusCode)
		}
		return string(respBody), nil
	}

	return "", fmt.Errorf("encx: GET %s: too many redirects", rawURL)
}

// --- Hint Management ---

// AdminCreateHint creates a new hint (regular or penalty) on the specified level.
func (c *Client) legacyAdminCreateHint(ctx context.Context, gameId, levelNum int, h AdminHint) error {
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

	if h.ReplaceNl {
		form.Set("chkReplaceNlToBr", "on")
	}

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
	c.adminDelay()
	return nil
}

// AdminDeleteHint deletes a hint by its ID.
//
// Penalty hints that a player has already taken cannot be deleted — the
// engine keeps them because penalty time was awarded. In that case the
// server's refusal is returned as an error instead of silently succeeding.
func (c *Client) legacyAdminDeleteHint(ctx context.Context, gameId, levelNum, hintId int) error {
	u := fmt.Sprintf("%s/Administration/Games/PromptEdit.aspx?gid=%d&level=%d&prid=%d&action=PromptDelete",
		c.baseURL(), gameId, levelNum, hintId)
	body, err := c.doGet(ctx, u)
	if err != nil {
		return fmt.Errorf("encx: admin delete hint: %w", err)
	}
	if msg := hintDeleteRefusal(body); msg != "" {
		return fmt.Errorf("encx: admin delete hint: %s", msg)
	}
	// Penalty hints are only deleted when the URL carries penalty=1; the
	// regular delete request above silently leaves them in place. Deleting an
	// already removed id is a no-op, so both variants are always issued.
	up := fmt.Sprintf("%s/Administration/Games/PromptEdit.aspx?penalty=1&gid=%d&level=%d&prid=%d&action=PromptDelete",
		c.baseURL(), gameId, levelNum, hintId)
	body, err = c.doGet(ctx, up)
	if err != nil {
		return fmt.Errorf("encx: admin delete penalty hint: %w", err)
	}
	if msg := hintDeleteRefusal(body); msg != "" {
		return fmt.Errorf("encx: admin delete penalty hint: %s", msg)
	}
	return nil
}

// hintDeleteRefusal extracts the engine's refusal message from a hint delete
// response, e.g. "Штрафная подсказка не может быть удалена, так как по ней
// было начислено штрафное время одному из игроков." Empty when none found.
func hintDeleteRefusal(body string) string {
	i := strings.Index(body, "не может быть удалена")
	if i < 0 {
		return ""
	}
	start := strings.LastIndexAny(body[:i], ">\n") + 1
	rest := body[start:]
	if end := strings.IndexAny(rest, "<\n"); end >= 0 {
		rest = rest[:end]
	}
	// The message may sit inside an inline script comment ("// Штрафная …").
	return strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(rest), "/"))
}

// --- Task Management ---

// AdminCreateTask creates a new task on the specified level.
func (c *Client) legacyAdminCreateTask(ctx context.Context, gameId, levelNum int, t AdminTask) error {
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
	c.adminDelay()
	return nil
}

// --- Comment Management ---

// AdminUpdateComment updates the level name and comment.
func (c *Client) legacyAdminUpdateComment(ctx context.Context, gameId, levelNum int, name, comment string) error {
	u := fmt.Sprintf("%s/Administration/Games/NameCommentEdit.aspx?gid=%d&level=%d", c.baseURL(), gameId, levelNum)

	form := url.Values{}
	form.Set("txtLevelName", name)
	form.Set("txtLevelComment", comment)

	_, err := c.doPost(ctx, u, form)
	if err != nil {
		return fmt.Errorf("encx: admin update comment: %w", err)
	}
	c.adminDelay()
	return nil
}

// --- Team Management ---

// AdminGetTeams fetches the list of teams registered for the game.
func (c *Client) legacyAdminGetTeams(ctx context.Context, gameId, levelNum int) ([]AdminTeam, error) {
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
func (c *Client) legacyAdminGetCorrections(ctx context.Context, gameId int) ([]AdminCorrection, error) {
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
func (c *Client) legacyAdminAddCorrection(ctx context.Context, gameId int, corr AdminCorrectionAdd) error {
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
func (c *Client) legacyAdminDeleteCorrection(ctx context.Context, gameId int, correctionId string) error {
	u := fmt.Sprintf("%s/GameBonusPenaltyTime.aspx?gid=%d&action=delete&correct=%s",
		c.baseURL(), gameId, correctionId)
	_, err := c.doGet(ctx, u)
	if err != nil {
		return fmt.Errorf("encx: admin delete correction: %w", err)
	}
	return nil
}

// legacyCreateDateLayout is the date format the ASP.NET admin forms render and accept.
const legacyCreateDateLayout = "02.01.2006 15:04:05"

// legacyGidRe pulls a game id out of the redirect the create form answers with.
var legacyGidRe = regexp.MustCompile(`(?i)[?&]gid=(\d+)`)

// legacyFormErrorRe matches the label the admin forms render a rejection into.
var legacyFormErrorRe = regexp.MustCompile(`(?is)<span[^>]*id="[^"]*lblErrMsg"[^>]*>(.*?)</span>`)

// legacyDateTime renders an ISO timestamp the way the admin forms expect. The
// new engine takes RFC3339, so the same params struct has to survive both
// engines; anything the layouts do not recognise is passed through untouched.
func legacyDateTime(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.Format(legacyCreateDateLayout)
		}
	}
	return value
}

// AdminCreateGame creates a game through the GameCreate.aspx form and returns its id.
func (c *Client) legacyAdminCreateGame(ctx context.Context, params AdminCreateGameParams) (int, error) {
	if strings.TrimSpace(params.Title) == "" {
		return 0, fmt.Errorf("encx: admin create game: title is required")
	}
	start := legacyDateTime(params.StartDateTime)
	if start == "" {
		return 0, fmt.Errorf("encx: admin create game: start date is required")
	}

	u := fmt.Sprintf("%s/Administration/Games/GameCreate.aspx", c.baseURL())
	formURL := fmt.Sprintf("%s?gamezonelist=%d&gametypeid=%d", u, params.ZoneID, params.GameType)

	// The page refuses a game without authors, and the client does not keep the
	// login around, so an unset Authors is filled from the value the pristine
	// form is pre-populated with: the creator.
	authors := params.Authors
	if strings.TrimSpace(authors) == "" {
		body, err := c.doGet(ctx, formURL)
		if err != nil {
			return 0, fmt.Errorf("encx: admin create game form: %w", err)
		}
		authors = parseEnabledInputs(body)["GameAuthors"]
		if authors == "" {
			return 0, fmt.Errorf("encx: admin create game: authors are required")
		}
	}

	form := url.Values{}
	form.Set("action", "create")
	form.Set("GameZoneList", strconv.Itoa(params.ZoneID))
	form.Set("GameTypeID", strconv.Itoa(params.GameType))
	form.Set("GameTitle", params.Title)
	form.Set("GameAuthors", authors)
	form.Set("Descr", params.Description)
	form.Set("StartDateTime", start)
	form.Set("FinishDateTime", legacyDateTime(params.FinishDateTime))
	form.Set("RequestLastDate", legacyDateTime(params.RequestLastDate))
	if params.IsModerated {
		form.Set("IsModerated", "true")
	} else {
		form.Set("IsModerated", "false")
	}

	// Defaults copied from the pristine form: the page rejects the post when a
	// numeric or date field arrives empty, and every one of them is editable
	// afterwards through AdminUpdateGameInfo.
	form.Set("Tabs1_tabsContent_baseSettings_vp0", "Tabs1_tabsContent_baseSettings_vp0")
	form.Set("Fee", "0,00")
	form.Set("FeeCurrency", "1")
	form.Set("Prize", "0")
	form.Set("GameStatAvailabilityList", "1")
	form.Set("GameScenarioAvailabilityList", "2")
	form.Set("ShowFeeList", "1")
	form.Set("MaxPlayers", "0")
	form.Set("MaxTeamPlayers", "0")
	form.Set("CertificateMode", "1")
	form.Set("FirstPlaces", "3")
	form.Set("NotFirstPlaces", "3")
	form.Set("radioAcceptRateMode", "1")
	form.Set("txtAcceptRateFrom", start)
	form.Set("ddlAuthorsCompexity", "10")
	form.Set("btnCreate.x", "1")
	form.Set("btnCreate.y", "1")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, fmt.Errorf("encx: admin create game: %w", err)
	}
	c.setHeaders(req)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", formURL)

	status, headers, respBody, err := c.doRequestAndRead(req)
	if err != nil {
		return 0, fmt.Errorf("encx: admin create game: %w", err)
	}
	if !isRedirectStatus(status) {
		// A rejected form is re-rendered in place, with the reason in a label.
		if m := legacyFormErrorRe.FindSubmatch(respBody); m != nil {
			if reason := strings.TrimSpace(stripTags(string(m[1]))); reason != "" {
				return 0, fmt.Errorf("encx: admin create game: %s", reason)
			}
		}
		return 0, fmt.Errorf("encx: admin create game: form rejected (HTTP %d)", status)
	}
	location := headers.Get("Location")
	if isLoginRedirect(location) {
		return 0, fmt.Errorf("encx: admin create game: session expired or access denied")
	}
	m := legacyGidRe.FindStringSubmatch(location)
	if m == nil {
		return 0, fmt.Errorf("encx: admin create game: no game id in redirect %q", location)
	}
	id, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, fmt.Errorf("encx: admin create game: bad game id in redirect %q", location)
	}
	return id, nil
}

// AdminGetGameInfo reads the game editor page and returns current game settings.
func (c *Client) legacyAdminGetGameInfo(ctx context.Context, gameId int) (*AdminGameInfo, error) {
	u := fmt.Sprintf("%s/Administration/Games/GameEditor.aspx?gid=%d&action=edit", c.baseURL(), gameId)
	body, err := c.doGet(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("encx: admin get game info: %w", err)
	}

	info := &AdminGameInfo{}
	inputs := parseEnabledInputs(body)

	info.Title = inputs["GameTitle"]
	info.Authors = inputs["GameAuthors"]
	info.Prize = inputs["Prize"]
	// A started game renders the start disabled, so parseEnabledInputs leaves
	// StartDateTime empty and legacyAdminUpdateGameInfo omits it in turn.
	info.StartDateTime = inputs["StartDateTime"]
	info.FinishDateTime = inputs["FinishDateTime"]
	info.RequestLastDate = inputs["RequestLastDate"]
	info.MaxPlayers = inputs["MaxPlayers"]
	info.MaxTeamPlayers = inputs["MaxTeamPlayers"]
	info.FirstPlaces = inputs["FirstPlaces"]
	info.NotFirstPlaces = inputs["NotFirstPlaces"]
	info.AcceptRateFrom = inputs["txtAcceptRateFrom"]

	// Description from textarea
	descrRe := regexp.MustCompile(`(?i)<textarea[^>]*name="Descr"[^>]*>([\s\S]*?)</textarea>`)
	if m := descrRe.FindStringSubmatch(body); m != nil {
		info.Description = m[1]
	}

	// Checkboxes
	checked := parseCheckedInputs(body)
	info.IsModerated = checked["IsModerated"]
	info.ShowFinishPlace = checked["chkShowFinishPlace"]

	// Radio/select values from hidden or selected options
	info.GameStatAvailability = parseSelectedRadioOrValue(body, "GameStatAvailabilityList", inputs)
	info.GameScenarioAvailability = parseSelectedRadioOrValue(body, "GameScenarioAvailabilityList", inputs)
	info.ShowFee = parseSelectedRadioOrValue(body, "ShowFeeList", inputs)
	info.CertificateMode = parseSelectedRadioOrValue(body, "CertificateMode", inputs)
	info.AcceptRateMode = parseSelectedRadioOrValue(body, "radioAcceptRateMode", inputs)
	info.AuthorComplexity = parseSelectedRadioOrValue(body, "ddlAuthorsCompexity", inputs)

	return info, nil
}

// AdminUpdateGameInfo updates game settings via the game editor page.
func (c *Client) legacyAdminUpdateGameInfo(ctx context.Context, gameId int, info AdminGameInfo) error {
	u := fmt.Sprintf("%s/Administration/Games/GameEditor.aspx", c.baseURL())

	form := url.Values{}
	form.Set("gid", strconv.Itoa(gameId))
	form.Set("action", "update")
	form.Set("GameTitle", info.Title)
	form.Set("GameAuthors", info.Authors)
	form.Set("Descr", info.Description)
	form.Set("Prize", info.Prize)
	// legacyDateTime leaves the form's own spelling alone and converts an
	// RFC3339 value into it, so a caller may write either.
	form.Set("FinishDateTime", legacyDateTime(info.FinishDateTime))
	form.Set("RequestLastDate", legacyDateTime(info.RequestLastDate))
	// The start travels only when the caller has one: the page keeps the current
	// value for a field the post omits, while an empty StartDateTime — what a
	// started game reads back — would be rejected as a missing date.
	if start := legacyDateTime(info.StartDateTime); start != "" {
		form.Set("StartDateTime", start)
	}
	form.Set("Tabs1_tabsContent_baseSettings_vp1", "Tabs1_tabsContent_baseSettings_vp1")

	if info.IsModerated {
		form.Set("IsModerated", "true")
	} else {
		form.Set("IsModerated", "false")
	}
	if info.ShowFinishPlace {
		form.Set("chkShowFinishPlace", "true")
	}

	form.Set("GameStatAvailabilityList", cmpOr(info.GameStatAvailability, "1"))
	form.Set("GameScenarioAvailabilityList", cmpOr(info.GameScenarioAvailability, "2"))
	form.Set("MaxPlayers", cmpOr(info.MaxPlayers, "0"))
	form.Set("MaxTeamPlayers", cmpOr(info.MaxTeamPlayers, "0"))
	form.Set("ShowFeeList", cmpOr(info.ShowFee, "1"))
	form.Set("CertificateMode", cmpOr(info.CertificateMode, "1"))
	form.Set("FirstPlaces", cmpOr(info.FirstPlaces, "3"))
	form.Set("NotFirstPlaces", cmpOr(info.NotFirstPlaces, "3"))
	form.Set("radioAcceptRateMode", cmpOr(info.AcceptRateMode, "1"))
	form.Set("txtAcceptRateFrom", info.AcceptRateFrom)
	form.Set("ddlAuthorsCompexity", cmpOr(info.AuthorComplexity, "10"))
	form.Set("btnUpdate.x", "1")
	form.Set("btnUpdate.y", "1")

	_, err := c.doPost(ctx, u, form)
	if err != nil {
		return fmt.Errorf("encx: admin update game info: %w", err)
	}
	return nil
}

// AdminNotDeliverGame marks a game as "not delivered" (несостоявшаяся).
func (c *Client) legacyAdminNotDeliverGame(ctx context.Context, gameId int) error {
	u := fmt.Sprintf("%s/Administration/GamesManager.aspx?gid=%d&action=NotDeliver", c.baseURL(), gameId)
	_, err := c.doGet(ctx, u)
	if err != nil {
		return fmt.Errorf("encx: admin not deliver game: %w", err)
	}
	return nil
}

// --- Level Reordering ---

// AdminSwapLevels swaps two levels by their numbers.
func (c *Client) legacyAdminSwapLevels(ctx context.Context, gameId, level1, level2 int) error {
	u := fmt.Sprintf("%s/Administration/Games/LevelManager.aspx?gid=%d&levels=swap&ddlSwapLevels1=%d&ddlSwapLevels2=%d",
		c.baseURL(), gameId, level1, level2)
	_, err := c.doGet(ctx, u)
	if err != nil {
		return fmt.Errorf("encx: admin swap levels: %w", err)
	}
	return nil
}

// AdminInsertLevel moves level src to the position after level dst.
func (c *Client) legacyAdminInsertLevel(ctx context.Context, gameId, src, dst int) error {
	u := fmt.Sprintf("%s/Administration/Games/LevelManager.aspx?gid=%d&levels=insert&ddlInsertAfterSrc=%d&ddlInsertAfterDst=%d",
		c.baseURL(), gameId, src, dst)
	_, err := c.doGet(ctx, u)
	if err != nil {
		return fmt.Errorf("encx: admin insert level: %w", err)
	}
	return nil
}

// AdminCloneLevels creates count new levels cloned from the specified level number.
func (c *Client) legacyAdminCloneLevels(ctx context.Context, gameId, count, likeLevel int) error {
	u := fmt.Sprintf("%s/Administration/Games/LevelManager.aspx?gid=%d&levels=createlike&ddlCreateLevelsNum=%d&ddlCreateLikeLevel=%d",
		c.baseURL(), gameId, count, likeLevel)
	_, err := c.doGet(ctx, u)
	if err != nil {
		return fmt.Errorf("encx: admin clone levels: %w", err)
	}
	return nil
}

// --- Task Delete/Update ---

// AdminDeleteTask deletes a task by its ID.
func (c *Client) legacyAdminDeleteTask(ctx context.Context, gameId, levelNum, taskId int) error {
	u := fmt.Sprintf("%s/Administration/Games/TaskEdit.aspx?gid=%d&level=%d&tid=%d&action=TaskDelete",
		c.baseURL(), gameId, levelNum, taskId)
	_, err := c.doGet(ctx, u)
	if err != nil {
		return fmt.Errorf("encx: admin delete task: %w", err)
	}
	return nil
}

// AdminUpdateTask updates an existing task by its ID.
func (c *Client) legacyAdminUpdateTask(ctx context.Context, gameId, levelNum, taskId int, t AdminTask) error {
	u := fmt.Sprintf("%s/Administration/Games/TaskEdit.aspx?gid=%d&level=%d&tid=%d&action=TaskEdit",
		c.baseURL(), gameId, levelNum, taskId)

	form := url.Values{}
	form.Set("inputTask", t.Text)
	form.Set("forMemberID", t.ForMemberID)

	if t.ReplaceNl {
		form.Set("chkReplaceNlToBr", "on")
	}

	_, err := c.doPost(ctx, u, form)
	if err != nil {
		return fmt.Errorf("encx: admin update task: %w", err)
	}
	c.adminDelay()
	return nil
}

// --- Bonus Update ---

// AdminUpdateBonus updates an existing bonus by its ID.
func (c *Client) legacyAdminUpdateBonus(ctx context.Context, gameId, levelNum, bonusId int, b AdminBonus) error {
	u := fmt.Sprintf("%s/Administration/Games/BonusEdit.aspx?gid=%d&level=%d&bonus=%d&action=save",
		c.baseURL(), gameId, levelNum, bonusId)

	form := url.Values{}
	form.Set("txtBonusName", b.Name)
	form.Set("txtTask", b.Task)
	form.Set("txtHelp", b.Hint)
	form.Set("ddlBonusFor", b.BonusFor)

	if b.LevelID == -1 || b.LevelID == 0 {
		form.Set("rbAllLevels", "1")
	} else {
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
		return fmt.Errorf("encx: admin update bonus: %w", err)
	}
	c.adminDelay()
	return nil
}

// --- Hint Update ---

// AdminUpdateHint updates an existing hint by its ID.
func (c *Client) legacyAdminUpdateHint(ctx context.Context, gameId, levelNum, hintId int, h AdminHint) error {
	var u string
	if h.IsPenalty || h.RequestConfirm {
		u = fmt.Sprintf("%s/Administration/Games/PromptEdit.aspx?penalty=1&gid=%d&level=%d&prid=%d",
			c.baseURL(), gameId, levelNum, hintId)
	} else {
		u = fmt.Sprintf("%s/Administration/Games/PromptEdit.aspx?gid=%d&level=%d&prid=%d",
			c.baseURL(), gameId, levelNum, hintId)
	}

	form := url.Values{}
	form.Set("NewPrompt", h.Text)
	form.Set("NewPromptTimeoutDays", strconv.Itoa(h.Days))
	form.Set("NewPromptTimeoutHours", strconv.Itoa(h.Hours))
	form.Set("NewPromptTimeoutMinutes", strconv.Itoa(h.Minutes))
	form.Set("NewPromptTimeoutSeconds", strconv.Itoa(h.Seconds))

	if h.ReplaceNl {
		form.Set("chkReplaceNlToBr", "on")
	}

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
		return fmt.Errorf("encx: admin update hint: %w", err)
	}
	c.adminDelay()
	return nil
}

// --- Game Lifecycle ---

// AdminDeliverGame marks a game as "delivered" (состоявшаяся).
func (c *Client) legacyAdminDeliverGame(ctx context.Context, gameId int) error {
	u := fmt.Sprintf("%s/Administration/GamesManager.aspx?gid=%d&action=Deliver", c.baseURL(), gameId)
	_, err := c.doGet(ctx, u)
	if err != nil {
		return fmt.Errorf("encx: admin deliver game: %w", err)
	}
	return nil
}

// AdminAwardPoints awards points to game participants.
func (c *Client) legacyAdminAwardPoints(ctx context.Context, gameId int) error {
	u := fmt.Sprintf("%s/Administration/GamesManager.aspx?gid=%d&action=AwardPoints", c.baseURL(), gameId)
	_, err := c.doGet(ctx, u)
	if err != nil {
		return fmt.Errorf("encx: admin award points: %w", err)
	}
	return nil
}

// AdminEndRatings ends accepting ratings for a game.
func (c *Client) legacyAdminEndRatings(ctx context.Context, gameId int) error {
	u := fmt.Sprintf("%s/Administration/GamesManager.aspx?gid=%d&action=EndRatings", c.baseURL(), gameId)
	_, err := c.doGet(ctx, u)
	if err != nil {
		return fmt.Errorf("encx: admin end ratings: %w", err)
	}
	return nil
}

// AdminCalculateIK calculates the game coefficient (ИК).
func (c *Client) legacyAdminCalculateIK(ctx context.Context, gameId int) error {
	u := fmt.Sprintf("%s/Administration/GamesManager.aspx?gid=%d&action=CalcIK", c.baseURL(), gameId)
	_, err := c.doGet(ctx, u)
	if err != nil {
		return fmt.Errorf("encx: admin calculate IK: %w", err)
	}
	return nil
}

// AdminGetActionMonitor reads the game action monitor rows.
func (c *Client) legacyAdminGetActionMonitor(ctx context.Context, gameId int) ([]AdminActionMonitorEntry, error) {
	u := fmt.Sprintf("%s/Administration/Games/ActionMonitor.aspx?gid=%d&type=own", c.baseURL(), gameId)
	body, err := c.doGet(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("encx: admin get action monitor: %w", err)
	}

	rowRe := regexp.MustCompile(`(?is)<tr[^>]*>(.*?)</tr>`)
	rows := rowRe.FindAllStringSubmatch(body, -1)
	entries := make([]AdminActionMonitorEntry, 0, len(rows))
	for _, row := range rows {
		tds := adminTdRe.FindAllStringSubmatch(row[1], -1)
		if len(tds) < 5 {
			continue
		}
		vals := make([]string, 0, len(tds))
		for _, td := range tds {
			vals = append(vals, strings.TrimSpace(stripTags(td[1])))
		}
		if len(vals) == 0 || vals[0] == "#" || vals[0] == "№" {
			continue
		}

		entry := AdminActionMonitorEntry{}
		switch len(vals) {
		case 5:
			entry.Number = vals[0]
			entry.Participant = vals[1]
			entry.Answer = vals[2]
			entry.DateTime = vals[3]
			entry.Sectors = vals[4]
		default:
			entry.Number = vals[0]
			entry.Participant = vals[1]
			entry.Direction = vals[2]
			entry.Answer = vals[3]
			entry.DateTime = vals[4]
			entry.Sectors = strings.Join(vals[5:], " | ")
		}
		if entry.Answer == "" && entry.DateTime == "" {
			continue
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

// --- Game Messages ---

// AdminCreateMessage creates a message for a game using the MessageEdit form.
func (c *Client) legacyAdminCreateMessage(ctx context.Context, gameId, levelID int, m AdminGameMessage) error {
	u := fmt.Sprintf("%s/Administration/Games/MessageEdit.aspx?gid=%d&level=%d&action=add", c.baseURL(), gameId, levelID)
	form := adminMessageForm(levelID, m)
	_, err := c.doPost(ctx, u, form)
	if err != nil {
		return fmt.Errorf("encx: admin create message: %w", err)
	}
	return nil
}

// AdminUpdateMessage updates an existing message by its ID.
func (c *Client) legacyAdminUpdateMessage(ctx context.Context, gameId, levelNum, messageId int, m AdminGameMessage) error {
	u := fmt.Sprintf("%s/Administration/Games/MessageEdit.aspx?gid=%d&level=%d&mid=%d", c.baseURL(), gameId, levelNum, messageId)
	form := adminMessageForm(levelNum, m)
	_, err := c.doPost(ctx, u, form)
	if err != nil {
		return fmt.Errorf("encx: admin update message: %w", err)
	}
	return nil
}

// AdminDeleteMessage deletes a message by its ID.
func (c *Client) legacyAdminDeleteMessage(ctx context.Context, gameId, levelNum, messageId int) error {
	u := fmt.Sprintf("%s/Administration/Games/MessageEdit.aspx?gid=%d&level=%d&mid=%d&action=delete", c.baseURL(), gameId, levelNum, messageId)
	_, err := c.doGet(ctx, u)
	if err != nil {
		return fmt.Errorf("encx: admin delete message: %w", err)
	}
	return nil
}

func adminMessageForm(levelID int, m AdminGameMessage) url.Values {
	form := url.Values{}
	form.Set("txtMessage", m.Text)
	if m.ReplaceNlToBr {
		form.Set("chkReplaceNlToBr", "on")
	}

	if m.ShowOnLevelsMode == 2 {
		form.Set("rbShowOnLevels", "2")
		levelIDs := m.LevelIDs
		if len(levelIDs) == 0 && levelID > 0 {
			levelIDs = []int{levelID}
		}
		for _, id := range levelIDs {
			if id > 0 {
				form.Set(fmt.Sprintf("lvl_%d", id), "on")
			}
		}
	} else {
		form.Set("rbShowOnLevels", "1")
	}

	if m.RequiredPoints != "" {
		form.Set("txtRequiredPoints", m.RequiredPoints)
	}
	return form
}

// parseSelectedRadioOrValue extracts a value from either an input field or a checked radio button.
func parseSelectedRadioOrValue(body, name string, inputs map[string]string) string {
	if v, ok := inputs[name]; ok {
		return v
	}
	// Try to find checked radio
	re := regexp.MustCompile(`(?i)<input[^>]*name="` + regexp.QuoteMeta(name) + `"[^>]*checked[^>]*value="([^"]*)"`)
	if m := re.FindStringSubmatch(body); m != nil {
		return m[1]
	}
	// Try value then checked order
	re2 := regexp.MustCompile(`(?i)<input[^>]*value="([^"]*)"[^>]*name="` + regexp.QuoteMeta(name) + `"[^>]*checked`)
	if m := re2.FindStringSubmatch(body); m != nil {
		return m[1]
	}
	// Try selected option in a select
	re3 := regexp.MustCompile(`(?i)<select[^>]*name="` + regexp.QuoteMeta(name) + `"[^>]*>[\s\S]*?<option[^>]*selected[^>]*value="([^"]*)"`)
	if m := re3.FindStringSubmatch(body); m != nil {
		return m[1]
	}
	return ""
}

// cmpOr returns val if non-empty, otherwise fallback.
func cmpOr(val, fallback string) string {
	if val != "" {
		return val
	}
	return fallback
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
