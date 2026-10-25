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
  amount_cents: number
  currency: string
  probability: number
  close_date: string
  next_step_at: string
  lost_reason: string
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
