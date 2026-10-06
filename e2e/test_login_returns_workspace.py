"""Sign-in returns to the workspace the person was last working in.

Drives a running paratrack server with Playwright. Signs in, moves into
the second workspace, signs out, and signs back in twice: once in the
same browser and once in a brand-new one that has never held a cookie.
Both must open the studio, not the personal space — that is where the
tracked time has to land. Ends by putting the account back on the
workspace it started on.

Run from the repo root with the .venv active:

    PARATRACK_BASE=http://127.0.0.1:8899 .venv/bin/python e2e/test_login_returns_workspace.py
"""

from __future__ import annotations

import json
import os
import uuid
from pathlib import Path

from playwright.sync_api import expect, sync_playwright

from target import BASE_URL as BASE

EMAIL = os.environ.get("PARATRACK_EMAIL", "member2@x.test")
PASSWORD = os.environ.get("PARATRACK_PASSWORD", "longenoughpw")
SCREENSHOTS = Path(os.environ.get("PARATRACK_SCREENSHOTS", "/tmp"))
STAMP = uuid.uuid4().hex[:8]

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
    expect(page.locator("#main h1").first).to_be_visible()


def csrf(page) -> str:
    for cookie in page.context.cookies():
        if cookie.get("name") == "paratrack_csrf":
            return cookie.get("value") or ""
    return ""


def api(page, method: str, path: str, **kw):
    headers = dict(kw.pop("headers", None) or {})
    headers.setdefault("X-CSRF-Token", csrf(page))
    return getattr(page.request, method)(BASE + path, headers=headers, **kw)


def me(page) -> dict:
    """Who we are and, more to the point, which workspace we are in."""
    response = api(page, "get", "/api/me")
    assert response.status == 200, f"/api/me said {response.status}"
    return response.json()


def user_teams(page) -> list[tuple[int, str]]:
    """The workspaces the sidebar offers."""
    html = api(page, "get", "/").text()
    marker = '<div id="react-page-data" hidden>'
    start = html.index(marker) + len(marker)
    bootstrap = json.loads(html[start:html.index("</div>", start)])
    return [(team["id"], team["name"]) for team in bootstrap["shell"]["userTeams"]]


def sidebar_workspace(page, name: str) -> str:
    """What the switcher in the sidebar shows for the open workspace.

    Both shells label that button with the workspace name in its tooltip
    and in the name itself, so the tooltip suffix finds it in either.
    """
    return page.locator(f'button[title$="{name}"]').first.inner_text().strip()


def switch_workspace(page, current: str, team_id: int) -> None:
    """The switcher in the sidebar, used the way a person uses it."""
    page.goto(BASE + "/")
    expect(page.locator("#main h1").first).to_be_visible()
    page.locator(f'button[title$="{current}"]').first.click()
    page.locator(
        f'form[action="/api/team/switch"]:has(input[name="team_id"][value="{team_id}"]) button[type=submit]'
    ).click()
    page.wait_for_load_state("load")
    page.goto(BASE + "/")
    expect(page.locator("#main h1").first).to_be_visible()


def sign_out(page) -> None:
    page.goto(BASE + "/")
    page.get_by_role("button", name=me(page)["user"]["name"], exact=True).first.click()
    # The account menu and the phone sheet each carry a sign-out form;
    # the menu item is the one open on screen at this width.
    page.locator('form[action="/api/logout"] [role="menuitem"]').click()
    page.wait_for_load_state("load")
    expect(page.locator("#email")).to_be_visible()


def team_cookie(page) -> str:
    for cookie in page.context.cookies():
        if cookie.get("name") == "paratrack_team":
            return cookie.get("value") or ""
    return ""


def main() -> int:
    with sync_playwright() as p:
        browser = p.chromium.launch()
        context = browser.new_context(viewport={"width": 1280, "height": 900})
        page = context.new_page()
        errors: list[str] = []
        page.on("pageerror", lambda e: errors.append(str(e)))

        try:
            # ------------------------------------------------------------------ 1
            print("\n== 1. Sign in and note where a person lands first")
            sign_in(page)
            start_id, start_name = me(page)["team"]["id"], me(page)["team"]["name"]
            teams = [team for team in user_teams(page) if team[0] != start_id]
            check("signed in with more than one workspace", bool(teams),
                  f"landed in {start_name!r}, also in {[n for _, n in teams]}")
            if not teams:
                raise SystemExit(f"this account has no second workspace to move into ({user_teams(page)})")
            other_id, other_name = teams[0]
            page.screenshot(path=str(SCREENSHOTS / f"login-workspace-{STAMP}-1.png"), full_page=True)

            # ------------------------------------------------------------------ 2
            print(f"\n== 2. Move into {other_name!r} and stay there")
            switch_workspace(page, start_name, other_id)
            check("the sidebar shows the workspace we moved into", sidebar_workspace(page, other_name) == other_name,
                  sidebar_workspace(page, other_name))
            check("the server scoped the session to it", me(page)["team"]["id"] == other_id,
                  f"{me(page)['team']['name']!r}")
            page.screenshot(path=str(SCREENSHOTS / f"login-workspace-{STAMP}-2.png"), full_page=True)

            # ------------------------------------------------------------------ 3
            print("\n== 3. Signing out leaves no workspace pointer in the browser")
            sign_out(page)
            check("signed out", "/login" in page.url, page.url)
            check("the workspace cookie is cleared on logout", team_cookie(page) == "", f"cookie={team_cookie(page)!r}")

            # ------------------------------------------------------------------ 4
            print("\n== 4. Sign in again in the same browser")
            sign_in(page)
            check("back in the same workspace without switching anything", me(page)["team"]["id"] == other_id,
                  f"{me(page)['team']['name']!r}")
            check("the sidebar agrees", sidebar_workspace(page, other_name) == other_name,
                  sidebar_workspace(page, other_name))
            page.screenshot(path=str(SCREENSHOTS / f"login-workspace-{STAMP}-3.png"), full_page=True)

            # ------------------------------------------------------------------ 5
            print("\n== 5. Sign in on a device that never held a cookie")
            fresh = browser.new_context(viewport={"width": 1280, "height": 900})
            other = fresh.new_page()
            check("the new device starts with no workspace cookie", team_cookie(other) == "")
            sign_in(other)
            check("the new device opens the same workspace too", me(other)["team"]["id"] == other_id,
                  f"{me(other)['team']['name']!r}")
            other.screenshot(path=str(SCREENSHOTS / f"login-workspace-{STAMP}-4.png"), full_page=True)
            fresh.close()

            check("no page errors", not errors, "; ".join(errors[:3]))
        finally:
            # ------------------------------------------------------------------ 6
            print("\n== 6. Put the account back on the workspace it started on")
            try:
                switch_workspace(page, me(page)["team"]["name"], start_id)
                check("workspace restored", me(page)["team"]["id"] == start_id, f"{me(page)['team']['name']!r}")
            except Exception as exc:  # a broken run must still report what it saw
                check("workspace restored", False, str(exc)[:160])
            context.close()
            browser.close()

    failed = [name for name, ok, _ in results if not ok]
    print(f"\n{len(results) - len(failed)}/{len(results)} checks passed")
    return 1 if failed else 0


if __name__ == "__main__":
    raise SystemExit(main())