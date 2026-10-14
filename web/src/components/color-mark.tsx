/**
 * The colour of an activity or a project, as one mark everywhere: a 3px spine
 * the height of a line of text. Lists, tables, the timer and the legend all
 * use it, so a colour means the same thing on every screen.
 *
 * Beside the text it sits with a gap of 8px. Where the mark is a direct child
 * of a flex row, that row's gap does it; in a table cell, or anywhere the text
 * follows without a flex parent, the caller passes `mr-2`.
 */
export function ColorMark({ color, className = "" }: { color: string; className?: string }) {
  return <span aria-hidden="true" className={`inline-block h-[1lh] w-[3px] shrink-0 align-top rounded-full ${className}`} style={{ backgroundColor: color }} />
}
