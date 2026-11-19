// The server answers a failed request with JSON {"error": "..."}, and older
// routes with a short HTML or text body. The person sees the message, not the
// markup or the braces around it.
export async function responseMessage(response: Response): Promise<string> {
  const body = (await response.text()).trim()
  if (!body) return response.statusText || "Request failed"
  try {
    const parsed = JSON.parse(body) as { error?: unknown }
    if (typeof parsed.error === "string" && parsed.error) return parsed.error
  } catch {
    // not JSON: fall through to the markup or text
  }
  const text = body.replace(/<[^>]*>/g, " ").replace(/\s+/g, " ").trim()
  return text || response.statusText || "Request failed"
}

// fetch rejects with a TypeError when the request never left the browser, and
// that message ("Failed to fetch") is English on every screen. The caller
// swaps it for a phrase of its own language.
export function isNetworkError(cause: unknown): boolean {
  return typeof cause === "object" && cause !== null && (cause as { name?: unknown }).name === "TypeError"
}
