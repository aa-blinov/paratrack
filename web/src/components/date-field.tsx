import * as React from "react"
import { createPortal } from "react-dom"
import { CalendarDays, ChevronLeft, ChevronRight } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
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
   * Fired when the field is done with: blur, Enter, or a day picked from the
   * calendar. The session editor saves on this, exactly as it saved when the
   * native control reported a blur.
   */
  onCommit?: () => void
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

  return <div className="w-64 rounded-lg border bg-card p-3 shadow-md" role="group" aria-label={lang === "en" ? "Calendar" : "Календарь"}>
    <div className="mb-2 flex items-center justify-between">
      <Button type="button" variant="ghost" size="icon" className="size-7" aria-label={lang === "en" ? "Previous month" : "Предыдущий месяц"} onClick={() => shift(-1)}>
        <ChevronLeft aria-hidden="true" className="size-4" />
      </Button>
      <span className="text-sm font-medium"><span className="capitalize">{monthLabel(cursor.month, lang)}</span> {cursor.year}</span>
      <Button type="button" variant="ghost" size="icon" className="size-7" aria-label={lang === "en" ? "Next month" : "Следующий месяц"} onClick={() => shift(1)}>
        <ChevronRight aria-hidden="true" className="size-4" />
      </Button>
    </div>
    <div className="mb-1 grid grid-cols-7 gap-0.5 text-center text-xs text-muted-foreground">
      {weekdayLabels(lang).map(label => <span key={label}>{label}</span>)}
    </div>
    <div className="grid grid-cols-7 gap-0.5">
      {Array.from({ length: lead }, (_, i) => <span key={`blank-${i}`} />)}
      {Array.from({ length: daysInMonth }, (_, i) => {
        const iso = `${cursor.year}-${pad(cursor.month + 1)}-${pad(i + 1)}`
        const chosen = iso === selected
        return <button
          key={iso}
          type="button"
          onClick={() => onPick(iso)}
          aria-pressed={chosen}
          aria-current={iso === today ? "date" : undefined}
          className={`h-8 rounded-md text-sm tabular-nums hover:bg-muted ${chosen ? "bg-primary text-primary-foreground" : ""} ${iso === today && !chosen ? "font-semibold text-foreground underline underline-offset-2" : ""}`}
        >{i + 1}</button>
      })}
    </div>
    <div className="mt-2 flex justify-between border-t pt-2">
      <Button type="button" variant="ghost" size="sm" className="h-7 text-xs" onClick={() => onPick(todayISO())}>{lang === "en" ? "Today" : "Сегодня"}</Button>
      <Button type="button" variant="ghost" size="sm" className="h-7 text-xs" onClick={() => onPick("")}>{lang === "en" ? "Clear" : "Очистить"}</Button>
    </div>
  </div>
}

function DateControl({ kind, fieldProps }: { kind: Kind; fieldProps: FieldProps }) {
  const { id, name, defaultValue = "", required, disabled, className, style, ariaLabel, autoFocus, onValueChange, onCommit } = fieldProps
  const parse = kind === "date" ? parseDateInput : parseDateTimeInput
  const show = kind === "date" ? formatDate : formatDateTime

  const root = React.useRef<HTMLDivElement>(null)
  const panel = React.useRef<HTMLDivElement>(null)
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
    const height = 296
    const left = Math.min(Math.max(8, box.left), window.innerWidth - width - 8)
    // Near the bottom of the window the grid opens upwards rather than below,
    // where half of it would hang off the screen.
    const below = box.bottom + 4
    const fits = below + height <= window.innerHeight - 8
    setAnchor({ top: fits ? below : Math.max(8, box.top - height - 4), left })
  }, [])

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
    const onKey = (event: KeyboardEvent) => { if (event.key === "Escape") setOpen(false) }
    document.addEventListener("mousedown", onDown)
    document.addEventListener("keydown", onKey)
    return () => {
      document.removeEventListener("mousedown", onDown)
      document.removeEventListener("keydown", onKey)
    }
  }, [])

  // Switching the interface language re-spells the text without touching it.
  React.useEffect(() => { setText(show(iso, lang)) }, [lang]) // eslint-disable-line react-hooks/exhaustive-deps

  const accept = (next: string) => {
    setISO(next)
    onValueChange?.(next)
  }

  return <div className="relative w-full min-w-0" ref={root}>
    {/* The form posts exactly what it always posted. */}
    <input type="hidden" name={name} value={iso} disabled={disabled} />
    <div className="flex min-w-0 items-center gap-1">
      <Input
        id={id}
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
        placeholder={show(kind === "date" ? "2000-01-02" : "2000-01-02 00:00", lang)}
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
          setBroken(parsed === null && raw.trim().length > 0)
          accept(parsed === null ? "" : parsed)
        }}
        onBlur={() => onCommit?.()}
        onFocus={() => setOpen(true)}
        onKeyDown={event => { if (event.key === "Enter" && !open) { setOpen(true); event.preventDefault() } }}
        className={`min-w-0 flex-1 ${broken ? "border-destructive focus-visible:ring-destructive/40" : ""} ${className || ""}`}
      />
      <Button
        type="button"
        variant="outline"
        size="icon"
        className="shrink-0"
        disabled={disabled}
        aria-label={lang === "en" ? "Pick a date" : "Выбрать дату"}
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
          setOpen(false)
          onCommit?.()
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