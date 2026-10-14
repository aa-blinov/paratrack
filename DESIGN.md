---
name: paratrack
description: Time tracker as a strict, honest ledger on a warm sheet of paper, built as a calm React UI on shadcn/ui.
colors:
  # Light theme. These oklch values are the normative source, copied from
  # web/src/ui.css. Activity colours are hex there and stay hex here.
  background: "oklch(0.975 0.012 85)"
  foreground: "oklch(0.17 0.008 75)"
  card: "oklch(0.99 0.008 85)"
  primary: "oklch(0.205 0 0)"
  primary-foreground: "oklch(0.985 0 0)"
  muted: "oklch(0.945 0.013 85)"
  muted-foreground: "oklch(0.49 0.01 75)"
  border: "oklch(0.922 0 0)"
  destructive: "oklch(0.577 0.245 27.325)"
  brand: "oklch(0.52 0.185 284)"
  positive: "oklch(0.55 0.128 163)"
  warning: "oklch(0.769 0.188 70.08)"
  pending: "oklch(0.72 0.155 70)"
  activity-1: "#6366f1"
  activity-2: "#0ea5e9"
  activity-3: "#14b8a6"
  activity-4: "#10b981"
  activity-5: "#84cc16"
  activity-6: "#d97706"
  activity-7: "#f97316"
  activity-8: "#8b5cf6"
  activity-9: "#a855f7"
  activity-10: "#ec4899"
  # Dark theme, on [data-theme=paratrack-dark].
  dark-background: "oklch(0.145 0 0)"
  dark-foreground: "oklch(0.985 0 0)"
  dark-card: "oklch(0.205 0 0)"
  dark-muted: "oklch(0.269 0 0)"
  dark-destructive: "oklch(0.704 0.191 22.216)"
typography:
  body:
    fontFamily: "Inter, ui-sans-serif, system-ui, sans-serif"
    fontSize: "1rem"
    fontWeight: 400
    lineHeight: 1.55
  figures:
    fontFamily: "JetBrains Mono, ui-monospace, monospace"
    fontVariantNumeric: "tabular-nums"
  wordmark:
    fontFamily: "Inter, ui-sans-serif, system-ui, sans-serif"
    fontWeight: 750
    letterSpacing: "-0.045em"
rounded:
  base: "0.625rem"
spacing:
  unit: "4px"
components:
  button-primary:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.primary-foreground}"
    typography: "{typography.body}"
    rounded: "{rounded.base}"
    height: "36px"
    padding: "0 10px"
  button-outline:
    backgroundColor: "{colors.card}"
    textColor: "{colors.foreground}"
    rounded: "{rounded.base}"
    height: "36px"
    padding: "0 10px"
  button-ghost:
    backgroundColor: "transparent"
    textColor: "{colors.foreground}"
    rounded: "8px"
    height: "28px"
    padding: "0 10px"
  input:
    backgroundColor: "transparent"
    textColor: "{colors.foreground}"
    typography: "{typography.body}"
    rounded: "{rounded.base}"
    height: "36px"
    padding: "4px 10px"
  card:
    backgroundColor: "{colors.card}"
    textColor: "{colors.foreground}"
    rounded: "14px"
    padding: "16px"
---

# Design System: paratrack

## Overview

**Creative North Star: "The Ledger Sheet"**

paratrack is a ledger of where time goes. The page is a warm sheet of paper
with ink on it. Figures stand in monospaced columns, hairline rules separate
rows, and the only colour on a screen belongs to the person's own activities.
Nothing decorates: the numbers have to be trusted enough to bill and pay from,
so they are set plainly.

The UI is a React application built on the official shadcn/ui components
(`radix-nova` style, `web/components.json`). Components in `web/src/components/ui`
are copied from shadcn and stay as supplied; the application binds them to its
data and translations. There is no other UI layer: the server-rendered pages
and the daisyUI/Alpine/htmx stack were removed.

Confirmed rejections: gradient text, glass effects, decorative blur, a shadow
on a resting card, and a colour used as the ink of a label.

**Key Characteristics:**
- Warm paper ground in light (hue 85, low chroma), neutral in dark.
- Figures in JetBrains Mono with tabular numerals, always in columns.
- One colour per activity, shown as a 3px spine beside the name.
- Flat at rest; shadows only on layers that float above the page.
- One easing curve and three durations for all motion.

### Source of truth

- Tokens: `web/src/ui.css`. Light values are on `:root`, dark on
  `[data-theme=paratrack-dark]`. The theme follows the system unless the person
  picks one; `app-bootstrap.js` applies it before the first paint.
- Components: `web/src/components/ui` (shadcn, unchanged) and the app's own
  screens under `web/src/<area>/`.
- Layout shell: `web/src/shell/app-shell.tsx` and `web/src/components/app-sidebar.tsx`
  (desktop), `web/src/components/mobile-nav.tsx` (phone).
- Feedback: `web/src/offline/` (offline banner, toasts, phone mini bar),
  `web/src/components/confirmation-dialog.tsx` (destructive confirmations).
- Email letters and the public site have their own rules below; they are not
  React screens.

## Colors

The neutral tokens carry the interface. Light values are oklch and exact in `ui.css`.

| Role | Light | Dark |
|---|---|---|
| Page (`--background`) | `oklch(0.975 0.012 85)` warm paper | `oklch(0.145 0 0)` |
| Text (`--foreground`) | `oklch(0.17 0.008 75)` warm ink | `oklch(0.985 0 0)` |
| Card (`--card`) | `oklch(0.99 0.008 85)` | `oklch(0.205 0 0)` |
| Primary action (`--primary`) | `oklch(0.205 0 0)` charcoal | `oklch(0.922 0 0)` |
| Quiet fill (`--muted`) | `oklch(0.945 0.013 85)` | `oklch(0.269 0 0)` |
| Secondary ink (`--muted-foreground`) | `oklch(0.49 0.01 75)` | `oklch(0.708 0 0)` |
| Hairline (`--border`) | `oklch(0.922 0 0)` | `oklch(1 0 0 / 10%)` |
| Error (`--destructive`) | `oklch(0.577 0.245 27.325)` | `oklch(0.704 0.191 22.216)` |

### Primary

- **The primary action is charcoal by default.** A form's one primary button is
  charcoal (`--primary`). Where a screen marks its main action with
  `data-action-primary`, that action takes brand violet instead: the «Старт» button
  on the dashboard is the case in point.
- **Activity colours belong to the person's activities.** `--activity-1` to
  `--activity-10` are the default palette (indigo first). An activity colour is a
  3px spine beside neutral text: it is never the ink of a label. A colour that
  becomes a background gets a computed contrast-safe ink.

### Secondary

- **Brand violet** (`--brand`, `oklch(0.52 0.185 284)`) is the marked primary action
  (`[data-action-primary]`, hover one step darker, disabled mixed toward the muted
  fill) and the selected period tab (`--brand-soft` ground). The focus ring is a
  separate token, `--ring`, at the same hue. Violet is never a surface colour.

### Status

- **One green for everything that went right** (`--positive`): a running
  timer and a settled invoice. Its text partner is `--positive-ink`.
- **Amber marks money that is waiting**, not lost (`--warning`, `--pending`).
  Red stays with destructive actions only.
- **Status is text plus tint.** Document statuses (draft, sent, paid) are
  badges with their name written out; colour repeats the word, never replaces it.

### Named Rules

**The Ledger Paper Rule.** The page is a warm sheet, never pure white. The
warmth is a hue of 85 at low chroma, so the activity colours stay the only
chroma on a screen.

**The Charts Literal Rule.** A canvas cannot read `var()`, so the graph resolves
each activity token to a literal colour when it paints
(`web/src/graph/timeline-chart.ts`) and repaints on a theme change.

## Typography

- **Inter** for everything read: headings, labels, body. Body text is 16px with a
  line height of 1.55 (`#paratrack-react-root` in `ui.css`).
- **JetBrains Mono** (self-hosted, `/static/fonts`) for every figure a person
  compares down a column: durations, money, times, counts. Always `tabular-nums`.
- **Fraunces is not used in the app.** The app wordmark is Inter 750 with tight
  tracking. The public site is the only place Fraunces appears (see Public site).
- Input text is 16px on a phone. iOS zooms a page when a field under 16px takes
  focus and does not always zoom back.
- Headings: one `h1` per page, naming the page. Cards open with a title, not a
  heading level jump.
- Measure: body text holds 65–75ch by container width.

### Hierarchy

- **Page title** (`h1`, Inter 600, 1.5rem on the boot screen): names the page.
- **Card title** (Inter 500–600, `text-sm`): opens each card.
- **Body** (Inter 400, 1rem on a phone, 14px for dense desktop rows).
- **Figure** (JetBrains Mono, tabular, `text-sm` to `text-base`): every time and sum.

### Named Rules

**The Figures Rule.** Every figure that a person compares down a column is
JetBrains Mono with `tabular-nums`.

## Layout

- Content sits in one column (`max-w-6xl`). Grids appear only where items are
  peers.
- **Desktop (1024px and up):** a shadcn `sidebar-01` sidebar. Groups: Меню,
  Анализ, Деньги, Команда, Данные, Настройки. A group of one item has no
  heading. «Данные» folds on a screen shorter than 1000px unless the person is
  inside it; on taller screens the last choice is remembered in the browser.
- **Phone (below 1024px):** a Material 3 navigation bar, 80dp tall, with a
  64x32dp active indicator and 12px labels. A fixed bottom bar with four destinations
  (Обзор, Табель, Статистика, По часам) and «Ещё», which opens a sheet with the
  rest and the account. The bar's settings group appears once, in the sheet.
  On the timesheet, statistics and the graph a floating timer button opens the
  timer with the caret in the field.
- **Running timers on the dashboard:** a list of live lines, one per activity,
  each with its own pause and stop. On a phone the line's actions sit under its
  meta line, flush left; on a wide screen they sit at the right of the line.
  Several activities can run at once; «Только эта» pauses the others.
- **Running timer on a phone:** a bar above the tab bar on every page but the
  dashboard, with the running timer and its pause and stop controls. Its
  controls are 48dp.
- Scrollable strips (settings tabs) fade at their right edge on a phone to show
  there is more to scroll.
- Body keeps clear of the notch and the home indicator in an installed app.

## Elevation & Depth

Depth is flat at rest. A card is a sheet separated from the paper by a 1px ring,
not a shadow. Shadows appear only on layers that float above the page:

- **Floating date calendar and toasts** (`shadow-md`, Tailwind v4 default):
  `0 4px 6px -1px rgb(0 0 0 / 0.1), 0 2px 4px -2px rgb(0 0 0 / 0.1)`.
- **Confirmation dialog** (`shadow-lg`): `0 10px 15px -3px rgb(0 0 0 / 0.1),
  0 4px 6px -4px rgb(0 0 0 / 0.1)`, above a 50% black scrim.
- **Phone timer button** (`.mobile-nav-fab`): `0 6px 16px rgb(0 0 0 / .18)`.

Modal layers dim the page: the confirmation dialog at 50% black, the phone's
bottom sheet at 10% black with a light backdrop blur where the browser supports it.

### Named Rules

**The Resting Flat Rule.** Cards and rows are flat at rest. A shadow appears only
as a response to a layer that floats above the page.

## Shapes

- **Radius base** is `0.625rem` (10px), the `--radius` token. Scales derive from it:
  `sm` 6px, `md` 8px, `lg` 10px, `xl` 14px.
- **Buttons** use `lg` (10px) at default size, `md` (8px) at `sm` and `icon` sizes.
- **Cards** use `xl` (14px) with a 1px ring.
- **Inputs** use `lg` (10px) with a 1px border at 3:1 contrast against the card.
- **Pills** (the phone timer button, the activity spine, status pills) are fully round.
- **Clipping:** the card paints with `overflow-hidden`, so any popover rendered
  inside it is portalled out (see Components).

## Components

**Material 3 on the phone.** The phone shell follows Material 3 at the baseline
sizes: the navigation bar (80dp), the top app bar (64dp), the emphasized
easing for every transition, pill buttons (40dp), pill segmented tabs, the
bottom sheet and the dialog at a 28dp corner, text fields 56dp tall at a 4dp corner
(compact table cells keep their size), assist and filter chips 32dp at an 8dp corner,
and the timer button a 56dp FAB at a 16dp corner.
On the phone the colour roles and the type scale are Material 3 too. Primary is
the brand violet (`--brand`, white ink on light, pale violet on dark), cards are
tonal surfaces (`--card` as a surface container, no ring), and the type roles are
the M3 baseline sizes in Inter: headline small 24/32, title medium 16/24 medium,
label large 14/20 medium, label medium 12/16 medium. The desktop keeps the neutral
ledger. The activity colours are not M3 roles; they stay as the person's marks.

For each component: the character line first, then shape, colour and states.

### Buttons
- **Shape:** `default` 36px tall, `sm` 28px, `icon` 32px square on the desktop.
  One variant per cluster; do not mix sizes in a row.
- **Phone (Material 3):** every button is a pill 40dp tall, `icon` buttons are
  40dp circles. The 48dp touch floor is the hit area, drawn 4px outside the
  visible shape in `::after`, so the shape stays at 40dp.
- **Primary:** charcoal `oklch(0.205 0 0)` with `oklch(0.985 0 0)` text. A screen's
  marked main action (`data-action-primary`) is violet `--brand` instead.
- **Outline:** card ground with a hairline border on the paper.
- **Ghost:** no ground at rest, the muted fill on hover. Row actions use ghost:
  «Только эта», «Пауза», «Стоп». Stop takes the destructive ink.
- **Touch targets:** 48dp on a phone for every control that is pressed
  (`test_mobile_navigation.py` measures the running-timer controls and the
  shell's touch floor). Text links inside a phone screen get their 48dp hit
  area from `::after` too, without moving the text.

### Inputs and fields
- **Style:** transparent ground, 1px `--input` border at 3:1 against the card
  and the muted row, 10px radius, 36px tall.
- **Text size:** 16px on a phone (iOS zoom), 14px from `md` up.
- **Focus:** the `--ring` colour with a 3px ring at 50% opacity.
- **Date and time fields** use `DateField` / `DateTimeField`
  (`web/src/components/date-field.tsx`). They read the document language, accept
  the screen's own order and ISO, and submit nothing for a value they cannot
  read. The calendar portals to the React root and opens upwards when there is
  no room below. Native `<input type="date">` is not used.

### Cards
- **Shape:** 14px radius, 1px ring, `--card` ground on the warm paper.
- **Title and body** align together. An empty state inside a card follows the
  card's title, not a centred block.
- **Tables** become labelled cards on a phone; cells that carry a time or a sum
  keep one line.

### Running timer row
- **Shape:** one row per live activity, separated by a hairline.
- **Spine:** a 3px by 20px pill in the activity colour, at the start of the name.
- **Clock:** JetBrains Mono, tabular, secondary ink.
- **Actions:** «Только эта», «Пауза» (or «Продолжить» when paused), «Стоп».

### Disclosures and destructive actions
- **Disclosures** (optional fields, saved snapshots) show a chevron so they read
  as buttons, not labels.
- **Forms with a destructive action** ask first through the confirmation dialog
  (`data-confirm` forms and `requestConfirmation`). Deleting is not undone by a
  second click.

### Navigation
- **Desktop:** shadcn sidebar, text labels, the active item on a muted fill.
- **Phone:** a fixed bottom bar with four labelled destinations and «Ещё»; the
  active destination carries a pill behind its icon.

### Feedback
- **Success:** a toast at the bottom of the screen, short, the same words the
  server names (`X-Toast`). It leaves on its own after about four seconds.
- **Undo:** stopping a timer offers the way back in the same strip, not a toast.
- **Errors:** the message under the control that failed, in the text the server
  names. JSON errors show their message, not their markup.
- **Offline:** a banner says what is saved and what waits. Actions taken with no
  connection are queued and sent in order when it returns, with their original
  click time; a refused action waits for a retry or a removal.
- **Empty states** explain the next step in one line and offer that step. A
  page-level empty state stays centred; a card's empty state follows the card's
  title alignment.
- **Loading:** the shell shows its skeleton before the page data arrives.

### Motion
- One easing token, `--m3-easing-emphasized` (`cubic-bezier(0.2, 0, 0, 1)`), is
  used by every transition and entrance.
- Three durations: `--motion-quick` 150ms for small state changes,
  `--motion-base` 250ms for panels, `--motion-thorough` 400ms for containers.
  Skeletons breathe on `--motion-loop` (1.6s).
- `prefers-reduced-motion` removes movement and duration but keeps the state
  change and the hierarchy.

## Do's and Don'ts

### Do
- **Do** keep depth flat: tone and 1px rings at rest; a shadow only on a floating layer.
- **Do** render every comparable figure in JetBrains Mono with `tabular-nums`.
- **Do** put one `h1` on each page and one primary action per form.
- **Do** let names truncate: `min-width: 0` on the container and `truncate` on the name.
- **Do** theme browser surfaces: selection, caret and scrollbar follow the palette.
- **Do** name what a control does and what will happen; cut any sentence the screen already says.

### Don't
- **Don't** use Fraunces, gradient text, glass effects or decorative blur in the app.
- **Don't** paint user-authored colours as text ink.
- **Don't** rely on hover for anything a touch user must reach.
- **Don't** wrap the header onto a second row: truncate the workspace name instead.
- **Don't** add a page without a reason to exist; a feature that is not reachable from a menu is not finished.
- **Don't** drop `prefers-reduced-motion`.

## Email

Letters (invoice to a client, password reset, team invite) share one
layout, `templates/email.html`, previewed at `/settings/email-preview`
(`?kind=invoice|reset|invite`, `&text=1` for the plain part).

- **Built for mail clients, not browsers:** tables and inline styles only,
  560 px sheet on `#f3f4f6`, white card with a 1 px `#e5e7eb` hairline,
  12 px radius. `color-scheme: light` so dark-mode clients don't invert it.
- **Type:** web fonts don't load reliably in mail, so body text uses the
  system stack `-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto,
  Inter, Arial, sans-serif` (Inter where installed); figures use the
  system mono stack; the wordmark is Georgia, the closest safe serif to
  the app's display face.
- **One dark button** (`#1a1d23`, white 15 px/600) with the raw link under
  it; the amount on an invoice sits under the 2 px total rule, like the
  ledger. Every letter also ships an identical plain-text part.

## Public site

The GitHub Pages site (`site/`: landing and docs, one stylesheet
`site/assets/site.css`) is the same ledger on a public page, not a
second world. Every rule above holds; these are the few things only
the site needs.

- **Dark ground:** in dark the page is `#15171c`, one step below the
  app's darkest base, so `#1a1d23` sheets still read as sheets on it
  with a `#2d3139` hairline. Both layouts set it as the dark `theme-color`.
- **Marketing headings:** Inter 700 with tighter tracking than the app's
  headline. Landing `h1` is `clamp(2.1rem, 1.3rem + 3vw, 3.4rem)`, lh 1.06,
  −0.035em, one per page. Section `h2` is `clamp(1.6rem, 1.1rem + 1.8vw,
  2.3rem)`, −0.025em. Docs `h1` is `clamp(1.9rem, 1.4rem + 1.6vw, 2.5rem)`.
  Fraunces is the site's wordmark face only (`site/assets/site.css`).
- **Live-fed figures:** a figure fed by the demo timers is set in bold
  ink, never in indigo. Indigo stays for focus, selection, and link hover.
- **Empty cells:** an empty table cell, or a "not included" cell, gets
  the stroked minus icon in `--ink-3` with `aria-label="нет"`. Never a
  bare dash or a blank cell.
