# Tasks

## 1. Воспроизведение и основа

- [x] 1.1 Воспроизвести на QA-стенде отказ создания счёта за период без часов: в данных страницы `Flash` пуст, в адресе флеш есть. Проверка: `scratchpad/repro_fin24.py` показывает пустой `Flash` до правки; результат записан в описании задачи.
- [x] 1.2 Добавить `flashCode(ok, msg)` с текущим поведением `encodeFlash` (сырой код). Проверка: тест на `e:`-префикс и успешный вариант.

## 2. Экранирование в одной функции

- [x] 2.1 `encodeFlash(ok, msg)` возвращает `url.QueryEscape(flashCode(ok, msg))`. Проверка: round-trip тест (задача 3.1) проходит.
- [x] 2.2 Убрать внешнее `url.QueryEscape(...)` в 14 местах, где `encodeFlash` уже экранирован: `handlers_invoice_actions.go` (3), `handlers_integrations.go` (2, внутри `importFlash`), `modules.go` (2), `handlers_tasks.go` (1), `prefs.go` (1), `handlers_import.go` (1), `handlers_timesheet.go` (3), `handlers_invoices.go` (1). Проверка: `go vet ./internal/web/` и grep по `url.QueryEscape(encodeFlash` пуст.
- [x] 2.3 В `handlers_invites.go` (строки с `url.Values{"flash": …}`) заменить `encodeFlash` на `flashCode`. Проверка: web-тест на приглашение с отказом и успехом проходит.

## 3. Тесты

- [x] 3.1 `internal/web/flash_roundtrip_test.go`: набор сообщений с «;», «&», «#», «+», «%», кириллицей и пробелами, включая текст из словаря «За этот период нечего выставлять…». Для каждого: `encodeFlash` → `http.Redirect` → `url.Parse(Location)` → `Query().Get` → `decodeFlash` возвращает исходный текст и полярность. Проверка: зелёный.
- [x] 3.2 `internal/web/flash_escape_test.go`: разбор исходников пакета через `go/parser`; падает с именем файла и строкой, если найден `url.QueryEscape(encodeFlash(…))` или `url.Values` с `encodeFlash(…)`. Проверка: зелёный; сам детектор проверен отдельным тестом на обе формы двойного экранирования (`TestFlashEscapeDetectorFindsBothDoubleEscapes`).
- [x] 3.3 Web-тест счетов: POST `/invoices` за период без часов → `Location` → страница счетов → `data.Flash` содержит «За этот период нечего выставлять», `FlashOK` false. Проверка: зелёный.
- [x] 3.4 Прогнать web-пакет целиком. Если тесты `handlers_projects_test.go` или `prefs_test.go` проверяли сырой текст флеша, поправить проверку на поведение (после декодирования), а не на строку адреса. Проверка: `go test ./internal/web/` зелёный.

## 4. Проверка

- [x] 4.1 Полный прогон Go-пакетов (`scripts/go-packages.sh | xargs scripts/test.sh`) с кодом 0.
- [x] 4.2 Холодный старт стенда (`scripts/qa-env.sh down && up`) и новый сценарий `e2e/test_invoice_no_billable_reason.py`: период без часов → причина видна на странице счетов как ошибка. Проверка: зелёный.
- [x] 4.3 Связанные e2e на том же стенде: `test_accounting_integrity.py`, `test_week_lock_and_load.py`, `test_admin_audit_trail.py`, `test_member_roles_on_screen.py`. Проверка: все зелёные.
- [x] 4.4 `make verify` с `GOTOOLCHAIN=go1.27.3` и `scripts/check-architecture.sh`. Проверка: зелёные.

## 5. Документы и backlog

- [x] 5.1 `docs/backlog/finance.md`: FIN-24 отмечен `[x]` с датой; в тексте указано, что причина общая (флеш с «;» терялся). `CHANGELOG.md`, раздел `[Unreleased]`: строка про сообщения формы, которые раньше терялись. Проверка: строки на месте.

## Workflow follow-up

- Коммит и пуш в master только по слову пользователя; после пуша смотрим CI до зелёного.
- Архивация изменения после проверок 4.1-4.4.
