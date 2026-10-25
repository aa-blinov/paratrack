import * as React from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Textarea } from "@/components/ui/textarea"
import { FieldIssue, FormIssue } from "@/components/form-issue"
import { translate as t } from "@/i18n"
import { apiJSON, ApiError, issueOf, type Issue } from "@/lib/json-api"
import { formatDateTime } from "@/lib/date-format"
import type { Client, Member } from "@/lib/team-directory"
import { activityKinds, type Activity, type Deal, type PipelineStage } from "@/crm/model"
import { DealForm } from "@/crm/deal-form"

type Props = {
  deal: Deal
  stages: PipelineStage[]
  lang: string
  csrf: string
  viewerID: number
  canManage: boolean
  clients: Client[]
  members: Member[]
  onChanged: () => void
}

/**
 * The open deal under its row: the edit form, the stage move and the timeline.
 * The edit and the stage move belong to the owner and to managers; anyone on
 * the team may read the timeline and log an entry on it.
 */
export function DealDetail({ deal, stages, lang, csrf, viewerID, canManage, clients, members, onChanged }: Props) {
  const canChange = canManage || deal.owner_id === viewerID
  const won = stages.find(item => item.stage === deal.stage)?.kind === "won"
  return <div className="grid gap-6 border-t pt-4 pb-2">
    {deal.lost_reason && <p className="text-sm"><span className="text-muted-foreground">{t(lang, "crm.lostReason")}: </span>{deal.lost_reason}</p>}
    {(deal.project_id !== 0 || (won && canChange)) && <DealProject deal={deal} lang={lang} csrf={csrf} onChanged={onChanged} />}
    {canChange && <section className="grid gap-3" aria-labelledby={`deal-${deal.id}-edit`}>
      <h3 id={`deal-${deal.id}-edit`} className="text-sm font-semibold">{t(lang, "crm.details")}</h3>
      <DealForm key={deal.id} lang={lang} csrf={csrf} viewerID={viewerID} canManage={canManage} clients={clients} members={members} deal={deal} onSaved={onChanged} />
    </section>}
    {canChange && <StageMove key={`${deal.id}-${deal.stage}`} deal={deal} stages={stages} lang={lang} csrf={csrf} onMoved={onChanged} />}
    <Timeline deal={deal} lang={lang} csrf={csrf} />
  </div>
}

// Why the server refuses the project step, by the field it names. Any other refusal
// keeps the server's own words.
const projectRefusals: Record<string, string> = {
  stage: "crm.projectNotWon",
  project: "crm.projectExists",
  client: "crm.projectNeedsClient",
  "project name": "crm.projectNameTaken",
}

/**
 * The project of a won deal. Until the deal has one, its owner or a manager makes it
 * here; after that the deal links to the project. The server decides who may make it
 * and says why a member may not.
 */
function DealProject({ deal, lang, csrf, onChanged }: { deal: Deal; lang: string; csrf: string; onChanged: () => void }) {
  const [slug, setSlug] = React.useState<string | null>(null)
  const [issue, setIssue] = React.useState<Issue | null>(null)
  const [busy, setBusy] = React.useState(false)

  // The deal keeps the project's id; the page link needs its slug from the project list.
  React.useEffect(() => {
    let live = true
    if (deal.project_id !== 0) {
      apiJSON<{ projects: { id: number; slug: string }[] }>("/api/projects", "GET", "")
        .then(body => { if (live) setSlug(body.projects.find(project => project.id === deal.project_id)?.slug ?? "") })
        .catch(() => { if (live) setSlug("") })
    }
    return () => { live = false }
  }, [deal.project_id])

  async function make() {
    setBusy(true)
    setIssue(null)
    try {
      await apiJSON(`/api/crm/deals/${deal.id}/project`, "POST", csrf, {})
      onChanged()
    } catch (cause) {
      const field = cause instanceof ApiError ? cause.field : ""
      setIssue({ key: "", message: projectRefusals[field] ? t(lang, projectRefusals[field]) : issueOf(cause, {}, t(lang, "err.generic")).message })
    } finally {
      setBusy(false)
    }
  }

  return <section className="grid gap-3" aria-labelledby={`deal-${deal.id}-project`}>
    <h3 id={`deal-${deal.id}-project`} className="text-sm font-semibold">{t(lang, "crm.project")}</h3>
    {deal.project_id !== 0
      ? <div><Button asChild variant="outline" size="sm"><a href={slug ? `/projects/${slug}` : "/projects"}>{t(lang, "crm.projectOpen")}</a></Button></div>
      : <div className="grid gap-3 sm:max-w-md">
        <FormIssue issue={issue} />
        <div><Button type="button" disabled={busy} onClick={() => void make()}>{t(lang, "crm.projectButton")}</Button></div>
      </div>}
  </section>
}

function StageMove({ deal, stages, lang, csrf, onMoved }: { deal: Deal; stages: PipelineStage[]; lang: string; csrf: string; onMoved: () => void }) {
  const [target, setTarget] = React.useState(deal.stage)
  const [reason, setReason] = React.useState("")
  const [issue, setIssue] = React.useState<Issue | null>(null)
  const [busy, setBusy] = React.useState(false)
  const lost = stages.find(item => item.stage === target)?.kind === "lost"
  // A lost deal needs its reason before the request is made; the server would
  // refuse it anyway, but the person should not have to read that refusal first.
  const blocked = busy || target === deal.stage || (lost && reason.trim() === "")

  async function move(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setBusy(true)
    setIssue(null)
    try {
      await apiJSON(`/api/crm/deals/${deal.id}/stage`, "POST", csrf, { stage: target, lost_reason: lost ? reason.trim() : "" })
      setReason("")
      onMoved()
    } catch (cause) {
      setIssue(issueOf(cause, { stage: "stage", lost_reason: "lost_reason", "lost reason": "lost_reason" }, t(lang, "err.generic")))
    } finally {
      setBusy(false)
    }
  }

  return <section className="grid gap-3" aria-labelledby={`deal-${deal.id}-stage`}>
    <h3 id={`deal-${deal.id}-stage`} className="text-sm font-semibold">{t(lang, "crm.moveStage")}</h3>
    <form onSubmit={move} className="grid gap-3 sm:max-w-md">
      <FormIssue issue={issue} />
      <div className="grid gap-2">
        <Label htmlFor={`deal-${deal.id}-target`}>{t(lang, "crm.stageLabel")}</Label>
        <Select value={target} onValueChange={setTarget}>
          <SelectTrigger id={`deal-${deal.id}-target`} className="w-full" aria-invalid={issue?.key === "stage" || undefined} style={{ fontSize: 16 }}><SelectValue /></SelectTrigger>
          <SelectContent>{stages.map(item => <SelectItem key={item.stage} value={item.stage}>{item.name}</SelectItem>)}</SelectContent>
        </Select>
        <FieldIssue issue={issue} name="stage" id={`deal-${deal.id}-stage-issue`} />
      </div>
      {lost && <div className="grid gap-2">
        <Label htmlFor={`deal-${deal.id}-reason`}>{t(lang, "crm.lostReason")}</Label>
        <Input id={`deal-${deal.id}-reason`} value={reason} onChange={event => setReason(event.target.value)} required maxLength={200} aria-invalid={issue?.key === "lost_reason" || undefined} aria-describedby={issue?.key === "lost_reason" ? `deal-${deal.id}-reason-issue` : undefined} style={{ fontSize: 16 }} />
        <FieldIssue issue={issue} name="lost_reason" id={`deal-${deal.id}-reason-issue`} />
      </div>}
      <div><Button type="submit" disabled={blocked}>{t(lang, "crm.moveButton")}</Button></div>
    </form>
  </section>
}

function Timeline({ deal, lang, csrf }: { deal: Deal; lang: string; csrf: string }) {
  const [items, setItems] = React.useState<Activity[] | null>(null)
  const [kind, setKind] = React.useState<string>("note")
  const [body, setBody] = React.useState("")
  const [issue, setIssue] = React.useState<Issue | null>(null)
  const [busy, setBusy] = React.useState(false)

  const load = React.useCallback(async () => {
    try {
      const response = await apiJSON<{ activities: Activity[] }>(`/api/crm/activities?deal_id=${deal.id}`, "GET", "")
      setItems(response.activities)
    } catch (cause) {
      setIssue(issueOf(cause, {}, t(lang, "err.generic")))
    }
  }, [deal.id, lang])

  React.useEffect(() => { void load() }, [load])

  async function log(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setBusy(true)
    setIssue(null)
    try {
      await apiJSON("/api/crm/activities", "POST", csrf, { deal_id: deal.id, kind, body: body.trim() })
      setBody("")
      await load()
    } catch (cause) {
      setIssue(issueOf(cause, { body: "body", kind: "kind" }, t(lang, "err.generic")))
    } finally {
      setBusy(false)
    }
  }

  return <section className="grid gap-3" aria-labelledby={`deal-${deal.id}-timeline`}>
    <h3 id={`deal-${deal.id}-timeline`} className="text-sm font-semibold">{t(lang, "crm.timeline")}</h3>
    <form onSubmit={log} className="grid gap-3 sm:max-w-md">
      <FormIssue issue={issue} />
      <div className="grid gap-2">
        <Label htmlFor={`deal-${deal.id}-kind`}>{t(lang, "crm.kindLabel")}</Label>
        <Select value={kind} onValueChange={setKind}>
          <SelectTrigger id={`deal-${deal.id}-kind`} className="w-full" aria-invalid={issue?.key === "kind" || undefined} style={{ fontSize: 16 }}><SelectValue /></SelectTrigger>
          <SelectContent>{activityKinds.map(item => <SelectItem key={item} value={item}>{t(lang, `crm.kind.${item}`)}</SelectItem>)}</SelectContent>
        </Select>
        <FieldIssue issue={issue} name="kind" id={`deal-${deal.id}-kind-issue`} />
      </div>
      <div className="grid gap-2">
        <Label htmlFor={`deal-${deal.id}-body`}>{t(lang, "crm.activityBody")}</Label>
        <Textarea id={`deal-${deal.id}-body`} rows={3} value={body} onChange={event => setBody(event.target.value)} required maxLength={4000} aria-invalid={issue?.key === "body" || undefined} aria-describedby={issue?.key === "body" ? `deal-${deal.id}-body-issue` : undefined} style={{ fontSize: 16 }} />
        <FieldIssue issue={issue} name="body" id={`deal-${deal.id}-body-issue`} />
      </div>
      <div><Button type="submit" variant="outline" disabled={busy || body.trim() === ""}>{t(lang, "crm.logActivity")}</Button></div>
    </form>
    {items === null ? null : items.length === 0 ? <p className="text-sm text-muted-foreground">{t(lang, "crm.noActivities")}</p> : <ol className="grid gap-3">
      {items.map(item => <li key={item.id} className="grid gap-1 border-l-2 pl-3">
        <span className="text-xs text-muted-foreground">{t(lang, `crm.kind.${item.kind}`)}, {formatDateTime(item.occurred_at.slice(0, 16), lang)}</span>
        <p className="whitespace-pre-line text-sm [overflow-wrap:anywhere]">{item.body || item.result}</p>
      </li>)}
    </ol>}
  </section>
}
