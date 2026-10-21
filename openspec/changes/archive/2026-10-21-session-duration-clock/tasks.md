# Tasks

## 1. Сервер и формат

- [x] 1.1 `internal/timeparse/timeparse.go`, `ParseDuration`: ветка `H:MM` до токенизатора, `^\d{1,3}:[0-5]\d$`, ноль отвергается. Проверка: `go test -count=1 ./internal/timeparse` зелёный, в таблице теста есть `1:30`, `1:05`, `0:45`, `1:60` и `1:5` (отказ).
- [x] 1.2 `internal/web/formatters.go`, `durationToHuman`: для `sec >= 60` возвращает `H:MM`, для `0 < sec < 60` прежний вид «N с». Проверка: `go test -count=1 ./internal/web -run 'Duration|Session'` зелёный, в тесте поле для 5400 секунд даёт `1:30`, для 45 секунд даёт `45 с`.

## 2. Интерфейс и словари

- [x] 2.1 `web/src/stats/stats-page.tsx`: строка-пример `stats.durationFormat` под полем длительности; подсказка редактора выводится всегда, без условия `editorOpen`. Проверка: `npx tsc --noEmit -p .` в `web/`.
- [x] 2.2 `internal/i18n/dict/ru.json` и `en.json`: `ph.duration` → `1:30`; `err.sessionBadDuration` по D5; новый `stats.durationFormat`; удалить `stats.editorFolded`. Проверка: `go test -count=1 ./internal/i18n` зелёный.
- [x] 2.3 `make ui`, бандл коммитится вместе с кодом. Проверка: `make ui` без ошибок.

## 3. Проверка

- [x] 3.1 `e2e/test_session_edit_enter.py`: ввод `1:30`, поле показывает `1:30`, подсказка формата видна на десктопе без «Показать», подсказка редактора видна на десктопе. Проверка: зелёный на свежем стенде.
- [x] 3.2 `e2e/test_session_project.py` на том же стенде. Проверка: зелёный.
- [x] 3.3 `make verify` и `scripts/check-architecture.sh`. Проверка: зелёные.

## 4. Документы и backlog

- [x] 4.1 `docs/backlog/time.md`: TIME-09 отмечен `[x]` с датой; пункт про обрезку секунд при правке заметки добавлен как новый пункт в `docs/backlog/time.md` (P1, S). `CHANGELOG.md`, раздел `[Unreleased]`: строка про формат `1:30` и подсказку. Проверка: строки на месте.

## Workflow follow-up

- Коммит и пуш в master только по слову пользователя; после пуша смотрим CI до зелёного.
- Архивация после проверок 3.1-3.3.
