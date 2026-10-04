---
title: "Свой сервер"
nav_order: 15
description: "Установка paratrack через docker compose, настройки, почта, SSO, бэкапы, обновление и CLI."
---

paratrack можно поставить у себя: одно приложение и Postgres в docker compose. На сервере нужны только Docker и docker compose, всё остальное собирается внутри образа. Во время сборки прогоняются тесты, и красная сборка в образ не попадёт.

## Установка

```bash
git clone https://github.com/aa-blinov/paratrack
cd paratrack
cp .env.example .env
```

В `.env` задайте два обязательных значения:

```bash
POSTGRES_PASSWORD=...      # openssl rand -hex 24
PARATRACK_SECRET_KEY=...   # openssl rand -hex 32
```

`PARATRACK_SECRET_KEY` шифрует в базе ключи интеграций, Stripe и секреты вебхуков. Сохраните копию: без него эти ключи придётся вводить заново.

Запуск:

```bash
docker compose up -d --build
```

paratrack ответит на `http://127.0.0.1:8000`. Откройте `/register` и создайте первый аккаунт. Схема базы создаётся и обновляется сама при каждом старте.

## Домен и HTTPS

Compose слушает только `127.0.0.1`. Наружу приложение выводится через обратный прокси с HTTPS, например Caddy:

```
time.example.com {
    reverse_proxy 127.0.0.1:8000
}
```

Укажите `PARATRACK_PUBLIC_URL=https://time.example.com` в `.env`. Для доверия к forwarded-заголовкам добавьте адрес или CIDR прокси в `PARATRACK_TRUSTED_PROXIES` (например, `172.20.0.0/16` для внутренней сети Docker). Только от этих адресов приложение принимает `X-Forwarded-Proto`, `X-Forwarded-For` и `X-Forwarded-Host`; без настройки запросы учитываются по адресу непосредственного клиента.

## Настройки

Все значения задаются в `.env`, пустое значение выключает возможность.

| Переменная | Зачем |
|---|---|
| `POSTGRES_PASSWORD` | пароль базы, обязательно |
| `PARATRACK_SECRET_KEY` | шифрование ключей в базе, обязательно |
| `PARATRACK_PORT` | порт на `127.0.0.1`, по умолчанию 8000 |
| `PARATRACK_TZ` | часовой пояс до того, как браузер сообщит свой, по умолчанию `Europe/Moscow` |
| `PARATRACK_PUBLIC_URL` | канонический origin приложения для ссылок в письмах, SSO и Stripe; вне `development`/`test` обязателен и должен начинаться с `https://` |
| `PARATRACK_TRUSTED_PROXIES` | CIDR доверенных reverse proxy; только от них принимаются `X-Forwarded-Host`, `X-Forwarded-Proto` и `X-Forwarded-For` |
| `PARATRACK_SMTP_HOST`, `PARATRACK_SMTP_USER`, `PARATRACK_SMTP_PASS`, `PARATRACK_MAIL_FROM` | почта: счета клиентам, приглашения, сброс пароля. Хост с портом, например `smtp.yandex.ru:465`. Без почты письма пишутся в лог сервера |
| `PARATRACK_OIDC_ISSUER`, `PARATRACK_OIDC_CLIENT_ID`, `PARATRACK_OIDC_CLIENT_SECRET` | вход через SSO (OIDC); issuer обязан использовать HTTPS вне `development`/`test`. Адрес возврата у провайдера: `https://ваш-домен/sso/callback` |
| `PARATRACK_OIDC_TRUST_EMAIL` | `1`, если провайдер проверяет почту, но не присылает `email_verified` |
| `PARATRACK_STRIPE_KEY`, `PARATRACK_STRIPE_WEBHOOK_SECRET` | ключи Stripe на весь сервер, если в пространстве свои не заданы |
| `PARATRACK_WEBHOOK_ALLOW_PRIVATE` | `1`: разрешить вебхуки на адреса локальной сети |
| `PARATRACK_JIRA_SITE`, `PARATRACK_GITLAB_SITE` | адрес Jira или своего GitLab по умолчанию |
| `PARATRACK_SENTRY_DSN`, `PARATRACK_SENTRY_TRACES` | отправка ошибок в Sentry или совместимый сервис |

После правки `.env` перезапустите приложение: `docker compose up -d`.

## Бэкапы

Данные живут в томе `pgdata`. Дамп вручную:

```bash
docker compose exec -T db pg_dump -U paratrack -d paratrack | gzip > paratrack-$(date +%F).sql.gz
```

Восстановление в работающий стек (перезапишет данные):

```bash
gunzip -c paratrack-2026-09-28.sql.gz | docker compose exec -T db psql -U paratrack -d paratrack
```

Ночные дампы в S3-совместимое хранилище делает `scripts/backup-s3.sh`: заполните `S3_ENDPOINT`, `S3_REGION`, `S3_BUCKET`, `S3_KEY_ID`, `S3_SECRET_KEY` в `.env` и поставьте скрипт в cron. Он хранит дампы 30 дней, а `scripts/backup-s3.sh --verify` разворачивает последний дамп во временный Postgres и сверяет число строк, не трогая рабочую базу.

## Обновление

```bash
git pull
docker compose up -d --build
```

Перед обновлением стоит снять дамп. Миграции схемы применяются сами при старте.

## CLI

Тот же бинарник работает из терминала с той же базой. Нужна переменная `PARATRACK_DATABASE_URL` со строкой подключения к Postgres.

```
paratrack                        что идёт сейчас
paratrack start <активность>     запустить (спросит заметку)
paratrack stop [активность]      остановить
paratrack pause [активность]     пауза
paratrack resume [активность]    продолжить
paratrack focus <активность>     «Только эта»
paratrack add                    сессия задним числом
paratrack log                    журнал за период
paratrack stats                  итоги
paratrack goal set --activity <имя> --daily 2h
paratrack tag add <имя>
paratrack project list | create | rename | archive | delete
paratrack web --addr :8000       веб-интерфейс
```

Короткие варианты: `s` stop, `p` pause, `r` resume, `sw` focus, `st` status, `a` add, `l` log.

## Без Docker

Нужны Go и любой Postgres 17:

```bash
export PARATRACK_DATABASE_URL='postgres://user:pass@127.0.0.1:5432/paratrack?sslmode=disable'
make build
./paratrack web --addr 127.0.0.1:8000
```
