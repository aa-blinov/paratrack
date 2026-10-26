# Design

## Context

Approved weeks are rows in `timesheet_approvals`. Each write path already calls `lockApprovedWeek` or a span check built on it, in the same transaction as the write (`internal/db/timesheet_approvals.go`). The workspace calendar (`teams.timezone`, `teams.week_start`, ADM-07) is set by the owner or an admin through `SetTeamCalendar`. The database layer gets its clock from `d.now()`, injected for tests. See proposal.md for the motivation.

## Goals / Non-Goals

**Goals:**
- One rule that refuses any write that starts before a date computed from the workspace's own calendar.
- The same refusal on every write path, with the same text.
- A timer that is already running can always be stopped.
- The timesheet page says up to which day the sheet is closed.

**Non-Goals:**
- Approval, payroll, invoices and the approved-week message do not change.
- No per-week exceptions and no new role.
- No scheduled job. The cutoff is computed on each write and on each page view.

## Decisions

**D1. The term is a column on `teams`, not a row per week.** `teams.autolock_days INTEGER NOT NULL DEFAULT 0`, allowed values 0, 3, 7, 14, 30. Existing workspaces get 0, so their behaviour does not change. Alternative: write `timesheet_approvals` rows from a job. Rejected: there is no scheduler, and it would turn a closing date into an approval, which pays out.

**D2. The cutoff is computed, not stored.** `cutoff = start of the local day N days before today`, where today is `d.now()` converted to the workspace's zone. The zone is `teams.timezone`, UTC when blank. A member's own zone is ignored for this rule, so a member far from the workspace zone can lose a few hours at the boundary day. Accepted: one boundary per workspace is what the rule needs, and the same zone already drives weeks and exports.

**D3. A separate check, not a change to `lockApprovedWeek`.** `lockApprovedWeek` takes no clock, and the stop path must skip the new check. A new function `lockAutoClosed(ctx, tx, teamID, at, now)` in `internal/db/timesheet_autolock.go` runs next to the approval check at these sites: `sessions.go` (update, create closed, delete), `timesheet.go` (cell and row spans), `session_transitions.go` (reopen). `UpdateSessionEnd` (stop) does not call it. Pause and resume do not call approval checks either, so they stay as they are.

**D4. A running session may be edited and deleted only if its start is after the cutoff.** Stop is exempt. A forgotten timer that started before the cutoff can therefore be stopped, but its start cannot be moved or deleted. Simplest rule that keeps the start fixed.

**D5. No bypass for the manager.** An owner or admin who needs to correct an old day changes the term (to a longer one or to 0), then changes it back. Alternative: let managers write past the cutoff. Rejected for this change: every write path would need the caller's role, the approval lock has no such bypass either, and a silent manager bypass is what the feature is meant to stop. Recorded as an assumption for the user to confirm.

**D6. A new error with its own sentinel.** `model.SessionAutoLockError{Before time.Time}` unwraps to `model.ErrDateClosed`. Web handlers map it next to `SessionApprovalLockError`. The API maps it to 409 with the same text.

**D7. The page gets a ready sentence from the server.** `TimesheetAutoLockCutoff(ctx, teamID) (time.Time, bool, error)` in `internal/db` returns the cutoff, and the tracking service exposes it. The timesheet view model carries `AutoLockNotice`, a localized sentence, or an empty string. The React page renders the string if it is not empty. An empty or missing value renders nothing, so existing fixtures keep working.

**D8. The term is set in the calendar form.** `appmodel.TeamCalendarRequest` gets `AutoLockDays`. `SetTeamCalendar` writes it with the same manager check (`execManagerTeamUpdate`). `model.Team` gets the field, read in the four `teams.go` queries. The form gets one select with fixed options, and the labels are four keys in `ru.json` and `en.json`.

**D9. Changes to the term are not in the journal.** The calendar form does not write an audit row today, and this change does not add one. Consequence: an owner can change the term to edit an old day with no trace. Recorded as an open question below.

## Risks / Trade-offs

- [A boundary day for members in a far zone] → the workspace zone decides, shown in the page's own date. Accepted per D2.
- [A manager edits an old day by changing the term] → it is visible only in the term's current value, not in the journal. Mitigation: open question below.
- [A term set to 3 days locks the last week while the manager is still approving] → approval does not need edits, so it still works. The notice on the page says what is closed.
- [More reads per write] → one indexed read of the team row inside the transaction. Writes are human-paced, so this is cheap.
- [Missed call site] → the write sites list above is the only one that touches sessions or cells. A test per site (manual add, edit, delete, cell, row, reopen, API) guards it.

## Migration Plan

1. Add the column with the default in `schema_compat.go`, like `timezone`.
2. Deploy. Every workspace keeps 0, so nothing changes until an owner sets a term.
3. Rollback: the column stays and is ignored by the previous build. Setting it back to 0 is enough to stop the rule without a schema change.

## Open Questions

- Should a change of the term write a journal row (ADM-02)? Not blocking: it can be added to this change or a follow-up without touching the rule. Default for now: no journal row, as today for the calendar.
