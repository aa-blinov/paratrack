import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import type { StatsData } from "@/dashboard/types"
import { translate as t } from "@/i18n"

type Overview = NonNullable<StatsData["Overview"]>

// The team's week on one screen for a manager (PLAN-05): each project's plan against the time
// tracked on it, and each person's planned load against the time they have for clients. Over
// plan and overloaded or idle are said in words, not only in colour.
export function TeamOverviewCard({ overview, lang }: { overview: Overview; lang: string }) {
  return <Card>
    <CardHeader><CardTitle>{t(lang, "stats.overviewTitle")}</CardTitle><CardDescription>{t(lang, "stats.overviewBlurb", overview.DayNorm)}</CardDescription></CardHeader>
    <CardContent className="grid gap-6 lg:grid-cols-2 lg:items-start">
      <section aria-label={t(lang, "stats.overviewProjects")} className="grid gap-2 overflow-x-auto">
        <h2 className="text-sm font-medium">{t(lang, "stats.overviewProjects")}</h2>
        {overview.Projects.length === 0 ? <p className="text-sm text-muted-foreground">{t(lang, "stats.overviewNoProjects")}</p> : <table className="w-full min-w-[20rem] border-collapse text-sm">
          <thead><tr className="border-b text-left text-muted-foreground">
            <th scope="col" className="px-2 py-2 font-medium">{t(lang, "stats.overviewProject")}</th>
            <th scope="col" className="px-2 py-2 text-right font-medium">{t(lang, "stats.overviewPlan")}</th>
            <th scope="col" className="px-2 py-2 text-right font-medium">{t(lang, "stats.overviewFact")}</th>
          </tr></thead>
          <tbody>{overview.Projects.map(row => <tr key={row.Name} className="border-b last:border-0">
            <th scope="row" className="px-2 py-2 text-left font-medium [overflow-wrap:anywhere]">{row.Name}</th>
            <td className="px-2 py-2 text-right tabular-nums">{row.Plan}</td>
            <td className={`px-2 py-2 text-right tabular-nums ${row.Over ? "text-destructive" : ""}`}>
              {row.Fact}
              {row.Over && <span className="block text-xs font-normal">{t(lang, "stats.overviewOver")}</span>}
            </td>
          </tr>)}</tbody>
        </table>}
      </section>
      <section aria-label={t(lang, "stats.overviewPeople")} className="grid gap-2 overflow-x-auto">
        <h2 className="text-sm font-medium">{t(lang, "stats.overviewPeople")}</h2>
        <table className="w-full min-w-[26rem] border-collapse text-sm">
          <thead><tr className="border-b text-left text-muted-foreground">
            <th scope="col" className="px-2 py-2 font-medium">{t(lang, "stats.overviewName")}</th>
            <th scope="col" className="px-2 py-2 text-right font-medium">{t(lang, "stats.overviewPlanned")}</th>
            <th scope="col" className="px-2 py-2 text-right font-medium">{t(lang, "stats.overviewAvailable")}</th>
            <th scope="col" className="px-2 py-2 text-right font-medium">{t(lang, "stats.overviewTracked")}</th>
            <th scope="col" className="px-2 py-2 text-right font-medium">{t(lang, "stats.overviewLoad")}</th>
          </tr></thead>
          <tbody>{overview.People.map(row => <tr key={row.Name} className="border-b last:border-0">
            <th scope="row" className="px-2 py-2 text-left font-medium [overflow-wrap:anywhere]">{row.Name}</th>
            <td className="px-2 py-2 text-right tabular-nums">{row.Planned}</td>
            <td className="px-2 py-2 text-right tabular-nums">{row.Available}</td>
            <td className="px-2 py-2 text-right tabular-nums">{row.Tracked}</td>
            <td className={`px-2 py-2 text-right ${row.Mark ? "text-destructive" : ""}`}>{row.Mark}</td>
          </tr>)}</tbody>
        </table>
      </section>
    </CardContent>
  </Card>
}
