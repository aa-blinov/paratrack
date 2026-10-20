import { useEffect, useRef, useState, type ReactNode } from "react"
import type * as React from "react"
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

// Links that switch the page's language or theme are a choice, not a page:
// they are marked aria-current="true", never "page".
export type SegmentedOption = {
  key: string
  label: string
  /** Navigates. Omit when the option filters in place instead. */
  href?: string
  onSelect?: () => void
}

export function SegmentedControl({ label, options, value, trailing, className, currentAs = "page" }: {
  label: string
  options: SegmentedOption[]
  value: string
  trailing?: ReactNode
  className?: string
  currentAs?: "page" | "true"
}) {
  // The fade says "more to the right", so it shows only while there is more:
  // drawn always, it faded the last tab of a row that fit.
  const ref = useRef<HTMLElement>(null)
  const [more, setMore] = useState(false)
  useEffect(() => {
    const el = ref.current
    if (!el) return
    const check = () => setMore(el.scrollLeft + el.clientWidth < el.scrollWidth - 1)
    check()
    el.addEventListener("scroll", check, { passive: true })
    const observer = new ResizeObserver(check)
    observer.observe(el)
    return () => { el.removeEventListener("scroll", check); observer.disconnect() }
  }, [])
  // Links between periods are navigation; buttons that filter or set a
  // preference (the theme) are a group of controls, not a landmark.
  const Root = options.some(option => option.href) ? "nav" : "div"
  return <Root ref={ref as React.RefObject<HTMLDivElement>} role={Root === "div" ? "group" : undefined} aria-label={label} // On a phone the periods are one row that scrolls sideways: wrapped, six
  // periods took three rows of a 390px screen.
  className={`period-tabs flex min-w-0 max-w-full flex-nowrap items-center gap-1 overflow-x-auto rounded-full bg-muted p-1 [scrollbar-width:none] ${more ? "max-lg:[mask-image:linear-gradient(to_right,#000_86%,transparent)]" : ""} lg:flex-wrap ${className || ""}`}>
    {options.map(option => {
      const active = option.key === value
      const variant = active ? "secondary" : "ghost"
      return option.href
        ? <Button key={option.key} asChild variant={variant} className="shrink-0"
            aria-current={active ? currentAs : undefined}>
            <a href={option.href}>{option.label}</a>
          </Button>
        : <Button key={option.key} type="button" variant={variant} className="shrink-0"
            aria-pressed={active} onClick={option.onSelect}>{option.label}</Button>
    })}
    {trailing}
  </Root>
}
