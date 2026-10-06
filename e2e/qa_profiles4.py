"""Fourth QA pass: the boundary question — can a participant see someone
else's tracked time, and can she correct her own sessions?

/stats carries a person filter (`data.People`) and /graph takes a `person=`
parameter. Neither is behind `s.manage`, so this pass asks the server directly
through the member's session and reports what comes back.

Run:  PARATRACK_BASE=http://127.0.0.1:8895 .venv/bin/python e2e/qa_profiles4.py
"""
from __future__ import annotations

import json
import os
import re
from pathlib import Path

from playwright.sync_api import sync_playwright

BASE = os.environ.get("PARATRACK_BASE", "http://127.0.0.1:8895")
OWNER = "owner2@x.test"
MEMBER = "member2@x.test"
PW = "longenoughpw"
ROOT = "#paratrack-react-root"
OUT = Path(__file__).parent / "screenshots" / "qa-profiles"

notes: list[str] = []
dump: dict[str, str] = {}


def note(section: str, text: str) -> None:
    notes.append(f"[{section}] {text}")
    print(f"[{section}] {text}")


def shot(page, name: str) -> None:
    try:
        page.screenshot(path=str(OUT / f"{name}.png"), full_page=True)
    except Exception:
        pass


def sign_in(page, email: str) -> None:
    page.goto(BASE + "/login")
    page.fill("#email", email)
    page.fill("#password", PW)
    page.click('button[type="submit"]')
    page.wait_for_load_state("load")
    if "/login" in page.url:
        raise SystemExit(f"sign-in failed for {email}: still on {page.url}")


def wait_app(page) -> None:
    try:
        page.wait_for_selector(f"{ROOT} .app-boot", state="detached", timeout=15000)
    except Exception:
        pass


def body(page, limit: int = 900) -> str:
    try:
        return " ".join(page.locator("body").inner_text(timeout=5000).split())[:limit]
    except Exception as exc:  # noqa: BLE001
        return f"<no text: {exc}>"


def main() -> int:
    report: dict = {}
    with sync_playwright() as p:
        browser = p.chromium.launch()
        masha = browser.new_context(viewport={"width": 1280, "height": 900}, locale="ru-RU").new_page()
        owner = browser.new_context(viewport={"width": 1280, "height": 900}, locale="ru-RU").new_page()

        # --- Пётр: его идентификатор и его собственная статистика (эталон)
        sign_in(owner, OWNER)
        wait_app(owner)
        owner.goto(BASE + "/settings/members")
        wait_app(owner)
        html = owner.locator("body").inner_text()
        note("участники-петра", html[150:600])
        dump["p-members"] = html[:800]
        shot(owner, "q1-owner-members")

        ids = sorted({int(m) for m in re.findall(r"(?:user|member)[-_]?id[=\"\':\s]+(\d+)", owner.content(), re.I)})
        report["ids_from_owner_page"] = ids
        note("идентификаторы", f"на странице участников: {ids}")

        owner.goto(BASE + "/stats?period=week")
        wait_app(owner)
        owner_stats = body(owner, 1500)
        dump["p-stats-week"] = owner_stats
        person_select = owner.locator("#stats-person").count()
        report["owner_person_filter"] = person_select
        note("статистика-петра", f"фильтр по человеку: {person_select} :: {owner_stats[200:800]}")
        options = []
        if person_select:
            owner.locator("#stats-person").click()
            owner.wait_for_timeout(400)
            for opt in owner.locator('[role="option"]').all():
                options.append(" ".join(opt.inner_text().split()))
            owner.keyboard.press("Escape")
        report["owner_person_options"] = options
        note("фильтр-опции", f"{options}")

        # эталон: сколько времени у Петра за неделю (через его собственный фильтр)
        for value in ("1", "2", "3"):
            owner.goto(BASE + f"/stats?period=week&person={value}")
            wait_app(owner)
            txt = body(owner, 900)
            if "Пётр" in txt or "мин" in txt:
                note("эталон-петр", f"person={value} :: {txt[200:700]}")
                dump[f"p-stats-person-{value}"] = txt
                break

        # --- Маша: попытка увидеть чужое время
        print("\n== Маша: фильтр по человеку")
        sign_in(masha, MEMBER)
        wait_app(masha)
        masha.locator(f'{ROOT} button:has-text("Пространство")').first.click()
        masha.wait_for_timeout(400)
        masha.locator(f'{ROOT} [role="menuitem"]:has-text("Пётр")').first.click()
        masha.wait_for_load_state("load")
        wait_app(masha)
        masha.wait_for_timeout(500)

        masha.goto(BASE + "/stats")
        wait_app(masha)
        report["member_person_filter"] = masha.locator("#stats-person").count()
        note("фильтр-маши", f"фильтр по человеку на /stats: {report['member_person_filter']}")
        shot(masha, "q2-masha-stats")

        for value in ("1", "2", "3"):
            masha.goto(BASE + f"/stats?period=week&person={value}")
            wait_app(masha)
            txt = body(masha, 1200)
            dump[f"q-stats-person-{value}"] = txt
            note("чужие-через-фильтр", f"person={value} :: {txt[200:700]}")
            shot(masha, f"q3-masha-stats-person-{value}")

        for value in ("1", "2", "3"):
            masha.goto(BASE + f"/graph?person={value}")
            wait_app(masha)
            txt = body(masha, 900)
            dump[f"q-graph-person-{value}"] = txt
            note("чужие-через-график", f"person={value} :: {txt[200:600]}")
            shot(masha, f"q4-masha-graph-person-{value}")

        # --- Маша: свои сессии за неделю и правка
        print("\n== Маша: свои сессии за неделю")
        masha.goto(BASE + "/stats?period=week")
        wait_app(masha)
        mine = body(masha, 1600)
        dump["q5-stats-week"] = mine
        note("свои-сессии", mine[200:1000])
        shot(masha, "q5-masha-stats-week")
        report["member_session_rows"] = [" ".join(r.inner_text().split())[:120] for r in masha.locator(f"{ROOT} table tbody tr").all()][:10]
        note("свои-строки", f"{report['member_session_rows']}")
        buttons = [" ".join(b.inner_text().split()) for b in masha.locator(f"{ROOT} main button").all()]
        note("кнопки-статистики", f"{[b for b in buttons if b][:24]}")

        # --- Маша: страница отказа и выход из неё
        print("\n== Маша: страница отказа")
        masha.goto(BASE + "/settings/members")
        wait_app(masha)
        note("отказ", f"{body(masha, 400)}")
        back = masha.get_by_role("link", name="Вернуться к обзору")
        report["deny_back_link"] = back.count()
        if back.count():
            back.click()
            masha.wait_for_load_state("load")
            wait_app(masha)
            note("отказ-возврат", f"клик сработал, адрес: {masha.url.replace(BASE, '')}")
        shot(masha, "q6-masha-deny")

        # --- Маша: формат дат на экспорте
        masha.goto(BASE + "/export")
        wait_app(masha)
        placeholders = []
        for i in range(masha.locator(f'{ROOT} input[type="date"], {ROOT} input[type="text"]').count()):
            try:
                placeholders.append(masha.locator(f'{ROOT} input[type="date"], {ROOT} input[type="text"]').nth(i).get_attribute("placeholder") or "")
            except Exception:
                pass
        note("даты", f"подписи полей даты: {placeholders}")
        shot(masha, "q7-masha-export")

        # --- Маша: график/цели — видно ли коллег
        masha.goto(BASE + "/goals")
        wait_app(masha)
        note("цели", f"{body(masha, 700)[200:700]}")
        shot(masha, "q8-masha-goals")

        report["notes"] = notes
        report["dump"] = dump
        (OUT / "report4.json").write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
        browser.close()

    print("\n===== ЗАМЕТКИ =====")
    for line in notes:
        print(line)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())