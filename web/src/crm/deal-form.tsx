import * as React from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { DateField } from "@/components/date-field"
import { FieldIssue, FormIssue } from "@/components/form-issue"
import { StatusLine } from "@/components/status-line"
import { translate as t } from "@/i18n"
import { apiJSON, issueOf, type Issue } from "@/lib/json-api"
import { centsInput, parseMoneyCents } from "@/lib/money"
import { memberLabel, type Client, type Member } from "@/lib/team-directory"
import { dealFieldKeys, type Deal } from "@/crm/model"

const noClient = "0"

type Props = {
  lang: string
  csrf: string
  viewerID: number
  canManage: boolean
  clients: Client[]
  members: Member[]
  /** Given, the form edits this deal and sends only the fields that changed. */
  deal?: Deal
  onSaved: () => void
}

export function DealForm({ lang, csrf, viewerID, canManage, clients, members, deal, onSaved }: Props) {
  const prefix = deal ? `deal-${deal.id}` : "deal-new"
  // A new deal has no answer to read the flag from, so the page's manager flag decides, as it does for the owner.
  const showAmount = deal ? deal.amount_visible : canManage
  const [title, setTitle] = React.useState(deal?.title ?? "")
  const [clientID, setClientID] = React.useState(String(deal?.client_id || noClient))
  const [owner, setOwner] = React.useState(String(deal?.owner_id || viewerID))
  const [amount, setAmount] = React.useState(centsInput(deal?.amount_cents ?? 0))
  const [probability, setProbability] = React.useState(String(deal?.probability ?? 0))
  const [closeDate, setCloseDate] = React.useState(deal?.close_date ?? "")
  const [nextStep, setNextStep] = React.useState(deal?.next_step_at ?? "")
  // A date that is typed but cannot be read blocks the save and is named. The
  // field would otherwise report an empty value, which is a different thing.
  const [broken, setBroken] = React.useState<Record<string, boolean>>({})
  const [issue, setIssue] = React.useState<Issue | null>(null)
  const [busy, setBusy] = React.useState(false)
  const [saved, setSaved] = React.useState(false)
  // The date fields keep their own text, so a reset from a saved create starts
  // them afresh by changing their key.
  const [epoch, setEpoch] = React.useState(0)

  const invalid = (key: string) => issue?.key === key || undefined
  const described = (key: string) => issue?.key === key ? `${prefix}-${key}-issue` : undefined

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setSaved(false)
    const cents = parseMoneyCents(amount)
    if (showAmount && cents === null) {
      setIssue({ key: "amount", message: t(lang, "err.invalidInput") })
      return
    }
    const probabilityValue = probability.trim() === "" ? 0 : Number(probability)
    if (!Number.isInteger(probabilityValue) || probabilityValue < 0 || probabilityValue > 100) {
      setIssue({ key: "probability", message: t(lang, "err.invalidInput") })
      return
    }
    const brokenKey = Object.keys(broken).find(key => broken[key])
    if (brokenKey) {
      setIssue({ key: brokenKey, message: t(lang, "err.invalidInput") })
      return
    }

    // A member's form never names the amount, so the server keeps the one a manager set.
    const values: Record<string, unknown> = {
      title: title.trim(),
      client_id: Number(clientID),
      probability: probabilityValue,
      close_date: closeDate,
      next_step_at: nextStep,
    }
    if (showAmount) values.amount_cents = cents
    const current: Record<string, unknown> = deal
      ? { title: deal.title, client_id: deal.client_id, amount_cents: deal.amount_cents ?? 0, probability: deal.probability, close_date: deal.close_date, next_step_at: deal.next_step_at }
      : {}
    const body: Record<string, unknown> = {}
    for (const [key, value] of Object.entries(values)) {
      if (!deal || value !== current[key]) body[key] = value
    }
    // The server already makes the caller the owner of a new deal, so the owner
    // is sent only when a manager picks someone else.
    const ownerID = Number(owner)
    if (canManage && ownerID !== (deal ? deal.owner_id : viewerID)) body.owner_id = ownerID

    setBusy(true)
    setIssue(null)
    try {
      if (deal) {
        await apiJSON(`/api/crm/deals/${deal.id}`, "POST", csrf, body)
        setSaved(true)
      } else {
        await apiJSON("/api/crm/deals", "POST", csrf, body)
        setTitle("")
        setClientID(noClient)
        setAmount("")
        setProbability("0")
        setCloseDate("")
        setNextStep("")
        setBroken({})
        setEpoch(value => value + 1)
      }
      onSaved()
    } catch (cause) {
      setIssue(issueOf(cause, dealFieldKeys, t(lang, "err.generic")))
    } finally {
      setBusy(false)
    }
  }

  return <form onSubmit={submit} className="grid gap-4" aria-busy={busy}>
    <FormIssue issue={issue} />
    <div className="grid gap-4 sm:grid-cols-2">
      <div className="grid gap-2 sm:col-span-2">
        <Label htmlFor={`${prefix}-title`}>{t(lang, "crm.title")}</Label>
        <Input id={`${prefix}-title`} value={title} onChange={event => setTitle(event.target.value)} required maxLength={200} aria-invalid={invalid("title")} aria-describedby={described("title")} />
        <FieldIssue issue={issue} name="title" id={`${prefix}-title-issue`} />
      </div>
      <div className="grid gap-2">
        <Label htmlFor={`${prefix}-client`}>{t(lang, "crm.client")}</Label>
        <Select value={clientID} onValueChange={setClientID}>
          <SelectTrigger id={`${prefix}-client`} className="w-full" aria-invalid={invalid("client")} aria-describedby={described("client")} style={{ fontSize: 16 }}><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value={noClient}>{t(lang, "crm.noClient")}</SelectItem>
            {clients.map(client => <SelectItem key={client.id} value={String(client.id)}>{client.name}</SelectItem>)}
          </SelectContent>
        </Select>
        <FieldIssue issue={issue} name="client" id={`${prefix}-client-issue`} />
      </div>
      {canManage && <div className="grid gap-2">
        <Label htmlFor={`${prefix}-owner`}>{t(lang, "crm.owner")}</Label>
        <Select value={owner} onValueChange={setOwner}>
          <SelectTrigger id={`${prefix}-owner`} className="w-full" aria-invalid={invalid("owner")} aria-describedby={described("owner")} style={{ fontSize: 16 }}><SelectValue /></SelectTrigger>
          <SelectContent>
            {members.map(member => <SelectItem key={member.id} value={String(member.id)}>{memberLabel(member)}</SelectItem>)}
          </SelectContent>
        </Select>
        <FieldIssue issue={issue} name="owner" id={`${prefix}-owner-issue`} />
      </div>}
      {showAmount && <div className="grid gap-2">
        <Label htmlFor={`${prefix}-amount`}>{t(lang, "crm.amount")}</Label>
        <Input id={`${prefix}-amount`} inputMode="decimal" value={amount} onChange={event => setAmount(event.target.value)} placeholder="0" aria-invalid={invalid("amount")} aria-describedby={described("amount")} style={{ fontSize: 16 }} />
        <FieldIssue issue={issue} name="amount" id={`${prefix}-amount-issue`} />
      </div>}
      <div className="grid gap-2">
        <Label htmlFor={`${prefix}-probability`}>{t(lang, "crm.probability")}</Label>
        <Input id={`${prefix}-probability`} type="number" inputMode="numeric" min={0} max={100} step={1} value={probability} onChange={event => setProbability(event.target.value)} aria-invalid={invalid("probability")} aria-describedby={described("probability")} style={{ fontSize: 16 }} />
        <FieldIssue issue={issue} name="probability" id={`${prefix}-probability-issue`} />
      </div>
      <div className="grid gap-2">
        <Label htmlFor={`${prefix}-close`}>{t(lang, "crm.closeDate")}</Label>
        <DateField key={`close-${epoch}`} id={`${prefix}-close`} ariaLabel={t(lang, "crm.closeDate")} defaultValue={closeDate} style={{ fontSize: 16 }} onValueChange={setCloseDate} onBrokenChange={value => setBroken(previous => ({ ...previous, close_date: value }))} />
        <FieldIssue issue={issue} name="close_date" id={`${prefix}-close_date-issue`} />
      </div>
      <div className="grid gap-2">
        <Label htmlFor={`${prefix}-next`}>{t(lang, "crm.nextStep")}</Label>
        <DateField key={`next-${epoch}`} id={`${prefix}-next`} ariaLabel={t(lang, "crm.nextStep")} defaultValue={nextStep} style={{ fontSize: 16 }} onValueChange={setNextStep} onBrokenChange={value => setBroken(previous => ({ ...previous, next_step_at: value }))} />
        <FieldIssue issue={issue} name="next_step_at" id={`${prefix}-next_step_at-issue`} />
      </div>
    </div>
    <div className="flex flex-wrap items-center gap-3">
      <Button type="submit" disabled={busy}>{deal ? t(lang, "profile.save") : t(lang, "crm.createDeal")}</Button>
      <StatusLine text={saved ? t(lang, "crm.saved") : ""} />
    </div>
  </form>
}
