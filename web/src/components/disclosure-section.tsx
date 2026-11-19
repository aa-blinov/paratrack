import { Disclosure, DisclosureTrigger } from "@/components/ui/collapsible"
import { useState, type ReactNode } from "react"
import { ChevronDown } from "lucide-react"

export function DisclosureSection({ title, description, children, defaultOpen = false, id, className = "" }: {
  title: string
  description?: string
  children: ReactNode
  defaultOpen?: boolean
  id?: string
  className?: string
}) {
  // defaultOpen reads like a controlled "open" prop, but a disclosure must
  // answer its own trigger too. Without onOpenChange the internal copy is
  // the only state; with it, the parent owns the toggle. Both modes work
  // because Disclosure routes the trigger through onOpenChange either way.
  const [open, setOpen] = useState(defaultOpen)
  return <Disclosure id={id} open={open} onOpenChange={setOpen} className={`group min-w-0 rounded-[12px] border bg-card ${className}`}>
    <DisclosureTrigger className="flex cursor-pointer list-none items-start gap-3 p-4 marker:hidden focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
      <span className="min-w-0 flex-1"><span className="block font-medium">{title}</span>{description && <span className="mt-1 block text-sm text-muted-foreground">{description}</span>}</span>
      <ChevronDown aria-hidden="true" className="mt-0.5 size-4 shrink-0 transition-transform group-data-[state=open]:rotate-180" />
    </DisclosureTrigger>
    <div className="min-w-0 border-t p-4 sm:p-5">{children}</div>
  </Disclosure>
}
