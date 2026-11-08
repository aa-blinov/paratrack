import type { ReactNode } from "react"
import type { LucideIcon } from "lucide-react"

// One shape for every "nothing here yet" block: what the place is, why it is
// worth filling, and the one thing to do next. It is left-aligned on purpose,
// so it agrees with the card titles around it instead of reading as a second
// decision stacked on the first.
export function EmptyState({ icon: Icon, title, text, action, children }: {
  icon?: LucideIcon
  title: string
  text?: string
  action?: ReactNode
  children?: ReactNode
}) {
  return <div className="grid max-w-xl justify-items-start gap-2" data-empty-state>
    {Icon && <span className="flex size-9 items-center justify-center rounded-md bg-muted text-muted-foreground"><Icon aria-hidden="true" className="size-4" /></span>}
    <p className="font-medium">{title}</p>
    {text && <p className="text-sm text-muted-foreground">{text}</p>}
    {children}
    {action && <div className="mt-1">{action}</div>}
  </div>
}
