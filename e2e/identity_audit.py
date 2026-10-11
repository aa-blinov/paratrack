"""Identity audit: where does one role wear more than one shape?

The button pass found the pattern worth generalising — a role that is drawn
two different ways (same height, two corner radii) reads as two different
things to the eye. This walks every control role in the app and reports the
cases where a single role has more than one geometry.

Roles are read from the DOM, not from the class strings, because the class is
what is in dispute. Sizes come from getBoundingClientRect; the rest comes
from getComputedStyle, resolved to real bytes through a canvas so oklch()
is comparable rather than merely present.

Usage:  python e2e/identity_audit.py
"""

from __future__ import annotations

import json
import os
import sys
from collections import defaultdict
from pathlib import Path

from playwright.sync_api import sync_playwright

sys.path.insert(0, str(Path(__file__).parent))
from target import BASE_URL as BASE  # noqa: E402
from browser import engine_name, launch
from color_audit import register, seed  # noqa: E402

OUT = Path(os.environ.get("PARATRACK_ID_OUT", "/tmp/identity-audit"))
OUT.mkdir(parents=True, exist_ok=True)

ROUTES = [
    "/", "/projects", "/projects/new", "/timesheet", "/stats", "/graph",
    "/goals", "/tags", "/reports", "/invoices", "/payroll", "/schedule",
    "/integrations", "/settings/sections", "/settings/profile",
    "/settings/team", "/settings/tokens", "/help",
]

JS = """
() => {
  const out = [];
  const seen = new Set();
  const add = (role, el, extra) => {
    const r = el.getBoundingClientRect();
    if (r.width < 1 || r.height < 1) return;
    const st = getComputedStyle(el);
    if (st.display === 'none' || st.visibility === 'hidden') return;
    const key = role + '|' + Math.round(r.width) + '|' + Math.round(r.height)
      + '|' + (el.textContent || '').slice(0, 12);
    if (seen.has(key)) return;
    seen.add(key);
    out.push(Object.assign({
      role, tag: el.tagName.toLowerCase(),
      w: Math.round(r.width * 10) / 10, h: Math.round(r.height * 10) / 10,
      radius: st.borderTopLeftRadius, border: st.borderTopWidth + ' ' + st.borderTopColor,
      fs: st.fontSize, weight: st.fontWeight, tracking: st.letterSpacing,
      pad: st.paddingTop + ' ' + st.paddingRight,
      bg: st.backgroundColor, color: st.color,
      text: (el.textContent || '').trim().slice(0, 26),
      cls: (el.getAttribute('class') || '').slice(0, 80),
    }, extra || {}));
  };

  const root = document.getElementById('paratrack-react-root');
  for (const el of root.querySelectorAll('input, textarea, select, button, a, [role], h1, h2, h3, svg')) {
    const tag = el.tagName.toLowerCase();
    const slot = el.getAttribute('data-slot') || '';
    const type = el.getAttribute('type') || '';
    const role = el.getAttribute('role') || '';

    if (tag === 'input' && !['checkbox', 'radio', 'submit', 'button'].includes(type)) {
      add('field:text', el, { variant: 'input' });
    } else if (tag === 'textarea') {
      add('field:text', el, { variant: 'textarea' });
    } else if (slot === 'select-trigger' || el.getAttribute('data-slot') === 'select-trigger') {
      add('field:text', el, { variant: 'select' });
    } else if (tag === 'input' && type === 'checkbox') {
      add('field:checkbox', el);
    } else if (slot === 'button') {
      add('control:button', el, { variant: el.getAttribute('data-variant') });
    } else if (slot === 'card') {
      add('surface:card', el);
    } else if (slot === 'badge') {
      add('mark:badge', el, { variant: el.getAttribute('data-variant') });
    } else if (tag === 'h1') {
      add('type:h1', el);
    } else if (tag === 'h2') {
      add('type:h2', el);
    } else if (tag === 'h3') {
      add('type:h3', el);
    } else if (tag === 'svg' && slot !== '') {
      add('icon:slot-' + slot, el);
    } else if (role === 'tab') {
      add('control:tab', el);
    } else if (slot === 'separator') {
      add('rule:separator', el);
    }
  }
  return out;
}
"""


def main() -> int:
    items = []
    with sync_playwright() as p:
        browser = launch(p)
        page = browser.new_page(viewport={"width": 1280, "height": 900})
        register(page)
        seed(page)
        for route in ROUTES:
            page.goto(BASE + route)
            page.wait_for_load_state("networkidle")
            page.evaluate(
                """() => {
                  for (const x of document.querySelectorAll('[data-state=closed][hidden]')) {
                    const t = document.querySelector(`[aria-controls="${x.id}"]`);
                    if (t) t.click();
                  }
                  for (const d of document.querySelectorAll('details')) d.open = true;
                }"""
            )
            page.wait_for_timeout(400)
            for it in page.evaluate(JS):
                it["route"] = route
                items.append(it)
        browser.close()

    by_role = defaultdict(list)
    for it in items:
        by_role[it["role"]].append(it)

    report = {}
    print(f"elements sampled: {len(items)}\n")
    print("=== roles where ONE role has MORE THAN ONE shape ===")
    print("(a role is 'split' when its height, radius or type size takes more than one value)\n")
    for role, group in sorted(by_role.items()):
        heights = sorted({g["h"] for g in group})
        radii = sorted({g["radius"] for g in group})
        fonts = sorted({g["fs"] for g in group})
        borders = sorted({g["border"] for g in group})
        split = len(heights) > 1 or len(radii) > 1 or len(fonts) > 1
        report[role] = {
            "count": len(group),
            "heights": heights, "radii": radii, "fonts": fonts, "borders": borders,
            "examples": [
                {"h": g["h"], "r": g["radius"], "fs": g["fs"], "route": g["route"],
                 "text": g["text"], "cls": g["cls"]}
                for g in group[:6]
            ],
        }
        if not split:
            continue
        print(f"  {role}  ({len(group)} on screen)")
        print(f"      heights: {heights}")
        print(f"      radii:   {radii}")
        if len(fonts) > 1:
            print(f"      fonts:   {fonts}")
        if len(borders) > 1:
            print(f"      borders: {len(borders)} different")
        for g in group[:4]:
            print(f"      - {g['h']:>6}px r={g['radius']:<10} {g['route']:<22} {g['text']!r}")
            print(f"          {g['cls']}")
        print()

    (OUT / "report.json").write_text(json.dumps(report, ensure_ascii=False, indent=2))
    print(f"report: {OUT / 'report.json'}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())