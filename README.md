# encx — Go client for Encounter (en.cx) Game Engine API

Go-клиент для взаимодействия с JSON API движка городских игр Encounter (en.cx).

## Установка

### CLI-утилита

```sh
go install github.com/skrashevich/encx-cli/cmd/encli@latest
```

### Библиотека (как зависимость)

```sh
go get github.com/skrashevich/encx-cli/encx
```

## Использование библиотеки

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/skrashevich/encx-cli/encx"
)

func main() {
	client := encx.New("demo.en.cx", encx.WithInsecureTLS())
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

| Опция | Описание |
|---|---|
| `WithInsecureTLS()` | Пропустить проверку TLS-сертификата |
| `WithHTTP()` | Использовать HTTP вместо HTTPS |
| `WithTimeout(d)` | Установить таймаут HTTP-клиента |
| `WithUserAgent(ua)` | Установить User-Agent |
| `WithLang(lang)` | Язык запросов (по умолчанию: `ru`) |

## CLI-утилита (encli)

```sh
# Авторизация (интерактивный ввод пароля)
encli login -domain demo.en.cx -insecure

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

### Флаги и переменные окружения

| Флаг | Env-переменная | Описание |
|---|---|---|
| `-domain` | `ENCX_DOMAIN` | Домен Encounter (по умолчанию: `demo.en.cx`) |
| `-login` | `ENCX_LOGIN` | Логин |
| `-password` | `ENCX_PASSWORD` | Пароль |
| `-game-id` | `ENCX_GAME_ID` | ID игры |
| `-insecure` | `ENCX_INSECURE` | Пропустить проверку TLS-сертификата |
| `-http` | — | Использовать HTTP вместо HTTPS |

### Сборка из исходников

```sh
go build -o encli ./cmd/encli/
```

## API

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

Подробная документация: [openapi.yaml](openapi.yaml)

## Тесты

Интеграционные тесты работают с реальным сервером `tech.en.cx`:

```sh
ENCX_INTEGRATION=1 go test ./encx/ -v -count=1
```

Без `ENCX_INTEGRATION` запускаются только юнит-тесты.
