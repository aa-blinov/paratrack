# Screenshots

There are no image files in this folder.

The screenshots of paratrack live with the landing page, in
`site/assets/shots/`, because the landing page is what shows them. They are
WebP, fourteen of them, and every one of them has a `.json` file next to it
that records where the picture came from.

Every one of the fourteen was taken the same way, on 2026-09-28, and the
sidecar says so in the same words: a screenshot of the application at 2x,
captured with Playwright and Chromium from a local instance seeded with
synthetic demo data, the workspace "Студия «Кедр»" and the demo user Анна
Смирнова, converted to WebP with ffmpeg. They are photographs of the
application, and nothing in them was drawn.

| Image | What it shows | Size |
| --- | --- | --- |
| `dashboard.webp` | The dashboard: three timers running at once, and the block of unbilled work below. | 2880 × 1800 |
| `dashboard-dark.webp` | The same dashboard in the dark theme. | 2880 × 1800 |
| `timesheet.webp` | The week timesheet: rows of activities, columns of days, and the totals. | 2880 × 1800 |
| `invoice.webp` | Invoice `INV-2026-001`: the contractor, the customer with their details, the lines of work and the total. | 2880 × 1800 |
| `stats.webp` | The statistics for a week: the breakdown by project and by activity, with the filters by period, person and tag. | 2880 × 1800 |
| `graph.webp` | Minutes by the hour of the day, over a month. | 2880 × 1800 |
| `members.webp` | The members: the roles, the rate per hour and the capacity. | 2880 × 1800 |
| `payroll.webp` | The payout calculation: a row per member and the total. | 2880 × 1800 |
| `schedule.webp` | The schedule: the people by day of the week, and the load. | 2880 × 1800 |
| `projects.webp` | The list of projects, with the time for today and for the last 30 days. | 2880 × 1800 |
| `prefs.webp` | The interface settings: the formats, the home page, the sections and the bottom bar. | 2880 × 1800 |
| `sections.webp` | The choice of workspace sections: three ready sets and the list of sections. | 2880 × 1800 |
| `phone.webp` | The application on a phone: the cards of the running timers and the bottom tab bar. | 780 × 1688 |
| `phone-stats.webp` | The application on a phone, on the statistics screen. | 780 × 1688 |

The landing page links them as `/assets/shots/<name>.webp`, and the
repository README links `site/assets/shots/dashboard.webp` directly.

The screen documents in `../screens/` describe the same screens in words,
one file per screen. A screen that has no picture still has a document.
