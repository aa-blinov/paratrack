// The CRM JSON shapes (internal/web/crm_views.go). A deal's stage is the key of one
// of its workspace's stages; the server returns the pipeline in the order the board
// shows it, with each stage's name and kind.

export const activityKinds = ["call", "meeting", "note"] as const

/** What a stage means to the rules: open stages can go overdue, won and lost close the deal. */
export type StageKind = "open" | "won" | "lost"

export type Deal = {
  id: number
  title: string
  stage: string
  client_id: number
  contact_id: number
  owner_id: number
  /** Only a manager gets the amount and the currency; amount_visible says which viewer this is. */
  amount_visible: boolean
  /** Left out of the answer when the viewer is a member or the amount is zero. */
  amount_cents?: number
  currency?: string
  probability: number
  close_date: string
  next_step_at: string
  lost_reason: string
  /** The project the deal made when it was won; 0 until then. */
  project_id: number
  overdue: boolean
  updated_at: string
}

export type PipelineStage = {
  stage: string
  name: string
  kind: StageKind
  count: number
}

export type Pipeline = {
  stages: PipelineStage[]
  overdue: number
}

export type Contact = {
  id: number
  client_id: number
  name: string
  role: string
  email: string
  phone: string
}

export type Activity = {
  id: number
  client_id: number
  deal_id: number
  kind: string
  body: string
  result: string
  occurred_at: string
  author_id: number
}

// Deal fields as the server names them in a refusal, mapped to the form's keys.
export const dealFieldKeys: Record<string, string> = {
  "deal title": "title",
  "title": "title",
  "client": "client",
  "client or deal": "client",
  "amount": "amount",
  "probability": "probability",
  "close date": "close_date",
  "next step date": "next_step_at",
}

// Leads (CRM-14). A lead is an enquiry before it is a client and a deal. Converted is set
// only by converting, so a person picks among the other four.
export const leadStatuses = ["new", "contacted", "qualified", "lost", "converted"] as const

export type LeadStatus = (typeof leadStatuses)[number]

export type Lead = {
  id: number
  company: string
  contact_name: string
  phone: string
  email: string
  source: string
  message: string
  /** 0 means nobody has taken the lead, which is how an enquiry from the website starts. */
  owner_id: number
  status: LeadStatus
  lost_reason: string
  next_step_at: string
  converted_deal_id: number
  converted_client_id: number
  created_at: string
  updated_at: string
  /** Minutes from creation to the first activity; 0 while there is none. */
  response_minutes: number
  /** Minutes since creation while the lead has no activity; 0 once it has one. */
  waiting_minutes: number
  /** The first activity came within the two-hour target. */
  within_target: boolean
}

/**
 * How a lead's first response stands (CRM-16): answered within the target, answered later, or
 * still waiting. A late answer always takes more than zero minutes, so a lead that is not within
 * the target with no response above zero is waiting.
 */
export type ResponseState =
  | { kind: "inTime"; minutes: number }
  | { kind: "late"; minutes: number }
  | { kind: "waiting"; minutes: number }

export function responseState(lead: Lead): ResponseState {
  if (lead.within_target) return { kind: "inTime", minutes: lead.response_minutes }
  if (lead.response_minutes > 0) return { kind: "late", minutes: lead.response_minutes }
  return { kind: "waiting", minutes: lead.waiting_minutes }
}

/** Lead fields as the server names them in a refusal. A name that is not an input stays at form level. */
export const leadFieldKeys: Record<string, string> = {
  company: "company",
  contact_name: "contact_name",
  phone: "phone",
  email: "email",
  source: "source",
  message: "message",
  owner_id: "owner",
  status: "status",
  lost_reason: "lost_reason",
}

/** Whether a lead can still be converted: converted and lost leads are closed. */
export function leadIsOpen(lead: Lead): boolean {
  return lead.status !== "converted" && lead.status !== "lost"
}

/**
 * The request the company site sends for an enquiry, with the key a manager just made. The
 * origin is the page's own, so the example points at the server the person is looking at.
 */
export function intakeExample(origin: string, key: string): string {
  return [
    `curl -X POST ${origin}/api/leads/intake \\`,
    `  -H "Content-Type: application/json" \\`,
    `  -H "X-Intake-Key: ${key}" \\`,
    `  -d '{"company":"Ромашка","contact_name":"Анна","email":"anna@example.test","source":"website","message":"Нужен сайт"}'`,
  ].join("\n")
}

/** One currency's sum of deal amounts. A total never adds two currencies together. */
export type FunnelAmount = { currency: string; amount_cents: number }

/** Counted deals, with their amounts per currency only when the viewer is a manager. */
export type FunnelTotal = { count: number; amounts?: FunnelAmount[] }

export type FunnelStage = FunnelTotal & { stage: string; name: string; kind: StageKind }

/** The funnel of a period (CRM-17). Amounts come only when amounts_visible is set. */
export type FunnelReport = {
  from: string
  to: string
  today: string
  amounts_visible: boolean
  pipeline: FunnelStage[]
  won: FunnelTotal
  lost: FunnelTotal
  /** Won over won plus lost in the period; null when nothing was closed. */
  conversion: number | null
  overdue: FunnelTotal
  leads: {
    statuses: { status: LeadStatus; count: number }[]
    created: number
    converted: number
    /** Converted over created, of the leads the period created; null when there were none. */
    conversion: number | null
    sources: { source: string; label: string; count: number }[]
  }
  response: {
    target_minutes: number
    created: number
    answered: number
    within_target: number
    median_minutes: number | null
  }
}

/** The period a funnel opens on: the first day of this month up to today, in the browser's zone. */
export function defaultFunnelPeriod(now: Date): { from: string; to: string } {
  const pad = (n: number) => String(n).padStart(2, "0")
  const day = (value: Date) => `${value.getFullYear()}-${pad(value.getMonth() + 1)}-${pad(value.getDate())}`
  return { from: day(new Date(now.getFullYear(), now.getMonth(), 1)), to: day(now) }
}

/** A rate as a whole percentage. */
export function percentLabel(ratio: number): string {
  return `${Math.round(ratio * 100)}%`
}
