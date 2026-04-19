# Encounter (en.cx) Game Engine API

Документация API движка Encounter, восстановленная из декомпиляции приложений:
- **EnApp** v2.0.1 (net.necto68.enapp) — React Native
- **EN+** v1.2.0 (com.encounter.enplus) — Expo/React Native + Kotlin, Hermes bytecode

## Общие сведения

- **Протокол:** HTTP (не HTTPS!)
- **Формат ответа:** JSON (при передаче параметра `json=1`)
- **Авторизация:** Cookie-based (session cookies, `withCredentials: true`)
- **HTTP-клиент:** axios с timeout 15 секунд
- **User-Agent:** `EnApp by necto68`

### Поддерживаемые домены

Приложение валидирует домен по следующим regex-паттернам:

| Паттерн | Примеры |
|---|---|
| `quest.ua` | quest.ua |
| `[\w-]+\.quest\.ua` | kyiv.quest.ua |
| `[\w-]+\.en\.cx` | quest.en.cx, demo.en.cx, nef.en.cx |
| `[\w-]+\.encounter\.cx` | quest.encounter.cx |
| `[\w-]+\.encounter\.ru` | quest.encounter.ru |

### Общие заголовки для всех запросов

```
User-Agent: EnApp by necto68
```

Cookies передаются автоматически (`withCredentials: true`, `maxRedirects: 0`).

---

## 1. Авторизация

### POST `/login/signin`

Авторизация пользователя на домене. Сессия сохраняется через cookies.

**URL:** `http://{domain}/login/signin`

**Method:** `POST`

**Query Parameters:**

| Параметр | Тип | Обязательный | Значение | Описание |
|---|---|---|---|---|
| `json` | integer | Да | `1` | Запрос JSON-ответа вместо HTML |
| `lang` | string | Да | `ru` | Язык ответа |

**Request Body** (application/json):

| Поле | Тип | Описание |
|---|---|---|
| `Login` | string | Логин пользователя на en.cx |
| `Password` | string | Пароль пользователя |

**Response** (application/json):

```json
{
  "Error": 0,
  "Message": ""
}
```

**Коды ошибок авторизации (поле `Error`):**

| Код | Описание |
|---|---|
| `0` | Успешная авторизация |
| `1` | Превышено количество неправильных попыток авторизации. Попробуйте авторизоваться в браузере телефона, затем попробуйте снова в приложении |
| `2` | Неправильный логин или пароль |
| `3` | Пользователь в сибири/чёрном списке/на домене нельзя авторизовываться с других доменов |
| `4` | В профиле включена блокировка по IP |
| `5` | Ошибка на сервере |
| `6` | Не используется в JSON запросах |
| `7` | Пользователь заброкирован администратором |
| `8` | Новый пользователь не активирован |
| `9` | Действия пользователя расценены как брутфорс. Попробуйте авторизоваться в браузере телефона, затем попробуйте снова в приложении |
| `10` | Пользователь не подтвердил E-Mail |

**Пример запроса:**

```
POST http://quest.en.cx/login/signin?json=1&lang=ru
User-Agent: EnApp by necto68
Content-Type: application/json

{
  "Login": "player1",
  "Password": "secret123"
}
```

---

## 2. Получение модели игры

### POST `/gameengines/encounter/play/{gameId}`

Основной endpoint. Используется для получения текущего состояния игры, отправки кодов, запроса штрафных подсказок. Все игровые действия проходят через этот единственный endpoint — различается только набор параметров в теле запроса.

**URL:** `http://{domain}/gameengines/encounter/play/{gameId}`

**Method:** `POST`

**Path Parameters:**

| Параметр | Тип | Описание |
|---|---|---|
| `gameId` | integer | ID игры (извлекается из ссылки `/play/{id}` или `gid={id}`) |

**Query Parameters:**

| Параметр | Тип | Обязательный | Значение | Описание |
|---|---|---|---|---|
| `json` | integer | Да | `1` | Запрос JSON-ответа |
| `lang` | string | Да | `ru` | Язык ответа |

**Headers:**

```
User-Agent: EnApp by necto68
```

**Flags:**
- `withCredentials: true` — отправлять cookies
- `maxRedirects: 0` — не следовать редиректам

**Request Body** (application/x-www-form-urlencoded, через qs.stringify):

Тело запроса зависит от выполняемого действия. Может быть пустым (просто получение состояния) или содержать параметры действия.

#### 2a. Получение текущего состояния (polling)

Тело запроса пустое или `{}`. Используется для периодического обновления модели игры (каждые 20 секунд, `REFRESH_INTERVAL_SECONDS: 20`).

Дополнительный query-параметр:

| Параметр | Тип | Описание |
|---|---|---|
| `LevelNumber` | integer | Номер текущего уровня (отправляется всегда) |

#### 2b. Отправка кода уровня

**Body Parameters:**

| Поле | Тип | Описание |
|---|---|---|
| `LevelId` | integer | ID текущего уровня (`Level.LevelId`) |
| `LevelAction.Answer` | string | Код (ответ) для уровня |

#### 2c. Отправка бонусного кода

**Body Parameters:**

| Поле | Тип | Описание |
|---|---|---|
| `LevelId` | integer | ID текущего уровня (`Level.LevelId`) |
| `BonusAction.Answer` | string | Код (ответ) для бонуса |

#### 2d. Запрос штрафной подсказки

**Body Parameters:**

| Поле | Тип | Описание |
|---|---|---|
| `pid` | integer | ID подсказки |
| `pact` | integer | Действие (`1` — запросить подсказку) |

**Примечание:** Штрафная подсказка запрашивается через метод `getGameModelWithParams`, а не через `getGameModal`. Разница — `getGameModelWithParams` использует HTTP GET с параметрами в query string, а `getGameModal` использует POST с `qs.stringify` в body.

---

### GET `/gameengines/encounter/play/{gameId}`

Альтернативный метод для получения модели игры (используется в `getGameModelWithParams` и `getTimeoutToGame`).

**Method:** `GET`

**Query Parameters:**

| Параметр | Тип | Описание |
|---|---|---|
| `json` | integer | `1` |
| `lang` | string | `ru` |
| `pid` | integer | ID подсказки (для запроса штрафной подсказки) |
| `pact` | integer | `1` (действие — запросить подсказку) |

Все параметры передаются в query string (не в body).

---

### Response: Модель игры (GameModel)

Ответ сервера содержит полную модель текущего состояния игры:

```json
{
  "Event": 0,
  "GameId": 12345,
  "UserId": 67890,
  "LevelSequence": 3,
  "Levels": [
    { "LevelId": 1, "Number": 1, "Name": "Уровень 1" }
  ],
  "Level": {
    "LevelId": 100,
    "Number": 1,
    "Name": "Название уровня",
    "Timeout": 3600,
    "TimeoutSecondsRemain": 1800,
    "TimeoutAward": -300,
    "SectorsLeftToClose": 3,
    "RequiredSectorsCount": 5,
    "Sectors": [
      {
        "SectorId": 1,
        "Name": "Сектор A",
        "IsAnswered": true,
        "Answer": "КОД123"
      }
    ],
    "Bonuses": [
      {
        "BonusId": 1,
        "Name": "Бонус 1",
        "IsAnswered": false
      }
    ],
    "PenaltyHelps": [
      {
        "PenaltyHelpId": 1,
        "Number": 1,
        "RemainSeconds": 600,
        "PenaltyComment": "Штрафная подсказка 1",
        "Penalty": 300
      }
    ],
    "MixedActions": [
      {
        "ActionId": 1,
        "UserId": 67890,
        "Login": "player1",
        "Answer": "КОД123",
        "LocDateTime": "2020-01-15 23:45:12",
        "IsCorrect": true,
        "Kind": 0
      }
    ]
  },
  "EngineAction": {
    "LevelAction": {
      "IsCorrectAnswer": null
    },
    "BonusAction": {
      "IsCorrectAnswer": null
    }
  }
}
```

### Поля GameModel

| Поле | Тип | Описание |
|---|---|---|
| `Event` | integer | Код события. `0` — нормальное состояние, ненулевые — специальные события |
| `GameId` | integer | ID игры |
| `UserId` | integer | ID текущего авторизованного пользователя |
| `LevelSequence` | integer | Тип последовательности уровней. `3` — свободная последовательность (можно выбирать уровень) |
| `Levels` | array | Список всех уровней игры |
| `Level` | object | Текущий активный уровень (подробнее ниже) |
| `EngineAction` | object | Результат последнего действия (отправки кода) |

### Поля Level

| Поле | Тип | Описание |
|---|---|---|
| `LevelId` | integer | ID уровня |
| `Number` | integer | Порядковый номер уровня |
| `Name` | string | Название уровня |
| `Timeout` | integer | Общее время на уровень (секунды). `0` — без таймера |
| `TimeoutSecondsRemain` | integer | Оставшееся время (секунды) |
| `TimeoutAward` | integer | Штраф/бонус за таймаут (секунды, отрицательное = штраф) |
| `SectorsLeftToClose` | integer | Сколько секторов осталось закрыть |
| `RequiredSectorsCount` | integer | Необходимое количество секторов для прохождения |
| `Sectors` | array | Список секторов уровня |
| `Bonuses` | array | Список бонусов уровня |
| `PenaltyHelps` | array | Список штрафных подсказок |
| `MixedActions` | array | Лог введённых кодов (всех игроков команды) |

### Поля EngineAction

| Поле | Тип | Описание |
|---|---|---|
| `LevelAction.IsCorrectAnswer` | boolean/null | `true` — код верный, `false` — код неверный, `null` — не было отправки |
| `BonusAction.IsCorrectAnswer` | boolean/null | `true` — бонусный код верный, `false` — неверный, `null` — не было отправки |

При `IsCorrectAnswer === false` приложение вибрирует (60мс).

### Поля MixedActions (лог кодов)

| Поле | Тип | Описание |
|---|---|---|
| `ActionId` | integer | ID действия |
| `UserId` | integer | ID пользователя, отправившего код |
| `Login` | string | Логин пользователя |
| `Answer` | string | Введённый код |
| `LocDateTime` | string | Локальное время ввода (`"YYYY-MM-DD HH:mm:ss"`) |
| `IsCorrect` | boolean | Верный ли код |
| `Kind` | integer | Тип действия |

### Поля PenaltyHelps (штрафные подсказки)

| Поле | Тип | Описание |
|---|---|---|
| `PenaltyHelpId` | integer | ID подсказки |
| `Number` | integer | Порядковый номер подсказки |
| `RemainSeconds` | integer | Секунды до открытия подсказки (обратный отсчёт) |
| `PenaltyComment` | string | Текст подсказки (доступен после открытия) |
| `Penalty` | integer | Штрафные секунды за использование |

### Коды событий (Event)

| Код | Описание | Поведение приложения |
|---|---|---|
| `0` | Нормальное состояние | Отображение игрового экрана |
| `16, 18, 19, 20, 21, 22` | События, требующие повторного запроса | Автоматический повторный вызов `updateGameModel()` |
| Другие целые числа | Прочие события | Показ LoadingView, пересоздание таймера |
| Нечисловое значение | Ошибка авторизации (сессия истекла) | Попытка повторного логина, при неудаче — выход |

---

## 3. Получение списка игр домена

### GET `http://m.{domain}/`

Парсинг HTML главной страницы мобильной версии для получения списка доступных игр.

**URL:** `http://m.{domain}/`

**Method:** `GET`

**Headers:**

```
User-Agent: EnApp by necto68
```

**Response:** HTML-страница мобильной версии.

**Парсинг:** Используется cheerio (HTML-парсер). Извлекаются элементы `h1.gametitle a`:

```javascript
$("h1.gametitle a").map(function(i, el) {
  return {
    title: $(el).text(),
    gameId: parseInt($(el).attr("href").match(/details\/(\d+)/)[1], 10)
  };
});
```

**Результат:**

```json
[
  { "title": "Название игры 1", "gameId": 12345 },
  { "title": "Название игры 2", "gameId": 12346 }
]
```

---

## 4. Проверка времени до начала игры

### GET `http://m.{domain}/gameengines/encounter/play/{gameId}`

Получение HTML-версии страницы игры для извлечения таймера обратного отсчёта до начала игры.

**URL:** `http://m.{domain}/gameengines/encounter/play/{gameId}`

**Method:** `GET`

**Query Parameters:**

| Параметр | Тип | Значение |
|---|---|---|
| `lang` | string | `ru` |

**Примечание:** Параметр `json=1` НЕ передаётся. Ответ — HTML.

**Headers:**

```
User-Agent: EnApp by necto68
```

**Flags:**
- `withCredentials: true`
- `maxRedirects: 0`

**Парсинг ответа:**

```javascript
var match = responseData.match(/"StartCounter":(\d+),/);
var startCounter = match ? parseInt(match[1], 10) : null;
```

Возвращает количество секунд до начала игры или `null`.

---

## Внутренняя логика приложения

### Очередь обновлений (updatesQueue)

Приложение использует очередь для последовательной обработки действий:

1. Каждое действие (отправка кода) добавляется в очередь с уникальным `$uniqueId` (случайное число 100000-999999)
2. Действия обрабатываются по одному (FIFO)
3. Пока обрабатывается одно действие (`isRefreshing: true`), новые запросы не отправляются
4. После успешной обработки элемент удаляется из очереди
5. Следующий элемент обрабатывается с задержкой 200-500мс (случайной)

### Автообновление

- Интервал: каждую 1 секунду (`setInterval(fn, 1000)`)
- Реальный запрос к серверу: каждые 20 секунд (`REFRESH_INTERVAL_SECONDS: 20`)
- `globalTimerCounter` инкрементируется каждую секунду для локального обновления UI-таймеров

### Push-уведомления

Приложение создаёт локальные уведомления:
- **Канал "UP":** Уведомление за 5 минут до истечения таймера уровня (`TimeoutSecondsRemain - 300`)
- **Канал "LEVEL_STATE":** Постоянное уведомление с состоянием уровня (номер, секторы, таймер, подсказки)

### Обработка ошибок авторизации в игре

При получении нечислового `Event` (сессия истекла):
1. Попытка повторного логина (`loginUser()`)
2. Повторный запрос модели игры
3. Если повторная авторизация не помогла — `signOut()`, возврат на LoginView

### Хранилище (AsyncStorage)

Приложение сохраняет следующие значения:

| Ключ | Описание |
|---|---|
| `domainValue` | Домен (напр. `quest.en.cx`) |
| `idGameValue` | ID игры |
| `loginValue` | Логин пользователя |
| `passwordValue` | Пароль пользователя (plain text!) |
| `cookiesValue` | Cookies сессии |

### Парсинг ссылок из буфера обмена

При фокусе на поле ввода домена приложение проверяет буфер обмена на наличие ссылок:
- Домен по паттернам: `quest.ua`, `*.quest.ua`, `*.en.cx`, `*.encounter.cx`, `*.encounter.ru`
- ID игры по паттернам: `/play/(\d+)` или `gid=(\d+)`

### Координаты

Текст заданий парсится регулярным выражением для поиска координат:

```
/(?![^<]*>)(-?\d{1,3}[.,]\d{3,8})[\s\S]+?(-?\d{1,3}[.,]\d{3,8})/gim
```

Найденные координаты становятся кликабельными и открываются в навигационном приложении.

---

## 5. Вход в игру (оплата/подтверждение)

### POST `/gameengines/encounter/makefee/Login.aspx`

Вход в конкретную игру. На некоторых доменах вход может быть платным (списание внутренней валюты). Этот endpoint выполняет регистрацию участника в игре.

**URL:** `http://{domain}/gameengines/encounter/makefee/Login.aspx`

**Method:** `POST`

**Обнаружен в:** EN+ v1.2.0

**Примечание:** Точные параметры запроса и формат ответа требуют дополнительного исследования. Предположительно передаётся `gid` (Game ID) и session cookies.

---

## 6. Команды

### GET `/Teams/TeamDetails.aspx?tid={teamId}`

Получение страницы с деталями команды. Ответ — HTML, парсится клиентом.

**URL:** `http://{domain}/Teams/TeamDetails.aspx?tid={teamId}`

**Method:** `GET`

**Query Parameters:**

| Параметр | Тип | Описание |
|---|---|---|
| `tid` | integer | ID команды |

**Парсинг:** EN+ извлекает данные команды из HTML с помощью regex:
- `TeamDetails[^>]*>([^<]+)<\/a>` — имя команды
- `TeamDetails\.aspx\?tid=(\d+)` — ID команды из ссылок

### GET `/Teams/TeamDetails.aspx?action=accept_invitation&tid={teamId}`

Принятие приглашения в команду.

**URL:** `http://{domain}/Teams/TeamDetails.aspx?action=accept_invitation&tid={teamId}`

**Method:** `GET`

**Query Parameters:**

| Параметр | Тип | Описание |
|---|---|---|
| `action` | string | `accept_invitation` |
| `tid` | integer | ID команды |

**Обнаружен в:** EN+ v1.2.0

---

## 7. Статистика игры

### GET `/GameDetails.aspx?gid={gameId}`

Получение страницы с деталями и статистикой игры. Ответ — HTML, парсится клиентом.

**URL:** `http://{domain}/GameDetails.aspx?gid={gameId}`

**Method:** `GET`

**Query Parameters:**

| Параметр | Тип | Описание |
|---|---|---|
| `gid` | integer | ID игры |

**Данные, извлекаемые EN+ из HTML:**

| Поле | Описание |
|---|---|
| `GameNum` | Номер игры на домене |
| `GameTitle` | Название игры |
| `Author` | Автор сценария |
| `GameType` | Тип игры (Штурм, Схватка, и т.д.) |
| `StartTime` | Время начала |
| `PlayerCount` | Количество игроков |
| `TotalLevels` | Общее количество уровней |
| `TotalSectors` | Общее количество секторов |
| `Sequence` | Тип последовательности уровней |

### Статистика прохождения (gamestatistics)

EN+ реализует детальную статистику прохождения игры с полями:

| Поле | Описание |
|---|---|
| `stats.levels` | Данные по уровням |
| `stats.teams` | Данные по командам |
| `stats.players` | Данные по игрокам |
| `stats.byLevel` | Статистика по уровням |
| `stats.byOrder` | Статистика по порядку прохождения |
| `stats.levelRank` | Ранг команды на каждом уровне |
| `stats.bonusesPenalties` | Штрафы и бонусы |
| `stats.closedEarlierBonuses` | Бонусы, закрытые раньше |
| `stats.closedEarlierSectors` | Секторы, закрытые раньше |
| `stats.diffWith` | Разница с другой командой |
| `stats.adjustedMs` | Скорректированное время (мс) |
| `stats.excludedLevels` | Исключённые уровни |
| `stats.timeoutMarker` | Маркер таймаута |
| `stats.totalCellBase` | Базовое значение ячейки |
| `stats.fetchUserProfile` | Профиль пользователя |

**Обнаружено в:** EN+ v1.2.0

---

## 8. Мониторинг и уведомления (EN+)

EN+ реализует систему real-time мониторинга игрового состояния:

### GameStateDiff

Механизм дифф-обновлений состояния игры. Вместо полной перезагрузки модели, EN+ отслеживает изменения (`GameStateDiff`) для оптимизации обновлений UI.

### GameStateNotification

Локальные уведомления о событиях в игре:
- Смена уровня
- Доступность подсказок
- Штрафные таймеры
- Изменения в секторах/бонусах

### Monitoring

Режим наблюдения за игрой без активного участия. EN+ поддерживает `MonitoringAction` для отслеживания состояния игры в режиме зрителя.

---

## Версии и зависимости

| Компонент | Версия |
|---|---|
| EnApp | 2.0.1 (build 15) |
| React Native | ~0.59.x (inferred from API usage) |
| MobX | 5.5.7 |
| axios | (bundled, с timeout 15s) |
| cheerio | (bundled, для парсинга HTML) |
| qs | (bundled, для stringify form data) |
