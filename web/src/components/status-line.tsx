import type { ComponentProps } from "react"

// The save feedback line. It stays in the page as a live region so a screen
// reader hears the change, but while it is empty it takes no space: a reserved
// line under every form used to read as a gap the size of a row.
export function StatusLine({ text, className = "text-xs text-muted-foreground", ...rest }: ComponentProps<"p"> & { text: string }) {
  return <p role="status" aria-live="polite" {...rest} className={text ? className : "sr-only"}>{text}</p>
}
