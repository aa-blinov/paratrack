---
version: 1
slug: "internal-web-templates-export-html"
primary_target: "internal/web/templates/export.html"
related_targets: []
---

Scope: authenticated /export page reached from desktop and mobile More, and from the statistics session log. Mode: Operate. The user needs to know exactly what the CSV contains before starting a download.

Audience and task: a person exporting their own time records for inspection or spreadsheet use. Preserve the existing raw CSV format and the direct /api/reports.csv URL for API clients. No new export formats or claims.

## Direction contract

THESIS: A download is an informed action, not a surprise side effect of opening a menu. One page names the dataset, the period and the resulting file before the user presses Download.

OWN-WORLD: Inherit DESIGN.md's Honest Ledger: flat paper-and-ink surface, explicit field labels, 1px rules, charcoal action, familiar responsive inputs. No ornamental file preview or invented example data.

STORY: Open Export, read that it contains completed sessions and which columns are included, optionally set dates, then download. An empty period still produces a CSV with a header.

FIRST VIEWPORT: Heading and one-line purpose above a single compact form. The dataset description sits above the date controls, followed by the filename and a clearly labeled Download CSV button; on a phone everything stacks without horizontal scrolling.

FORM: A precise extension of the existing app UI; no concept seed required for this narrow request.

FINISH: unreviewed and undocumented is unfinished; this build ends with the finish review, the verdict, DESIGN.md, and every shipping raster carrying its provenance
