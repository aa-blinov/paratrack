import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { DateField } from "@/components/date-field"
import { Label } from "@/components/ui/label"
import type { TaskReportRow } from "@/dashboard/types"
import { translate as t } from "@/i18n"

// The task report (TASK-04): one line per task with time in the period. The period
// is a plain GET form, so the page reloads with the same days in the address and
// the CSV link carries them too.
export function TaskReport({ lang, from, to, note, rows, total }: { lang: string; from: string; to: string; note: string; rows: TaskReportRow[]; total: string }) {
  const csvHref = `/api/tasks/report.csv?${new URLSearchParams({ from, to })}`
  return <section aria-label={t(lang, "tasks.reportTab")} className="grid gap-4">
    <form method="get" action="/tasks" className="flex flex-wrap items-end gap-3">
      <input type="hidden" name="view" value="report" />
      <div className="grid gap-1.5">
        <Label htmlFor="report-from" className="text-xs">{t(lang, "tasks.reportFrom")}</Label>
        <DateField id="report-from" name="from" defaultValue={from} required />
      </div>
      <div className="grid gap-1.5">
        <Label htmlFor="report-to" className="text-xs">{t(lang, "tasks.reportTo")}</Label>
        <DateField id="report-to" name="to" defaultValue={to} required />
      </div>
      <Button type="submit" variant="outline" size="sm">{t(lang, "tasks.reportShow")}</Button>
      <Button asChild variant="ghost" size="sm"><a href={csvHref} download>{t(lang, "tasks.reportCSV")}</a></Button>
    </form>

    {note ? <p role="alert" className="rounded-md border border-destructive/40 p-3 text-sm text-destructive">{note}</p>
      : rows.length === 0 ? <Card><CardContent className="py-7 text-center"><p className="font-medium">{t(lang, "tasks.reportEmpty")}</p></CardContent></Card>
      : <Card className="overflow-hidden">
        <CardContent className="overflow-x-auto p-0">
          <table className="w-full min-w-[44rem] border-collapse text-sm">
            <thead><tr className="border-b text-left text-muted-foreground">
              <th scope="col" className="px-3 py-2 font-medium">{t(lang, "tasks.reportTask")}</th>
              <th scope="col" className="px-3 py-2 font-medium">{t(lang, "tasks.project")}</th>
              <th scope="col" className="px-3 py-2 font-medium">{t(lang, "tasks.assignee")}</th>
              <th scope="col" className="px-3 py-2 font-medium">{t(lang, "tasks.status")}</th>
              <th scope="col" className="px-3 py-2 text-right font-medium">{t(lang, "tasks.estimate")}</th>
              <th scope="col" className="px-3 py-2 text-right font-medium">{t(lang, "tasks.tracked")}</th>
              <th scope="col" className="px-3 py-2 font-medium">{t(lang, "tasks.due")}</th>
            </tr></thead>
            <tbody>
              {rows.map((row, index) => <tr key={index} className="border-b last:border-0">
                <td className="px-3 py-2 font-medium [overflow-wrap:anywhere]">{row.Title}{row.Archived && <span className="ms-2 text-xs font-normal text-muted-foreground">{t(lang, "tasks.reportArchived")}</span>}</td>
                <td className="px-3 py-2">{row.ProjectName}</td>
                <td className="px-3 py-2">{row.AssigneeGone ? <span className="text-destructive">{t(lang, "tasks.assigneeGone")}</span> : row.AssigneeName || t(lang, "tasks.nobody")}</td>
                <td className="px-3 py-2">{t(lang, `tasks.${row.Status}`)}</td>
                <td className="px-3 py-2 text-right tabular-nums">{row.EstimateLabel}</td>
                <td className="px-3 py-2 text-right tabular-nums">{row.TrackedLabel}</td>
                <td className="px-3 py-2 whitespace-nowrap">{row.DueLabel}</td>
              </tr>)}
            </tbody>
            <tfoot><tr className="font-medium">
              <th scope="row" colSpan={5} className="px-3 py-2 text-left">{t(lang, "tasks.reportTotal")}</th>
              <td className="px-3 py-2 text-right tabular-nums">{total}</td>
              <td />
            </tr></tfoot>
          </table>
        </CardContent>
      </Card>}

    <p className="text-sm text-muted-foreground">{t(lang, "tasks.reportHint")}</p>
  </section>
}
