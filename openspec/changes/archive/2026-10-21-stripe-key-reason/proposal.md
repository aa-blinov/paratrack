# Proposal

## Why

Кнопка «Создать ссылку на оплату» при неверном ключе Stripe отвечает общей фразой «Что-то пошло не так у нас. Попробуйте ещё раз.». Бухгалтер не знает, что ключ отвергнут и где его поменять (FIN-14, P1; общий принцип в UI-04).

Проверено в коде. `internal/stripe/checkout.go` при статусе ответа не 200 возвращает обычную строку `stripe returned status %d: …` без типа, поэтому обработчик `handleInvoicePayLink` (`internal/web/handlers_billing.go`) не узнаёт отказ ключа и уходит в ветку `else` с логом. Адаптер `stripeProtocol` в `internal/app/adapters.go` передаёт ошибку дальше без признака. Отдельно: сообщение «stripe key is not configured (team settings or process configuration)» захардкожено по-английски и выводится в русском интерфейсе.

## What Changes

- В `internal/stripe` ошибка статуса становится типизированной `StatusError` с кодом ответа. Текст остаётся для журнала.
- Коды 401 и 403 сопоставляются с новой ошибкой `appmodel.ErrStripeKeyRejected` в адаптере и слое `invoicing`. Прочие статусы и сетевые ошибки остаются как были: общее сообщение и запись в журнал.
- Обработчик показывает причину: «Stripe не принял ключ. Проверьте ключ в разделе «Онлайн-оплата» настроек пространства.» (ru) и английский вариант. Сообщение «ключ не задан» переводится через словарь (`stripe.notConfigured`) вместо английской строки.
- Словарь: `stripe.keyRejected`, `stripe.notConfigured` в `ru.json` и `en.json`.
- `ExpireCheckout` получает тот же тип ошибки, но в пути пользователя не меняется (вне scope).

## Capabilities

### New Capabilities
- `stripe-checkout-errors`: отказ Stripe при создании ссылки на оплату называет причину на языке интерфейса и место, где её исправить; прочие сбои остаются общими.

### Modified Capabilities
Нет. В `openspec/specs/` спецификаций оплаты нет.

## Impact

- Go: `internal/stripe/checkout.go` (тип ошибки, `CreateCheckout` и `ExpireCheckout`), `internal/app/adapters.go` (`stripeProtocol.CreateCheckout`), `internal/appmodel/errors.go` (`ErrStripeKeyRejected`), `internal/invoicing/stripe.go` и `service.go` (проброс ошибки), `internal/web/handlers_billing.go` (ветка причины и перевод «ключ не задан»).
- i18n: три ключа в ru и en.
- Тесты: `checkout_test.go` уже использует `roundTripFunc` как подменный транспорт; добавляются кейсы 401 и 403 (ключ отвергнут), 500 (общая ошибка). Web-тест: 401 → причина ключа на странице счетов; 503 → общая фраза; ключ не задан → русская причина. E2E на холодном стенде: путь «ключ не задан» (ключ не настроен) показывает русскую причину.
- Совместимость: внешний вид ошибок Stripe для пользователя меняется только для 401 и 403. Сеть и 5xx не меняются.
- Вне scope: вебхуки Stripe, `ExpireCheckout` в путях пользователя, FIN-08, FIN-09, другие FIN- и UI-02 пункты.
