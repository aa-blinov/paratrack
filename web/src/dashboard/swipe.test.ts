import { describe, expect, it } from "vitest"
import { onInteractiveControl, swipeIntent } from "./swipe"

// @vitest-environment node

// «Только эта» жестом (UX-32): clearly sideways and far enough, and never on
// a control the row already owns.
describe("swipeIntent", () => {
  it("fires on a leftward swipe", () => {
    expect(swipeIntent({ dx: -80, dy: 6, onControl: false })).toBe("focus")
  })

  it("keeps a small drift a tap", () => {
    expect(swipeIntent({ dx: -20, dy: 2, onControl: false })).toBeNull()
  })

  it("keeps a vertical travel a scroll", () => {
    expect(swipeIntent({ dx: -80, dy: 70, onControl: false })).toBeNull()
    expect(swipeIntent({ dx: -80, dy: -44, onControl: false })).toBeNull()
  })

  it("never fires on a control the row owns", () => {
    expect(swipeIntent({ dx: -120, dy: 0, onControl: true })).toBeNull()
  })

  it("does not read a rightward drag as a gesture", () => {
    expect(swipeIntent({ dx: 90, dy: -10, onControl: false })).toBeNull()
  })
})

// The closest() contract only: the suite runs in node, so the DOM selector
// string is exercised with a stand-in that answers it directly.
describe("onInteractiveControl", () => {
  function elementWith(matches: string | null): Element {
    return { closest: (selector: string) => (matches !== null && selector.includes(matches) ? {} : null) } as unknown as Element
  }

  it("is true for a control touch", () => {
    expect(onInteractiveControl(elementWith("button"))).toBe(true)
    expect(onInteractiveControl(elementWith("select"))).toBe(true)
  })

  it("is false for the row's own text", () => {
    expect(onInteractiveControl(elementWith(null))).toBe(false)
  })
})
