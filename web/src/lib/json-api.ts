// One JSON call to the CRM and calendar API. The server answers a refusal with
// {"error": "...", "field": "..."}; the error keeps both so a form can mark the
// input at fault instead of printing one line for the whole screen.

export class ApiError extends Error {
  readonly field: string

  constructor(message: string, field = "") {
    super(message)
    this.name = "ApiError"
    this.field = field
  }
}

export async function apiJSON<T>(url: string, method: "GET" | "POST", csrf: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = { Accept: "application/json" }
  if (method !== "GET") headers["X-CSRF-Token"] = csrf
  if (body !== undefined) headers["Content-Type"] = "application/json"
  const response = await fetch(url, {
    method,
    credentials: "same-origin",
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (response.status === 204) return undefined as T
  const payload = await response.json().catch(() => ({})) as { error?: unknown; field?: unknown }
  if (!response.ok) {
    throw new ApiError(
      typeof payload.error === "string" ? payload.error : "",
      typeof payload.field === "string" ? payload.field : "",
    )
  }
  return payload as T
}

/** A refusal shown at the input it names, or at the top of the form when it names none. */
export type Issue = { key: string; message: string }

/**
 * Turns a failed call into an Issue. The server's field names are not the form's
 * input keys, so each form passes its own map; a field the map does not know
 * stays at form level under key "".
 */
export function issueOf(error: unknown, fields: Record<string, string>, fallback: string): Issue {
  if (error instanceof ApiError) return { key: fields[error.field] ?? "", message: error.message || fallback }
  return { key: "", message: fallback }
}
