import * as React from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Textarea } from "@/components/ui/textarea"
import { DisclosureSection } from "@/components/disclosure-section"
import { FieldIssue, FormIssue } from "@/components/form-issue"
import { translate as t } from "@/i18n"
import { apiJSON, ApiError, issueOf, type Issue } from "@/lib/json-api"
import { formatDate, formatDateTime } from "@/lib/date-format"
import { memberLabel, type Member } from "@/lib/team-directory"
import { activityKinds, leadFieldKeys, leadIsOpen, leadStatuses, type Activity, type Lead, type LeadStatus } from "@/crm/model"
import { IntakeKeyCard } from "@/crm/intake-key-card"

const noOwner = "0"

/**
 * Leads: enquiries before they are clients and deals. The list filters by status, a lead
 * opens in place, and an open lead can be converted into a client, a deal and a contact.
 * A manager also sees the website's intake key below the list.
 */
export function LeadsPanel({ lang, csrf, viewerID, canManage, members }: { lang: string; csrf: string; viewerID: number; canManage: boolean; members: Member[] }) {
  const [leads, setLeads] = React.useState<Lead[] | null>(null)
  const [status, setStatus] = React.useState("")
  const [selectedID, setSelectedID] = React.useState<number | null>(null)
  const [error, setError] = React.useState("")

  const load = React.useCallback(async () => {
    const query = new URLSearchParams()
    if (status) query.set("status", status)
    try {
      const body = await apiJSON<{ leads: Lead[] }>(`/api/crm/leads?${query}`, "GET", "")
      setLeads(body.leads)
      setError("")
    } catch (cause) {
      setError(cause instanceof ApiError && cause.message ? cause.message : t(lang, "err.generic"))
    }
  }, [status, lang])

  React.useEffect(() => { void load() }, [load])

  return <div className="grid gap-5">
    <p className="text-sm text-muted-foreground">{t(lang, "crm.lead.blurb")}</p>
    {error && <p role="alert" className="rounded-md border border-destructive/40 p-3 text-sm text-destructive">{error}</p>}
    <DisclosureSection title={t(lang, "crm.lead.add")}>
      <LeadForm lang={lang} csrf={csrf} onSaved={() => void load()} />
    </DisclosureSection>
    <Card>
      <CardHeader><CardTitle>{t(lang, "crm.leads")}</CardTitle></CardHeader>
      <CardContent className="grid gap-4">
        <div role="group" aria-label={t(lang, "crm.leads")} className="flex flex-wrap gap-2">
          <Button type="button" variant={status === "" ? "secondary" : "outline"} aria-pressed={status === ""} onClick={() => setStatus("")}>{t(lang, "crm.allStages")}</Button>
          {leadStatuses.map(item => <Button key={item} type="button" variant={status === item ? "secondary" : "outline"} aria-pressed={status === item} onClick={() => setStatus(status === item ? "" : item)}>
            {t(lang, `crm.leadStatus.${item}`)}
          </Button>)}
        </div>
        {leads !== null && leads.length === 0 && <p className="py-6 text-center text-sm text-muted-foreground">{status ? t(lang, "crm.lead.emptyFiltered") : t(lang, "crm.lead.empty")}</p>}
        {leads !== null && leads.length > 0 && <ul className="grid divide-y rounded-lg border">
          {leads.map(lead => <li key={lead.id} className="px-3">
            <button type="button" aria-expanded={selectedID === lead.id} onClick={() => setSelectedID(current => current === lead.id ? null : lead.id)} className="grid w-full gap-1 py-3 text-left">
              <span className="flex flex-wrap items-center gap-2"><span className="min-w-0 font-medium [overflow-wrap:anywhere]">{leadName(lead, lang)}</span><Badge variant="secondary">{t(lang, `crm.leadStatus.${lead.status}`)}</Badge></span>
              <span className="flex flex-wrap gap-x-4 gap-y-1 text-sm text-muted-foreground">
                {lead.source && <span>{lead.source}</span>}
                {lead.owner_id === 0 ? <span>{t(lang, "crm.lead.noOwner")}</span> : ownerLabel(members, lead.owner_id) && <span>{ownerLabel(members, lead.owner_id)}</span>}
                {lead.next_step_at && <span>{t(lang, "crm.nextStep")}: {formatDate(lead.next_step_at, lang)}</span>}
              </span>
            </button>
            {selectedID === lead.id && <div className="pb-4">
              <LeadDetail lead={lead} lang={lang} csrf={csrf} viewerID={viewerID} canManage={canManage} members={members} onChanged={() => void load()} />
            </div>}
          </li>)}
        </ul>}
      </CardContent>
    </Card>
    {canManage && <IntakeKeyCard lang={lang} csrf={csrf} />}
  </div>
}

function leadName(lead: Lead, lang: string): string {
  return lead.company || lead.contact_name || lead.email || lead.phone || t(lang, "crm.lead.noName")
}

// A member who is not in the list is shown by no name: members are loaded for managers only.
function ownerLabel(members: Member[], id: number): string {
  const member = members.find(item => item.id === id)
  return member ? memberLabel(member) : ""
}

function LeadForm({ lang, csrf, onSaved }: { lang: string; csrf: string; onSaved: () => void }) {
  const [company, setCompany] = React.useState("")
  const [contactName, setContactName] = React.useState("")
  const [phone, setPhone] = React.useState("")
  const [email, setEmail] = React.useState("")
  const [source, setSource] = React.useState("")
  const [message, setMessage] = React.useState("")
  const [issue, setIssue] = React.useState<Issue | null>(null)
  const [busy, setBusy] = React.useState(false)

  const invalid = (key: string) => issue?.key === key || undefined
  const described = (key: string) => issue?.key === key ? `lead-new-${key}-issue` : undefined

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setBusy(true)
    setIssue(null)
    try {
      await apiJSON("/api/crm/leads", "POST", csrf, {
        company: company.trim(), contact_name: contactName.trim(), phone: phone.trim(), email: email.trim(),
        source: source.trim(), message: message.trim(),
      })
      setCompany(""); setContactName(""); setPhone(""); setEmail(""); setSource(""); setMessage("")
      onSaved()
    } catch (cause) {
      setIssue(issueOf(cause, leadFieldKeys, t(lang, "err.generic")))
    } finally {
      setBusy(false)
    }
  }

  return <form onSubmit={submit} className="grid gap-4" aria-busy={busy}>
    <FormIssue issue={issue} />
    <div className="grid gap-4 sm:grid-cols-2">
      <div className="grid gap-2">
        <Label htmlFor="lead-new-company">{t(lang, "crm.lead.company")}</Label>
        <Input id="lead-new-company" value={company} onChange={event => setCompany(event.target.value)} maxLength={200} aria-invalid={invalid("company")} aria-describedby={described("company")} style={{ fontSize: 16 }} />
        <FieldIssue issue={issue} name="company" id="lead-new-company-issue" />
      </div>
      <div className="grid gap-2">
        <Label htmlFor="lead-new-contact">{t(lang, "crm.lead.contactName")}</Label>
        <Input id="lead-new-contact" value={contactName} onChange={event => setContactName(event.target.value)} maxLength={200} aria-invalid={invalid("contact_name")} aria-describedby={described("contact_name")} style={{ fontSize: 16 }} />
        <FieldIssue issue={issue} name="contact_name" id="lead-new-contact_name-issue" />
      </div>
      <div className="grid gap-2">
        <Label htmlFor="lead-new-phone">{t(lang, "crm.lead.phone")}</Label>
        <Input id="lead-new-phone" type="tel" value={phone} onChange={event => setPhone(event.target.value)} maxLength={40} aria-invalid={invalid("phone")} aria-describedby={described("phone")} style={{ fontSize: 16 }} />
        <FieldIssue issue={issue} name="phone" id="lead-new-phone-issue" />
      </div>
      <div className="grid gap-2">
        <Label htmlFor="lead-new-email">{t(lang, "crm.lead.email")}</Label>
        <Input id="lead-new-email" type="email" value={email} onChange={event => setEmail(event.target.value)} maxLength={200} aria-invalid={invalid("email")} aria-describedby={described("email")} style={{ fontSize: 16 }} />
        <FieldIssue issue={issue} name="email" id="lead-new-email-issue" />
      </div>
      <div className="grid gap-2">
        <Label htmlFor="lead-new-source">{t(lang, "crm.lead.source")}</Label>
        <Input id="lead-new-source" value={source} onChange={event => setSource(event.target.value)} maxLength={200} aria-invalid={invalid("source")} aria-describedby={described("source")} style={{ fontSize: 16 }} />
        <FieldIssue issue={issue} name="source" id="lead-new-source-issue" />
      </div>
      <div className="grid gap-2 sm:col-span-2">
        <Label htmlFor="lead-new-message">{t(lang, "crm.lead.message")}</Label>
        <Textarea id="lead-new-message" rows={3} value={message} onChange={event => setMessage(event.target.value)} maxLength={4000} aria-invalid={invalid("message")} aria-describedby={described("message")} style={{ fontSize: 16 }} />
        <FieldIssue issue={issue} name="message" id="lead-new-message-issue" />
      </div>
    </div>
    <div><Button type="submit" disabled={busy || (company.trim() === "" && contactName.trim() === "" && phone.trim() === "" && email.trim() === "")}>{t(lang, "crm.lead.add")}</Button></div>
  </form>
}

function LeadDetail({ lead, lang, csrf, viewerID, canManage, members, onChanged }: {
  lead: Lead; lang: string; csrf: string; viewerID: number; canManage: boolean; members: Member[]; onChanged: () => void
}) {
  const [nextStatus, setNextStatus] = React.useState<LeadStatus>(lead.status === "converted" ? "new" : lead.status)
  const [lostReason, setLostReason] = React.useState(lead.lost_reason)
  const [owner, setOwner] = React.useState(String(lead.owner_id || noOwner))
  const [issue, setIssue] = React.useState<Issue | null>(null)
  const [busy, setBusy] = React.useState(false)
  const open = leadIsOpen(lead)
  // A member may move a lead only when they own it; a manager may move any lead.
  const mayMove = canManage || lead.owner_id === viewerID

  async function run(action: () => Promise<unknown>) {
    setBusy(true)
    setIssue(null)
    try {
      await action()
      onChanged()
    } catch (cause) {
      setIssue(issueOf(cause, leadFieldKeys, t(lang, "err.generic")))
    } finally {
      setBusy(false)
    }
  }

  return <div className="grid gap-5">
    <FormIssue issue={issue} />
    <dl className="grid gap-2 text-sm sm:grid-cols-2">
      {lead.contact_name && <Fact label={t(lang, "crm.lead.contactName")} value={lead.contact_name} />}
      {lead.phone && <Fact label={t(lang, "crm.lead.phone")} value={lead.phone} />}
      {lead.email && <Fact label={t(lang, "crm.lead.email")} value={lead.email} />}
      {lead.source && <Fact label={t(lang, "crm.lead.source")} value={lead.source} />}
      {lead.message && <div className="grid gap-1 sm:col-span-2"><dt className="text-muted-foreground">{t(lang, "crm.lead.message")}</dt><dd className="whitespace-pre-line [overflow-wrap:anywhere]">{lead.message}</dd></div>}
      {lead.lost_reason && <Fact label={t(lang, "crm.lostReason")} value={lead.lost_reason} />}
      <Fact label={t(lang, "crm.lead.created")} value={formatDateTime(lead.created_at.slice(0, 16), lang)} />
    </dl>

    {canManage && open && <div className="grid gap-2 sm:max-w-sm">
      <Label htmlFor={`lead-${lead.id}-owner`}>{t(lang, "crm.owner")}</Label>
      <Select value={owner} onValueChange={setOwner}>
        <SelectTrigger id={`lead-${lead.id}-owner`} className="w-full" style={{ fontSize: 16 }}><SelectValue /></SelectTrigger>
        <SelectContent>
          <SelectItem value={noOwner}>{t(lang, "crm.lead.noOwner")}</SelectItem>
          {members.map(member => <SelectItem key={member.id} value={String(member.id)}>{memberLabel(member)}</SelectItem>)}
        </SelectContent>
      </Select>
      <div><Button type="button" variant="outline" disabled={busy || Number(owner) === lead.owner_id} onClick={() => void run(() => apiJSON(`/api/crm/leads/${lead.id}`, "POST", csrf, { owner_id: Number(owner) }))}>{t(lang, "crm.lead.setOwner")}</Button></div>
    </div>}

    {open && mayMove && <form className="grid gap-3 sm:max-w-sm" onSubmit={event => { event.preventDefault(); void run(() => apiJSON(`/api/crm/leads/${lead.id}/status`, "POST", csrf, { status: nextStatus, lost_reason: lostReason.trim() })) }}>
      <div className="grid gap-2">
        <Label htmlFor={`lead-${lead.id}-status`}>{t(lang, "crm.lead.status")}</Label>
        <Select value={nextStatus} onValueChange={value => setNextStatus(value as LeadStatus)}>
          <SelectTrigger id={`lead-${lead.id}-status`} className="w-full" aria-invalid={issue?.key === "status" || undefined} aria-describedby={issue?.key === "status" ? `lead-${lead.id}-status-issue` : undefined} style={{ fontSize: 16 }}><SelectValue /></SelectTrigger>
          <SelectContent>
            {leadStatuses.filter(item => item !== "converted").map(item => <SelectItem key={item} value={item}>{t(lang, `crm.leadStatus.${item}`)}</SelectItem>)}
          </SelectContent>
        </Select>
        <FieldIssue issue={issue} name="status" id={`lead-${lead.id}-status-issue`} />
      </div>
      {nextStatus === "lost" && <div className="grid gap-2">
        <Label htmlFor={`lead-${lead.id}-reason`}>{t(lang, "crm.lostReason")}</Label>
        <Input id={`lead-${lead.id}-reason`} value={lostReason} onChange={event => setLostReason(event.target.value)} maxLength={200} aria-invalid={issue?.key === "lost_reason" || undefined} style={{ fontSize: 16 }} />
        <FieldIssue issue={issue} name="lost_reason" id={`lead-${lead.id}-reason-issue`} />
      </div>}
      <div><Button type="submit" variant="outline" disabled={busy || (nextStatus === lead.status && nextStatus !== "lost")}>{t(lang, "crm.lead.setStatus")}</Button></div>
    </form>}

    {open && mayMove && <div className="grid gap-2">
      <p className="text-sm text-muted-foreground">{t(lang, "crm.lead.convertHint")}</p>
      <div><Button type="button" disabled={busy} onClick={() => void run(() => apiJSON(`/api/crm/leads/${lead.id}/convert`, "POST", csrf, {}))}>{t(lang, "crm.lead.convert")}</Button></div>
    </div>}
    {lead.status === "converted" && <p className="text-sm">
      {t(lang, "crm.lead.converted")} {lead.converted_client_id > 0 && <a className="underline underline-offset-4" href={`/clients/${lead.converted_client_id}`}>{t(lang, "crm.lead.openClient")}</a>}
    </p>}

    <LeadTimeline leadID={lead.id} lang={lang} csrf={csrf} />
  </div>
}

function Fact({ label, value }: { label: string; value: string }) {
  return <div className="grid gap-1"><dt className="text-muted-foreground">{label}</dt><dd className="[overflow-wrap:anywhere]">{value}</dd></div>
}

function LeadTimeline({ leadID, lang, csrf }: { leadID: number; lang: string; csrf: string }) {
  const [items, setItems] = React.useState<Activity[] | null>(null)
  const [kind, setKind] = React.useState<string>("note")
  const [body, setBody] = React.useState("")
  const [issue, setIssue] = React.useState<Issue | null>(null)
  const [busy, setBusy] = React.useState(false)

  const load = React.useCallback(async () => {
    try {
      const response = await apiJSON<{ activities: Activity[] }>(`/api/crm/activities?lead_id=${leadID}`, "GET", "")
      setItems(response.activities)
    } catch (cause) {
      setIssue(issueOf(cause, {}, t(lang, "err.generic")))
    }
  }, [leadID, lang])

  React.useEffect(() => { void load() }, [load])

  async function log(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setBusy(true)
    setIssue(null)
    try {
      await apiJSON("/api/crm/activities", "POST", csrf, { lead_id: leadID, kind, body: body.trim() })
      setBody("")
      await load()
    } catch (cause) {
      setIssue(issueOf(cause, { body: "body", kind: "kind" }, t(lang, "err.generic")))
    } finally {
      setBusy(false)
    }
  }

  return <section className="grid gap-3" aria-labelledby={`lead-${leadID}-timeline`}>
    <h3 id={`lead-${leadID}-timeline`} className="text-sm font-semibold">{t(lang, "crm.timeline")}</h3>
    <form onSubmit={log} className="grid gap-3 sm:max-w-md">
      <FormIssue issue={issue} />
      <div className="grid gap-2">
        <Label htmlFor={`lead-${leadID}-kind`}>{t(lang, "crm.kindLabel")}</Label>
        <Select value={kind} onValueChange={setKind}>
          <SelectTrigger id={`lead-${leadID}-kind`} className="w-full" style={{ fontSize: 16 }}><SelectValue /></SelectTrigger>
          <SelectContent>{activityKinds.map(item => <SelectItem key={item} value={item}>{t(lang, `crm.kind.${item}`)}</SelectItem>)}</SelectContent>
        </Select>
      </div>
      <div className="grid gap-2">
        <Label htmlFor={`lead-${leadID}-body`}>{t(lang, "crm.activityBody")}</Label>
        <Textarea id={`lead-${leadID}-body`} rows={3} value={body} onChange={event => setBody(event.target.value)} required maxLength={4000} aria-invalid={issue?.key === "body" || undefined} style={{ fontSize: 16 }} />
        <FieldIssue issue={issue} name="body" id={`lead-${leadID}-body-issue`} />
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
