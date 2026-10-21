# Tasks

## 1. Редактор сессии

- [x] 1.1 `web/src/stats/stats-page.tsx`, `EditableSession`: вынести сравнение с сохранённым значением в `commitDuration` и `commitNote`, которые вызывают `onBlur`; новый `onKeyDown` идёт через `commitOnEnter`, который вызывает коммит, только если Enter без активной композиции (`event.nativeEvent.isComposing` false). Проверка: `npx tsc --noEmit -p .` в `web/`.
- [x] 1.2 `internal/i18n/dict/ru.json` и `en.json`: текст `stats.editHint` по D3 из design.md. Проверка: `go test -count=1 ./internal/i18n` зелёный.

## 2. Бандл

- [x] 2.1 `make ui`, бандл коммитится вместе с кодом. Проверка: `make ui` без ошибок, в `git status` только файлы этого change и бандл.

## 3. Проверка

- [x] 3.1 Новый сценарий `e2e/test_session_edit_enter.py` на стенде: Enter в длительности сохраняет одним PATCH; Enter без изменения запрос не шлёт; Enter в заметке сохраняет; подсказка говорит про Enter; отказ по утверждённой неделе виден после Enter. Композиция IME в Playwright не эмулируется, это проверяется чтением кода (1.1). Проверка: зелёный.
- [x] 3.2 `e2e/test_session_project.py` на том же стенде: редактор проекта не сломан. Проверка: зелёный.
- [x] 3.3 `make verify` и `scripts/check-architecture.sh`. Проверка: зелёные.

## 4. Документы и backlog

- [x] 4.1 `docs/backlog/time.md`: TIME-08 отмечен `[x]` с датой. `CHANGELOG.md`, раздел `[Unreleased]`: строка про Enter в редакторе сессии. Проверка: строки на месте.

## Workflow follow-up

- Коммит и пуш в master только по слову пользователя; после пуша смотрим CI до зелёного.
- Архивация после проверок 3.1-3.3.
