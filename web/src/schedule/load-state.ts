// The load note of a person's week. Overload comes first, so a person is never both
// overloaded and idle. Idle is strictly under half of a positive available time: exactly
// half is a normal week, and a zero norm has no load to judge.
export type LoadState = "overload" | "idle" | null

export function loadState(planned: number, available: number): LoadState {
  if (planned > available) return "overload"
  if (available > 0 && planned * 2 < available) return "idle"
  return null
}
