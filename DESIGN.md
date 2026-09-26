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

# Design System: paratrack

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
- One accent (indigo ink), used sparingly and with weight
- A measured type scale (1.2 ratio) with negative tracking on headings
- A single radius language: 6px for small selectors, 10px for fields and containers
- Every control has a fixed geometric lane — 40 / 32 / 24 px

## Colors

Two themes, one grammar. Light is the canonical reading of the palette; dark is
its inversion with the same roles and the same meanings. Names below are the
character of the colour, not its hex.

### Primary
- **Indigo Ink** (`#6366f1`): the signature. Reserved for the wordmark's world,
  active affordances, focus rings and the one primary action per surface. Its
  rarity is the point — a ledger is signed once per page.

### Secondary
- **Violet Ink** (`#8b5cf6`): the second hand. Related-but-distinct entities —
  a linked project chip, a secondary series in a chart. Never competes with the
  signature.

### Neutral
- **Ledger White** (`#ffffff`): the sheet. Card and input surfaces in light theme.
- **Ledger Paper** (`#f9fafb`): the desk the sheet lies on. Page background,
  badge fills, quiet chips.
- **Ledger Rule** (`#e5e7eb`): hairlines, dividers, table rules, input strokes.
- **Ledger Ink** (`#1a1d23`): the writing. All primary text in light theme.
- **Ledger Charcoal** (`#1f2937`): the primary button fill and dark-neutral blocks.

### Status
- **Info Cyan** (`#06b6d4`): informational notices only.
- **Verdict Green** (`#10b981`): paid, done, active, on-track. A conclusion, not a mood.
- **Caution Amber** (`#f59e0b`): paused, near-limit, needs attention.
- **Verdict Red** (`#dc2626`): destructive and failed. High contrast against white
  ink (4.83:1) — a destructive label must never be hard to read.

### Named Rules

**The Signature Rule.** The primary accent occupies ≤10% of any screen. It marks
identity, focus, and the single primary action. If two things are indigo, one of
them is wrong.

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
- **Title** (600, 1.2rem / `--step-1`, lh 1.3, tracking −0.01em): card titles and
  section anchors.
- **Body** (400, 1rem / `--step-0`, lh 1.55): all running text. Measure held to
  65–75ch by container width.
- **Label** (600, 0.694rem / `--step--2`, tracking 0.08em, uppercase): stat
  titles, overlines, column heads. The ledger's column headers.

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
`lg` 64rem (1024px). Tables collapse to labelled cards below 640px
(`.responsive-collapse`). The topbar is one row that never wraps: logo · scrollable
link cluster · fixed action cluster. Scrollable strips (`period-tabs`, nav)
pan horizontally under touch (`touch-action: pan-x pan-y`) and never swallow
vertical page scroll. Compact controls expand their hit area to 44×44px under a
coarse pointer without changing their drawn size.

**Named Rules.**

**The One-Row Rule.** The header never wraps onto a second line. If the links do
not fit, they scroll — they do not stack. A crooked header is the first thing a
user sees.

**The Shrink-to-Truncate Rule.** A grid or flex item that carries a name must
allow itself to shrink (`min-width: 0`) so `truncate` can work. A 200-character
project name must never widen the page.

## Elevation & Depth

**This system is flat.** Depth is conveyed by tonal layering — `base-100` on
`base-200` on `base-300` — and by a hairline `1px` rule (`--border`). There are
no drop shadows, no ambient glows, no floating cards, and no blur-as-decoration.

A modal or dropdown that must sit above the page says so with a border and a
tonal step, not with a shadow. The rule is visible; that is the depth system.

### Named Rules

**The No-Shadow Rule.** Surfaces do not cast shadows. `box-shadow` is reserved
for the focus ring (an accessibility affordance) and is never used for elevation.
If a component looks like it needs a shadow to separate from its background, the
background tone is wrong.

**The Hairline Rule.** A `1px` solid `base-300` rule is the separator. It is used
for card edges, table rows, dividers and input strokes — the same weight
everywhere. Thickness variation (`border-t-2`) is allowed only as a table total
rule, where it marks a sum.

## Shapes

One corner language, two steps:

- **6px (`--radius-selector`)** — small internal controls: badges, chips,
  checkboxes, toggles, kbd caps.
- **10px (`--radius-field` / `--radius-box`)** — every field and every container:
  buttons, inputs, selects, cards, dropdowns, alerts.

Corner radius is applied to the outside of the element and never doubled by an
inner radius. Icon-only controls are full circles (`btn-circle`) — the one place
the radius language breaks, and only for a control with no label.

Borders are `1px` hairlines in `base-300` (or `base-content` at low opacity in
dark). No thick accent bars on cards. No gradient strokes.

## Components

### Buttons

A **four-lane ladder**. The lane is the geometry; the variant is the meaning.
Measured heights are 40 / 32 / 24 px with a fixed 10px radius and a 6px
icon-to-label gap (`gap-1.5`; `gap-1` on the 24px lane).

- **Shape:** 10px radius (`--radius-field`); icon-only buttons are circles.
- **CTA (40px)** — `btn-neutral`: charcoal fill (`#1f2937`), paper ink
  (`#f9fafb`), 16px inline padding, 14px semibold. Exactly one per form.
- **Row / nav (32px)** — `btn-ghost`: transparent, ink text, 12px padding, 12px
  semibold. Navigation, toolbars, row actions.
- **Destructive (32px)** — `btn-error`: `verdict-red` fill, white ink (4.83:1).
  Stop, Delete. Never decorative.
- **Quiet-danger (32px)** — `btn-ghost` + `text-error` ink on transparent.
  Row-level removals.
- **Chip / micro (24px)** — `btn-ghost` `btn-xs`: 8px padding, 11px semibold.
  Filter chips, tag remove, inline toggles.

**States.** Hover lifts the fill one tonal step (ghost gains `base-200`).
`focus-visible` draws a 2px solid `primary` ring at 2px offset. Disabled is
`base-content` at 20% opacity with `pointer-events: none`. Loading sets
`aria-busy` and disables the button.

**Named Rules.**

**The Ladder Rule.** A button's height states its rank. 40px = do this now,
32px = act on this row, 24px = tweak this chip. Mixing ranks in one cluster is
a defect.

**The Fixed Order Rule.** Classes read `btn → variant → size → shape → gap →
layout → state`. Scrambled order is a signal the system was not consulted.

### Chips & Badges

Round-ended (`999px`), 24px tall, `base-200` fill on `base-100`. Project and
activity chips carry a **6px colour swatch** before neutral ink — the swatch is
the colour, the text is always readable. Removable chips carry a 24px circular
remove control with a 44px hit area on touch.

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
- **Focus:** stroke goes to full `base-content` and a 2px `primary` ring appears
  at 2px offset. No glow.
- **Error:** `verdict-red` stroke plus inline message beside the field
- **Disabled:** `base-200` fill, 40% ink, `not-allowed` cursor
- **Labels** are always programmatically associated (`for`/`id`). A label that is
  only visually adjacent is a defect.

### Navigation

A single 48px row. Wordmark left (Fraunces), primary links center in a
scrollable cluster, actions right in a fixed cluster. Links are 32px ghost
buttons; the active page is `btn-active` (one tonal step). Below `sm` the link
cluster collapses to a ☰ dropdown and the labels drop from the language and
theme controls. The current language is a non-link `span` with a check — it does
not reload the page.

### Tables

Hairline rules, `base-200` zebra on even rows, uppercase 11px column heads, and
monospace tabular numerals in every measured column. Total rows take a `2px`
top rule. Below 640px rows become labelled cards (`.responsive-collapse`).

## Do's and Don'ts

### Do:
- **Do** keep depth flat: tone and hairlines only. A `1px base-300` rule separates everything.
- **Do** render every comparable figure in JetBrains Mono with `tabular-nums` — durations, money, times, counts.
- **Do** put exactly one `h1` on a page and one `btn-neutral` CTA in a form.
- **Do** use a colour swatch beside neutral text for user-authored colours; compute contrast-safe ink (`inkFor`) when a colour becomes a background.
- **Do** keep the primary accent under 10% of a screen. One signature per page.
- **Do** measure the button lanes: 40 / 32 / 24 px, 10px radius, 6px icon gap.
- **Do** let names truncate: `min-width: 0` on the container, `truncate` on the name.
- **Do** theme browser surfaces: `::selection`, `caret-color`, `text-underline-offset`, `scrollbar-color`.
- **Do** hold body measure to 65–75ch and set labels in uppercase `--step--2` with 0.08em tracking.

### Don't:
- **Don't** use box-shadow for elevation. Shadows are for the focus ring only.
- **Don't** use Fraunces anywhere but the wordmark. The seal does not do the writing.
- **Don't** paint user-authored colours as text ink. A lime activity name on white is a defect.
- **Don't** wrap the header onto a second row. Scroll, do not stack.
- **Don't** hard-code `#fff` ink on a user-coloured background — compute it.
- **Don't** mix button lanes in one cluster, or scramble the class order.
- **Don't** rely on hover for anything a touch user must reach; give compact controls a 44px hit area.
- **Don't** add gradient text, glassmorphism, decorative blur, or thick accent borders on cards. The ledger does not shimmer.
- **Don't** drop `prefers-reduced-motion`: kill duration and movement, keep state change and hierarchy.
