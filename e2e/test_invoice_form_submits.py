"""The invoice form still posts when no week is unapproved (FIN-12).

One account, through the form's own submit button: the period has no billable
time, so there are no unapproved weeks and no question must come up. The form
must reach the server, and the server's own answer (no billable time in that
period) must reach the page. A submit handler that swallowed the press would
leave the page as it is with no answer at all.
"""
import sys
import time

from playwright.sync_api import Page, expect, sync_playwright

from browser import launch
from target import BASE_URL as BASE
from test_clients import register

failures: list[str] = []
REASON = "За этот период нечего выставлять. В счёт попадают завершённые сессии оплачиваемых проектов со ставкой, которых ещё нет в других счетах; идущий таймер сначала остановите."


def check(name: str, ok: bool, detail: str = "") -> None:
    print(f"  [{'PASS' if ok else 'FAIL'}] {name}" + (f": {detail}" if not ok and detail else ""))
    if not ok:
        failures.append(name)


def send(page: Page, path: str, fields: dict[str, str]) -> dict:
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
    with sync_playwright() as p:
        browser = launch(p)
        context = browser.new_context(viewport={"width": 1280, "height": 900})
        page = context.new_page()
        register(page, context, "Форма счёта", f"invoice-form{stamp}@example.com", "invoice-form-qa-pass-2026")
        send(page, "/api/team/modules", {"preset": "studio"})

        print("\n== No unapproved week: the question stays away and the form posts")
        page.goto(f"{BASE}/invoices", wait_until="networkidle")
        page.fill("#inv-client", "Клиент без часов")
        page.locator('form[action="/invoices"] button[type="submit"]').click()
        alert = page.get_by_role("alert").filter(has_text=REASON)
        expect(alert).to_have_count(1, timeout=10000)
        check("the server's reason reaches the page", alert.count() == 1)
        check("no question is asked", page.get_by_role("alertdialog").count() == 0)

        browser.close()

    print(f"\n{len(failures)} failure(s)" if failures else "\nall invoice form checks passed")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
