package encx

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var (
	levelSequenceSelectRE   = regexp.MustCompile(`(?is)<select\b([^>]*)\bid=["']ddlLevelsSequence["']([^>]*)>(.*?)</select>`)
	levelSequenceOptionRE   = regexp.MustCompile(`(?is)<option\b([^>]*)>(.*?)</option>`)
	levelSequenceValueRE    = regexp.MustCompile(`(?i)\bvalue=["'](\d+)["']`)
	levelSequenceSelectedRE = regexp.MustCompile(`(?i)\bselected(?:\s|=|>|$)`)
	levelSequenceDisabledRE = regexp.MustCompile(`(?i)\bdisabled(?:\s|=|>|$)`)
)

func parseLegacyLevelSequence(body string) (*AdminLevelSequence, error) {
	selectMatch := levelSequenceSelectRE.FindStringSubmatch(body)
	if selectMatch == nil {
		return nil, fmt.Errorf("encx: level sequence selector not found (check game and editor access)")
	}
	options := levelSequenceOptionRE.FindAllStringSubmatch(selectMatch[3], -1)
	if len(options) == 0 {
		return nil, fmt.Errorf("encx: level sequence selector has no options")
	}
	id := -1
	for i, option := range options {
		value := levelSequenceValueRE.FindStringSubmatch(option[1])
		if value == nil {
			continue
		}
		if i == 0 || levelSequenceSelectedRE.MatchString(option[1]) {
			id, _ = strconv.Atoi(value[1])
		}
		if levelSequenceSelectedRE.MatchString(option[1]) {
			break
		}
	}
	if id < 0 {
		return nil, fmt.Errorf("encx: level sequence selector has no numeric option")
	}
	attrs := selectMatch[1] + selectMatch[2]
	return &AdminLevelSequence{
		ID: id,
		CanChange: strings.Contains(body, `name="sequences" value="change"`) &&
			!levelSequenceDisabledRE.MatchString(attrs),
	}, nil
}

func (c *Client) legacyAdminGetLevelSequence(ctx context.Context, gameID int) (*AdminLevelSequence, error) {
	page := fmt.Sprintf("%s/Administration/Games/LevelManager.aspx?gid=%d", c.baseURL(), gameID)
	body, err := c.doGet(ctx, page)
	if err != nil {
		return nil, fmt.Errorf("encx: admin get level sequence: %w", err)
	}
	if err := guardAdminHTMLRequiresLogin([]byte(body)); err != nil {
		return nil, fmt.Errorf("encx: admin get level sequence: %w", err)
	}
	return parseLegacyLevelSequence(body)
}

func (c *Client) legacyAdminSetLevelSequence(ctx context.Context, gameID, sequenceID int) error {
	current, err := c.legacyAdminGetLevelSequence(ctx, gameID)
	if err != nil {
		return err
	}
	if current.ID == sequenceID {
		return nil
	}
	if !current.CanChange {
		return fmt.Errorf("encx: game %d does not allow changing its level sequence", gameID)
	}
	query := url.Values{
		"gid":               {strconv.Itoa(gameID)},
		"sequences":         {"change"},
		"ddlLevelsSequence": {strconv.Itoa(sequenceID)},
	}
	page := c.baseURL() + "/Administration/Games/LevelManager.aspx?" + query.Encode()
	if _, err := c.doGet(ctx, page); err != nil {
		return fmt.Errorf("encx: admin set level sequence: %w", err)
	}
	after, err := c.legacyAdminGetLevelSequence(ctx, gameID)
	if err != nil {
		return err
	}
	if after.ID != sequenceID {
		return fmt.Errorf("encx: level sequence remains %d after update, wanted %d", after.ID, sequenceID)
	}
	return nil
}
