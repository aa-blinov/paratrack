"""An invoice for a period with no unbilled time says why, on the invoices page.

One account, through the product's own form: the period has no tracked time, so the
refusal must reach the page as an error. The refusal used to vanish: its ";" cut the
flash out of the redirect address, and the page showed nothing.
"""
import sys
import time

from playwright.sync_api import Page, sync_playwright

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
        register(page, context, "Причина счёта", f"invoice-reason{stamp}@example.com", "invoice-reason-qa-pass-2026")
        send(page, "/api/team/modules", {"preset": "studio"})

        print("\n== The period has no unbilled time")
        created = send(page, "/invoices", {
            "client": "Клиент без часов", "start": "2020-01-06", "end": "2020-01-12", "notes": "", "project_id": "",
        })
        check("the form answers with the invoices page", "/invoices" in created["url"], created["url"])

        page.goto(created["url"], wait_until="networkidle")
        alert = page.get_by_role("alert").filter(has_text=REASON)
        check("the reason is shown as an error", alert.count() == 1, f"alerts: {page.get_by_role('alert').count()}")

        browser.close()

    print(f"\n{len(failures)} failure(s)" if failures else "\nall invoice reason checks passed")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
