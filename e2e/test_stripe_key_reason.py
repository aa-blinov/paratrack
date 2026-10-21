"""A payment link with no Stripe key set says why, in Russian, on the invoice page.

The QA stand has no route to Stripe, so this covers the path that needs none: the key is
not set. A refused key is covered by the Go tests with a stub transport.
"""
import re
import sys
import time
from datetime import date, timedelta

from playwright.sync_api import Page, sync_playwright

from browser import launch
from target import BASE_URL as BASE
from test_clients import register

failures: list[str] = []
REASON = "Ключ Stripe не задан. Задайте его в разделе «Онлайн-оплата» настроек пространства."


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
    yesterday = date.today() - timedelta(days=1)
    project = f"Ключ {stamp}"
    with sync_playwright() as p:
        browser = launch(p)
        context = browser.new_context(viewport={"width": 1280, "height": 900})
        page = context.new_page()
        register(page, context, "Ключ Бухгалтер", f"stripe-key{stamp}@example.com", "stripe-key-qa-pass-2026")
        send(page, "/api/team/modules", {"preset": "studio"})

        print("\n== An invoice with billable time, then a payment link with no key")
        send(page, "/projects/new", {"name": project, "rate": "3000", "billable": "1"})
        page.goto(f"{BASE}/projects", wait_until="networkidle")
        project_id = next(row["ID"] for row in page_data(page)["Projects"] if row["Name"] == project)
        send(page, "/api/sessions/backfill", {"activity": f"Ключ {stamp}", "start": f"{yesterday.isoformat()}T09:00", "end": f"{yesterday.isoformat()}T10:00", "note": "", "project_id": str(project_id)})
        created = send(page, "/invoices", {"project_id": str(project_id), "client": "Клиент Stripe", "start": yesterday.isoformat(), "end": yesterday.isoformat(), "notes": ""})
        invoice = re.search(r"/invoices/(\d+)", created["url"])
        check("the invoice is created", invoice is not None, created["url"])
        invoice_id = invoice.group(1) if invoice else "0"

        paid = send(page, f"/invoices/{invoice_id}/pay", {"mode": "stripe"})
        check("the payment link answers with the invoice page", f"/invoices/{invoice_id}" in paid["url"], paid["url"])
        page.goto(paid["url"], wait_until="networkidle")
        alert = page.get_by_role("alert").filter(has_text=REASON)
        check("the reason is shown as an error", alert.count() == 1, f"alerts: {page.get_by_role('alert').count()}")
        check("the English sentence is gone", "stripe key is not configured" not in page.inner_text("body"))

        browser.close()

    print(f"\n{len(failures)} failure(s)" if failures else "\nall Stripe key reason checks passed")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
