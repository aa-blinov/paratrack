import { useEffect, useState } from "react"
import type { ToastDetail } from "./controller"

type Toast = ToastDetail & { id: number }

const LIFETIME_MS = 4500
let nextId = 1

// Short confirmations after an action ("Saved", "Added"). The live region stays
// in the page; each message leaves on its own.
export function ToastStack() {
  const [toasts, setToasts] = useState<Toast[]>([])
  useEffect(() => {
    const show = (event: Event) => {
      const detail = (event as CustomEvent<ToastDetail>).detail
      const id = nextId++
      setToasts(current => [...current.slice(-2), { ...detail, id }])
      window.setTimeout(() => setToasts(current => current.filter(toast => toast.id !== id)), LIFETIME_MS)
    }
    window.addEventListener("paratrack:toast", show)
    return () => window.removeEventListener("paratrack:toast", show)
  }, [])
  return <div role="status" aria-live="polite" className="pointer-events-none fixed bottom-24 left-1/2 z-50 flex -translate-x-1/2 flex-col gap-2 lg:bottom-6">
    {toasts.map(toast => <div key={toast.id} data-toast={toast.kind}
      className={`rounded-md border px-4 py-2 text-sm shadow-md ${toast.kind === "error" ? "border-destructive/40 bg-background text-destructive" : "border-border bg-background text-foreground"}`}>
      {toast.message}
    </div>)}
  </div>
}
