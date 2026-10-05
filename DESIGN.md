---
name: paratrack
description: Minimalist time tracker — a strict, honest ledger of where time goes.
colors:
  ink-indigo: "#6366f1"
  ink-violet: "#8b5cf6"
  ledger-white: "#ffffff"
  ledger-paper: "#f9fafb"
  ledger-rule: "#e5e7eb"
  ledger-ink: "#1a1d23"
  ledger-charcoal: "#1f2937"
  info-cyan: "#06b6d4"
  verdict-green: "#10b981"
  caution-amber: "#f59e0b"
  verdict-red: "#dc2626"
typography:
  display:
    fontFamily: "Fraunces, Iowan Old Style, Palatino Linotype, Palatino, Georgia, serif"
    fontSize: "1.35rem"
    fontWeight: 620
    lineHeight: 1
    letterSpacing: "-0.03em"
    fontVariation: "\"SOFT\" 40, \"WONK\" 1, \"opsz\" 48"
  headline:
    fontFamily: "Inter, ui-sans-serif, system-ui, sans-serif"
    fontSize: "1.728rem"
    fontWeight: 650
    lineHeight: 1.15
    letterSpacing: "-0.02em"
  title:
    fontFamily: "Inter, ui-sans-serif, system-ui, sans-serif"
    fontSize: "1.2rem"
    fontWeight: 600
    lineHeight: 1.3
    letterSpacing: "-0.01em"
  body:
    fontFamily: "Inter, ui-sans-serif, system-ui, sans-serif"
    fontSize: "1rem"
    fontWeight: 400
    lineHeight: 1.55
    letterSpacing: "normal"
  label:
    fontFamily: "Inter, ui-sans-serif, system-ui, sans-serif"
    fontSize: "0.694rem"
    fontWeight: 600
    lineHeight: 1.2
    letterSpacing: "0.08em"
  mono:
    fontFamily: "JetBrains Mono, ui-monospace, SFMono-Regular, Menlo, monospace"
    fontSize: "0.92em"
    fontWeight: 400
    lineHeight: 1.55
    fontFeature: "tabular-nums"
rounded:
  selector: "6px"
  field: "10px"
  box: "10px"
spacing:
  unit: "4px"
  tight: "8px"
  control: "12px"
  block: "16px"
  section: "24px"
components:
  button-primary:
    backgroundColor: "{colors.ledger-charcoal}"
    textColor: "{colors.ledger-paper}"
    rounded: "{rounded.field}"
    padding: "16px"
    height: "40px"
  button-row:
    backgroundColor: "transparent"
    textColor: "{colors.ledger-ink}"
    rounded: "{rounded.field}"
    padding: "12px"
    height: "32px"
  button-destructive:
    backgroundColor: "{colors.verdict-red}"
    textColor: "{colors.ledger-white}"
    rounded: "{rounded.field}"
    padding: "12px"
    height: "32px"
  button-chip:
    backgroundColor: "transparent"
    textColor: "{colors.ledger-ink}"
    rounded: "{rounded.field}"
    padding: "8px"
    height: "24px"
  input:
    backgroundColor: "{colors.ledger-white}"
    textColor: "{colors.ledger-ink}"
    rounded: "{rounded.field}"
    padding: "12px"
    height: "40px"
  card:
    backgroundColor: "{colors.ledger-white}"
    textColor: "{colors.ledger-ink}"
    rounded: "{rounded.box}"
    padding: "24px"
  badge:
    backgroundColor: "{colors.ledger-paper}"
    textColor: "{colors.ledger-ink}"
    rounded: "{rounded.selector}"
    padding: "4px 8px"
    height: "24px"
---

# Current React UI: shadcn/ui

The authenticated React application and React authentication pages use the
official shadcn/ui `radix-nova` component style. The current source of truth is
`web/components.json`, `web/src/ui.css`, and the components copied by the
shadcn CLI into `web/src/components/ui/`. The shared navigation follows the
official `sidebar-01` block; sign-in follows `login-03`.

Keep these components and blocks as supplied by shadcn. Bind them to paratrack
data and translations, and remove example-only controls or placeholder data.
Do not apply the old custom ledger styling to React components. The historical
guidance below applies only to legacy Go/HTMX surfaces, emails, and documents
that have not moved to the React UI.

# Previous Design System: paratrack ledger

## Overview

**Creative North Star: "The Honest Ledger"**

paratrack is a book of accounts for time. Every surface behaves like a well-kept
ledger: the numbers line up, the rules are visible, and nothing is claimed that
cannot be checked. The interface is a measuring instrument, not a stage — it
reports, it does not persuade. Where marketing software shouts, this one states.

The visual world is **strict and high-contrast**. Type is large enough to read at
a glance, boundaries are explicit, and the ink is dark on paper. Density is
deliberate: an operator scanning a timesheet at 09:00 wants the figures, not
atmosphere. This is an anti-reference to marketing gloss and to the purple
gradient AI aesthetic — the accent here is a *signature*, applied once and
meaningly, never as decoration.

Depth is **flat**. Surfaces are separated by tone and by a hairline rule, the way
a ledger separates columns. There are no floating cards, no ambient glows, no
drop shadows pretending to be material. If something is on top of something
else, a 1px border says so.

**Key Characteristics:**
- Flat surfaces; depth from tonal layering (`base-100 / 200 / 300`) and hairline rules only
- High contrast ink-on-paper in both themes
- Tabular numerals everywhere digits must align in a column
- One accent (indigo ink), used for identity, the current place and focus, never for the main button
- A measured type scale (1.2 ratio) with negative tracking on headings
- A single radius language: 6px for small selectors, 10px for fields and containers, fully round for status pills and chips; the phone shell's sheet and cards round a little more
- Every control has a fixed geometric lane — 40 / 32 / 24 px, grown for touch

## Colors

Two themes, one grammar. Light is the canonical reading of the palette; dark is
its inversion with the same roles and the same meanings. Names below are the
character of the colour, not its hex. Values live in the two daisyUI themes in
`web/input.css` (`paratrack-light`, `paratrack-dark`).

### Primary
- **Indigo Ink** (`#6366f1`): the signature. It marks where you are and what
  is live: the active tab in the phone tab bar, the focus ring, a sent
  document's status, the action inside a toast, text selection. It is **not**
  the main button: the one call to action per form is charcoal (see Buttons).
  Its rarity is the point — a ledger is signed, not coloured in.
- **Ink for text on tints** (`--ink-primary #4f46e5`, `--ink-success #047857`,
  `--ink-warning #92400e` in light; the plain theme colours in dark): the
  saturated status hues fail contrast as text on their own 15% tint (≈2:1),
  so pills and statuses write in these deeper inks.

### Secondary
- **Violet Ink** (`#8b5cf6`): the second hand, kept as the theme's `secondary`
  but not used by any screen today. If it is ever needed, it marks a
  related-but-distinct entity and never competes with the signature.

### Neutral
- **Ledger White** (`#ffffff`): the sheet. Card and input surfaces in light theme.
- **Ledger Paper** (`#f9fafb`): the desk the sheet lies on. Page background,
  badge fills, quiet chips.
- **Ledger Rule** (`#e5e7eb`): hairlines, dividers, table rules, input strokes.
- **Ledger Ink** (`#1a1d23`): the writing. All primary text in light theme.
- **Ledger Charcoal** (`#1f2937`): the primary button fill (`btn-neutral`) and dark-neutral blocks.

### Dark theme
The same roles, one step lighter or darker:

| Role | Light | Dark |
|---|---|---|
| Page (`base-200`) | `#f9fafb` | `#232730` |
| Sheet (`base-100`) | `#ffffff` | `#1a1d23` |
| Rule (`base-300`) | `#e5e7eb` | `#2d3139` |
| Ink (`base-content`) | `#1a1d23` | `#e5e7eb` |
| Signature (`primary`) | `#6366f1` | `#818cf8` |
| Main button (`neutral`) | `#1f2937` fill, `#f9fafb` ink | `#e5e7eb` fill, `#0f1115` ink |
| Success / warning / error | `#10b981` / `#f59e0b` / `#dc2626` | `#34d399` / `#fbbf24` / `#f87171` |
| Info | `#06b6d4` | `#22d3ee` |
| Secondary (unused) | `#8b5cf6` | `#a78bfa` |

### Status
- **Info Cyan** (`#06b6d4`): informational notices only.
- **Verdict Green** (`#10b981`): paid, done, active, on-track. A conclusion, not a mood.
- **Caution Amber** (`#f59e0b`): paused, near-limit, needs attention.
- **Verdict Red** (`#dc2626`): destructive and failed. High contrast against white
  ink (4.83:1) — a destructive label must never be hard to read.

### Named Rules

**The Signature Rule.** The primary accent occupies ≤10% of any screen. It marks
identity, the current place and focus. The main action is charcoal, so indigo
never has to shout. If indigo appears on a figure or a decoration, it is wrong.

**The No-Fake-Ink Rule.** A user-authored colour (project colour, activity
colour) is never used as text ink. It appears as a swatch beside neutral text,
or with an explicitly computed contrast-safe ink (`inkFor`). A pale lime project
name must still be readable.

**The Two-Theme Rule.** Every semantic colour means the same thing in both
themes. Green is a verdict in both; amber is caution in both. Only the lightness
shifts.

## Typography

**Display Font:** Fraunces (fallback: Iowan Old Style, Palatino) — **the wordmark
only.** Nowhere else. The one place the ledger is signed.
**Body Font:** Inter (fallback: system UI sans) — everything readable.
**Label/Mono Font:** JetBrains Mono — code, durations, money, IDs, times.

**Character:** Inter is the clerk's hand: neutral, even, unfussy. Fraunces is the
seal on the cover — used once, never borrowed for headings. JetBrains Mono is
the ruled column: digits must sit in a vertical line, so every measured figure
is monospaced and tabular.

### Hierarchy

- **Display** (620, 1.35rem, lh 1, tracking −0.03em, Fraunces `SOFT 40 / WONK 1`):
  the `paratrack` wordmark and nothing else.
- **Headline** (650, 1.728rem / `--step-3`, lh 1.15, tracking −0.02em): page `h1`,
  exactly one per page.
- **Section** (620, 1.44rem / `--step-2`, lh 1.2, tracking −0.015em): `h2` section
  headings on a page.
- **Title** (600, 1.2rem / `--step-1`, lh 1.3, tracking −0.01em): card titles and
  `h3`.
- **Body** (400, 1rem / `--step-0`, lh 1.55): all running text. Measure held to
  65–75ch by container width.
- **Label** (600, 0.694rem / `--step--2`, tracking 0.08em, uppercase): a block's
  own name when the block is a ledger strip, such as «ИДУТ СЕЙЧАС» or
  «НЕ ВЫСТАВЛЕНО» (`.ledger-label`, `.stat-title`). It names the block; it is never
  a kicker above a separate heading.
- **Control text:** labels of fields 550 at `--step--1`; nothing interactive or
  readable goes under 12px (daisyUI's xs sizes are raised to 0.75rem).

Scale ratio is **1.2**, anchored at `--step-0 = 1rem`:
`0.694 · 0.833 · 1 · 1.2 · 1.44 · 1.728 · 2.074rem`.

### Named Rules

**The One Headline Rule.** Exactly one `h1` per page, and it names the page. Cards
open with `h2`/`h3`, never with a heading level jump.

**The Wordmark Lock.** Fraunces is the logo face. A heading, a stat, or a card
title set in Fraunces is a bug — the seal does not do the writing.

**The Ruled Column Rule.** Any figure a user compares vertically (durations,
money, timestamps, counts) renders in JetBrains Mono with `tabular-nums`. Ragged
digits in a column are a defect, not a style.

## Layout

A single centered column, `max-w-6xl` (72rem), with 16px gutters (`px-2` below
400px). Content stacks vertically; multi-column grids (`sm:grid-cols-2`,
`lg:grid-cols-3`) appear only where items are peers — project cards, report
templates, marketplace tiles.

Spacing rhythm is a **4px unit** (`--spacing: 0.25rem`):
- **4px** — icon-to-label gap in compact controls
- **8px** — tight groups (chip internals, label→input)
- **12px** — control padding, inter-control gap
- **16px** — block padding, card gutters, standard separation
- **24px** — card internal padding (`p-5`/`p-6`), section separation

Related content sits tightly; distinct groups are separated generously. More
space above a heading than below it.

**Responsive.** Three real breakpoints: `sm` 40rem (640px), `md` 48rem (768px),
`lg` 64rem (1024px).
- **Below 1024px** the app becomes a phone shell: the header keeps only the
  wordmark, a fixed tab bar (`.tabbar`, 3.5rem) carries four user-chosen tabs
  and «Ещё», which opens a bottom sheet (`dialog.sheet`) with every other
  section; a running timer sits in a bar above the tabs (`.minibar`). Wide edit
  tables (`.collapse-lg`) turn into cards at this width too.
- **Below 640px** every data table collapses to labelled cards
  (`.responsive-collapse`); card cells wrap, and cells marked `whitespace-nowrap`
  (times, sums) keep one line. In the stats log card
  date-times take the full width, duration and note sit side by side.
- Scrollable strips (`period-tabs`, settings tabs) pan horizontally under touch
  (`touch-action: pan-x pan-y`), never swallow vertical scroll, and open with
  the current tab scrolled into view.
- **Touch** (`pointer: coarse`), measured from `web/input.css`:
  - `btn-sm` and `select` draw at 44px;
  - `input-sm`, `input-xs`, `select-sm` and `btn-xs` draw at 36px;
  - `btn-xs` and `btn-circle` also get a 44×44px hit area from a centred
    pseudo-element, and `checkbox-xs` the same;
  - period tabs draw at 40px;
  - badges and table links keep their drawn size and get a hit area 10px taller
    on each side (about 40px) from a pseudo-element.

**Named Rules.**

**The One-Row Rule.** The header never wraps onto a second line. At 1024px and
up it is wordmark · workspace switcher · links · «Ещё» · account; the workspace
name is the one item that gives up width (it truncates, the full name is in its
tooltip). The link cluster is not a scroll container, because overflow would
clip its dropdowns. Below 1024px the header is the wordmark alone. A crooked
header is the first thing a user sees.

**The Shrink-to-Truncate Rule.** A grid or flex item that carries a name must
allow itself to shrink (`min-width: 0`) so `truncate` can work. A 200-character
project name must never widen the page.

## Elevation & Depth

**This system is flat.** Depth is conveyed by tonal layering — `base-100` on
`base-200` on `base-300` — and by a hairline `1px` rule (`--border`). There are
no drop shadows, no ambient glows, no floating cards, and no blur-as-decoration.

A dropdown or menu that must sit above the page says so with a border and a
tonal step, not with a shadow. The rule is visible; that is the depth system.
The one overlay that dims the page is the phone's bottom sheet: a native
`<dialog>` without a border over a 40% black backdrop, because it takes the
whole screen's attention.

### Named Rules

**The No-Shadow Rule.** Surfaces do not cast shadows. `box-shadow` has one
allowed use: an inset 1px ring that draws a hairline without shifting layout
(the active period tab, the chosen preset card). The focus ring is an `outline`,
not a shadow. Shadows are never used for elevation (`--depth: 0` in both themes). If a component looks like
it needs a shadow to separate from its background, the background tone is wrong.

**The Hairline Rule.** A `1px` solid `base-300` rule is the separator. It is used
for card edges, table rows, dividers and input strokes — the same weight
everywhere. A 2px rule appears only as a total rule, where it marks a sum (a
table's total row, the ledger strip's «Сегодня» line, the amount on a letter).

## Shapes

One corner language: two steps for the app's controls and containers, round for tokens, and a few larger corners reserved for the phone shell:

- **6px (`--radius-selector`)** — small internal controls: daisyUI badges (the
  project badge in a row, a tag link), checkboxes, toggles, kbd caps.
- **10px (`--radius-field` / `--radius-box`)** — every field and every container:
  buttons, inputs, selects, cards, dropdowns, alerts.
- **Phone shell surfaces** — the only larger corners, all on the touch shell:
  the bottom sheet's top corners 20px (`1.25rem`), the running-timer card in
  the minibar 16px, rows inside the sheet 12px. The chart's loading bars round
  their top corners at 4px.
- **Round (`999px`)** — things that state a status or hold a label as a token:
  status pills (`.status-pill` «Активна» / «Пауза»), tag chips (`.tag-chip`),
  document statuses (`.doc-status`), colour dots, and icon-only controls
  (`btn-circle`).

Corner radius is applied to the outside of the element and never doubled by an
inner radius.

Borders are `1px` hairlines in `base-300` (or `base-content` at low opacity in
dark). No thick accent bars on cards. No gradient strokes.

## Components

### Buttons

A **three-step ladder** of heights with several variants on it. The height is the
rank; the variant is the meaning. Measured heights are 40 / 32 / 24 px with a fixed 10px radius and a 6px
icon-to-label gap (`gap-1.5`; `gap-1` on the 24px lane).

- **Shape:** 10px radius (`--radius-field`); icon-only buttons are circles.
- **CTA (40px)** — `btn-neutral`: charcoal fill (`#1f2937`), paper ink
  (`#f9fafb`), 16px inline padding, 14px semibold. Exactly one per form.
- **Row / nav (32px)** — `btn-ghost`: transparent, ink text, 12px padding, 12px
  semibold. Navigation, toolbars, row actions.
- **Destructive (32px)** — `btn-error`: `verdict-red` fill, white ink (4.83:1).
  Irreversible Delete. Never decorative.
- **Stop (32px)** — `.btn-stop`: transparent, hairline `base-300` border,
  `verdict-red` ink, tint only on hover. Stop is instant and undoable from
  its toast, so it reads as a line item, not an alarm. On a phone card
  Pause/Resume is the wide button and Stop its narrower peer.
- **Quiet-danger (32px)** — `btn-ghost` + `text-error` ink on transparent.
  Row-level removals.
- **Chip / micro (24px)** — `btn-ghost` `btn-xs`: 8px padding, 12px semibold.
  Filter chips, tag remove, inline toggles.

- **Quick start (32px)** — `btn-sm btn-quick`: transparent, hairline `base-300`
  border, weight 500. The example activities under an empty «Идут сейчас»
  («работа», «чтение», …): one tap starts that timer.

**States.** Hover lifts the fill one tonal step (ghost gains `base-200`).
`focus-visible` draws a 2px solid `primary` outline at 2px offset (1px inside
a toast, where the action sits against the toast's edge). Disabled is
`base-content` at 20% opacity with `pointer-events: none`. Loading sets
`aria-busy` and disables the button.

**Named Rules.**

**The Ladder Rule.** A button's height states its rank. 40px = do this now,
32px = act on this row, 24px = tweak this chip. Mixing ranks in one cluster is
a defect.

**The Fixed Order Rule.** Classes read `btn → variant → size → shape → gap →
layout → state`. Scrambled order is a signal the system was not consulted.

### Chips & Badges

Two kinds, and they do not swap:

- **Pills and chips (round, `999px`)**: `.status-pill` for a timer's state,
  tinted with the status colour at 15% and written in the deeper status ink;
  `.tag-chip` on `base-200` for a tag; `.doc-status` for an invoice or pay run,
  which reads by word and ink (sent in `--ink-primary`, paid in `--ink-success`),
  not a grey pill for every state.
- **Badges (6px, daisyUI `badge`)**: the project badge next to an activity in a
  row, filled with the project's colour and written in computed ink (`inkFor`);
  a tag as a link. A long project name truncates inside the badge (a `span.truncate`
  in it, `max-width` on the badge) and never pushes the activity out.

Activity and project names elsewhere carry a colour dot before neutral ink —
the dot is the colour, the text is always readable. Removable chips carry a
24px circular remove control with a 44px hit area on touch.

### Cards / Containers

- **Corner:** 10px (`--radius-box`)
- **Background:** `base-100` on a `base-200` page (light); the inverse in dark
- **Border:** 1px `base-300` hairline — this *is* the elevation
- **Shadow:** none
- **Padding:** 24px (`p-5` / `p-6`)
- **Title:** `title` role, `h2`, with more space above than below

### Inputs / Fields

- **Style:** `base-100` fill, 1px `base-content` stroke at 20% opacity, 10px
  radius, 12px padding, 40px height
- **Focus:** stroke goes to full `base-content` and a 2px `primary` outline appears
  at 2px offset. No glow.
- **Error:** `verdict-red` stroke plus inline message beside the field
- **Disabled:** `base-200` fill, 40% ink, `not-allowed` cursor
- **Labels** are always programmatically associated (`for`/`id`). A label that is
  only visually adjacent is a defect.

### Navigation

- **Desktop (1024px and up):** a single 48px row (see The One-Row Rule). Links
  are 32px ghost buttons with an icon; the active page is `btn-active` (one tonal
  step). «Ещё» is a dropdown with the sections the workspace has switched on;
  the account menu holds settings, help and sign-out.
- **Phone (below 1024px):** the tab bar at the bottom: four tabs the person picks
  in «Под себя» plus «Ещё». The current tab is indigo (the signature marks the
  place), the rest are ink at reduced opacity. «Ещё» opens a bottom sheet with
  every other section, the workspaces and the account. Its group headings are
  small caps of their own: 0.75rem, weight 600, uppercase, tracking 0.05em, at
  55% ink.
- **Footer (both):** language and theme. The current language is a non-link
  `span` with a check; theme cycles auto / light / dark.
- Menus and dropdowns sit above page content: the header carries its own
  z-index, because its `view-transition-name` makes it a stacking context.

### Tables

Hairline rules, `base-200` zebra on even rows, column heads in sentence case at
weight 600, and monospace tabular numerals in every measured column. Total rows
take a `2px` top rule. An empty measured cell shows «—» in the app (the public
site uses a minus icon, see Public site). Below 640px rows become labelled
cards (`.responsive-collapse`); long logs show the newest rows first with a way
to see all, never an unbounded table.

## Do's and Don'ts

### Do:
- **Do** keep depth flat: tone and hairlines only. A `1px base-300` rule separates everything; only the phone's bottom sheet dims the page.
- **Do** render every comparable figure in JetBrains Mono with `tabular-nums` — durations, money, times, counts.
- **Do** put exactly one `h1` on a page and one `btn-neutral` CTA in a form; keep indigo off that button.
- **Do** use a colour swatch beside neutral text for user-authored colours; compute contrast-safe ink (`inkFor`) when a colour becomes a background.
- **Do** keep the primary accent under 10% of a screen. One signature per page.
- **Do** measure the button lanes: 40 / 32 / 24 px, 10px radius, 6px icon gap.
- **Do** let names truncate: `min-width: 0` on the container, `truncate` on the name.
- **Do** theme browser surfaces: `::selection`, `caret-color`, `text-underline-offset`, `scrollbar-color`.
- **Do** hold body measure to 65–75ch and set labels in uppercase `--step--2` with 0.08em tracking.

### Don't:
- **Don't** use box-shadow for elevation. Its one use is the inset 1px hairline ring; focus is an outline.
- **Don't** use Fraunces anywhere but the wordmark. The seal does not do the writing.
- **Don't** paint user-authored colours as text ink. A lime activity name on white is a defect.
- **Don't** wrap the header onto a second row: truncate the workspace name, and below 1024px move links to the tab bar.
- **Don't** hard-code `#fff` ink on a user-coloured background — compute it.
- **Don't** mix button lanes in one cluster, or scramble the class order.
- **Don't** rely on hover for anything a touch user must reach; give compact controls a 44px hit area.
- **Don't** add gradient text, glassmorphism, decorative blur, or thick accent borders on cards. The ledger does not shimmer.
- **Don't** drop `prefers-reduced-motion`: kill duration and movement, keep state change and hierarchy.

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
