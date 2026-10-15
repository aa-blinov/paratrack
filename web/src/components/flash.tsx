// The server's one-line answer to a form (?flash=…). One look on every page:
// it used to be a framed note in settings and bare text elsewhere, where
// «Проект создан.» read like a stray line of the page.
export function Flash({ message, ok }: { message: string; ok: boolean }) {
  if (!message) return null
  return <p role={ok ? "status" : "alert"} className={`rounded-md border p-3 text-sm ${ok ? "border-border" : "border-destructive/40 text-destructive"}`}>{message}</p>
}
