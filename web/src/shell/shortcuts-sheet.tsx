// The `?` sheet (UX-40): every keyboard shortcut the product answers, on one
// screen, from any screen. The list itself lives in shortcuts.ts — this sheet
// reads it like the help page does, so the two can never disagree.
import * as React from "react"
import type { ReactNode } from "react"
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { shortcuts } from "@/shell/shortcuts"
import { translate as t } from "@/i18n"

// Returns the sheet itself and the way to open it from the pointer (the
// footer's "все клавиши"): the keyboard's way is the `?` key anywhere.
export function useShortcutsSheet(lang: string): [ReactNode, () => void] {
  const [open, setOpen] = React.useState(false)
  React.useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      // A `?` that arrives inside a field is punctuation, not a request.
      if (event.key !== "?") return
      const target = event.target as HTMLElement | null
      if (target?.closest?.("input, textarea, select, [contenteditable=true]")) return
      event.preventDefault()
      setOpen(true)
    }
    document.addEventListener("keydown", onKeyDown)
    return () => document.removeEventListener("keydown", onKeyDown)
  }, [])

  const sheet = (
    <Sheet open={open} onOpenChange={setOpen}>
      <SheetContent side="right" className="shortcuts-sheet">
        <SheetHeader>
          <SheetTitle className="text-left">{t(lang, "kbd.sheetTitle")}</SheetTitle>
        </SheetHeader>
        <dl className="grid gap-2.5 text-sm">
          {shortcuts.map(row => (
            <div key={row.key} className="flex items-center gap-3">
              <dt><kbd className="rounded-md border bg-muted px-2 py-0.5 font-mono text-xs">{row.key}</kbd></dt>
              <dd className="text-muted-foreground">{t(lang, row.label)}</dd>
            </div>
          ))}
        </dl>
        <p className="text-xs text-muted-foreground">{t(lang, "kbd.sheetHint")}</p>
      </SheetContent>
    </Sheet>
  )
  return [sheet, () => setOpen(true)]
}
