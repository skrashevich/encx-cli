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

func (e *newEngine) GetGameStatistics(ctx context.Context, gameId int) (*GameStatisticsResponse, error) {
	var stats enapi.GameStatisticsResponse
	q := url.Values{}
	q.Set("lang", e.c.lang)
	path := fmt.Sprintf("/games/%d/statistics", gameId)
	if err := e.c.api().GetJSON(ctx, path, q, &stats); err != nil {
		return nil, err
	}
	return gameStatisticsFromAPI(&stats), nil
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
	out.StatItems = statGroupsFromAPI(stats.LevelStats)

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
func statGroupsFromAPI(levelStats map[string][]enapi.LevelStatItem) [][]StatItem {
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
		groups = append(groups, statItemsFromAPI(levelStats[key]))
	}
	return groups
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

func statItemsFromAPI(items []enapi.LevelStatItem) []StatItem {
	if len(items) == 0 {
		return nil
	}
	out := make([]StatItem, 0, len(items))
	for _, item := range items {
		out = append(out, StatItem{
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
		})
	}
	return out
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
