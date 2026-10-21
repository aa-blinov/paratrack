# Design

## Context

Путь ошибки при создании ссылки на оплату идёт через четыре слоя:
1. `internal/stripe/checkout.go`: `CreateCheckout` делает HTTP-запрос с `*http.Client` и ключом. При статусе не 200 возвращает `fmt.Errorf("stripe returned status %d: …")`. Тип ошибки теряется.
2. `internal/app/adapters.go`: `stripeProtocol.CreateCheckout` вызывает `stripeadapter.CreateCheckout` и передаёт ошибку дальше.
3. `internal/invoicing/stripe.go`: `CreateStripePaymentLink` оборачивает ошибку через `%w` (`create Stripe checkout: %w`), `service.go` переэкспортирует ошибки `appmodel`.
4. `internal/web/handlers_billing.go`, `handleInvoicePayLink`: ветки `ErrForbidden`, `ErrStripeUnavailable`, `ErrInvalidInvoice`, иначе `logInternalError` и `err.internal`.

Проверено: `ErrStripeUnavailable` уже различается в обработчике, но сообщение о ней захардкожено по-английски. Тест `internal/stripe/checkout_test.go` уже подменяет транспорт через `roundTripFunc`.

## Decisions

### D1. Тип ошибки статуса в `internal/stripe`

`type StatusError struct { Code int; Body string }` с `Error()` в том же виде, что сейчас (текст для журнала не меняется). `CreateCheckout` и `ExpireCheckout` возвращают `*StatusError` при статусе не 200. Другие ошибки (сеть, разбор ответа) остаются как были.

### D2. Сопоставление в адаптере

`stripeProtocol.CreateCheckout` проверяет `errors.As(err, &statusErr)`. Для 401 и 403 возвращает `fmt.Errorf("%w: %w", appmodel.ErrStripeKeyRejected, err)`, чтобы в журнале остался исходный текст, а `errors.Is` видел причину. Прочие коды возвращаются без изменений.

Почему в адаптере, а не в `stripe`: `appmodel` задаёт контракт между слоями, а пакет `stripe` не должен знать о сообщениях приложения. Так же, как `ErrStripeUnavailable`.

### D3. Функция причины для обработчика

Вынести `stripeLinkReason(lang, err) (string, bool)` рядом с `handleInvoicePayLink`:
- `ErrForbidden` → как сейчас (`invoiceErrorMessage`);
- `ErrStripeKeyRejected` → `stripe.keyRejected`;
- `ErrStripeUnavailable` → `stripe.notConfigured` (вместо английской строки);
- `ErrInvalidInvoice` → как сейчас;
- иначе → `err.internal` с записью в журнал, как сейчас.

Функция чистая, поэтому тестируется таблицей без браузера и без подмены Stripe в веб-окружении.

### D4. Ключи словаря

`stripe.keyRejected` (ru: «Stripe не принял ключ. Проверьте ключ в разделе «Онлайн-оплата» настроек пространства.», en: «Stripe rejected the key. Check the key in the Online payments section of the workspace settings.») и `stripe.notConfigured` (ru: «Ключ Stripe не задан. Задайте его в разделе «Онлайн-оплата» настроек пространства.», en: «The Stripe key is not set. Set it in the Online payments section of the workspace settings.»). Перед добавлением проверить, что ключей с такими именами ещё нет.

### D5. `ExpireCheckout`

Меняется только тип ошибки (тот же `*StatusError`), путь пользователя не трогаем. Причина ключа там не выводится.

## Risks

- 403 у Stripe может означать ограниченный ключ без нужных прав, а не неверный ключ. Текст «не принял ключ» и указание проверить ключ остаются полезными: проверка прав там же, в настройках ключа.
- Ошибка из сети может выглядеть как отказ ключа только в тексте журнала, но не в коде: сетевые ошибки не являются `StatusError`, поэтому остаются общими.
- Английский вариант причины ещё не проверен глазами: отмечаем в tasks.

## Verification

- `internal/stripe/checkout_test.go`: 401 и 403 дают `*StatusError` с нужным кодом (`roundTripFunc`), 500 даёт `*StatusError` с кодом 500, сетевая ошибка не является `*StatusError`.
- `internal/app` или `internal/invoicing`: адаптер на 401 даёт `errors.Is(err, appmodel.ErrStripeKeyRejected)`, на 500 нет.
- `internal/web`: таблица для `stripeLinkReason`: ключ отвергнут → причина; не задан → русская причина; 5xx → общее сообщение; `ErrForbidden` → как раньше.
- `go test ./internal/i18n`: паритет ключей.
- E2E на холодном стенде: `e2e/test_stripe_key_reason.py`: аккаунт без ключа, ссылка на оплату в режиме Stripe → русская причина «Ключ Stripe не задан…» на странице счёта как ошибка. Путь «ключ отвергнут» в e2e не проверяется: в стенде нет выхода в интернет до Stripe.
- `make verify` с `GOTOOLCHAIN=go1.27.3`, `scripts/check-architecture.sh`. Новые ключи выводятся сервером через флеш, React их не читает. Словарь целиком входит в бандл (`chunks/ru-*.js`, `en-*.js`), поэтому `make ui` всё равно пересобираем после правки словарей, а коммит бандла идёт вместе с кодом.

## Open questions

- Нет. Вне scope: вебхуки Stripe и `ExpireCheckout` в путях пользователя.
