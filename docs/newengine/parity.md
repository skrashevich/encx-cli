# Матрица паритета движков

Каждый экспортируемый метод `encx.Client` перечислен ниже: где он живёт в
старом ASP.NET-движке и чем обслуживается в новом REST-движке
(`docs/newengine/swagger.json`). Пробелы отмечены явно.

Файл сверяется тестом `TestEngineParityMatrixCoversEveryMethod`: метод, который
диспетчеризуется по движкам, но отсутствует в таблице, роняет сборку — так
документация не может разойтись с кодом.

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
| `GetGameModelLevel` | GET /gameengines/encounter/play/{id}?json=1&level=N | тот же маршрут с level=N, ответ — models.EngineState |
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
| `AdminGetGames` | GET /Administration/GamesManager.aspx (Status не заполняется) | GET /admin/games |
| `AdminGetLevels` | GET /Administration/Games/LevelManager.aspx | GET /admin/games/{id}/levels |
| `AdminCreateLevels` | LevelManager.aspx?levels=create | POST /admin/games/{id}/levels (по одному на уровень) |
| `AdminDeleteLevel` | LevelManager.aspx?levels=delete | DELETE /admin/games/{id}/levels/{levelId} |
| `AdminRenameLevels` | форма LevelManager | PUT /admin/games/{id}/levels/{levelId}/meta |
| `AdminSwapLevels` | LevelManager.aspx?levels=swap&ddlSwapLevels1/2 | POST /admin/games/{id}/levels/exchange |
| `AdminInsertLevel` | LevelManager.aspx?levels=insert&ddlInsertAfterSrc/Dst | POST /admin/games/{id}/levels/put |
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
- `GameInfo.Fee`/`Prize`: `models.Game` отдаёт целые единицы валюты, а
  `AdminUpdateGameRequest` принимает `price_cents`/`prize_cents`; при записи
  значение умножается на 100. Единицы в спецификации не задокументированы —
  стоит перепроверить на живом стенде перед массовым редактированием призов.

