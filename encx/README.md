# encx — Go library for Encounter (en.cx) Game Engine API

Go-клиент для взаимодействия с JSON API движка городских игр Encounter (en.cx).

## Установка

```sh
go get github.com/svk/encx/encx
```

## Возможности

- Авторизация (cookie-based sessions)
- Получение состояния игры (polling)
- Отправка кодов уровня и бонусных кодов
- Запрос штрафных подсказок
- Получение списка игр домена
- Проверка таймера до начала игры
- Поддержка TLS skip для тестовых серверов

## Использование библиотеки

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/svk/encx/encx"
)

func main() {
	// Создание клиента (с игнорированием невалидного SSL)
	client := encx.New("demo.en.cx", encx.WithInsecureTLS())
	ctx := context.Background()

	// Авторизация
	resp, err := client.Login(ctx, "user", "password")
	if err != nil {
		log.Fatal(err)
	}
	if resp.Error != 0 {
		log.Fatalf("Login error %d: %s", resp.Error, encx.LoginErrorText(resp.Error))
	}

	// Список игр на домене
	games, _ := client.GetDomainGames(ctx)
	for _, g := range games {
		fmt.Printf("%d: %s\n", g.GameId, g.Title)
	}

	// Получение состояния игры
	model, _ := client.GetGameModel(ctx, 12345)
	if model.Level != nil {
		fmt.Printf("Уровень %d: %s\n", model.Level.Number, model.Level.Name)
	}

	// Отправка кода
	result, _ := client.SendCode(ctx, 12345, model.Level.LevelId, "КОД123")
	if result.EngineAction.LevelAction.IsCorrectAnswer != nil {
		if *result.EngineAction.LevelAction.IsCorrectAnswer {
			fmt.Println("Верный код!")
		}
	}
}
```

## CLI-приложение

```sh
# Сборка
go build -o encx-cli ./cmd/encx-cli/

# Авторизация
./encx-cli --domain demo.en.cx --login user --password pass --insecure login

# Список игр
./encx-cli --domain demo.en.cx --insecure games

# Статус игры
./encx-cli --domain demo.en.cx --login user --password pass --game-id 12345 --insecure status

# Отправка кода
./encx-cli --domain demo.en.cx --login user --password pass --game-id 12345 --insecure send-code "КОД123"

# Отправка бонусного кода
./encx-cli --domain demo.en.cx --login user --password pass --game-id 12345 --insecure send-bonus "БОНУС"

# Запрос штрафной подсказки
./encx-cli --domain demo.en.cx --login user --password pass --game-id 12345 --insecure hint 42
```

## Тесты

Интеграционные тесты работают с реальным сервером `demo.en.cx`:

```sh
ENCX_INTEGRATION=1 go test ./encx/ -v -count=1
```

Без переменной `ENCX_INTEGRATION` тесты пропускаются.

## Структура API

| Метод | Endpoint | Описание |
|---|---|---|
| `Login` | `POST /login/signin` | Авторизация |
| `GetGameModel` | `POST /gameengines/encounter/play/{id}` | Состояние игры |
| `SendCode` | `POST /gameengines/encounter/play/{id}` | Отправка кода уровня |
| `SendBonusCode` | `POST /gameengines/encounter/play/{id}` | Отправка бонусного кода |
| `GetPenaltyHint` | `GET /gameengines/encounter/play/{id}` | Запрос штрафной подсказки |
| `GetDomainGames` | `GET http://m.{domain}/` | Список игр домена |
| `GetTimeoutToGame` | `GET http://m.{domain}/gameengines/encounter/play/{id}` | Таймер до начала |

Подробная документация API: [EN_CX_API.md](../EN_CX_API.md) | [openapi.yaml](../openapi.yaml)
