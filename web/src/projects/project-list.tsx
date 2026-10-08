import { FolderKanban, Plus } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { translate as t } from "@/i18n"
import type { ProjectListData } from "@/dashboard/types"

export function ProjectList({ data }: { data: ProjectListData }) {
  const archivedURL = data.ShowArchived ? "/projects" : "/projects?archived=1"

  return (
    <main className="space-y-6">
      <header className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">{t(data.Lang, "nav.projects")}</h1>
          <p className="mt-1 text-sm text-muted-foreground">{t(data.Lang, "projects.blurb")}</p>
        </div>
        <div className="flex items-center gap-2">
          <Button asChild variant="outline" size="sm">
            <a href={archivedURL}>{t(data.Lang, data.ShowArchived ? "projects.hideArchived" : "projects.showArchived")}</a>
          </Button>
          {data.CanManage && (
            <Button asChild size="sm">
              <a href="/projects/new"><Plus aria-hidden="true" />{t(data.Lang, "projects.new")}</a>
            </Button>
          )}
        </div>
      </header>

      {data.Flash && <p className={data.FlashOK ? "text-sm text-foreground" : "text-sm text-destructive"} role={data.FlashOK ? "status" : "alert"}>{data.Flash}</p>}

      {data.Projects?.length ? (
        <section aria-label={t(data.Lang, "nav.projects")} className="grid min-w-0 gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {data.Projects.map((project) => (
            <a key={project.ID} href={`/projects/${encodeURIComponent(project.Slug)}`} className="group block min-w-0 rounded-lg focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
              <Card className="h-full transition-colors group-hover:border-foreground/30">
                <CardContent className="space-y-4 p-5">
                  <div className="flex min-w-0 items-center gap-2">
                    <span aria-hidden="true" className="size-3 shrink-0 rounded-full" style={{ backgroundColor: project.Color }} />
                    <h2 className="min-w-0 truncate text-base font-medium" title={project.Name}>{project.Name}</h2>
                    {project.Archived && <Badge variant="secondary">{t(data.Lang, "projects.archivedBadge")}</Badge>}
                  </div>
                  <dl className="grid grid-cols-3 gap-2 text-xs text-muted-foreground">
                    <div><dt>{t(data.Lang, "projects.activities")}</dt><dd className="mt-1 text-base font-medium tabular-nums text-foreground">{project.Activities}</dd></div>
                    <div><dt>{t(data.Lang, "dash.today")}</dt><dd className="mt-1 text-base font-medium tabular-nums text-foreground">{project.TodayLabel}</dd></div>
                    <div><dt>{t(data.Lang, "projects.last30")}</dt><dd className="mt-1 text-base font-medium tabular-nums text-foreground">{project.MonthLabel}</dd></div>
                  </dl>
                </CardContent>
              </Card>
            </a>
          ))}
        </section>
      ) : (
        <Card>
          <CardContent className="flex flex-col items-center gap-3 py-12 text-center">
            <FolderKanban aria-hidden="true" className="size-5 text-muted-foreground" />
            <p className="text-sm text-muted-foreground">{t(data.Lang, data.ShowArchived ? "projects.emptyArchived" : "projects.empty")}</p>
            {data.CanManage
              ? <Button asChild size="sm"><a href="/projects/new"><Plus aria-hidden="true" />{t(data.Lang, "projects.createNew")}</a></Button>
              : <p className="max-w-md text-sm text-muted-foreground">{t(data.Lang, "projects.emptyMember")}</p>}
          </CardContent>
        </Card>
      )}
    </main>
  )
}
