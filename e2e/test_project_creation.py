"""Project creation through the form, on a clean account.

test_dashboard.py covers the project journey too, but it opens the mobile
sheet and changes the viewport first, and the shell keeps a Radix modal
state around afterwards: the optional fields cannot take focus and anything
typed lands in the autofocused name box. This suite starts from a fresh
account and never touches the sheet, so the form is exercised the way a
person would.
"""
import sys
import time
from pathlib import Path

from playwright.sync_api import Page, expect, sync_playwright

from target import BASE_URL as BASE

SHOTS = Path(__file__).resolve().parents[1] / "e2e" / "screenshots"
failures: list[str] = []


def check(name: str, ok: bool, detail: str = "") -> None:
    print(f"  [{'PASS' if ok else 'FAIL'}] {name}" + (f" — {detail}" if not ok and detail else ""))
    if not ok:
        failures.append(name)


def register(page: Page, name: str, email: str, password: str) -> None:
    page.goto(f"{BASE}/register", wait_until="networkidle")
    page.fill('input[name="name"]', name)
    page.fill('input[name="email"]', email)
    page.fill('input[name="password"]', password)
    page.locator('button[type="submit"]').first.click()
    page.wait_for_load_state("networkidle")
    if "/welcome" in page.url:
        # On /welcome the skip control is a link, not a button.
        skip = page.get_by_role("link", name="Пропустить, оставить всё")
        expect(skip).to_be_visible(timeout=5000)
        skip.click()
        page.wait_for_url(lambda url: "/welcome" not in url, timeout=10000)


def open_disclosures(scope, target: str) -> None:
    """Open the disclosure that holds `target`, if it is closed.

    Optional fields live behind Radix disclosures that start closed when the
    account has nothing to suggest. `scope` may be a locator or the page.
    Click through the DOM: Playwright's retrying click can land on a control
    that slid into the trigger's old spot — the currency select, for one —
    and that steals focus from the fields below it.
    """
    holder = scope.page if hasattr(scope, "page") else scope
    for _ in range(4):
        root = holder.locator(f'[data-disclosure]:has({target})')
        if not root.count():
            return
        if root.first.get_attribute("data-state") == "open":
            return
        trigger = root.first.locator("> button[aria-expanded]").first
        if not trigger.count():
            return
        trigger.evaluate("node => node.click()")
        holder.wait_for_timeout(300)
def main() -> int:
    stamp = int(time.time())
    email = f"proj-{stamp}@x.test"
    password = "longenoughpw"
    proj_slug = f"novyy-proekt-{stamp}"
    proj_name = f"Новый проект {stamp}"

    with sync_playwright() as p:
        browser = p.chromium.launch()
        context = browser.new_context(viewport={"width": 1280, "height": 900}, locale="ru-RU")
        page = context.new_page()
        errors: list[str] = []
        page.on("pageerror", lambda exc: errors.append(str(exc)))

        # ---------------------------------------------------------------- 1
        print("\n== 1. A fresh account")
        register(page, "Создатель", email, password)
        check("registered and past onboarding", "/welcome" not in page.url, page.url)

        # ---------------------------------------------------------------- 2
        print("\n== 2. The create form keeps each field's own value")
        page.goto(f"{BASE}/projects/new", wait_until="networkidle")
        expect(page.locator("#new-project-name")).to_be_visible()
        check("form mounts the React shell",
              page.locator('#paratrack-react-root form[action="/projects/new"] [data-slot="input"]').count() >= 3)

        page.fill("#new-project-name", proj_name)
        open_disclosures(page.locator("#new-project-name").locator("xpath=ancestor::form"), "#new-project-slug")
        check("optional fields are reachable once options are open",
              page.locator("#new-project-slug").is_visible())

        page.fill("#new-project-slug", proj_slug)
        page.fill('input[name="color"][pattern]', "#7c3aed")
        page.fill("#new-project-rate", "120")
        check("name, slug, colour and rate each kept their own value",
              page.locator("#new-project-name").input_value() == proj_name
              and page.locator("#new-project-slug").input_value() == proj_slug
              and page.locator('input[name="color"][pattern]').input_value() == "#7c3aed"
              and page.locator("#new-project-rate").input_value() == "120",
              f"name={page.locator('#new-project-name').input_value()!r}"
              f" slug={page.locator('#new-project-slug').input_value()!r}"
              f" rate={page.locator('#new-project-rate').input_value()!r}")

        # ---------------------------------------------------------------- 3
        print("\n== 3. The slug reaches the address bar unchanged")
        page.get_by_role("button", name="Создать").click()
        page.wait_for_url(f"**/projects/{proj_slug}")
        check(f"landed on /projects/{proj_slug}", page.url.endswith(f"/projects/{proj_slug}"), page.url)
        expect(page.locator("h1")).to_contain_text(proj_name)
        check("detail page shows the project", proj_name in page.content())
        page.screenshot(path=str(SHOTS / "project-creation.png"), full_page=False)

        # ---------------------------------------------------------------- 4
        print("\n== 4. Rate and estimate are editable on the detail page")
        open_disclosures(page.locator("h1").locator("xpath=ancestor::main"), "#project-name")
        page.fill("#project-name", f"{proj_name} — переименован")
        page.fill("#project-estimate", "480")
        page.locator('#paratrack-react-root form[method="POST"][action^="/projects/"] button[type="submit"]').first.click()
        page.wait_for_load_state("networkidle")
        check("rename saved", f"{proj_name} — переименован" in page.content())
        check("estimate saved", page.locator("#project-estimate").input_value() == "480",
              page.locator("#project-estimate").input_value())

        # ---------------------------------------------------------------- 5
        print("\n== 5. The project is offered to the dashboard timer")
        page.goto(f"{BASE}/", wait_until="networkidle")
        # The timer picks a project through a Radix select, so open it and read
        # the items rather than the trigger.
        open_disclosures(page.locator("#activity").locator("xpath=ancestor::form"), "#project_id")
        picker = page.locator("#project_id")
        expect(picker).to_be_visible(timeout=5000)
        picker.click()
        items = page.locator('[role="option"]')
        expect(items.first).to_be_visible(timeout=5000)
        names = [items.nth(i).inner_text().strip() for i in range(items.count())]
        check("dashboard offers the new project in the timer picker",
              any(proj_name in name for name in names), " | ".join(names))

        check("no page errors", not errors, "; ".join(errors[:3]))
        context.close()
        browser.close()

    print(f"\n{len(failures)} failed" if failures else "\nall checks passed")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())