"""Wave 9 verification — offline mode + web push, against a live server.

Run:  . .venv/bin/activate && python e2e/wave9_verify.py
Saves screenshots to e2e/screenshots/wave9/.
"""
from __future__ import annotations

import pathlib
import sys
import time

from playwright.sync_api import sync_playwright

BASE = __import__("os").environ.get("PARATRACK_BASE", "https://paratrack.duckdns.org")
OUT = pathlib.Path(__file__).resolve().parent / "screenshots" / "wave9"
OUT.mkdir(parents=True, exist_ok=True)

PASS, FAIL = 0, 0


def check(name: str, ok: bool, detail: str = ""):
    global PASS, FAIL
    mark = "PASS" if ok else "FAIL"
    if ok:
        PASS += 1
    else:
        FAIL += 1
    print(f"  [{mark}] {name}" + (f" — {detail}" if detail else ""))


def main() -> int:
    email = f"wave9{int(time.time())}@x.test"
    with sync_playwright() as p:
        br = p.chromium.launch()
        ctx = br.new_context(viewport={"width": 1280, "height": 800})
        pg = ctx.new_page()
        errors: list[str] = []
        pg.on("pageerror", lambda e: errors.append(str(e)))

        # ---- register / login ----
        pg.goto(f"{BASE}/register", wait_until="networkidle")
        pg.fill("input[name=name]", "Wave9")
        pg.fill("input[name=email]", email)
        pg.fill("input[name=password]", "longenoughpw")
        pg.click("button[type=submit]")
        pg.wait_for_url(f"{BASE}/", timeout=15000)
        check("register + land on dashboard", True)

        # ---- service worker registers ----
        pg.goto(f"{BASE}/", wait_until="networkidle")
        pg.wait_for_timeout(1500)
        sw = pg.evaluate("""async () => {
          if (!("serviceWorker" in navigator)) return {ok:false, why:"no SW api"};
          const reg = await navigator.serviceWorker.getRegistration();
          return {ok: !!reg, scope: reg && reg.scope, active: !!(reg && reg.active)};
        }""")
        check("service worker registered", sw.get("ok") and sw.get("active"), str(sw))
        pg.screenshot(path=str(OUT / "a1-dashboard.png"), full_page=True)

        # ---- offline banner + mutation queue ----
        pg.goto(f"{BASE}/", wait_until="networkidle")
        ctx.set_offline(True)
        pg.wait_for_timeout(400)
        pg.evaluate("window.dispatchEvent(new Event('offline'))")
        pg.wait_for_timeout(300)
        banner = pg.evaluate("""() => {
          const el = document.getElementById("offline-banner");
          return el ? {text: el.textContent.trim(), visible: el.style.display !== "none"} : null;
        }""")
        check("offline banner appears", bool(banner and banner.get("visible")), str(banner))
        pg.screenshot(path=str(OUT / "b1-offline-banner.png"), full_page=True)

        # queue a mutation while offline
        queued = pg.evaluate("""() => {
          try {
            fetch("/api/start", {
              method: "POST",
              credentials: "same-origin",
              headers: {"Content-Type": "application/x-www-form-urlencoded",
                        "X-CSRF-Token": decodeURIComponent((document.cookie.match(/paratrack_csrf=([^;]+)/)||[])[1]||"")},
              body: new URLSearchParams({csrf_token: decodeURIComponent((document.cookie.match(/paratrack_csrf=([^;]+)/)||[])[1]||""), activity: "wave9-offline-task"}),
            }).catch(()=>{});
            return true;
          } catch (e) { return false; }
        }""")
        pg.wait_for_timeout(600)
        q = pg.evaluate("() => JSON.parse(localStorage.getItem('paratrack-offline-queue') || '[]')")
        has_body = isinstance(q, list) and len(q) >= 1 and (q[0].get("body") or {}).get("activity") == "wave9-offline-task"
        check("offline mutation enters the queue (with body)", has_body, f"queued={q}")
        pg.screenshot(path=str(OUT / "b2-offline-queued.png"), full_page=True)

        # ---- back online: queue flushes ----
        ctx.set_offline(False)
        pg.evaluate("window.dispatchEvent(new Event('online'))")
        pg.wait_for_timeout(1500)
        q2 = pg.evaluate("() => JSON.parse(localStorage.getItem('paratrack-offline-queue') || '[]')")
        check("queue flushes after reconnect", isinstance(q2, list) and len(q2) == 0,
              f"remaining={q2}")
        active = pg.evaluate("""async () => {
          const r = await fetch("/api/me", {credentials:"same-origin"});
          return r.status;
        }""")
        # the replayed start must have produced a live session
        has_sess = pg.evaluate("""async () => {
          const r = await fetch("/api/v1/sessions", {credentials:"same-origin"});
          if (!r.ok) return {ok:false, status:r.status};
          const j = await r.json();
          const items = j.sessions || j.items || j || [];
          const list = Array.isArray(items) ? items : [];
          return {ok: list.some(s => (s.activity||s.name||"").includes("wave9-offline-task")), n: list.length};
        }""")
        check("replayed offline start actually created the session", bool(has_sess.get("ok")), str(has_sess))
        pg.screenshot(path=str(OUT / "b3-online-flushed.png"), full_page=True)

        # ---- push: VAPID public key endpoint ----
        key = pg.evaluate("""async () => {
          const r = await fetch("/api/push/key", {credentials:"same-origin"});
          if (!r.ok) return {ok:false, status:r.status};
          const j = await r.json();
          const k = j.publicKey || j.key; return {ok:true, hasKey: typeof k === "string" && k.length > 10, key: (k||"").slice(0,12)+"…"};
        }""")
        check("GET /api/push/key returns VAPID public key", key.get("ok") and key.get("hasKey"), str(key))

        # ---- push settings page ----
        pg.goto(f"{BASE}/settings/notifications", wait_until="networkidle")
        check("push page exposes window.paratrackPush",
              pg.evaluate("typeof window.paratrackPush") == "object")
        ds = pg.eval_on_selector("#push-status", "el => ({a: el.dataset.msgActive, o: el.dataset.msgOff, d: el.dataset.msgDenied})")
        check("push status messages are bound (data-msg-*)",
              all(ds.get(k) for k in ("a", "o", "d")), str(ds))
        pg.screenshot(path=str(OUT / "c1-push-settings.png"), full_page=True)

        # click Enable — headless chrome has notification permission denied by default,
        # so the status must show the localized denied message (not stay empty).
        pg.click("#push-enable")
        pg.wait_for_timeout(800)
        st = pg.eval_on_selector("#push-status", "el => el.textContent.trim()")
        check("Enable push updates status (no silent click)", len(st) > 0, repr(st))
        pg.screenshot(path=str(OUT / "c2-push-status.png"), full_page=True)

        # subscribe API shape (fake endpoint) — server should accept and store
        sub = pg.evaluate("""async () => {
          const csrf = decodeURIComponent((document.cookie.match(/paratrack_csrf=([^;]+)/)||[])[1]||"");
          const body = new URLSearchParams({
            csrf_token: csrf,
            endpoint: "https://fcm.googleapis.com/fcm/send/wave9-test-endpoint",
            p256dh: "BPtestp256dh",
            auth: "testauth",
          });
          const r = await fetch("/api/push/subscribe", {
            method: "POST", credentials: "same-origin",
            headers: {"Content-Type": "application/x-www-form-urlencoded", "X-CSRF-Token": csrf},
            body,
          });
          return {status: r.status};
        }""")
        check("POST /api/push/subscribe accepts a subscription", sub.get("status") in (200, 204), str(sub))

        unsub = pg.evaluate("""async () => {
          const csrf = decodeURIComponent((document.cookie.match(/paratrack_csrf=([^;]+)/)||[])[1]||"");
          const r = await fetch("/api/push/unsubscribe", {
            method: "POST", credentials: "same-origin",
            headers: {"Content-Type": "application/x-www-form-urlencoded", "X-CSRF-Token": csrf},
            body: new URLSearchParams({csrf_token: csrf, endpoint: "https://fcm.googleapis.com/fcm/send/wave9-test-endpoint"}),
          });
          return {status: r.status};
        }""")
        check("POST /api/push/unsubscribe removes it", unsub.get("status") in (200, 204), str(unsub))

        # ---- PWA manifest still reachable ----
        man = pg.evaluate("""async () => {
          const r = await fetch("/static/manifest.webmanifest", {credentials:"same-origin"});
          if (!r.ok) return {ok:false, status:r.status};
          const j = await r.json();
          return {ok:true, name: j.name, display: j.display, icons: (j.icons||[]).length};
        }""")
        check("PWA manifest is valid JSON", man.get("ok") and man.get("display") == "standalone", str(man))

        # ---- no page errors ----
        check("no uncaught page errors", len(errors) == 0, str(errors[:3]))

        br.close()

    print(f"\nWave 9: {PASS}/{PASS+FAIL} passed · screenshots → {OUT}")
    return 1 if FAIL else 0


if __name__ == "__main__":
    sys.exit(main())
