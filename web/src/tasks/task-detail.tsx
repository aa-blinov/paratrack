import * as React from "react"
import { ArrowLeft, Archive, Plus, RotateCcw } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { DateField } from "@/components/date-field"
import { requestConfirmation } from "@/components/confirmation-dialog"
import { translate as t } from "@/i18n"
import type { TaskActivity, TaskDetailData } from "@/dashboard/types"

const statuses = ["todo", "doing", "done"] as const
const noPerson = "0"

async function post(url: string, csrf: string, fields: Record<string, string>): Promise<unknown> {
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
  const body = (await response.json().catch(() => ({}))) as { error?: string }
  if (!response.ok) throw new Error(body.error || response.statusText)
  return body
}

export function TaskDetailPage({ data }: { data: TaskDetailData }) {
  const lang = data.Lang
  const task = data.Task
  const [title, setTitle] = React.useState(task.Title)
  const [assignee, setAssignee] = React.useState(task.AssigneeID ? String(task.AssigneeID) : noPerson)
  const [due, setDue] = React.useState(task.Due)
  const [dueBroken, setDueBroken] = React.useState(false)
  const [estimate, setEstimate] = React.useState(task.EstimateMin ? String(task.EstimateMin) : "")
  const [activities, setActivities] = React.useState<TaskActivity[]>(task.Activities)
  const [linkable, setLinkable] = React.useState<TaskActivity[]>(data.Linkable)
  const [newName, setNewName] = React.useState("")
  const [linkID, setLinkID] = React.useState("")
  const [status, setStatus] = React.useState(task.Status)
  const [error, setError] = React.useState("")
  const [saved, setSaved] = React.useState(false)

  async function run(action: () => Promise<unknown>) {
    setError("")
    setSaved(false)
    try {
      await action()
      setSaved(true)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t(lang, "err.generic"))
    }
  }

  function save(event: React.FormEvent) {
    event.preventDefault()
    if (dueBroken) {
      setError(t(lang, "tasks.dueInvalid"))
      return
    }
    void run(() => post(`/api/tasks/${task.ID}`, data.CSRFToken, {
      title, assignee_id: assignee === noPerson ? "0" : assignee, due, estimate_minutes: estimate || "0",
    }))
  }

  function changeStatus(value: string) {
    setStatus(value)
    void run(() => post(`/api/tasks/${task.ID}/status`, data.CSRFToken, { status: value }))
  }

  async function addActivity(name: string, activityID: string) {
    setError("")
    try {
      const body = (await post(`/api/tasks/${task.ID}/activities`, data.CSRFToken, activityID ? { activity_id: activityID } : { name })) as { activity: TaskActivity }
      setActivities(previous => [...previous, body.activity])
      setLinkable(previous => previous.filter(item => item.ID !== body.activity.ID))
      setNewName("")
      setLinkID("")
      setSaved(true)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t(lang, "err.generic"))
    }
  }

  async function archive() {
    if (!await requestConfirmation(t(lang, "tasks.archiveConfirm"), { action: t(lang, "tasks.archive") })) return
    void run(async () => {
      await post(`/api/tasks/${task.ID}/archive`, data.CSRFToken, {})
      window.location.assign("/tasks")
    })
  }

  function restore() {
    void run(async () => {
      await post(`/api/tasks/${task.ID}/restore`, data.CSRFToken, {})
      window.location.reload()
    })
  }

  return <main className="mx-auto w-full max-w-3xl space-y-6">
    <a href="/tasks" className="inline-flex items-center gap-1 text-sm text-muted-foreground underline-offset-4 hover:underline"><ArrowLeft aria-hidden="true" className="size-4" />{t(lang, "tasks.back")}</a>

    <header className="space-y-1">
      <p className="flex items-center gap-2 text-sm text-muted-foreground"><span aria-hidden="true" className="size-2.5 rounded-full" style={{ backgroundColor: task.ProjectColor }} />{task.ProjectName}</p>
      <h1 className="text-2xl font-semibold tracking-tight">{title}</h1>
      {task.ExternalURL && <a href={task.ExternalURL} target="_blank" rel="noreferrer" className="w-fit text-sm text-muted-foreground underline underline-offset-4 hover:text-foreground">{t(lang, "tasks.openGitHub")}</a>}
    </header>

    {task.Archived && <div role="status" className="flex flex-wrap items-center justify-between gap-3 rounded-md border p-3 text-sm">
      <span>{t(lang, "tasks.archivedNotice")}</span>
      {task.CanArchive && <Button type="button" variant="outline" onClick={restore}><RotateCcw aria-hidden="true" />{t(lang, "tasks.restore")}</Button>}
    </div>}
    {error && <p role="alert" className="rounded-md border border-destructive/40 p-3 text-sm text-destructive">{error}</p>}
    {saved && !error && <p role="status" className="text-sm text-muted-foreground">{t(lang, "tasks.saved")}</p>}

    <form onSubmit={save} className="grid gap-4 sm:grid-cols-2" aria-label={t(lang, "tasks.cardTitle")}>
      <div className="grid gap-1.5 sm:col-span-2">
        <Label htmlFor="task-title">{t(lang, "tasks.name")}</Label>
        <Input id="task-title" value={title} onChange={event => setTitle(event.target.value)} required maxLength={200} disabled={task.Archived} />
      </div>
      <div className="grid gap-1.5">
        <Label htmlFor="task-status">{t(lang, "tasks.status")}</Label>
        <Select value={status} onValueChange={changeStatus} disabled={task.Archived}>
          <SelectTrigger id="task-status" className="w-full"><SelectValue /></SelectTrigger>
          <SelectContent>{statuses.map(option => <SelectItem key={option} value={option}>{t(lang, `tasks.${option}`)}</SelectItem>)}</SelectContent>
        </Select>
      </div>
      <div className="grid gap-1.5">
        <Label htmlFor="task-assignee">{t(lang, "tasks.assignee")}</Label>
        <Select value={assignee} onValueChange={setAssignee} disabled={task.Archived}>
          <SelectTrigger id="task-assignee" className="w-full"><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value={noPerson}>{t(lang, "tasks.nobody")}</SelectItem>
            {data.People.map(person => <SelectItem key={person.ID} value={String(person.ID)}>{person.Name}</SelectItem>)}
          </SelectContent>
        </Select>
      </div>
      <div className="grid gap-1.5">
        <Label htmlFor="task-due">{t(lang, "tasks.due")}</Label>
        <DateField id="task-due" name="due" defaultValue={task.Due} onValueChange={setDue} onBrokenChange={setDueBroken} disabled={task.Archived} />
      </div>
      <div className="grid gap-1.5">
        <Label htmlFor="task-estimate">{t(lang, "tasks.estimate")}</Label>
        <Input id="task-estimate" type="number" min={0} step={30} inputMode="numeric" value={estimate} onChange={event => setEstimate(event.target.value)} placeholder="480" disabled={task.Archived} />
      </div>
      <div className="sm:col-span-2 flex flex-wrap items-center justify-between gap-2">
        <p className="text-sm text-muted-foreground">{t(lang, "tasks.tracked")}: <span className="font-mono text-foreground">{task.TrackedLabel}</span></p>
        <div className="flex gap-2">
          {!task.Archived && task.CanArchive && <Button type="button" variant="ghost" onClick={archive}><Archive aria-hidden="true" />{t(lang, "tasks.archive")}</Button>}
          {!task.Archived && <Button type="submit">{t(lang, "projects.save")}</Button>}
        </div>
      </div>
    </form>

    <section aria-label={t(lang, "tasks.activities")} className="space-y-3">
      <h2 className="text-sm font-semibold tracking-wide text-muted-foreground">{t(lang, "tasks.activities")}</h2>
      {activities.length === 0 ? <p className="text-sm text-muted-foreground">{t(lang, "tasks.noActivities")}</p> : <ul className="flex flex-wrap gap-2">
        {activities.map(activity => <li key={activity.ID} className="inline-flex items-center gap-2 rounded-full border px-3 py-1 text-sm">
          <span aria-hidden="true" className="size-2 rounded-full" style={{ backgroundColor: activity.Color }} />{activity.Name}
        </li>)}
      </ul>}

      {!task.Archived && <Card><CardContent className="grid gap-4 p-4">
        <form onSubmit={event => { event.preventDefault(); void addActivity(newName.trim(), "") }} className="grid gap-2">
          <Label htmlFor="task-activity-name">{t(lang, "tasks.addActivity")}</Label>
          <div className="flex gap-2">
            <Input id="task-activity-name" value={newName} onChange={event => setNewName(event.target.value)} placeholder={t(lang, "tasks.activityName")} maxLength={120} />
            <Button type="submit" disabled={!newName.trim()}><Plus aria-hidden="true" />{t(lang, "tasks.addActivity")}</Button>
          </div>
        </form>
        {linkable.length > 0 && <form onSubmit={event => { event.preventDefault(); if (linkID) void addActivity("", linkID) }} className="grid gap-2">
          <Label htmlFor="task-link-activity">{t(lang, "tasks.linkExisting")}</Label>
          <div className="flex gap-2">
            <Select value={linkID} onValueChange={setLinkID}>
              <SelectTrigger id="task-link-activity" className="w-full"><SelectValue placeholder={t(lang, "tasks.linkExisting")} /></SelectTrigger>
              <SelectContent>{linkable.map(item => <SelectItem key={item.ID} value={String(item.ID)}>{item.Name}</SelectItem>)}</SelectContent>
            </Select>
            <Button type="submit" disabled={!linkID}>{t(lang, "tasks.linkExisting")}</Button>
          </div>
        </form>}
      </CardContent></Card>}
    </section>
  </main>
}
