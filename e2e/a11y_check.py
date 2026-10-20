"""Accessibility regressions that the design detector cannot catch.

The impeccable detector scans HTML and CSS but not `.tsx`, so the React tree
was never scanned — proven by feeding it a byte-identical file with two
extensions and watching the count change from 4 to 0. Saying "detector clean"
about this app therefore said nothing.

This is the replacement: the same measurements the detector would have made,
run against the live DOM in CI, where a regression fails the build instead of
being noticed by eye.

Checks, per theme:
  * every visible text node meets WCAG AA against its composited background;
  * every form control's boundary meets the 3:1 of 1.4.11;
  * every control's accessible name contains its visible text (2.5.3), so
    voice-control users can say what they see.

Run:  python e2e/a11y_check.py        (needs the QA stand running)
"""

from __future__ import annotations

import os
import sys
from pathlib import Path

from playwright.sync_api import sync_playwright

sys.path.insert(0, str(Path(__file__).parent))
from target import BASE_URL as BASE  # noqa: E402
from browser import engine_name, launch
from color_audit import contrast, over, parse, register, seed  # noqa: E402

ROUTES = [
    "/", "/projects", "/timesheet", "/stats", "/graph",
    "/invoices", "/payroll", "/schedule", "/settings/sections",
    "/settings/profile",
]

# getComputedStyle hands back oklch() as oklch(), which is useless to compare.
# Painting it on a canvas and reading the pixel is the ground truth.
JS = """
() => {
  const px = c => {
    const cv = document.createElement('canvas');
    cv.width = cv.height = 1;
    const x = cv.getContext('2d', { willReadFrequently: true });
    x.fillStyle = '#000'; x.fillStyle = c;
    x.fillRect(0, 0, 1, 1);
    const d = x.getImageData(0, 0, 1, 1).data;
    return [d[0], d[1], d[2], d[3] / 255];
  };
  // Composite the background down the tree instead of stopping at the first
  // opaque layer. Skipping a translucent one is wrong: the button's own
  // `hover:bg-primary/80` is 80% opaque, so a walk that requires alpha > 0.9
  // walks straight past it, lands on the white card, and reports white text on
  // a white background at 1.04:1 — a finding that never existed. Measured in
  // Firefox with the pointer resting on the button; Chromium reported the same
  // element fully opaque and stayed silent about a real defect.
  const bgOf = el => {
    const layers = [];
    let n = el;
    while (n && n !== document.documentElement) {
      const c = px(getComputedStyle(n).backgroundColor);
      if (c[3] > 0) layers.push(c);
      if (c[3] >= 0.999) break;
      n = n.parentElement;
    }
    let out = px(getComputedStyle(document.body).backgroundColor);
    for (let i = layers.length - 1; i >= 0; i--) {
      const [r, g, b, a] = layers[i];
      out = [r * a + out[0] * (1 - a), g * a + out[1] * (1 - a), b * a + out[2] * (1 - a), 1];
    }
    return out;
  };
  const root = document.getElementById('paratrack-react-root');
  const text = [], controls = [], names = [];
  // An element can compute display:block while an ancestor is hidden — the
  // bottom navigation is `hidden lg:flex`, so its children still measure as
  // laid out. Checking the element alone counted the phone-only controls on
  // a desktop viewport and invented findings. offsetParent is null for
  // anything inside a display:none subtree.
  const shown = el => el.offsetParent !== null || el.getClientRects().length > 0;
  for (const el of root.querySelectorAll('*')) {
    const st = getComputedStyle(el);
    if (st.display === 'none' || st.visibility === 'hidden') continue;
    const r = el.getBoundingClientRect();
    if (r.width < 1 || r.height < 1) continue;
    if (!shown(el)) continue;

    const own = [...el.childNodes].some(n => n.nodeType === 3 && n.textContent.trim());
    if (own) {
      const fg = px(st.color), bg = bgOf(el);
      text.push({
        fg: `rgb(${fg[0]},${fg[1]},${fg[2]})`, bg: `rgb(${bg[0]},${bg[1]},${bg[2]})`,
        size: parseFloat(st.fontSize), weight: parseInt(st.fontWeight, 10) || 400,
        what: (el.textContent || '').trim().slice(0, 30),
      });
    }

    // A field's edge is what identifies it; 1.4.11 asks 3:1 for that edge.
    if (['INPUT', 'TEXTAREA', 'SELECT'].includes(el.tagName)
        && ['checkbox', 'radio', 'hidden'].indexOf(el.type) < 0) {
      const bw = parseFloat(st.borderTopWidth);
      if (bw > 0) {
        const bc = px(st.borderTopColor), bg = bgOf(el.parentElement || el);
        controls.push({
          border: `rgb(${bc[0]},${bc[1]},${bc[2]})`, bg: `rgb(${bg[0]},${bg[1]},${bg[2]})`,
          what: el.id || el.getAttribute('name') || el.tagName.toLowerCase(),
        });
      }
    }

    // 2.5.3: the accessible name has to contain the visible label text.
    // A combobox is excluded on purpose: what it displays is the current
    // value, not the label of the control. "Без проекта" under the name
    // "Проект (необязательно)" is correct behaviour — a screen reader needs
    // both, and 2.5.3 is about a user saying the visible *label* aloud.
    if (['BUTTON', 'A'].includes(el.tagName)
        && el.getAttribute('role') !== 'combobox'
        && (el.textContent || '').trim()) {
      const visible = (el.innerText || '').replace(/\\s+/g, ' ').trim();
      const name = (el.getAttribute('aria-label') || el.innerText || '').replace(/\\s+/g, ' ').trim();
      if (visible && name && !name.includes(visible) && !visible.includes(name)) {
        names.push({ visible: visible.slice(0, 40), name: name.slice(0, 40) });
      }
    }
  }
  return { text, controls, names };
}
"""


def main() -> int:
    text_fails, control_fails, name_fails = [], [], []
    with sync_playwright() as p:
        browser = launch(p)
        page = browser.new_page(viewport={"width": 1280, "height": 900})
        register(page)
        seed(page)
        # The app transitions colours. Measuring 200ms after flipping
        # data-theme caught the middle of the transition: a dark background
        # with the light theme's near-black text still in flight, reported as
        # 1.1:1 for the whole sidebar. The wait cannot fix it — only the
        # runner's speed decides — so the transitions are switched off for the
        # duration of the measurement.
        for route in ROUTES:
            page.goto(BASE + route)
            page.wait_for_load_state("networkidle")
            page.add_style_tag(content="*,*::before,*::after{transition:none!important;animation:none!important}")
            page.wait_for_timeout(250)
            # Both widths: the bottom navigation exists only on a phone and the
            # sidebar footer only on a desktop, so each viewport reveals a
            # different set of controls. Checking one and calling it covered
            # would leave the other unmeasured.
            for width in (1280, 390):
                page.set_viewport_size({"width": width, "height": 900})
                page.wait_for_timeout(150)
                for theme in ("light", "dark"):
                    page.evaluate(
                        "t => document.documentElement.setAttribute('data-theme', t)",
                        "paratrack-dark" if theme == "dark" else "light",
                    )
                    page.wait_for_timeout(200)
                    res = page.evaluate(JS)
                    where = f"{route}@{width}"
                    for item in res["text"]:
                        fg = parse(item["fg"]); bg = parse(item["bg"])
                        if not fg or not bg:
                            continue
                        ratio = contrast(over(fg, bg), bg)
                        large = item["size"] >= 24 or (item["size"] >= 18.66 and item["weight"] >= 700)
                        need = 3.0 if large else 4.5
                        if ratio + 0.005 < need:
                            # Keep the raw pair: a contrast number without the
                            # colours it came from cannot be checked by whoever
                            # reads the report.
                            text_fails.append((round(ratio, 2), need, theme, where,
                                                item["what"], item["fg"], item["bg"]))
                    for item in res["controls"]:
                        bc = parse(item["border"]); bg = parse(item["bg"])
                        if not bc or not bg:
                            continue
                        ratio = contrast(over(bc, bg), bg)
                        if ratio + 0.005 < 3.0:
                            control_fails.append((round(ratio, 2), theme, where, item["what"]))
                    for item in res["names"]:
                        name_fails.append((theme, where, item["visible"], item["name"]))
        browser.close()

    print(f"text below AA:      {len(text_fails)}")
    for f in text_fails[:15]:
        print(f"   {f[0]}:1 (need {f[1]}) {f[2]} {f[3]} {f[4]!r}")
        print(f"        fg={f[5]} on bg={f[6]}")
    print(f"control edge < 3:1: {len(control_fails)}")
    seen = set()
    for f in control_fails:
        key = (f[0], f[3])
        if key in seen:
            continue
        seen.add(key)
        print(f"   {f[0]}:1 {f[1]} {f[2]} {f[3]!r}")
    print(f"label-in-name:      {len(name_fails)}")
    seen = set()
    for f in name_fails:
        key = (f[2], f[3])
        if key in seen:
            continue
        seen.add(key)
        print(f"   {f[0]} {f[1]} visible={f[2]!r} name={f[3]!r}")

    return 1 if (text_fails or control_fails or name_fails) else 0


if __name__ == "__main__":
    raise SystemExit(main())
