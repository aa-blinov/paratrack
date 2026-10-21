# Tasks

## 1. Сервер

- [x] 1.1 `internal/web/view_dashboard_data.go`: константа `dayNormSeconds = 6 * 3600` с комментарием о регламенте; поле `DayNormLabel` в `dashboardData` (`view_dashboard.go`), заполняется через `fmtDur`. Проверка: `go vet ./internal/web`.
- [x] 1.2 Тест `internal/web/dashboard_day_norm_test.go`: на главной `reactData[dashboardData]` даёт `DayNormLabel`, равный `fmtDur(6 ч)`. Проверка: `scripts/test.sh -count=1 -run TestDashboardDayNorm ./internal/web` зелёный.

## 2. Интерфейс и словари

- [x] 2.1 `web/src/dashboard/types.ts`: поле `DayNormLabel` в типе обзора. Проверка: `npx tsc --noEmit -p .` в `web/`.
- [x] 2.2 `web/src/main.tsx`: строка `dash.dayNorm` под суммой в шапке. Проверка: `npx tsc --noEmit -p .`.
- [x] 2.3 `internal/i18n/dict/ru.json` и `en.json`: ключ `dash.dayNorm` с `%s`. Проверка: `go test -count=1 ./internal/i18n` зелёный.
- [x] 2.4 `make ui`, бандл коммитится вместе с кодом.

## 3. Проверка

- [x] 3.1 `e2e/test_dashboard.py` на свежем стенде: зелёный (шапка обзора, регрессия).
- [x] 3.2 `make verify` и `scripts/check-architecture.sh`: зелёные.

## 4. Документы и backlog

- [x] 4.1 `docs/backlog/time.md`: TIME-07 отмечен `[x]` с датой. `CHANGELOG.md`, раздел `[Unreleased]`: строка про норму дня на обзоре.

## Workflow follow-up

- Архивация после проверок 3.1-3.2.
