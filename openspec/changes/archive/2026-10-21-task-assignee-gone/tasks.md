# Tasks

## 1. Сервер

- [x] 1.1 `internal/web/handlers_tasks.go`: поле `AssigneeGone` в `taskItem`, вычисляется в `taskItems` по D1. Проверка: `scripts/test.sh -count=1 -run TestTaskAssignee ./internal/web` зелёный.
- [x] 1.2 Тест `internal/web/task_assignee_gone_test.go`: задача с исполнителем, который удалён через `/api/team/members/{id}/remove`, даёт `AssigneeGone` true и сохраняет `AssigneeID`; задача без исполнителя и задача с участником в команде дают false.

## 2. Интерфейс и словари

- [x] 2.1 `web/src/dashboard/types.ts`: поле `AssigneeGone` в типе задачи. Проверка: `npx tsc --noEmit -p .` в `web/`.
- [x] 2.2 `web/src/tasks/tasks-page.tsx`: таблица и мобильная карточка показывают `tasks.assigneeGone` с `text-destructive`. Проверка: `npx tsc --noEmit -p .`.
- [x] 2.3 `internal/i18n/dict/ru.json` и `en.json`: ключ `tasks.assigneeGone`. Проверка: `go test -count=1 ./internal/i18n` зелёный.
- [x] 2.4 `make ui`, бандл коммитится вместе с кодом.

## 3. Проверка

- [x] 3.1 `e2e/test_tasks.py` на свежем стенде: зелёный (регрессия таблицы задач).
- [x] 3.2 `make verify` и `scripts/check-architecture.sh`: зелёные.

## 4. Документы и backlog

- [x] 4.1 `docs/backlog/tasks.md`: TASK-06 отмечен `[x]` с датой. `docs/backlog/admin.md`: в ADM-05 убрать «задачи уходят без исполнителя» и написать, что ссылка сохраняется и задача показывает предупреждение. `CHANGELOG.md`, раздел `[Unreleased]`: строка про предупреждение.

## Workflow follow-up

- Архивация после проверок 3.1-3.2.
