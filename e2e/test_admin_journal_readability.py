"""The journal names who acted and reads its targets as words.

Two checks on a fresh account, through the product's own forms and the audit page:

1. Each row names its author by the account's name, not by an id.
2. A created API token shows its name on the page, where the journal used to show an id.

Role changes and the former-member filter need a second account: test_admin_journal_members.py
covers them with one.
"""
import sys
import time

from playwright.sync_api import Page, sync_playwright

from browser import launch
from target import BASE_URL as BASE
from test_clients import register

failures: list[str] = []
OWNER = "Журнал Владелец"


def check(name: str, ok: bool, detail: str = "") -> None:
    print(f"  [{'PASS' if ok else 'FAIL'}] {name}" + (f": {detail}" if not ok and detail else ""))
    if not ok:
        failures.append(name)


def send(page: Page, path: str, fields: dict[str, str]) -> dict:
    """Send a form the way the app's own pages do, following redirects."""
    token = next(c["value"] for c in page.context.cookies() if c["name"] == "paratrack_csrf")
    return page.evaluate(
        """async ([path, fields, token]) => {
            const response = await fetch(path, {
                method: "POST", credentials: "same-origin",
                headers: { "X-CSRF-Token": token, "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8" },
                body: new URLSearchParams({ ...fields, csrf_token: token }),
            })
            return { status: response.status, url: response.url }
        }""",
        [path, fields, token],
    )


def main() -> int:
    stamp = int(time.time())
    token_name = f"readable-token-{stamp}"
    with sync_playwright() as p:
        browser = launch(p)
        context = browser.new_context(viewport={"width": 1280, "height": 900})
        page = context.new_page()
        register(page, context, OWNER, f"journal-readable{stamp}@example.com", "journal-readable-qa-pass-2026")
        send(page, "/api/team/modules", {"preset": "studio"})
        created = send(page, "/api/tokens", {"name": token_name, "read_only": "", "expires": ""})
        check("the token is created", created["status"] in (200, 204), str(created["status"]))

        print("\n== The journal page")
        page.goto(f"{BASE}/settings/audit", wait_until="networkidle")
        body = page.inner_text("body")
        check("the rows name the owner", OWNER in body)
        check("the created token shows its name", token_name in body)

        browser.close()

    print(f"\n{len(failures)} failure(s)" if failures else "\nall journal readability checks passed")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
