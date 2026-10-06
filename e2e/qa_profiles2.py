"""Second QA pass: the questions the first pass left open.

Adds up: a timer run with a filled field, persistence of the timesheet edit,
what the CSV export hands a participant, whether the schedule shows a
colleague's tracked time, and whether writes outside her own data are refused.

Run:  PARATRACK_BASE=http://127.0.0.1:8895 .venv/bin/python e2e/qa_profiles2.py
"""
from __future__ import annotations

import json
import os
import time
from pathlib import Path

from playwright.sync_api import sync_playwright

BASE = os.environ.get("PARATRACK_BASE", "http://127.0.0.1:8895")
OWNER = "owner2@x.test"
MEMBER = "member2@x.test"
PW = "longenoughpw"
ROOT = "#paratrack-react-root"
OUT = Path(__file__).parent / "screenshots" / "qa-profiles"
OUT.mkdir(parents=True, exist_ok=True)

notes: list[str] = []
dump: dict[str, str] = {}
logout_hits: list[str] = []


def note(section: str, text: str) -> None:
    notes.append(f"[{section}] {text}")
    print(f"[{section}] {text}")


def shot(page, name: str) -> str:
    try:
        page.screenshot(path=str(OUT / f"{name}.png"), full_page=True)
        return name
    except Exception as exc:  # noqa: BLE001
        print(f"        shot failed {name}: {exc}")
        return ""


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


def keep(page, key: str, label: str = "") -> str:
    text = body(page)
    dump[key] = text
    if label:
        print(f"        [{key}] {text[:260]}")
    return text


def csrf(page) -> str:
    for c in page.context.cookies():
        if c.get("name") == "paratrack_csrf":
            return c.get("value") or ""
    return ""


def api(page, method: str, path: str, body_str: str = "") -> dict:
    try:
        r = getattr(page.request, method)(
            BASE + path,
            headers={"X-CSRF-Token": csrf(page), "Content-Type": "application/x-www-form-urlencoded"},
            data=body_str or None,
            max_redirects=0,
        )
        text = " ".join(r.text().split())[:260]
        note("запись-API", f"{method} {path} → {r.status} :: {text or '<пусто>'}")
        return {"status": r.status, "body": text}
    except Exception as exc:  # noqa: BLE001
        note("запись-API", f"{method} {path} → исключение: {str(exc)[:160]}")
        return {"status": None, "body": str(exc)[:160]}


def main() -> int:
    report: dict = {}
    with sync_playwright() as p:
        browser = p.chromium.launch()
        ctx_m = browser.new_context(viewport={"width": 1280, "height": 900}, locale="ru-RU", accept_downloads=True)
        ctx_o = browser.new_context(viewport={"width": 1280, "height": 900}, locale="ru-RU", accept_downloads=True)
        masha = ctx_m.new_page()
        owner = ctx_o.new_page()
        js_errors: list[str] = []
        for pg, who in ((masha, "member"), (owner, "owner")):
            pg.on("pageerror", lambda e, w=who: js_errors.append(f"{w}: {e}"))
            pg.on("response", lambda r, w=who: logout_hits.append(f"{time.strftime('%H:%M:%S')} {w} {r.request.method} {r.url} → {r.status}") if "/api/logout" in r.url else None)

        # ------------------------------------------------ Пётр: справочные данные
        print("\n== Пётр: чужой контекст для сравнения")
        sign_in(owner, OWNER)
        wait_app(owner)
        owner_nav = []
        for loc in owner.locator(f"{ROOT} aside a").all():
            try:
                owner_nav.append((" ".join(loc.inner_text().split()), loc.get_attribute("href") or ""))
            except Exception:
                pass
        seen, owner_unique = set(), []
        for text, href in owner_nav:
            if href and href not in seen:
                seen.add(href)
                owner_unique.append((text, href))
        report["owner_nav"] = owner_unique
        note("меню-петра", f"{len(owner_unique)} уникальных пунктов: " + ", ".join(f"{t}→{h}" for t, h in owner_unique))

        owner.goto(BASE + "/export")
        wait_app(owner)
        try:
            with owner.expect_download(timeout=15000) as dl:
                owner.get_by_role("button", name="Скачать CSV").click()
            path = OUT / "p5-owner-export.csv"
            dl.value.save_as(str(path))
            lines = path.read_text(encoding="utf-8", errors="replace").splitlines()
            report["owner_csv"] = lines[:12]
            note("CSV-петра", f"{len(lines)} строк, первые: {lines[:4]}")
        except Exception as exc:  # noqa: BLE001
            note("CSV-петра", f"не скачался: {str(exc)[:160]}")

        # чужой activity_id — из CSV владельца, если получилось
        foreign = None
        for line in report.get("owner_csv", [])[1:]:
            parts = line.split(",")
            if len(parts) > 1:
                foreign = {"id": parts[0], "name": parts[1]}
                break
        report["foreign_activity"] = foreign
        note("чужое-время", f"кандидат на чужую активность из CSV: {foreign}")

        # ------------------------------------------------ Маша
        print("\n== Маша: таймер с заполненным полем")
        sign_in(masha, MEMBER)
        wait_app(masha)

        # роль в каждом пространстве — из выпадающего списка
        ws = []
        masha.locator(f"{ROOT} button:has-text('Пространство')").first.click()
        masha.wait_for_timeout(400)
        ws = [" ".join(o.inner_text().split()) for o in masha.locator(f'{ROOT} [role="menuitem"]').all()]
        masha.keyboard.press("Escape")
        masha.wait_for_timeout(300)
        report["workspace_options"] = ws
        note("пространства", f"в списке: {ws}")

        masha.goto(BASE + "/")
        wait_app(masha)
        field = masha.locator(f'{ROOT} input[type="text"], {ROOT} input[placeholder]').first
        timer = {"clicks": 0}
        if field.count():
            field.click()
            field.fill("чтение ТЗ для Nordwind")
            timer["clicks"] = 1
            start = masha.get_by_role("button", name="Старт", exact=True).first
            start.click()
            timer["clicks"] = 2
            masha.wait_for_timeout(1500)
            timer["running_text"] = body(masha, 1400)
            dump["n1-running"] = timer["running_text"]
            shot(masha, "n1-masha-timer-running")
            note("таймер", f"кликов: {timer['clicks']} (1 — ввод, 2 — «Старт»); состояние: {timer['running_text'][200:600]}")
            btns = []
            for b in masha.locator(f"{ROOT} main button, {ROOT} main a").all():
                try:
                    label = " ".join(b.inner_text().split())
                    if label:
                        btns.append(label)
                except Exception:
                    pass
            report["running_controls"] = btns[:40]
            note("таймер-кнопки", f"{btns[:26]}")
            stop = masha.locator(f'{ROOT} main button:has-text("Стоп")').first
            if stop.count():
                stop.click()
                timer["clicks"] = 3
                masha.wait_for_timeout(1500)
                timer["after_stop"] = body(masha, 1200)
                dump["n2-stopped"] = timer["after_stop"]
                shot(masha, "n2-masha-timer-stopped")
                note("таймер-стоп", f"после стопа: {timer['after_stop'][200:700]}")
            else:
                note("таймер-стоп", "кнопка «Стоп» в основной области не найдена")
        else:
            note("таймер", "текстовое поле активности не найдено")
        report["timer"] = timer

        # ------------------------------------------- табель: сохранилось ли
        print("\n== Маша: табель после правок")
        masha.goto(BASE + "/timesheet")
        wait_app(masha)
        cells = masha.locator(f'{ROOT} input[type="number"]')
        values = []
        for i in range(min(cells.count(), 7)):
            try:
                values.append(cells.nth(i).input_value())
            except Exception:
                values.append("?")
        report["timesheet_values_after_reload"] = values
        keep(masha, "n3-timesheet-reload", label="табель после перезагрузки")
        note("табель", f"первые 7 ячеек после перезагрузки: {values}; всего полей: {cells.count()}")
        shot(masha, "n3-masha-timesheet")

        # ------------------------------------------------- свои цифры
        masha.goto(BASE + "/stats")
        wait_app(masha)
        report["stats_text"] = keep(masha, "n4-stats", label="статистика")
        shot(masha, "n4-masha-stats")

        # ------------------------------------------------- экспорт участницы
        print("\n== Маша: что отдаёт экспорт")
        masha.goto(BASE + "/export")
        wait_app(masha)
        try:
            with masha.expect_download(timeout=15000) as dl:
                masha.get_by_role("button", name="Скачать CSV").click()
            path = OUT / "n5-masha-export.csv"
            dl.value.save_as(str(path))
            lines = path.read_text(encoding="utf-8", errors="replace").splitlines()
            report["member_csv"] = lines
            note("CSV-маши", f"{len(lines)} строк: {lines[:8]}")
            petr_rows = [l for l in lines if "Пётр" in l or "owner2" in l]
            note("CSV-маши-чужие", f"строк с чужим именем/почтой: {len(petr_rows)}" + (f" → {petr_rows[:3]}" if petr_rows else ""))
        except Exception as exc:  # noqa: BLE001
            note("CSV-маши", f"не скачался: {str(exc)[:200]}")

        # ------------------------------------------------- расписание по проектам
        print("\n== Маша: расписание — видно ли чужое отработанное")
        masha.goto(BASE + "/schedule")
        wait_app(masha)
        options = []
        sel = masha.locator(f"{ROOT} select").first
        if sel.count():
            for i in range(sel.locator("option").count()):
                options.append(sel.locator("option").nth(i).inner_text())
        report["schedule_projects"] = options
        note("расписание", f"проекты в фильтре: {options}")
        for index, label in enumerate(options):
            if "Все" in label:
                continue
            try:
                sel.select_option(index=index)
                masha.wait_for_timeout(900)
                txt = body(masha, 1200)
                dump[f"n6-schedule-{index}"] = txt
                note("расписание-проект", f"[{label}] {txt[150:800]}")
                shot(masha, f"n6-masha-schedule-{index}")
            except Exception as exc:  # noqa: BLE001
                note("расписание-проект", f"[{label}] не открылся: {str(exc)[:120]}")

        # ------------------------------------------- записи вне своих данных
        print("\n== Маша: попытки записи (границы прав)")
        report["writes"] = {}
        report["writes"]["invite"] = api(masha, "POST", "/api/team/invites", f"csrf_token={csrf(masha)}")
        if foreign:
            fdate = "2019-03-04"
            report["writes"]["timesheet_foreign"] = api(
                masha, "POST", "/api/timesheet/cell",
                f"csrf_token={csrf(masha)}&activity_id={foreign['id']}&date={fdate}&minutes=17",
            )
            report["writes"]["schedule_foreign"] = api(
                masha, "POST", "/api/schedule/cell",
                f"csrf_token={csrf(masha)}&activity_id={foreign['id']}&date={fdate}&minutes=17",
            )
            report["writes"]["cleanup"] = api(
                masha, "POST", "/api/timesheet/row/clear",
                f"csrf_token={csrf(masha)}&activity_id={foreign['id']}",
            )
        report["writes"]["payroll"] = api(masha, "POST", "/api/payroll", f"csrf_token={csrf(masha)}&user_id=1&amount=100")
        report["writes"]["invoice"] = api(masha, "POST", "/api/invoices", f"csrf_token={csrf(masha)}&client=QA&total=100")
        report["writes"]["member_role"] = api(masha, "POST", "/api/team/members/1", f"csrf_token={csrf(masha)}&role=owner")
        report["writes"]["team_modules"] = api(masha, "POST", "/api/team/modules", f"csrf_token={csrf(masha)}&preset=studio")

        # ------------------------------------------- токены участницы
        print("\n== Маша: свои API-токены")
        masha.goto(BASE + "/settings/tokens")
        wait_app(masha)
        keep(masha, "n7-tokens", label="токены")
        note("токены", body(masha, 600)[150:600])
        shot(masha, "n7-masha-tokens")

        # ------------------------------------------- чужие цифры на дашборде
        print("\n== Маша: чужие цифры на экранах")
        for path, key, shot_name in [("/", "n8-home", "n8-masha-dashboard"), ("/projects", "n9-projects", "n9-masha-projects"), ("/timesheet", "n10-timesheet", "n10-masha-timesheet")]:
            masha.goto(BASE + path)
            wait_app(masha)
            txt = body(masha, 1500)
            dump[key] = txt
            marks = [w for w in ("Пётр", "owner2", "140", "160", "ставк", "₽", "Ставка") if w.lower() in txt.lower()]
            note("чужие-данные", f"{path}: маркеры {marks} :: {txt[150:600]}")
            shot(masha, shot_name)

        # ------------------------------------------- Пётр после всего
        print("\n== Пётр: состояние сессии")
        owner.goto(BASE + "/")
        wait_app(owner)
        alive = "/login" not in owner.url
        note("владелец", f"{'на дашборде' if alive else 'выброшен на ' + owner.url}")
        report["owner_alive"] = alive
        report["owner_text"] = body(owner, 600)
        shot(owner, "p5-owner-after-member-run")
        if alive:
            owner.goto(BASE + "/settings/audit")
            wait_app(owner)
            audit = body(owner, 1200)
            dump["p5-audit"] = audit
            note("журнал", audit[150:900])
            shot(owner, "p5-owner-audit")

        report["js_errors"] = js_errors[:20]
        report["logout_hits"] = logout_hits
        note("консоль", f"ошибок JS: {len(js_errors)}" + (f" → {js_errors[:3]}" if js_errors else ""))
        note("выходы", f"POST /api/logout из браузера: {len(logout_hits)}" + (f" → {logout_hits}" if logout_hits else ""))
        report["notes"] = notes
        report["dump"] = dump
        (OUT / "report2.json").write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
        browser.close()

    print("\n===== ЗАМЕТКИ =====")
    for line in notes:
        print(line)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())