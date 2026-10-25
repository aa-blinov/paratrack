// The clients and members a CRM or calendar form picks from. The members list is
// the managers' view: the server answers a member with 403, so callers ask only
// when the page says the person manages the workspace.

import { apiJSON } from "@/lib/json-api"

export type Client = { id: number; name: string; currency: string; archived: boolean }
export type Member = { id: number; name: string; email: string; role: string }

export async function loadClients(): Promise<Client[]> {
  const body = await apiJSON<{ clients: Client[] }>("/api/v1/clients", "GET", "")
  return body.clients
}

export async function loadMembers(): Promise<Member[]> {
  const body = await apiJSON<{ members: Member[] }>("/api/v1/members", "GET", "")
  return body.members
}

export function memberLabel(member: Member): string {
  return member.name || member.email
}
