"""Timesheet cell undo and row clear.

Both flows are checked against the built UI with deterministic server
answers, so the assertions are about what the person sees: the undo link next
to the status, and a row that needs confirmation before it disappears.
"""
import asyncio
import json
from urllib.parse import urlparse

from playwright.async_api import async_playwright

from test_react_navigation import document, STATIC

DAYS = [{"ISO": f"2026-10-{5 + index:02d}", "Label": str(index + 1), "Date": str(5 + index), "IsToday": index == 0} for index in range(7)]


def timesheet_document(minutes=45):
    rows = [{
        "ActivityID": activity, "ActivityName": name, "ProjectID": activity, "Color": "#123456",
        "RowTotalLabel": f"{minutes} мин",
        "Cells": [
            {"Index": cell, "ISO": DAYS[cell]["ISO"], "Min": minutes if cell == 0 else 0,
             "Secs": minutes * 60 if cell == 0 else 0, "Total": f"{minutes} мин" if cell == 0 else "0 мин",
             "IsToday": cell == 0}
            for cell in range(7)
        ],
    } for activity, name in ((1, "Разработка"), (2, "Сопровождение"))]
    boot = {
        "data": {"Lang": "ru", "Active": "timesheet", "TimesheetReact": True, "CSRFToken": "test", "Rows": rows,
                 "ProjectNames": {"1": "Первый проект", "2": "Второй проект"}, "Days": DAYS,
                 "DayTotalLabels": [f"{minutes} мин"] + ["0 мин"] * 6, "Others": [], "Added": [],
                 "DateISO": "2026-10-05", "WeekLabel": "5–11 октября", "PrevWeek": "2026-09-28",
                 "NextWeek": "2026-10-12", "GrandTotal": minutes * 60, "GrandTotalLabel": f"{minutes} мин"},
        "shell": {"active": "timesheet", "requestPath": "/timesheet", "title": "Табель"},
    }
    return f'''<!doctype html><html lang="ru" data-theme="paratrack-light"><head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width,initial-scale=1">
    <title>Табель</title><link rel="stylesheet" href="/static/css/paratrack.css">
    <link rel="stylesheet" href="/static/ui/app.css">
    <script type="module" blocking="render" src="/static/ui/app.js"></script></head><body data-i18n-undo="Отменить">
    <div id="react-page-data" hidden>{json.dumps(boot, ensure_ascii=False)}</div>
    <div id="paratrack-react-root"><div class="app-boot"><h1>Загрузка…</h1></div></div>
    </body></html>'''


def row_response(activity_id=1, minutes=0, error=None):
    body = {
        "activityId": activity_id, "activityName": "Разработка" if activity_id == 1 else "Сопровождение", "color": "#123456",
        "cells": [{"iso": DAYS[cell]["ISO"], "secs": minutes * 60 if cell == 0 else 0,
                   "min": minutes if cell == 0 else 0, "total": f"{minutes} мин" if cell == 0 else "0 мин"} for cell in range(7)],
        "rowTotal": minutes * 60, "rowTotalLabel": f"{minutes} мин" if minutes else "0 мин",
        "dayTotals": [{"secs": minutes * 60 if cell == 0 else 0, "total": f"{minutes} мин" if cell == 0 else "0 мин"} for cell in range(7)],
        "grandTotal": minutes * 60, "grandTotalLabel": f"{minutes} мин" if minutes else "0 мин",
    }
    if error:
        body["error"] = error
    return body


async def check_undo(browser, width):
    context = await browser.new_context(viewport={"width": width, "height": 900})
    page = await context.new_page()
    errors = []
    page.on("pageerror", lambda error: errors.append(str(error)))
    posts = []

    async def respond(route):
        path = urlparse(route.request.url).path
        if path.startswith("/static/"):
            await route.fulfill(path=str(STATIC / path.removeprefix("/static/")))
        elif path == "/api/timesheet/cell":
            fields = dict(pair.split("=", 1) for pair in route.request.post_data.split("&"))
            posts.append((fields["activity_id"], fields["date"], fields["minutes"]))
            if fields["minutes"] == "60":
                await route.fulfill(status=500, body="Сеть недоступна", content_type="text/plain; charset=utf-8")
                return
            await route.fulfill(json=row_response(activity_id=int(fields["activity_id"]), minutes=int(fields["minutes"])))
        else:
            await route.fulfill(content_type="text/html; charset=utf-8", body=timesheet_document())
    await context.route("**/*", respond)
    await page.goto("http://paratrack.test/timesheet")
    await page.locator("#ts-body").wait_for()

    first = page.get_by_role("spinbutton", name="Разработка — Первый проект — 2026-10-05", exact=True)
    second = page.get_by_role("spinbutton", name="Разработка — Первый проект — 2026-10-06", exact=True)

    # A wrong number is undoable, and undo restores the minutes it had.
    await first.fill("70")
    await first.press("Enter")
    await page.get_by_role("status").filter(has_text="Сохранено: Разработка, 2026-10-05.").wait_for()
    assert await first.input_value() == "70", posts
    undo = page.get_by_role("button", name="Отменить", exact=True)
    assert "Разработка" in await undo.get_attribute("title"), "Undo names the cell it will restore"
    await undo.click()
    await page.get_by_role("status").filter(has_text="Возвращено: Разработка, 2026-10-05.").wait_for()
    assert await first.input_value() == "45", posts
    assert posts[-1] == ("1", "2026-10-05", "45"), posts

    # Undo follows the most recent edit, not the cell it happened to start from.
    await second.fill("90")
    await second.press("Enter")
    await page.get_by_role("status").filter(has_text="Сохранено: Разработка, 2026-10-06.").wait_for()
    assert await page.get_by_role("button", name="Отменить", exact=True).count() == 1
    await page.get_by_role("button", name="Отменить", exact=True).click()
    await page.get_by_role("status").filter(has_text="Возвращено: Разработка, 2026-10-06.").wait_for()
    assert posts[-1] == ("1", "2026-10-06", "0"), f"empty day must be rewritten, not skipped: {posts}"

    # A successful save stays undoable when the next write fails.
    await second.fill("90")
    await second.press("Enter")
    await page.get_by_role("status").filter(has_text="Сохранено: Разработка, 2026-10-06.").wait_for()
    await first.fill("60")
    await first.press("Enter")
    await page.get_by_role("alert").filter(has_text="Сеть недоступна").wait_for()
    assert await first.input_value() == "60", "a failed save keeps the typed minutes"
    undo = page.get_by_role("button", name="Отменить", exact=True)
    assert "2026-10-06" in await undo.get_attribute("title"), f"undo must still name the last saved cell: {await undo.get_attribute('title')}"
    await undo.click()
    await page.get_by_role("status").filter(has_text="Возвращено: Разработка, 2026-10-06.").wait_for()

    assert not errors, errors
    assert await page.evaluate("document.documentElement.scrollWidth <= innerWidth")
    await page.screenshot(path=f"/tmp/paratrack-timesheet-undo-{width}.png", full_page=True)
    print(f"PASS {width}px: undo restores previous minutes, follows the last edit, stays available after a failed write")
    await context.close()


async def check_row_clear(browser, width):
    context = await browser.new_context(viewport={"width": width, "height": 900})
    page = await context.new_page()
    errors = []
    page.on("pageerror", lambda error: errors.append(str(error)))
    clears = []

    async def respond(route):
        path = urlparse(route.request.url).path
        if path.startswith("/static/"):
            await route.fulfill(path=str(STATIC / path.removeprefix("/static/")))
        elif path == "/api/timesheet/row/clear":
            clears.append(route.request.post_data)
            if len(clears) == 1:
                await route.fulfill(json=row_response(activity_id=1, minutes=0))
            else:
                await route.fulfill(json=row_response(activity_id=1, minutes=0, error="Это время уже в выставленном счёте."))
        else:
            await route.fulfill(content_type="text/html; charset=utf-8", body=timesheet_document())
    await context.route("**/*", respond)
    await page.goto("http://paratrack.test/timesheet")
    await page.locator("#ts-body").wait_for()

    first_row = page.locator("#ts-row-1")
    assert await first_row.get_by_role("spinbutton").first.input_value() == "45"

    # Cancelling asks the same dialog and writes nothing.
    await first_row.get_by_role("button", name="Очистить строку «Разработка»", exact=True).click()
    dialog = page.get_by_role("alertdialog")
    await dialog.wait_for()
    await dialog.get_by_role("button", name="Отмена", exact=True).click()
    await dialog.wait_for(state="hidden")
    assert not clears, clears
    assert await first_row.get_by_role("spinbutton").first.input_value() == "45"

    # Confirming empties the row and says so.
    await first_row.get_by_role("button", name="Очистить строку «Разработка»", exact=True).click()
    await dialog.get_by_role("button", name="Подтвердить", exact=True).click()
    await page.get_by_role("status").filter(has_text="Строка очищена: Разработка.").wait_for()
    assert clears and "activity_id=1" in clears[0] and "date=2026-10-05" in clears[0], clears
    assert await first_row.get_by_role("spinbutton").first.input_value() == "", "cleared cell shows the dash placeholder"
    assert await first_row.get_by_text("0 мин").count() >= 1
    assert await page.get_by_role("button", name="Отменить", exact=True).count() == 0, "an emptied row has nothing to undo"

    # A refusal is reported, not swallowed.
    await first_row.get_by_role("button", name="Очистить строку «Разработка»", exact=True).click()
    await dialog.get_by_role("button", name="Подтвердить", exact=True).click()
    await page.get_by_role("alert").filter(has_text="Это время уже в выставленном счёте.").wait_for()

    assert not errors, errors
    assert await page.evaluate("document.documentElement.scrollWidth <= innerWidth")
    await page.screenshot(path=f"/tmp/paratrack-timesheet-clear-{width}.png", full_page=True)
    print(f"PASS {width}px: row clear asks, cancels silently, empties the row, reports a refusal")
    await context.close()


async def main():
    async with async_playwright() as playwright:
        browser = await playwright.chromium.launch()
        for width in [1440, 390]:
            await check_undo(browser, width)
            await check_row_clear(browser, width)
        await browser.close()


if __name__ == "__main__":
    asyncio.run(main())