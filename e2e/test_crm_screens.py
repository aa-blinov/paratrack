"""E2E walkthrough for the CRM and calendar screens, with screenshots.

Registers a fresh studio account, then drives the real UI: a client with
requisites, a contact, two deals (one moved to another stage), a logged call,
a lead, a timed calendar event and an all-day one. Every screenshot gets one
PASS/FAIL line: the page answered 200 and its key content is visible.

Run from the repo root with the .venv active:

    python e2e/test_crm_screens.py

The target defaults to http://127.0.0.1:8888 and can be overridden with
PARATRACK_BASE.
"""

from __future__ import annotations

import sys
import uuid
from datetime import datetime, timedelta, timezone
from pathlib import Path

from playwright.sync_api import expect, sync_playwright

from target import BASE_URL as BASE
from browser import launch

SCREENSHOTS = Path(__file__).parent / "screenshots"
SCREENSHOTS.mkdir(parents=True, exist_ok=True)

results: list[tuple[str, bool, str]] = []


def check(name: str, ok: bool, detail: str = "") -> None:
    status = "PASS" if ok else "FAIL"
    print(f"  [{status}] {name}{(' — ' + detail) if detail else ''}")
    results.append((name, ok, detail))


def shot(page, name: str) -> Path:
    p = SCREENSHOTS / f"{name}.png"
    page.screenshot(path=str(p), full_page=True)
    print(f"          📸 {p.name} ({p.stat().st_size // 1024} KB)")
    return p


def open_disclosures(page, target: str) -> None:
    """Open the disclosure that holds `target`, if it is closed.

    Radix disclosures start closed when there is nothing to suggest, and the
    CRM forms (deal, lead) sit inside them.
    """
    for _ in range(4):
        root = page.locator(f'[data-disclosure]:has({target})')
        if not root.count():
            return
        if root.first.get_attribute("data-state") == "open":
            return
        trigger = root.first.locator("> button[aria-expanded]").first
        if not trigger.count():
            return
        trigger.evaluate("node => node.click()")
        page.wait_for_timeout(300)


def main() -> int:
    today = datetime.now(timezone.utc).date()
    tomorrow = today + timedelta(days=1)
    day_after = today + timedelta(days=2)

    with sync_playwright() as p:
        browser = launch(p)
        context = browser.new_context(
            viewport={"width": 1280, "height": 900},
            color_scheme="light",
            locale="ru-RU",
            device_scale_factor=2,
        )
        page = context.new_page()
        page.on("requestfailed", lambda r: print(f"  reqfail: {r.url} — {r.failure}"))

        # ------------------------------------------------------------------
        print("\n== 1. Register and pick the studio sections")
        email = f"crm-{uuid.uuid4().hex[:12]}@paratrack.test"
        page.goto(BASE + "/register")
        page.fill("#name", "CRM Студия")
        page.fill("#email", email)
        page.fill("#password", "paratrack-demo")
        page.click('button[type="submit"]')
        page.wait_for_load_state("load")
        if page.url.endswith("/welcome"):
            page.locator(
                'form[action="/api/team/modules"]:has(input[name="preset"][value="studio"]):not([data-welcome-skip]) button'
            ).click()
        page.wait_for_url(BASE + "/")
        check("register + studio preset reaches the dashboard", page.url.rstrip("/") == BASE, page.url)

        # ------------------------------------------------------------------
        print("\n== 2. Client with requisites (the /clients form)")
        resp = page.goto(BASE + "/clients")
        expect(page.locator("h1")).to_have_text("Клиенты")
        # The create block opens on an empty directory; otherwise open it.
        if not page.locator("#new-client-name").is_visible():
            page.locator("#client-new summary").click()
        page.fill("#new-client-name", "ООО Ромашка")
        page.fill("#new-client-email", "buh@romashka.ru")
        page.fill("#new-client-details", "ИНН 7707083893, КПП 770701001, 127006, г. Москва, ул. Тверская, 1")
        page.fill("#new-client-notes", "Оплата по договору №12, оплачивает бухгалтерия")
        page.locator('form[action="/clients"] button[type="submit"]').click()
        row = page.locator('a[href^="/clients/"]', has_text="ООО Ромашка").first
        expect(row).to_be_visible(timeout=5000)
        client_href = row.get_attribute("href")
        check("client card in the directory", resp.status == 200 and bool(client_href), f"{resp.status} {client_href}")

        # ------------------------------------------------------------------
        print("\n== 3. CRM: contact, deals, a stage move and a logged call")
        resp = page.goto(BASE + "/crm")
        expect(page.locator("h1")).to_have_text("CRM")
        expect(page.locator("text=Поиск клиентов и контактов")).to_be_visible()
        check("/crm opens", resp.status == 200, f"{resp.status}")

        # The contact panel picks the first client on its own; one client exists.
        open_disclosures(page, "#contact-name")
        page.fill("#contact-name", "Анна Смирнова")
        page.fill("#contact-role", "Менеджер закупок")
        page.fill("#contact-email", "anna@romashka.ru")
        page.fill("#contact-phone", "+7 495 000-11-22")
        page.get_by_role("button", name="Добавить контакт").click()
        expect(page.locator("li", has_text="Анна Смирнова").first).to_be_visible(timeout=5000)
        check("contact saved and listed under the client", True)

        # Deal one: the pipeline board shows it under «Новая».
        open_disclosures(page, "#deal-new-title")
        page.fill("#deal-new-title", "Портал для Ромашки")
        page.locator("#deal-new-client").click()
        page.get_by_role("option", name="ООО Ромашка").click()
        page.fill("#deal-new-amount", "450000")
        page.fill("#deal-new-probability", "60")
        page.get_by_role("button", name="Создать сделку").click()
        expect(page.locator('section[aria-labelledby="stage-new"] li', has_text="Портал для Ромашки")).to_be_visible(
            timeout=5000
        )
        check("deal «Портал для Ромашки» in stage «Новая»", True)

        # Deal two, then moved to «В работе» through the deal's own form.
        page.fill("#deal-new-title", "Мобильное приложение")
        page.locator("#deal-new-client").click()
        page.get_by_role("option", name="ООО Ромашка").click()
        page.fill("#deal-new-amount", "900000")
        page.fill("#deal-new-probability", "30")
        page.get_by_role("button", name="Создать сделку").click()
        expect(page.locator('section[aria-labelledby="stage-new"] li', has_text="Мобильное приложение")).to_be_visible(
            timeout=5000
        )
        page.locator('section[aria-labelledby="stage-new"] li', has_text="Мобильное приложение").locator(
            "button[aria-expanded]"
        ).first.click()
        stage_move = page.locator('section[aria-labelledby$="-stage"]')
        expect(stage_move).to_be_visible(timeout=5000)
        stage_move.locator('button[id$="-target"]').click()
        page.get_by_role("option", name="В работе").click()
        stage_move.get_by_role("button", name="Перевести").click()
        expect(
            page.locator('section[aria-labelledby="stage-working"] li', has_text="Мобильное приложение")
        ).to_be_visible(timeout=5000)
        check("deal «Мобильное приложение» moved to «В работе»", True)

        # A call on the open deal's timeline.
        timeline = page.locator('section[aria-labelledby$="-timeline"]')
        expect(timeline).to_be_visible(timeout=5000)
        timeline.locator('button[id$="-kind"]').click()
        page.get_by_role("option", name="Звонок").click()
        timeline.locator('textarea[id$="-body"]').fill("Обсудили смету на портал, прислали КП")
        timeline.get_by_role("button", name="Записать").click()
        expect(timeline.locator("li", has_text="Обсудили смету")).to_be_visible(timeout=5000)
        check("call logged on the deal timeline", True)

        # ------------------------------------------------------------------
        print("\n== 4. Screenshot: /crm board (deals + clients in search)")
        # Typing in the client search puts the client and its contact on the
        # same screen as the pipeline, so one frame carries both halves of CRM.
        search = page.get_by_label("Поиск клиентов и контактов")
        search.fill("Ромашка")
        expect(page.locator('a[href^="/clients/"]', has_text="ООО Ромашка").first).to_be_visible(timeout=5000)
        shot(page, "crm-01-board")
        board_ok = (
            page.locator('section[aria-labelledby="stage-new"] li', has_text="Портал для Ромашки").count() == 1
            and page.locator('section[aria-labelledby="stage-working"] li', has_text="Мобильное приложение").count() == 1
            and page.locator('a[href^="/clients/"]', has_text="ООО Ромашка").count() >= 1
            and page.locator("li", has_text="Анна Смирнова").count() >= 1
        )
        check("crm-01-board: deals by stage, client and contact visible", board_ok)

        # ------------------------------------------------------------------
        print("\n== 5. Screenshot: the client card")
        resp = page.goto(BASE + (client_href or "/clients"))
        expect(page.locator("h1")).to_contain_text("ООО Ромашка")
        # Scoped to paragraphs: the bootstrap JSON in <div hidden> carries the
        # same strings, and a bare text= locator would hit both.
        expect(page.locator("p", has_text="ИНН 7707083893")).to_be_visible()
        shot(page, "crm-02-client")
        check(
            "crm-02-client: name, requisites and invoice email on the card",
            resp.status == 200
            and page.locator("p", has_text="ИНН 7707083893").count() >= 1
            and page.locator("p", has_text="buh@romashka.ru").count() >= 1,
            f"{resp.status}",
        )

        # ------------------------------------------------------------------
        print("\n== 6. Screenshot: the leads tab")
        resp = page.goto(BASE + "/crm")
        expect(page.locator("h1")).to_have_text("CRM")
        page.get_by_role("button", name="Лиды").click()
        expect(page.locator("text=Новый лид").first).to_be_visible()
        open_disclosures(page, "#lead-new-company")
        page.fill("#lead-new-company", "ООО Технопарк")
        page.fill("#lead-new-contact", "Игорь Петров")
        page.fill("#lead-new-phone", "+7 921 000-33-44")
        page.fill("#lead-new-email", "igor@technopark.ru")
        page.fill("#lead-new-source", "Сайт")
        page.fill("#lead-new-message", "Хотим портал для дилеров, бюджет обсуждаем.")
        page.locator('button[type="submit"]:has-text("Новый лид")').click()
        expect(page.locator("li", has_text="ООО Технопарк").first).to_be_visible(timeout=5000)
        shot(page, "crm-03-leads")
        check(
            "crm-03-leads: lead listed with its status badge",
            resp.status == 200
            and page.locator("li", has_text="ООО Технопарк").count() == 1
            and page.locator("li", has_text="ООО Технопарк").locator("text=Новый").count() >= 1,
            f"{resp.status}",
        )

        # ------------------------------------------------------------------
        print("\n== 7. Calendar: a timed event and an all-day one")
        resp = page.goto(BASE + "/calendar")
        expect(page.locator("h1")).to_have_text("Календарь")
        open_disclosures(page, "#event-new-title")
        page.fill("#event-new-title", "Созвон с Ромашкой")
        page.fill("#event-new-start", f"{today}T18:00")
        page.fill("#event-new-end", f"{today}T19:00")
        page.fill("#event-new-location", "Zoom")
        page.fill("#event-new-notes", "Смета и план релиза")
        page.locator('label:has-text("Видно всей команде") [role="checkbox"]').click()
        page.get_by_role("button", name="Создать событие").click()
        expect(page.locator("li", has_text="Созвон с Ромашкой").first).to_be_visible(timeout=5000)
        check("timed event in the week agenda", True)

        page.fill("#event-new-title", "Конференция Дизайн-Осень")
        page.locator('label:has-text("Весь день") [role="checkbox"]').click()
        # The fields turn into plain days once the event is all-day.
        page.fill("#event-new-start", str(tomorrow))
        page.fill("#event-new-end", str(day_after))
        page.fill("#event-new-location", "Москва, Экспо")
        page.get_by_role("button", name="Создать событие").click()
        # A saved create empties the form, and a refused one leaves the title
        # in place, so the cleared field is the save signal.
        expect(page.locator("#event-new-title")).to_have_value("", timeout=5000)
        check("all-day event saved (form cleared)", True)

        # ------------------------------------------------------------------
        print("\n== 8. Screenshot: the month grid, then the week agenda")
        page.locator('div[aria-label="Вид календаря"] button', has_text="Месяц").click()
        grid = page.locator("div.grid-cols-7")
        expect(grid).to_be_visible(timeout=5000)
        expect(page.locator("button[aria-pressed]", has_text="Созвон с Ромашкой").first).to_be_visible(timeout=5000)
        shot(page, "calendar-01-month")
        month_ok = (
            resp.status == 200
            and page.locator("button[aria-pressed]", has_text="Созвон с Ромашкой").count() >= 1
            and page.locator("button[aria-pressed]", has_text="Конференция Дизайн-Осень").count() >= 1
        )
        check("calendar-01-month: both events sit in the month grid", month_ok, f"{resp.status}")

        # The day picked in the grid lists its events below. The id sits on the
        # day's heading, so the list is the section that heading labels.
        page.locator("button[aria-pressed]", has_text="Конференция Дизайн-Осень").first.click()
        picked = page.locator('section[aria-labelledby="picked-day"]', has_text="Конференция Дизайн-Осень")
        expect(picked).to_be_visible(timeout=5000)
        check("the picked day lists the all-day event below the grid", picked.count() == 1)

        page.locator('div[aria-label="Вид календаря"] button', has_text="Неделя").click()
        today_section = page.locator(f'section[aria-labelledby="day-{today}"]')
        expect(today_section.locator("li", has_text="Созвон с Ромашкой")).to_be_visible(timeout=5000)
        shot(page, "calendar-02-week")
        week_ok = (
            today_section.locator("li", has_text="Созвон с Ромашкой").count() == 1
            and page.locator("text=Общее").count() >= 1
        )
        check("calendar-02-week: the agenda shows today's shared call", week_ok)

        context.close()
        browser.close()

    failed = [name for name, ok, _ in results if not ok]
    print(f"\nCRM/calendar screens: {len(results) - len(failed)}/{len(results)} checks passed")
    for name in failed:
        print(f"  FAILED: {name}")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
