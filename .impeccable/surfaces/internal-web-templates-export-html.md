---
version: 1
slug: "internal-web-templates-export-html"
primary_target: "internal/web/templates/export.html"
related_targets: []
---

Scope: authenticated /export page reached from desktop and mobile More, and from the statistics session log. Mode: Operate. The user chooses between exporting individual sessions and finding an existing summary report, and knows what each CSV contains before downloading.

Audience and task: a person exporting their own time records for inspection or spreadsheet use. Preserve the existing raw CSV format and the direct /api/reports.csv URL for API clients. No new export formats or claims.

## Direction contract

THESIS: A download is an informed action, not a surprise side effect of opening a menu. Individual sessions and summary reports are different jobs, not a list of arbitrary file formats.

OWN-WORLD: Inherit DESIGN.md's Honest Ledger: flat paper-and-ink surface, explicit field labels, 1px rules, charcoal action, familiar responsive inputs. No ornamental file preview or invented example data.

STORY: Open Export, choose either a CSV of completed sessions with optional dates or the existing report gallery for totals by project, activity or day. An empty session period still produces a CSV with a header. The reports route is offered only to owners/admins of workspaces with Reports enabled; other states explain what is unavailable.

FIRST VIEWPORT: Heading names the choice, then the session export explains its columns above optional dates, filename and a clear Download CSV button. A second, shorter section links to existing reports or explains how to enable/access them; the phone stacks both without horizontal scrolling.

FORM: A precise extension of the existing app UI; no concept seed required for this narrow request.

FINISH: unreviewed and undocumented is unfinished; this build ends with the finish review, the verdict, DESIGN.md, and every shipping raster carrying its provenance
