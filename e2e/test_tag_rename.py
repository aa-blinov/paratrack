"""Tag rename: fix a typo in place and keep the sessions that carry the tag.

Drives a running paratrack server with Playwright. Signs in once, creates two
throwaway tags, renames one of them through the /tags UI, checks the duplicate
name is refused instead of merging the two tags, and confirms the stats filter
follows the new name. Everything it creates is removed again at the end.

Run from the repo root with the .venv active:

    PARATRACK_BASE=http://127.0.0.1:8899 .venv/bin/python e2e/test_tag_rename.py
"""

from __future__ import annotations

import json
import os
import re
import uuid
from pathlib import Path

from playwright.sync_api import expect, sync_playwright

from target import BASE_URL as BASE

EMAIL = os.environ.get("PARATRACK_EMAIL", "doc-1791228444@x.test")
PASSWORD = os.environ.get("PARATRACK_PASSWORD", "longenoughpw")
SCREENSHOTS = Path(os.environ.get("PARATRACK_SCREENSHOTS", "/tmp"))
STAMP = uuid.uuid4().hex[:8]
SOURCE = f"e2e-src-{STAMP}"
TAKEN = f"e2e-taken-{STAMP}"
RENAMED = f"e2e-renamed-{STAMP}"
PERIOD = "month"

results: list[tuple[str, bool, str]] = []


def check(name: str, ok: bool, detail: str = "") -> None:
    print(f"  [{'PASS' if ok else 'FAIL'}] {name}{(' — ' + detail) if detail else ''}")
    results.append((name, ok, detail))


def sign_in(page) -> None:
    page.goto(BASE + "/login")
    page.fill("#email", EMAIL)
    page.fill("#password", PASSWORD)
    page.click('button[type="submit"]')
    page.wait_for_load_state("load")
    check("signed in", "/login" not in page.url, f"url={page.url}")


def csrf(page) -> str:
    for cookie in page.context.cookies():
        if cookie.get("name") == "paratrack_csrf":
            return cookie.get("value") or ""
    return ""


def api(page, method: str, path: str, **kw):
    headers = dict(kw.pop("headers", None) or {})
    headers.setdefault("X-CSRF-Token", csrf(page))
    return getattr(page.request, method)(BASE + path, headers=headers, **kw)


def page_data(page, path: str) -> dict:
    """The React bootstrap the server embeds in every screen."""
    html = api(page, "get", path).text()
    marker = '<div id="react-page-data" hidden>'
    start = html.index(marker) + len(marker)
    return json.loads(html[start:html.index("</div>", start)])["data"]


def tag_id(page, name: str) -> int | None:
    for tag in json.loads(api(page, "get", "/api/tags").text())["tags"]:
        if tag["name"] == name:
            return tag["id"]
    return None


def chip(page, name: str):
    return page.locator("ul > li").filter(has_text=f"#{name}")


def new_session(page) -> int:
    """A one-off tracked session, so the stats leg works on a fresh instance."""
    api(page, "post", "/api/start", form={"activity": f"e2e {STAMP}"})
    session_id = int(re.search(r"/api/sessions/(\d+)/stop", api(page, "get", "/api/active").text()).group(1))
    api(page, "post", f"/api/sessions/{session_id}/stop")
    api(page, "patch", f"/api/sessions/{session_id}", form={"duration": "10m"})
    return session_id


def main() -> int:
    with sync_playwright() as p:
        browser = p.chromium.launch()
        context = browser.new_context(viewport={"width": 1280, "height": 900})
        page = context.new_page()
        errors: list[str] = []
        page.on("pageerror", lambda e: errors.append(str(e)))
        session_id = None
        own_session = False

        # ------------------------------------------------------------------ 1
        print("\n== 1. Seed two throwaway tags and tag one session")
        sign_in(page)
        stats = page_data(page, f"/stats?period={PERIOD}")
        if stats.get("Sessions"):
            session_id = stats["Sessions"][0]["ID"]
        else:
            session_id, own_session = new_session(page), True
        for name in (SOURCE, TAKEN):
            api(page, "post", "/api/tags", form={"name": name})
        check("two throwaway tags created", tag_id(page, SOURCE) is not None and tag_id(page, TAKEN) is not None)
        api(page, "post", f"/api/sessions/{session_id}/tags", form={"name": SOURCE})
        tagged = [row for row in page_data(page, f"/stats?period={PERIOD}").get("Sessions", []) if row["ID"] == session_id]
        check("seeded session carries the tag", any(tag["Name"] == SOURCE for row in tagged for tag in row.get("Tags") or []))

        try:
            exercise(page, session_id)
        finally:
            # ---------------------------------------------------------------- 7
            print("\n== 7. Restore the workspace")
            # Both names are detached: a run that died before the rename
            # succeeded would otherwise leave the seed tag on the session.
            for name in (RENAMED, SOURCE):
                api(page, "delete", f"/api/sessions/{session_id}/tags?name={name}")
            for name in (RENAMED, TAKEN, SOURCE):
                identifier = tag_id(page, name)
                if identifier is not None:
                    api(page, "delete", f"/api/tags?id={identifier}")
            if own_session:
                api(page, "delete", f"/api/sessions/{session_id}")
            check("no leftover test tags", tag_id(page, RENAMED) is None and tag_id(page, TAKEN) is None and tag_id(page, SOURCE) is None)
            left = page_data(page, f"/stats?period={PERIOD}").get("Sessions", [])
            check("seeded session has no test tag left",
                  not any(tag["Name"].startswith("e2e-") for row in left for tag in row.get("Tags") or []))

        check("no page errors", not errors, "; ".join(errors[:3]))
        context.close()
        browser.close()

    failed = [name for name, ok, _ in results if not ok]
    print(f"\n{len(results) - len(failed)}/{len(results)} checks passed")
    return 1 if failed else 0


def exercise(page, session_id: int) -> None:
    """Everything that touches the workspace lives here so the run can be undone."""
    # ------------------------------------------------------------------ 2
    print("\n== 2. Open the rename editor on one tag")
    page.goto(BASE + "/tags")
    page.locator("#main h1").wait_for()
    expect(chip(page, SOURCE)).to_be_visible()
    button = chip(page, SOURCE).locator("[data-tag-rename]")
    button.click()
    form = page.locator("[data-tag-rename-form]")
    expect(form).to_be_visible()
    editor = form.locator("input")
    expect(editor).to_be_focused()
    check("editor opens focused on the current name", editor.input_value() == SOURCE, editor.input_value())
    check("error line is present and empty", form.locator("[role=alert]").inner_text() == "")
    check("only the edited chip grows", page.locator("[data-tag-rename-form]").count() == 1)
    page.screenshot(path=str(SCREENSHOTS / "tag-rename-open.png"), full_page=True)

    # ------------------------------------------------------------------ 3
    print("\n== 3. A name another tag already owns is refused")
    editor.fill(TAKEN)
    editor.press("Enter")
    expect(form.locator("[role=alert]")).not_to_have_text("")
    check("duplicate name explained inline", form.locator("[role=alert]").inner_text().strip() != "")
    check("editor keeps the typed name for another try", editor.input_value() == TAKEN)
    check("both tags survive the refused rename",
          chip(page, SOURCE).count() == 1 and chip(page, TAKEN).count() == 1)
    page.screenshot(path=str(SCREENSHOTS / "tag-rename-conflict.png"), full_page=True)

    # ------------------------------------------------------------------ 4
    print("\n== 4. Cancel returns focus to the tag's rename button")
    editor.press("Escape")
    expect(form).to_have_count(0)
    check("cancelling keeps focus on the rename button", button.evaluate("node => node === document.activeElement"))
    check("cancel leaves the name untouched", chip(page, SOURCE).count() == 1 and chip(page, RENAMED).count() == 0)

    # ------------------------------------------------------------------ 5
    print("\n== 5. Rename keeps the tag, its sessions and its count")
    button.click()
    editor = page.locator("[data-tag-rename-form] input")
    expect(editor).to_be_focused()
    editor.fill(RENAMED)
    editor.press("Enter")
    expect(chip(page, RENAMED)).to_be_visible()
    expect(page.locator("[data-tag-rename-form]")).to_have_count(0)
    check("old name is gone from the list", chip(page, SOURCE).count() == 0)
    check("the other tag is untouched", chip(page, TAKEN).count() == 1)
    check("session count follows the rename", chip(page, RENAMED).inner_text().split()[-1] == "1",
          chip(page, RENAMED).inner_text())
    page.set_viewport_size({"width": 390, "height": 844})
    chip(page, RENAMED).locator("[data-tag-rename]").click()
    expect(page.locator("[data-tag-rename-form]")).to_be_visible()
    check("the open editor fits a phone width", page.evaluate("document.documentElement.scrollWidth <= window.innerWidth"))
    page.screenshot(path=str(SCREENSHOTS / "tag-rename-mobile.png"), full_page=True)
    page.locator("[data-tag-rename-form] input").press("Escape")
    expect(page.locator("[data-tag-rename-form]")).to_have_count(0)
    page.set_viewport_size({"width": 1280, "height": 900})
    page.screenshot(path=str(SCREENSHOTS / "tag-rename-done.png"), full_page=True)

    # ------------------------------------------------------------------ 6
    print("\n== 6. Stats filters by the new name only")
    filtered = page_data(page, f"/stats?period={PERIOD}&tag={RENAMED}")
    check("stats accepts the new name", filtered["TagFilter"] == RENAMED, filtered["TagFilter"])
    check("new name is offered for the next tag", RENAMED in filtered["AllTagNames"] and SOURCE not in filtered["AllTagNames"])
    carried = [row for row in filtered.get("Sessions", []) if any(tag["Name"] == RENAMED for tag in row.get("Tags") or [])]
    check("tagged session is still listed under the new name", any(row["ID"] == session_id for row in carried))
    stale = page_data(page, f"/stats?period={PERIOD}&tag={SOURCE}")
    check("the old name filters nothing away silently", stale["TagFilter"] == SOURCE and not stale.get("Sessions"))


if __name__ == "__main__":
    raise SystemExit(main())