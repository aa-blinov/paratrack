// Every keyboard shortcut the product answers (UX-40). The list is the single
// source: the `?` sheet, the help page and the footer hint read it, so a
// shortcut cannot exist without its row — the Ctrl/Cmd+B one lived undocumented
// once for exactly that reason.
export const shortcuts = [
  { key: "N", label: "kbd.new" },
  { key: "D", label: "kbd.overview" },
  { key: "S", label: "kbd.stats" },
  { key: "G", label: "kbd.graph" },
  { key: "P", label: "kbd.pause" },
  { key: "T", label: "kbd.theme" },
  { key: "Ctrl/Cmd + B", label: "kbd.sidebar" },
] as const
