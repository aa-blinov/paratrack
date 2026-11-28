// The popup against its own popup.html: the elements, their ids, the hidden
// sections and the Russian messages are the real ones, so a message is checked
// in the section that is actually on screen. An earlier version of this test
// handed every section one shared status line and passed while the timer
// screen's messages went to a hidden block.
//
// Run with: node --test extension/
import { test } from "node:test"
import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import vm from "node:vm"

const source = readFileSync(new URL("./popup.js", import.meta.url), "utf8")
const html = readFileSync(new URL("./popup.html", import.meta.url), "utf8")
const messages = JSON.parse(readFileSync(new URL("./_locales/ru/messages.json", import.meta.url), "utf8"))

function getMessage(key, subs = []) {
  const m = messages[key]
  if (!m) return ""
  return subs.reduce((text, sub, i) => text.replaceAll("$" + (i + 1), sub), m.message)
}

// fakeDOM builds one element per tag in popup.html's body with its id,
// classes and data attributes.
function fakeDOM() {
  const byId = {}
  const all = []
  const body = html.slice(html.indexOf("<body>"), html.indexOf("</body>"))
  for (const m of body.matchAll(/<([a-z0-9]+)([^>]*)>/g)) {
    const [, tag, attrs] = m
    const attr = (name) => (attrs.match(new RegExp(`\\s${name}="([^"]*)"`)) || [])[1]
    const listeners = {}
    const classes = new Set((attr("class") || "").split(/\s+/).filter(Boolean))
    const dataset = {}
    for (const d of attrs.matchAll(/\sdata-([a-z0-9-]+)="([^"]*)"/g)) {
      dataset[d[1].replace(/-([a-z])/g, (_, c) => c.toUpperCase())] = d[2]
    }
    const el = {
      tag, id: attr("id"), dataset, textContent: "", value: "", placeholder: "", innerHTML: "", attrs,
      classList: { add: (c) => classes.add(c), remove: (c) => classes.delete(c), contains: (c) => classes.has(c) },
      addEventListener: (type, fn) => { listeners[type] = fn },
      appendChild() {},
      listeners,
    }
    if (el.id) {
      assert.ok(!byId[el.id], `popup.html has two elements with id="${el.id}"`)
      byId[el.id] = el
    }
    all.push(el)
  }
  return {
    byId,
    document: {
      getElementById: (id) => byId[id] || null,
      querySelectorAll: (sel) => {
        const name = sel.match(/^\[([a-z0-9-]+)\]$/)[1]
        return all.filter((el) => new RegExp(`\\s${name}="`).test(el.attrs))
      },
      createElement: (tag) => ({ tag, value: "", textContent: "" }),
    },
  }
}

function load(respond, stored = { base: "https://pt.example", token: "pt_test" }) {
  const { byId, document } = fakeDOM()
  const store = { ...stored }
  const chrome = {
    i18n: { getMessage },
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
  // What the person sees: the status line of the section that is not hidden.
  const visible = () => {
    const shown = byId.app.classList.contains("hidden") ? "setup" : "app"
    return { section: shown, status: byId[shown + "-status"].textContent }
  }
  return { ctx, el: byId, store, visible }
}

const json = (status, body) => new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })
const settle = () => new Promise((r) => setTimeout(r, 0))
const ok = (path) => json(200, path === "/api/me" ? { user: { name: "Антон" } } : path === "/api/active" ? { Items: [] } : { tasks: [] })

test("the popup speaks Russian from its own messages", async () => {
  const { el } = load(ok)
  await settle()
  assert.equal(el.start.textContent, "Старт")
  assert.equal(el.save.textContent, "Сохранить и подключиться")
})

test("a refused token is not saved, the setup stays open and says why", async () => {
  const { el, store, visible } = load((path, opts) =>
    (opts.headers || {}).Authorization === "Bearer pt_dead" ? json(401, { error: "Токен не принят сервером" }) : ok(path), {})
  await settle()
  el.token.value = "pt_dead"
  el.base.value = "https://pt.example"
  await el.save.listeners.click()
  assert.deepEqual(visible(), { section: "setup", status: "Токен не принят сервером" })
  assert.equal(store.token, undefined, "a dead token was saved")
})

test("a saved token that stops working brings the setup back with the reason", async () => {
  const { ctx, visible } = load(() => json(403, { error: "Доступа к пространству больше нет" }))
  await settle()
  assert.equal(await ctx.refresh(), false)
  assert.deepEqual(visible(), { section: "setup", status: "Доступа к пространству больше нет" })
})

test("a read-only token on Start shows the reason on the timer screen", async () => {
  const { ctx, el, visible } = load((path) => path === "/api/start"
    ? json(403, { error: "Этот токен только для чтения" }) : ok(path))
  await settle()
  el.activity.value = "вёрстка"
  await ctx.startTimer()
  assert.deepEqual(visible(), { section: "app", status: "Этот токен только для чтения" })
})

test("an error page is a server error and a wrong address is named, not unreachable", async () => {
  let page = 502
  const { ctx, visible } = load((path) => path === "/api/me" ? ok(path) : new Response("<html>…</html>", { status: page }))
  await settle()
  assert.equal(await ctx.refresh(), false)
  assert.deepEqual(visible(), { section: "app", status: "Ошибка на сервере (502)" })
  page = 404
  assert.equal(await ctx.refresh(), false)
  assert.match(visible().status, /отвечает не paratrack/)
})

test("nothing answering is the only 'server unreachable'", async () => {
  const { ctx, visible } = load(() => { throw new TypeError("fetch failed") })
  await settle()
  assert.equal(await ctx.refresh(), false)
  assert.deepEqual(visible(), { section: "setup", status: "Сервер не отвечает" })
})

test("an accepted token is saved and says connected on the timer screen", async () => {
  const { el, store, visible } = load(ok, {})
  await settle()
  el.token.value = "pt_good"
  el.base.value = "https://pt.example"
  await el.save.listeners.click()
  assert.equal(store.token, "pt_good")
  assert.deepEqual(visible(), { section: "app", status: "Подключено" })
})
