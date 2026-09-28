---
version: 1
slug: "site-index-html"
primary_target: "site/index.html"
related_targets: ["site/_layouts/doc.html"]
---

Scope: GitHub Pages landing for paratrack (site/index.html) plus the user docs layout (site/_layouts/doc.html). Mode: Persuade for the landing, Read for docs. Russian only.

Audience: someone tracking their own day, a freelancer billing clients, a small studio. Job: understand what paratrack is and open the service (primary CTA https://paratrack.duckdns.org/register), or self-host (secondary). Proof: the product itself (a working replica of «Идут сейчас», real screenshots from seeded demo data, labeled as demo), the invoice math. Constraints: no prices, no customers, no usage numbers, no invented claims (PRODUCT.md).

## Direction contract

THESIS: The first screen is the product, not a picture of it: parallel timers tick and respond, and further down the same minutes carry into a week timesheet and an invoice whose lines hold "hours × rate = amount". Refuses the category default of hero slogan + laptop mockup + three icon cards.

OWN-WORLD: The app's Honest Ledger world from DESIGN.md: ink #1a1d23 on paper #f9fafb / white sheets, 1px #e5e7eb rules, flat (no shadows), indigo #6366f1 only for the one primary action and focus, Fraunces for the wordmark only, Inter for text, JetBrains Mono tabular for every figure, 10px radius, 2px total rule on sums.

STORY: Visitor sees three timers running at once, pauses one, presses «Только эта», and understands parallel tracking; scrolls to see those minutes as a timesheet row and an invoice total that adds up; learns it grows from one person to a studio and can run on their own server; opens the service or the docs.

FIRST VIEWPORT: Top bar: wordmark left, links Документация / GitHub, button «Открыть paratrack». Left column (5/12): h1 naming the product in one line, one paragraph, primary button «Открыть paratrack», secondary «Развернуть у себя». Right (7/12): a live «Идут сейчас» card, three rows ticking (mono), working Пауза / Только эта / Стоп, today's total under a rule.

FORM: "Живой журнал", position 1 of 7 on the ranked list, seed key 44812dfd.

FINISH: unreviewed and undocumented is unfinished; this build ends with the finish review, the verdict, DESIGN.md, and every shipping raster carrying its provenance
