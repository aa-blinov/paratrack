// The popup's status line, against a fake browser: a refusal shows the
// server's reason, an error page is not "unreachable", "server unreachable"
// is kept for when nothing answered, and the settings screen says
// "connected" only when the token was accepted.
//
// Run with: node --test extension/
import { test } from "node:test"
import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import vm from "node:vm"

const source = readFileSync(new URL("./popup.js", import.meta.url), "utf8")

function element() {
  const listeners = {}
  const el = {
    textContent: "", value: "", innerHTML: "", dataset: {}, className: "",
    classList: { add() {}, remove() {}, contains: () => false },
    addEventListener: (type, fn) => { listeners[type] = fn },
    querySelector: () => el.status,
    appendChild() {},
    listeners,
  }
  return el
}

// load builds a popup whose fetch answers with respond(path, opts).
function load(respond, token = "pt_test") {
  const status = element()
  const els = {}
  const document = {
    getElementById: (id) => {
      if (!els[id]) {
        els[id] = element()
        els[id].status = status
      }
      return els[id]
    },
    createElement: () => element(),
  }
  const store = { base: "https://pt.example", token }
  const chrome = {
    storage: { local: { get: async () => ({ ...store }), set: async (v) => Object.assign(store, v) } },
    permissions: { contains: async () => true, request: async () => true },
  }
  const ctx = {
    document, chrome, URLSearchParams, URL, JSON, Date, Math, Number, String, Array, Error,
    fetch: async (url, opts) => respond(new URL(url).pathname, opts || {}),
    setInterval: () => 0,
  }
  vm.createContext(ctx)
  vm.runInContext(source, ctx)
  return { ctx, el: (id) => document.getElementById(id), status }
}

const json = (status, body) => new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })
const settle = () => new Promise((r) => setTimeout(r, 0))

test("a refused token shows the server's reason, and saving does not say connected", async () => {
  const { el, status } = load(() => json(401, { error: "Токен не принят" }))
  await settle()
  el("token").value = "pt_dead"
  await el("save").listeners.click()
  assert.equal(status.textContent, "Токен не принят")
})

test("a read-only token on Start shows the server's reason", async () => {
  const { ctx, el, status } = load((path) => path === "/api/start"
    ? json(403, { error: "Этот токен только для чтения" })
    : json(200, path === "/api/active" ? { Items: [] } : path === "/api/me" ? { user: { name: "Антон" } } : { tasks: [] }))
  await settle()
  el("activity").value = "вёрстка"
  el("task").value = ""
  await ctx.startTimer()
  assert.equal(status.textContent, "Этот токен только для чтения")
})

test("an error page from the server is a server error, not unreachable", async () => {
  const { ctx, status } = load((path) => path === "/api/me"
    ? json(200, { user: { name: "Антон" } })
    : new Response("<html>Bad gateway</html>", { status: 502 }))
  await settle()
  assert.equal(await ctx.refresh(), false)
  assert.equal(status.textContent, "server error (502)")
})

test("nothing answering is the only server unreachable", async () => {
  const { ctx, status } = load(() => { throw new TypeError("fetch failed") })
  await settle()
  assert.equal(await ctx.refresh(), false)
  assert.equal(status.textContent, "server unreachable")
})

test("an accepted token says connected", async () => {
  const { el, status } = load((path) => json(200,
    path === "/api/me" ? { user: { name: "Антон" } } : path === "/api/active" ? { Items: [] } : { tasks: [] }))
  await settle()
  el("token").value = "pt_good"
  el("base").value = "https://pt.example"
  await el("save").listeners.click()
  assert.equal(status.textContent, "connected")
})
