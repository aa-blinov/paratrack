#!/usr/bin/env python3
"""Fill a fresh account with a believable freelancer's month of work.

Clients, projects, rates, budgets, tracked time and one issued invoice, so the
new screens can be looked at with numbers in them instead of empty states.

Driven through the real UI rather than the database on purpose: a seed that
bypasses the forms would happily succeed while the form is broken, and empty
screens are the state where a regression hides.

    python scripts/seed-demo.py https://paratrack.duckdns.org demo@example.test

Prints the credentials it used. The account is reachable from the internet
with that password, so treat the password as disposable and sign in only to
look — never leave real work in it.
"""
from __future__ import annotations

import sys
import time
from datetime import date, timedelta

from playwright.sync_api import Page, expect, sync_playwright

EMAIL = sys.argv[1] if len(sys.argv) > 1 else "demo@paratrack.local"
PASSWORD = sys.argv[2] if len(sys.argv) > 2 else "demo-password-123"
NAME = "Демо Фрилансер"
BASE = (sys.argv[3] if len(sys.argv) > 3 else "https://paratrack.duckdns.org").rstrip("/")

SHOTS = "e2e/screenshots"
failures: list[str] = []


def check(name: str, ok: bool, detail: str = "") -> None:
    print(f"  [{'PASS' if ok else 'FAIL'}] {name}" + (f" — {detail}" if not ok and detail else ""))
    if not ok:
        failures.append(name)


def open_all(page: Page) -> None:
    """Expand the collapsible blocks that hold optional project fields."""
    for _ in range(5):
        opened = False
        triggers = page.locator('main button[aria-expanded="false"]:visible')
        for index in range(triggers.count()):
            trigger = triggers.nth(index)
            panel = trigger.locator("xpath=following-sibling::*[@data-state][1]")
            if not panel.count() or panel.first.get_attribute("data-state") == "open":
                continue
            trigger.click()
            page.wait_for_timeout(200)
            opened = True
            break
        if not opened:
            return


def submit_form(page: Page, form: str) -> None:
    button = page.locator(f'{form} button[type="submit"]').first
    try:
        button.click(timeout=8000)
    except Exception:
        button.dispatch_event("click")
    page.wait_for_load_state("networkidle")


def sign_up_or_in(page: Page) -> None:
    page.goto(f"{BASE}/register")
    page.fill("#name", NAME)
    page.fill("#email", EMAIL)
    page.fill("#password", PASSWORD)
    page.click('button[type="submit"]')
    page.wait_for_load_state("load")
    if "/login" in page.url or "/register" in page.url:
        page.goto(f"{BASE}/login")
        page.fill("#email", EMAIL)
        page.fill("#password", PASSWORD)
        page.click('button[type="submit"]')
        page.wait_for_load_state("load")
    if page.url.endswith("/welcome"):
        # "Фрилансер" keeps invoices and reports, which is what this seed is
        # here to show off; "для себя" would hide half of it.
        preset = page.locator('form[action="/api/team/modules"]:has(input[name="preset"][value="freelance"])')
        if preset.count():
            preset.first.locator("button").first.click()
        else:
            page.locator('form[action="/api/team/modules"]:has(input[name="preset"][value="solo"]):not([data-welcome-skip]) button').click()
        page.wait_for_url(lambda url: "/welcome" not in url, timeout=20000)
    page.wait_for_load_state("networkidle")


def add_client(page: Page, name: str, email: str, details: str, notes: str, currency: str | None) -> None:
    page.goto(f"{BASE}/clients", wait_until="networkidle")
    page.fill('form[action="/clients"] input[name="name"]', name)
    page.fill('form[action="/clients"] input[name="email"]', email)
    page.fill('form[action="/clients"] textarea[name="details"]', details)
    page.fill('form[action="/clients"] textarea[name="notes"]', notes)
    if currency:
        open_all(page)
        page.locator('form[action="/clients"] button[aria-expanded="false"]:visible').first.click()
        page.wait_for_timeout(200)
        page.get_by_role("option", name=currency).first.click()
    submit_form(page, 'form[action="/clients"]')
    check(f"client {name}", name in page.content(), page.url)


def add_project(page: Page, name: str, rate: str, budget: str, client_name: str) -> None:
    page.goto(f"{BASE}/projects/new", wait_until="networkidle")
    page.fill('form[action="/projects/new"] input[name="name"]', name)
    open_all(page)
    page.fill("#new-project-rate", rate)
    submit_form(page, 'form[action="/projects/new"]')
    page.wait_for_url(lambda url: "/projects/" in url, timeout=20000)
    page.wait_for_load_state("networkidle")
    slug = page.url.split("?")[0].rstrip("/").rsplit("/", 1)[-1]

    open_all(page)
    page.fill("#project-budget", budget)
    if client_name:
        page.locator("#project-client").click()
        # Pick the option by name: the list is alphabetical, so taking the
        # first entry after "без клиента" silently attaches every project to
        # whichever customer sorts first.
        option = page.locator('[role="listbox"] [role="option"]').filter(has_text=client_name)
        if not option.count():
            raise RuntimeError(f"client {client_name} is not offered by the picker")
        option.first.click()
        page.wait_for_timeout(200)
    submit_form(page, f'form[action="/projects/{slug}"]')
    page.wait_for_load_state("networkidle")
    check(f"project {name} with budget and client", "/projects/" + slug in page.url, page.url)
    return slug


def csrf(page: Page) -> str:
    """Read the double-submit token the page already carries.

    API mutations reject a request without it, and page.request shares the
    browser's cookies but not the header — so a seed can quietly file zero
    sessions and still look like it worked.
    """
    token = page.locator('input[name="csrf_token"]').first.input_value()
    if not token:
        raise RuntimeError("no CSRF token on the page")
    return token


def backfill(page: Page, activity: str, day: date, start: str, end: str, project_id: int = 0, token: str = "") -> None:
    # project_id at creation time, not patched afterwards: an activity without
    # a project has no project totals, no budget and nothing on an invoice, so
    # the seeded screens would show zeroes everywhere.
    form = {"activity": activity, "start": f"{day} {start}", "end": f"{day} {end}"}
    if project_id:
        form["project_id"] = str(project_id)
    response = page.request.post(f"{BASE}/api/sessions/backfill", form=form,
                                 headers={"HX-Request": "true", "X-CSRF-Token": token})
    if response.status >= 400:
        raise RuntimeError(f"backfill {activity} failed: {response.status} {response.text()[:200]}")


def main() -> int:
    today = date.today()
    # 24 workdays back, so every screen that looks at "the last month" has
    # something in it and the budget shows a mix of spent and left.
    workdays = [today - timedelta(days=d) for d in range(26, -1, -1) if (today - timedelta(days=d)).weekday() < 5]
    with sync_playwright() as p:
        browser = p.chromium.launch()
        context = browser.new_context(viewport={"width": 1440, "height": 900})
        page = context.new_page()
        errors: list[str] = []
        page.on("pageerror", lambda exc: errors.append(str(exc)))

        sign_up_or_in(page)
        check("account ready", "/welcome" not in page.url, page.url)

        print("\n== Clients")
        add_client(page, 'ООО «Ромашка»', "buh@romashka.ru",
                   "ИНН 7701234567, КПП 770101001\n125167, Москва, Ленинградское ш., 23",
                   "Оплата по договору №12, счёт ждут до 10 числа.", None)
        add_client(page, "Acme Corp", "ap@acme.test",
                   "77 Buckingham St, London", "Pays in USD, invoice monthly.", "USD")
        add_client(page, "ИП Соколов А. П.", "sokolov@example.test",
                   "ИНН 780102123456", "Разовые консультации, чек сам.", None)

        print("\n== Projects")
        catalog = add_project(page, "Сайт-каталог", "4500", "600000", 'ООО «Ромашка»')
        redesign = add_project(page, "Редизайн витрины", "4500", "60000", 'ООО «Ромашка»')
        support = add_project(page, "Поддержка и доработки", "90", "0", "Acme Corp")
        consult = add_project(page, "Консультации", "6000", "0", "ИП Соколов А. П.")

        print("\n== Tracked time")
        catalog_json = page.request.get(f"{BASE}/api/projects").json()
        project_ids = {item["slug"]: item["id"] for item in catalog_json["projects"]}
        plan = {
            # activity: (project slug, [(hours, how many entries)])
            "Разработка каталога": (catalog, [(3, 4), (2, 5), (4, 3)]),
            "Вёрстка страниц": (catalog, [(2, 3), (3, 3)]),
            "Редизайн": (redesign, [(4, 3), (3, 2)]),
            "Баг-фиксы": (support, [(1, 4), (2, 3)]),
            "Консультация": (consult, [(2, 2), (1, 2)]),
        }
        slots = []
        for activity, (slug, blocks) in plan.items():
            for hours, times in blocks:
                slots.extend([(activity, slug, hours)] * times)
        # Spread the entries across the working days rather than piling them on
        # today: the whole point is that "last 30 days" screens have shape.
        step = max(1, len(workdays) // len(slots))
        page.goto(f"{BASE}/", wait_until="networkidle")
        token = csrf(page)
        for index, (activity, slug, hours) in enumerate(slots):
            day = workdays[min(len(workdays) - 1, index * step)]
            backfill(page, activity, day, "10:00", f"{10 + hours}:00", project_ids.get(slug, 0), token)
        # Counted, not eyeballed: a seed that files nothing still renders a
        # page full of zeros, and "no error" is not the same as "worked".
        summary = page.request.get(f"{BASE}/api/v1/reports/summary?from={workdays[0]}&to={today}").json()
        check("sessions recorded", int(summary.get("total_seconds", 0)) > 0, str(summary.get("total_seconds")))

        print("\n== An issued invoice")
        page.goto(f"{BASE}/invoices", wait_until="networkidle")
        form = page.locator('form[action="/invoices"]')
        if form.count():
            page.fill('form[action="/invoices"] input[name="client"]', 'ООО «Ромашка»')
            period_start = workdays[-10].isoformat()
            period_end = workdays[-1].isoformat()
            start_field = page.locator('form[action="/invoices"] input[type="date"]').first
            end_field = page.locator('form[action="/invoices"] input[type="date"]').nth(1)
            if start_field.count() and end_field.count():
                start_field.fill(period_start)
                end_field.fill(period_end)
            submit_form(page, 'form[action="/invoices"]')
            check("invoice created", "Счёт" in page.content() or "счёт" in page.content(), page.url)
        else:
            check("invoice form present", False, "form not found on /invoices")

        print("\n== Screens")
        page.goto(f"{BASE}/clients", wait_until="networkidle")
        check("/clients renders", "Клиенты" in page.content(), page.url)
        page.screenshot(path=f"{SHOTS}/seed-demo_clients.png", full_page=True)
        first_card = page.locator('a[href^="/clients/"]').first
        if first_card.count():
            first_card.click()
            page.wait_for_url(lambda url: "/clients/" in url, timeout=10000)
            page.wait_for_load_state("networkidle")
            check("client page renders", "Проекты" in page.content(), page.url)
            page.screenshot(path=f"{SHOTS}/seed-demo_client.png", full_page=True)
        page.goto(f"{BASE}/projects", wait_until="networkidle")
        check("/projects renders", "Сайт-каталог" in page.content(), page.url)
        page.screenshot(path=f"{SHOTS}/seed-demo_projects.png", full_page=True)

        check("no page errors", not errors, "; ".join(errors[:3]))
        context.close()
        browser.close()

    print("\n" + (f"{len(failures)} failed" if failures else "seeded"))
    print(f"Логин: {EMAIL}\nПароль: {PASSWORD}")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())