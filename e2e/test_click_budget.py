"""UX-42: гейт «кликов до действий» на золотых путях.

Считает клики (кнопки и ссылки; ввод текста и выбор значения в полях —
не считаются, как в клик-картах `docs/screens`) на золотых путях
и падает, как только путь дороже бюджета из клик-карты:

    Запустить таймер             1  (docs/screens/dashboard.md)
    Остановить таймер            1  (docs/screens/dashboard.md)
    Добавить сессию задним числом 2  (docs/screens/dashboard.md)
    Выставить счёт с обзора       3  (в клик-карте 2: «Выставить счёт» →
                                     «Сформировать»; подтверждение FIN-12
                                     для неутверждённого окна добавило клик)
    Утвердить неделю и выплатить  7  (меню → Табель; ←; «Утвердить неделю»;
                                     «Подтвердить»; меню → Выплаты →
                                     «Сформировать» = 2 по клик-карте
                                     payroll.md; «Выплачено» = 1 — после
                                     формирования расчёта его страница
                                     уже открыта)

Подтверждения в диалогах — тоже клики человека, поэтому считаются.

Запуск (нужен работающий сервер, свежий стенд через scripts/qa-env.sh):

    PYTHONIOENCODING=utf-8 PARATRACK_BASE=http://127.0.0.1:8890 \
        .venv/Scripts/python.exe e2e/test_click_budget.py
"""

from __future__ import annotations

import datetime
import re
import sys
import uuid
from pathlib import Path

from playwright.sync_api import sync_playwright

from browser import launch
from target import BASE_URL as BASE

BUDGETS = {
    "Запустить таймер": 1,
    "Остановить таймер": 1,
    "Добавить сессию задним числом": 2,
    "Выставить счёт с обзора": 3,
    "Утвердить неделю и выплатить": 7,
}


def check(name: str, ok: bool, detail: str = "") -> None:
    print(f"  [{'PASS' if ok else 'FAIL'}] {name}" + (f": {detail}" if not ok and detail else ""))


class Budget:
    """Счётчик кликов одного пути. Дороже бюджета — гейт красный сразу."""

    def __init__(self, name: str) -> None:
        self.name = name
        self.limit = BUDGETS[name]
        self.spent = 0

    def click(self, page, locator) -> None:
        self.spent += 1
        if self.spent > self.limit:
            print(f"\n  [FAIL] {self.name}: {self.spent} кликов при бюджете {self.limit}")
            print("Клик-бюджет превышен — верни путь к карте в docs/screens или обоснуй новый бюджет там же.")
            raise SystemExit(1)
        locator.click()

    def assert_within(self) -> None:
        status = "PASS" if self.spent <= self.limit else "FAIL"
        print(f"  [{status}] {self.name}: {self.spent}/{self.limit} кликов")
        if self.spent > self.limit:
            raise SystemExit(1)


def menu_link(page, path: str, budget, group_title: str) -> None:
    """Клик по пункту меню. Складные группы («Команда», «Данные») на невысоком
    экране закрыты — их открытие не считают: и клик-карта, и этот гейт мерят
    путь при раскрытом сайдбаре («Отсчёт от обзора, сайдбар раскрыт»)."""
    link = page.locator(f'a[href="{path}"]').first
    toggle = page.locator(f'button[aria-expanded]:has-text("{group_title}")').first
    if toggle.count() and toggle.get_attribute("aria-expanded") != "true":
        toggle.click()
        page.wait_for_timeout(400)
    budget.click(page, link)


def confirm_dialog(page, budget) -> None:
    """Подтверждение — последний кнопкой диалога, и оно считается кликом."""
    page.get_by_role("alertdialog").wait_for(timeout=10000)
    budget.click(page, page.get_by_role("alertdialog").locator("button").last)


def _csrf(page) -> str:
    for c in page.context.cookies():
        if c.get("name") == "paratrack_csrf":
            return c.get("value") or ""
    return ""


METHODS = {"GET": "get", "POST": "post", "PATCH": "patch", "DELETE": "delete"}


def api(page, method: str, url: str, **kw):
    headers = dict(kw.pop("headers", None) or {})
    headers.setdefault("X-CSRF-Token", _csrf(page))
    if url.startswith("/"):
        url = BASE + url
    return getattr(page.request, METHODS[method])(url, headers=headers, **kw)


def register(page, name: str, email: str, password: str) -> None:
    page.goto(BASE + "/register")
    page.fill("#name", name)
    page.fill("#email", email)
    page.fill("#password", password)
    page.click('button[type="submit"]')
    page.wait_for_load_state("load")
    if page.url.endswith("/welcome"):
        page.locator('form[action="/api/team/modules"]:has(input[name="preset"][value="studio"]) button').click()
        page.wait_for_url(f"{BASE}/")


def main() -> int:
    stamp = uuid.uuid4().hex[:8]
    email = f"click-budget-{stamp}@paratrack.test"
    today = datetime.date.today()
    # Расчётная неделя — прошлая, законченная. Часы для счёта даёт сам таймер:
    # старт+стоп на текущей неделе, расчёт её не трогает (окно расчёта —
    # прошлая неделя), счёт и выплату не клинит на одних часах.
    pay_monday = today - datetime.timedelta(days=today.weekday() + 7)

    with sync_playwright() as p:
        browser = launch(p)
        context = browser.new_context(viewport={"width": 1280, "height": 900})
        page = context.new_page()

        # ---------------- подготовка (клики не считаются) --------------------
        print("== Setup")
        register(page, "Клик-бюджет", email, "click-budget-qa-pass-2026")
        # Проект со ставкой: время на нём попадает в счёт и в виджет.
        page.goto(BASE + "/projects/new")
        page.fill("#new-project-name", "Клик-бюджет")
        page.fill("#new-project-rate", "100")
        page.locator("main form button[type=submit]").first.click()
        page.wait_for_url(re.compile(r"/projects/"))
        # Проект по умолчанию: форма старта и бэкфилла уже несут его в себе,
        # иначе золотой путь просел бы на выбор проекта, а карта мерила
        # состояние «проект выбран заранее» (dashboard.md, строка
        # «Если у пространства задан проект по умолчанию…»).
        projects = api(page, "GET", "/api/v1/projects").json()
        plist = projects.get("projects", projects)
        proj_id = str(plist[0]["id"])
        # urlencoded вручную: ключ widgets повторяется, словарь формы
        # Playwright повторов не умеет.
        from urllib.parse import urlencode
        api(page, "POST", "/api/me/preferences", data=urlencode([
            ("widgets", "unbilled"), ("widgets", "goals"),
            ("widgets", "recent"), ("widgets", "backfill"),
            ("default_project", proj_id),
        ]), headers={"Content-Type": "application/x-www-form-urlencoded"})
        # Почасовая ставка участнику, иначе расчёт не сформируется.
        page.goto(BASE + "/settings/members")
        pay_form = page.locator('form[action="/api/member/pay"]').first
        pay_form.locator('input[name="hourly_pay"]').fill("50")
        pay_form.locator('input[name="capacity_minutes"]').fill("480")
        pay_form.locator('button[type="submit"]').click()
        page.wait_for_load_state("networkidle")
        print("  setup done")

        # ------------ путь 1: сессия задним числом (бюджет 2) ----------------
        print("\n== Путь: добавить сессию задним числом")
        backfill = Budget("Добавить сессию задним числом")
        day = (pay_monday + datetime.timedelta(days=1)).isoformat()
        page.goto(BASE + "/", wait_until="networkidle")
        # Подсказка «Забыли запустить?» сама раскрывает форму: 1 клик.
        backfill.click(page, page.locator("[data-backfill-hint]"))
        page.fill("#b-activity", "консультация")
        page.fill("#b-start", f"{day} 09:00")
        page.fill("#b-end", f"{day} 11:00")
        backfill.click(page, page.locator("#backfill form button[type=submit]"))
        page.wait_for_timeout(600)
        check("сессия появилась в итогах", "консультация" in page.inner_text("body"))
        backfill.assert_within()

        # ------------ путь 2: старт таймера (бюджет 1) -----------------------
        print("\n== Путь: запустить таймер")
        start = Budget("Запустить таймер")
        page.fill("#activity", "правка текста")
        start.click(page, page.locator("button[data-action-primary]").first)
        page.wait_for_timeout(800)
        check("таймер идёт", page.locator("#active-list [data-session-id]").count() == 1)
        start.assert_within()

        # ------------ путь 3: стоп таймера (бюджет 1) ------------------------
        print("\n== Путь: остановить таймер")
        stop = Budget("Остановить таймер")
        stop.click(page, page.locator("#active-list").get_by_role("button", name=re.compile("Стоп|Stop")).first)
        page.wait_for_timeout(800)
        check("полоса идущих пуста", page.locator("#active-list [data-session-id]").count() == 0)
        stop.assert_within()

        # ------------ путь 4: счёт с обзора (бюджет 3) -----------------------
        print("\n== Путь: выставить счёт с обзора")
        invoice = Budget("Выставить счёт с обзора")
        # Часы для счёта — сессия остановленного таймера на проекте.
        page.goto(BASE + "/", wait_until="networkidle")
        check("виджет невыставленного на обзоре", page.get_by_role("link", name="Выставить счёт").count() >= 1)
        invoice.click(page, page.get_by_role("link", name="Выставить счёт").first)
        page.wait_for_load_state("networkidle")
        page.fill("#inv-client", "QA Клиент")
        invoice.click(page, page.locator('form[action="/invoices"] button[type="submit"]').first)
        page.wait_for_load_state("networkidle")
        # Окно затрагивает неутверждённое время — счёт спрашивает (FIN-12).
        confirm_dialog(page, invoice)
        page.wait_for_url("**/invoices/*")
        check("счёт создан", "INV-" in page.inner_text("body"))
        invoice.assert_within()

        # ------------ путь 5: утвердить неделю и выплатить (бюджет 7) --------
        print("\n== Путь: утвердить неделю и выплатить")
        paid = Budget("Утвердить неделю и выплатить")
        # меню → Табель
        menu_link(page, "/timesheet", paid, "Время")
        page.wait_for_load_state("networkidle")
        # ← к расчётной неделе
        paid.click(page, page.locator('main a[title="Прошлая неделя"], main a[aria-label="Прошлая неделя"]').first)
        page.wait_for_load_state("networkidle")
        paid.click(page, page.locator('form[action="/api/timesheet/approve"] button[type="submit"]').first)
        # Неделя закроется для правок участников — подтвердить.
        confirm_dialog(page, paid)
        page.wait_for_load_state("networkidle")
        check("неделя утверждена", "Неделя утверждена" in page.inner_text("body"))
        # меню → Выплаты; пока расчётов нет, форма раскрыта и выбор периода
        # текстом кликом не считается (правило клик-карт).
        menu_link(page, "/payroll", paid, "Команда")
        page.wait_for_selector('form[action="/payroll"] button[type="submit"]')
        page.fill('form[action="/payroll"] input[data-date-input="start"]', pay_monday.isoformat())
        page.fill('form[action="/payroll"] input[data-date-input="end"]', (pay_monday + datetime.timedelta(days=6)).isoformat())
        paid.click(page, page.locator('form[action="/payroll"] button[type="submit"]'))
        page.wait_for_url("**/payroll/*")
        check("расчёт создан", "PAY-" in page.inner_text("body"))
        paid.click(page, page.locator('form[action$="/paid"] button[type="submit"]').first)
        page.wait_for_load_state("networkidle")
        check("расчёт отмечен выплаченным", page.locator('form[action$="/paid"]').count() == 0)
        paid.assert_within()

        browser.close()

    print("\nВсе золотые пути в бюджете.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
