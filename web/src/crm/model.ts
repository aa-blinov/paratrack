// The CRM JSON shapes (internal/web/crm_views.go). Stages are fixed by the server
// and listed here in the order the board shows them.

export const dealStages = ["new", "working", "proposal", "won", "lost"] as const
export const activityKinds = ["call", "meeting", "note"] as const

export type Deal = {
  id: number
  title: string
  stage: string
  client_id: number
  contact_id: number
  owner_id: number
  amount_cents: number
  currency: string
  probability: number
  close_date: string
  next_step_at: string
  lost_reason: string
  overdue: boolean
  updated_at: string
}

export type Pipeline = {
  stages: { stage: string; count: number }[]
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
