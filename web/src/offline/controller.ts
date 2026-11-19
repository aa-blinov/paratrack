// Offline mode: queue the mutations a person makes without a connection and
// replay them, in order and with their original click time, when it returns.
// Also the PWA surface: service worker, install prompt and the running-timer
// count on the app icon and in the tab title.
import { requestConfirmation } from "@/components/confirmation-dialog"
import { responseMessage } from "@/lib/response-message"
import { readCSRFCookie } from "@/lib/csrf"
import { translate as t } from "@/i18n"
import { isOfflineQueueableMutation } from "./policy"
import { encodeRequestBody, QUEUE_KEY, readQueue, sameOriginPath, writeQueue, type QueueItem } from "./queue"

export type OfflineState = {
  online: boolean
  queueLength: number
  // Null when nothing is blocked; 0 when the network itself failed. An HTTP
  // refusal no longer blocks here — it marks its own entry with the server's
  // words, and the round walks on.
  blockedStatus: number | null
  // What the last round did: the actions the server took, the ones it
  // refused, and its own words for the refusal — a code says nothing.
  appliedCount: number
  failedCount: number
  lastError: string
  // Set after an install click on iOS, where there is no prompt to show.
  hint: "iosInstall" | null
}

const storage: Storage | null = (() => { try { return window.localStorage } catch { return null } })()
const nativeFetch = window.fetch.bind(window)

// Counters derived from the stored queue, so a reload or another tab's write
// lands in the banner without anyone re-measuring it by hand.
function queueStats() {
  const queue = readQueue(storage)
  const failed = queue.filter(item => item.failed)
  return { queueLength: queue.length, failedCount: failed.length, lastError: failed.length ? failed[failed.length - 1].failed! : "" }
}

let state: OfflineState = { online: navigator.onLine, blockedStatus: null, appliedCount: 0, hint: null, ...queueStats() }
const listeners = new Set<() => void>()
let flushing = false
let installPrompt: BeforeInstallPromptEvent | null = null
let hintTimer: number | undefined

// One entry per action, also across replays: an entry without a key (written by
// an earlier build) is told apart by where and when it was made.
function markerOf(item: QueueItem): string {
  return item.key ?? `${item.verb} ${item.url} ${item.ts ?? 0}`
}

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

// A retry says the refusal was temporary: the marks come off, and a fresh
// round tries every entry again, in order. Without a connection there is
// nothing to retry over, and wiping the marks would lose the refusals the
// person has not read yet.
export function retryQueue() {
  if (!navigator.onLine) { update({ online: false }); return }
  const queue = readQueue(storage)
  if (queue.some(item => item.failed)) {
    const cleared = queue.map(item => (item.failed ? withFailed(item, undefined) : item))
    if (!writeQueue(storage, cleared)) { update({ blockedStatus: 0 }); return }
    update({ ...queueStats(), blockedStatus: null })
  } else {
    update({ blockedStatus: null })
  }
  void flush()
}

// What the entry was, in the words the person used when they made it: the
// activity they typed, the minutes they booked. Only the route when the body
// has neither — a stop or a delete names nothing.
function describeQueueItem(item: QueueItem, lang: string): string {
  const field = (name: string): string | undefined => {
    if (item.encoding === "form") return item.body.find(([key]) => key === name)?.[1]
    const value = item.body[name]
    return typeof value === "string" ? value : typeof value === "number" ? String(value) : undefined
  }
  const parts: string[] = []
  const activity = field("activity")
  if (activity) parts.push(`«${activity}»`)
  const minutes = field("minutes")
  if (minutes !== undefined) parts.push(`${minutes} ${t(lang, "graph.min")}`)
  return parts.join(", ") || `${item.verb} ${item.url}`
}

// The entry the banner is about: the first one the server refused, else the
// head. One click used to erase it in silence; now it says what it was, asks,
// and only then goes.
export async function discardFirstQueued(lang: string) {
  const queue = readQueue(storage)
  const failedAt = queue.findIndex(item => item.failed)
  const at = failedAt >= 0 ? failedAt : 0
  const item = queue[at]
  if (!item) return
  const description = describeQueueItem(item, lang)
  const question = item.failed ? `${description}. ${item.failed}` : description
  if (!(await requestConfirmation(question, { title: t(lang, "pwa.queueDiscardTitle"), action: t(lang, "pwa.queueDiscard") }))) return
  const rest = [...queue.slice(0, at), ...queue.slice(at + 1)]
  if (!writeQueue(storage, rest)) {
    update({ blockedStatus: 0, ...queueStats() })
    return
  }
  update({ blockedStatus: null, ...queueStats() })
  window.dispatchEvent(new CustomEvent<ToastDetail>("paratrack:toast", {
    detail: { message: t(lang, "pwa.queueDiscarded", description), kind: "success" },
  }))
  void flush()
}

function withFailed(item: QueueItem, message: string | undefined): QueueItem {
  return { ...item, failed: message }
}

function queueRequest(method: string, rawURL: string, body: unknown, contentType: string, key: string): boolean {
  const url = sameOriginPath(rawURL)
  const verb = method.toUpperCase()
  if (!url || !isOfflineQueueableMutation(verb, new URL(url, window.location.href).pathname)) return false
  const payload = encodeRequestBody(body, contentType)
  if (!payload) return false
  const queue = readQueue(storage)
  queue.push({ verb, url, ts: Date.now(), key, ...payload } as QueueItem)
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

// init.headers would replace the headers of a Request object wholesale, so the
// key rides only the init the app always builds; a raw Request keeps its own
// headers and the key waits in the queue instead.
function withIdempotencyKey(init: RequestInit | undefined, key: string): RequestInit {
  const headers = new Headers(init?.headers)
  headers.set("Idempotency-Key", key)
  return { ...init, headers }
}

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
    // The key is minted before the first send, not at replay time: a write
    // that reached the server before the connection dropped and the same
    // write replayed later are then one write to the server, not two.
    const path = sameOriginPath(rawURL)
    const key = path && isOfflineQueueableMutation(method, new URL(path, window.location.href).pathname) ? newRequestKey() : ""
    const sendInit = key && !request ? withIdempotencyKey(init, key) : init
    try {
      const response = await nativeFetch(input, sendInit)
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
      if (!queueRequest(method, rawURL, body, requestHeader(init, request, "Content-Type"), key)) throw error
      return new Response(JSON.stringify({ queued: true }), {
        status: 202,
        headers: { "Content-Type": "application/json", "X-Offline-Queued": "1" },
      })
    }
  }
}

function newRequestKey(): string {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") return crypto.randomUUID()
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`
}

// One entry, changed or removed in place under a fresh read: another tab may
// have touched the queue since this round read it. Null when the entry is
// already gone or the storage refused the write — the caller stops, and the
// next reconnect picks the queue up again.
function alterQueue(marker: string, change: (item: QueueItem) => QueueItem | null): QueueItem[] | null {
  const queue = readQueue(storage)
  const index = queue.findIndex(item => markerOf(item) === marker)
  if (index < 0) return null
  const next = change(queue[index])
  const updated = next
    ? [...queue.slice(0, index), next, ...queue.slice(index + 1)]
    : [...queue.slice(0, index), ...queue.slice(index + 1)]
  return writeQueue(storage, updated) ? updated : null
}

// Sends the queue in order. A 2xx dequeues an item. A refusal is the server's
// decision about that one action: the entry keeps the refusal text and the
// round moves on, so one entry the server will not take cannot strand the
// ones behind it. A network-level failure ends the round where it stands.
export async function flush() {
  if (flushing) return
  if (!navigator.onLine) { update({ online: false }); return }
  if (!readQueue(storage).length) { update({ ...queueStats(), blockedStatus: null }); return }
  flushing = true
  let sent = 0
  // One attempt per entry this round: a refusal stays queued, so the loop
  // needs its own memory of what it already tried.
  const attempted = new Set<string>()
  try {
    while (navigator.onLine) {
      const queue = readQueue(storage)
      if (!queue.length) break
      if (!writeQueue(storage, queue)) { update({ blockedStatus: 0 }); break }
      const index = queue.findIndex(item => !attempted.has(markerOf(item)))
      if (index < 0) break
      const item = queue[index]
      const marker = markerOf(item)
      // A retry starts clean: the mark from an earlier refusal describes an
      // attempt that is over.
      if (item.failed) {
        const rest = alterQueue(marker, entry => withFailed(entry, undefined))
        if (!rest) { update({ blockedStatus: 0 }); break }
        update({ ...queueStats() })
        continue
      }
      const url = sameOriginPath(item.url)
      const pathname = url ? new URL(url, window.location.href).pathname : ""
      if (!url || !isOfflineQueueableMutation(item.verb, pathname)) {
        // Records from older builds that the replay list no longer allows.
        const rest = alterQueue(marker, () => null)
        if (!rest) { update({ blockedStatus: 0 }); break }
        update({ queueLength: rest.length })
        continue
      }
      const isJSON = item.encoding === "json"
      const body = isJSON ? JSON.stringify(item.body) : new URLSearchParams(item.body)
      if (!isJSON && body instanceof URLSearchParams) {
        if (!body.has("csrf_token")) body.set("csrf_token", readCSRFCookie())
        // A timer action happened at click time, not at reconnect time.
        if (item.ts) body.set("client_ts", String(item.ts))
      }
      let response: Response
      try {
        response = await nativeFetch(url, {
          method: item.verb,
          credentials: "same-origin",
          headers: {
            "X-CSRF-Token": readCSRFCookie(),
            "Content-Type": isJSON ? "application/json" : "application/x-www-form-urlencoded",
            ...(item.key ? { "Idempotency-Key": item.key } : {}),
          },
          body,
        })
      } catch {
        update({ blockedStatus: 0 })
        break
      }
      attempted.add(marker)
      if (!response.ok) {
        // The body says why the server refused; a bare code says nothing.
        const refusal = await responseMessage(response)
        const rest = alterQueue(marker, entry => withFailed(entry, refusal))
        if (!rest) { update({ blockedStatus: 0 }); break }
        update({ ...queueStats() })
        continue
      }
      // Another tab may have dequeued this entry while it was in flight; the
      // idempotency key keeps that harmless where it did reach the server.
      const rest = alterQueue(marker, () => null)
      if (!rest) continue
      sent += 1
      update({ queueLength: rest.length, blockedStatus: null })
    }
  } finally {
    flushing = false
  }
  update({ ...queueStats(), appliedCount: sent })
  scheduleBadge()
  // The page was rendered before these went out: show what the server now
  // holds. Entries that failed stay in the banner with their own words.
  if (sent > 0) window.location.reload()
}

export function startOffline() {
  window.addEventListener("online", () => { update({ online: true }); void flush() })
  window.addEventListener("offline", () => update({ online: false }))
  // Another tab may queue, replay or discard while this one is open: the
  // counts it keeps in memory go stale, and this tab's flush would race the
  // other's. Follow the storage, and let one flush at a time do the sending.
  window.addEventListener("storage", event => {
    if (event.key !== null && event.key !== QUEUE_KEY) return
    update({ ...queueStats() })
    if (navigator.onLine) void flush()
  })
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
    const base = document.title.replace(/^\(\d+\) /, "")
    document.title = running ? `(${running}) ${base}` : base
    const nav = navigator as Navigator & { setAppBadge?: (n: number) => Promise<void>; clearAppBadge?: () => Promise<void> }
    if (running) void nav.setAppBadge?.(running)?.catch(() => {})
    else void nav.clearAppBadge?.()?.catch(() => {})
  } catch {
    // the badge is decoration; a failed read leaves it as it was
  }
}
