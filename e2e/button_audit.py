"""Button audit: what shapes does the interface actually ship?

Collects every button-like control on every screen and reports the geometry
the browser really computed — height, corner radius, padding, font size —
grouped by appearance. The question this answers is not "how many buttons
are there" but "how many *shapes* of button does a person have to read".

Covers more than <Button>: raw <button>, links styled as buttons, and the
CSS-drawn controls (period toggles, the phone FAB, bottom navigation), which
never touch the component at all.

Usage:  python e2e/button_audit.py
"""

from __future__ import annotations

import json
import os
import sys
from collections import Counter, defaultdict
from pathlib import Path

from playwright.sync_api import sync_playwright

sys.path.insert(0, str(Path(__file__).parent))
from target import BASE_URL as BASE  # noqa: E402
from browser import engine_name, launch
from color_audit import open_disclosures, register, seed  # noqa: E402

OUT = Path(os.environ.get("PARATRACK_BTN_OUT", "/tmp/button-audit"))
OUT.mkdir(parents=True, exist_ok=True)

ROUTES = [
    "/", "/projects", "/projects/new", "/timesheet", "/stats", "/graph",
    "/goals", "/tags", "/reports", "/invoices", "/payroll", "/schedule",
    "/integrations", "/settings/sections", "/settings/profile",
]

JS = """
() => {
  const sel = 'button, a[href], [role="button"], input[type="submit"], summary';
  const out = [];
  for (const el of document.querySelectorAll('#paratrack-react-root ' + sel)) {
    const r = el.getBoundingClientRect();
    if (r.width < 1 || r.height < 1) continue;
    const st = getComputedStyle(el);
    if (st.display === 'none' || st.visibility === 'hidden') continue;
    const cls = (el.getAttribute('class') || '');
    // a link that wraps nothing and has no button chrome is a text link
    const isPlainLink = el.tagName === 'A' && !el.hasAttribute('data-slot')
      && st.borderTopWidth === '0px' && st.backgroundColor === 'rgba(0, 0, 0, 0)';
    if (isPlainLink) continue;
    out.push({
      tag: el.tagName.toLowerCase(),
      slot: el.getAttribute('data-slot') || '',
      variant: el.getAttribute('data-variant') || '',
      size: el.getAttribute('data-size') || '',
      h: Math.round(r.height * 10) / 10,
      w: Math.round(r.width * 10) / 10,
      radius: st.borderTopLeftRadius,
      px: st.paddingLeft,
      py: st.paddingTop,
      fs: st.fontSize,
      weight: st.fontWeight,
      iconOnly: !el.textContent.trim(),
      cls: cls.slice(0, 90),
      text: (el.textContent || '').trim().slice(0, 24),
    });
  }
  return out;
}
"""


def shape(item):
    """The identity a person perceives: how tall, how round, how inset."""
    return (item["h"], item["radius"], item["fs"])


def main() -> int:
    findings = defaultdict(list)
    with sync_playwright() as p:
        browser = launch(p)
        page = browser.new_page(viewport={"width": 1280, "height": 900})
        register(page)
        seed(page)
        for route in ROUTES:
            page.goto(BASE + route)
            page.wait_for_load_state("networkidle")
            # open every disclosure so controls inside them are measured too
            page.evaluate(
                """() => {
                  for (const p of document.querySelectorAll('[data-state=closed][hidden]')) {
                    const t = document.querySelector(`[aria-controls="${p.id}"]`);
                    if (t) t.click();
                  }
                  for (const d of document.querySelectorAll('details')) d.open = true;
                }"""
            )
            page.wait_for_timeout(400)
            for item in page.evaluate(JS):
                findings[shape(item)].append((route, item))
        browser.close()

    report = {
        "base": BASE,
        "shapes": [
            {
                "height": h, "radius": r, "font": f,
                "count": len(items),
                "sources": sorted({f"{rt}:{i['tag']}{'['+i['variant']+'/'+i['size']+']' if i['slot']=='button' else ''}" for rt, i in items})[:8],
                "sample": items[0][1]["text"] or "(иконка)",
            }
            for (h, r, f), items in sorted(findings.items(), key=lambda kv: -len(kv[1]))
        ],
    }
    (OUT / "report.json").write_text(json.dumps(report, ensure_ascii=False, indent=2))

    total = sum(len(v) for v in findings.values())
    print(f"buttons measured: {total}")
    print(f"distinct shapes:  {len(findings)}\n")
    for s in report["shapes"]:
        print(f"  h={s['height']:>5}  r={s['radius']:<9} font={s['font']:<7} x{s['count']:<3} {s['sample']!r}")
        for src in s["sources"][:4]:
            print(f"        {src}")
    print(f"\nreport: {OUT / 'report.json'}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
