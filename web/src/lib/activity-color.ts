/**
 * The activity palette, in one place.
 *
 * The Go palette in `internal/model/activity_color.go` decides *which* colour
 * an activity wears — that mapping is the product's memory, and a person
 * learns their own marks. This module decides *how* that colour is painted:
 * it hands back the theme's own token for the mark, so the same activity is
 * the same colour on the dashboard, in the stats list and on the graph, and
 * follows the theme instead of sitting outside it.
 *
 * Both themes define all ten. The light values are the historical hexes
 * unchanged; the dark ones are lifted for a near-black background, because
 * measured L≈0.585 for #6366f1 loses its edge on a card at L=0.205. Hue is
 * never changed — that is what keeps the name-to-colour memory intact.
 */
const PALETTE_HEX = [
  "#6366f1", "#0ea5e9", "#14b8a6", "#10b981", "#84cc16",
  "#d97706", "#f97316", "#8b5cf6", "#a855f7", "#ec4899",
]

/** The CSS custom property that paints this mark in the current theme. */
export function activityToken(hex: string | null | undefined): string | null {
  const wanted = String(hex ?? "").trim().toLowerCase()
  const index = PALETTE_HEX.indexOf(wanted)
  if (index < 0) return null
  return `var(--activity-${index + 1})`
}

/**
 * The value to put in a `style` attribute. Falls back to the stored hex for
 * anything outside the palette — a workspace may hold a colour written
 * before the palette was fixed, and showing it in its own colour beats
 * showing it in a wrong one.
 *
 * Only a hex is ever returned: this string lands in an inline `background`,
 * and the graph tooltip has the same guard for the same reason.
 */
export function activityColor(hex: string | null | undefined): string {
  const wanted = typeof hex === "string" ? hex.trim().toLowerCase() : ""
  if (!/^#[0-9a-f]{3,8}$/.test(wanted)) return ""
  return activityToken(wanted) ?? wanted
}