# Tasks

## 1. Основа

- [ ] 1.1 Проверить на стенде страницу проекта с двумя людьми на плане: карточка «Спланировано за всё время» и сумма строк. Проверка: в описании задачи записаны обе цифры до правки.
- [ ] 1.2 Добавить ключ `proj.totalAllTime` в `ru.json` («Итого за всё время») и `en.json` («Total, all time»). Проверка: `go test ./internal/i18n` зелёный.

## 2. Данные страницы

- [ ] 2.1 В `internal/web/project_plan.go` добавить в `projectPlanView` поле `TrackedLabel`: сумма учтённого по участникам, которых видно (`showTracked`), пусто без права. Считать в том же цикле, что `trackedTotal`. Проверка: тест в 3.1.
- [ ] 2.2 Передать `TrackedLabel` в данные страницы (`handlers_projects_pages.go`, `view_projects.go`). Проверка: `go vet ./internal/web/`.

## 3. Тесты

- [ ] 3.1 `internal/web/project_plan_total_test.go`: на проекте два человека с планом 10 и 5 часов: `PlannedLabel` = 15 ч и совпадает с суммой строк; для руководителя `TrackedLabel` = сумме учтённого; для участника без права `TrackedLabel` пуст; пустой план даёт пустой `People` и пустой итог. Проверка: зелёный.

## 4. Страница

- [ ] 4.1 `web/src/projects/project-detail.tsx`: убрать `SummaryCard` с `proj.plannedAll`; добавить строку «Итого за всё время» в конец таблицы «По людям» при непустой таблице (колонка плана: `PlannedLabel`, колонка учтённого: `TrackedLabel` при `CanManage`). Проверка: `npx tsc --noEmit -p .`.
- [ ] 4.2 `web/src/dashboard/types.ts`: поле `TrackedLabel` в типе данных страницы проекта. Проверка: `npx tsc --noEmit -p .`.

## 5. Бандл и проверка

- [ ] 5.1 `make ui` (бандл), коммит бандла вместе с кодом. Проверка: `make ui` без ошибок.
- [ ] 5.2 Холодный старт стенда (`scripts/qa-env.sh down && up`) и новый сценарий `e2e/test_project_plan_single.py`: одна сумма плана, строка итога равна сумме строк, карточки «Спланировано за всё время» нет, на 375 px строка итога в пределах экрана. Проверка: зелёный.
- [ ] 5.3 Связанные e2e на том же стенде: `test_dashboard.py`, `test_tasks.py`, `test_clients.py` (там есть страницы проектов). Проверка: зелёные.
- [ ] 5.4 Полный прогон Go-пакетов (`scripts/go-packages.sh | xargs scripts/test.sh`) с кодом 0. Проверка: зелёный.
- [ ] 5.5 `make verify` и `scripts/check-architecture.sh`. Проверка: зелёные.

## 6. Документы и backlog

- [ ] 6.1 `docs/backlog/planning.md`: PLAN-03 отмечен `[x]` с датой. `CHANGELOG.md`, раздел `[Unreleased]`: строка про итог плана на странице проекта. Проверка: строки на месте.

## Workflow follow-up

- Коммит и пуш в master только по слову пользователя; после пуша смотрим CI до зелёного.
- Архивация изменения после проверок 5.4-5.5.
