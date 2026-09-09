# Матрица паритета движков

Каждый экспортируемый метод `encx.Client` перечислен ниже: где он живёт в
старом ASP.NET-движке и чем обслуживается в новом REST-движке
(`docs/newengine/swagger.json`). Пробелы отмечены явно.

Файл сверяется тестом `TestEngineParityMatrixCoversEveryMethod`: метод, который
диспетчеризуется по движкам, но отсутствует в таблице, роняет сборку — так
документация не может разойтись с кодом.

**Спецификация — гипотеза, а не источник истины.** `docs/newengine/swagger.json`
дважды по ходу этой работы оказалась существенно неверна о развёрнутом API:
`prize_cents` описан примечанием, принадлежащим соседнему `price_cents` (на этом
построенное умножение на 100 превращало обычный read-modify-write в стократное
повышение), а `GET /teams/{id}/members` документирован как массив `models.User` с
ключом `id`, тогда как отдаёт строки членства с `user_id` и тремя флагами,
которых у `models.User` нет. Оба расхождения найдены замером. Модели в
`encx/enapi` с пометкой «Measured against …» сверены с живым ответом, остальные
переписаны из спецификации и могут быть неверны так же — прежде чем строить
поведение на непроверенном поле, прочитайте один настоящий ответ.

Утверждения о поведении нового движка в разделе «Известные пробелы» получены
живым прогоном на demo.en.cx, а не выведены из спецификации. Воспроизвести:

```
ENCX_LIVE_DOMAIN=demo.en.cx ENCX_LIVE_LOGIN=… ENCX_LIVE_PASSWORD=… \
  go test ./encx/ -run 'TestLive' -v
```

`encx/live_admin_test.go` создаёт собственную игру, гоняет через неё весь
админский CRUD и удаляет её за собой; `encx/live_player_test.go` покрывает
игровую сторону. Оба набора пропускаются без учётных данных.

Пути нового движка указаны относительно API-хоста (по умолчанию `api.<зона>`),
старого — относительно домена сайта.


## Авторизация и сессия

| Метод `encx.Client` | Старый движок | Новый движок |
|---|---|---|
| `Login` | POST /login/signin?json=1 | POST /login (JWT + X-En-Client) |
| `LoginComplete` | Login.aspx, затем /login/signin | POST /login + GET /auth/session |
| `VerifyAdminSession` | GET /Administration/Games/LevelManager.aspx | GET /auth/session |
| `GetProfile` | GET /UserDetails.aspx (HTML) | GET /auth/session, при необходимости GET /users/{id} |

## Игровой движок

| Метод `encx.Client` | Старый движок | Новый движок |
|---|---|---|
| `GetGameModel` | GET или POST-форма на /gameengines/encounter/play/{id}?json=1 | тот же маршрут, тело models.EngineClientMessage |
| `GetGameModelLevel` | GET /gameengines/encounter/play/{id}?json=1&level=N | тот же маршрут с level=N, ответ — models.EngineState (см. пробел про штурм) |
| `SendCode` | POST LevelAction.Answer | POST {"type":"answer"} |
| `SendBonusCode` | POST BonusAction.Answer | POST {"type":"bonus"} |
| `GetPenaltyHint` | GET ?pid=&pact=1 | POST {"type":"penalty","penalty_act":1} |

## Каталог и страница игры

| Метод `encx.Client` | Старый движок | Новый движок |
|---|---|---|
| `GetGameList` | GET /home/?json=1 | GET /games/home; для страниц >1 — /games/active и /games/coming |
| `GetDomainGames` | /home/?json=1, затем HTML главной | GET /games/home |
| `GetGameDetails` | GET /GameDetails.aspx (HTML) | GET /games/{id}/details (JSON вместо HTML) |
| `GetTimeoutToGame` | StartCounter со страницы движка | GET /games/{id}/fee-box -> seconds_to_start |
| `EnterGame` | GET /MakeGameFee.aspx?confirm=yes | POST /games/{id}/make-fee?confirm=yes |
| `FetchResource` | относительные ссылки от домена сайта | относительные ссылки от API-хоста (/media/...) |

## Статистика

| Метод `encx.Client` | Старый движок | Новый движок |
|---|---|---|
| `GetGameStatistics` | GET /gamestatistics/full/{id}?json=1 | GET /games/{id}/statistics |

## Команды

| Метод `encx.Client` | Старый движок | Новый движок |
|---|---|---|
| `GetTeamDetails` | GET /Teams/TeamDetails.aspx (HTML) | GET /teams/{id} (JSON вместо HTML) |
| `GetMyTeamDetails` | GET /Teams/TeamDetails.aspx | GET /auth/session -> GET /teams/{id} |
| `GetTeamManagementInfo` | разбор ссылок на TeamDetails.aspx | GET /teams/{id} |
| `GetTeamInvitations` | разбор своей страницы команды | GET /users/me/team-invitations |
| `AcceptTeamInvitation` | ?action=accept_invitation | POST /teams/{id}/invitations/respond {accept:true} |
| `RejectTeamInvitation` | ?action=reject_invitation | POST /teams/{id}/invitations/respond {accept:false} |
| `RequestTeamMembership` | POST /Teams/SendRequest.aspx | GET /teams?q= -> POST /teams/{id}/requests |
| `InviteTeamMember` | форма на TeamDetails.aspx | POST /teams/{id}/invitations |
| `RemoveTeamInvitation` | ?action=remove_invitation | DELETE /teams/{id}/invitations/{userId} |
| `LeaveTeam` | ссылка действия на странице команды | POST /teams/{id}/leave |
| `RenameTeam` | форма на TeamDetails.aspx | PUT /teams/{id} (read-modify-write) |
| `SetTeamSite` | форма на TeamDetails.aspx | PUT /teams/{id} (read-modify-write) |
| `SetTeamForum` | форма на TeamDetails.aspx | PUT /teams/{id} (read-modify-write) |

## Сценарий

| Метод `encx.Client` | Старый движок | Новый движок |
|---|---|---|
| `GetGameScenario` | GET /GameScenario.aspx + HTML-парсер | GET /games/{id}/scenario |
| `GetGameScenarioHTML` | GET /GameScenario.aspx | **нет**: HTML-экспорта не существует, метод возвращает ошибку с указанием GetGameScenario |

## Админка: игры и уровни

| Метод `encx.Client` | Старый движок | Новый движок |
|---|---|---|
| `AdminCreateGame` | POST Administration/Games/GameCreate.aspx, id из редиректа (?gid=) | POST /admin/games, id из game_id ответа |
| `AdminGetGames` | GET /Administration/GamesManager.aspx (Status не заполняется) | GET /admin/games, все страницы |
| `AdminGetLevels` | GET /Administration/Games/LevelManager.aspx | GET /admin/games/{id}/levels |
| `AdminCreateLevels` | LevelManager.aspx?levels=create | POST /admin/games/{id}/levels (по одному на уровень) |
| `AdminDeleteLevel` | LevelManager.aspx?levels=delete | DELETE /admin/games/{id}/levels/{levelId} |
| `AdminRenameLevels` | форма LevelManager | PUT /admin/games/{id}/levels/{levelId}/meta |
| `AdminSwapLevels` | LevelManager.aspx?levels=swap&ddlSwapLevels1/2 | POST /admin/games/{id}/levels/exchange (результат проверяется — см. пробелы) |
| `AdminInsertLevel` | LevelManager.aspx?levels=insert&ddlInsertAfterSrc/Dst | POST /admin/games/{id}/levels/put (результат проверяется — см. пробелы) |
| `AdminCloneLevels` | LevelManager.aspx?levels=createlike | POST /admin/games/{id}/levels/copy |
| `AdminGetComment` | LevelManager | GET /admin/games/{id}/levels |
| `AdminUpdateComment` | форма LevelManager | PUT /admin/games/{id}/levels/{levelId}/meta |

## Админка: содержимое уровня

| Метод `encx.Client` | Старый движок | Новый движок |
|---|---|---|
| `AdminGetTaskIds` | LevelEditor.aspx | GET /admin/games/{id}/levels/{levelId}/editor |
| `AdminGetTask` | TaskEdit.aspx | тот же editor-документ |
| `AdminCreateTask` | POST TaskEdit.aspx | POST .../levels/{levelId}/tasks |
| `AdminUpdateTask` | POST TaskEdit.aspx | PUT .../tasks/{taskId} |
| `AdminDeleteTask` | ссылка LevelEditor | DELETE .../tasks/{taskId} |
| `AdminGetHintIds` | LevelEditor.aspx | editor-документ (helps + penalty_helps) |
| `AdminGetHint` | HelpEdit.aspx | тот же editor-документ |
| `AdminCreateHint` | POST HelpEdit.aspx | POST .../levels/{levelId}/helps |
| `AdminUpdateHint` | POST HelpEdit.aspx | PUT .../helps/{helpId} |
| `AdminDeleteHint` | ссылка LevelEditor | DELETE .../helps/{helpId} |
| `AdminGetBonusIds` | LevelEditor.aspx | editor-документ |
| `AdminGetBonus` | BonusEdit.aspx | editor-документ |
| `AdminCreateBonus` | POST BonusEdit.aspx | POST .../levels/{levelId}/bonuses |
| `AdminUpdateBonus` | POST BonusEdit.aspx | PUT .../bonuses/{bonusId} |
| `AdminDeleteBonus` | ссылка LevelEditor | DELETE .../bonuses/{bonusId} |
| `AdminGetMessageIds` | LevelEditor.aspx | editor-документ |
| `AdminGetMessage` | MessageEdit.aspx | editor-документ |
| `AdminCreateMessage` | POST MessageEdit.aspx | POST .../levels/{levelId}/messages |
| `AdminUpdateMessage` | POST MessageEdit.aspx | PUT .../messages/{messageId} |
| `AdminDeleteMessage` | ссылка LevelEditor | DELETE .../messages/{messageId} |
| `AdminGetSectorRefs` | ALoader/LevelInfo.aspx | editor-документ (sectors) |
| `AdminGetSectorAnswers` | LevelEditor.aspx?swanswers=1 | editor-документ (sectors + answers) |
| `AdminCreateSector` | форма LevelEditor | POST .../sectors, затем POST .../answers/batch |
| `AdminUpdateSector` | форма LevelEditor | PUT .../sectors/{sectorId} + сверка ответов |
| `AdminDeleteSector` | ?delsector= | DELETE .../sectors/{sectorId} |
| `AdminAddSectorAnswers` | форма LevelEditor | POST .../answers/batch |
| `AdminClearLevelSectors` | перебор ?delsector= | DELETE .../sectors/{sectorId} по каждому сектору |
| `AdminGetLevelSettings` | разбор LevelEditor.aspx | editor-документ |
| `AdminUpdateAutopass` | форма LevelEditor | PUT .../levels/{levelId}/autopass |
| `AdminUpdateAnswerBlock` | форма LevelEditor | PUT .../levels/{levelId}/settings (section=blocking) |
| `AdminUpdateSectorCompletion` | форма LevelEditor | PUT .../levels/{levelId}/settings (section=sectors) |
| `AdminGetTeams` | выпадающий список forMemberID на TaskEdit.aspx | editor-документ (members) |

## Админка: управление игрой

| Метод `encx.Client` | Старый движок | Новый движок |
|---|---|---|
| `AdminGetGameInfo` | GET /Administration/Games/GameEditor.aspx | GET /admin/games/{id} |
| `AdminUpdateGameInfo` | POST GameEditor.aspx | PATCH /admin/games/{id} (частичное обновление) |
| `AdminDeleteGame` | GamesManager.aspx?page=1&action=Delete | DELETE /admin/games/{id} |
| `AdminDeliverGame` | ?action=Deliver | PUT /admin/games/{id}/status — **требуется код status_id** |
| `AdminNotDeliverGame` | ?action=NotDeliver | PUT /admin/games/{id}/status — **требуется код status_id** |
| `AdminAwardPoints` | ?action=AwardPoints | POST /admin/games/{id}/points/calculate |
| `AdminEndRatings` | ?action=EndRatings | POST /admin/games/{id}/rate/close |
| `AdminCalculateIK` | ?action=CalcIK | POST /admin/games/{id}/quality-index |
| `AdminGetCorrections` | GET Corrections.aspx | GET /games/{id}/corrections |
| `AdminAddCorrection` | POST Corrections.aspx | GET /games/{id}/corrections (резолв имён) -> POST /games/{id}/corrections |
| `AdminDeleteCorrection` | ссылка Corrections.aspx | DELETE /games/{id}/corrections/{correctId} |
| `AdminGetActionMonitor` | GET ActionMonitor.aspx | GET /games/{id}/monitoring |

## Методы, не зависящие от движка

| Метод `encx.Client` | Примечание |
|---|---|
| `AdminCopyGame` | оркестрация над другими методами encx.Client — работает на обоих движках без отдельной реализации |
| `AdminWipeGame` | оркестрация над другими методами encx.Client — работает на обоих движках без отдельной реализации |
| `ExportCookies` | сессия: массив cookies на legacy, объект с apiToken когда задействован новый движок |
| `ImportCookies` | принимает оба формата |
| `APIToken` | JWT нового движка (пусто на legacy) |
| `SetAPIToken` | восстановление ранее полученного JWT |
| `APIBaseURL` | хост нового движка |
| `Engine` | фактически выбранный движок |
| `EngineMode` | запрошенный режим (может быть auto) |
| `SetEngine` | смена движка в рантайме |
| `AdminDelay` | троттлинг ASP-форм, к REST не применяется |
| `SetAdminDelay` | то же |
| `AdminGETDelay` | то же |
| `ClearHAR` | запись HAR — общая для обоих движков |
| `ClearHARFirst` | то же |
| `ExportHARJSON` | то же |
| `ExportHARSnapshot` | то же |
| `HAREntryCount` | то же |
| `SetHARRecordingEnabled` | то же |
| `LoginForAntiSpamRecovery` | на новом движке сводится к обычному Login: стены NotHumanRequest там нет |
| `LoginViaLoginPage` | **legacy-only**: форма Login.aspx существует только в ASP-движке |
| `ResolveAntiSpamLoginURL` | **legacy-only**: разбор страницы NotHumanRequest.aspx |

## Известные пробелы

- `AdminSwapLevels` / `AdminInsertLevel`: на развёрнутом бэкенде маршруты
  `POST /levels/exchange` и `POST /levels/put` отвечают `204 No Content` и **не
  меняют порядок уровней**. Проверено на demo.en.cx в 13 контролируемых прогонах:
  одиночная перестановка, повторная, смежных и несмежных уровней, при
  `levels_sequence_id` 0/1/2, с ожиданием до 5 секунд — порядок не меняется ни в
  менеджере уровней, ни в редакторе уровня, ни в игровой модели. Передача номеров
  вместо идентификаторов даёт `404 level not found`, то есть обработчик находит
  уровни и всё равно ничего не делает. Поэтому обе операции **перечитывают список
  уровней и возвращают ошибку**, если порядок не изменился: молчаливый успех
  оставил бы импорт или редактор уверенными в порядке, которого в игре нет.
  Перестановка, которая и так уже выполнена, ошибкой не считается, а порядок,
  ставший отличным и от исходного, и от запрошенного, докладывается отдельно —
  это уже не «движок проигнорировал», а другой результат.
  `can_manipulate_levels` из ответа менеджера проверяется до записи всеми пятью
  методами, меняющими состав уровней (создание, удаление, клонирование,
  перестановка, вставка), чтобы отказ назывался своей причиной. Замерено, что
  это безопасно: у начавшихся игр (26365, 26190, 25590, 25487) поле истинно, а
  ложным для них становится соседнее `can_change_levels_sequence`. Иначе проверка
  ломала бы правку уровней в идущей игре, которую старый движок разрешает.
- Язык: движок локализует то, что рендерит сам (названия рангов, длительности
  корректировок), по заголовку `Accept-Language`; параметр `lang` принимают лишь
  отдельные маршруты (`/games/{id}/statistics`, движковый маршрут). `enapi.Client`
  шлёт `Accept-Language` из настроенного языка — без него весь API отвечал
  по-английски там, где старый движок отдавал язык сайта. Паритет при этом
  условный: `legacyAdminGetCorrections` жёстко запрашивает `lang=ru` независимо
  от `WithLang`, поэтому при `WithLang("en")` движки разойдутся — старый всё
  равно ответит по-русски.
- Постраничные маршруты. Три метода принимают от движка постраничный ответ и не
  имеют параметра страницы в сигнатуре, поэтому читают все страницы и сообщают
  об ошибке, если движок так и не дошёл до конца: `AdminGetGames`,
  `GetGameStatistics` и `AdminGetActionMonitor`. Для статистики это не
  косметика: `LevelStatInfo.PassedPlayers` и `LevelPlayerCount.Count`
  вычисляются из самих строк, поэтому неполная страница дала бы не короткий
  ответ, а неверные итоги.
- `GetGameStatistics`: движок публикует поправки времени отдельным списком
  `level_corrections`, а не внутри каждой строки, как старый документ. Это
  агрегат по игре целиком (`GetSummByGameID` в спецификации), повторяющийся на
  каждой странице, поэтому при склейке страниц он не накапливается — иначе
  поправка умножалась бы на число прочитанных страниц. Значения сводятся в
  `StatItem.Corrections` по паре «уровень + участник» со знаком (бонус
  отрицателен, штраф положителен); агрегатные группы под отрицательными ключами
  (-1 общее время, -2 чистое) поправок не получают — это уже посчитанные движком
  итоги. Замер на игре 27053: 486 поправок, все с `user_id` и `level_id`, 483 из
  них нашли свою строку; остальные принадлежат игрокам без строки на этом уровне.
- `GetGameStatistics` и `needs_confirm`: если автор закрыл статистику, движок
  отвечает 200 с пустой таблицей и этим флагом. Старый маршрут `full` отдавал
  таблицу, поэтому запрос повторяется один раз с `confirm=1`. Именно один раз и
  только в ответ на флаг: сервер записывает такой просмотр в журнал, а слать
  подтверждение на каждое чтение любой игры — побочный эффект, которого у старого
  вызова не было. Если и после подтверждения флаг стоит, возвращается ошибка, а
  не пустая таблица.
- Командные операции проверяются после записи, как это делали страницы старого
  движка: приём и отклонение приглашения, выход из команды, переименование и
  смена сайта/форума перечитывают состояние и возвращают `*TeamActionError`,
  если оно не изменилось. Неудача самой проверки (401, таймаут) — не успех, а
  ошибка «verify state». Членство читается из `GET /teams/{id}/members`, а не из
  `/auth/session`: сессию можно собрать из предъявленного токена, и тогда она
  назовёт прежнюю команду — проверка на ней превращала бы каждый удачный приём
  приглашения и выход в жёсткую ошибку. Сравнение сайта и форума ведётся по
  нормализованному виду (регистр, схема, хвостовой слэш), потому что серверы
  ссылки переписывают; «значение просто изменилось» критерием не годится — так
  прошла бы и подстановка сервером чего-то своего вместо отклонённой ссылки.
  Добавленные сервером путь и поддомен принимаются, но только с якорем по
  разделителю: без него `evil-team.example` сошло бы за `team.example`.
  Форма ответа `GET /teams/{id}/members` взята с живого стенда — там `user_id`,
  ключа `id` нет; спецификация описывает маршрут как массив `models.User` с
  ключом `id` и без флагов членства, поэтому читаются оба написания.
  `InviteTeamMember` и `RemoveTeamInvitation` проверить
  нечем — списка исходящих приглашений новый движок не публикует (см. пробел про
  `TeamManagementInfo`), поэтому там успех означает только принятый запрос.
- `Profile.Rank`: новый API публикует только `rank_sentence_key` (справочник
  `GET /ranks` тоже отдаёт ключи, не строки), поэтому в `Rank` попадает ключ
  фразы, а не готовая надпись, которую показывала HTML-страница старого движка.
- `AdminCorrection.Reason`: заполняется из `correct_text`, который на
  корректировках, порождённых бонусами, приходит пустым — колонка «причина»
  старой страницы для таких строк остаётся пустой.
- `AdminGetCorrections`: `GET /games/{id}/corrections` отвечает `403` для игры,
  которая ещё не стартовала, тогда как `Corrections.aspx` рисовал пустую таблицу.
  Замер на demo.en.cx: корректировки читаются у любой стартовавшей игры, включая
  чужие, и отказ приходит только на своей неначавшейся. Пустой список поэтому
  возвращается лишь при обоих условиях сразу — игра наша И не начата; всё
  остальное пробрасывается как есть. Признак «игра наша» — `GET /admin/games/{id}`
  (403 на чужой игре). `GET /admin/games/{id}/lifecycle` для этого не годится:
  он отвечает 200 на любую игру, в том числе чужую.
- `AdminGameInfo.AuthorComplexity`: соответствует `afc`, умноженному на 10.
  Измерено на demo.en.cx: маршрут принимает `afc` в диапазоне 0..1 включительно
  (1.0001 отвергается) и хранит значение с шагом 0.1, отбрасывая остаток
  (0.25 -> 0.2, 0.99 -> 0.9). То есть поле имеет ровно 11 состояний — это в
  точности целочисленный диапазон 0..10 старой выпадашки `ddlAuthorsCompexity`,
  чьё умолчание равно 10; спецификация подтверждает соотношение примечанием
  «SP stores *10». Дробные legacy-значения теряют дробную часть (`3.5` -> `3`),
  значение вне 0..10 отклоняется до отправки запроса.
- `AdminGameInfo.Prize`: `prize_cents` названо по колонке, а не по единице
  измерения — запись `prize_cents=1234` читается обратно как `models.Game.prize`
  = 1234 (проверено вживую). Значение передаётся без пересчёта, как старая форма
  передавала своё поле `Prize`. Прежнее умножение на 100 превращало обычный
  read-modify-write в стократное повышение, а за пределом сервера — в `HTTP 400`.
  Осторожно: `GameInfo.Fee`/`Prize` в каталоге по-прежнему заполняются как
  `Money{Value: v, Cents: v*100}`; в какой единице сервер хранит саму величину,
  спецификация не задаёт.
- `AdminLevelSettings` (автопроход): движок хранит награду за таймаут **со
  знаком** — `timeout_time_award_sec` отрицательное для штрафа и положительное
  для бонуса (запись `award_is_penalty=true` с 15 минутами читается как `-900`).
  Старая форма выражала то же парой «галочка + беззнаковая длительность», поэтому
  знак читается в `TimeoutPenalty`, а модуль — в `Penalty*`.
- `AdminLevelSettings.RequiredSectorsCount`: значимо только при
  `passing_condition_id == 1`; поле сохраняет прежнее значение при других
  условиях (0 — все секторы, 2 — очки), поэтому при них возвращается 0, как и на
  старом движке.
- `AdminGetGames`: админский список постраничный и сообщает `total_pages`.
  Метод не принимает номера страницы, поэтому читаются все страницы (с потолком в
  100 запросов и отбрасыванием повторов) — иначе вызывающий получал бы
  необозначенный префикс своих игр. Старая страница `GamesManager.aspx` тоже
  постраничная, но своего пейджера не публиковала, так что усечение там было
  незаметно и для самого клиента.
- `AdminUpdateSector`: непустой список ответов **заменяет** ответы сектора,
  пустой — оставляет их нетронутыми (меняется только имя). Так же читается
  «пустое поле значит не трогать» в остальном API нового движка.
- `ErrSectorStarted`: экспортированный сентинел, на который вызывающие проверяют
  через `errors.Is`. Новый API не описывает тело ошибки для этого случая, но
  наблюдаемое условие то же, что проверял старый клиент: удаление не прошло, а
  сектор остался на уровне. `AdminDeleteSector` перечитывает уровень при ошибке и
  возвращает `ErrSectorStarted`, если сектор на месте — но только когда движок
  именно ОТКАЗАЛ (400, 403, 409). 500 или таймаут, которые сектор пережил, — это
  сбой попытки, а не неудаляемый сектор, и объявлять их сентинелом значило бы
  увести вызывающего от повтора, который сработал бы. Ответ 404 — «сектора уже
  нет» — считается достигнутой целью: идентификатор взят из чтения одним
  запросом раньше, и гонка с чужим удалением не должна ронять очистку уровня
  (старый клиент перечитывал список каждый круг и просто не видел исчезнувший
  сектор). `AdminClearLevelSectors`
  не бросает уровень на первом отказе, а проходит все секторы — но возвращает
  `ErrSectorStarted`, если хоть один остался: старый цикл с повторами тоже
  докладывал успех только на пустом уровне, а `cmd/encli/import_scenario.go`
  пишет новые секторы поверх «очищенного».
- Бонус на нескольких уровнях: `AdminBonus.LevelID` — одно число, поэтому чтение
  бонуса, привязанного к уровням 3, 5 и 7, вернёт только первый, а обратная
  запись сузит бонус до него. Старый движок такого бонуса выразить не мог вовсе,
  но модель нового может, и round-trip через `AdminGetBonus`/`AdminUpdateBonus`
  её разрушает.
- `AdminGame.Number`: старый менеджер игр номер не публикует и оставляет 0, новый
  заполняет его из `game_num`.
- `GetGameList` для страниц >1: первая страница приходит из непостраничного
  `/games/home`, последующие — из `/games/active` и `/games/coming` с их
  собственным размером страницы. Если в домашнем блоке окажется не ровно столько
  игр, сколько в странице каталога, перебор страниц может пропустить или
  повторить игру. Унаследовано от исходной реализации, живым прогоном не
  проверялось.
- `GetDomainGames`: у старого движка была цепочка запасных вариантов (JSON, затем
  мобильный и десктопный HTML главной), потому что часть доменов публиковала
  список игр только в разметке. Новый движок отдаёт каталог как данные, поэтому
  запасных путей нет: домен без игр в каталоге вернёт пустой список.
- `AdminCreateMessage(gameId, levelID, …)` принимает **идентификатор** уровня, а
  `AdminUpdateMessage(gameId, levelNum, …)` — **номер**. Асимметрия унаследована
  от сигнатур старого движка (там оба значения попадали в один и тот же параметр
  страницы) и сохранена, чтобы не ломать вызывающих.
- `AdminDeliverGame` / `AdminNotDeliverGame`: новый API меняет статус игры через
  `PUT /admin/games/{id}/status` с числовым `status_id`, но значения не описаны
  ни в swagger, ни где-либо ещё, а операция необратима. До получения кодов оба
  метода на новом движке возвращают явную ошибку и не отправляют запрос;
  подставить значения можно через `encx.GameStatusDelivered` и
  `encx.GameStatusCancelled`.
- `GetGameScenarioHTML`: HTML-экспорта сценария в новом движке нет. Используйте
  `GetGameScenario`, который работает на обоих.
- `GetGameDetails` и `GetTeamDetails`: на legacy возвращают HTML-страницу, на
  новом движке — тот же документ в JSON, потому что HTML-эквивалента не
  существует.
- `GameStatisticsResponse.Level` и `.User`: в `models.GameStatisticsResponse`
  аналогов нет, поля остаются `nil`.
- `GetGameModelLevel`: параметр `level` выбирает уровень только в играх-штурмах
  (спецификация: «номер уровня (штурм)», legacy-комментарий говорит то же). В
  линейной игре оба движка отвечают тем уровнем, на котором игрок сейчас, а
  запрошенный номер игнорируют — это не расхождение, а смысл параметра. Проверено
  на demo.en.cx: игра 27053 линейная (`levels_sequence_id=0`), и ни одно
  написание параметра (`level`, `level_number`, `level_id`) уровень не меняет.
  Поведение в игре-штурме живьём не проверялось: учётная запись не участвует ни в
  одной из штурмовых игр домена, а вступать в чужую игру ради проверки нельзя.
- `GameStatisticsResponse.PagerVisible` всегда `false`: на старой странице оно
  значило «таблица показана не целиком», а здесь все страницы уже прочитаны, и
  предлагать по нему пейджер значило бы звать за строками, которые у вызывающего
  уже есть.
- `TeamManagementInfo.PendingInvitations` и `.Actions`: описывают HTML-страницу
  команды; новый движок список исходящих приглашений не публикует, а операции
  выражает маршрутами.
- `AdminGameInfo`: поля `NotFirstPlaces` и `AcceptRateMode` не имеют
  соответствия в `models.AdminUpdateGameRequest` и не переносятся. Кроме того,
  legacy-форма подставляет значения по умолчанию для пустых полей, а REST
  трактует пустое поле как «не менять».
- `AdminGame.Status`: не заполняется ни на одном движке. Legacy-страница его не
  публикует, а в админском списке нового движка есть только числовой
  `status_id`, значения которого не описаны.
- `AdminHint.ReplaceNl`: в `models.AdminHelpRequest` нет соответствующего поля,
  на новом движке флаг не переносится и читается как `false`.
- `EngineAction.PenaltyAction` и причина отказа (`reject_reason`): в
  `models.EngineActionSnapshot` нет данных для `PenaltyActionResult`, поле
  остаётся `nil`.
- Сценарий: `whole_game_bonuses` (бонусы на всю игру) не попадают в
  `scenario.Document` — в модели документа нет места для бонусов вне уровня.

