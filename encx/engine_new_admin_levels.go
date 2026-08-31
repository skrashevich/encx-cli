package encx

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/skrashevich/encx-cli/encx/enapi"
)

// The new engine addresses levels by ID where the legacy admin pages addressed
// them by number, so every call resolves the number through the level manager
// first. Resolution is not cached: level numbers shift under create, delete and
// reorder, and a stale map would edit the wrong level.

func adminLevelsPath(gameID int) string {
	return fmt.Sprintf("/admin/games/%d/levels", gameID)
}

func adminLevelPath(gameID, levelID int, suffix string) string {
	return fmt.Sprintf("/admin/games/%d/levels/%d%s", gameID, levelID, suffix)
}

func (e *newEngine) adminLevelList(ctx context.Context, gameID int) (*enapi.AdminGameLevelsResponse, error) {
	var resp enapi.AdminGameLevelsResponse
	if err := e.c.api().GetJSON(ctx, adminLevelsPath(gameID), nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (e *newEngine) resolveLevelID(ctx context.Context, gameID, levelNum int) (int, error) {
	list, err := e.adminLevelList(ctx, gameID)
	if err != nil {
		return 0, err
	}
	for _, level := range list.Levels {
		if level.LevelNumber == levelNum {
			return level.LevelID, nil
		}
	}
	return 0, fmt.Errorf("encx: game %d has no level %d", gameID, levelNum)
}

// levelEditor reads the whole level in one request: on the new engine tasks,
// hints, bonuses, sectors, answers and messages all arrive together.
func (e *newEngine) levelEditor(ctx context.Context, gameID, levelNum int) (*enapi.AdminLevelEditorResponse, error) {
	levelID, err := e.resolveLevelID(ctx, gameID, levelNum)
	if err != nil {
		return nil, err
	}
	var editor enapi.AdminLevelEditorResponse
	if err := e.c.api().GetJSON(ctx, adminLevelPath(gameID, levelID, "/editor"), nil, &editor); err != nil {
		return nil, err
	}
	return &editor, nil
}

// --- Games and levels ---

func (e *newEngine) AdminGetGames(ctx context.Context) ([]AdminGame, error) {
	var resp enapi.AdminGamesListResponse
	if err := e.c.api().GetJSON(ctx, "/admin/games", nil, &resp); err != nil {
		return nil, err
	}
	games := make([]AdminGame, 0, len(resp.Items))
	for _, item := range resp.Items {
		// Status stays empty: the legacy manager page does not fill it either,
		// and the admin listing publishes only a numeric status_id whose values
		// the API does not document. Deriving a label from the start and finish
		// times would read as authoritative while being wrong for a game that
		// was cancelled.
		games = append(games, AdminGame{
			ID:     item.GameID,
			Number: item.GameNum,
			Title:  item.Title,
		})
	}
	return games, nil
}

func (e *newEngine) AdminGetLevels(ctx context.Context, gameId int) ([]AdminLevel, error) {
	list, err := e.adminLevelList(ctx, gameId)
	if err != nil {
		return nil, err
	}
	levels := make([]AdminLevel, 0, len(list.Levels))
	for _, level := range list.Levels {
		levels = append(levels, AdminLevel{
			Number: level.LevelNumber,
			Name:   level.LevelName,
			ID:     level.LevelID,
		})
	}
	return levels, nil
}

func (e *newEngine) AdminCreateLevels(ctx context.Context, gameId, count int) error {
	for i := 0; i < count; i++ {
		if err := e.c.api().PostJSON(ctx, adminLevelsPath(gameId), enapi.AdminCreateLevelRequest{}, nil); err != nil {
			return err
		}
	}
	return nil
}

func (e *newEngine) AdminDeleteLevel(ctx context.Context, gameId, levelNum int) error {
	levelID, err := e.resolveLevelID(ctx, gameId, levelNum)
	if err != nil {
		return err
	}
	return e.c.api().Delete(ctx, adminLevelPath(gameId, levelID, ""), nil, nil)
}

func (e *newEngine) AdminRenameLevels(ctx context.Context, gameId int, names map[int]string) error {
	if len(names) == 0 {
		return nil
	}
	list, err := e.adminLevelList(ctx, gameId)
	if err != nil {
		return err
	}
	byNumber := make(map[int]enapi.AdminLevelItem, len(list.Levels))
	for _, level := range list.Levels {
		byNumber[level.LevelNumber] = level
	}

	// Renames are applied in level order so a partial failure leaves a
	// predictable prefix applied rather than an arbitrary subset.
	numbers := make([]int, 0, len(names))
	for number := range names {
		numbers = append(numbers, number)
	}
	sort.Ints(numbers)

	for _, number := range numbers {
		level, ok := byNumber[number]
		if !ok {
			return fmt.Errorf("encx: game %d has no level %d", gameId, number)
		}
		body := enapi.AdminLevelMetaRequest{LevelName: names[number], Comment: level.Comment}
		if err := e.c.api().PutJSON(ctx, adminLevelPath(gameId, level.LevelID, "/meta"), body, nil); err != nil {
			return err
		}
	}
	return nil
}

func (e *newEngine) AdminSwapLevels(ctx context.Context, gameId, level1, level2 int) error {
	first, err := e.resolveLevelID(ctx, gameId, level1)
	if err != nil {
		return err
	}
	second, err := e.resolveLevelID(ctx, gameId, level2)
	if err != nil {
		return err
	}
	body := enapi.AdminExchangeLevelsRequest{Level1ID: first, Level2ID: second}
	return e.c.api().PostJSON(ctx, adminLevelsPath(gameId)+"/exchange", body, nil)
}

func (e *newEngine) AdminInsertLevel(ctx context.Context, gameId, src, dst int) error {
	list, err := e.adminLevelList(ctx, gameId)
	if err != nil {
		return err
	}
	byNumber := make(map[int]int, len(list.Levels))
	for _, level := range list.Levels {
		byNumber[level.LevelNumber] = level.LevelID
	}
	srcID, ok := byNumber[src]
	if !ok {
		return fmt.Errorf("encx: game %d has no level %d", gameId, src)
	}
	// Both engines mean the same thing: put src after dst. The legacy form calls
	// its fields ddlInsertAfterSrc / ddlInsertAfterDst, and the REST route names
	// the level the moved one must follow, so dst maps straight across.
	afterID := 0
	if dst > 0 {
		afterID, ok = byNumber[dst]
		if !ok {
			return fmt.Errorf("encx: game %d has no level %d", gameId, dst)
		}
	}
	body := enapi.AdminPutLevelRequest{LevelID: srcID, AfterLevelID: afterID}
	return e.c.api().PostJSON(ctx, adminLevelsPath(gameId)+"/put", body, nil)
}

func (e *newEngine) AdminCloneLevels(ctx context.Context, gameId, count, likeLevel int) error {
	levelID, err := e.resolveLevelID(ctx, gameId, likeLevel)
	if err != nil {
		return err
	}
	body := enapi.AdminCopyLevelsRequest{FromLevelID: levelID, Count: count}
	return e.c.api().PostJSON(ctx, adminLevelsPath(gameId)+"/copy", body, nil)
}

// --- Tasks ---

func (e *newEngine) AdminGetTaskIds(ctx context.Context, gameId, levelNum int) ([]int, error) {
	editor, err := e.levelEditor(ctx, gameId, levelNum)
	if err != nil {
		return nil, err
	}
	ids := make([]int, 0, len(editor.Tasks))
	for _, task := range editor.Tasks {
		ids = append(ids, task.TaskID)
	}
	return ids, nil
}

func (e *newEngine) AdminGetTask(ctx context.Context, gameId, levelNum, taskId int) (*AdminTask, error) {
	editor, err := e.levelEditor(ctx, gameId, levelNum)
	if err != nil {
		return nil, err
	}
	for _, task := range editor.Tasks {
		if task.TaskID != taskId {
			continue
		}
		return &AdminTask{
			Text:        task.TaskText,
			ReplaceNl:   task.ReplaceNlToBr,
			ForMemberID: memberIDFrom(task.ForUserID, task.ForTeamID),
		}, nil
	}
	return nil, fmt.Errorf("encx: level %d of game %d has no task %d", levelNum, gameId, taskId)
}

func (e *newEngine) AdminCreateTask(ctx context.Context, gameId, levelNum int, t AdminTask) error {
	levelID, err := e.resolveLevelID(ctx, gameId, levelNum)
	if err != nil {
		return err
	}
	return e.c.api().PostJSON(ctx, adminLevelPath(gameId, levelID, "/tasks"), taskRequest(t), nil)
}

func (e *newEngine) AdminUpdateTask(ctx context.Context, gameId, levelNum, taskId int, t AdminTask) error {
	levelID, err := e.resolveLevelID(ctx, gameId, levelNum)
	if err != nil {
		return err
	}
	path := adminLevelPath(gameId, levelID, fmt.Sprintf("/tasks/%d", taskId))
	return e.c.api().PutJSON(ctx, path, taskRequest(t), nil)
}

func (e *newEngine) AdminDeleteTask(ctx context.Context, gameId, levelNum, taskId int) error {
	levelID, err := e.resolveLevelID(ctx, gameId, levelNum)
	if err != nil {
		return err
	}
	path := adminLevelPath(gameId, levelID, fmt.Sprintf("/tasks/%d", taskId))
	return e.c.api().Delete(ctx, path, nil, nil)
}

func taskRequest(t AdminTask) enapi.AdminTaskRequest {
	return enapi.AdminTaskRequest{
		TaskText:      t.Text,
		ReplaceNlToBr: t.ReplaceNl,
		ForMemberID:   memberIDValue(t.ForMemberID),
	}
}

// --- Hints ---

func (e *newEngine) AdminGetHintIds(ctx context.Context, gameId, levelNum int) ([]int, error) {
	editor, err := e.levelEditor(ctx, gameId, levelNum)
	if err != nil {
		return nil, err
	}
	ids := make([]int, 0, len(editor.Helps)+len(editor.PenaltyHelps))
	for _, help := range editor.Helps {
		ids = append(ids, help.HelpID)
	}
	for _, help := range editor.PenaltyHelps {
		ids = append(ids, help.HelpID)
	}
	return ids, nil
}

func (e *newEngine) AdminGetHint(ctx context.Context, gameId, levelNum, hintId int) (*AdminHint, error) {
	editor, err := e.levelEditor(ctx, gameId, levelNum)
	if err != nil {
		return nil, err
	}
	for _, help := range append(append([]enapi.AdminHelpDTO{}, editor.Helps...), editor.PenaltyHelps...) {
		if help.HelpID != hintId {
			continue
		}
		days, hours, minutes, seconds := splitDuration(help.Timeout)
		penaltyHours, penaltyMinutes, penaltySeconds := splitClock(help.PenaltyTime)
		return &AdminHint{
			Text:           help.HelpText,
			Days:           days,
			Hours:          hours,
			Minutes:        minutes,
			Seconds:        seconds,
			ForMemberID:    memberIDFrom(help.ForUserID, help.ForTeamID),
			IsPenalty:      help.IsPenalty,
			PenaltyHours:   penaltyHours,
			PenaltyMinutes: penaltyMinutes,
			PenaltySeconds: penaltySeconds,
			PenaltyComment: help.PenaltyComment,
			RequestConfirm: help.RequestPenaltyConfirm,
		}, nil
	}
	return nil, fmt.Errorf("encx: level %d of game %d has no hint %d", levelNum, gameId, hintId)
}

func (e *newEngine) AdminCreateHint(ctx context.Context, gameId, levelNum int, h AdminHint) error {
	levelID, err := e.resolveLevelID(ctx, gameId, levelNum)
	if err != nil {
		return err
	}
	return e.c.api().PostJSON(ctx, adminLevelPath(gameId, levelID, "/helps"), helpRequest(h), nil)
}

func (e *newEngine) AdminUpdateHint(ctx context.Context, gameId, levelNum, hintId int, h AdminHint) error {
	levelID, err := e.resolveLevelID(ctx, gameId, levelNum)
	if err != nil {
		return err
	}
	path := adminLevelPath(gameId, levelID, fmt.Sprintf("/helps/%d", hintId))
	return e.c.api().PutJSON(ctx, path, helpRequest(h), nil)
}

func (e *newEngine) AdminDeleteHint(ctx context.Context, gameId, levelNum, hintId int) error {
	levelID, err := e.resolveLevelID(ctx, gameId, levelNum)
	if err != nil {
		return err
	}
	path := adminLevelPath(gameId, levelID, fmt.Sprintf("/helps/%d", hintId))
	return e.c.api().Delete(ctx, path, nil, nil)
}

func helpRequest(h AdminHint) enapi.AdminHelpRequest {
	return enapi.AdminHelpRequest{
		HelpText:              h.Text,
		Timeout:               ((h.Days*24+h.Hours)*60+h.Minutes)*60 + h.Seconds,
		ForMemberID:           memberIDValue(h.ForMemberID),
		IsPenalty:             h.IsPenalty,
		PenaltyTime:           (h.PenaltyHours*60+h.PenaltyMinutes)*60 + h.PenaltySeconds,
		PenaltyComment:        h.PenaltyComment,
		RequestPenaltyConfirm: h.RequestConfirm,
	}
}

// --- Bonuses ---

func (e *newEngine) AdminGetBonusIds(ctx context.Context, gameId, levelNum int) ([]int, error) {
	editor, err := e.levelEditor(ctx, gameId, levelNum)
	if err != nil {
		return nil, err
	}
	ids := make([]int, 0, len(editor.Bonuses))
	for _, bonus := range editor.Bonuses {
		ids = append(ids, bonus.BonusID)
	}
	return ids, nil
}

func (e *newEngine) AdminGetBonus(ctx context.Context, gameId, levelNum, bonusId int) (*AdminBonus, error) {
	editor, err := e.levelEditor(ctx, gameId, levelNum)
	if err != nil {
		return nil, err
	}
	for _, bonus := range editor.Bonuses {
		if bonus.BonusID != bonusId {
			continue
		}
		awardHours, awardMinutes, awardSeconds := splitClock(bonus.BonusTime)
		delayHours, delayMinutes, delaySeconds := splitClock(bonus.DelaySec)
		workHours, workMinutes, workSeconds := splitClock(bonus.LifeTimeSec)
		result := &AdminBonus{
			Name:         bonus.BonusName,
			Task:         bonus.Task,
			Hint:         bonus.BonusHelp,
			Answers:      append([]string(nil), bonus.Answers...),
			BonusFor:     memberIDFrom(bonus.UserID, bonus.TeamID),
			AwardHours:   awardHours,
			AwardMinutes: awardMinutes,
			AwardSeconds: awardSeconds,
			Negative:     bonus.Negative,
			DelayHours:   delayHours,
			DelayMinutes: delayMinutes,
			DelaySeconds: delaySeconds,
			WorkHours:    workHours,
			WorkMinutes:  workMinutes,
			WorkSeconds:  workSeconds,
		}
		if bonus.HasAbsoluteLimit {
			result.ValidFrom = bonus.ValidFrom
			result.ValidTo = bonus.ValidTo
		}
		// Mirror of bonusRequest: a game-wide bonus reports LevelID 0.
		if !bonus.AllLevels && len(bonus.LevelIDs) > 0 {
			result.LevelID = bonus.LevelIDs[0]
		}
		return result, nil
	}
	return nil, fmt.Errorf("encx: level %d of game %d has no bonus %d", levelNum, gameId, bonusId)
}

func (e *newEngine) AdminCreateBonus(ctx context.Context, gameId, levelNum int, b AdminBonus) error {
	levelID, err := e.resolveLevelID(ctx, gameId, levelNum)
	if err != nil {
		return err
	}
	path := adminLevelPath(gameId, levelID, "/bonuses")
	return e.c.api().PostJSON(ctx, path, bonusRequest(b, levelID), nil)
}

func (e *newEngine) AdminUpdateBonus(ctx context.Context, gameId, levelNum, bonusId int, b AdminBonus) error {
	levelID, err := e.resolveLevelID(ctx, gameId, levelNum)
	if err != nil {
		return err
	}
	path := adminLevelPath(gameId, levelID, fmt.Sprintf("/bonuses/%d", bonusId))
	return e.c.api().PutJSON(ctx, path, bonusRequest(b, levelID), nil)
}

func (e *newEngine) AdminDeleteBonus(ctx context.Context, gameId, levelNum, bonusId int) error {
	levelID, err := e.resolveLevelID(ctx, gameId, levelNum)
	if err != nil {
		return err
	}
	path := adminLevelPath(gameId, levelID, fmt.Sprintf("/bonuses/%d", bonusId))
	return e.c.api().Delete(ctx, path, nil, nil)
}

func bonusRequest(b AdminBonus, levelID int) enapi.AdminBonusRequest {
	req := enapi.AdminBonusRequest{
		BonusName:   b.Name,
		Task:        b.Task,
		BonusHelp:   b.Hint,
		Answers:     append([]string(nil), b.Answers...),
		BonusTime:   (b.AwardHours*60+b.AwardMinutes)*60 + b.AwardSeconds,
		Negative:    b.Negative,
		ForMemberID: memberIDValue(b.BonusFor),
	}
	// AdminBonus.LevelID carries the legacy rbAllLevels choice: zero and -1 mean
	// "the whole game", anything else names one level. Sending the level the
	// caller happens to be editing instead would quietly narrow a game-wide
	// bonus on every read-modify-write.
	if b.LevelID <= 0 {
		req.AllLevels = true
	} else {
		req.LevelIDs = []int{b.LevelID}
	}
	if b.ValidFrom != "" || b.ValidTo != "" {
		req.HasAbsoluteLimit = true
		req.ValidFrom = b.ValidFrom
		req.ValidTo = b.ValidTo
	}
	if delay := (b.DelayHours*60+b.DelayMinutes)*60 + b.DelaySeconds; delay > 0 {
		req.HasDelay = true
		req.DelaySec = delay
	}
	if life := (b.WorkHours*60+b.WorkMinutes)*60 + b.WorkSeconds; life > 0 {
		req.HasRelativeLimit = true
		req.LifeTimeSec = life
	}
	return req
}

// --- Level messages ---

func (e *newEngine) AdminGetMessageIds(ctx context.Context, gameId, levelNum int) ([]int, error) {
	editor, err := e.levelEditor(ctx, gameId, levelNum)
	if err != nil {
		return nil, err
	}
	ids := make([]int, 0, len(editor.Messages))
	for _, message := range editor.Messages {
		ids = append(ids, message.MessageID)
	}
	return ids, nil
}

func (e *newEngine) AdminGetMessage(ctx context.Context, gameId, levelNum, messageId int) (*AdminGameMessage, error) {
	editor, err := e.levelEditor(ctx, gameId, levelNum)
	if err != nil {
		return nil, err
	}
	for _, message := range editor.Messages {
		if message.MessageID != messageId {
			continue
		}
		result := &AdminGameMessage{
			ID:            message.MessageID,
			Text:          message.MessageText,
			ReplaceNlToBr: message.ReplaceNlToBr,
			LevelIDs:      append([]int(nil), message.LevelIDs...),
		}
		if message.AllLevels {
			result.ShowOnLevelsMode = 1
		} else {
			result.ShowOnLevelsMode = 2
		}
		if message.RequiredPoints > 0 {
			result.RequiredPoints = strconv.Itoa(message.RequiredPoints)
		}
		return result, nil
	}
	return nil, fmt.Errorf("encx: level %d of game %d has no message %d", levelNum, gameId, messageId)
}

func (e *newEngine) AdminCreateMessage(ctx context.Context, gameId, levelID int, m AdminGameMessage) error {
	path := adminLevelPath(gameId, levelID, "/messages")
	return e.c.api().PostJSON(ctx, path, messageRequest(m, levelID), nil)
}

func (e *newEngine) AdminUpdateMessage(ctx context.Context, gameId, levelNum, messageId int, m AdminGameMessage) error {
	levelID, err := e.resolveLevelID(ctx, gameId, levelNum)
	if err != nil {
		return err
	}
	path := adminLevelPath(gameId, levelID, fmt.Sprintf("/messages/%d", messageId))
	return e.c.api().PutJSON(ctx, path, messageRequest(m, levelID), nil)
}

func (e *newEngine) AdminDeleteMessage(ctx context.Context, gameId, levelNum, messageId int) error {
	levelID, err := e.resolveLevelID(ctx, gameId, levelNum)
	if err != nil {
		return err
	}
	path := adminLevelPath(gameId, levelID, fmt.Sprintf("/messages/%d", messageId))
	return e.c.api().Delete(ctx, path, nil, nil)
}

func messageRequest(m AdminGameMessage, levelID int) enapi.AdminMessageRequest {
	// ShowOnLevelsMode is optional in AdminGameMessage, and the legacy form
	// treats anything other than 2 as "show on all levels" — including the zero
	// value that callers leave unset.
	req := enapi.AdminMessageRequest{
		MessageText:   m.Text,
		ReplaceNlToBr: m.ReplaceNlToBr,
		AllLevels:     m.ShowOnLevelsMode != 2,
	}
	if !req.AllLevels {
		req.LevelIDs = append([]int(nil), m.LevelIDs...)
		if len(req.LevelIDs) == 0 && levelID > 0 {
			req.LevelIDs = []int{levelID}
		}
	}
	if points, err := strconv.Atoi(strings.TrimSpace(m.RequiredPoints)); err == nil {
		req.RequiredPoints = points
	}
	return req
}

// --- Sectors and answers ---

func (e *newEngine) AdminGetSectorRefs(ctx context.Context, gameId, levelNum int) ([]AdminSector, error) {
	editor, err := e.levelEditor(ctx, gameId, levelNum)
	if err != nil {
		return nil, err
	}
	sectors := make([]AdminSector, 0, len(editor.Sectors))
	for _, sector := range editor.Sectors {
		sectors = append(sectors, AdminSector{ID: sector.SectorID, Name: sector.SectorName})
	}
	return sectors, nil
}

func (e *newEngine) AdminGetSectorAnswers(ctx context.Context, gameId, levelNum int) ([]AdminSector, error) {
	editor, err := e.levelEditor(ctx, gameId, levelNum)
	if err != nil {
		return nil, err
	}
	bySector := make(map[int][]string, len(editor.Sectors))
	for _, answer := range editor.Answers {
		bySector[answer.SectorID] = append(bySector[answer.SectorID], answer.AnswerText)
	}
	sectors := make([]AdminSector, 0, len(editor.Sectors))
	for _, sector := range editor.Sectors {
		sectors = append(sectors, AdminSector{
			ID:      sector.SectorID,
			Name:    sector.SectorName,
			Answers: bySector[sector.SectorID],
		})
	}
	return sectors, nil
}

func (e *newEngine) AdminCreateSector(ctx context.Context, gameId, levelNum int, s AdminSector) error {
	levelID, err := e.resolveLevelID(ctx, gameId, levelNum)
	if err != nil {
		return err
	}
	body := enapi.AdminSectorRequest{SectorName: s.Name}
	var created enapi.AdminSectorDTO
	if err := e.c.api().PostJSON(ctx, adminLevelPath(gameId, levelID, "/sectors"), body, &created); err != nil {
		return err
	}
	if len(s.Answers) == 0 {
		return nil
	}
	// A sector without its answers is an empty shell, so the answers are added
	// in the same call the caller made.
	return e.addAnswers(ctx, gameId, levelID, created.SectorID, s.Answers, s.ForMemberID)
}

func (e *newEngine) AdminUpdateSector(ctx context.Context, gameId, levelNum, sectorId int, s AdminSector) error {
	levelID, err := e.resolveLevelID(ctx, gameId, levelNum)
	if err != nil {
		return err
	}
	body := enapi.AdminSectorRequest{SectorName: s.Name}
	path := adminLevelPath(gameId, levelID, fmt.Sprintf("/sectors/%d", sectorId))
	if err := e.c.api().PutJSON(ctx, path, body, nil); err != nil {
		return err
	}
	if len(s.Answers) == 0 {
		return nil
	}
	return e.replaceAnswers(ctx, gameId, levelID, levelNum, sectorId, s.Answers, s.ForMemberID)
}

func (e *newEngine) AdminDeleteSector(ctx context.Context, gameId, levelNum, sectorId int) error {
	levelID, err := e.resolveLevelID(ctx, gameId, levelNum)
	if err != nil {
		return err
	}
	path := adminLevelPath(gameId, levelID, fmt.Sprintf("/sectors/%d", sectorId))
	return e.c.api().Delete(ctx, path, nil, nil)
}

func (e *newEngine) AdminAddSectorAnswers(ctx context.Context, gameId, levelNum, sectorId int, answers []string) error {
	levelID, err := e.resolveLevelID(ctx, gameId, levelNum)
	if err != nil {
		return err
	}
	return e.addAnswers(ctx, gameId, levelID, sectorId, answers, "")
}

func (e *newEngine) addAnswers(ctx context.Context, gameID, levelID, sectorID int, answers []string, forMember string) error {
	items := make([]enapi.AdminAnswerBatchItem, 0, len(answers))
	for _, answer := range answers {
		items = append(items, enapi.AdminAnswerBatchItem{
			AnswerText:  answer,
			ForMemberID: memberIDValue(forMember),
		})
	}
	if len(items) == 0 {
		return nil
	}
	body := enapi.AdminAnswersBatchRequest{SectorID: sectorID, Answers: items}
	return e.c.api().PostJSON(ctx, adminLevelPath(gameID, levelID, "/answers/batch"), body, nil)
}

// replaceAnswers makes the sector's answers match the requested list, which is
// what the legacy form did when it saved a sector.
func (e *newEngine) replaceAnswers(ctx context.Context, gameID, levelID, levelNum, sectorID int, answers []string, forMember string) error {
	editor, err := e.levelEditor(ctx, gameID, levelNum)
	if err != nil {
		return err
	}
	wanted := make(map[string]bool, len(answers))
	for _, answer := range answers {
		wanted[answer] = true
	}

	existing := make(map[string]bool)
	for _, answer := range editor.Answers {
		if answer.SectorID != sectorID {
			continue
		}
		if wanted[answer.AnswerText] {
			existing[answer.AnswerText] = true
			continue
		}
		path := adminLevelPath(gameID, levelID, fmt.Sprintf("/answers/%d", answer.AnswerID))
		if err := e.c.api().Delete(ctx, path, nil, nil); err != nil {
			return err
		}
	}

	missing := make([]string, 0, len(answers))
	for _, answer := range answers {
		if !existing[answer] {
			missing = append(missing, answer)
		}
	}
	return e.addAnswers(ctx, gameID, levelID, sectorID, missing, forMember)
}

func (e *newEngine) AdminClearLevelSectors(ctx context.Context, gameID, levelNum int) error {
	editor, err := e.levelEditor(ctx, gameID, levelNum)
	if err != nil {
		return err
	}
	if editor.Level == nil {
		return fmt.Errorf("encx: game %d has no level %d", gameID, levelNum)
	}
	for _, sector := range editor.Sectors {
		path := adminLevelPath(gameID, editor.Level.LevelID, fmt.Sprintf("/sectors/%d", sector.SectorID))
		if err := e.c.api().Delete(ctx, path, nil, nil); err != nil {
			return err
		}
	}
	return nil
}

// --- Level settings ---

func (e *newEngine) AdminGetLevelSettings(ctx context.Context, gameId, levelNum int) (*AdminLevelSettings, error) {
	editor, err := e.levelEditor(ctx, gameId, levelNum)
	if err != nil {
		return nil, err
	}
	autopassHours, autopassMinutes, autopassSeconds := splitClock(editor.TimeoutSec)
	penaltyHours, penaltyMinutes, penaltySeconds := splitClock(editor.TimeoutTimeAwardSec)
	periodHours, periodMinutes, periodSeconds := splitClock(editor.AttemptsPeriodSec)

	settings := &AdminLevelSettings{
		AutopassHours:         autopassHours,
		AutopassMinutes:       autopassMinutes,
		AutopassSeconds:       autopassSeconds,
		TimeoutPenalty:        editor.TimeoutTimeAwardSec > 0,
		PenaltyHours:          penaltyHours,
		PenaltyMinutes:        penaltyMinutes,
		PenaltySeconds:        penaltySeconds,
		AttemptsNumber:        editor.AttemptsNumber,
		AttemptsPeriodHours:   periodHours,
		AttemptsPeriodMinutes: periodMinutes,
		AttemptsPeriodSeconds: periodSeconds,
		RequiredSectorsCount:  editor.RequiredSectorsCount,
	}
	// BlockTypeID: 1 user, 2 team; the legacy flag records only "per player".
	if editor.BlockTypeID == 1 {
		settings.ApplyForPlayer = 1
	}
	return settings, nil
}

func (e *newEngine) AdminUpdateAutopass(ctx context.Context, gameId, levelNum int, s AdminLevelSettings) error {
	levelID, err := e.resolveLevelID(ctx, gameId, levelNum)
	if err != nil {
		return err
	}
	body := enapi.AdminLevelAutoPassRequest{
		TimeoutHours:   s.AutopassHours,
		TimeoutMinutes: s.AutopassMinutes,
		TimeoutSeconds: s.AutopassSeconds,
		PenaltyEnabled: s.TimeoutPenalty,
		PenaltyHours:   s.PenaltyHours,
		PenaltyMinutes: s.PenaltyMinutes,
		PenaltySeconds: s.PenaltySeconds,
		AwardIsPenalty: s.TimeoutPenalty,
	}
	return e.c.api().PutJSON(ctx, adminLevelPath(gameId, levelID, "/autopass"), body, nil)
}

func (e *newEngine) AdminUpdateAnswerBlock(ctx context.Context, gameId, levelNum int, s AdminLevelSettings) error {
	levelID, err := e.resolveLevelID(ctx, gameId, levelNum)
	if err != nil {
		return err
	}
	blockType := 2 // team
	if s.ApplyForPlayer == 1 {
		blockType = 1
	}
	body := enapi.AdminLevelSettingsRequest{
		Section:               enapi.AdminSettingsSectionBlocking,
		AttemptsNumber:        s.AttemptsNumber,
		AttemptsCount:         s.AttemptsNumber,
		AttemptsPeriodHours:   s.AttemptsPeriodHours,
		AttemptsPeriodMinutes: s.AttemptsPeriodMinutes,
		AttemptsPeriodSeconds: s.AttemptsPeriodSeconds,
		BlockTypeID:           blockType,
	}
	return e.c.api().PutJSON(ctx, adminLevelPath(gameId, levelID, "/settings"), body, nil)
}

func (e *newEngine) AdminUpdateSectorCompletion(ctx context.Context, gameId, levelNum, requiredCount int) error {
	levelID, err := e.resolveLevelID(ctx, gameId, levelNum)
	if err != nil {
		return err
	}
	// PassingConditionID: 0 all sectors, 1 a given count.
	condition := 1
	if requiredCount <= 0 {
		condition = 0
	}
	body := enapi.AdminLevelSettingsRequest{
		Section:              enapi.AdminSettingsSectionSectors,
		PassingConditionID:   condition,
		RequiredSectorsCount: requiredCount,
	}
	return e.c.api().PutJSON(ctx, adminLevelPath(gameId, levelID, "/settings"), body, nil)
}

func (e *newEngine) AdminGetComment(ctx context.Context, gameId, levelNum int) (string, string, error) {
	list, err := e.adminLevelList(ctx, gameId)
	if err != nil {
		return "", "", err
	}
	for _, level := range list.Levels {
		if level.LevelNumber == levelNum {
			return level.LevelName, level.Comment, nil
		}
	}
	return "", "", fmt.Errorf("encx: game %d has no level %d", gameId, levelNum)
}

func (e *newEngine) AdminUpdateComment(ctx context.Context, gameId, levelNum int, name, comment string) error {
	levelID, err := e.resolveLevelID(ctx, gameId, levelNum)
	if err != nil {
		return err
	}
	body := enapi.AdminLevelMetaRequest{LevelName: name, Comment: comment}
	return e.c.api().PutJSON(ctx, adminLevelPath(gameId, levelID, "/meta"), body, nil)
}

// --- Shared helpers ---

// memberIDFrom renders the legacy ForMember dropdown value: 0 means everyone,
// otherwise the user or team the item is addressed to.
func memberIDFrom(userID, teamID int) string {
	if userID > 0 {
		return strconv.Itoa(userID)
	}
	if teamID > 0 {
		return strconv.Itoa(teamID)
	}
	return "0"
}

func memberIDValue(forMember string) int {
	id, err := strconv.Atoi(strings.TrimSpace(forMember))
	if err != nil || id < 0 {
		return 0
	}
	return id
}

// splitClock breaks seconds into hours, minutes and seconds; hours are not
// folded into days, matching the legacy admin forms.
func splitClock(total int) (hours, minutes, seconds int) {
	if total <= 0 {
		return 0, 0, 0
	}
	return total / 3600, (total % 3600) / 60, total % 60
}

// splitDuration breaks seconds into days, hours, minutes and seconds, which is
// what AdminHint records for hint timeouts.
func splitDuration(total int) (days, hours, minutes, seconds int) {
	if total <= 0 {
		return 0, 0, 0, 0
	}
	return total / 86400, (total % 86400) / 3600, (total % 3600) / 60, total % 60
}
