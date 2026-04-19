# encx-cli

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
encli task -game-id 12345

# Сообщения от организаторов
encli messages -game-id 12345

# Вступить в игру
encli enter -game-id 12345

# Отправка кодов
encli send-code -game-id 12345 "КОД123"
encli send-bonus -game-id 12345 "БОНУС"

# Запрос штрафной подсказки
encli hint -game-id 12345 42

# Версия
encli -v

# Выход
encli logout
```

### Команды CLI

| Команда | Что делает |
|---|---|
| `login` | Логинится и сохраняет сессию |
| `logout` | Чистит сохраненную сессию |
| `games` | Показывает список игр через HTML-страницу домена |
| `game-list` | Показывает список игр через JSON API |
| `status` | Показывает текущее состояние игры |
| `task` | Печатает текст текущего задания |
| `messages` | Показывает сообщения от организаторов |
| `enter` | Подает заявку на вход в игру |
| `send-code` | Отправляет код уровня |
| `send-bonus` | Отправляет бонусный код |
| `hint` | Запрашивает штрафную подсказку |
| `-v` | Показывает версию |

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

Пример:

```sh
export ENCX_DOMAIN=tech.en.cx
export ENCX_LOGIN=my_login
export ENCX_PASSWORD=my_password
export ENCX_GAME_ID=12345

encli login -insecure
encli status
```

### Docker

```sh
docker run --rm ghcr.io/skrashevich/encx-cli -v
docker run --rm ghcr.io/skrashevich/encx-cli games -domain tech.en.cx
```

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
| `GetGameList` | `GET /home/?json=1` | Список игр (JSON) |
| `GetDomainGames` | `GET m.{domain}/` | Список игр (HTML) |
| `GetTimeoutToGame` | `GET m.{domain}/gameengines/encounter/play/{id}` | Таймер до начала |
| `EnterGame` | `POST /gameengines/encounter/makefee/Login.aspx` | Вступить в игру |
| `GetGameDetails` | `GET /GameDetails.aspx?gid={id}` | Детали игры (HTML) |
| `GetTeamDetails` | `GET /Teams/TeamDetails.aspx?tid={id}` | Информация о команде |
| `AcceptTeamInvitation` | `GET /Teams/TeamDetails.aspx?action=accept_invitation&tid={id}` | Принять приглашение |

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
