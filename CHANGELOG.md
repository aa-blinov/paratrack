# Changelog

All notable changes to paratrack. Format: [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Changed
- **Остановка таймера оставляет путь назад.** Сервер и раньше отвечал на остановку маршрутом отмены, но React-оболочка его теряла: тост показывала только старая система. Теперь под обзором появляется строка «Остановлено: …» с кнопками «Отменить» (снова идёт) и «Поправить» (прямо в запись). «Стоп всем» тоже отдаёт отмену для каждого таймера — это самое легко промахнуть действие.
- **На телефоне неделя открывается на «сегодня».** Семь дней — это прокрутка ради вопроса «сколько сегодня». Переключатель «Сегодня / Вся неделя» стоит над списком дней.
- **Деньги на счетах объясняют своё округление.** Правило пространства округляет время построчно, но интерфейс об этом молчал: человек сравнивал «1 ч 28 мин» с суммой и не понимал расхождения. Теперь под шапкой счетов одна строка о том, как именно считается.

### Fixed
- **Плавающая кнопка закрывала «Стоп» в полосе идущего таймера.** Пока что-то идёт, плавающая кнопка уходит: главное действие сейчас — не начать ещё один таймер, а закончить этот.

### Changed (ранее)
- **Неделя в табеле больше не прокручивается вбок.** Замер на восьми ширинах показал, что сетка не помещается на 640px (608px места), 768px (736px) и 1024px (720px, рельс забирает 256) и помещается на 900px и от 1100. Теперь правило одно: ниже 1100px неделя раскладывается по дням, выше — сетка. Плата: на 900–1099px список дней там, где сетка поместилась бы, — выбрано «никогда не прокручивается» вместо «совпадает с порогом шелла».
- **Режим на обзоре говорит, что открывает.** «Режим: Для себя» называл режим и ничего не говорил о цене; теперь строка звучит как «Режим: Для себя — открыто 5 из 9 разделов», и счётчик берётся с сервера из того же каталога, который правит страница разделов.
- **Статистика отвечает в порядке вопросов.** Экран отвечал сразу на три вопроса — «сколько», «куда» и «где ошибка», — и человек с вопросом «куда ушло время» не мог отличить ответ от редактора. Теперь итог стоит первым в карточке разбивки и всегда в первом экране (замерено: `y=272` при окне 900, `y=404` при 844), доля названа один раз под заголовком, а строки объяснены как ссылки. Блок «Сессии» стал «Проверкой и правкой» и на телефоне начинается свёрнутым под кнопкой «Показать».
- **Фильтр по тегу вернулся на статистику.** Путь «тег → время» был односторонним: зайти с `/tags` и отфильтровать можно было, вернуться к списку по тегу на самом экране — нет. Теперь рядом с чипсами проектов есть строка «Теги:», и она показывается, только когда в пространстве есть теги.

### Fixed
- **Доля строки объяснялась один раз, а не в каждой.** Числа вроде «50,0%» нигде не пояснялись.

- **Телефонный каркас по заветам Material.** Ниже 1024px вместо сайдбара-листа с кнопкой 28px — фиксированная нижняя панель 64px с четырьмя направлениями и «Ещё», а «Ещё» открывает нижний лист с остальными разделами, переключателем пространств, пунктами аккаунта и выходом. Верхняя панель 64px, плавающая кнопка 56px на экранах чтения времени. Компоненты прежние: shadcn, без новых зависимостей.
- **Эргономика под палец.** Цели касания не ниже 48dp, ширина кнопки с подписью — не меньше 48px, поля ввода не меньше 16px (иначе iOS Safari зумит страницу при фокусе), мелкие чипы фильтров берут зону касания псевдоэлементом, нажатие даёт состояние.

### Fixed
- **Бейдж тега в строке сессии стал ссылкой.** Экран обещает, что по тегу фильтруется статистика, а тег в строке сессии был обычным текстом. Теперь `#тег` открывает `/stats` с тем же периодом и этим тегом, а кнопка снятия рядом осталась кнопкой.
- **Табель на телефоне — список дней вместо прокрутки вбок.** Неделя это 48rem: на 320px было видно два дня из семи, и каждая строка требовала прокрутки. Ниже 640px неделя раскладывается по карточкам дня с теми же полями и тем же сохранением; с 640px прежняя сетка.
- **Свёрнутый рельс прятал нижние иконки.** `overflow: hidden` останавливает прокрутку колесом и пальцем, поэтому на экране 720px часть из 18 пунктов была недостижима. Теперь рельс прокручивается, и подпись «список продолжается» появляется честно.
- **«Пропустить» в онбординге вело в командный режим.** Человек, которому нужно было своё время, попадал в студийный набор: 18 пунктов и подводка про команду. Теперь «Пропустить — пока для себя» выбирает набор «Для себя», как и сказано в подписи; поменять можно в «Ещё» → «Разделы».
- **Две навигации одновременно на 768–1023px.** Сайдбар становился постоянной колонкой на 768px, а телефонный шелл начинался на 1024px: на планшете в портрете висели и рельс, и нижняя панель, причём панель лежала поверх оболочки. Теперь у оболочки один переход — 1024px: ниже сайдбар открывается листом поверх, как и на телефоне.
- **Бейдж проекта резал текст на обзоре.** Длинное название переносилось на вторую строку внутри фиксированной высоты 20px и обрезалось. Теперь бейдж растёт под текст.
- **Диалог подтверждения терял кнопку на телефоне.** Окно росло вместе с текстом без ограничения: на экране 844px длинное сообщение растягивало его до 1262px, и «Подтвердить» оказывалась на y≈1000 — за пределами экрана, а верх диалога уходил над ним. Теперь высота ограничена экраном, заголовок и кнопки закреплены, прокручивается только текст.
- **Карточка участника обрезалась на 320px.** Форма оплаты просила две колонки по 5rem плюс кнопку — 272px внутри 224px, поэтому «Сохранить» и подпись под ней уезжали за край карточки. На узком экране поля встают в две колонки, кнопка занимает всю ширину.
- **На телефоне были недостижимы профиль, API-токены и справка.** Они живут в меню шапки, а `.app-shell-actions` скрывается ниже 1024px: человек не мог ни открыть профиль, ни выйти, ни сменить тему. Теперь всё это в листе «Ечё», и добавлен переключатель пространств, который раньше жил в шапке сайдбара.
- **Карта переходов по ролям.** Обход всех экранов стенда (32 страницы владельцем, 30 участницей) нашёл две ссылки, которые ведут в отказ: у участницы `/projects` показывал «Новый проект» (а `/projects/new` закрыт правами), а справка предлагала «Вебхуки» из карточки «Где это настроить» (а `/settings/webhooks` закрыт правами). Обе кнопки теперь за `CanManage`; вместо кнопки на пустом списке проектов объясняется, что проекты создаёт руководитель. Проверка — `e2e/test_navigation_map_by_role.py`.
- **Сохранённые отчёты получили свой экран.** Список жил внутри свёрнутого блока на `/stats`, а пункт меню «Отчёты» открывал `/reports` с одними шаблонами — объект без своего дома. Теперь список на `/reports` и ведёт в статистику с закреплённым периодом и фильтрами; на `/stats` остались действие «Сохранить текущий срез» и видимая строка «Сохранённые отчёты (N)» со ссылкой на список.

### Changed
- **Тег ведёт в своё время.** `#тег` на `/tags` — ссылка на `/stats?period=week&tag=…`, а не текст; подсказка под списком об этом говорит. Раньше единственный путь к фильтру по тегу был через таблицу последних сессий на обзоре или вручную правкой адреса.
- **Проект в табеле — ссылка.** Название проекта под активностью в недельной сетке ведёт в проект, если сервер прислал слаг; для строки без проекта подпись прежняя.

### Fixed
- **Сохранённые отчёты статистики** помнят фильтр по проекту и тегу через идентификаторы, а не текстом. Переименование тега больше не обнуляет пресет, а удалённый проект или тег читается как «без фильтра», а не как старое имя. Схема расширена на `saved_reports.project_id` и `tag_id`, перенос старых строк идемпотентен при старте.
- **`e2e/qa_full.py`** снова проходит: сквозной сюит писался под htmx-разметку до перехода интерфейса на React. Разобран бутстрап страниц вместо прежней разметки, необязательные поля открываются через их раскрытия, разрушительные действия подтверждаются общим диалогом, а две пустые проверки тостов заменены настоящей проверкой переключения темы. Сюит добавлен в CI — пока он не гонялся, он и разошелся с интерфейсом.
- **Sidebar sections**: the `Separator` primitive styled itself with `data-horizontal:`, which Tailwind v4 compiles to a literal `[data-horizontal]` attribute while Radix emits `data-orientation`. Every separator in the app had zero height and was invisible. They now render.
- **Workspace chip**: the workspace name is stored with a localized prefix (`Пространство: …`, `…'s workspace`) and was clipped to an ellipsis in the 256px rail. The name now wraps to two lines and the box grows to fit.
- **Sidebar footer**: language and theme sat in a 2×2 grid where the second column broke the icon gutter every nav item shares. The block is now avatar + name + email, then `Язык · RU` and `Тема · авто`, then separators, with every row on the same icon column.
- **Audit log**: a form control named `action` shadowed `HTMLFormElement.action`, so the filter form submitted to `/settings/[object HTMLSelectElement]`. The control is `event` now.
- **Renaming a tag** no longer empties saved stats presets — they store the tag by name, and the rename now carries them along in the same transaction.
- **`npm audit`**: `source-map-js` 1.2.1 → 1.2.2 (dev-only, event-loop DoS advisory).

### Added — actions that the screens could not do
- **Timesheet**: undo the last cell edit (a stack of the 10 most recent, each entry naming the cell it will restore) and clear an activity row for the week with a confirmation. Clearing refuses when the week was already sent on an invoice, and only touches the caller's own sessions.
- **Tags**: rename a tag in place — the row keeps its id, so tagged sessions, counters and the statistics filter follow — plus `PATCH /api/tags` in the public API.
- **Audit log**: filter by date range, person and action; the filter lives in the address, the window is 100–400 with "show more events", and a truncated list says so instead of looking complete.
- **Invites**: copy the invitation link with feedback (and a readable fallback when the browser refuses clipboard access), and remove a spent or expired invitation from the list. Removing it never revokes the access it granted.
- **Profile**: change the login email at `/settings/profile`, requiring the current password like a password change does, with distinct errors for a wrong password, a taken address and a malformed one. The write is conditional on the address still being the old one, so concurrent changes cannot overwrite each other.
- **Notifications**: pick which topics you hear about, and send a verification push that reports the real outcome — delivered, no permission, no subscription, or a browser without web push. Existing accounts keep every topic: the new column stores only what you muted.
- **Webhooks**: "Send test" delivers a signed sample event through the same payload builder and HMAC as a live delivery (the body carries `"action":"test"` so a receiver can tell it apart), and the delivery log now shows the request and response bodies, clipped at 2000 bytes with a "showing the first…" note.
- **Graph**: a "Custom dates" period with from/to inputs, and a print button that hides the sidebar and header.
- **Sections**: a set changed by hand is now labelled as such, with a "bring back every section" button instead of a silent blank state.

### Removed
- **SQLite**. Postgres is the only database: the CLI and a bare binary need `PARATRACK_DATABASE_URL`, `paratrack migrate-to-postgres` is gone, and the tests run on a throwaway Postgres (`scripts/test.sh`, and inside the image build).

### Added — product waves 1 to 9
- **Timesheet** (`/timesheet`): week grid of activity × days, a cell holds that day's minutes, `0` clears it. Writes collapse the day into one synthetic `note="timesheet"` session so reads and writes agree.
- **Estimates vs actual**: `projects.estimate_minutes` plus a card on the project page comparing tracked time against it.
- **Saved reports**: named filter presets on `/stats`, shown as chips.
- **Eight integrations**: GitHub, GitLab, Jira, Trello, Asana, ClickUp, Todoist, Notion. Issues and cards import into `external_tasks` with a one-click timer start. Marketplace at `/integrations/marketplace` lists 11 cards, three of them reserved as coming soon.
- **Browser extension**: MV3 popup with a timer and task picker, `extension/`.
- **Import from Toggl / Harvest / Clockify** at `/import`.
- **Billable rates and invoices**: `projects.billable_rate_cents` plus `invoices`/`invoice_lines` numbered `INV-YYYY-NNN`, statuses `draft|sent|paid`. Non-billable projects and unassigned time never reach an invoice; rates are snapshotted onto the lines.
- **PDF invoices** rendered in pure Go (`github.com/go-pdf/fpdf`) at `/invoices/{id}/pdf`.
- **Online payment**: Stripe Checkout via a form-encoded call to the Stripe API (no SDK), plus a manual payment-link fallback and a Mark-paid action. `POST /api/stripe/webhook` is public and flips `checkout.session.completed` to paid.
- **Payroll** (`/payroll`): `memberships.hourly_pay_cents` and `capacity_minutes`, runs numbered `PAY-YYYY-NNN`, `draft|paid`. Sessions attribute through `sessions.user_id`.
- **Resource scheduling** (`/schedule`): people × week grid with load % against capacity.
- **Report templates** (`/reports`): five presets (by project, by activity, by day, billable, utilization) with HTML and CSV output.
- **API tokens** (`/settings/tokens`): `pt_`-prefixed bearer credentials, SHA-256 at rest, raw shown once. Bearer auth sits inside `requireAuth` as an alternative to the session cookie.
- **API v1**: `GET|POST /api/v1/sessions`, `PATCH|DELETE /api/v1/sessions/{id}`, `GET /api/v1/projects`, `GET /api/v1/reports/summary`. The session list returns running sessions too.
- **Webhooks** with HMAC-SHA256 signatures (`X-Paratrack-Signature`) and a delivery log; **audit log** at `/settings/audit`; **OIDC SSO** at `/sso/login` when `PARATRACK_OIDC_*` is set.
- **PWA** (manifest + service worker at `/sw.js`, root scope), **offline queue** that buffers mutations and replays them on reconnect, **web push** (VAPID, `/settings/notifications`).

### Added — interface
- **Russian is the default language**, English is one click away. Dictionaries carry 572 keys per locale and every user-visible string goes through them.
- **Loading skeletons** only where there is a real wait: the graph canvas and HTMX list swaps. A thin top progress bar tracks every HTMX request. Both are deferred 180 ms so a fast request never flashes.
- **Empty states with a next action** on every data screen: timesheet, schedule, invoices, payroll, reports, integrations, audit, push and the rest.
- **First-run onboarding**: a three-step checklist on the dashboard (start a timer, look at stats, add a project). It shows only while the account has no sessions and dismisses permanently.
- **`/help`** collects the whole feature map in one page, reachable from the user menu. **CSV** moved into the top nav. CLI references dropped from the product surface.
- **Design system** written down in `DESIGN.md` plus `.impeccable/design.json`: a flat "Honest Ledger" world with tokens, an 8-step tonal ramp per colour, and self-contained HTML/CSS for nine components. Depth comes from tone and hairlines, not shadows.
- **Button geometry unified** to a 40 / 32 / 24 px ladder with a single class order (`btn → variant → size → shape → gap → layout → state`).
- **Site copy** reviewed with humanizer-ru: em-dashes and CLI wording removed, HARD BANS clean.

### Changed — typography and icons
- **Type scale restored**: DaisyUI 5 resets `h1..h6` to `font-size: inherit`, which flattened the whole UI to one size. A minor-third scale (`--step--2` … `--step-4`) is now defined on `:root` and applied to headings, card titles, labels and meta.
- **UI font is Inter** (variable, latin + latin-ext + cyrillic) replacing Manrope. Body is pinned to 16px with `font-optical-sizing: auto`.
- **Logo wordmark is Fraunces** (variable "full" cut with SOFT/WONK axes) — used only for `.wordmark` "paratrack", with a Lucide `timer` glyph beside it. JetBrains Mono stays for code, kbd and timestamps.
- **Lucide icon set** (ISC) vendored as an SVG sprite at `/static/icons.svg` (35 symbols) and exposed as `{{icon "play"}}`. Replaces the hand-drawn ▶/⏸/☰/● glyphs in nav, status pills and session actions (Focus/Pause/Resume/Stop/Start/Add).

### Changed
- **Typography and icons**: minor-third type scale on `:root` (DaisyUI 5 resets headings to `inherit`), Inter as the UI face, Fraunces for the wordmark only, JetBrains Mono for code and figures. Lucide replaces hand-drawn glyphs, vendored as one SVG sprite.
- **Theme resolves before first paint** and the toggle icons are driven by CSS against `data-theme-mode`, so nothing flashes when the page loads. `:root` no longer transitions its background.
- **Activity names render as a colour swatch beside neutral text** instead of coloured text, which used to fail contrast on pale hues. `inkFor` computes contrast-safe ink for any user-authored colour used as a background.
- **`/settings/members`** collapses to cards below 640 px and its table scrolls instead of widening the page.

### Added — production hardening
- **CSRF double-submit** on every state-changing request. A readable `paratrack_csrf` cookie is echoed via hidden form fields and the `X-CSRF-Token` header (HTMX gets it from `htmx:configRequest`, `fetch` from `paratrackCSRF()`). Missing/incorrect token → 403.
- **Security headers** on every response: `Content-Security-Policy` (self + the inline/eval relaxations Alpine 3 requires), `X-Content-Type-Options`, `X-Frame-Options: DENY`, `Referrer-Policy`, `Permissions-Policy`, and HSTS when the request arrived over TLS.
- **Secure cookies** — `Secure` is set on session / team / CSRF cookies when the request is HTTPS (direct TLS or `X-Forwarded-Proto: https`).
- **Auth rate limiting** — in-memory sliding window per IP: login 10/min, register 5/min, password-forgot 5/min, password-reset 10/min. 429 with `Retry-After`.
- **Password recovery** — `/forgot-password` + `/reset-password`, single-use 30-minute tokens (`password_reset_tokens`), "if that address exists" responses (no enumeration), successful reset logs out every other session. Mail goes through a pluggable `internal/mail.Sender`: log sink by default, SMTP relay via `PARATRACK_SMTP_HOST` / `PARATRACK_SMTP_USER` / `PARATRACK_SMTP_PASS` / `PARATRACK_MAIL_FROM`.
- **Web backfill** — "Add a past session" card on the dashboard (`POST /api/sessions/backfill`), natural-language times via the same parser as `paratrack add`.
- **Focus control in the UI** — per-row Focus button on active sessions (and a "Focus first" shortcut) calling `POST /api/focus/{name}`.
- Toast sits under the topbar (no longer covers the nav).
- Tests: `saas_test.go` (CSRF reject, security headers, full reset flow with spy mailer, rate-limit 429, backfill) and `rl_test.go` (limiter window accounting).

### Fixed
- **No more flicker on load**: fonts use `font-display: block`, the theme is set by an inline bootstrap in `<head>`, and loading feedback waits before appearing.
- **Long words no longer break mid-word** in tables (`Demo` used to render as `Dem/o`); only genuine run-together tokens wrap.
- **Same-day invoices and pay runs are accepted** (a one-day period used to be rejected as `bad period`).
- **Unassigned time is never invoiced** (it used to land on the invoice at rate 0).
- **`/projects` no longer 500s** (`{{.T}}` called inside a `{{range}}` on a row type without a `T()` method).
- **Offline queue keeps the request body** and only dequeues on 2xx, so a replayed timer start actually creates the session.
- **Push notifications are legible**: branded icon and monochrome badge instead of a dark square, plus `tag`/`renotify` grouping and Open/Dismiss actions. The settings page no longer dies on a JS syntax error caused by `html/template` double-escaping.
- **Service worker registers at `/sw.js`** so its scope covers the app, not just `/static/`.

- **Stop folds live elapsed into `accumulated_seconds`**, so a closed session's `DurationSeconds` is no longer 0 for never-paused rows and correctly excludes pause gaps for paused ones.
- **Tracked time is pause-aware and window-scaled everywhere**: `Session.TrackedSecondsInWindow` attributes a session's non-paused total to a period by its wall-clock overlap fraction. Goals (both active and closed), `/stats` aggregates and project card windows all use it — previously goals counted live sessions as accumulated but closed ones as wall-clock (including pauses).
- **`DeleteTag` is team-scoped** — a signed-in user could previously delete any workspace's tag by numeric id. Missing goal/tag now map to 404 (was 500).
- **Names in query strings are `urlquery`-escaped** on goals delete, session tag detach, and `/stats?tag=` links — "deep work" used to produce a broken URL.
- **CSV `project` column carries the project name** (was slug) and `duration_seconds` is the tracked duration (was wall-clock span).
- **`POST /api/start` fails cleanly when the chosen project can't be assigned** (was silently starting the session uncategorized).
- **Inline tag attach actually updates the row**: the `+ tag` input was missing `hx-target` / `hx-swap`, so HTMX dumped the `session-row` response into the `<input>` and the chip never appeared (toast still said "tagged"). Now Enter and the new `+` button both replace the row.
- **Inline duration edit no longer kills row bindings**: `paratrackResize` injects HTML outside HTMX and must call `htmx.process()`, otherwise tag/pause/delete stops working after an inline edit.
- **Mobile header no longer overflows the viewport** (was ~723px at 390px width): primary nav collapses into a hamburger menu under `sm`, workspace name truncates, period tabs scroll horizontally instead of stretching the document. `ui_audit.py` asserts `scrollWidth == clientWidth` on dashboard/stats/goals.
- **Duration format unified** to `Xh Ym` / `Xm` / `Xh` everywhere (stats, goals, projects cards, graph total, live ticker). The template `fmtDuration` no longer uses the zero-padded `%dh %02dm` ladder; the Alpine live ticker no longer emits `HH:MM:SS`.
- **Dashboard and tag-filter totals** no longer collapse to zero: aggregates read `sessionView.DurationSecs` instead of parsing the human label with a `HH:MM:SS` parser (`parseHMSStrict` removed).
- **Graph "Total tracked"** was 60× off (minutes fed into a seconds formatter).
- **Tag filter now scopes the whole /stats page** — breakdown, distribution and totals all describe the same row set (previously the breakdown ignored `?tag=`).
- **HTMX response contract**: `POST/DELETE /api/tags` and `/api/goals` return the `tags-list` / `goals-list` fragments under `HX-Request`; session tag attach/detach return the re-rendered `session-row` (they used to return an empty body that wiped the row on `outerHTML` swap). Non-HTMX callers keep the JSON/200 shapes.
- **Inline session edit keeps tags + project badge** (`handleUpdateSession` now hydrates before re-rendering the row).
- **Goals and Tags pages** ship a real `<title>` and nav highlight (`render()` no longer drops Title/Active for those view-models).
- **Dashboard "Recent sessions (last 7 days)"** actually queries 7 days; empty state no longer links to a nonexistent "log a past session" action.
- **/stats period tabs preserve `?project=`**; `/graph` gained the missing "Last month" tab; the filter banner reflects both project and tag.
- **Project detail totals**: "Last 30 days" uses clipped duration (was `accumulated_seconds`), "All time" is computed and rendered (was always `0m`). Project card windows compare timestamps via `db.FormatTime` (RFC3339Nano) instead of a mismatched local format.
- **Invite revoke form** works (`POST /api/team/invites/{token}/revoke` added next to `DELETE /api/team/invites/{token}`).
- **Register preserves `?next=`** so the invite-accept → sign-up → join flow lands on the invite.
- **Invite-accept "log out"** is a POST form, not a GET link to a POST-only route.
- **Delete workspace** has a Danger-zone control; deleting one of several workspaces switches to the next instead of logging the user out. `next` on workspace switch is validated as a relative path (open-redirect close).
- **Password change requires the current password** (was skippable by leaving the field empty).
- **Session mutations are team-scoped**: `GetSession` / `UpdateSessionEnd` / `PauseSession` / `ResumeSession` / `DeleteSession` / `AttachTag` / `DetachTag` take a `teamID` and refuse cross-workspace access. Previously any signed-in user could mutate any session by numeric id.
- **Primary CTAs on /projects** toned to `btn-neutral` to match the rest of the app.
- Slug input `pattern` made a valid HTML5 regular expression (the `-` placement broke the `/v` unicode-classes parser in Chromium).




## [0.1.0] — earlier work

Первые релизы до волн 1-9: аутентификация, командные пространства,
DaisyUI-интерфейс, цели, теги, график, CSV.

### Added
- Regression tests for the duration ladder, `DurationSecs`, graph total units, tag filter, `pageMeta` coverage, HTMX/JSON dual shape, and cross-team session isolation.
- **API contract suite** (`internal/web/api_contract_test.go`) covering session lifecycle + pause-aware CSV durations, goals HTMX-fragment vs JSON shapes with spaced names, tag team-scope deletes, CSV project-name column, duplicate-start 409, and inline duration edit round-trip.

### Added
- **DaisyUI v5 + Tailwind v4 CSS pipeline** in `web/`. `make ui` (or `make build`) installs npm deps and produces `internal/web/static/css/paratrack.css` (~16 KB minified, embedded via `go:embed`). Two custom themes: `paratrack-light` (default) and `paratrack-dark`.
- **Per-activity goals**: `paratrack goal set/list/unset` + `/api/goals`. Dashboard widget with live progress; period windows (daily / ISO-week / monthly) computed in UTC.
- **Free-form tags**: `paratrack tag add/list/attach/detach`. Inline "+ tag" input on stats rows; `/stats?tag=…` filter. Auto-create on first attach.
- **/goals and /tags management pages** with chip UIs and CLI hints.
- **ECharts graph** (vendored) — stacked hour-of-day bars, clickable legend, MutationObserver rebuilds on theme change.
- **GitHub Actions** (`.github/workflows/ci.yml`) — three jobs: `ui` (npm), `unit` (Go), `e2e` (Playwright). Uploads screenshots on failure.
- **48 Go unit tests** + **45 Playwright E2E checks** covering dashboard, stats, graph, theme, keyboard, ECharts, CSV, goals, tags.
- **3 colour tests** (`TestColorForReturnsNeutralGrey`, `TestColorForDeterministic`, `TestColorForCaseInsensitive`) pinning the monochrome `colorFor` contract.
- **Multi-user collaboration** — local email + password auth (bcrypt, 30-day sessions, HttpOnly cookies), per-user personal team created at registration, owner/member roles, 7-day invite tokens, `/settings/team|members|invites|profile`, top-bar workspace switcher dropdown, last-owner-cannot-leave guard. `internal/teams` is the new home for team CRUD; `internal/auth` owns users + sessions.
- **Hard-break schema migration**: pre-auth `track.db` is renamed to `track.db.bak.<UTC>` on first launch; fresh schema with `users / auth_sessions / teams / memberships / invites` is created. No manual import step.
- **Multi-tenant data layer**: every domain row (activities, sessions, tags, goals) carries `team_id`; all db helpers take `teamID int64` as their first arg, supplied by the auth middleware via `teamID(r)`. CLI / tests can still pass `0` to opt out of scoping.
- **86 Go unit tests** + **47 Playwright E2E checks** covering dashboard, stats, graph, theme, keyboard, ECharts, CSV, goals, tags, auth gate, workspace switcher.

### Changed
- **Whole UI on DaisyUI v5.** Every page uses `card`, `btn`, `input`, `table`, `badge`, `alert`, `progress`, `stat`, `kbd` — replacing the hand-rolled `app.css` (deleted). Light/dark parity is now driven entirely by `data-theme`.
- **Case-insensitive activity and tag names.** `COLLATE NOCASE` on the `name` columns; inserts/lookups lowercase on the Go side. `Work`, `work`, `WORK` resolve to one row.
- Stats tables collapse to a card-list on phones via `.responsive-collapse`.
- Topbar + nav wrap on narrow screens; theme button hides its AUTO/DARK/LIGHT label on phones.
- Status badges get play/pause glyphs.
- **Monochrome UI**: per-activity rainbow palette dropped — `colorFor` now returns a neutral `#6b7280` for every name, the `.activity-mark` decorative left-edge bar is gone, and chart series render as a single grey. Activity identity is carried by the name alone.
- **Per-activity colour coding restored** — `colorFor` hashes the lower-cased name onto a curated 10-colour palette (indigo, sky, teal, emerald, lime, amber, orange, violet, purple, pink), so each new activity lands on a stable, distinguishable slot. The colour is applied to the activity name itself (`style="color: {{.Color}}"`) on dashboard / stats / goals lists and on the graph legend chips; chart series reuse the same colour, so chips and stacked bars share one cue. Pure red is reserved for destructive actions.
- **Primary CTAs toned to neutral** — Start / Set goal / Add are now `btn-neutral` (solid black) instead of indigo `btn-primary`. The Stats Distribution bar fill is `bg-base-content` so it matches the goals progress bars. Destructive actions (Stop / Delete) keep `btn-error`, and status pills keep their success / warning tint — colour now only signals action severity, never decoration.
- **`s.render` propagates `r`**: every auth-gated page now calls the auth-aware renderer (`renderPageForRequest`), so the top-bar `{{if .User}}` branch actually picks up the user menu + workspace switcher on dashboard, stats, graph, tags, and goals — not only on the settings routes.
- Light/dark via CSS variables; focus-visible ring; `prefers-reduced-motion` short-circuit.
- Toast is a solid colored alert with a glyph (✓/✕).

### Fixed
- Inline-edit duration recomputes `end_at` server-side.
- ECharts hover events no longer eaten by Alpine's reactive proxy.
- Tooltip on empty graph hour shows zeros, not the previous hour's values.
- `goals.team_id` is now `NOT NULL DEFAULT 0` so the `UNIQUE(team_id, activity_id, period)` upsert actually detects duplicate goals (NULLs treated are treated as distinct otherwise).
- All db helpers (`CreateActivity`, `ListActivities`, `GetOrCreateActivity`, `CreateSession`, `CreateClosedSession`, `ListActiveSessions`, `ListClosedSessionsInRange`, `CreateTag`, `GetTagByName`, `ListTags`, `AttachTag`, `DetachTag`, `SetTagsForSession`, `ListSessionsByTag`, `ListAllTagsWithCounts`, `UpsertGoal`, `ListGoals`, `DeleteGoal`, `ProgressForGoals`, `TagsForSessions`) now take a `teamID int64` first arg. Pass 0 to skip the team scope (legacy / tests); the auth middleware always supplies the real id via `teamID(r)`. `goals.team_id` UNIQUE was widened to (team_id, activity_id, period) so per-team goals don't collide.
- `tags` table gains `UNIQUE (team_id, name)` so `CreateTag`'s `ON CONFLICT(team_id, name) DO NOTHING` actually resolves and the existing tag is returned (was failing with 400 before).
- `clipSeconds` rounds sub-second overlaps up with `math.Ceil`, so a session that literally just started (or one whose end was clamped to "now") no longer disappears from `/stats` due to `int(0.1s) = 0` truncation.

### Removed
- Legacy Python implementation deleted. Go binary is the only runtime.

## Migration notes

**Hard break on first launch.** A pre-auth `track.db` is renamed to `track.db.bak.<UTC-timestamp>` and a fresh schema is created. No manual import step. This is intentional — the auth model assumes a clean user/team table.

**Multi-tenant queries.** Every web request hits the middleware, which sets `User` and `Team` on the request context; db calls pass `teamID(r)` so cross-team reads are impossible by construction. The CLI is intentionally team-scope-free (passes `0` everywhere) and is therefore read-only-equivalent to a single-user legacy session — fine for personal scripts, not safe for shared hosts.

Case-insensitive rollout: any pre-existing activity or tag rows colliding under `COLLATE NOCASE` (e.g. `Work` + `work`) are merged — the lowest-id row wins, sessions / session_tags are re-pointed at the winner, losers are deleted. Idempotent.

## Past highlights

* **Phase 3 (polish)** — duration-edit, theme toggle, SVG timeline.
* **Phase 2 (web)** — HTMX + Alpine.js embedded UI, 11 API endpoints.
* **Phase 1 (CLI)** — natural-language time parser, `add`/`log`/`stats`.
* **Phase 0 (bootstrap)** — `go.mod`, modernc.org/sqlite, schema, `status` command.
