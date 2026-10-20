import * as React from "react"
import { Archive, Plus, RotateCcw } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { DateField } from "@/components/date-field"
import { requestConfirmation } from "@/components/confirmation-dialog"
import { Flash } from "@/components/flash"
import { translate as t } from "@/i18n"
import type { TaskItem, TasksData } from "@/dashboard/types"

const statuses = ["todo", "doing", "done"] as const
const noPerson = "0"

async function post(url: string, csrf: string, fields: Record<string, string>): Promise<void> {
  const response = await fetch(url, {
    method: "POST",
    credentials: "same-origin",
    headers: {
      Accept: "application/json",
      "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8",
      "X-CSRF-Token": csrf,
    },
    body: new URLSearchParams({ csrf_token: csrf, ...fields }),
  })
  if (!response.ok) {
    const body = (await response.json().catch(() => ({}))) as { error?: string }
    throw new Error(body.error || response.statusText)
  }
}

export function TasksPage({ data }: { data: TasksData }) {
  const lang = data.Lang
  const [tasks, setTasks] = React.useState<TaskItem[]>(data.Tasks)
  const [error, setError] = React.useState("")
  const [dragging, setDragging] = React.useState<number | null>(null)
  const [title, setTitle] = React.useState("")
  const [project, setProject] = React.useState(String(data.ProjectID || data.Projects[0]?.ID || ""))
  const [assignee, setAssignee] = React.useState(noPerson)
  const [due, setDue] = React.useState("")
  const [dueBroken, setDueBroken] = React.useState(false)
  const [adding, setAdding] = React.useState(false)
  const [estimate, setEstimate] = React.useState("")
  const [creating, setCreating] = React.useState(false)
  const [importProject, setImportProject] = React.useState(String(data.ProjectID || data.Projects[0]?.ID || ""))
  const archivedView = data.View === "archive"

  async function run(action: () => Promise<void>) {
    setError("")
    try {
      await action()
      const response = await fetch(`/api/tasks?project=${data.ProjectID}${archivedView ? "&archived=1" : ""}`, { credentials: "same-origin", headers: { Accept: "application/json" } })
      const body = (await response.json()) as { tasks: TaskItem[] }
      setTasks(body.tasks)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t(lang, "err.generic"))
    }
  }

  function changeStatus(task: TaskItem, status: string) {
    if (task.Status === status) return
    void run(() => post(`/api/tasks/${task.ID}/status`, data.CSRFToken, { status }))
  }

  function changeAssignee(task: TaskItem, value: string) {
    void run(() => post(`/api/tasks/${task.ID}`, data.CSRFToken, { assignee_id: value === noPerson ? "0" : value }))
  }

  async function archive(task: TaskItem) {
    if (!await requestConfirmation(t(lang, "tasks.archiveConfirm"), { action: t(lang, "tasks.archive") })) return
    void run(() => post(`/api/tasks/${task.ID}/archive`, data.CSRFToken, {}))
  }

  function restore(task: TaskItem) {
    void run(() => post(`/api/tasks/${task.ID}/restore`, data.CSRFToken, {}))
  }

  async function create(event: React.FormEvent) {
    event.preventDefault()
    if (dueBroken) {
      setError(t(lang, "tasks.dueInvalid"))
      return
    }
    setCreating(true)
    setError("")
    try {
      const response = await fetch("/api/tasks", {
        method: "POST",
        credentials: "same-origin",
        headers: { Accept: "application/json", "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8", "X-CSRF-Token": data.CSRFToken },
        body: new URLSearchParams({
          csrf_token: data.CSRFToken, title, project_id: project,
          assignee_id: assignee === noPerson ? "0" : assignee, due, estimate_minutes: estimate || "0",
        }),
      })
      const body = (await response.json()) as { id?: number; error?: string }
      if (!response.ok || !body.id) throw new Error(body.error || response.statusText)
      window.location.assign(`/tasks/${body.id}`)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t(lang, "err.generic"))
      setCreating(false)
    }
  }

  const byStatus = (status: string) => tasks.filter(task => task.Status === status)
  const viewHref = (view: string) => `/tasks?view=${view}${data.ProjectID ? `&project=${data.ProjectID}` : ""}`

  return <main className="mx-auto w-full max-w-6xl space-y-4">
    <header className="flex flex-wrap items-end justify-between gap-4">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">{t(lang, "tasks.title")}</h1>
        <p className="mt-1 text-sm text-muted-foreground">{t(lang, "tasks.blurb")}</p>
      </div>
      <nav className="flex items-center gap-1" aria-label={t(lang, "tasks.title")}>
        <Button asChild variant={data.View === "table" ? "secondary" : "ghost"} size="sm"><a href={viewHref("table")} aria-current={data.View === "table" ? "page" : undefined}>{t(lang, "tasks.table")}</a></Button>
        <Button asChild variant={data.View === "board" ? "secondary" : "ghost"} size="sm"><a href={viewHref("board")} aria-current={data.View === "board" ? "page" : undefined}>{t(lang, "tasks.board")}</a></Button>
        <Button asChild variant={archivedView ? "secondary" : "ghost"} size="sm"><a href={viewHref("archive")} aria-current={archivedView ? "page" : undefined}>{t(lang, "tasks.archiveTab")}</a></Button>
      </nav>
    </header>

    <Flash message={data.Flash} ok={data.FlashOK} />
    {error && <p role="alert" className="rounded-md border border-destructive/40 p-3 text-sm text-destructive">{error}</p>}

    <div className="flex flex-wrap items-center gap-2">
      <Label htmlFor="tasks-project" className="text-sm text-muted-foreground">{t(lang, "tasks.project")}</Label>
      <Select value={String(data.ProjectID)} onValueChange={value => window.location.assign(`/tasks?view=${data.View}&project=${value}`)}>
        <SelectTrigger id="tasks-project" className="w-full bg-background text-foreground sm:w-64"><SelectValue /></SelectTrigger>
        <SelectContent>
          <SelectItem value="0">{t(lang, "tasks.allProjects")}</SelectItem>
          {data.Projects.map(item => <SelectItem key={item.ID} value={String(item.ID)}>{item.Name}</SelectItem>)}
        </SelectContent>
      </Select>
    </div>

    {!archivedView && <>
    <Button type="button" variant="outline" className="sm:hidden" aria-expanded={adding} aria-controls="new-task-form" onClick={() => setAdding(open => !open)}><Plus aria-hidden="true" />{t(lang, "tasks.new")}</Button>
    <form id="new-task-form" onSubmit={event => void create(event)} className={`${adding ? "grid" : "hidden"} gap-3 sm:grid sm:grid-cols-2 lg:grid-cols-[minmax(0,1.8fr)_minmax(0,1.5fr)_minmax(0,1.2fr)_minmax(0,1.3fr)_minmax(0,0.8fr)_auto] lg:items-end`} aria-label={t(lang, "tasks.new")}>
      <div className="flex min-w-0 flex-col gap-1.5">
        <Label htmlFor="new-task-title" className="text-xs">{t(lang, "tasks.new")}</Label>
        <Input id="new-task-title" value={title} onChange={event => setTitle(event.target.value)} placeholder={t(lang, "tasks.titlePh")} required maxLength={200} />
      </div>
      <div className="flex min-w-0 flex-col gap-1.5">
        <Label htmlFor="new-task-project" className="text-xs">{t(lang, "tasks.project")}</Label>
        <Select value={project} onValueChange={setProject}>
          <SelectTrigger id="new-task-project" className="w-full"><SelectValue className="min-w-0 truncate" /></SelectTrigger>
          <SelectContent>{data.Projects.map(item => <SelectItem key={item.ID} value={String(item.ID)}>{item.Name}</SelectItem>)}</SelectContent>
        </Select>
      </div>
      <div className="flex min-w-0 flex-col gap-1.5">
        <Label htmlFor="new-task-assignee" className="text-xs">{t(lang, "tasks.assignee")}</Label>
        <Select value={assignee} onValueChange={setAssignee}>
          <SelectTrigger id="new-task-assignee" className="w-full"><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value={noPerson}>{t(lang, "tasks.nobody")}</SelectItem>
            {data.People.map(person => <SelectItem key={person.ID} value={String(person.ID)}>{person.Name}</SelectItem>)}
          </SelectContent>
        </Select>
      </div>
      <div className="flex min-w-0 flex-col gap-1.5">
        <Label htmlFor="new-task-due" className="text-xs">{t(lang, "tasks.due")}</Label>
        <DateField id="new-task-due" name="due" defaultValue={due} onValueChange={setDue} onBrokenChange={setDueBroken} />
      </div>
      <div className="flex min-w-0 flex-col gap-1.5">
        <Label htmlFor="new-task-estimate" className="text-xs">{t(lang, "tasks.estimate")}</Label>
        <Input id="new-task-estimate" type="number" min={0} step={30} inputMode="numeric" value={estimate} onChange={event => setEstimate(event.target.value)} placeholder="480" />
      </div>
      <Button type="submit" className="self-end" disabled={creating || !project}><Plus aria-hidden="true" />{t(lang, "tasks.new")}</Button>
    </form>
    </>}

    {tasks.length === 0 ? <Card><CardContent className="py-7 text-center">
      <p className="font-medium">{archivedView ? t(lang, "tasks.archiveEmpty") : t(lang, "tasks.empty")}</p>
      <p className="mx-auto mt-1 text-sm text-muted-foreground">{archivedView ? t(lang, "tasks.archiveEmptyHint") : t(lang, "tasks.emptyHint")}</p>
    </CardContent></Card> : data.View === "board" ? (
      <div className="grid gap-4 md:grid-cols-3">
        {statuses.map(status => <section key={status} aria-label={t(lang, `tasks.${status}`)} className="grid content-start gap-2 rounded-lg bg-muted/40 p-3" onDragOver={event => event.preventDefault()} onDrop={() => { const task = tasks.find(item => item.ID === dragging); if (task) changeStatus(task, status); setDragging(null) }}>
          <h2 className="flex items-center gap-2 px-1 text-xs font-semibold uppercase tracking-wider text-muted-foreground">{t(lang, `tasks.${status}`)}<span className="tabular-nums">{byStatus(status).length}</span></h2>
          {byStatus(status).map(task => <article key={task.ID} draggable onDragStart={() => setDragging(task.ID)} onDragEnd={() => setDragging(null)} className="grid gap-2 rounded-lg border bg-card p-3 text-sm shadow-sm">
            <a href={`/tasks/${task.ID}`} className="font-medium underline-offset-4 hover:underline">{task.Title}</a>
            <span className="flex items-center gap-2 text-xs text-muted-foreground"><span aria-hidden="true" className="size-2 shrink-0 rounded-full" style={{ backgroundColor: task.ProjectColor }} />{task.ProjectName}</span>
            <span className="flex flex-wrap gap-x-3 text-xs text-muted-foreground">
              {task.AssigneeName && <span>{task.AssigneeName}</span>}
              {task.DueLabel && <span className={task.Overdue ? "text-destructive" : undefined}>{task.DueLabel}</span>}
              {task.Overdue && <span className="font-medium text-destructive">{t(lang, "tasks.overdue")}</span>}
              {task.TrackedLabel && <span className="font-mono">{task.TrackedLabel}</span>}
            </span>
            <Select value={task.Status} onValueChange={value => changeStatus(task, value)}>
              <SelectTrigger aria-label={`${t(lang, "tasks.status")}: ${task.Title}`} className="h-8 w-full text-xs"><SelectValue /></SelectTrigger>
              <SelectContent>{statuses.map(option => <SelectItem key={option} value={option}>{t(lang, `tasks.${option}`)}</SelectItem>)}</SelectContent>
            </Select>
          </article>)}
        </section>)}
      </div>
    ) : archivedView ? (
      <>
        <Card className="hidden overflow-hidden md:block">
          <CardContent className="overflow-x-auto p-0">
            <table className="w-full min-w-[40rem] border-collapse text-sm">
              <thead><tr className="border-b text-left text-muted-foreground">
                <th scope="col" className="px-3 py-2 font-medium">{t(lang, "tasks.title")}</th>
                <th scope="col" className="px-3 py-2 font-medium">{t(lang, "tasks.project")}</th>
                <th scope="col" className="px-3 py-2 font-medium">{t(lang, "tasks.status")}</th>
                <th scope="col" className="px-3 py-2 font-medium">{t(lang, "tasks.assignee")}</th>
                <th scope="col" className="px-3 py-2 text-right font-medium">{t(lang, "tasks.tracked")}</th>
                <th scope="col" className="px-3 py-2"><span className="sr-only">{t(lang, "tasks.restore")}</span></th>
              </tr></thead>
              <tbody>{tasks.map(task => <tr key={task.ID} className="border-b last:border-0">
                <td className="px-3 py-2"><a href={`/tasks/${task.ID}`} className="font-medium underline-offset-4 hover:underline">{task.Title}</a></td>
                <td className="px-3 py-2">{task.ProjectName}</td>
                <td className="px-3 py-2">{t(lang, `tasks.${task.Status}`)}</td>
                <td className="px-3 py-2">{task.AssigneeName || t(lang, "tasks.nobody")}</td>
                <td className="px-3 py-2 text-right font-mono tabular-nums">{task.TrackedLabel}</td>
                <td className="px-3 py-2 text-right">{task.CanArchive && <Button type="button" variant="outline" size="sm" onClick={() => restore(task)}><RotateCcw aria-hidden="true" />{t(lang, "tasks.restore")}</Button>}</td>
              </tr>)}</tbody>
            </table>
          </CardContent>
        </Card>
        <div className="grid gap-2 md:hidden">
          {tasks.map(task => <Card key={task.ID}><CardContent className="grid gap-2 p-3 text-sm">
            <a href={`/tasks/${task.ID}`} className="font-medium underline-offset-4 hover:underline">{task.Title}</a>
            <span className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
              <span className="flex items-center gap-2"><span aria-hidden="true" className="size-2 shrink-0 rounded-full" style={{ backgroundColor: task.ProjectColor }} />{task.ProjectName}</span>
              <span>{t(lang, `tasks.${task.Status}`)}</span>
              {task.TrackedLabel && <span className="font-mono">{task.TrackedLabel}</span>}
            </span>
            {task.CanArchive && <Button type="button" variant="outline" size="sm" className="w-fit" onClick={() => restore(task)}><RotateCcw aria-hidden="true" />{t(lang, "tasks.restore")}</Button>}
          </CardContent></Card>)}
        </div>
      </>
    ) : <>
      <Card className="hidden overflow-hidden md:block">
        <CardContent className="overflow-x-auto p-0">
          <table className="w-full min-w-[48rem] border-collapse text-sm">
            <thead><tr className="border-b text-left text-muted-foreground">
              <th scope="col" className="px-3 py-2 font-medium">{t(lang, "tasks.title")}</th>
              <th scope="col" className="px-3 py-2 font-medium">{t(lang, "tasks.project")}</th>
              <th scope="col" className="px-3 py-2 font-medium">{t(lang, "tasks.status")}</th>
              <th scope="col" className="px-3 py-2 font-medium">{t(lang, "tasks.assignee")}</th>
              <th scope="col" className="px-3 py-2 font-medium">{t(lang, "tasks.due")}</th>
              <th scope="col" className="px-3 py-2 text-right font-medium">{t(lang, "tasks.estimate")}</th>
              <th scope="col" className="px-3 py-2 text-right font-medium">{t(lang, "tasks.tracked")}</th>
              <th scope="col" className="px-3 py-2"><span className="sr-only">{t(lang, "tasks.archive")}</span></th>
            </tr></thead>
            <tbody>{tasks.map(task => <tr key={task.ID} className="border-b last:border-0">
              <td className="px-3 py-2"><a href={`/tasks/${task.ID}`} className="font-medium underline-offset-4 hover:underline">{task.Title}</a></td>
              <td className="max-w-[11rem] px-3 py-2"><span className="flex min-w-0 items-center gap-2"><span aria-hidden="true" className="size-2 shrink-0 rounded-full" style={{ backgroundColor: task.ProjectColor }} /><span className="truncate">{task.ProjectName}</span></span></td>
              <td className="px-3 py-2">
                <Select value={task.Status} onValueChange={value => changeStatus(task, value)}>
                  <SelectTrigger aria-label={`${t(lang, "tasks.status")}: ${task.Title}`} className="h-8 w-32 text-xs"><SelectValue /></SelectTrigger>
                  <SelectContent>{statuses.map(option => <SelectItem key={option} value={option}>{t(lang, `tasks.${option}`)}</SelectItem>)}</SelectContent>
                </Select>
              </td>
              <td className="px-3 py-2">
                <Select value={task.AssigneeID ? String(task.AssigneeID) : noPerson} onValueChange={value => changeAssignee(task, value)}>
                  <SelectTrigger aria-label={`${t(lang, "tasks.assignee")}: ${task.Title}`} className="h-8 w-36 text-xs"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem value={noPerson}>{t(lang, "tasks.nobody")}</SelectItem>
                    {data.People.map(person => <SelectItem key={person.ID} value={String(person.ID)}>{person.Name}</SelectItem>)}
                  </SelectContent>
                </Select>
              </td>
              <td className={`px-3 py-2 ${task.Overdue ? "text-destructive" : ""}`}>{task.DueLabel || "—"}{task.Overdue && <span className="ml-2 font-medium">{t(lang, "tasks.overdue")}</span>}</td>
              <td className="px-3 py-2 text-right font-mono tabular-nums">{task.EstimateLabel || "—"}</td>
              <td className="px-3 py-2 text-right font-mono tabular-nums">{task.TrackedLabel}</td>
              <td className="px-3 py-2 text-right">{task.CanArchive && <Button type="button" variant="ghost" size="icon" aria-label={t(lang, "tasks.archive")} title={t(lang, "tasks.archive")} onClick={() => void archive(task)}><Archive aria-hidden="true" /></Button>}</td>
            </tr>)}</tbody>
          </table>
        </CardContent>
      </Card>
      <div className="grid gap-2 md:hidden">
        {tasks.map(task => <Card key={task.ID}><CardContent className="grid gap-2 p-3 text-sm">
          <a href={`/tasks/${task.ID}`} className="font-medium underline-offset-4 hover:underline">{task.Title}</a>
          <span className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
            <span className="flex items-center gap-2"><span aria-hidden="true" className="size-2 shrink-0 rounded-full" style={{ backgroundColor: task.ProjectColor }} />{task.ProjectName}</span>
            {task.DueLabel && <span className={task.Overdue ? "text-destructive" : undefined}>{task.DueLabel}</span>}
            {task.Overdue && <span className="font-medium text-destructive">{t(lang, "tasks.overdue")}</span>}
            {task.TrackedLabel && <span className="font-mono">{task.TrackedLabel}</span>}
          </span>
          <Select value={task.Status} onValueChange={value => changeStatus(task, value)}>
            <SelectTrigger aria-label={`${t(lang, "tasks.status")}: ${task.Title}`} className="h-9 w-full text-xs"><SelectValue /></SelectTrigger>
            <SelectContent>{statuses.map(option => <SelectItem key={option} value={option}>{t(lang, `tasks.${option}`)}</SelectItem>)}</SelectContent>
          </Select>
          <Select value={task.AssigneeID ? String(task.AssigneeID) : noPerson} onValueChange={value => changeAssignee(task, value)}>
            <SelectTrigger aria-label={`${t(lang, "tasks.assignee")}: ${task.Title}`} className="h-9 w-full text-xs"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value={noPerson}>{t(lang, "tasks.nobody")}</SelectItem>
              {data.People.map(person => <SelectItem key={person.ID} value={String(person.ID)}>{person.Name}</SelectItem>)}
            </SelectContent>
          </Select>
        </CardContent></Card>)}
      </div>
    </>}

    {data.CanImport && !archivedView && <Card><CardContent className="grid gap-3 p-4">
      <div className="grid gap-1">
        <h2 className="font-medium">{t(lang, "tasks.githubTitle")}</h2>
        <p className="text-sm text-muted-foreground">{t(lang, "tasks.githubBlurb")}</p>
      </div>
      {data.Projects.length === 0 ? <p className="text-sm text-muted-foreground">{t(lang, "tasks.githubNoProject")}</p> : <form method="post" action="/api/tasks/import/github" className="grid gap-3 lg:grid-cols-[minmax(0,1fr)_minmax(0,2fr)_minmax(0,1.2fr)_auto] lg:items-end" aria-label={t(lang, "tasks.githubTitle")}>
        <input type="hidden" name="csrf_token" value={data.CSRFToken} />
        <input type="hidden" name="project_id" value={importProject} />
        <div className="flex min-w-0 flex-col gap-1.5">
          <Label htmlFor="github-project" className="text-xs">{t(lang, "tasks.project")}</Label>
          <Select value={importProject} onValueChange={setImportProject}>
            <SelectTrigger id="github-project" className="w-full"><SelectValue className="min-w-0 truncate" /></SelectTrigger>
            <SelectContent>{data.Projects.map(item => <SelectItem key={item.ID} value={String(item.ID)}>{item.Name}</SelectItem>)}</SelectContent>
          </Select>
        </div>
        <div className="flex min-w-0 flex-col gap-1.5">
          <Label htmlFor="github-url" className="text-xs">{t(lang, "tasks.githubURL")}</Label>
          <Input id="github-url" name="project_url" type="url" required placeholder="https://github.com/orgs/acme/projects/3" />
        </div>
        <div className="flex min-w-0 flex-col gap-1.5">
          <Label htmlFor="github-token" className="text-xs">{t(lang, "tasks.githubToken")}</Label>
          <Input id="github-token" name="token" type="password" autoComplete="off" required />
        </div>
        <Button type="submit" disabled={!importProject}>{t(lang, "tasks.githubRun")}</Button>
        <p className="text-xs text-muted-foreground lg:col-span-4">{t(lang, "tasks.githubTokenHint")}</p>
      </form>}
    </CardContent></Card>}

    {!archivedView && <p className="text-sm text-muted-foreground">{t(lang, "tasks.hint")}</p>}
  </main>
}
