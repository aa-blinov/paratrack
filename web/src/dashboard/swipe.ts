// The one-hand gesture on a running timer's row (UX-32): a leftward swipe
// means «Только эта» — every other timer goes on pause and this one keeps
// going. The action is safe (others come back with a tap), so it needs no
// confirmation, unlike a stop.

// The intent of one finished swipe gesture. A drag that is mostly vertical is
// a scroll, not a gesture; a swipe that starts on a control keeps being that
// control's press. dx and dy are the whole travel from the touch's start.
export type SwipeIntent = "focus" | null

export function swipeIntent(input: { dx: number; dy: number; onControl: boolean }): SwipeIntent {
  const { dx, dy, onControl } = input
  if (onControl) return null
  // Far enough to be on purpose, and clearly more sideways than vertical.
  if (dx > -48) return null
  if (Math.abs(dy) > Math.abs(dx) / 2) return null
  return "focus"
}

// True when the swipe's travel began on a control the row already owns.
export function onInteractiveControl(target: Element | null): boolean {
  return Boolean(target?.closest("button, a, input, select, textarea, label, [role=menu]"))
}
