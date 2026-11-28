// The badge on the toolbar icon shows the running timer without opening the
// popup: its time for one timer, the count for several, nothing when none
// runs. It is refreshed every minute and right after the popup changes a
// timer. A refused token or an unreachable server clears it rather than
// showing a stale time.
const DEFAULT_BASE = "https://paratrack.duckdns.org"

async function activeTimers() {
  const cfg = await chrome.storage.local.get(["base", "token"])
  if (!cfg.token) return []
  const base = (cfg.base || DEFAULT_BASE).replace(/\/$/, "")
  const res = await fetch(base + "/api/active", {
    headers: { Authorization: "Bearer " + cfg.token },
    credentials: "omit",
  })
  if (!res.ok) return []
  const data = await res.json()
  return Array.isArray(data.Items) ? data.Items : []
}

// badgeText is the time of a single running timer as h:mm, or the number of
// running timers when there are several.
export function badgeText(items, now = Date.now()) {
  const running = items.filter((x) => !x.Paused)
  if (running.length === 0) return ""
  if (running.length > 1) return String(running.length)
  const t = running[0]
  const started = new Date(t.ResumeISO || t.StartISO).getTime()
  const sec = Math.max(0, Number(t.AccumulatedSeconds || 0) + (now - started) / 1000)
  const m = Math.floor(sec / 60)
  return Math.floor(m / 60) + ":" + String(m % 60).padStart(2, "0")
}

async function refreshBadge() {
  let text = ""
  try {
    text = badgeText(await activeTimers())
  } catch (_) {
    text = ""
  }
  await chrome.action.setBadgeText({ text })
  await chrome.action.setBadgeBackgroundColor({ color: "#15803d" })
}

if (typeof chrome !== "undefined" && chrome.alarms) {
  chrome.runtime.onInstalled.addListener(() => {
    chrome.alarms.create("badge", { periodInMinutes: 1 })
    refreshBadge()
  })
  chrome.runtime.onStartup.addListener(() => {
    chrome.alarms.create("badge", { periodInMinutes: 1 })
    refreshBadge()
  })
  chrome.alarms.onAlarm.addListener((alarm) => {
    if (alarm.name === "badge") refreshBadge()
  })
  chrome.runtime.onMessage.addListener((message) => {
    if (message && message.type === "refresh-badge") refreshBadge()
  })
}
