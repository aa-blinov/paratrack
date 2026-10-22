import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import type { StatsData } from "@/dashboard/types"
import { translate as t } from "@/i18n"

type TeamWeek = NonNullable<StatsData["TeamWeek"]>

// The team's week for a manager (PLAN-04): who tracked what on each day. A day that has
// passed, or is today, under the daily norm carries the word as well as the colour.
export function TeamWeekCard({ week, lang }: { week: TeamWeek; lang: string }) {
  return <Card>
    <CardHeader><CardTitle>{t(lang, "stats.teamWeekTitle")}</CardTitle><CardDescription>{t(lang, "stats.teamWeekBlurb", week.Norm)}</CardDescription></CardHeader>
    <CardContent className="overflow-x-auto p-0">
      <table className="w-full min-w-[44rem] border-collapse text-sm">
        <thead><tr className="border-b text-muted-foreground">
          <th scope="col" className="px-3 py-2 text-left font-medium">{t(lang, "stats.person")}</th>
          {week.Days.map(day => <th key={day.Date} scope="col" className={`px-2 py-2 text-right font-medium ${day.Today ? "text-foreground" : ""}`} aria-current={day.Today ? "date" : undefined}>
            <span className="block">{day.Label}</span><span className="block text-xs font-normal">{day.Date}</span>
          </th>)}
          <th scope="col" className="px-3 py-2 text-right font-medium">{t(lang, "stats.teamTotal")}</th>
        </tr></thead>
        <tbody>
          {week.Rows.map(row => <tr key={row.Name} className="border-b last:border-0">
            <th scope="row" className="px-3 py-2 text-left font-medium [overflow-wrap:anywhere]">{row.Name}</th>
            {row.Cells.map((cell, index) => <td key={index} data-below={cell.Below || undefined} title={cell.Below ? t(lang, "stats.teamBelowNorm", week.Norm) : undefined}
              className={`px-2 py-2 text-right tabular-nums ${cell.Below ? "text-destructive" : ""}`}>
              {cell.Label}
              {cell.Below && <span className="block text-xs font-normal">{t(lang, "stats.teamBelowShort", week.Norm)}</span>}
              {cell.Mark && <span className="block text-xs font-normal" title={cell.Note || undefined}>{cell.Mark}</span>}
            </td>)}
            <td className="px-3 py-2 text-right font-medium tabular-nums">{row.Total}</td>
          </tr>)}
        </tbody>
      </table>
    </CardContent>
  </Card>
}
