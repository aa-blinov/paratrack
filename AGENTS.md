# Agents

## After every push

Check the GitHub Actions run for that commit and wait until it finishes:

```bash
gh run list --limit 3
gh run watch <run-id> --exit-status
gh run view <run-id> --log-failed   # on failure
```

A red run is not done work: fix it and push again before reporting the task
finished. Tell the user the CI result (green, or what failed and the fix).

## Interface copy and the middot `·`

A middot used as decoration is slop: no separator between a label and its
caption. It is gone from the budget card, where it used to sit mid-line
("Отработано · Валюта клиента" — the source is now its own line under the
sum).

The middot in an invoice line ("Сайт-каталог · Вёрстка страниц") is **not**
decoration and must stay. It is document text: `OverlappingInvoices`
compares line labels to catch the same hours on two documents, and the
`stampLegacyInvoices` data migration matches on `project || ' · ' ||
activity`. Changing the separator is a data migration over issued invoices,
not a UI cleanup.

Paragraphs that restate the visible interface are slop and are gone
(dashboard, projects, stats, export, reports, integrations, clients).
Hints that carry meaning stay: empty states, dangerous actions, and places
where the interface is not self-explanatory.

A hint that restates its own card's title is slop too — under «Часы по дням»
there is nothing to add with «Итоги за каждый день периода». Keep the
sentence that says what a number means, what a control does, or what will
happen; cut the one the screen already says.

## Alignment inside a card

A card's title and its own body must agree. Empty states used to be centred
while every card title is left-aligned, so a single card read as two
decisions stacked on top of each other. Measured across every tab: 4 cards
disagreed, now 0. A page-level empty state with no card title above it stays
centred — there is nothing for it to disagree with.

Note that the shadcn `Card` paints with a ring, not a border: a detector that
looks for `border-width` finds no cards at all and reports a clean sweep.

## Dates

Never reach for `<input type="date">` or `datetime-local` again. The browser
draws them in *its* locale and no attribute moves it, which is how a Russian
screen ended up offering «10/13/2026». Use `DateField` / `DateTimeField` from
`@/components/date-field`: they read the document language, carry a hidden
input with the same ISO value the server always received, and accept both the
screen's own order and ISO in either language.

Two rules the component holds to, both learned the hard way:

- A value that could not be read submits **nothing**, not the previous one. A
  half-typed period must never become an invoice period.
- `--text-base` is 14px in this theme, so a field that must stay at 16px on a
  phone (iOS zooms below that) says so through the `style` prop. A Tailwind
  `text-base` class will not do it: it resolves to the same 14px token.

`web/src/lib/date-format.ts` holds the parsing and is covered by
`date-format.test.ts` — run it with `npm run test:unit` in `web/`. The tests
pin the traps: 3/4/2026 is 4 March in English and 3 April in Russian, and
31 February must be refused rather than rolled into March.

Two more traps in the same component, both found by looking rather than by
reading:

- The `Card` paints with `overflow-hidden`, so a calendar rendered inside it
  loses its bottom half. It is portalled out and placed against the field —
  and it opens **upwards** when there is no room below.
- That portal targets `#paratrack-react-root`, **not** `document.body`: the
  interface CSS is scoped under that id, so a calendar sent to the body comes
  out unstyled and transparent, sitting on the text behind it.

## Measuring colour

The app transitions colours, so anything that reads computed styles has to
switch transitions off first. `a11y_check.py` measured 200 ms after flipping
`data-theme` and caught the middle of the transition — a dark background with
the light theme's near-black text still in flight, reported as 1.1:1 for the
whole sidebar. It passed locally and failed on CI, and a re-run confirmed it
was a race rather than a regression.

Measured, when it was pinned down: right after the switch the text is
`oklab(0.145 0 0)`, and by 50 ms it is already `oklch(0.985 0 0)`. A fixed
wait only loses on a slow runner. Inject
`*,*::before,*::after{transition:none!important;animation:none!important}`
after each navigation — `add_style_tag` does not survive a `goto`.

## Lists of things: table, not tiles

`/projects` is a table with rows that open into their activities. It was a
tile grid, and the tile was the wrong shape: it spent a quarter of the screen
per project to show three numbers that could not be compared across projects,
and it had nowhere to put the client the project is billed to.

When a list grows a column that a tile cannot carry, make it a table rather
than adding a fifth number to the card. On phones the table drops its columns
and stacks what is left under the name — measured, not guessed: at 375px a
name plus three numeric columns truncated to «Або…».
