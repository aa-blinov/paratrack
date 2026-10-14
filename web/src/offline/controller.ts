// Offline mode: queue the mutations a person makes without a connection and
// replay them, in order and with their original click time, when it returns.
// Also the PWA surface: service worker, install prompt and the running-timer
// count on the app icon and in the tab title.
import { isOfflineQueueableMutation } from "./policy"
import { encodeRequestBody, readQueue, sameOriginPath, writeQueue, type QueueItem } from "./queue"

export type OfflineState = {
  online: boolean
  queueLength: number
  // null: nothing blocked. 0: the network failed. Otherwise the HTTP status the server refused with.
  blockedStatus: number | null
  // Set after an install click on iOS, where there is no prompt to show.
  hint: "iosInstall" | null
}

const storage: Storage | null = (() => { try { return window.localStorage } catch { return null } })()
const nativeFetch = window.fetch.bind(window)
let state: OfflineState = { online: navigator.onLine, queueLength: readQueue(storage).length, blockedStatus: null, hint: null }
const listeners = new Set<() => void>()
let flushing = false
let installPrompt: BeforeInstallPromptEvent | null = null
let hintTimer: number | undefined

type BeforeInstallPromptEvent = Event & { prompt: () => Promise<void>; userChoice: Promise<unknown> }

function update(next: Partial<OfflineState>) {
  state = { ...state, ...next }
  listeners.forEach(listener => listener())
}

export function subscribeOffline(listener: () => void) {
  listeners.add(listener)
  return () => { listeners.delete(listener) }
}

export function getOfflineState(): OfflineState {
  return state
}

export function retryQueue() {
  update({ blockedStatus: null })
  void flush()
}

export function discardFirstQueued() {
  const pending = readQueue(storage)
  pending.shift()
  if (!writeQueue(storage, pending)) {
    update({ blockedStatus: 0, queueLength: readQueue(storage).length })
    return
  }
  update({ blockedStatus: null, queueLength: pending.length })
  void flush()
}

function queueRequest(method: string, rawURL: string, body: unknown, contentType: string): boolean {
  const url = sameOriginPath(rawURL)
  const verb = method.toUpperCase()
  if (!url || !isOfflineQueueableMutation(verb, new URL(url, window.location.href).pathname)) return false
  const payload = encodeRequestBody(body, contentType)
  if (!payload) return false
  const queue = readQueue(storage)
  queue.push({ verb, url, ts: Date.now(), ...payload } as QueueItem)
  if (!writeQueue(storage, queue)) return false
  update({ queueLength: queue.length })
  return true
}

function requestHeader(init: RequestInit | undefined, request: Request | null, name: string): string {
  return new Headers(init?.headers ?? request?.headers).get(name) ?? ""
}

// Wraps fetch once for the whole app: every mutation goes through it, so the
// queue needs no change in the pages that send them. A queued mutation answers
// 202 with X-Offline-Queued so the caller knows it is saved, not sent.
// The server names what a successful action did in X-Toast (percent-encoded,
// the kind in X-Toast-Kind). A stop with an undo shows its own strip instead.
function announce(response: Response) {
  const raw = response.headers.get("X-Toast")
  if (!raw || response.headers.get("X-Toast-Undo")) return
  let message: string
  try { message = decodeURIComponent(raw) } catch { message = raw }
  window.dispatchEvent(new CustomEvent<ToastDetail>("paratrack:toast", {
    detail: { message, kind: response.headers.get("X-Toast-Kind") || "success" },
  }))
}

export type ToastDetail = { message: string; kind: string }

export function installOffline() {
  window.fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
    const request = input instanceof Request ? input : null
    const method = (init?.method ?? request?.method ?? "GET").toUpperCase()
    if (method === "GET") return nativeFetch(input, init)
    const rawURL = typeof input === "string" ? input : input instanceof URL ? input.href : input.url
    let replay: Request | null = null
    if (request) {
      try { replay = request.clone() } catch { replay = null }
    }
    try {
      const response = await nativeFetch(input, init)
      if (response.ok) {
        announce(response)
        scheduleBadge()
      }
      return response
    } catch (error) {
      if (navigator.onLine) throw error
      let body: unknown = init?.body
      if (body === undefined && replay) {
        try { body = await replay.text() } catch { throw error }
      }
      if (!queueRequest(method, rawURL, body, requestHeader(init, request, "Content-Type"))) throw error
      return new Response(JSON.stringify({ queued: true }), {
        status: 202,
        headers: { "Content-Type": "application/json", "X-Offline-Queued": "1" },
      })
    }
  }
}

function csrfToken(): string {
  const match = document.cookie.match(/(?:^|;\s*)paratrack_csrf=([^;]+)/)
  if (!match) return ""
  try { return decodeURIComponent(match[1]) } catch { return match[1] }
}

// Sends the queue in order. Only a 2xx dequeues an item: a refusal stays for
// correction, an explicit retry or removal.
export async function flush() {
  if (flushing) return
  if (!navigator.onLine) { update({ online: false }); return }
  if (!readQueue(storage).length) { update({ queueLength: 0 }); return }
  flushing = true
  let sent = 0
  try {
    while (navigator.onLine) {
      const queue = readQueue(storage)
      if (!queue.length) break
      if (!writeQueue(storage, queue)) { update({ blockedStatus: 0 }); break }
      const item = queue[0]
      const url = sameOriginPath(item.url)
      const pathname = url ? new URL(url, window.location.href).pathname : ""
      if (!url || !isOfflineQueueableMutation(item.verb, pathname)) {
        // Records from older builds that the replay list no longer allows.
        queue.shift()
        if (!writeQueue(storage, queue)) { update({ blockedStatus: 0 }); break }
        update({ queueLength: queue.length })
        continue
      }
      const isJSON = item.encoding === "json"
      const body = isJSON ? JSON.stringify(item.body) : new URLSearchParams(item.body)
      if (!isJSON && body instanceof URLSearchParams) {
        if (!body.has("csrf_token")) body.set("csrf_token", csrfToken())
        // A timer action happened at click time, not at reconnect time.
        if (item.ts) body.set("client_ts", String(item.ts))
      }
      let response: Response
      try {
        response = await nativeFetch(url, {
          method: item.verb,
          credentials: "same-origin",
          headers: {
            "X-CSRF-Token": csrfToken(),
            "Content-Type": isJSON ? "application/json" : "application/x-www-form-urlencoded",
          },
          body,
        })
      } catch {
        update({ blockedStatus: 0 })
        break
      }
      if (!response.ok) { update({ blockedStatus: response.status }); break }
      const latest = readQueue(storage)
      if (JSON.stringify(latest[0]) !== JSON.stringify(item)) break
      latest.shift()
      if (!writeQueue(storage, latest)) { update({ blockedStatus: 0 }); break }
      sent += 1
      update({ queueLength: latest.length, blockedStatus: null })
    }
  } finally {
    flushing = false
  }
  update({ queueLength: readQueue(storage).length })
  scheduleBadge()
  // The page was rendered before these went out: show what the server now holds.
  if (sent > 0 && !readQueue(storage).length) window.location.reload()
}

export function startOffline() {
  window.addEventListener("online", () => { update({ online: true }); void flush() })
  window.addEventListener("offline", () => update({ online: false }))
  if (navigator.onLine) void flush()
  initPWA()
}

// The bootstrap says whether a person is signed in; the running-timer count is
// only read for them, never on the sign-in page.
function signedIn(): boolean {
  try {
    const payload = JSON.parse(document.getElementById("react-page-data")?.textContent || "{}") as { shell?: { user?: unknown } }
    return Boolean(payload.shell?.user)
  } catch {
    return false
  }
}

// ---- PWA -------------------------------------------------------------------

function initPWA() {
  if ("serviceWorker" in navigator) {
    window.addEventListener("load", () => { navigator.serviceWorker.register("/sw.js", { scope: "/" }).catch(() => {}) })
  }
  const standalone = matchMedia("(display-mode: standalone)").matches || (navigator as Navigator & { standalone?: boolean }).standalone === true
  const ios = /iphone|ipad|ipod/i.test(navigator.userAgent)
  const showInstall = (on: boolean) => document.querySelectorAll<HTMLElement>("[data-install]").forEach(el => { el.hidden = !on })
  window.addEventListener("beforeinstallprompt", (event) => {
    event.preventDefault()
    installPrompt = event as BeforeInstallPromptEvent
    showInstall(true)
  })
  window.addEventListener("appinstalled", () => { installPrompt = null; showInstall(false) })
  // iOS has no prompt: the button explains Share, then Add to Home Screen.
  if (ios && !standalone) showInstall(true)
  else showInstall(false)
  document.addEventListener("click", async (event) => {
    const target = event.target instanceof Element ? event.target.closest("[data-install]") : null
    if (!target) return
    event.preventDefault()
    if (installPrompt) {
      const prompt = installPrompt
      installPrompt = null
      await prompt.prompt()
      await prompt.userChoice
      showInstall(false)
    } else if (ios) {
      update({ hint: "iosInstall" })
      window.clearTimeout(hintTimer)
      hintTimer = window.setTimeout(() => update({ hint: null }), 8000)
    }
  })
  if (signedIn()) scheduleBadge()
}

let badgeTimer: number | undefined
export function scheduleBadge() {
  window.clearTimeout(badgeTimer)
  badgeTimer = window.setTimeout(() => { void updateBadge() }, 250)
}

// The count of running timers, on the installed app's icon and in the title.
async function updateBadge() {
  if (!navigator.onLine) return
  try {
    const response = await nativeFetch("/api/active", { credentials: "same-origin", headers: { Accept: "application/json" } })
    if (!response.ok) return
    const payload = await response.json() as { Items?: { Paused?: boolean }[] }
    const running = (payload.Items ?? []).filter(item => !item.Paused).length
    const base = document.title.replace(/^● \d+ · /, "")
    document.title = running ? `● ${running} · ${base}` : base
    const nav = navigator as Navigator & { setAppBadge?: (n: number) => Promise<void>; clearAppBadge?: () => Promise<void> }
    if (running) void nav.setAppBadge?.(running)?.catch(() => {})
    else void nav.clearAppBadge?.()?.catch(() => {})
  } catch {
    // the badge is decoration; a failed read leaves it as it was
  }
}
