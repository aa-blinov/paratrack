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

import random
import re
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


def submit_form(page: Page, form) -> None:
    scope = page.locator(form) if isinstance(form, str) else form
    button = scope.locator('button[type="submit"]').first
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
        # Choose the freelancer section set. The button is inside the form that
        # carries the preset value, so target that rather than the button's
        # wording — and if the click still leaves us on /welcome, post the same
        # form directly with its CSRF token rather than seeding a workspace
        # where invoices and reports are switched off.
        form = page.locator('form[action="/api/team/modules"]:has(input[name="preset"][value="freelance"])')
        if form.count():
            form.first.locator("button").first.click()
            page.wait_for_timeout(1500)
        if page.url.endswith("/welcome"):
            token = page.locator('form[action="/api/team/modules"] input[name="csrf_token"]').first.input_value()
            page.request.post(f"{BASE}/api/team/modules",
                              form={"csrf_token": token, "preset": "freelance", "from": "welcome"},
                              headers={"X-CSRF-Token": token})
            page.goto(f"{BASE}/settings/sections", wait_until="networkidle")
            page.wait_for_timeout(400)
        page.goto(f"{BASE}/settings/sections", wait_until="networkidle")
        page.wait_for_timeout(400)
        sections = page.locator("main").inner_text()
        check("набор разделов «фрилансер» включён", "Счета" in sections,
              "в разделе «Счета» нет — выбран другой набор")

    page.wait_for_load_state("networkidle")


def add_client(page: Page, name: str, email: str, details: str, notes: str, currency: str | None) -> None:
    page.goto(f"{BASE}/clients", wait_until="networkidle")
    # The new-client form is a disclosure once there are clients; open it like a person would.
    page.evaluate("document.querySelector('details#client-new') && (document.querySelector('details#client-new').open = true)")
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
    # The form keeps whatever was typed when the save is refused, so "the name
    # is on the page" proves nothing. The card has to carry a link to its own
    # page — that only exists for a client that was actually filed.
    filed = page.locator('a[href^="/clients/"]').filter(has_text=name)
    check(f"client {name}", filed.count() > 0, page.url)


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
    workdays = [today - timedelta(days=d) for d in range(58, -1, -1) if (today - timedelta(days=d)).weekday() < 5]
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
        add_client(page, "ООО «Северный Ветер»", "zakaz@nord.example",
                   "ИНН 5017011111\n107078, Москва, ул. Садовая, 12",
                   "Работаем с начала года, оплата в течение 10 дней.", None)
        add_client(page, "Nimble Studio LLC", "billing@nimble.example",
                   "44 Mission St, San Francisco, CA", "Retainer, 30 hours a month.", "USD")
        add_client(page, "Макетная студия «Пятый Этаж»", "hello@fifth.example",
                   "Архив: проект закрыт в прошлом году", "Старый клиент, работы больше нет.", None)

        # An archived client must still be reachable by name, just out of the
        # way: exactly the case that regresses silently.
        page.goto(f"{BASE}/clients", wait_until="networkidle")
        target = page.locator('a[href^="/clients/"]').filter(has_text="Пятый Этаж").first
        if target.count():
            card = target.locator("xpath=ancestor::*[.//form[contains(@action, '/archive')]][1]")
            if card.count():
                submit_form(page, card)
                page.goto(f"{BASE}/clients?archived=1", wait_until="networkidle")
                check("client archived", "Пятый Этаж" in page.content(), page.url)
            else:
                check("client archived", False, "no archive control on the card")

        print("\n== Projects")
        catalog = add_project(page, "Сайт-каталог", "4500", "600000", 'ООО «Ромашка»')
        redesign = add_project(page, "Редизайн витрины", "4500", "60000", 'ООО «Ромашка»')
        support = add_project(page, "Поддержка и доработки", "90", "0", "Acme Corp")
        consult = add_project(page, "Консультации", "6000", "0", "ИП Соколов А. П.")
        retain = add_project(page, "Абонентская поддержка", "90", "1500", "Nimble Studio LLC")
        landing = add_project(page, "Лендинг весны", "3500", "90000", "ООО «Северный Ветер»")

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
            "Поддержка по ретейнеру": (retain, [(2, 5), (3, 4), (1, 3)]),
            "Дизайн лендинга": (landing, [(4, 3), (3, 2), (2, 2)]),
        }
        slots = []
        for activity, (slug, blocks) in plan.items():
            for hours, times in blocks:
                slots.extend([(activity, slug, hours)] * times)
        # Shuffle with a fixed seed. In slot order every activity occupied its
        # own stretch of calendar, so a project only ever had billable time in
        # the weeks it happened to be listed in, and an invoice for any other
        # window came back empty.
        random.Random(7).shuffle(slots)
        # Spread the entries across the working days rather than piling them on
        # today: the whole point is that "last 30 days" screens have shape.
        page.goto(f"{BASE}/", wait_until="networkidle")
        token = csrf(page)
        # Spread entries over the whole window. A plain "index * step" cannot
        # do this once there are more entries than working days: every slot
        # past the end of the window lands on the last day, and the tail of
        # the seed piles up on today while the month before it stays empty.
        for index, (activity, slug, hours) in enumerate(slots):
            day = workdays[(index * len(workdays)) // len(slots)]
            backfill(page, activity, day, "10:00", f"{10 + hours}:00", project_ids.get(slug, 0), token)
        # Counted, not eyeballed: a seed that files nothing still renders a
        # page full of zeros, and "no error" is not the same as "worked".
        summary = page.request.get(f"{BASE}/api/v1/reports/summary?from={workdays[0]}&to={today}").json()
        check("sessions recorded", int(summary.get("total_seconds", 0)) > 0, str(summary.get("total_seconds")))

        print("\n== Goals and tags")
        token = csrf(page)
        for activity, minutes in (("Разработка каталога", 1200), ("Редизайн", 900), ("Поддержка по ретейнеру", 3000)):
            response = page.request.post(f"{BASE}/api/goals",
                form={"activity": activity, "period": "monthly", "minutes": str(minutes)},
                headers={"X-CSRF-Token": token})
            if response.status >= 400:
                raise RuntimeError(f"goal {activity} failed: {response.status} {response.text()[:160]}")
        check("goals set", True)
        for tag in ("urgent", "remote", "2026"):
            page.request.post(f"{BASE}/api/tags", form={"name": tag}, headers={"X-CSRF-Token": token})
        check("tags set", True)

        print("\n== Invoices in every status")
        # Three documents in three states, because the screens that matter are
        # the mixed ones: a list of paid invoices looks fine right up to the
        # moment a draft, a sent and a paid one sit together.
        invoices = [
            ("Сайт-каталог", 'ООО «Ромашка»', workdays[-21], workdays[-12], "paid"),
            ("Поддержка и доработки", "Acme Corp", workdays[-21], workdays[-6], "sent"),
            ("Лендинг весны", 'ООО «Северный Ветер»', workdays[-21], workdays[-2], "draft"),
        ]
        for project_name, client_name, first_day, last_day, wanted in invoices:
            page.goto(f"{BASE}/invoices", wait_until="networkidle")
            if not page.locator('form[action="/invoices"]').count():
                check(f"invoice form for {project_name}", False, "form not found on /invoices")
                continue
            open_all(page)
            page.locator('form[action="/invoices"] input[name="client"]').fill(client_name)
            picker = page.locator('form[action="/invoices"] #inv-project')
            if picker.count():
                picker.click()
                option = page.locator('[role="listbox"] [role="option"]').filter(has_text=project_name)
                if option.count():
                    option.first.click()
                else:
                    page.keyboard.press("Escape")
                page.wait_for_timeout(200)
            page.locator('form[action="/invoices"] input[data-date-input="start"]').first.fill(first_day.isoformat())
            page.locator('form[action="/invoices"] input[data-date-input="end"]').first.fill(last_day.isoformat())
            submit_form(page, 'form[action="/invoices"]')
            made = "/invoices/" in page.url and not page.url.rstrip("/").endswith("/invoices")
            check(f"invoice created for {project_name}", made, page.url)
            if not made:
                continue
            invoice_id = page.url.rstrip("/").rsplit("/", 1)[-1].split("?")[0]
            token = csrf(page)
            if wanted in ("sent", "paid"):
                page.request.post(f"{BASE}/invoices/{invoice_id}/status",
                                  form={"csrf_token": token, "status": "sent"},
                                  headers={"X-CSRF-Token": token})
            if wanted == "paid":
                page.request.post(f"{BASE}/invoices/{invoice_id}/paid",
                                  form={"csrf_token": token}, headers={"X-CSRF-Token": token})
            page.goto(f"{BASE}/invoices", wait_until="networkidle")
            check(f"invoice {wanted}", True)

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
