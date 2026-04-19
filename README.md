# encx — Go library for Encounter (en.cx) Game Engine API

Go-клиент для взаимодействия с JSON API движка городских игр Encounter (en.cx).

## Установка

```sh
go get github.com/skrashevich/encx-cli/encx
```

## Возможности

- Авторизация (cookie-based sessions) с поддержкой CAPTCHA и выбора сети
- Получение состояния игры (polling) с полными данными уровня
- Отправка кодов уровня и бонусных кодов
- Запрос штрафных подсказок
- Получение списка игр домена (HTML + JSON API)
- Просмотр задания уровня и сообщений от организаторов
- Вступление в игру
- Проверка таймера до начала игры
- Обработка всех Event-статусов движка
- Поддержка TLS skip для тестовых серверов

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

	list, _ := client.GetGameList(ctx)
	for _, g := range list.ActiveGames {
		fmt.Printf("%d: %s\n", g.GameID, g.Title)
	}

	model, _ := client.GetGameModel(ctx, 12345)
	if model.Level != nil {
		fmt.Printf("Уровень %d: %s\n", model.Level.Number, model.Level.Name)

		if model.Level.Task != nil {
			fmt.Println(model.Level.Task.TaskText)
		}
	}

	result, _ := client.SendCode(ctx, 12345, model.Level.LevelId, model.Level.Number, "КОД123")
	if result.EngineAction.LevelAction.IsCorrectAnswer != nil {
		if *result.EngineAction.LevelAction.IsCorrectAnswer {
			fmt.Println("Верный код!")
		}
	}
}
```

## CLI-приложение (encli)

```sh
# Сборка
go build -o encli ./cmd/encli/

# Авторизация
./encli login -login user -password pass -domain demo.en.cx -insecure

# Список игр (простой, HTML)
./encli games -domain demo.en.cx -insecure

# Список игр (полный, JSON API)
./encli game-list -domain demo.en.cx -insecure

# Статус игры
./encli status -game-id 12345

# Задание текущего уровня
./encli task -game-id 12345

# Сообщения от организаторов
./encli messages -game-id 12345

# Вступить в игру
./encli enter -game-id 12345

# Отправка кода
./encli send-code -game-id 12345 "КОД123"

# Отправка бонусного кода
./encli send-bonus -game-id 12345 "БОНУС"

# Запрос штрафной подсказки
./encli hint -game-id 12345 42

# Выход
./encli logout
```

## Тесты

Интеграционные тесты работают с реальным сервером `tech.en.cx`:

```sh
ENCX_INTEGRATION=1 go test ./encx/ -v -count=1
```

Без переменной `ENCX_INTEGRATION` тесты пропускаются.

## Флаги CLI

| Флаг | Env-переменная | Описание |
|---|---|---|
| `-domain` | `ENCX_DOMAIN` | Домен Encounter (по умолчанию: `demo.en.cx`) |
| `-login` | `ENCX_LOGIN` | Логин |
| `-password` | `ENCX_PASSWORD` | Пароль |
| `-game-id` | `ENCX_GAME_ID` | ID игры |
| `-insecure` | `ENCX_INSECURE` | Пропустить проверку TLS-сертификата |
| `-http` | — | Использовать HTTP вместо HTTPS |

## Структура API

| Метод | Endpoint | Описание |
|---|---|---|
| `Login` | `POST /login/signin` | Авторизация (CAPTCHA, выбор сети) |
| `GetGameModel` | `POST /gameengines/encounter/play/{id}` | Состояние игры |
| `SendCode` | `POST /gameengines/encounter/play/{id}` | Отправка кода уровня |
| `SendBonusCode` | `POST /gameengines/encounter/play/{id}` | Отправка бонусного кода |
| `GetPenaltyHint` | `GET /gameengines/encounter/play/{id}` | Запрос штрафной подсказки |
| `GetGameList` | `GET /home/?json=1` | Список игр (JSON) |
| `GetDomainGames` | `GET http://m.{domain}/` | Список игр (HTML) |
| `GetTimeoutToGame` | `GET http://m.{domain}/gameengines/encounter/play/{id}` | Таймер до начала |
| `EnterGame` | `POST /gameengines/encounter/makefee/Login.aspx` | Вступить в игру |
| `GetGameDetails` | `GET /GameDetails.aspx?gid={id}` | Детали игры (HTML) |
| `GetTeamDetails` | `GET /Teams/TeamDetails.aspx?tid={id}` | Информация о команде (HTML) |
| `AcceptTeamInvitation` | `GET /Teams/TeamDetails.aspx?action=accept_invitation&tid={id}` | Принять приглашение в команду |

Подробная документация API: [openapi.yaml](openapi.yaml)
