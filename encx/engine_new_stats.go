package encx

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/skrashevich/encx-cli/encx/enapi"
)

// statisticsPageLimit bounds the walk over the statistics table. The route
// answers with a 10000-row page by default, so a game needing more than this
// many pages does not exist; the cap only stops a server that keeps claiming
// another page.
const statisticsPageLimit = 50

func (e *newEngine) GetGameStatistics(ctx context.Context, gameId int) (*GameStatisticsResponse, error) {
	path := fmt.Sprintf("/games/%d/statistics", gameId)
	query := func(page int, confirm bool) url.Values {
		q := url.Values{"lang": {e.c.lang}}
		if page > 1 {
			q.Set("page", strconv.Itoa(page))
		}
		if confirm {
			q.Set("confirm", "1")
		}
		return q
	}

	var stats enapi.GameStatisticsResponse
	if err := e.c.api().GetJSON(ctx, path, query(1, false), &stats); err != nil {
		return nil, err
	}
	// needs_confirm is the engine asking "the author closed these statistics,
	// view anyway?" and answering with an empty table until told. The legacy
	// endpoint handed the table over, so the override is sent — but only here,
	// on the one answer that asks for it: the server records the override in its
	// audit log, and doing that on every read of every game would be a side
	// effect the legacy call never had.
	confirmed := false
	if stats.NeedsConfirm {
		if err := e.c.api().GetJSON(ctx, path, query(1, true), &stats); err != nil {
			return nil, err
		}
		confirmed = true
		if stats.NeedsConfirm {
			return nil, fmt.Errorf("encx: game statistics %d: %s", gameId,
				firstNonEmpty(stats.ConfirmMessage, "автор закрыл статистику"))
		}
	}

	// The table is paged. GetGameStatistics takes no page argument, and the
	// counts below are computed from the rows themselves, so a partial page
	// would not merely shorten the answer — it would publish wrong totals.
	//
	// The page number is this loop's own, not the server's echo: a backend that
	// pages correctly but does not report current_page would otherwise look like
	// one that never advances.
	read := 1
	for page := 2; page <= statisticsPageLimit && read < stats.TotalPages; page++ {
		var next enapi.GameStatisticsResponse
		if err := e.c.api().GetJSON(ctx, path, query(page, confirmed), &next); err != nil {
			return nil, err
		}
		read = page
		if len(next.LevelStats) == 0 {
			break
		}
		mergeStatisticsPage(&stats, &next)
	}
	if read < stats.TotalPages {
		return nil, fmt.Errorf(
			"encx: game statistics %d: движок отдаёт %d страниц, прочитано %d — результат был бы неполным",
			gameId, stats.TotalPages, read)
	}

	return gameStatisticsFromAPI(&stats), nil
}

// mergeStatisticsPage folds a later page into the accumulated document. Only the
// paged row collections grow; everything else describes the whole game and is
// repeated verbatim on every page.
//
// level_corrections is one of those repeats — the specification derives it from
// GetSummByGameID, a per-game aggregate — so appending it across pages would
// multiply every correction by the number of pages read.
func mergeStatisticsPage(into, next *enapi.GameStatisticsResponse) {
	if into.LevelStats == nil {
		into.LevelStats = map[string][]enapi.LevelStatItem{}
	}
	for key, items := range next.LevelStats {
		into.LevelStats[key] = append(into.LevelStats[key], items...)
	}
	into.TotalPages = next.TotalPages
}

// gameStatisticsFromAPI maps the new statistics document onto the legacy shape.
//
// The engines disagree on layout: the new one keys result groups in a map, the
// legacy one ships an array of groups. Levels and LevelPlayers describe the
// level list; StatItems carries every group the engine published, including the
// aggregate ones that belong to no level.
func gameStatisticsFromAPI(stats *enapi.GameStatisticsResponse) *GameStatisticsResponse {
	if stats == nil {
		return nil
	}

	out := &GameStatisticsResponse{
		IsLevelNamesVisible: !stats.HideLevelsNames,
		ShowAdminWarning:    strings.TrimSpace(stats.AdminWarning) != "",
		PagerVisible:        stats.TotalPages > 1,
	}

	out.Levels = make([]LevelStatInfo, 0, len(stats.Levels))
	out.LevelPlayers = make([]LevelPlayerCount, 0, len(stats.Levels))
	for _, meta := range stats.Levels {
		count := len(statItemsForLevel(stats, meta))
		out.Levels = append(out.Levels, LevelStatInfo{
			LevelId:       meta.LevelID,
			LevelNumber:   meta.LevelNumber,
			LevelName:     meta.LevelName,
			Dismissed:     meta.Dismissed,
			PassedPlayers: count,
		})
		out.LevelPlayers = append(out.LevelPlayers, LevelPlayerCount{
			LevelNum: meta.LevelNumber,
			Count:    count,
		})
	}
	out.StatItems = statGroupsFromAPI(stats.LevelStats, correctionIndexFromAPI(stats.LevelCorrections))

	out.Game = &GameInfo{
		GameID:     stats.GameID,
		GameNum:    stats.GameNum,
		Title:      stats.GameTitle,
		GameTypeID: stats.GameTypeID,
		ZoneId:     stats.ZoneID,
		// The statistics document carries the sequence but not the rest of the
		// catalog entry; callers that need it read GetGameList.
		LevelsSequence:   stats.LevelsSequenceID,
		LevelsSequenceId: stats.LevelsSequenceID,
	}
	return out
}

// statGroupsFromAPI turns the keyed result map into the group array the legacy
// document uses.
//
// Every key is kept, not only those matching a level: the engine also publishes
// aggregate groups under negative keys (-1 total time, -2 net time), one row per
// player, and those are exactly the totals the statistics page shows. Groups are
// ordered by key so a Go map's random iteration cannot reshuffle the report.
func statGroupsFromAPI(levelStats map[string][]enapi.LevelStatItem, corrections correctionIndex) [][]StatItem {
	if len(levelStats) == 0 {
		return nil
	}
	keys := make([]string, 0, len(levelStats))
	for key := range levelStats {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		left, leftErr := strconv.Atoi(keys[i])
		right, rightErr := strconv.Atoi(keys[j])
		if leftErr == nil && rightErr == nil {
			return left < right
		}
		return keys[i] < keys[j]
	})

	groups := make([][]StatItem, 0, len(keys))
	for _, key := range keys {
		groups = append(groups, statItemsFromAPI(levelStats[key], corrections))
	}
	return groups
}

// correctionIndex holds the time corrections of a game keyed by the level and
// the participant they apply to.
//
// The legacy statistics document carried the correction inside every row, and
// consumers add it to SpentSeconds to get the standing time. The new engine
// publishes the same numbers once, in a separate list, so they are indexed here
// and folded back into the rows — dropping them ranks teams by raw level time
// and quietly disagrees with the official result.
type correctionIndex map[correctionKey]int

type correctionKey struct {
	levelID int
	teamID  int
	userID  int
}

func correctionIndexFromAPI(sums []enapi.LevelCorrectionSum) correctionIndex {
	if len(sums) == 0 {
		return nil
	}
	index := make(correctionIndex, len(sums))
	for _, sum := range sums {
		key := correctionKey{levelID: sum.LevelID, teamID: sum.TeamID, userID: sum.UserID}
		// Each row already is the sum for its key, so assignment — not
		// accumulation — is the right fold: a repeated row then costs nothing
		// instead of silently doubling a correction.
		index[key] = sum.CorrectionValue
	}
	return index
}

func (index correctionIndex) lookup(item enapi.LevelStatItem) (int, bool) {
	if index == nil {
		return 0, false
	}
	// The aggregate groups the engine publishes under negative keys are totals
	// it has already computed; a correction belongs to the level row it was
	// issued against, not to a sum of rows.
	if item.LevelID <= 0 && item.LevelNum <= 0 {
		return 0, false
	}
	// A game is played either by teams or by single players, so the row matches
	// on whichever identity the correction names. A correction keyed to a team
	// applies to that team's row on that level, which is what a per-team
	// correction means.
	for _, key := range []correctionKey{
		{levelID: item.LevelID, teamID: item.TeamID, userID: item.UserID},
		{levelID: item.LevelID, teamID: item.TeamID},
		{levelID: item.LevelID, userID: item.UserID},
	} {
		if key.teamID == 0 && key.userID == 0 {
			continue
		}
		if value, ok := index[key]; ok {
			return value, true
		}
	}
	return 0, false
}

// statItemsForLevel resolves a level's rows. The map is keyed by level number
// on the wire; the level ID is accepted as well so a future keying change does
// not silently empty the statistics.
func statItemsForLevel(stats *enapi.GameStatisticsResponse, meta enapi.StatLevelMeta) []enapi.LevelStatItem {
	if items, ok := stats.LevelStats[strconv.Itoa(meta.LevelNumber)]; ok {
		return items
	}
	if items, ok := stats.LevelStats[strconv.Itoa(meta.LevelID)]; ok {
		return items
	}
	return nil
}

func statItemsFromAPI(items []enapi.LevelStatItem, corrections correctionIndex) []StatItem {
	if len(items) == 0 {
		return nil
	}
	out := make([]StatItem, 0, len(items))
	for _, item := range items {
		row := StatItem{
			ActionTime:     dateTimeFromAPI(item.EnterDateTime),
			UserId:         item.UserID,
			LevelId:        item.LevelID,
			TeamId:         item.TeamID,
			UserName:       firstNonEmpty(item.UserName, item.UserLogin),
			TeamName:       item.TeamName,
			LevelNum:       item.LevelNum,
			LevelOrder:     item.LevelOrder,
			SpentSeconds:   item.SpentSeconds,
			SpentLevelTime: durationFromSeconds(item.SpentSeconds),
			PassType:       item.PassTypeID,
			Scores:         item.Scores,
		}
		if seconds, ok := corrections.lookup(item); ok {
			row.Corrections = durationFromSignedSeconds(seconds)
		}
		out = append(out, row)
	}
	return out
}

// durationFromSignedSeconds keeps the direction of a correction: a bonus takes
// time off and a penalty adds it, and collapsing the two would misreport the
// standing time by twice the correction.
func durationFromSignedSeconds(seconds int) *Duration {
	if seconds == 0 {
		return nil
	}
	if seconds > 0 {
		return durationFromSeconds(seconds)
	}
	negated := durationFromSeconds(-seconds)
	if negated == nil {
		return nil
	}
	return &Duration{
		Days:         -negated.Days,
		Hours:        -negated.Hours,
		Minutes:      -negated.Minutes,
		Seconds:      -negated.Seconds,
		TotalDays:    -negated.TotalDays,
		TotalHours:   -negated.TotalHours,
		TotalMinutes: -negated.TotalMinutes,
		TotalSeconds: -negated.TotalSeconds,
	}
}

func durationFromSeconds(seconds int) *Duration {
	if seconds <= 0 {
		return nil
	}
	return &Duration{
		Days:         seconds / 86400,
		Hours:        (seconds % 86400) / 3600,
		Minutes:      (seconds % 3600) / 60,
		Seconds:      seconds % 60,
		TotalDays:    float64(seconds) / 86400,
		TotalHours:   float64(seconds) / 3600,
		TotalMinutes: float64(seconds) / 60,
		TotalSeconds: float64(seconds),
	}
}

func (e *newEngine) GetProfile(ctx context.Context) (*Profile, error) {
	var session enapi.Session
	if err := e.c.api().GetJSON(ctx, "/auth/session", nil, &session); err != nil {
		return nil, err
	}
	userID := session.CurrentUserID()
	if userID == 0 {
		return nil, fmt.Errorf("encx: get profile: the session does not name a user")
	}

	user := session.User
	if user == nil || user.Login == "" {
		var fetched enapi.User
		if err := e.c.api().GetJSON(ctx, fmt.Sprintf("/users/%d", userID), nil, &fetched); err != nil {
			return nil, err
		}
		user = &fetched
	}
	return e.c.profileFromAPI(user), nil
}

func (c *Client) profileFromAPI(user *enapi.User) *Profile {
	profile := &Profile{
		ID:     user.ID,
		Login:  user.Login,
		Name:   strings.TrimSpace(strings.Join(nonEmpty(user.FirstName, user.LastName), " ")),
		Rank:   user.RankSentenceKey,
		TeamID: user.TeamID,
		Domain: c.domain,
		Points: strconv.FormatFloat(user.Points, 'f', -1, 64),
	}
	if user.Team != nil {
		profile.Team = user.Team.Name
		if profile.TeamID == 0 {
			profile.TeamID = user.Team.ID
		}
	}
	if user.Site != nil {
		profile.Location = siteLocation(user.Site)
		if user.Site.PrimaryDomain != "" {
			profile.Domain = user.Site.PrimaryDomain
		}
	}
	return profile
}

func siteLocation(site *enapi.Site) string {
	parts := make([]string, 0, 3)
	if site.City != nil && site.City.Name != "" {
		parts = append(parts, site.City.Name)
	}
	if site.Province != nil && site.Province.Name != "" {
		parts = append(parts, site.Province.Name)
	}
	if site.Country != nil && site.Country.Name != "" {
		parts = append(parts, site.Country.Name)
	}
	return strings.Join(parts, ", ")
}

func nonEmpty(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			out = append(out, s)
		}
	}
	return out
}
