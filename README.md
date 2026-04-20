# encx-cli

<!-- badges:start -->
[![GitHub stars](https://img.shields.io/github/stars/skrashevich/encx-cli?style=flat-square)](https://github.com/skrashevich/encx-cli/stargazers)
[![Last commit](https://img.shields.io/github/last-commit/skrashevich/encx-cli?style=flat-square)](https://github.com/skrashevich/encx-cli/commits/main)
[![License](https://img.shields.io/github/license/skrashevich/encx-cli?style=flat-square)](https://github.com/skrashevich/encx-cli/blob/main/LICENSE)
<!-- badges:end -->


`encx-cli` — это Go-клиент и CLI для API движка городских квестов Encounter (`en.cx`).

Если короче: здесь есть и библиотека для встраивания в свой код, и консольная утилита, которой можно быстро залогиниться, посмотреть игру, прочитать задания и отправить код без браузера.

- Модуль: `github.com/skrashevich/encx-cli`
- CLI: `cmd/encli`
- Пакет-библиотека: `encx`
- Тестовый домен для примеров и интеграционных тестов: `tech.en.cx`

## Что внутри

Проект пригодится в двух сценариях:

- хотите написать свой тулинг поверх Encounter API — берите пакет `encx`;
- хотите просто работать из терминала — ставьте `encli`.

## Установка

### CLI

```sh
go install github.com/skrashevich/encx-cli/cmd/encli@latest
```

### Docker

```sh
docker run --rm ghcr.io/skrashevich/encx-cli -v
docker run --rm ghcr.io/skrashevich/encx-cli games -domain tech.en.cx
```


### Библиотека

```sh
go get github.com/skrashevich/encx-cli/encx
```

## Использование библиотеки

Ниже минимальный пример: логинимся, смотрим список игр, читаем состояние и пробуем отправить код.

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/skrashevich/encx-cli/encx"
)

func main() {
	client := encx.New("tech.en.cx", encx.WithInsecureTLS())
	ctx := context.Background()

	resp, err := client.Login(ctx, "user", "password")
	if err != nil {
		log.Fatal(err)
	}
	if resp.Error != 0 {
		log.Fatalf("Login error %d: %s", resp.Error, encx.LoginErrorText(resp.Error))
	}

	// Список игр
	list, _ := client.GetGameList(ctx)
	for _, g := range list.ActiveGames {
		fmt.Printf("%d: %s\n", g.GameID, g.Title)
	}

	// Состояние игры
	model, _ := client.GetGameModel(ctx, 12345)
	if model.Level != nil {
		fmt.Printf("Уровень %d: %s\n", model.Level.Number, model.Level.Name)
	}

	// Отправка кода
	result, _ := client.SendCode(ctx, 12345, model.Level.LevelId, model.Level.Number, "КОД123")
	if result.EngineAction != nil && result.EngineAction.LevelAction != nil &&
		result.EngineAction.LevelAction.IsCorrectAnswer != nil &&
		*result.EngineAction.LevelAction.IsCorrectAnswer {
		fmt.Println("Верный код!")
	}

	// Список игр с пагинацией
	page2, _ := client.GetGameList(ctx, 2)
	for _, g := range page2.ComingGames {
		fmt.Printf("%d: %s (levels: %d)\n", g.GameID, g.Title, g.LevelNumber)
	}

	// Статистика игры
	stats, _ := client.GetGameStatistics(ctx, 12345)
	if stats.Game != nil {
		fmt.Printf("Игра: %s, Уровней: %d\n", stats.Game.Title, len(stats.Levels))
		for _, l := range stats.Levels {
			fmt.Printf("  Уровень %d: %s\n", l.LevelNumber, l.LevelName)
		}
	}
}
```

### Опции клиента

Можно подкрутить поведение клиента через опции:

| Опция | Описание |
|---|---|
| `WithInsecureTLS()` | Пропустить проверку TLS-сертификата |
| `WithHTTP()` | Использовать HTTP вместо HTTPS |
| `WithTimeout(d)` | Установить таймаут HTTP-клиента |
| `WithUserAgent(ua)` | Установить User-Agent |
| `WithLang(lang)` | Язык запросов (по умолчанию: `ru`) |

## CLI: `encli`

`encli` полезен, когда нужно быстро дернуть API руками и не городить под это отдельный код.

Типичный поток такой:

1. залогиниться;
2. выбрать игру;
3. смотреть статус, задания и сообщения;
4. отправлять коды, бонусы и запрашивать подсказки.

```sh
# Авторизация (интерактивный ввод пароля)
encli login -domain tech.en.cx -insecure

# Список игр
encli games
encli game-list

# Статус игры
encli status -game-id 12345

# Задание текущего уровня
encli level -game-id 12345

# Все уровни с прогрессом
encli levels -game-id 12345

# Бонусы текущего уровня
encli bonuses -game-id 12345

# Подсказки (обычные и штрафные)
encli hints -game-id 12345

# Секторы текущего уровня
encli sectors -game-id 12345

# Лог пробитий кодов
encli log -game-id 12345

# Сообщения от организаторов
encli messages -game-id 12345

# Вступить в игру
encli enter -game-id 12345

# Отправка кодов
encli send-code -game-id 12345 "КОД123"
encli send-bonus -game-id 12345 "БОНУС"

# Запрос штрафной подсказки
encli hint -game-id 12345 42

# Статистика игры
encli game-stats -game-id 12345

# Версия
encli -v

# LLM-режим через OpenRouter
OPENROUTER_API_KEY=... encli -game-id 12345 --llm "создай 3 уровня с бонусами и подсказками"

# Отладка CLI и LLM-потока
OPENROUTER_API_KEY=... encli -debug -game-id 12345 --llm "покажи статус игры"
OPENROUTER_API_KEY=... encli -game-id 12345 --llm "покажи статус игры" -debug

# Выход
encli logout

# --- Admin-команды ---

# Список авторских игр
encli admin-games

# Список уровней с ID
encli admin-levels -game-id 12345

# Создать 3 уровня
encli admin-create-levels -game-id 12345 3

# Удалить уровень №5
encli admin-delete-level -game-id 12345 5

# Переименовать уровень (ID из admin-levels)
encli admin-rename-level -game-id 12345 67890 "Новое название"

# Установить автопереход 1ч 30мин с штрафом 15мин
encli admin-set-autopass -game-id 12345 1 1:30:00 0:15:00

# Блокировка: 3 попытки за 1 минуту, на игрока
encli admin-set-block -game-id 12345 1 3 0:01:00 player

# Создать бонус (уровень 1, level-id 67890, название, ответы)
encli admin-create-bonus -game-id 12345 1 67890 "Бонус 1" "ответ1" "ответ2"

# Удалить бонус
encli admin-delete-bonus -game-id 12345 1 111

# Создать сектор
encli admin-create-sector -game-id 12345 1 "Сектор А" "код1" "код2"

# Удалить сектор
encli admin-delete-sector -game-id 12345 1 222

# Создать подсказку (откроется через 30 минут)
encli admin-create-hint -game-id 12345 1 0:30:00 "Текст подсказки"

# Удалить подсказку
encli admin-delete-hint -game-id 12345 1 333

# Создать задание
encli admin-create-task -game-id 12345 1 "Текст задания уровня"

# Установить имя и комментарий уровня
encli admin-set-comment -game-id 12345 1 "Название" "Комментарий для орга"

# Список команд в игре
encli admin-teams -game-id 12345

# Начисления бонусного/штрафного времени
encli admin-corrections -game-id 12345
encli admin-add-correction -game-id 12345 "Team Name" bonus 0:10:00 0 "за красоту"
encli admin-delete-correction -game-id 12345 444

# Полная очистка игры (обнуление)
encli admin-wipe-game -game-id 67890

# Копирование игры целиком (из 12345 в 67890)
# Рекомендуется сначала admin-wipe-game на целевой
encli admin-copy-game -game-id 12345 67890
```

### Команды CLI

| Команда | Что делает |
|---|---|
| `login` | Логинится и сохраняет сессию |
| `logout` | Чистит сохраненную сессию |
| `games` | Показывает список игр через HTML-страницу домена |
| `game-list` | Показывает список игр через JSON API |
| `status` | Показывает текущее состояние игры |
| `level` | Печатает текст текущего задания |
| `levels` | Показывает все уровни с прогрессом |
| `bonuses` | Показывает бонусы текущего уровня |
| `hints` | Показывает подсказки (обычные и штрафные) |
| `sectors` | Показывает секторы текущего уровня |
| `log` | Показывает лог пробитий кодов |
| `messages` | Показывает сообщения от организаторов |
| `enter` | Подает заявку на вход в игру |
| `send-code` | Отправляет код уровня |
| `send-bonus` | Отправляет бонусный код |
| `hint` | Запрашивает штрафную подсказку |
| `game-stats` | Показывает статистику игры (уровни, команды, результаты) |
| `--llm <prompt>` | Выполняет естественно-языковую команду через OpenRouter |
| `-v` | Показывает версию |

**Admin-команды (требуют прав редактора игры):**

| Команда | Что делает |
|---|---|
| `admin-games` | Показывает список авторских игр |
| `admin-levels` | Показывает все уровни с их ID (админка) |
| `admin-create-levels` | Создаёт указанное количество новых уровней |
| `admin-delete-level` | Удаляет уровень по номеру |
| `admin-rename-level` | Переименовывает уровень |
| `admin-set-autopass` | Устанавливает таймер автоперехода |
| `admin-set-block` | Настраивает блокировку ответов |
| `admin-create-bonus` | Создаёт бонус на уровне |
| `admin-delete-bonus` | Удаляет бонус по ID |
| `admin-create-sector` | Создаёт сектор на уровне |
| `admin-delete-sector` | Удаляет сектор по ID |
| `admin-create-hint` | Создаёт подсказку на уровне |
| `admin-delete-hint` | Удаляет подсказку по ID |
| `admin-create-task` | Создаёт задание на уровне |
| `admin-set-comment` | Устанавливает название и комментарий уровня |
| `admin-teams` | Показывает команды в игре |
| `admin-corrections` | Показывает начисления бонусного/штрафного времени |
| `admin-add-correction` | Добавляет начисление времени |
| `admin-delete-correction` | Удаляет начисление по ID |
| `admin-wipe-game` | Полностью обнуляет игру (удаляет всё содержимое) |
| `admin-copy-game` | Копирует всю игру (уровни, настройки, бонусы, секторы, подсказки) в другую |

### Флаги и переменные окружения

Почти все можно передавать либо через флаги, либо через env. Удобно, если гоняете команды часто.

| Флаг | Env-переменная | Описание |
|---|---|---|
| `-domain` | `ENCX_DOMAIN` | Домен Encounter (по умолчанию: `tech.en.cx`) |
| `-login` | `ENCX_LOGIN` | Логин |
| `-password` | `ENCX_PASSWORD` | Пароль |
| `-game-id` | `ENCX_GAME_ID` | ID игры |
| `-insecure` | `ENCX_INSECURE` | Пропустить проверку TLS-сертификата |
| `-http` | — | Использовать HTTP вместо HTTPS |
| `-json` | — | Выводить результат в формате JSON |
| `-debug` | `ENCX_DEBUG` | Включить отладочный вывод в `stderr` |
| — | `OPENROUTER_API_KEY` | API-ключ для `--llm` |
| — | `OPENROUTER_MODEL` | Переопределить модель для `--llm` |

Пример:

```sh
export ENCX_DOMAIN=tech.en.cx
export ENCX_LOGIN=my_login
export ENCX_PASSWORD=my_password
export ENCX_GAME_ID=12345
export ENCX_DEBUG=1

encli login -insecure
encli status
encli -debug status
```

В `-debug` режиме `encli` пишет в `stderr` разбор аргументов, шаги LLM-агента, запуск и завершение tool-call'ов, а также HTTP-запросы `encx` с таймингами. Это полезно, когда нужно понять, на каком шаге процесс завис или долго ждёт сеть/ответ модели.

В `--llm` режиме большие результаты tool-call'ов автоматически ужимаются перед отправкой обратно в модель: вместо многомегабайтного сырого JSON в prompt уходит компактная сводка. Это снижает риск зависаний и ошибок по лимиту токенов.

### Сборка из исходников

```sh
go build -o encli ./cmd/encli/
```

## API

Ниже краткая шпаргалка по основным методам, которые уже завернуты в клиент.

| Метод | Endpoint | Описание |
|---|---|---|
| `Login` | `POST /login/signin` | Авторизация |
| `GetGameModel` | `POST /gameengines/encounter/play/{id}` | Состояние игры |
| `SendCode` | `POST /gameengines/encounter/play/{id}` | Отправка кода уровня |
| `SendBonusCode` | `POST /gameengines/encounter/play/{id}` | Отправка бонусного кода |
| `GetPenaltyHint` | `GET /gameengines/encounter/play/{id}` | Запрос штрафной подсказки |
| `GetGameList` | `GET /home/?json=1` | Список игр (JSON, с пагинацией) |
| `GetDomainGames` | `GET m.{domain}/` | Список игр (HTML) |
| `GetGameStatistics` | `GET /gamestatistics/full/{id}?json=1` | Полная статистика игры |
| `GetTimeoutToGame` | `GET m.{domain}/gameengines/encounter/play/{id}` | Таймер до начала |
| `EnterGame` | `POST /gameengines/encounter/makefee/Login.aspx` | Вступить в игру |
| `GetGameDetails` | `GET /GameDetails.aspx?gid={id}` | Детали игры (HTML) |
| `GetTeamDetails` | `GET /Teams/TeamDetails.aspx?tid={id}` | Информация о команде |
| `AcceptTeamInvitation` | `GET /Teams/TeamDetails.aspx?action=accept_invitation&tid={id}` | Принять приглашение |

**Admin API (требует прав редактора):**

| Метод | Endpoint | Описание |
|---|---|---|
| `AdminGetLevels` | `GET /Administration/Games/LevelManager.aspx` | Список уровней (ID, названия) |
| `AdminCreateLevels` | `GET /Administration/Games/LevelManager.aspx?levels=create` | Создание уровней |
| `AdminDeleteLevel` | `GET /Administration/Games/LevelManager.aspx?levels=delete` | Удаление уровня |
| `AdminRenameLevels` | `POST /Administration/Games/LevelManager.aspx?level_names=update` | Переименование уровней |
| `AdminUpdateAutopass` | `POST /Administration/Games/LevelEditor.aspx` | Настройка автоперехода |
| `AdminUpdateAnswerBlock` | `POST /Administration/Games/LevelEditor.aspx` | Настройка блокировки ответов |
| `AdminCreateBonus` | `POST /Administration/Games/BonusEdit.aspx?action=save` | Создание бонуса |
| `AdminDeleteBonus` | `GET /Administration/Games/BonusEdit.aspx?action=delete` | Удаление бонуса |
| `AdminCreateSector` | `POST /Administration/Games/LevelEditor.aspx` | Создание сектора |
| `AdminDeleteSector` | `GET /Administration/Games/LevelEditor.aspx?delsector={id}` | Удаление сектора |
| `AdminCreateHint` | `POST /Administration/Games/PromptEdit.aspx` | Создание подсказки |
| `AdminDeleteHint` | `GET /Administration/Games/PromptEdit.aspx?action=PromptDelete` | Удаление подсказки |
| `AdminCreateTask` | `POST /Administration/Games/TaskEdit.aspx` | Создание задания |
| `AdminUpdateComment` | `POST /Administration/Games/NameCommentEdit.aspx` | Обновление названия/комментария |
| `AdminGetTeams` | `GET /Administration/Games/TaskEdit.aspx` | Список команд |
| `AdminGetCorrections` | `GET /GameBonusPenaltyTime.aspx` | Список начислений времени |
| `AdminAddCorrection` | `POST /GameBonusPenaltyTime.aspx?action=save` | Добавление начисления |
| `AdminDeleteCorrection` | `GET /GameBonusPenaltyTime.aspx?action=delete` | Удаление начисления |
| `AdminGetLevelSettings` | `GET /Administration/Games/LevelEditor.aspx` | Чтение настроек уровня (автопереход, блокировка) |
| `AdminGetBonusIds` | `GET /Administration/Games/LevelEditor.aspx` | Список ID бонусов на уровне |
| `AdminGetBonus` | `GET /Administration/Games/BonusEdit.aspx?action=edit` | Чтение деталей бонуса |
| `AdminGetHintIds` | `GET /Administration/Games/LevelEditor.aspx` | Список ID подсказок на уровне |
| `AdminGetHint` | `GET /Administration/Games/PromptEdit.aspx?action=PromptEdit` | Чтение деталей подсказки (обычной и штрафной) |
| `AdminGetTaskIds` | `GET /Administration/Games/LevelEditor.aspx` | Список ID заданий на уровне |
| `AdminGetTask` | `GET /Administration/Games/TaskEdit.aspx?action=TaskEdit` | Чтение деталей задания |
| `AdminGetComment` | `GET /Administration/Games/NameCommentEdit.aspx` | Чтение названия и комментария уровня |
| `AdminGetSectorAnswers` | `GET /ALoader/LevelInfo.aspx` | Чтение секторов и ответов уровня |
| `AdminWipeGame` | (комбинированный) | Полная очистка игры (удаление всего содержимого) |
| `AdminCopyGame` | (комбинированный) | Полное копирование игры (уровни, настройки, бонусы, секторы, подсказки) |

Полная неофициальная (полученная методом реверс-инжиниринга) спецификация API в формате OpenAPI 3.1: [openapi.yaml](openapi.yaml).

Поддерживаемые домены: `*.en.cx`, `*.encounter.cx`, `*.encounter.ru`. Домен `quest.ua` deprecated — мигрирован в `{city}questua.en.cx` (напр. `kharkov.quest.ua` -> `kharkovquestua.en.cx`).

## Тестовый домен

Для тестирования собственных разработок предусмотрен специализированный домен `tech.en.cx`. Чтобы получить на нём права создания игр (исключительно в технологических целях) — напишите в [техподдержку сети](https://world.en.cx/UserDetails.aspx?uid=7).

## Тесты

Интеграционные тесты ходят в `tech.en.cx`:

```sh
ENCX_INTEGRATION=1 go test ./encx/ -v -count=1
```

Если переменную `ENCX_INTEGRATION` не задавать, запустятся только юнит-тесты.
