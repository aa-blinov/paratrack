import * as React from "react"
import { ChevronDown, FolderKanban, Plus } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { translate as t } from "@/i18n"
import type { ProjectListData, ProjectListRow } from "@/dashboard/types"
import { activityColor } from "@/lib/activity-color"
import { ColorMark } from "@/components/color-mark"

// Список проектов — таблица, а не плитки. Плитка на четверть экрана
// показывала четыре числа и не давала ни сравнить проекты между собой, ни
// сказать, кому проект выставляется. Строка читается слева направо, а
// активности — то, ради чего плитка показывала голое число «2» — открываются
// по требованию и не занимают место, пока их не спросили.
const COLUMN_COUNT = 5

function ActivitiesCell({ lang, project }: { lang: string; project: ProjectListRow }) {
  if (!project.ActivityNames?.length) {
    return <span className="text-sm text-muted-foreground">{t(lang, "projects.noActivities")}</span>
  }
  // Отступ равен префиксу первой ячейки (шеврон + цветной значок + промежутки),
  // чтобы активности встали ровно под названием проекта, а не левее него.
  return <ul className="flex flex-wrap gap-x-4 gap-y-1.5 ps-[3.375rem]">
    {project.ActivityNames.map((activity, index) => <li key={`${activity.Name}:${index}`} className="flex min-w-0 items-center gap-1.5 text-sm">
      <ColorMark color={activityColor(activity.Color)} />
      <span className="[overflow-wrap:anywhere]">{activity.Name}</span>
      {activity.Hidden && <Badge variant="secondary" className="h-5">{t(lang, "projects.archivedBadge")}</Badge>}
    </li>)}
  </ul>
}

function ProjectRow({ data, project, open, onToggle }: {
  data: ProjectListData
  project: ProjectListRow
  open: boolean
  onToggle: () => void
}) {
  const lang = data.Lang
  const href = `/projects/${encodeURIComponent(project.Slug)}`
  const panelID = `project-activities-${project.ID}`

  return <>
    <TableRow data-list-item="2" className={open ? "bg-muted/40" : undefined}>
      <TableCell className="align-middle">
        <div className="flex min-w-0 items-center gap-2">
          <Button
            type="button"
            variant="ghost"
            size="icon"
            className="size-7 shrink-0"
            aria-expanded={open}
            aria-controls={panelID}
            aria-label={t(lang, "projects.toggleActivities", project.Name)}
            onClick={onToggle}
          >
            <ChevronDown aria-hidden="true" className={`size-4 transition-transform ${open ? "rotate-180" : ""}`} />
          </Button>
          <ColorMark color={project.Color} />
          <a href={href} title={project.Name} className="min-w-0 truncate font-medium underline-offset-4 hover:underline">{project.Name}</a>
          {project.Archived && <Badge variant="secondary" className="shrink-0">{t(lang, "projects.archivedBadge")}</Badge>}
        </div>
        {/* На телефоне отдельных колонок для клиента и чисел нет: 375 пикселей
            не оставляют названию места, а «Або…» не читается. Клиент и метрики
            идут под названием, таблица остаётся таблицей. */}
        {project.ClientName && <p className="truncate ps-6 text-xs text-muted-foreground sm:hidden">{project.ClientName}</p>}
        <p className="flex flex-wrap gap-x-4 ps-6 text-xs tabular-nums text-muted-foreground sm:hidden">
          <span>{t(lang, "dash.today")} <span className="font-mono text-foreground">{project.TodayLabel}</span></span>
          <span>{t(lang, "projects.last30")} <span className="font-mono text-foreground">{project.MonthLabel}</span></span>
        </p>
      </TableCell>
      <TableCell className="hidden align-middle text-sm text-muted-foreground sm:table-cell">
        <span>{project.ClientName || "—"}</span>
      </TableCell>
      <TableCell className="hidden align-middle text-right font-mono tabular-nums md:table-cell">{project.Activities}</TableCell>
      <TableCell className="hidden whitespace-nowrap align-middle text-right font-mono tabular-nums sm:table-cell">{project.TodayLabel}</TableCell>
      <TableCell className="hidden whitespace-nowrap align-middle text-right font-mono tabular-nums sm:table-cell">{project.MonthLabel}</TableCell>
    </TableRow>
    {open && <TableRow id={panelID} className="hover:bg-transparent">
      <TableCell colSpan={COLUMN_COUNT} className="bg-muted/40">
        <ActivitiesCell lang={lang} project={project} />
      </TableCell>
    </TableRow>}
  </>
}

export function ProjectList({ data }: { data: ProjectListData }) {
  const lang = data.Lang
  const archivedURL = data.ShowArchived ? "/projects" : "/projects?archived=1"
  // Открытые строки живут в состоянии списка, а не в URL: раскрытие — это
  // «показать подробности», а не место, куда стоит отправлять ссылку.
  const [openRows, setOpenRows] = React.useState<number[]>([])
  const toggleRow = (id: number) => setOpenRows(current => current.includes(id) ? current.filter(x => x !== id) : [...current, id])

  return (
    <main className="space-y-6">
      <header className="flex flex-wrap items-end justify-between gap-4">
        <h1 className="text-2xl font-semibold tracking-tight">{t(lang, "nav.projects")}</h1>
        <div className="flex flex-wrap items-center gap-2">
          <Button asChild variant="outline" size="sm">
            <a href={archivedURL}>{t(lang, data.ShowArchived ? "projects.hideArchived" : "projects.showArchived")}</a>
          </Button>
          {data.CanManage && (
            <Button asChild size="sm">
              <a href="/projects/new"><Plus aria-hidden="true" />{t(lang, "projects.new")}</a>
            </Button>
          )}
        </div>
      </header>

      {data.Flash && <p className={data.FlashOK ? "text-sm text-foreground" : "text-sm text-destructive"} role={data.FlashOK ? "status" : "alert"}>{data.Flash}</p>}

      {data.Projects?.length ? (
        // Подложка как у карточки: без неё таблица лежит на фоне страницы, и
        // в тёмной теме строки теряются.
        <div className="min-w-0 overflow-hidden rounded-lg border bg-card">
          <Table>
            {/* На телефоне от таблицы остаётся одна полоса, и над ней висит одинокая
          шапка «Проекты» — то же слово, что стоит заголовком страницы. */}
            <TableHeader className="hidden sm:table-header-group">
              <TableRow>
                <TableHead>{t(lang, "nav.projects")}</TableHead>
                <TableHead className="hidden whitespace-nowrap align-middle md:table-cell">{t(lang, "inv.client")}</TableHead>
                <TableHead className="hidden text-right md:table-cell">{t(lang, "projects.activities")}</TableHead>
                <TableHead className="hidden text-right sm:table-cell">{t(lang, "dash.today")}</TableHead>
                <TableHead className="hidden text-right sm:table-cell">{t(lang, "projects.last30")}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {data.Projects.map((project) => <ProjectRow
                key={project.ID}
                data={data}
                project={project}
                open={openRows.includes(project.ID)}
                onToggle={() => toggleRow(project.ID)}
              />)}
            </TableBody>
          </Table>
        </div>
      ) : (
        <Card>
          <CardContent className="flex flex-col items-center gap-3 py-12 text-center">
            <FolderKanban aria-hidden="true" className="size-5 text-muted-foreground" />
            <p className="text-sm text-muted-foreground">{t(lang, data.ShowArchived ? "projects.emptyArchived" : "projects.empty")}</p>
            {data.CanManage
              ? <Button asChild size="sm"><a href="/projects/new"><Plus aria-hidden="true" />{t(lang, "projects.createNew")}</a></Button>
              : <p className="max-w-md text-sm text-muted-foreground">{t(lang, "projects.emptyMember")}</p>}
          </CardContent>
        </Card>
      )}
    </main>
  )
}