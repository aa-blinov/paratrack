"""Fifth QA pass: the last two claims.

1. Can a participant really correct her own session in place on /stats
   ("Начало, конец, длительность и заметка сохраняются после выхода из поля")?
2. Do the project rates the owner entered (140 / 160) show up anywhere in her
   view of the studio?
3. Exact click count from a cold sign-in to a running timer.

Run:  PARATRACK_BASE=http://127.0.0.1:8895 .venv/bin/python e2e/qa_profiles5.py
"""
from __future__ import annotations

import json
import os
from pathlib import Path

from playwright.sync_api import sync_playwright

BASE = os.environ.get("PARATRACK_BASE", "http://127.0.0.1:8895")
MEMBER = "member2@x.test"
PW = "longenoughpw"
ROOT = "#paratrack-react-root"
OUT = Path(__file__).parent / "screenshots" / "qa-profiles"
notes: list[str] = []


def note(section: str, text: str) -> None:
    notes.append(f"[{section}] {text}")
    print(f"[{section}] {text}")


def sign_in(page) -> None:
    page.goto(BASE + "/login")
    page.fill("#email", MEMBER)
    page.fill("#password", PW)
    page.click('button[type="submit"]')
    page.wait_for_load_state("load")
    if "/login" in page.url:
        raise SystemExit("sign-in failed")


def wait_app(page) -> None:
    try:
        page.wait_for_selector(f"{ROOT} .app-boot", state="detached", timeout=15000)
    except Exception:
        pass


def main() -> int:
    report: dict = {}
    with sync_playwright() as p:
        browser = p.chromium.launch()
        page = browser.new_context(viewport={"width": 1280, "height": 900}, locale="ru-RU").new_page()
        errors: list[str] = []
        page.on("pageerror", lambda e: errors.append(str(e)))

        # --- холодный вход: считаем клики до работающего таймера
        clicks = 0
        sign_in(page)
        wait_app(page)
        clicks += 1  # отправить форму входа
        note("клики", f"после входа: {clicks}, пространство: {'студия' if 'Пётр' in page.locator('body').inner_text()[:400] else 'личное'}")
        page.locator(f'{ROOT} button:has-text("Пространство")').first.click()
        clicks += 1
        page.wait_for_timeout(400)
        page.locator(f'{ROOT} [role="menuitem"]:has-text("Пётр")').first.click()
        clicks += 1
        page.wait_for_load_state("load")
        wait_app(page)
        page.wait_for_timeout(400)
        page.locator(f'{ROOT} input[type="text"], {ROOT} input[placeholder]').first.click()
        page.locator(f'{ROOT} input[type="text"], {ROOT} input[placeholder]').first.fill("сверка сметы")
        clicks += 1
        page.get_by_role("button", name="Старт", exact=True).first.click()
        clicks += 1
        page.wait_for_timeout(1500)
        running = " ".join(page.locator("body").inner_text().split())
        report["clicks_to_running_timer"] = clicks
        note("клики", f"от формы входа до идущего таймера: {clicks} кликов ({'студия' if 'Пётр' in running[:400] else '?'})")
        page.screenshot(path=str(OUT / "r1-masha-timer-clicks.png"), full_page=True)
        page.locator(f'{ROOT} main button:has-text("Стоп")').first.click()
        page.wait_for_timeout(1200)

        # --- правка своей сессии на /stats
        page.goto(BASE + "/stats?period=week")
        wait_app(page)
        page.wait_for_timeout(600)
        note("сессии", " ".join(page.locator("body").inner_text().split())[300:900])
        editables = []
        for i in range(page.locator(f"{ROOT} main input, {ROOT} main textarea").count()):
            loc = page.locator(f"{ROOT} main input, {ROOT} main textarea").nth(i)
            try:
                editables.append({"type": loc.get_attribute("type"), "name": loc.get_attribute("name"), "label": loc.get_attribute("aria-label") or "", "value": loc.input_value()})
            except Exception:
                pass
        report["stats_editables"] = editables
        note("поля-правки", f"{editables}")

        # попытка изменить длительность своей сессии
        duration = None
        for item in editables:
            if item["type"] in ("number", "text") and item["name"] and ("dur" in item["name"] or "min" in item["name"] or "note" in item["name"]):
                duration = item
                break
        if duration:
            locator = page.locator(f'{ROOT} main [name="{duration["name"]}"]').first
            locator.click()
            locator.fill("90")
            locator.blur()
            page.wait_for_timeout(1500)
            text_after = " ".join(page.locator("body").inner_text().split())
            report["after_edit"] = text_after[300:900]
            note("правка", f"поле «{duration['name']}» → 90; итог на экране: {text_after[420:700]}")
            page.screenshot(path=str(OUT / "r2-masha-session-edit.png"), full_page=True)
            # проверка перезагрузкой
            page.reload()
            wait_app(page)
            page.wait_for_timeout(800)
            reloaded = " ".join(page.locator("body").inner_text().split())
            report["after_reload"] = reloaded[300:900]
            note("правка-проверка", f"после перезагрузки: {reloaded[420:700]}")
        else:
            note("правка", "поле для правки не найдено среди " + str(len(editables)))

        # --- ставки в её виде студии
        page.goto(BASE + "/projects")
        wait_app(page)
        projects = " ".join(page.locator("body").inner_text().split())
        report["projects_text"] = projects[300:1200]
        leaks = [w for w in ("140", "160", "₽", "руб", "ставка 1", "140 ₽") if w in projects]
        note("ставки", f"в тексте страницы проектов: {leaks}")
        page.screenshot(path=str(OUT / "r3-masha-projects.png"), full_page=True)

        report["notes"] = notes
        report["js_errors"] = errors
        (OUT / "report5.json").write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
        browser.close()

    print("\n===== ИТОГ =====")
    for line in notes:
        print(line)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())