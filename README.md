# paratrack

Учёт времени, из которого сразу выходит счёт. Параллельные таймеры, табель, статистика, счета клиентам и выплаты команде в одном приложении. Работает в браузере и на телефоне, ставится на свой сервер одним `docker compose`.

**[Сайт](https://aa-blinov.github.io/paratrack/)** · **[Документация](https://aa-blinov.github.io/paratrack/docs/)** · **[Открыть сервис](https://paratrack.duckdns.org/register)**

![Обзор: три таймера идут одновременно, ниже блок «Не выставлено»](site/assets/shots/dashboard.webp)

## Что умеет

- **Таймеры.** Несколько сразу, пауза, «Только эта» (остальные на паузу), стоп с отменой. Записи задним числом свободным текстом: «вчера 14:00», «2 ч назад». Идущий таймер виден на любой странице и на телефоне.
- **Разбор времени.** Статистика по проектам, активностям, людям и тегам, правка сессий прямо в таблице. Табель недели, распределение по часам суток, цели на день, неделю и месяц, сохранённые отчёты.
- **Деньги.** Счёт клиенту из учтённого времени: PDF, акт выполненных работ, письмо с вложением, онлайн-оплата через Stripe, чек «Мой налог». Время каждой строки округляется до 0,01 ч, поэтому «часы × ставка = сумма» сходится до копейки. Правила пространства: округление до 6/15/30/60 минут, префикс номера, логотип. Выплаты участникам по их ставкам, расписание загрузки команды, пять готовых отчётов с HTML и CSV.
- **Команда.** Пространства, роли владельца, администратора и участника, приглашения по ссылке, передача владения. Участник видит только своё время, ставки клиентов только у менеджеров.
- **Под себя.** При регистрации выбираете набор разделов: «Для себя», «Фрилансер», «Студия». Лично настраиваются формат длительности, начало недели, часовой пояс, блоки главной, разделы меню и нижняя панель на телефоне.
- **Экосистема.** Задачи из GitHub, GitLab, Jira, Trello, Asana, ClickUp, Todoist и Notion с запуском таймера в один клик. Перенос истории из Toggl, Harvest и Clockify. REST API с токенами и пагинацией, подписанные вебхуки, журнал действий, вход через SSO (OIDC), расширение для Chrome, CLI.
- **Телефон и офлайн.** Ставится на главный экран как приложение. Без сети действия копятся и отправляются потом с тем временем, когда вы нажали кнопку. Push-уведомления о своих событиях.

Интерфейс на русском, есть английский. Светлая и тёмная темы.

| | |
|---|---|
| ![Табель недели](site/assets/shots/timesheet.webp) | ![Счёт с реквизитами и итогом](site/assets/shots/invoice.webp) |
| **Табель:** неделя как таблица, минуты в ячейке | **Счёт:** реквизиты, строки работ, PDF и акт |
| ![Статистика за неделю](site/assets/shots/stats.webp) | ![Тёмная тема](site/assets/shots/dashboard-dark.webp) |
| **Статистика:** разбивка, фильтры, правка на месте | **Тёмная тема** |

Скриншоты сняты на демонстрационных данных.

## Быстрый старт

На машине нужен только Docker с docker compose. Образ собирает CSS, прогоняет тесты на своём временном Postgres и собирает бинарник, рядом поднимается Postgres для данных.

```bash
git clone https://github.com/aa-blinov/paratrack
cd paratrack
cp .env.example .env   # задайте POSTGRES_PASSWORD и PARATRACK_SECRET_KEY
docker compose up -d --build
# → http://127.0.0.1:8000/register
```

Данные живут только в Postgres, в томе `pgdata`. Схема создаётся и обновляется сама при старте. Почта, SSO, Stripe, домен с HTTPS, бэкапы в S3 и обновление описаны в документации: [Свой сервер](https://aa-blinov.github.io/paratrack/docs/self-host/).

Без Docker нужны Go и любой Postgres 17:

```bash
export PARATRACK_DATABASE_URL='postgres://postgres:dev@127.0.0.1:5432/postgres?sslmode=disable'
export PARATRACK_ENV=development PARATRACK_PUBLIC_URL=http://127.0.0.1:8000
make build    # соберёт CSS и React bundle (npm ci + Tailwind + Vite)
./paratrack web --addr 127.0.0.1:8000
```

CLI работает с той же базой и использует workspace с наименьшим ID: `paratrack start вёрстка`, `paratrack stop`, `paratrack stats`. Полный список команд — `paratrack help`.

## Для разработчиков

### Устройство

Один Go-бинарник без CGO. Переход на React и shadcn/ui идёт поэтапно: dashboard, цели, график, выплаты, расписание, теги, табель и страницы проектов используют React; остальные страницы пока рендерятся через `html/template`, HTMX и Alpine.js. Оба интерфейса и их стили встраиваются в бинарник через `go:embed`; внешних CDN нет.

```
paratrack/
├── cmd/paratrack/     process root: конфигурация, сборка графа и lifecycle
├── internal/
│   ├── cli/, web/      CLI и HTTP transport adapters
│   ├── application/    consumer contracts для transports
│   ├── app/            сборка workflow и outbound adapters
│   ├── auth/, teams/, tracking/, invoicing/, ...
│   │                   feature workflows и их persistence ports
│   ├── db/             Postgres (pgx): schema, migrations, queries
│   ├── model/          transport-neutral domain types
│   └── mail/, stripe/, netclients/, ... outbound adapters и shared leaves
├── web/               исходники стилей (make ui)
├── site/              сайт и пользовательская документация (GitHub Pages)
├── extension/         расширение для Chrome
├── e2e/               Playwright-проверки
└── docs/              PRODUCT, QA, JOURNEY: внутренние заметки
```

Подробная карта зависимостей, владения ресурсами и правил слоёв — в
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md). Структура SQL задана в
`internal/db/schema.sql`; startup migrations и совместимость со старыми
базами принадлежат `internal/db`.

Каждая страница и каждый `/api/*` проходят через `requireAuth`: кука сессии
или токен `Authorization: Bearer pt_…`. Все выборки ограничены пространством,
у участника ещё и своими сессиями.

### Проверка

```bash
scripts/test.sh                  # все Go-тесты на временном Postgres в docker
scripts/test.sh -run TestName ./internal/web
make vet

python3 -m venv .venv && . .venv/bin/activate
pip install playwright requests && python -m playwright install chromium
python e2e/qa_full.py            # интерфейс и визуал со скриншотами
python e2e/qa_logic.py           # математика денег и времени поверх HTTP
```

CI на GitHub Actions прогоняет юнит-тесты на Postgres, сборку стилей и Playwright. Сайт из папки `site/` публикуется на GitHub Pages отдельным workflow при изменениях в ней.

### Документы

- [docs/PRODUCT.md](docs/PRODUCT.md): возможности, бизнес-логика денег и времени, модель безопасности, HTTP-поверхность, схема данных
- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md): пакеты, dependency rules, resource ownership и workflow boundaries
- [docs/QA.md](docs/QA.md): матрица проверок и найденные баги
- [docs/JOURNEY.md](docs/JOURNEY.md): карта переходов и пустые состояния
- [DESIGN.md](DESIGN.md): дизайн-система
- [CONTRIBUTING.md](CONTRIBUTING.md), [CHANGELOG.md](CHANGELOG.md)

## Стек

Go 1.27 · PostgreSQL 17 (pgx) · net/http · React 19 + shadcn/ui (dashboard and project pages; migration in progress) · html/template · HTMX 2 · Alpine.js 3 · ECharts 5 · Tailwind v4 + DaisyUI v5

## Лицензия

MIT
