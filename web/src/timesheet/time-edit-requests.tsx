import * as React from "react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Textarea } from "@/components/ui/textarea"
import { translate as t } from "@/i18n"
import type { TimeEditRequest, TimesheetDay, TimesheetRow } from "@/dashboard/types"

type Props = {
  lang: string
  csrf: string
  days: TimesheetDay[]
  rows: TimesheetRow[]
  requests: TimeEditRequest[]
  canDecide: boolean
}

// TimeEditRequests asks for a change to one timesheet cell and lists the requests. A manager
// approves or refuses the pending ones (TIME-19).
export function TimeEditRequests({ lang, csrf, days, rows, requests, canDecide }: Props) {
  const [activity, setActivity] = React.useState("")
  const [day, setDay] = React.useState("")
  const [minutes, setMinutes] = React.useState("")
  const [reason, setReason] = React.useState("")
  const [error, setError] = React.useState("")

  async function send(path: string, fields: Record<string, string>) {
    const response = await fetch(path, {
      method: "POST",
      credentials: "same-origin",
      headers: { Accept: "application/json", "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8", "X-CSRF-Token": csrf },
      body: new URLSearchParams({ csrf_token: csrf, ...fields }),
    })
    const json = (await response.json()) as { ok?: boolean; error?: string }
    if (!response.ok) throw new Error(json.error || response.statusText)
    return json
  }

  async function submit(event: React.FormEvent) {
    event.preventDefault()
    setError("")
    try {
      await send("/api/timesheet/edits", { activity_id: activity, day, minutes, reason })
      window.location.reload()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t(lang, "err.generic"))
    }
  }

  async function decide(id: number, approve: boolean) {
    setError("")
    try {
      await send(`/api/timesheet/edits/${id}/decision`, { approve: approve ? "1" : "0" })
      window.location.reload()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t(lang, "err.generic"))
    }
  }

  const ready = Boolean(activity && day && minutes.trim() && reason.trim())

  return <Card id="time-edits" className="scroll-mt-20">
    <CardHeader><CardTitle>{t(lang, "ts.editsTitle")}</CardTitle></CardHeader>
    <CardContent className="grid gap-4">
      <form onSubmit={submit} className="grid gap-3 sm:grid-cols-2">
        <div className="grid gap-1.5">
          <Label htmlFor="edit-activity">{t(lang, "ts.editsActivity")}</Label>
          <Select value={activity} onValueChange={setActivity}>
            <SelectTrigger id="edit-activity"><SelectValue placeholder={t(lang, "ts.editsActivity")} /></SelectTrigger>
            <SelectContent>{rows.map(row => <SelectItem key={row.ActivityID} value={String(row.ActivityID)}>{row.ActivityName}</SelectItem>)}</SelectContent>
          </Select>
        </div>
        <div className="grid gap-1.5">
          <Label htmlFor="edit-day">{t(lang, "ts.editsDay")}</Label>
          <Select value={day} onValueChange={setDay}>
            <SelectTrigger id="edit-day"><SelectValue placeholder={t(lang, "ts.editsDay")} /></SelectTrigger>
            <SelectContent>{days.map(item => <SelectItem key={item.ISO} value={item.ISO}>{`${item.Label} ${item.Date}`}</SelectItem>)}</SelectContent>
          </Select>
        </div>
        <div className="grid gap-1.5">
          <Label htmlFor="edit-minutes">{t(lang, "ts.editsMinutes")}</Label>
          <Input id="edit-minutes" inputMode="numeric" value={minutes} onChange={event => setMinutes(event.target.value)} placeholder="90" />
        </div>
        <div className="grid gap-1.5 sm:col-span-2">
          <Label htmlFor="edit-reason">{t(lang, "ts.editsReason")}</Label>
          <Textarea id="edit-reason" value={reason} onChange={event => setReason(event.target.value)} rows={2} maxLength={500} />
        </div>
        <div><Button type="submit" size="sm" disabled={!ready}>{t(lang, "ts.editsSend")}</Button></div>
      </form>
      {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
      {requests.length ? <ul className="grid gap-3">{requests.map(item => <li key={item.ID} className="grid gap-1 border-b pb-3 text-sm last:border-0">
        <p><span className="font-medium">{item.Person}</span>, {item.Activity}, {item.Day}, {item.Hours}</p>
        <p className="whitespace-pre-line text-muted-foreground [overflow-wrap:anywhere]">{item.Reason}</p>
        <p className="text-xs text-muted-foreground">{t(lang, `ts.edit.${item.Status}`)}{item.Note ? `: ${item.Note}` : ""}</p>
        {canDecide && item.Status === "pending" && <div className="flex gap-2">
          <Button type="button" size="sm" onClick={() => void decide(item.ID, true)}>{t(lang, "ts.editsApprove")}</Button>
          <Button type="button" size="sm" variant="outline" onClick={() => void decide(item.ID, false)}>{t(lang, "ts.editsRefuse")}</Button>
        </div>}
      </li>)}</ul> : <p className="text-sm text-muted-foreground">{t(lang, "ts.editsEmpty")}</p>}
    </CardContent>
  </Card>
}
