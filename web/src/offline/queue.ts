// The durable offline mutation queue: storage, validation and body encoding.
// The storage key matches the one the earlier build wrote, so actions queued
// before this build are still sent after it.
export const QUEUE_KEY = "paratrack-offline-queue"

export type FormEntries = [string, string][]

export type QueueItem =
  | { verb: string; url: string; ts?: number; encoding: "json"; body: Record<string, unknown> }
  | { verb: string; url: string; ts?: number; encoding: "form"; body: FormEntries }

export function readQueue(storage: Storage | null): QueueItem[] {
  if (!storage) return []
  try {
    const value: unknown = JSON.parse(storage.getItem(QUEUE_KEY) || "[]")
    return Array.isArray(value) ? value.filter(isValidQueueItem) : []
  } catch {
    return []
  }
}

export function writeQueue(storage: Storage | null, items: QueueItem[]): boolean {
  if (!storage) return false
  try {
    storage.setItem(QUEUE_KEY, JSON.stringify(items))
    return true
  } catch {
    return false
  }
}

export function isValidQueueItem(item: unknown): item is QueueItem {
  if (!item || typeof item !== "object" || Array.isArray(item)) return false
  const candidate = item as Record<string, unknown>
  if (typeof candidate.verb !== "string" || typeof candidate.url !== "string") return false
  if (candidate.ts !== undefined && typeof candidate.ts !== "number") return false
  if (candidate.encoding === "json") {
    return candidate.body !== null && typeof candidate.body === "object" && !Array.isArray(candidate.body)
  }
  return candidate.encoding === "form" && Array.isArray(candidate.body) &&
    candidate.body.every(entry => Array.isArray(entry) && entry.length === 2 && entry.every(value => typeof value === "string"))
}

// Same-origin requests only: returns the path and query, or null.
export function sameOriginPath(rawURL: string, pageURL: string = window.location.href): string | null {
  try {
    const page = new URL(pageURL)
    const url = new URL(rawURL, page)
    if (url.origin !== page.origin) return null
    return url.pathname + url.search
  } catch {
    return null
  }
}

function formEntries(body: unknown): FormEntries | null {
  if (body == null) return []
  if (body instanceof URLSearchParams) return [...body.entries()]
  if (typeof FormData !== "undefined" && body instanceof FormData) {
    const entries = [...body.entries()]
    return entries.every(([, value]) => typeof value === "string") ? entries as FormEntries : null
  }
  if (typeof body === "string") return [...new URLSearchParams(body).entries()]
  if (Object.getPrototypeOf(body) === Object.prototype) {
    return Object.entries(body as Record<string, unknown>).flatMap(([key, value]) =>
      (Array.isArray(value) ? value : [value]).map(item => [key, String(item)] as [string, string]))
  }
  return null
}

export function encodeRequestBody(
  body: unknown, contentType: string,
): { encoding: "json"; body: Record<string, unknown> } | { encoding: "form"; body: FormEntries } | null {
  if (/(?:^|\s|;)application\/(?:[\w.+-]*\+)?json(?:\s*;|$)/i.test(contentType)) {
    try {
      const value: unknown = typeof body === "string" ? JSON.parse(body) : body
      if (value && typeof value === "object" && !Array.isArray(value)) {
        return { encoding: "json", body: value as Record<string, unknown> }
      }
    } catch {
      // not JSON we can replay
    }
    return null
  }
  const entries = formEntries(body)
  return entries === null ? null : { encoding: "form", body: entries }
}
