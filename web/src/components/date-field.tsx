import * as React from "react"
import { createPortal } from "react-dom"
import { CalendarDays, ChevronLeft, ChevronRight } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { translate as t } from "@/i18n"
import {
  firstWeekday, formatDate, formatDateTime, monthLabel, parseDateInput,
  parseDateTimeInput, parseISODate, weekdayLabels,
} from "@/lib/date-format"

/**
 * A date field that speaks the interface's language.
 *
 * The browser draws `<input type="date">` in *its* locale and no attribute
 * moves it, so on an en-US machine a Russian screen offered «10/13/2026».
 * This renders the text itself, in the language of the screen, and keeps a
 * hidden input carrying the same ISO string the server has always received:
 * handlers, queries and stored dates are untouched by this component.
 *
 * Typing is forgiving — the screen's own order, and ISO either way, because a
 * value pasted from a URL is ISO. An unparseable field submits nothing rather
 * than a stale date, so a half-typed date can never become an invoice period.
 */

const pad = (n: number) => String(n).padStart(2, "0")

type Kind = "date" | "datetime"

type FieldProps = {
  id?: string
  name?: string
  /** ISO `YYYY-MM-DD`, or `YYYY-MM-DDTHH:MM` for the time variant. */
  defaultValue?: string
  required?: boolean
  disabled?: boolean
  className?: string
  /**
   * Inline styles for the text field. The project theme sizes `--text-base` at
   * 14px, so anything that must stay at 16px on a phone — iOS zooms the page
   * on focus below that — has to say so here.
   */
  style?: React.CSSProperties
  ariaLabel?: string
  autoFocus?: boolean
  onValueChange?: (iso: string) => void
  /**
   * Fired when the typed text stops being a date, and again when it becomes
   * one. An empty value means both "no date" and "a date that could not be
   * read", so a form that saves on this field needs this to tell them apart.
   */
  onBrokenChange?: (broken: boolean) => void
  /**
   * Fired when the field is done with: blur, Enter, or a day picked from the
   * calendar. The session editor saves on this, exactly as it saved when the
   * native control reported a blur.
   */
  // Receives the value to save: a pick commits the day it just chose, which is
  // not yet in the caller's state when the callback runs.
  onCommit?: (iso: string) => void
}

function todayISO(): string {
  const now = new Date()
  return `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}`
}

function MonthGrid({ lang, selected, onPick }: { lang: string; selected: string; onPick: (iso: string) => void }) {
  const [cursor, setCursor] = React.useState(() => {
    const start = parseISODate(selected) || parseISODate(todayISO())!
    return { year: start.y, month: start.m - 1 }
  })
  // Following the field: picking elsewhere (or typing a date) moves the grid.
  React.useEffect(() => {
    const picked = parseISODate(selected)
    if (picked) setCursor({ year: picked.y, month: picked.m - 1 })
  }, [selected])

  const shift = (step: number) => setCursor(current => {
    const m = current.month + step
    return { year: current.year + Math.floor(m / 12), month: ((m % 12) + 12) % 12 }
  })

  const lead = (new Date(Date.UTC(cursor.year, cursor.month, 1)).getUTCDay() - firstWeekday(lang) + 7) % 7
  const daysInMonth = new Date(Date.UTC(cursor.year, cursor.month + 1, 0)).getUTCDate()
  const today = todayISO()

  // Focus follows the arrows, not the tab order: the day buttons take no tab
  // stops of their own, and the one the keyboard sits on is focused by hand.
  const [pending, setPending] = React.useState("")
  const grid = React.useRef<HTMLDivElement>(null)
  React.useLayoutEffect(() => {
    if (!pending) return
    grid.current?.querySelector<HTMLButtonElement>(`button[data-day="${pending}"]`)?.focus()
    setPending("")
  }, [pending])

  // A step lands on a real date, so it can cross the month edge: the grid
  // follows the focus there instead of stopping at the border the eye sees.
  const stepTo = (iso: string, days: number) => {
    const from = parseISODate(iso)!
    const next = new Date(Date.UTC(from.y, from.m - 1, from.d + days))
    setCursor({ year: next.getUTCFullYear(), month: next.getUTCMonth() })
    setPending(`${next.getUTCFullYear()}-${pad(next.getUTCMonth() + 1)}-${pad(next.getUTCDate())}`)
  }

  // Enter and Space stay with the button, which picks its day natively; Esc
  // is answered by the field's own document listener.
  const onGridKey = (event: React.KeyboardEvent) => {
    const day = event.target instanceof Element ? (event.target as Element).closest<HTMLElement>("[data-day]") : null
    if (!day?.dataset.day) return
    const steps: Record<string, number> = { ArrowLeft: -1, ArrowRight: 1, ArrowUp: -7, ArrowDown: 7 }
    const step = steps[event.key]
    if (step === undefined) return
    event.preventDefault()
    stepTo(day.dataset.day, step)
  }

  // Weeks as rows, so the calendar can be a grid: a screen reader hears rows
  // and columns, and the blanks keep the 1st on its own weekday.
  const weeks: Array<Array<string | null>> = []
  let row: Array<string | null> = Array.from({ length: lead }, () => null)
  for (let day = 1; day <= daysInMonth; day += 1) {
    row.push(`${cursor.year}-${pad(cursor.month + 1)}-${pad(day)}`)
    if (row.length === 7) { weeks.push(row); row = [] }
  }
  if (row.length) {
    while (row.length < 7) row.push(null)
    weeks.push(row)
  }

  return <div className="w-64 rounded-lg border bg-card p-3 shadow-md">
    <div className="mb-2 flex items-center justify-between">
      <Button type="button" variant="ghost" size="icon" aria-label={t(lang, "date.prevMonth")} onClick={() => shift(-1)}>
        <ChevronLeft aria-hidden="true" className="size-4" />
      </Button>
      <span className="text-sm font-medium"><span className="capitalize">{monthLabel(cursor.month, lang)}</span> {cursor.year}</span>
      <Button type="button" variant="ghost" size="icon" aria-label={t(lang, "date.nextMonth")} onClick={() => shift(1)}>
        <ChevronRight aria-hidden="true" className="size-4" />
      </Button>
    </div>
    <div className="mb-1 grid grid-cols-7 gap-0.5 text-center text-xs text-muted-foreground">
      {weekdayLabels(lang).map(label => <span key={label}>{label}</span>)}
    </div>
    <div ref={grid} role="grid" aria-label={t(lang, "date.calendar")} className="grid gap-0.5" onKeyDown={onGridKey}>
      {weeks.map((week, rowIndex) => <div role="row" key={rowIndex} className="grid grid-cols-7 gap-0.5">
        {week.map((iso, cellIndex) => iso === null
          ? <span role="gridcell" aria-hidden="true" key={`blank-${rowIndex}-${cellIndex}`} />
          : <div role="gridcell" key={iso}><button
              type="button"
              data-day={iso}
              tabIndex={-1}
              onClick={() => onPick(iso)}
              aria-pressed={iso === selected}
              aria-current={iso === today ? "date" : undefined}
              className={`h-8 rounded-md text-sm tabular-nums hover:bg-muted ${iso === selected ? "bg-primary text-primary-foreground" : ""} ${iso === today && iso !== selected ? "font-semibold text-foreground underline underline-offset-2" : ""}`}
            >{Number(iso.slice(8))}</button></div>)}
      </div>)}
    </div>
    <div className="mt-2 flex justify-between border-t pt-2">
      <Button type="button" variant="ghost" size="sm" onClick={() => onPick(todayISO())}>{t(lang, "date.today")}</Button>
      <Button type="button" variant="ghost" size="sm" onClick={() => onPick("")}>{t(lang, "date.clear")}</Button>
    </div>
  </div>
}

function DateControl({ kind, fieldProps }: { kind: Kind; fieldProps: FieldProps }) {
  const { id, name, defaultValue = "", required, disabled, className, style, ariaLabel, autoFocus, onValueChange, onBrokenChange, onCommit } = fieldProps
  const parse = kind === "date" ? parseDateInput : parseDateTimeInput
  const show = kind === "date" ? formatDate : formatDateTime

  const root = React.useRef<HTMLDivElement>(null)
  const panel = React.useRef<HTMLDivElement>(null)
  const input = React.useRef<HTMLInputElement>(null)
  const [open, setOpen] = React.useState(false)
  const [anchor, setAnchor] = React.useState<{ top: number; left: number } | null>(null)
  // Every page sets <html lang>, and a prop here would be one more thing a
  // screen could forget to pass.
  const lang = typeof document !== "undefined" ? document.documentElement.lang === "en" ? "en" : "ru" : "ru"

  const [iso, setISO] = React.useState(defaultValue)
  const [text, setText] = React.useState(() => show(defaultValue, lang))
  const [broken, setBroken] = React.useState(false)

  // The card paints with overflow-hidden, so a grid rendered inside it loses
  // its bottom half. It goes on the React root instead and is placed against
  // the field, following it while the page scrolls.
  //
  // The portal target is the React root, not <body>: the interface CSS is
  // scoped under #paratrack-react-root, so a grid sent to the body would come
  // out unstyled and transparent.
  const portalRoot = typeof document !== "undefined"
    ? document.getElementById("paratrack-react-root")
    : null
  const place = React.useCallback(() => {
    const box = root.current?.getBoundingClientRect()
    if (!box) return
    const width = 256
    // The panel's real height once it is on the page; the guess only serves
    // the first frame, before the panel exists.
    const height = panel.current?.offsetHeight || 296
    const left = Math.min(Math.max(8, box.left), window.innerWidth - width - 8)
    // Near the bottom of the window the grid opens upwards rather than below,
    // where half of it would hang off the screen.
    const below = box.bottom + 4
    const fits = below + height <= window.innerHeight - 8
    const top = fits ? below : Math.max(8, box.top - height - 4)
    // Same place as before: no state change, so the placement does not loop.
    setAnchor(previous => previous && previous.top === top && previous.left === left ? previous : { top, left })
  }, [])

  // Once the panel is on the page its height is known: place it again from it.
  React.useLayoutEffect(() => {
    if (open && anchor) place()
  }, [open, anchor, place])

  React.useEffect(() => {
    if (!open) return
    place()
    window.addEventListener("scroll", place, true)
    window.addEventListener("resize", place)
    return () => {
      window.removeEventListener("scroll", place, true)
      window.removeEventListener("resize", place)
    }
  }, [open, place])

  React.useEffect(() => {
    const onDown = (event: MouseEvent) => {
      const target = event.target as Node
      if (root.current?.contains(target) || panel.current?.contains(target)) return
      setOpen(false)
    }
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return
      // Esc from the grid hands the focus back to the field the panel came
      // from, instead of dropping the keyboard on <body>.
      const inside = panel.current?.contains(document.activeElement) === true
      setOpen(false)
      if (inside) input.current?.focus()
    }
    // Tabbing on past the field closes its grid: two open grids used to sit
    // over the form while the focus was already on the submit button. The
    // check is the whole field-plus-panel container, so the focus the grid
    // itself takes (below) keeps the panel open.
    const onFocus = (event: FocusEvent) => {
      const target = event.target as Node
      if (root.current?.contains(target) || panel.current?.contains(target)) return
      setOpen(false)
    }
    document.addEventListener("mousedown", onDown)
    document.addEventListener("keydown", onKey)
    document.addEventListener("focusin", onFocus)
    return () => {
      document.removeEventListener("mousedown", onDown)
      document.removeEventListener("keydown", onKey)
      document.removeEventListener("focusin", onFocus)
    }
  }, [])

  // Switching the interface language re-spells the text without touching it.
  React.useEffect(() => { setText(show(iso, lang)) }, [lang]) // eslint-disable-line react-hooks/exhaustive-deps

  const accept = (next: string) => {
    setISO(next)
    onValueChange?.(next)
  }

  // The portal sits at the end of the React root, so the browser's own tab
  // order reaches it only after the whole form — long after the focusin
  // above has closed it. Tab and ↓ walk into the grid by hand instead, onto
  // the chosen day, or today when nothing is chosen yet.
  const enterGrid = () => {
    const buttons = panel.current?.querySelectorAll<HTMLButtonElement>("button[data-day]")
    if (!buttons || !buttons.length) return
    const wanted = iso.slice(0, 10) || todayISO()
    const hit = [...buttons].find(button => button.dataset.day === wanted) ?? buttons[0]
    hit.focus()
  }

  return <div className="relative w-full min-w-0" ref={root}>
    {/* The form posts exactly what it always posted. */}
    <input type="hidden" name={name} value={iso} disabled={disabled} />
    {/* The calendar is a trailing icon inside the field, not a second control beside it:
        a round button next to a 12dp field read as two objects. */}
    <div className="relative flex min-w-0 items-center">
      <Input
        id={id}
        ref={input}
        type="text"
        inputMode="numeric"
        autoComplete="off"
        autoFocus={autoFocus}
        required={required}
        disabled={disabled}
        aria-label={ariaLabel}
        aria-invalid={broken || undefined}
        data-date-input={name || id || ""}
        value={text}
        style={style}
        // The format, not a sample date: «02.01.2000» in two empty filter
        // fields read as a range already set.
        placeholder={t(lang, kind === "date" ? "date.formatHint" : "date.formatHintTime")}
        onChange={event => {
          const raw = event.target.value
          setText(raw)
          // Typing means "finish this by hand": the grid steps aside, so it
          // cannot sit on top of the button the reader is heading for.
          setOpen(false)
          const parsed = parse(raw, lang)
          // The text stays put so a half-finished date can be completed, but
          // nothing goes to the form: a value that could not be read must never
          // travel on as an invoice period.
          const broken = parsed === null && raw.trim().length > 0
          setBroken(broken)
          onBrokenChange?.(broken)
          accept(parsed === null ? "" : parsed)
        }}
        onBlur={() => onCommit?.(iso)}
        // A click or Enter opens the grid, a Tab passing through does not:
        // on a phone the grid opened with the keyboard and hid the next field.
        // Once it is open, Tab and ↓ step into it — the browser would take
        // the Tab through the whole form first and close it on the way.
        onClick={() => setOpen(true)}
        onKeyDown={event => {
          if (event.key === "Enter" && !open) { setOpen(true); event.preventDefault(); return }
          if (!open) return
          if (event.key === "ArrowDown" || (event.key === "Tab" && !event.shiftKey)) { event.preventDefault(); enterGrid() }
        }}
        className={`min-w-0 flex-1 pe-11 ${broken ? "border-destructive focus-visible:ring-destructive/40" : ""} ${className || ""}`}
      />
      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
        className="date-field-trailing end-1 top-1/2 -translate-y-1/2"
        disabled={disabled}
        aria-label={t(lang, "date.choose")}
        aria-expanded={open}
        onClick={() => setOpen(true)}
      >
        <CalendarDays aria-hidden="true" className="size-4" />
      </Button>
    </div>
    {open && anchor && portalRoot && createPortal(
      <div ref={panel} className="fixed z-50" style={{ top: anchor.top, left: anchor.left }}>
        <MonthGrid
        lang={lang}
        selected={iso.slice(0, 10)}
        onPick={day => {
          const next = !day
            ? ""
            : kind === "date"
              ? day
              // Choosing a day keeps the time that was already there.
              : `${day}T${(iso.split("T")[1] || "00:00").slice(0, 5)}`
          accept(next)
          setText(show(next, lang))
          setBroken(false)
          onBrokenChange?.(false)
          setOpen(false)
          // The chosen day's button is about to unmount: the field takes the
          // focus back, so a keyboard pick does not drop it on <body>.
          input.current?.focus()
          onCommit?.(next)
        }}
        />
      </div>,
      portalRoot,
    )}
  </div>
}

export function DateField(props: FieldProps) {
  return <DateControl kind="date" fieldProps={props} />
}

export function DateTimeField(props: FieldProps) {
  return <DateControl kind="datetime" fieldProps={props} />
}
