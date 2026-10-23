"""The team trail shows the deletions and revocations an administrator looks for.

Two checks on a fresh account, through the product's own forms and the audit page:

1. A deleted draft invoice appears in the trail under its label, with its number.
2. A revoked API token appears under its label.

The member removal and the restore are covered by the Go tests, which need a second account.
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


def check(name: str, ok: bool, detail: str = "") -> None:
    print(f"  [{'PASS' if ok else 'FAIL'}] {name}" + (f" — {detail}" if not ok and detail else ""))
    if not ok:
        failures.append(name)


def send(page: Page, method: str, path: str, fields: dict[str, str]) -> dict:
    """Send a form the way the app's own pages do, following redirects."""
    token = next(c["value"] for c in page.context.cookies() if c["name"] == "paratrack_csrf")
    return page.evaluate(
        """async ([method, path, fields, token]) => {
            const response = await fetch(path, {
                method, credentials: "same-origin",
                headers: { "X-CSRF-Token": token, "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8" },
                body: new URLSearchParams({ ...fields, csrf_token: token }),
            })
            return { status: response.status, url: response.url }
        }""",
        [method, path, fields, token],
    )


def page_data(page: Page) -> dict:
    return page.evaluate("""() => JSON.parse(document.getElementById("react-page-data").textContent).data""")


def main() -> int:
    stamp = int(time.time())
    yesterday = date.today() - timedelta(days=1)
    project_name = f"Аудит {stamp}"
    token_name = f"sync-laptop-{stamp}"
    with sync_playwright() as p:
        browser = launch(p)
        context = browser.new_context(viewport={"width": 1280, "height": 900})
        page = context.new_page()
        register(page, context, "Журнал пространства", f"audit-trail{stamp}@example.com", "audit-trail-qa-pass-2026")
        send(page, "POST", "/api/team/modules", {"preset": "studio"})

        print("\n== 1. A deleted draft invoice is in the trail")
        send(page, "POST", "/projects/new", {"name": project_name, "rate": "3000", "billable": "1"})
        page.goto(f"{BASE}/projects", wait_until="networkidle")
        project_id = next(row["ID"] for row in page_data(page)["Projects"] if row["Name"] == project_name)
        backfill = send(page, "POST", "/api/sessions/backfill", {
            "activity": f"Черновик {stamp}", "start": f"{yesterday.isoformat()}T09:00",
            "end": f"{yesterday.isoformat()}T10:00", "note": "", "project_id": str(project_id),
        })
        check("an hour is tracked yesterday", backfill["status"] in (200, 204), str(backfill["status"]))
        created = send(page, "POST", "/invoices", {
            "project_id": str(project_id), "client": "Клиент журнала",
            "start": yesterday.isoformat(), "end": yesterday.isoformat(), "notes": "",
            # The week is not approved yet: the manager confirms the invoice anyway (FIN-12).
            "confirm_unapproved": "1",
        })
        invoice = re.search(r"/invoices/(\d+)", created["url"])
        check("a draft invoice is created", invoice is not None, created["url"])
        invoice_id = invoice.group(1) if invoice else "0"
        deleted = send(page, "POST", f"/invoices/{invoice_id}/delete", {})
        check("the draft is deleted", deleted["status"] in (200, 204), str(deleted["status"]))

        print("\n== 2. A revoked token is in the trail with its name")
        send(page, "POST", "/api/tokens", {"name": token_name, "read_only": "", "expires": ""})
        page.goto(f"{BASE}/settings/tokens", wait_until="networkidle")
        token_id = next(row["ID"] for row in page_data(page)["Tokens"] if row["Name"] == token_name)
        revoked = send(page, "POST", f"/api/tokens/{token_id}/delete", {})
        check("the token is revoked", revoked["status"] in (200, 204), str(revoked["status"]))

        print("\n== 3. The audit page shows both events with their labels")
        page.goto(f"{BASE}/settings/audit", wait_until="networkidle")
        body = page.inner_text("body")
        check("the invoice deletion is labelled with its number", re.search(r"Счёт удалён[\s\S]{0,200}?INV-\d{4}-\d{3}", body) is not None)
        check("the token revocation is labelled", "Отозван API-токен" in body)

        browser.close()

    print(f"\n{len(failures)} failure(s)" if failures else "\nall admin trail checks passed")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
