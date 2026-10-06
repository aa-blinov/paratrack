"""Invitation link actions on /settings/invites.

Covers the two things the screen used to make impossible: copying a freshly
created invite link, and clearing a spent invitation out of the list without
taking the access it granted back.

Drives a running paratrack server with Playwright. The base URL comes from
target.py and can be overridden with PARATRACK_BASE. Signs in twice (the login
rate limit is ten attempts a minute) and cleans up after itself: the account it
registers for the "somebody joined" step loses its membership again, and every
invitation it creates is removed.

    PARATRACK_BASE=http://127.0.0.1:8899 .venv/bin/python e2e/test_invite_link_actions.py
"""

from __future__ import annotations

import os
import sys
import time

from playwright.sync_api import expect, sync_playwright

from target import BASE_URL as BASE

OWNER_EMAIL = os.environ.get("PARATRACK_OWNER_EMAIL", "doc-1791228444@x.test")
OWNER_PASSWORD = os.environ.get("PARATRACK_OWNER_PASSWORD", "longenoughpw")
INVITEE_EMAIL = f"doc-invitee-{int(time.time())}@x.test"

ROOT = "#paratrack-react-root"
STATUS = f'{ROOT} [role="status"]'
ALERT = f'{ROOT} [role="alert"]'
results: list[tuple[str, bool, str]] = []


def check(name: str, ok: bool, detail: str = "") -> None:
    results.append((name, ok, detail))
    print(f"{'PASS' if ok else 'FAIL'}  {name}{'' if ok else ' — ' + detail}")


def sign_in(page, email: str, password: str) -> None:
    page.goto(BASE + "/login")
    page.fill("#email", email)
    page.fill("#password", password)
    page.click('button[type="submit"]')
    page.wait_for_load_state("load")
    if "/login" in page.url:
        raise SystemExit(f"sign-in failed for {email}: still on {page.url}")


def register(page, name: str, email: str, password: str) -> None:
    page.goto(BASE + "/register")
    page.fill("#name", name)
    page.fill("#email", email)
    page.fill("#password", password)
    page.click('button[type="submit"]')
    page.wait_for_load_state("load")
    if page.url.endswith("/welcome"):
        page.locator('form[action="/api/team/modules"]:has(input[name="preset"][value="solo"]) button').click()
        page.wait_for_load_state("load")


def open_invites(page) -> None:
    page.goto(BASE + "/settings/invites")
    page.locator(f"{ROOT} main").wait_for()


def invite_rows(page):
    return page.locator(f"{ROOT} article")


def row_for_token(page, token: str):
    """Find the one row that belongs to this token.

    A spent invitation renders as plain text instead of a link, so the row is
    matched on the token prefix the list shows rather than on a link.
    """
    return invite_rows(page).filter(has_text=token[:12])


def create_invite(page) -> str:
    page.get_by_role("button", name="Создать ссылку-приглашение").click()
    page.wait_for_load_state("load")
    link = page.locator(f"{ROOT} a[href^='http']")
    expect(link).to_have_count(1)
    return link.get_attribute("href") or ""


def answer_dialog(page, accept: bool) -> None:
    # Radix renders the confirmation as an alertdialog.
    dialog = page.get_by_role("alertdialog")
    expect(dialog).to_be_visible()
    dialog.get_by_role("button", name="Подтвердить" if accept else "Отмена").click()
    page.wait_for_load_state("load")


def remove_row(page, row) -> None:
    row.locator("form[action$='/revoke'] button").click()
    answer_dialog(page, accept=False)
    expect(row).to_be_visible()
    row.locator("form[action$='/revoke'] button").click()
    answer_dialog(page, accept=True)


def run(browser) -> None:
    context = browser.new_context()
    context.grant_permissions(["clipboard-read", "clipboard-write"], origin=BASE)
    owner = context.new_page()
    errors: list[str] = []
    owner.on("pageerror", lambda error: errors.append(str(error)))

    sign_in(owner, OWNER_EMAIL, OWNER_PASSWORD)
    open_invites(owner)
    rows_before = invite_rows(owner).count()
    used_before = invite_rows(owner).filter(has_text="использовано").count()

    link = create_invite(owner)
    token = link.rsplit("/", 1)[-1]
    check("the fresh invite link is a link, not a sentence", link.startswith(BASE + "/invites/"), link)
    check(
        "the link sits under the flash message that explains it",
        "Ссылка-приглашение создана" in owner.locator(STATUS).first.inner_text(),
        owner.locator(STATUS).first.inner_text(),
    )
    copy = owner.get_by_role("button", name="Копировать")
    check("the fresh link offers a copy button", copy.is_visible())
    copy.click()
    expect(owner.get_by_role("button", name="Скопировано")).to_be_visible()
    clipboard = owner.evaluate("navigator.clipboard.readText()")
    check("copying puts the whole link on the clipboard", clipboard == link, f"clipboard={clipboard!r} link={link!r}")
    check("no page errors on the invitations screen", not errors, str(errors))

    # A browser that refuses clipboard access must still leave a way out: the
    # link gets selected so it can be copied by hand.
    blocked_context = browser.new_context()
    blocked_context.add_init_script(
        "Object.defineProperty(navigator, 'clipboard',"
        "{value: {writeText: () => Promise.reject(new Error('denied'))}, configurable: true})"
    )
    blocked = blocked_context.new_page()
    sign_in(blocked, OWNER_EMAIL, OWNER_PASSWORD)
    open_invites(blocked)
    refused_link = create_invite(blocked)
    blocked.get_by_role("button", name="Копировать").click()
    expect(blocked.locator(ALERT)).to_be_visible()
    selected = blocked.evaluate("window.getSelection().toString()").strip()
    check(
        "a refused clipboard leaves the link selected for a manual copy",
        selected == refused_link,
        f"selection={selected!r} link={refused_link!r}",
    )
    blocked_context.close()

    # Somebody joins, so that invitation becomes spent: it can no longer admit
    # anybody, but the membership it created is the owner's to keep.
    invitee_context = browser.new_context()
    invitee = invitee_context.new_page()
    register(invitee, "Приглашённый", INVITEE_EMAIL, "longenoughpw")
    invitee.goto(refused_link)
    invitee.locator("form[action$='/accept'] button").click()
    invitee.wait_for_load_state("load")
    check("the invitee joined through the link", "/invites/" not in invitee.url, invitee.url)

    open_invites(owner)
    refused_token = refused_link.rsplit("/", 1)[-1]
    spent = row_for_token(owner, refused_token)
    expect(spent).to_have_count(1)
    check("the spent invitation is listed as used", "использовано" in spent.inner_text(), spent.inner_text())
    check(
        "a spent invitation can be removed from the list",
        spent.locator("form[action$='/revoke'] button").is_visible(),
    )
    remove_row(owner, spent)
    check(
        "the removed invitation is gone and the rest of the list stayed",
        invite_rows(owner).filter(has_text=refused_token[:12]).count() == 0
        and invite_rows(owner).filter(has_text="использовано").count() == used_before,
        f"used rows left={invite_rows(owner).filter(has_text='использовано').count()} was={used_before}",
    )
    check("no page errors after removal", not errors, str(errors))

    owner.goto(BASE + "/settings/members")
    expect(owner.locator(ROOT)).to_contain_text(INVITEE_EMAIL)
    check("the invitee keeps workspace access after the invitation is removed", True)

    # Cleanup: drop the throwaway membership, then take back every invitation
    # this run created so the workspace looks the way it did before.
    invite_rows(owner).filter(has_text=INVITEE_EMAIL).locator("form[action$='/remove'] button").click()
    answer_dialog(owner, accept=True)
    expect(owner.locator(ROOT)).not_to_contain_text(INVITEE_EMAIL)

    open_invites(owner)
    for _ in range(4):
        if row_for_token(owner, token).count() == 0:
            break
        remove_row(owner, row_for_token(owner, token))
    open_invites(owner)
    check(
        "cleanup restored the invitation list",
        invite_rows(owner).count() == rows_before,
        f"rows before={rows_before} after={invite_rows(owner).count()}",
    )
    invitee_context.close()
    context.close()


def main() -> int:
    with sync_playwright() as playwright:
        browser = playwright.chromium.launch()
        try:
            run(browser)
        finally:
            browser.close()
    failed = [name for name, ok, _ in results if not ok]
    print(f"\n{len(results) - len(failed)}/{len(results)} checks passed")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())