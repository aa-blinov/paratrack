import { useEffect, useState } from "react"

// The page ground per theme, as hex for the browser chrome (theme-color).
// Same values as --background in ui.css: the meta used to say plain white
// beside warm paper, and a different near-black in each of three places.
const CHROME = { light: "#faf6ee", dark: "#0a0a0a" }

export type ThemeMode = "auto" | "light" | "dark"

export function applyTheme(mode: ThemeMode) {
  const dark = mode === "dark" || (mode === "auto" && matchMedia("(prefers-color-scheme: dark)").matches)
  document.documentElement.dataset.themeMode = mode
  document.documentElement.dataset.theme = dark ? "paratrack-dark" : "paratrack-light"
  document.querySelectorAll('meta[name="theme-color"]').forEach(meta => meta.setAttribute("content", dark ? CHROME.dark : CHROME.light))
}

// One theme switch for the sidebar and the phone menu. «Авто» follows the
// system while the page is open, not only on the next full load.
export function useTheme() {
  const [mode, setMode] = useState<ThemeMode>(() => (document.documentElement.dataset.themeMode as ThemeMode) || "auto")
  // Two switches (the menu and the Interface page) share one document theme;
  // each follows the attribute so neither shows a stale mode.
  useEffect(() => {
    const observer = new MutationObserver(() => setMode((document.documentElement.dataset.themeMode as ThemeMode) || "auto"))
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme-mode"] })
    return () => observer.disconnect()
  }, [])
  useEffect(() => {
    if (mode !== "auto") return
    const media = matchMedia("(prefers-color-scheme: dark)")
    const follow = () => applyTheme("auto")
    media.addEventListener("change", follow)
    return () => media.removeEventListener("change", follow)
  }, [mode])
  function choose(next: ThemeMode) {
    setMode(next)
    try { localStorage.setItem("paratrack-theme", next) } catch { /* storage can be disabled */ }
    applyTheme(next)
  }
  function cycle() {
    choose(mode === "auto" ? "light" : mode === "light" ? "dark" : "auto")
  }
  return [mode, cycle, choose] as const
}
