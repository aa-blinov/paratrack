// Load is what was tracked against what was planned for the same days, in percent.
// A row with no plan has nothing to measure against, so it has no load at all
// rather than a load of zero.
export function planLoad(trackedSecs: number, plannedSecs: number): number | null {
  if (plannedSecs <= 0) return null
  return (trackedSecs / plannedSecs) * 100
}
