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

## Lists of things: table, not tiles

`/projects` is a table with rows that open into their activities. It was a
tile grid, and the tile was the wrong shape: it spent a quarter of the screen
per project to show three numbers that could not be compared across projects,
and it had nowhere to put the client the project is billed to.

When a list grows a column that a tile cannot carry, make it a table rather
than adding a fifth number to the card. On phones the table drops its columns
and stacks what is left under the name — measured, not guessed: at 375px a
name plus three numeric columns truncated to «Або…».
