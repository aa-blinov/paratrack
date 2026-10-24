# Tasks

## 1. Данные и правило записи

- [x] 1.1 Добавить `teams.autolock_days INTEGER NOT NULL DEFAULT 0` в `internal/db/schema_compat.go` рядом с `timezone`
- [x] 1.2 Добавить `AutoLockDays` в `model.Team` и читать колонку в запросах `internal/db/teams.go`: команда, список команд и членство (`FindMembershipForUser`, `ListMembershipsForUser`)
- [x] 1.3 Добавить `model.ErrDateClosed` и `model.SessionAutoLockError{Before}` рядом с утверждённой неделей
- [x] 1.4 Создать `internal/db/timesheet_autolock.go`: `TimesheetAutoLockCutoff` (дата отсечки в поясе пространства, UTC при пустом поясе) и `lockAutoClosed` для записи с известным началом
- [x] 1.5 Подключить `lockAutoClosed` в `sessions.go` (старт таймера, правка, создание закрытой сессии, удаление), в `timesheet.go` (ячейка и строка) и в `session_transitions.go` (возврат остановленной сессии); в `UpdateSessionEnd` не подключать

## 2. Настройка пространства

- [x] 2.1 Добавить `AutoLockDays` в `appmodel.TeamCalendarRequest`, проверять допустимые значения 0, 3, 7, 14, 30 и писать колонку в `SetTeamCalendar`
- [x] 2.2 В `handleAPITeamCalendar` читать `autolock_days`; форма без поля сохраняет текущий срок, недопустимое значение отклоняется как неверный ввод
- [x] 2.3 В карточке календаря `web/src/settings/team-settings-page.tsx` добавить select «Закрывать правки старше» с вариантами «Выключено», «3 дня», «7 дней», «14 дней», «30 дней»; ключи парные в `ru.json` и `en.json`

## 3. Отказы и страница табеля

- [x] 3.1 Маппить `SessionAutoLockError` в `handlers_timesheet.go`, `handlers_timers.go` (общий ответ сессий, в том числе `/api/v1` с 409) и `handlers_backfill.go` с текстом «Правки до ДД.ММ закрыты настройкой пространства»
- [x] 3.2 Добавить `TimesheetAutoLockCutoff` в tracking service и в `dependencies.go`, передать в представление табеля поле `AutoLockNotice`
- [x] 3.3 В `web/src/timesheet/timesheet-page.tsx` вывести `AutoLockNotice`, если строка не пустая; пересобрать `make ui`, бандл коммитить вместе с кодом

## 4. Документация

- [x] 4.1 Дописать в `docs/PRODUCT.md`, раздел «Approved weeks» (новый подраздел «Closing past time by age»), правило закрытия по сроку и исключение для остановки таймера
- [x] 4.2 Добавить TIME-22 в `docs/backlog/time.md` и отметить [x] с датой 2026-10-24 в том же коммите, что и код

## 5. Проверка

- [x] 5.1 Go-тесты в `internal/db`, `internal/web` и `internal/appmodel`: отказ по каждому пути записи, граница месяца, граница дня в поясе пространства, остановка забытого таймера, правка забытого таймера отклоняется, срок выключен не меняет поведение, форма без срока не сбрасывает его, участник не меняет срок
- [x] 5.2 `make verify` (прошёл) и полный набор `go test` через `scripts/test.sh`, при занятом порте 15432 через копию скрипта на 15433
- [x] 5.3 Пересобрать QA-стенд (`scripts/qa-env.sh down && up`, прошёл) и прогнать CI-список e2e
- [ ] 5.4 Руками на QA-стенде: включить срок 7 дней, добавить сессию на 10 октября, проверить отказ и строку на табеле, выключить срок и повторить
