"""Colour audit for the React UI.

Measures, per theme, how much of the rendered interface actually carries
chroma, and which roles do. Reports:

  * every distinct foreground/background pair actually painted, with its
    computed contrast, so AA can be checked rather than assumed;
  * how much of the painted area is non-grey (chroma above a floor);
  * the status pills and document badges as the browser really paints them,
    which is the part the eye cannot be trusted on.

Usage:  python e2e/color_audit.py
"""

from __future__ import annotations

import json
import os
import re
import sys
import time
import uuid
from collections import Counter
from pathlib import Path

from playwright.sync_api import sync_playwright

sys.path.insert(0, str(Path(__file__).parent))
from target import BASE_URL as BASE  # noqa: E402

OUT = Path(os.environ.get("PARATRACK_AUDIT_OUT", "/tmp/color-audit"))
OUT.mkdir(parents=True, exist_ok=True)

# WCAG relative luminance / contrast, computed in Python so the answer does
# not depend on the browser's own judgement.
def parse(css: str):
    """Parse a computed colour into (r, g, b, a). Handles rgb()/rgba()."""
    css = css.strip()
    if css.startswith("oklch"):
        return None  # the browser already resolved these; see note below
    if not css.startswith("rgb"):
        return None
    inner = css[css.index("(") + 1 : css.rindex(")")]
    parts = [p.strip() for p in inner.replace("/", ",").split(",") if p.strip() != ""]
    nums = []
    for p in parts[:4]:
        try:
            nums.append(float(p))
        except ValueError:
            nums.append(1.0)
    while len(nums) < 3:
        nums.append(0.0)
    a = nums[3] if len(nums) > 3 else 1.0
    return (nums[0], nums[1], nums[2], a)


def over(fg, bg):
    """Composite fg (with alpha) over opaque bg."""
    a = fg[3]
    return tuple(fg[i] * a + bg[i] * (1 - a) for i in range(3)) + (1.0,)


def lum(rgb):
    def chan(c):
        c = c / 255.0
        return c / 12.92 if c <= 0.03928 else ((c + 0.055) / 1.055) ** 2.4
    r, g, b = (chan(x) for x in rgb[:3])
    return 0.2126 * r + 0.7152 * g + 0.0722 * b


def contrast(a, b):
    la, lb = lum(a), lum(b)
    hi, lo = max(la, lb), min(la, lb)
    return (hi + 0.05) / (lo + 0.05)


def chroma(rgb):
    r, g, b = rgb[:3]
    return max(r, g, b) - min(r, g, b)


# A colour has to be resolved to bytes before it can be compared. Chromium
# keeps oklch() as oklch() in getComputedStyle, and canvas only accepts it as a
# string but paints it correctly — so the pixel is the ground truth here.
# Measured: oklch(0.52 0.185 284) → [97, 80, 205], which is #6150cd, the same
# brand colour the email template uses.
JS_RESOLVE = """
(css) => {
  const c = document.createElement('canvas');
  c.width = c.height = 1;
  const x = c.getContext('2d', { willReadFrequently: true });
  x.clearRect(0, 0, 1, 1);
  x.fillStyle = '#000';
  x.fillStyle = css;
  x.fillRect(0, 0, 1, 1);
  const d = x.getImageData(0, 0, 1, 1).data;
  if (d[3] === 0) return [0, 0, 0, 0];
  return [d[0], d[1], d[2], d[3] / 255];
}
"""

JS_PROBE = """
() => {
  const px = (el) => getComputedStyle(el);
  const canvas = document.createElement('canvas');
  canvas.width = canvas.height = 1;
  const ctx = canvas.getContext('2d', { willReadFrequently: true });
  const resolve = (css) => {
    ctx.clearRect(0, 0, 1, 1);
    ctx.fillStyle = '#000';
    ctx.fillStyle = css;
    ctx.fillRect(0, 0, 1, 1);
    const d = ctx.getImageData(0, 0, 1, 1).data;
    return [d[0], d[1], d[2], d[3] / 255];
  };
  const stack = (els) => {
    const out = [];
    for (const el of els) { out.push(el); if (el.shadowRoot) out.push(...el.shadowRoot.querySelectorAll('*')); }
    return out;
  };
  const roots = [...document.querySelectorAll('#paratrack-react-root, #paratrack-react-root *')];
  const nodes = stack(roots);
  const root = document.querySelector('#paratrack-react-root');
  const out = { text: [], chips: [], fills: [], total: 0, painted: 0 };
  for (const el of nodes) {
    const r = el.getBoundingClientRect();
    if (r.width < 1 || r.height < 1) continue;
    const st = px(el);
    if (st.visibility === 'hidden' || st.display === 'none') continue;
    out.total += r.width * r.height;
    const bg = resolve(st.backgroundColor);
    const area = r.width * r.height;
    out.painted += area;
    if (bg[3] > 0.05) out.fills.push({ cls: (el.getAttribute('class') || '').slice(0, 60), bg: bg, area });
    const own = [...el.childNodes].some(n => n.nodeType === 3 && n.textContent.trim());
    if (own) {
      // Walk up for the first opaque background actually behind the text.
      let bgCss = 'rgb(255,255,255)', n = el;
      while (n && n !== document.documentElement.parentElement) {
        const cand = resolve(px(n).backgroundColor);
        if (cand[3] > 0.9) { bgCss = `rgb(${cand[0]},${cand[1]},${cand[2]})`; break; }
        n = n.parentElement;
      }
      const fg = resolve(st.color);
      out.text.push({
        tag: el.tagName.toLowerCase(),
        slot: el.getAttribute('data-slot') || '',
        fg: `rgb(${fg[0]},${fg[1]},${fg[2]})`,
        fgA: fg[3],
        bg: bgCss,
        size: parseFloat(st.fontSize),
        weight: parseInt(st.fontWeight, 10) || 400,
        text: (el.textContent || '').trim().slice(0, 40),
      });
    }
    const cls = el.getAttribute('class') || '';
    const slot = el.getAttribute('data-slot') || '';
    if ((slot === 'badge' || cls.includes('status-pill') || cls.includes('doc-status')) && bg[3] > 0.05) {
      out.chips.push({
        cls: cls.slice(0, 90), color: st.color, bg: st.backgroundColor,
        border: st.borderColor, text: (el.textContent || '').trim().slice(0, 30),
      });
    }
  }
  out.rootBg = root ? px(root).backgroundColor : null;
  out.bodyBg = px(document.body).backgroundColor;
  return out;
}
"""


def open_disclosures(page, selector, scope="body"):
    """Reveal `selector` by opening whatever hides it.

    Two mechanisms in this app, and both have to be handled: a native
    <details> on the server-rendered screens, and a Radix Collapsible on the
    React ones (measured: the project form's slug field lives in a
    `<Disclosure>`, not a <details>, so walking ancestors for DETAILS never
    finds it). Click the visible trigger first, then fall back to <details>.
    """
    root = page.locator(scope) if scope != "body" else page

    def visible():
        try:
            el = root.locator(selector).first
            return bool(el.count()) and el.is_visible()
        except Exception:
            return False

    if visible():
        return True
    # The label-based attempt comes first: clicking every Radix trigger on the
    # screen also toggles panels that are already open, which closes the one
    # being asked for (measured on /projects/new).
    for label in (
        "Новый счёт",
        "New invoice",
        "Цвет, адрес и ставка",
        "Color, URL and hourly rate",
        "Изменить",
        "Edit",
        "Настройки",
        "Settings",
        "Добавить прошедшую сессию",
        "Add a past session",
    ):
        for pattern in ("button", "[role=button]", "summary"):
            trig = page.locator(f"{pattern}:has-text('{label}')").first
            if trig.count():
                try:
                    trig.click(timeout=2000)
                    page.wait_for_timeout(250)
                    if visible():
                        return True
                except Exception:
                    pass
    # Anything still closed: open by aria-controls, then native <details>.
    page.evaluate(
        """() => {
          for (const panel of document.querySelectorAll('[data-state=closed][hidden], [hidden][data-radix-collapsible-content]')) {
            const id = panel.id;
            const trigger = id && document.querySelector(`[aria-controls="${id}"]`);
            if (trigger) trigger.click();
          }
          for (const s of document.querySelectorAll('details')) if (!s.open) s.open = true;
        }"""
    )
    page.wait_for_timeout(300)
    for _ in range(6):
        opened = page.evaluate(
            """(sel) => {
              const target = document.querySelector(sel);
              if (!target) return false;
              let n = target.parentElement;
              while (n && n !== document.body) {
                if (n.tagName === 'DETAILS' && !n.open) { n.open = true; return true; }
                n = n.parentElement;
              }
              return false;
            }""",
            selector,
        )
        if not opened:
            break
        page.wait_for_timeout(150)
        if visible():
            return True
    return visible()


def seed(page):
    """Put one of every state on screen.

    Status chips only exist where there is a document in that status, so an
    empty workspace shows no colour at all — which is exactly how a
    "monochrome UI" claim gets made by accident. Measured: /invoices on a
    fresh account reports 0 chips.

    Everything goes through the screens rather than the API: /api/start
    answered 200 from the browser and still wrote nothing the stats page
    could see, because the period and the CSRF token each came from a
    different page. The UI path is what qa_full.py already relies on.
    """
    stamp = int(os.environ.get("PARATRACK_TS", "0")) or 1700000000

    # A project with a colour and an hourly rate, both set at creation time
    # (measured: the create form carries #new-project-slug, #new-project-rate
    # and the colour input; the detail page's settings panel has no rate
    # field at all). Without a rate its time is not billable and the invoice
    # generator answers "no billable time in that period".
    page.goto(BASE + "/projects/new")
    page.wait_for_load_state("networkidle")
    page.fill("#new-project-name", "Nordwind")
    open_disclosures(page, "#new-project-slug")
    page.fill("#new-project-slug", f"nordwind-{stamp}")
    open_disclosures(page, "#new-project-rate")
    page.fill("#new-project-rate", "100")
    color_input = page.locator('input[name="color"][pattern]').first
    if color_input.count():
        color_input.fill("#6366f1")
    page.get_by_role("button", name=re.compile("Создать|Create", re.I), exact=False).first.click()
    page.wait_for_timeout(1200)

    # Tracked time on it. The backfill form lives on the dashboard (#b-activity /
    # #b-start / #b-end), not on /stats — measured: /stats renders no such
    # inputs at all.
    page.goto(BASE + "/")
    page.wait_for_load_state("networkidle")
    # The backfill form is inside a collapsed section on the dashboard.
    open_disclosures(page, "#b-activity")
    project_slugs = page.evaluate(
        """async () => {
          const r = await fetch('/api/projects', {credentials: 'same-origin'});
          const j = await r.json();
          const list = j.projects || j.items || j || [];
          return list.map(p => ({id: p.id, name: p.name, slug: p.slug}));
        }"""
    )
    project_id = project_slugs[0]["id"] if project_slugs else None
    for activity, hours in (("Разработка", 3), ("Правки", 1), ("Созвон", 2)):
        now = time.localtime()
        start = time.localtime(time.time() - hours * 3600)
        pad = lambda n: str(n).zfill(2)
        stamp = lambda t: f"{t.tm_year}-{pad(t.tm_mon)}-{pad(t.tm_mday)} {pad(t.tm_hour)}:{pad(t.tm_min)}:00"
        page.fill("#b-activity", activity)
        page.fill("#b-start", stamp(start))
        page.fill("#b-end", stamp(now))
        if project_id:
            # Unassigned time is not invoiceable, so the session must carry its
            # project. The form picks it through a Radix select: a trigger
            # button (#b-project) plus a hidden <select> holding the value.
            # Measured: no [name=project_id] on the form.
            trigger = page.locator("#b-project").first
            if trigger.count():
                try:
                    trigger.click(timeout=3000)
                    page.wait_for_timeout(300)
                    opt = page.get_by_role("option", name=re.compile("Nordwind", re.I)).first
                    if opt.count():
                        opt.click(timeout=3000)
                        page.wait_for_timeout(250)
                    else:
                        page.keyboard.press("Escape")
                except Exception as exc:
                    print(f"  ! project select: {exc}")
        page.locator('form:has(#b-activity) button[type="submit"]').first.click()
        page.wait_for_timeout(900)
        warned = page.evaluate("()=>[...document.querySelectorAll('[role=alert]')].map(e=>e.textContent.trim()).filter(Boolean)")
        if warned:
            print(f"  ! {activity}: {warned}")

    # Bind an activity to the project, without which time is not invoiceable.
    page.goto(BASE + f"/projects/nordwind-{stamp}")
    page.wait_for_load_state("networkidle")

    # Two invoices: one paid, one still open. Paid against unpaid is the pair
    # a person has to tell apart at a glance.
    for client in ("Nordwind GmbH", "Второй клиент"):
        page.goto(BASE + "/invoices")
        page.wait_for_load_state("networkidle")
        open_disclosures(page, 'input[name="client"]')
        page.fill('input[name="client"]', client)
        page.locator('form[action="/invoices"] button[type="submit"]').click()
        page.wait_for_timeout(1200)
        if client.startswith("Nordwind"):
            mark = page.locator('form[action$="/paid"]').first
            if mark.count():
                mark.click()
                page.wait_for_timeout(1000)
            else:
                print("  ! no 'mark paid' control found")

    page.goto(BASE + "/payroll")
    page.wait_for_load_state("networkidle")
    open_disclosures(page, 'form[action="/payroll"] button[type="submit"]')
    if page.locator('form[action="/payroll"] button[type="submit"]').count():
        page.locator('form[action="/payroll"] button[type="submit"]').click()
        page.wait_for_timeout(900)
    return {"seeded": stamp}


def audit(page, theme):
    page.evaluate(
        "t => { document.documentElement.setAttribute('data-theme', t); }",
        "paratrack-dark" if theme == "dark" else "light",
    )
    page.wait_for_timeout(350)
    return page.evaluate(JS_PROBE)


def register(page):
    email = f"ca-{uuid.uuid4().hex[:10]}@x.test"
    page.goto(BASE + "/register")
    page.fill("#name", "Color Audit")
    page.fill("#email", email)
    page.fill("#password", "longenoughpw")
    page.click('button[type="submit"]')
    page.wait_for_load_state("load")
    if page.url.endswith("/welcome"):
        # studio, not solo: solo has no invoices and no payroll, and the
        # document statuses are exactly what this audit is about.
        page.locator(
            'form[action="/api/team/modules"]:has(input[name="preset"][value="studio"]):not([data-welcome-skip]) button'
        ).click()
        page.wait_for_url(BASE + "/")
    return email


def main() -> int:
    report = {"base": BASE, "themes": {}}
    with sync_playwright() as p:
        browser = p.chromium.launch()
        page = browser.new_page(viewport={"width": 1280, "height": 900})
        register(page)
        seed(page)
        for route in ("/", "/invoices", "/payroll", "/graph", "/stats", "/projects", "/timesheet"):
            page.goto(BASE + route)
            page.wait_for_load_state("networkidle")
            for theme in ("light", "dark"):
                res = audit(page, theme)
                key = f"{route}#{theme}"
                pairs = Counter()
                lows = []
                chroma_area = 0.0
                for item in res["text"]:
                    fg = parse(item["fg"])
                    bgc = parse(item["bg"])
                    if not fg or not bgc:
                        continue
                    fg = over(fg, bgc)
                    c = contrast(fg, bgc)
                    need = 3.0 if (item["size"] >= 24 or (item["size"] >= 18.66 and item["weight"] >= 700)) else 4.5
                    pairs[(item["fg"], item["bg"])] += 1
                    if c + 0.005 < need:
                        lows.append({"c": round(c, 2), "need": need, "text": item["text"],
                                     "fg": item["fg"], "bg": item["bg"]})
                for fill in res["fills"]:
                    # A wash tinted 12% is still colour; a 4% hairline is not
                    # what a person reads as "this area has a colour".
                    if chroma(fill["bg"][:3]) >= 12 and fill["bg"][3] > 0.5:
                        chroma_area += fill["area"]
                report["themes"][key] = {
                    "distinct_pairs": len(pairs),
                    "text_nodes": sum(pairs.values()),
                    "below_AA": lows[:12],
                    "chips": res["chips"][:20],
                    "chroma_share": round(chroma_area / max(res["painted"], 1), 4),
                }
            page.screenshot(path=str(OUT / f"{route.strip('/').replace('/', '-') or 'home'}.png"), full_page=False)
        browser.close()
    (OUT / "report.json").write_text(json.dumps(report, ensure_ascii=False, indent=2))

    for key, data in report["themes"].items():
        low = len(data["below_AA"])
        print(f"{key:22} pairs={data['distinct_pairs']:3} text={data['text_nodes']:4} "
              f"belowAA={low} chroma={data['chroma_share'] * 100:.1f}%")
        for item in data["below_AA"]:
            print(f"    ! {item['c']} (need {item['need']}) {item['fg']} on {item['bg']} — {item['text']!r}")
    print(f"\nreport: {OUT / 'report.json'}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())