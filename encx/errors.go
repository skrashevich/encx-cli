package encx

// LoginErrorText returns a human-readable description for a login error code.
func LoginErrorText(code int) string {
	if msg, ok := loginErrors[code]; ok {
		return msg
	}
	return "Неизвестная ошибка"
}

var loginErrors = map[int]string{
	0:  "Успешная авторизация",
	1:  "Превышено количество неправильных попыток авторизации",
	2:  "Неправильный логин или пароль",
	3:  "Пользователь в чёрном списке или кросс-доменная блокировка",
	4:  "В профиле включена блокировка по IP",
	5:  "Ошибка на сервере",
	6:  "Не используется в JSON запросах",
	7:  "Пользователь заблокирован администратором",
	8:  "Новый пользователь не активирован",
	9:  "Действия пользователя расценены как брутфорс",
	10: "Пользователь не подтвердил E-Mail",
}

// EventText returns a human-readable description for a game event status code.
func EventText(code int) string {
	if msg, ok := eventCodes[code]; ok {
		return msg
	}
	return "Неизвестный статус"
}

var eventCodes = map[int]string{
	0:  "Игра в процессе",
	2:  "Игра с таким ID не существует",
	3:  "Игра не соответствует движку",
	4:  "Игрок не авторизован",
	5:  "Игра ещё не началась",
	6:  "Игра завершена",
	7:  "Заявка игрока не подана",
	8:  "Заявка команды не подана",
	9:  "Заявка игрока ещё не принята",
	10: "Игрок не состоит в команде",
	11: "Игрок неактивен в команде",
	12: "В игре нет уровней",
	13: "Превышен лимит участников в команде",
	16: "Уровень снят — запросите заново",
	17: "Игра окончена",
	18: "Уровень снят — запросите заново",
	19: "Уровень пройден по автопереходу",
	20: "Все сектора разгаданы",
	21: "Уровень снят — запросите заново",
	22: "Таймаут уровня",
}

// Game event constants.
const (
	EventGameNormal          = 0
	EventGameNotFound        = 2
	EventEngineMismatch      = 3
	EventPlayerNotLoggedIn   = 4
	EventGameNotStarted      = 5
	EventGameFinished        = 6
	EventPlayerNoApplication = 7
	EventTeamNoApplication   = 8
	EventPlayerNotAccepted   = 9
	EventPlayerNoTeam        = 10
	EventPlayerInactive      = 11
	EventNoLevels            = 12
	EventTeamLimitExceeded   = 13
	EventLevelDismissed16    = 16
	EventGameEnded           = 17
	EventLevelDismissed18    = 18
	EventLevelAutoAdvance    = 19
	EventAllSectorsSolved    = 20
	EventLevelDismissed21    = 21
	EventLevelTimeout        = 22
)

// Level sequence types.
const (
	SequenceLinear        = 0
	SequenceSpecified     = 1
	SequenceRandom        = 2
	SequenceAssault       = 3
	SequenceDynamicRandom = 4
)

// Game type constants.
const (
	GameTypeSingle   = 0
	GameTypeTeam     = 1
	GameTypePersonal = 2
)

// Zone type constants.
const (
	ZoneQuest        = 0
	ZoneBrainstorm   = 1
	ZonePhotohunt    = 2
	ZoneWetWar       = 3
	ZoneCompetition  = 4
	ZonePhotoextreme = 5
	ZonePoints       = 7
	ZoneCompetition2 = 8
	ZoneQuiz         = 9
)
