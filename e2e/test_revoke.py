"""Revoke stops what it promises to stop, on a clean account.

A live invitation link opens until it is revoked, and then it is refused. An
API token authenticates until it is revoked, and then it is refused. Each
revoke goes through the confirmation dialog a person sees, so the test presses
the same confirm button rather than calling the endpoint directly.
"""
import sys
import time

from playwright.sync_api import Page, expect, sync_playwright

from browser import launch
from target import BASE_URL as BASE
from test_clients import register

failures: list[str] = []


def check(name: str, ok: bool, detail: str = "") -> None:
    print(f"  [{'PASS' if ok else 'FAIL'}] {name}" + (f" — {detail}" if not ok and detail else ""))
    if not ok:
        failures.append(name)


def confirm(page: Page) -> None:
    """Press the destructive button of the confirmation dialog, the last one in it."""
    dialog = page.get_by_role("alertdialog")
    expect(dialog).to_be_visible()
    dialog.locator("button").last.click()
    page.wait_for_load_state("networkidle")


def main() -> int:
    stamp = int(time.time())
    email = f"revoke{stamp}@example.com"
    guest = f"guest{stamp}@example.com"
    token_name = f"Расширение {stamp}"
    with sync_playwright() as p:
        browser = launch(p)
        context = browser.new_context(viewport={"width": 1280, "height": 900})
        page = context.new_page()
        register(page, context, "Тест отзыва", email, "revoke-qa-pass-2026")

        print("\n== 1. A live invitation link works until it is revoked")
        page.goto(f"{BASE}/settings/invites", wait_until="networkidle")
        page.fill("#invite-email", guest)
        page.locator('form[action="/api/team/invites"] button[type="submit"]').click()
        page.wait_for_load_state("networkidle")
        link = page.locator('article a[href^="/invites/"]').first
        check("a live invitation is listed", link.count() == 1)
        href = link.get_attribute("href") or ""
        opened = context.new_page()
        opened.goto(f"{BASE}{href}", wait_until="networkidle")
        check("the link opens before the revoke", "недействительна или отозвана" not in opened.inner_text("body"))
        opened.close()

        print("\n== 2. Revoking the invitation refuses the same link")
        page.goto(f"{BASE}/settings/invites", wait_until="networkidle")
        row = page.locator("article", has=page.locator(f'a[href="{href}"]'))
        row.get_by_role("button", name="Отозвать").click()
        confirm(page)
        check("the revoke is flashed", "Приглашение отозвано" in page.inner_text("body"))
        opened = context.new_page()
        opened.goto(f"{BASE}{href}", wait_until="networkidle")
        check("the revoked link is refused", "недействительна или отозвана" in opened.inner_text("body"))
        opened.close()

        print("\n== 3. An API token authenticates until it is revoked")
        page.goto(f"{BASE}/settings/tokens", wait_until="networkidle")
        page.fill("#token-name", token_name)
        page.locator('form[action="/api/tokens"] button[type="submit"]').click()
        page.wait_for_load_state("networkidle")
        token = page.locator("code").first.inner_text().strip()
        check("the new token is shown once", len(token) >= 20, token[:12])
        headers = {"Authorization": f"Bearer {token}"}
        before = page.request.get(f"{BASE}/api/me", headers=headers).status
        check("the token authenticates before the revoke", before == 200, str(before))

        print("\n== 4. Revoking the token refuses it")
        page.goto(f"{BASE}/settings/tokens", wait_until="networkidle")
        token_row = page.locator("div.rounded-md.border", has_text=token_name).filter(
            has=page.get_by_role("button", name="Отозвать"))
        check("the token is listed with its revoke button", token_row.count() == 1)
        token_row.get_by_role("button", name="Отозвать").click()
        confirm(page)
        check("the token revoke is flashed", "Токен отозван" in page.inner_text("body"))
        after = page.request.get(f"{BASE}/api/me", headers=headers).status
        check("the revoked token no longer authenticates", after == 401, str(after))

        browser.close()

    print(f"\n{len(failures)} failure(s)" if failures else "\nall revoke checks passed")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
