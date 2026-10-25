import * as React from "react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { FormIssue } from "@/components/form-issue"
import { translate as t } from "@/i18n"
import { ApiError, apiJSON, type Issue } from "@/lib/json-api"
import type { PipelineStage, StageKind } from "@/crm/model"

const stageKinds: StageKind[] = ["open", "won", "lost"]

type Props = {
  lang: string
  csrf: string
  /** The pipeline in its order; a stage's place is its index plus one. */
  stages: PipelineStage[]
  onChanged: () => void
}

/**
 * The pipeline editor for managers. The kind of a stage sets its rules, so each
 * stage shows its kind. A stage that still has deals is kept: the refusal names
 * the stage, and the person moves the deals first.
 */
export function StageEditor({ lang, csrf, stages, onChanged }: Props) {
  const [name, setName] = React.useState("")
  const [kind, setKind] = React.useState<StageKind>("open")
  const [issue, setIssue] = React.useState<Issue | null>(null)
  const [busy, setBusy] = React.useState(false)

  // The server names the refused field. A field-level refusal is shown once above
  // the editor, because every input here is a stage rather than a form field.
  function refusal(cause: unknown): Issue {
    const field = cause instanceof ApiError ? cause.field : ""
    if (field === "stage") return { key: "", message: t(lang, "crm.stageInUse") }
    if (field === "kind") return { key: "", message: t(lang, "crm.stageNeedsKinds") }
    if (cause instanceof ApiError && field) return { key: "", message: t(lang, "err.invalidInput") }
    return { key: "", message: cause instanceof ApiError && cause.message ? cause.message : t(lang, "err.generic") }
  }

  async function act(request: () => Promise<unknown>): Promise<boolean> {
    setBusy(true)
    setIssue(null)
    try {
      await request()
      onChanged()
      return true
    } catch (cause) {
      setIssue(refusal(cause))
      return false
    } finally {
      setBusy(false)
    }
  }

  async function add(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const added = await act(() => apiJSON("/api/crm/stages", "POST", csrf, { name: name.trim(), kind }))
    if (added) setName("")
  }

  return <Card>
    <CardHeader><CardTitle>{t(lang, "crm.stagesTitle")}</CardTitle></CardHeader>
    <CardContent className="grid gap-5">
      <p className="text-sm text-muted-foreground">{t(lang, "crm.stagesHint")}</p>
      <FormIssue issue={issue} />
      <ol className="grid gap-3">
        {stages.map((stage, index) => <StageRow key={`${stage.stage}-${stage.name}-${stage.kind}-${index}`} stage={stage} index={index} count={stages.length} lang={lang} csrf={csrf} busy={busy} act={act} />)}
      </ol>
      <form onSubmit={add} className="grid gap-3 border-t pt-4 sm:grid-cols-[1fr_11rem_auto] sm:items-end">
        <div className="grid gap-2">
          <Label htmlFor="crm-stage-new-name">{t(lang, "crm.stageName")}</Label>
          <Input id="crm-stage-new-name" value={name} onChange={event => setName(event.target.value)} required maxLength={200} style={{ fontSize: 16 }} />
        </div>
        <div className="grid gap-2">
          <Label htmlFor="crm-stage-new-kind">{t(lang, "crm.stageKindLabel")}</Label>
          <Select value={kind} onValueChange={value => setKind(value as StageKind)}>
            <SelectTrigger id="crm-stage-new-kind" className="w-full" style={{ fontSize: 16 }}><SelectValue /></SelectTrigger>
            <SelectContent>{stageKinds.map(item => <SelectItem key={item} value={item}>{t(lang, `crm.stageKind.${item}`)}</SelectItem>)}</SelectContent>
          </Select>
        </div>
        <Button type="submit" disabled={busy || name.trim() === ""}>{t(lang, "crm.stageAdd")}</Button>
      </form>
    </CardContent>
  </Card>
}

function StageRow({ stage, index, count, lang, csrf, busy, act }: {
  stage: PipelineStage
  index: number
  count: number
  lang: string
  csrf: string
  busy: boolean
  act: (request: () => Promise<unknown>) => Promise<boolean>
}) {
  const [name, setName] = React.useState(stage.name)
  const [kind, setKind] = React.useState<StageKind>(stage.kind)
  const url = `/api/crm/stages/${encodeURIComponent(stage.stage)}`
  const changed = name.trim() !== stage.name || kind !== stage.kind

  return <li className="grid gap-3 rounded-lg border p-3 sm:grid-cols-[1fr_11rem] sm:items-end">
    <div className="grid gap-2">
      <Label htmlFor={`crm-stage-${index}-name`}>{t(lang, "crm.stageName")}</Label>
      <Input id={`crm-stage-${index}-name`} value={name} onChange={event => setName(event.target.value)} maxLength={200} style={{ fontSize: 16 }} />
    </div>
    <div className="grid gap-2">
      <Label htmlFor={`crm-stage-${index}-kind`}>{t(lang, "crm.stageKindLabel")}</Label>
      <Select value={kind} onValueChange={value => setKind(value as StageKind)}>
        <SelectTrigger id={`crm-stage-${index}-kind`} className="w-full" style={{ fontSize: 16 }}><SelectValue /></SelectTrigger>
        <SelectContent>{stageKinds.map(item => <SelectItem key={item} value={item}>{t(lang, `crm.stageKind.${item}`)}</SelectItem>)}</SelectContent>
      </Select>
    </div>
    <div className="flex flex-wrap gap-2 sm:col-span-2">
      <Button type="button" size="sm" disabled={busy || !changed || name.trim() === ""}
        onClick={() => void act(() => apiJSON(url, "POST", csrf, { name: name.trim(), kind }))}>{t(lang, "crm.stageSave")}</Button>
      <Button type="button" size="sm" variant="outline" disabled={busy || index === 0}
        onClick={() => void act(() => apiJSON(url, "POST", csrf, { position: index }))}>{t(lang, "crm.stageUp")}</Button>
      <Button type="button" size="sm" variant="outline" disabled={busy || index === count - 1}
        onClick={() => void act(() => apiJSON(url, "POST", csrf, { position: index + 2 }))}>{t(lang, "crm.stageDown")}</Button>
      <Button type="button" size="sm" variant="ghost" disabled={busy}
        onClick={() => void act(() => apiJSON(`${url}/delete`, "POST", csrf))}>{t(lang, "crm.stageDelete")}</Button>
    </div>
  </li>
}
