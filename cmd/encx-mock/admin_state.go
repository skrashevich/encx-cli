package main

import (
	"fmt"
	"sync"

	"github.com/skrashevich/encx-cli/encx/scenario"
)

// The admin state is the editable half of the mock's game: levels and their
// tasks, hints, bonuses, sectors, answers, messages and settings, plus the game
// editor fields. Both engines serve it, so `encli -engine legacy` and
// `-engine new` can be compared against one source of truth — the same principle
// the player-facing routes already follow.
//
// Field shapes come from responses captured on live domains, not from the
// specification: docs/newengine/swagger.json has been wrong about this API more
// than once, and a mock written from it would repeat exactly the assumptions the
// live audit disproved.

// Identifier bases keep the mock's ids in the range the real engines use, which
// makes a captured request recognisable at a glance.
const (
	adminLevelIDBase   = 316000
	adminTaskIDBase    = 133000
	adminHelpIDBase    = 266000
	adminBonusIDBase   = 243000
	adminSectorIDBase  = 308000
	adminAnswerIDBase  = 626000
	adminMessageIDBase = 4300
)

// Level settings enumerations, as measured on the deployed engine.
const (
	blockTypeUser = 1
	blockTypeTeam = 2

	passingConditionAllSectors  = 0
	passingConditionSectorCount = 1
)

type adminState struct {
	mu sync.Mutex

	gameID  int
	gameNum int
	title   string
	descr   string
	authors []string

	startDateTime   string
	finishDateTime  string
	requestLastDate string
	acceptRateFrom  string

	prize             int
	maxPlayers        int
	maxTeamMembers    int
	showFee           int
	certificateMode   int
	certificatePlaces int
	statAvailability  int
	scenarioAvail     int
	afc               float64
	isModerated       bool
	showFinishPlace   bool

	started bool

	// reorderIsNoOp reproduces the deployed backend, whose level exchange and
	// put routes answer 204 and change nothing. It is off by default so the mock
	// is a reference implementation; turn it on to exercise the client's
	// verification path.
	reorderIsNoOp bool

	// refuseSectorDeletes reproduces a level whose sectors participants have
	// started: the engine declines to remove them and they stay on the level.
	refuseSectorDeletes bool

	levels []*adminLevel

	nextLevel, nextTask, nextHelp, nextBonus, nextSector, nextAnswer, nextMessage int
}

type adminLevel struct {
	id      int
	name    string
	comment string

	tasks    []*adminTask
	helps    []*adminHelp
	bonuses  []*adminBonus
	sectors  []*adminSector
	answers  []*adminAnswer
	messages []*adminMessage

	timeoutSec int
	// timeoutAwardSec carries its direction in its sign, the way the engine
	// stores it: negative is a penalty, positive a bonus.
	timeoutAwardSec int

	attemptsNumber    int
	attemptsPeriodSec int
	blockTypeID       int

	passingConditionID   int
	requiredSectorsCount int
}

type adminTask struct {
	id          int
	text        string
	replaceNl   bool
	forMemberID int
}

type adminHelp struct {
	id             int
	number         int
	text           string
	timeout        int
	isPenalty      bool
	penaltyTime    int
	penaltyComment string
	requestConfirm bool
	forMemberID    int
}

type adminBonus struct {
	id        int
	name      string
	task      string
	help      string
	answers   []string
	bonusTime int
	negative  bool

	allLevels bool
	levelIDs  []int

	hasAbsoluteLimit bool
	validFrom        string
	validTo          string
	hasDelay         bool
	delaySec         int
	hasRelativeLimit bool
	lifeTimeSec      int

	forMemberID int
}

type adminSector struct {
	id   int
	name string
}

type adminAnswer struct {
	id          int
	sectorID    int
	text        string
	forMemberID int
}

type adminMessage struct {
	id             int
	text           string
	replaceNlToBr  bool
	allLevels      bool
	levelIDs       []int
	requiredPoints int
}

// newAdminState builds the editable game from whatever the mock is already
// serving to players, so the two halves describe the same game.
func newAdminState(gameID, levelCount int, title string, doc *scenario.Document, reorderIsNoOp bool) *adminState {
	st := &adminState{
		gameID:           gameID,
		gameNum:          gameID % 100000,
		title:            title,
		descr:            "Mock game served by encx-mock",
		authors:          []string{mockAdminLogin},
		startDateTime:    mockAdminStart,
		finishDateTime:   mockAdminFinish,
		requestLastDate:  mockAdminStart,
		acceptRateFrom:   mockAdminStart,
		maxPlayers:       0,
		maxTeamMembers:   0,
		statAvailability: 0,
		scenarioAvail:    0,
		reorderIsNoOp:    reorderIsNoOp,
		nextLevel:        adminLevelIDBase,
		nextTask:         adminTaskIDBase,
		nextHelp:         adminHelpIDBase,
		nextBonus:        adminBonusIDBase,
		nextSector:       adminSectorIDBase,
		nextAnswer:       adminAnswerIDBase,
		nextMessage:      adminMessageIDBase,
	}

	for i := 0; i < levelCount; i++ {
		level := st.newLevel()
		level.name = fmt.Sprintf("Mock level %d", i+1)
		if doc != nil && i < len(doc.Levels) {
			st.seedFromScenario(level, doc.Levels[i])
		}
		st.levels = append(st.levels, level)
	}
	return st
}

// seedFromScenario copies what the scenario export describes onto a level, so a
// mock started with -scenario serves the same content through the admin routes.
func (st *adminState) seedFromScenario(level *adminLevel, src scenario.Level) {
	if src.Name != "" {
		level.name = src.Name
	}
	level.comment = src.Comment
	level.timeoutSec = src.AutopassSecond
	if src.AutopassPenaltySecond > 0 {
		level.timeoutAwardSec = -src.AutopassPenaltySecond
	}
	level.requiredSectorsCount = src.RequiredSectorsCount
	if src.RequiredSectorsCount > 0 {
		level.passingConditionID = passingConditionSectorCount
	}

	for _, text := range src.Tasks {
		level.tasks = append(level.tasks, &adminTask{id: st.takeTaskID(), text: text, replaceNl: true})
	}
	for _, hint := range src.Hints {
		level.helps = append(level.helps, &adminHelp{
			id: st.takeHelpID(), number: len(level.helps) + 1,
			text: hint.Text, timeout: hint.DelaySeconds,
		})
	}
	for _, hint := range src.PenaltyHints {
		level.helps = append(level.helps, &adminHelp{
			id: st.takeHelpID(), number: len(level.helps) + 1,
			text: hint.Text, timeout: hint.DelaySeconds, isPenalty: true,
			penaltyTime: hint.PenaltySeconds, penaltyComment: hint.Comment,
			requestConfirm: hint.RequestConfirm,
		})
	}
	for i, sector := range src.Sectors {
		created := &adminSector{id: st.takeSectorID(), name: sector.Name}
		level.sectors = append(level.sectors, created)
		answers := sector.Answers
		if i < len(src.SectorAnswers) && len(src.SectorAnswers[i]) > 0 {
			answers = src.SectorAnswers[i]
		}
		for _, text := range answers {
			level.answers = append(level.answers, &adminAnswer{
				id: st.takeAnswerID(), sectorID: created.id, text: text,
			})
		}
	}
	for _, bonus := range src.Bonuses {
		level.bonuses = append(level.bonuses, &adminBonus{
			id: st.takeBonusID(), name: bonus.Name, task: bonus.Task, help: bonus.Hint,
			answers:   append([]string(nil), bonus.Answers...),
			bonusTime: bonus.AwardSeconds, levelIDs: []int{level.id},
		})
	}
}

func (st *adminState) newLevel() *adminLevel {
	return &adminLevel{
		id:                 st.takeLevelID(),
		blockTypeID:        blockTypeUser,
		passingConditionID: passingConditionSectorCount,
	}
}

func (st *adminState) takeLevelID() int   { st.nextLevel++; return st.nextLevel }
func (st *adminState) takeTaskID() int    { st.nextTask++; return st.nextTask }
func (st *adminState) takeHelpID() int    { st.nextHelp++; return st.nextHelp }
func (st *adminState) takeBonusID() int   { st.nextBonus++; return st.nextBonus }
func (st *adminState) takeSectorID() int  { st.nextSector++; return st.nextSector }
func (st *adminState) takeAnswerID() int  { st.nextAnswer++; return st.nextAnswer }
func (st *adminState) takeMessageID() int { st.nextMessage++; return st.nextMessage }

// levelByNumber resolves a 1-based level number.
func (st *adminState) levelByNumber(number int) *adminLevel {
	if number < 1 || number > len(st.levels) {
		return nil
	}
	return st.levels[number-1]
}

func (st *adminState) levelByID(id int) (*adminLevel, int) {
	for i, level := range st.levels {
		if level.id == id {
			return level, i + 1
		}
	}
	return nil, 0
}

func (st *adminState) numberOf(level *adminLevel) int {
	for i, existing := range st.levels {
		if existing == level {
			return i + 1
		}
	}
	return 0
}

// exchange swaps two levels by id, the operation POST /levels/exchange names.
func (st *adminState) exchange(first, second int) bool {
	a, _ := st.levelByID(first)
	b, _ := st.levelByID(second)
	if a == nil || b == nil {
		return false
	}
	if st.reorderIsNoOp {
		return true
	}
	i, j := st.numberOf(a)-1, st.numberOf(b)-1
	st.levels[i], st.levels[j] = st.levels[j], st.levels[i]
	return true
}

// putAfter moves a level so it follows afterID; afterID 0 moves it to the front.
func (st *adminState) putAfter(levelID, afterID int) bool {
	moved, _ := st.levelByID(levelID)
	if moved == nil {
		return false
	}
	if afterID != 0 {
		if target, _ := st.levelByID(afterID); target == nil {
			return false
		}
	}
	if st.reorderIsNoOp {
		return true
	}

	rest := make([]*adminLevel, 0, len(st.levels))
	for _, level := range st.levels {
		if level != moved {
			rest = append(rest, level)
		}
	}
	if afterID == 0 || afterID == levelID {
		if afterID == levelID {
			return true
		}
		st.levels = append([]*adminLevel{moved}, rest...)
		return true
	}
	out := make([]*adminLevel, 0, len(st.levels))
	for _, level := range rest {
		out = append(out, level)
		if level.id == afterID {
			out = append(out, moved)
		}
	}
	st.levels = out
	return true
}

func (st *adminState) deleteLevel(id int) bool {
	for i, level := range st.levels {
		if level.id == id {
			st.levels = append(st.levels[:i], st.levels[i+1:]...)
			return true
		}
	}
	return false
}

// answersOf returns a level's answers for one sector, in insertion order.
func (level *adminLevel) answersOf(sectorID int) []*adminAnswer {
	out := make([]*adminAnswer, 0, len(level.answers))
	for _, answer := range level.answers {
		if answer.sectorID == sectorID {
			out = append(out, answer)
		}
	}
	return out
}

func (level *adminLevel) sectorByID(id int) *adminSector {
	for _, sector := range level.sectors {
		if sector.id == id {
			return sector
		}
	}
	return nil
}

func (level *adminLevel) deleteSector(id int) bool {
	for i, sector := range level.sectors {
		if sector.id != id {
			continue
		}
		level.sectors = append(level.sectors[:i], level.sectors[i+1:]...)
		kept := level.answers[:0]
		for _, answer := range level.answers {
			if answer.sectorID != id {
				kept = append(kept, answer)
			}
		}
		level.answers = kept
		return true
	}
	return false
}

func (level *adminLevel) deleteAnswer(id int) bool {
	for i, answer := range level.answers {
		if answer.id == id {
			level.answers = append(level.answers[:i], level.answers[i+1:]...)
			return true
		}
	}
	return false
}
