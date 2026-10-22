"""An invoice for a period with an unapproved week waits for the manager (FIN-12).

One account, through the product's own forms: billable time sits in a week
nobody approved. Pressing the invoice button must ask first and name the week.
Cancelling leaves no invoice behind; confirming issues it.
"""
import re
import sys
import time
from datetime import date

from playwright.sync_api import Page, expect, sync_playwright

from browser import launch
from target import BASE_URL as BASE
from test_clients import register

failures: list[str] = []


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


def page_data(page: Page) -> dict:
    return page.evaluate("""() => JSON.parse(document.getElementById("react-page-data").textContent).data""")


def main() -> int:
    stamp = int(time.time())
    project_name = f"Сайт {stamp}"
    first_day = date.today().replace(day=1).isoformat()
    with sync_playwright() as p:
        browser = launch(p)
        context = browser.new_context(viewport={"width": 1280, "height": 900})
        page = context.new_page()
        register(page, context, "Неделя на утверждение", f"invoice-week{stamp}@example.com", "invoice-week-qa-pass-2026")
        send(page, "/api/team/modules", {"preset": "studio"})
        send(page, "/projects/new", {"name": project_name, "rate": "100", "billable": "1"})

        page.goto(f"{BASE}/invoices", wait_until="networkidle")
        project_id = next((str(row["ID"]) for row in page_data(page)["Projects"] if row["Name"] == project_name), "")
        check("the billable project exists", project_id != "", project_name)

        print("\n== Billable time in a week nobody approved")
        booked = send(page, "/api/sessions/backfill", {
            "activity": "Разработка", "start": f"{first_day} 09:00", "end": f"{first_day} 10:00", "project_id": project_id,
        })
        check("the hour is booked", booked["status"] < 400, str(booked["status"]))

        def ask_for_invoice(client: str) -> None:
            page.goto(f"{BASE}/invoices", wait_until="networkidle")
            page.fill("#inv-client", client)
            page.locator('form[action="/invoices"] button[type="submit"]').click()

        print("\n== The invoice asks first and names the week")
        ask_for_invoice("Клиент с неделей")
        dialog = page.get_by_role("alertdialog")
        expect(dialog).to_be_visible(timeout=10000)
        check("the question names the unapproved weeks", "Есть неутверждённые недели" in dialog.inner_text(), dialog.inner_text()[:120])

        print("\n== Cancelling creates nothing")
        dialog.locator("button").first.click()
        page.wait_for_timeout(800)
        page.goto(f"{BASE}/invoices", wait_until="networkidle")
        check("cancelling leaves no invoice", len(page_data(page)["Items"]) == 0, str(len(page_data(page)["Items"])))

        print("\n== Confirming issues the invoice")
        ask_for_invoice("Клиент с неделей")
        expect(page.get_by_role("alertdialog")).to_be_visible(timeout=10000)
        page.get_by_role("alertdialog").locator("button").last.click()
        page.wait_for_url(re.compile(r"/invoices/\d+"), timeout=10000)
        check("the invoice opens after confirming", re.search(r"/invoices/\d+", page.url) is not None, page.url)

        browser.close()

    print(f"\n{len(failures)} failure(s)" if failures else "\nall unapproved-week checks passed")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
