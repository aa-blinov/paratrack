---
name: paratrack
description: Time tracker as a strict, honest ledger, built as a calm React UI on shadcn/ui.
colors:
  # Light theme. Exact values are oklch in web/src/ui.css; these hex values are
  # the nearest sRGB equivalents, for tools that read hex.
  background: "#ffffff"
  foreground: "#0a0a0a"
  card: "#ffffff"
  primary: "#171717"
  primary-foreground: "#fafafa"
  muted: "#f5f5f5"
  muted-foreground: "#737373"
  border: "#e5e5e5"
  destructive: "#dc2626"
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
  dark-background: "#0a0a0a"
  dark-foreground: "#fafafa"
  dark-muted: "#262626"
  dark-destructive: "#f87171"
typography:
  body:
    fontFamily: "Inter, ui-sans-serif, system-ui, sans-serif"
    fontSize: "1rem"
    fontWeight: 400
    lineHeight: 1.5
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
---

# paratrack design system

## Overview

paratrack is a ledger of where time goes. The interface reports figures and
states what a control does; it does not decorate. Surfaces are neutral, the
accent colours belong to the user's own activities, and every comparable
number lines up in a monospaced column.

The UI is a React application built on the official shadcn/ui components
(`radix-nova` style, `web/components.json`). Components in `web/src/components/ui`
are copied from shadcn and stay as supplied; the application binds them to its
data and translations. There is no other UI layer: the server-rendered pages
and the daisyUI/Alpine/htmx stack were removed.

## Source of truth

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

Neutral tokens carry the interface (oklch, exact values in `ui.css`):

| Role | Light | Dark |
|---|---|---|
| Page (`--background`) | `oklch(1 0 0)` | `oklch(0.145 0 0)` |
| Text (`--foreground`) | `oklch(0.145 0 0)` | `oklch(0.985 0 0)` |
| Primary action (`--primary`) | `oklch(0.205 0 0)` charcoal | `oklch(0.922 0 0)` |
| Quiet fill (`--muted`) | `oklch(0.97 0 0)` | `oklch(0.269 0 0)` |
| Hairline (`--border`) | `oklch(0.922 0 0)` | `oklch(1 0 0 / 10%)` |
| Error (`--destructive`) | `oklch(0.577 0.245 27.325)` | `oklch(0.704 0.191 22.216)` |

- **Charcoal is the action.** The one primary button on a form is charcoal,
  not a colour. Colour does not carry calls to action.
- **Activity colours belong to the person's activities.** `--activity-1` to
  `--activity-10` are the default palette (indigo first). A project or activity
  colour is a swatch beside neutral text: it is never the ink of a label. A
  colour that becomes a background gets a computed contrast-safe ink.
- **Charts paint literal colours.** A canvas cannot read `var()`, so the graph
  resolves each activity token to a literal colour when it paints
  (`web/src/graph/timeline-chart.ts`), and repaints on a theme change.
- **Status is text plus tint.** Document statuses (draft, sent, paid) are
  badges with their name written out; colour repeats the word, never replaces it.

## Typography

- **Inter** for everything read: headings, labels, body. Body text is 16px.
- **JetBrains Mono** for every figure a person compares down a column:
  durations, money, times, counts. Always `tabular-nums`.
- **Fraunces is not used.** The wordmark is Inter 750 with tight tracking.
- Input text is 16px on a phone. iOS zooms a page when a field under 16px takes
  focus and does not always zoom back.
- Headings: one `h1` per page, naming the page. Cards open with a title, not a
  heading level jump.
- Measure: body text holds 65–75ch by container width.

## Layout

- Content sits in one column (`max-w-6xl`). Grids appear only where items are
  peers.
- **Desktop (1024px and up):** a shadcn `sidebar-01` sidebar. Groups: Меню,
  Анализ, Деньги, Команда, Данные, Настройки. A group of one item has no
  heading. «Данные» folds on a screen shorter than 1000px unless the person is
  inside it; on taller screens the last choice is remembered in the browser.
- **Phone (below 1024px):** a fixed bottom bar with four destinations
  (Обзор, Табель, Статистика, По часам) and «Ещё», which opens a sheet with the
  rest and the account. The bar's settings group appears once, in the sheet.
  On the timesheet, statistics and the graph a floating timer button opens the
  timer with the caret in the field.
- **Running timer on a phone:** a bar above the tab bar on every page but the
  dashboard, with the running timer and its pause and stop controls. Its
  controls are 48dp.
- Scrollable strips (settings tabs) fade at their right edge on a phone to show
  there is more to scroll.
- Body keeps clear of the notch and the home indicator in an installed app.

## Components

- **Buttons** are shadcn variants: `default` (h-9), `sm` (h-7), `icon`
  (size-9). One variant per cluster; do not mix sizes in a row.
- **Touch targets:** 48dp on a phone for every control that is pressed
  (`test_mobile_navigation.py` measures the running-timer controls and the
  shell's touch floor).
- **Cards** are shadcn cards with a ring, not a border. A card's title and its
  body align together; an empty state inside a card follows the card's title.
- **Tables** become labelled cards on a phone; cells that carry a time or a sum
  keep one line.
- **Date and time fields** use `DateField` / `DateTimeField`
  (`web/src/components/date-field.tsx`). They read the document language, accept
  the screen's own order and ISO, and submit nothing for a value they cannot
  read. The calendar portals to the React root and opens upwards when there is
  no room below. Native `<input type="date">` is not used.
- **Disclosures** (optional fields, saved snapshots) show a chevron so they read
  as buttons, not labels.
- **Forms with a destructive action** ask first through the confirmation dialog
  (`data-confirm` forms and `requestConfirmation`). Deleting is not undone by a
  second click.

## Feedback and states

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

## Motion

- One easing token, `--m3-easing-emphasized`, is used by transitions.
- Durations stay per component.
- `prefers-reduced-motion` removes movement and duration but keeps the state
  change and the hierarchy.

## Do's and Don'ts

### Do
- Keep depth flat: tone and 1px hairlines. Only the phone's bottom sheet dims the page.
- Render every comparable figure in JetBrains Mono with `tabular-nums`.
- Put one `h1` on each page and one primary action per form.
- Let names truncate: `min-width: 0` on the container and `truncate` on the name.
- Theme browser surfaces: selection, caret and scrollbar follow the palette.
- Name what a control does and what will happen; cut any sentence the screen already says.

### Don't
- Don't use Fraunces, gradient text, glass effects or decorative blur.
- Don't paint user-authored colours as text ink.
- Don't rely on hover for anything a touch user must reach.
- Don't wrap the header onto a second row: truncate the workspace name instead.
- Don't add a page without a reason to exist; a feature that is not reachable from a menu is not finished.
- Don't drop `prefers-reduced-motion`.

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
  Fraunces stays on the wordmark only.
- **Live-fed figures:** a figure fed by the demo timers is set in bold
  ink, never in indigo. Indigo stays for focus, selection, and link hover.
- **Empty cells:** an empty table cell, or a "not included" cell, gets
  the stroked minus icon in `--ink-3` with `aria-label="нет"`. Never a
  bare dash or a blank cell.
