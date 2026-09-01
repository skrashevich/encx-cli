package encx

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
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
	return levelIDByNumber(list, gameID, levelNum)
}

// levelIDByNumber resolves a level number against an already-read manager list.
func levelIDByNumber(list *enapi.AdminGameLevelsResponse, gameID, levelNum int) (int, error) {
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

// adminGamesPageLimit bounds the walk over the admin listing. An author with
// more than this many pages of games is far outside anything Encounter has, and
// the cap keeps a server that always reports another page from looping forever.
const adminGamesPageLimit = 100

func (e *newEngine) AdminGetGames(ctx context.Context) ([]AdminGame, error) {
	// The listing is paged and says how many pages there are, so all of them are
	// read. AdminGetGames takes no page argument, which means a caller has no way
	// to ask for the rest: returning the first page alone would be an unmarked
	// prefix of the answer.
	var games []AdminGame
	seen := make(map[int]bool)
	for page := 1; page <= adminGamesPageLimit; page++ {
		var resp enapi.AdminGamesListResponse
		q := url.Values{}
		if page > 1 {
			q.Set("page", strconv.Itoa(page))
		}
		if err := e.c.api().GetJSON(ctx, "/admin/games", q, &resp); err != nil {
			return nil, err
		}
		for _, item := range resp.Items {
			if seen[item.GameID] {
				continue
			}
			seen[item.GameID] = true
			// Status stays empty: the legacy manager page does not fill it
			// either, and the admin listing publishes only a numeric status_id
			// whose values the API does not document. Deriving a label from the
			// start and finish times would read as authoritative while being
			// wrong for a game that was cancelled.
			games = append(games, AdminGame{
				ID:     item.GameID,
				Number: item.GameNum,
				Title:  item.Title,
			})
		}
		if len(resp.Items) == 0 || page >= resp.TotalPages {
			if games == nil {
				games = []AdminGame{}
			}
			return games, nil
		}
	}
	// Falling out of the loop means the listing never said it was done. Handing
	// back what was collected would be the unmarked prefix this walk exists to
	// avoid, so it is reported instead.
	return nil, fmt.Errorf(
		"encx: список игр: движок отдаёт больше %d страниц — результат был бы неполным",
		adminGamesPageLimit)
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
	// The legacy list was ordered by construction — it came from the page in
	// document order — and callers index it positionally. The REST list is not
	// promised to arrive sorted, so the order is imposed here rather than left
	// to the server.
	sort.SliceStable(levels, func(i, j int) bool { return levels[i].Number < levels[j].Number })
	return levels, nil
}

func (e *newEngine) AdminCreateLevels(ctx context.Context, gameId, count int) error {
	if count <= 0 {
		return nil
	}
	list, err := e.adminLevelList(ctx, gameId)
	if err != nil {
		return err
	}
	if err := checkCanManipulateLevels(list, gameId, "создание уровней"); err != nil {
		return err
	}
	for i := 0; i < count; i++ {
		if err := e.c.api().PostJSON(ctx, adminLevelsPath(gameId), enapi.AdminCreateLevelRequest{}, nil); err != nil {
			return err
		}
	}
	return nil
}

func (e *newEngine) AdminDeleteLevel(ctx context.Context, gameId, levelNum int) error {
	list, err := e.adminLevelList(ctx, gameId)
	if err != nil {
		return err
	}
	if err := checkCanManipulateLevels(list, gameId, fmt.Sprintf("удаление уровня %d", levelNum)); err != nil {
		return err
	}
	levelID, err := levelIDByNumber(list, gameId, levelNum)
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
	list, err := e.adminLevelList(ctx, gameId)
	if err != nil {
		return err
	}
	operation := fmt.Sprintf("перестановка уровней %d и %d", level1, level2)
	if err := checkCanManipulateLevels(list, gameId, operation); err != nil {
		return err
	}
	order, err := levelIDOrder(list, gameId)
	if err != nil {
		return err
	}
	first, err := levelIDAt(order, gameId, level1)
	if err != nil {
		return err
	}
	second, err := levelIDAt(order, gameId, level2)
	if err != nil {
		return err
	}

	body := enapi.AdminExchangeLevelsRequest{Level1ID: first, Level2ID: second}
	if err := e.c.api().PostJSON(ctx, adminLevelsPath(gameId)+"/exchange", body, nil); err != nil {
		return err
	}

	want := append([]int(nil), order...)
	want[level1-1], want[level2-1] = want[level2-1], want[level1-1]
	return e.confirmLevelOrder(ctx, gameId, operation, order, want)
}

func (e *newEngine) AdminInsertLevel(ctx context.Context, gameId, src, dst int) error {
	list, err := e.adminLevelList(ctx, gameId)
	if err != nil {
		return err
	}
	operation := fmt.Sprintf("перенос уровня %d за уровень %d", src, dst)
	if err := checkCanManipulateLevels(list, gameId, operation); err != nil {
		return err
	}
	order, err := levelIDOrder(list, gameId)
	if err != nil {
		return err
	}
	srcID, err := levelIDAt(order, gameId, src)
	if err != nil {
		return err
	}
	// Both engines mean the same thing: put src after dst. The legacy form calls
	// its fields ddlInsertAfterSrc / ddlInsertAfterDst, and the REST route names
	// the level the moved one must follow, so dst maps straight across. dst 0 is
	// the legacy marker for "move it to the front".
	afterID := 0
	if dst > 0 {
		if afterID, err = levelIDAt(order, gameId, dst); err != nil {
			return err
		}
	}

	body := enapi.AdminPutLevelRequest{LevelID: srcID, AfterLevelID: afterID}
	if err := e.c.api().PostJSON(ctx, adminLevelsPath(gameId)+"/put", body, nil); err != nil {
		return err
	}

	want := moveLevelAfter(order, srcID, afterID)
	return e.confirmLevelOrder(ctx, gameId, operation, order, want)
}

// levelIDOrder returns the level ids in level-number order and rejects a list
// whose numbering is not the dense 1..n the level manager promises — reordering
// against a gapped list would move the wrong level.
func levelIDOrder(list *enapi.AdminGameLevelsResponse, gameID int) ([]int, error) {
	order := make([]int, len(list.Levels))
	for _, level := range list.Levels {
		if level.LevelNumber < 1 || level.LevelNumber > len(list.Levels) {
			return nil, fmt.Errorf("encx: game %d has level number %d in a list of %d levels",
				gameID, level.LevelNumber, len(list.Levels))
		}
		if order[level.LevelNumber-1] != 0 {
			return nil, fmt.Errorf("encx: game %d lists level number %d twice", gameID, level.LevelNumber)
		}
		order[level.LevelNumber-1] = level.LevelID
	}
	return order, nil
}

func levelIDAt(order []int, gameID, number int) (int, error) {
	if number < 1 || number > len(order) {
		return 0, fmt.Errorf("encx: game %d has no level %d", gameID, number)
	}
	return order[number-1], nil
}

// moveLevelAfter returns the order that results from taking levelID out and
// putting it back directly after afterID; afterID 0 moves it to the front.
//
// Asking for a level to follow itself leaves the game alone, which is what the
// legacy form did too.
func moveLevelAfter(order []int, levelID, afterID int) []int {
	if afterID == levelID {
		return append([]int(nil), order...)
	}
	moved := make([]int, 0, len(order))
	for _, id := range order {
		if id != levelID {
			moved = append(moved, id)
		}
	}
	if afterID == 0 {
		return append([]int{levelID}, moved...)
	}
	out := make([]int, 0, len(order))
	for _, id := range moved {
		out = append(out, id)
		if id == afterID {
			out = append(out, levelID)
		}
	}
	return out
}

// confirmLevelOrder re-reads the level manager and reports an error when the
// engine did not apply a reordering.
//
// The REST routes answer 204 whether or not they moved anything, and on the
// deployed backend they routinely move nothing at all. Reporting success then
// would leave the caller — an import, a level editor, a script — believing a
// game is ordered the way it asked for, so the outcome is verified instead of
// assumed.
//
// before is the order read just before the write, which is what tells "the
// engine ignored the request" apart from "the game ended up somewhere else".
func (e *newEngine) confirmLevelOrder(ctx context.Context, gameID int, operation string, before, want []int) error {
	list, err := e.adminLevelList(ctx, gameID)
	if err != nil {
		return fmt.Errorf("encx: %s: не удалось проверить результат: %w", operation, err)
	}
	got, err := levelIDOrder(list, gameID)
	if err != nil {
		return fmt.Errorf("encx: %s: не удалось проверить результат: %w", operation, err)
	}
	if slices.Equal(got, want) {
		return nil
	}
	if slices.Equal(got, before) {
		return fmt.Errorf(
			"encx: %s в игре %d не выполнена: движок ответил успехом, но порядок уровней не изменился "+
				"(%v, ожидалось %v); на развёрнутом бэкенде маршруты %s/exchange и %s/put "+
				"отвечают 204 и ничего не меняют",
			operation, gameID, got, want, adminLevelsPath(gameID), adminLevelsPath(gameID))
	}
	return fmt.Errorf(
		"encx: %s в игре %d дала не тот порядок: получено %v, ожидалось %v (было %v)",
		operation, gameID, got, want, before)
}

// checkCanManipulateLevels refuses a reordering the game itself says is not
// allowed, so a failure names its real cause instead of being reported as the
// backend ignoring the request.
func checkCanManipulateLevels(list *enapi.AdminGameLevelsResponse, gameID int, operation string) error {
	if list.CanManipulateLevels {
		return nil
	}
	return fmt.Errorf("encx: %s: игра %d не разрешает менять состав и порядок уровней", operation, gameID)
}

func (e *newEngine) AdminCloneLevels(ctx context.Context, gameId, count, likeLevel int) error {
	list, err := e.adminLevelList(ctx, gameId)
	if err != nil {
		return err
	}
	if err := checkCanManipulateLevels(list, gameId, fmt.Sprintf("клонирование уровня %d", likeLevel)); err != nil {
		return err
	}
	levelID, err := levelIDByNumber(list, gameId, likeLevel)
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
	return e.deleteSector(ctx, gameId, levelID, levelNum, sectorId)
}

// deleteSector removes one sector and classifies a refusal the way the legacy
// engine did.
//
// ErrSectorStarted is exported and callers branch on it with errors.Is, so the
// new engine has to be able to produce it. The API documents no error body for
// this case, but the observable condition is the same one the legacy client
// checked: the delete did not go through and the sector is still on the level.
//
// Two conditions, not one. The legacy client read the refusal off a specific
// message, so ErrSectorStarted meant what it says; inferring it from any failure
// the sector survives would label a 500 or a timeout an unremovable sector and
// send a caller that branches on it away from a retry that would have worked.
// Only a refusal — the statuses a server uses to say "not allowed" or
// "conflicts with current state" — is classified.
func (e *newEngine) deleteSector(ctx context.Context, gameID, levelID, levelNum, sectorID int) error {
	path := adminLevelPath(gameID, levelID, fmt.Sprintf("/sectors/%d", sectorID))
	err := e.c.api().Delete(ctx, path, nil, nil)
	if err == nil {
		return nil
	}
	// A sector that is already gone is the outcome this call wanted. The id came
	// from a read one request earlier, so a 404 here means somebody removed it in
	// between — and letting that abort a whole level clear would turn a harmless
	// race into a failure. The legacy client never saw it: it re-read the list
	// every round, so a vanished sector was simply not in it.
	if enapi.IsNotFound(err) {
		return nil
	}
	if !isSectorRefusal(err) {
		return err
	}
	editor, readErr := e.levelEditor(ctx, gameID, levelNum)
	if readErr != nil {
		return err
	}
	for _, sector := range editor.Sectors {
		if sector.SectorID == sectorID {
			return fmt.Errorf("%w (сектор %d): %v", ErrSectorStarted, sectorID, err)
		}
	}
	// The sector is gone despite the error, so the delete did happen. The
	// server's complaint is still worth a line: a delete that works while
	// reporting a failure is a partial-failure mode nobody would otherwise see.
	e.c.debugf("encx: delete sector %d reported %v but the sector is gone", sectorID, err)
	return nil
}

// isSectorRefusal reports whether the engine declined to delete a sector, as
// opposed to failing to try. 400 is what the deployed backend answers for the
// level operations it refuses ("dismiss not allowed"), 403 is a permission and
// 409 a state conflict; a 500 or a transport failure is neither.
func isSectorRefusal(err error) bool {
	apiErr, ok := enapi.AsAPIError(err)
	if !ok {
		return false
	}
	switch apiErr.Status {
	case http.StatusBadRequest, http.StatusForbidden, http.StatusConflict:
		return true
	}
	return false
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

// AdminClearLevelSectors deletes every sector of a level.
//
// A sector participants have already started cannot be removed. The legacy
// implementation kept going past it instead of abandoning the rest of the level,
// but it did not call the level cleared either: its retry loop came back to the
// survivors and reported ErrSectorStarted once they refused again. So a level
// that still has sectors is an error here, however many were removed —
// cmd/encli's scenario import writes the new sectors on top of whatever this
// call left behind, and "cleared" has to mean empty.
func (e *newEngine) AdminClearLevelSectors(ctx context.Context, gameID, levelNum int) error {
	editor, err := e.levelEditor(ctx, gameID, levelNum)
	if err != nil {
		return err
	}
	if editor.Level == nil {
		return fmt.Errorf("encx: game %d has no level %d", gameID, levelNum)
	}

	var refused error
	for _, sector := range editor.Sectors {
		err := e.deleteSector(ctx, gameID, editor.Level.LevelID, levelNum, sector.SectorID)
		switch {
		case err == nil:
		case errors.Is(err, ErrSectorStarted):
			refused = err
		default:
			return err
		}
	}
	return refused
}

// --- Level settings ---

func (e *newEngine) AdminGetLevelSettings(ctx context.Context, gameId, levelNum int) (*AdminLevelSettings, error) {
	editor, err := e.levelEditor(ctx, gameId, levelNum)
	if err != nil {
		return nil, err
	}
	autopassHours, autopassMinutes, autopassSeconds := splitClock(editor.TimeoutSec)
	// The autopass award carries its direction in its sign: the engine stores a
	// penalty as negative seconds and a bonus as positive ones (writing
	// award_is_penalty=true with 15 minutes reads back as -900). The legacy form
	// had a checkbox and an unsigned duration, so the sign becomes the flag and
	// the magnitude becomes the duration.
	penaltyHours, penaltyMinutes, penaltySeconds := splitClock(absInt(editor.TimeoutTimeAwardSec))
	periodHours, periodMinutes, periodSeconds := splitClock(editor.AttemptsPeriodSec)

	settings := &AdminLevelSettings{
		AutopassHours:         autopassHours,
		AutopassMinutes:       autopassMinutes,
		AutopassSeconds:       autopassSeconds,
		TimeoutPenalty:        editor.TimeoutTimeAwardSec < 0,
		PenaltyHours:          penaltyHours,
		PenaltyMinutes:        penaltyMinutes,
		PenaltySeconds:        penaltySeconds,
		AttemptsNumber:        editor.AttemptsNumber,
		AttemptsPeriodHours:   periodHours,
		AttemptsPeriodMinutes: periodMinutes,
		AttemptsPeriodSeconds: periodSeconds,
	}
	// PassingConditionID decides whether required_sectors_count means anything:
	// only condition 1 counts sectors. The field keeps its last value under the
	// other conditions, and echoing it would report "close 3 of 5 sectors" for a
	// level that in fact requires all of them.
	if editor.PassingConditionID == passingConditionSectorCount {
		settings.RequiredSectorsCount = editor.RequiredSectorsCount
	}
	// BlockTypeID: 1 user, 2 team; the legacy flag records only "per player".
	if editor.BlockTypeID == 1 {
		settings.ApplyForPlayer = 1
	}
	return settings, nil
}

// PassingConditionID values of the level editor: 0 every sector, 1 a given
// number of sectors, 2 a score. Only the first two have a legacy equivalent.
const (
	passingConditionAllSectors  = 0
	passingConditionSectorCount = 1
)

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func (e *newEngine) AdminUpdateAutopass(ctx context.Context, gameId, levelNum int, s AdminLevelSettings) error {
	levelID, err := e.resolveLevelID(ctx, gameId, levelNum)
	if err != nil {
		return err
	}
	// The engine splits what the legacy form expressed as one checkbox into two
	// switches: penalty_enabled says whether there is a timeout award at all,
	// award_is_penalty says which way it points. Deriving both from
	// TimeoutPenalty would make a positive award — the bonus AdminGetLevelSettings
	// now reads back — unwritable, so reading a level and saving it unchanged
	// would erase it.
	award := (s.PenaltyHours*60+s.PenaltyMinutes)*60 + s.PenaltySeconds
	body := enapi.AdminLevelAutoPassRequest{
		TimeoutHours:   s.AutopassHours,
		TimeoutMinutes: s.AutopassMinutes,
		TimeoutSeconds: s.AutopassSeconds,
		PenaltyEnabled: award != 0,
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
	condition := passingConditionSectorCount
	if requiredCount <= 0 {
		condition = passingConditionAllSectors
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
