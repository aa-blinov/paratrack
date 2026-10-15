import type { ReactNode } from "react"
import { Button } from "@/components/ui/button"

/**
 * The period picker — one control, one shape.
 *
 * It existed three times over, and they had drifted apart: statistics built
 * it out of raw links at 36px with a 4px corner and a near-black active
 * fill, "by hour" used the shadcn button at 40px with a soft brand tint, and
 * the timesheet hand-rolled a CSS class at 44px. Same control, three ways to
 * read it — and the statistics version was the heaviest of the three, which
 * is exactly backwards for something you tap on the way to a number.
 *
 * 44px everywhere: that is the height the timesheet already argued for and
 * the one a finger needs. Links and buttons both live here, because "pick a
 * period" is one idea whether it navigates or filters in place.
 */

export type SegmentedOption = {
  key: string
  label: string
  /** Navigates. Omit when the option filters in place instead. */
  href?: string
  onSelect?: () => void
}

export function SegmentedControl({ label, options, value, trailing, className }: {
  label: string
  options: SegmentedOption[]
  value: string
  trailing?: ReactNode
  className?: string
}) {
  return <nav aria-label={label} // On a phone the periods are one row that scrolls sideways: wrapped, six
  // periods took three rows of a 390px screen.
  className={`period-tabs flex min-w-0 max-w-full flex-nowrap items-center gap-1 overflow-x-auto rounded-lg border bg-card p-1 [scrollbar-width:none] max-lg:[mask-image:linear-gradient(to_right,#000_86%,transparent)] lg:flex-wrap ${className || ""}`}>
    {options.map(option => {
      const active = option.key === value
      const variant = active ? "secondary" : "ghost"
      return option.href
        ? <Button key={option.key} asChild size="sm" variant={variant} className="h-11 shrink-0"
            aria-current={active ? "page" : undefined}>
            <a href={option.href}>{option.label}</a>
          </Button>
        : <Button key={option.key} type="button" size="sm" variant={variant} className="h-11 shrink-0"
            aria-pressed={active} onClick={option.onSelect}>{option.label}</Button>
    })}
    {trailing}
  </nav>
}