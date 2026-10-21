# Tasks

## 1. Воспроизведение

- [x] 1.1 Проверить на стенде, что ссылка на оплату без ключа отвечает английской строкой, а не русской причиной. Проверка: в описании задачи записан текст флеша до правки (ожидаем «stripe key is not configured…» на русском интерфейсе).
- [x] 1.2 Проверить в коде, что отказ ключа (401) сейчас уходит в `err.internal`. Проверка: тест в `checkout_test.go` на `roundTripFunc` со статусом 401 показывает обычную строку ошибки до правки (записан в описании задачи).

## 2. Тип ошибки статуса

- [x] 2.1 В `internal/stripe/checkout.go` ввести `StatusError{Code int; Body string}` с `Error()` в прежнем тексте. `CreateCheckout` и `ExpireCheckout` возвращают `*StatusError` при статусе не 200. Проверка: `go vet ./internal/stripe/` и `checkout_test.go` проходят.
- [x] 2.2 Тесты в `checkout_test.go`: 401 и 403 дают `*StatusError` с нужным кодом, 500 даёт `*StatusError` с кодом 500, сетевая ошибка не является `*StatusError`. Проверка: зелёный.

## 3. Сопоставление в адаптере и приложении

- [x] 3.1 В `internal/appmodel/errors.go` добавить `ErrStripeKeyRejected`; в `internal/invoicing/service.go` переэкспортировать её рядом с `ErrStripeUnavailable`. Проверка: `go build ./...`.
- [x] 3.2 В `internal/app/adapters.go` `stripeProtocol.CreateCheckout`: для 401 и 403 вернуть `fmt.Errorf("%w: %w", appmodel.ErrStripeKeyRejected, err)`, остальные коды без изменений. Проверка: тест адаптера или `invoicing` на `errors.Is(err, ErrStripeKeyRejected)` для 401 и его отсутствие для 500.
- [x] 3.3 `internal/invoicing/stripe.go`: проверить, что `CreateStripePaymentLink` сохраняет цепочку `%w`. Проверка: тест на `errors.Is` через `invoicing`.

## 4. Причина на странице

- [x] 4.1 Ключи `stripe.keyRejected` и `stripe.notConfigured` в `ru.json` и `en.json` (тексты из design D4). Проверка: `go test ./internal/i18n` зелёный.
- [x] 4.2 В `internal/web/handlers_billing.go` вынести `stripeLinkReason(lang, err) (string, bool)` по правилам design D3; в `handleInvoicePayLink` заменить английскую строку и добавить ветку `ErrStripeKeyRejected`. Проверка: таблица-тест на все ветки (ключ отвергнут, не задан, 5xx, `ErrForbidden`, `ErrInvalidInvoice`).
- [x] 4.3 Web-тест счетов: POST на ссылку на оплату без ключа → страница счетов показывает русскую причину как ошибку. Проверка: зелёный.

## 5. Проверка

- [x] 5.1 `make ui` (бандл после правки словаря), коммит бандла вместе с кодом. Проверка: `make ui` без ошибок.
- [x] 5.2 Полный прогон Go-пакетов (`scripts/go-packages.sh | xargs scripts/test.sh`) с кодом 0. Проверка: зелёный.
- [x] 5.3 Холодный старт стенда (`scripts/qa-env.sh down && up`) и новый сценарий `e2e/test_stripe_key_reason.py`: ключ не задан → русская причина на странице счетов как ошибка. Проверка: зелёный.
- [x] 5.4 `make verify` с `GOTOOLCHAIN=go1.27.3` и `scripts/check-architecture.sh`. Проверка: зелёные.

## 6. Документы и backlog

- [x] 6.1 `docs/backlog/finance.md`: FIN-14 отмечен `[x]` с датой; текст про английскую строку и причину 401/403 уточнён. `CHANGELOG.md`, раздел `[Unreleased]`: строка про причину отказа ключа Stripe. Проверка: строки на месте.

## Workflow follow-up

- Коммит и пуш в master только по слову пользователя; после пуша смотрим CI до зелёного.
- Архивация изменения после проверок 5.2-5.4.
