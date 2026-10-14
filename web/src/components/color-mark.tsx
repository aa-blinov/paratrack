/**
 * The colour of an activity or a project, as one mark everywhere: a 3px spine
 * the height of a line of text. Lists, tables, the timer and the legend all
 * use it, so a colour means the same thing on every screen.
 */
export function ColorMark({ color }: { color: string }) {
  return <span aria-hidden="true" className="block h-[1lh] w-[3px] shrink-0 rounded-full" style={{ backgroundColor: color }} />
}
