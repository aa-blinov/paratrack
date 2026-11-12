"""Regressions the React screens had, checked on fixed pages without a server.

- A slow reply for a week, filter or client already left must not replace the screen.
- The search box keeps the keyboard inside it, closes on Escape and gives the focus back.
- A chart whose script fails to load says so instead of showing a skeleton forever.
- A goal's day is the workspace's day, whatever zone the browser is in.

Run after `cd web && npm run build:react`: python3 e2e/test_screen_regressions.py
No server, account, or database is required: every response is fixed here.
"""
import asyncio
import json
from pathlib import Path
from urllib.parse import parse_qs, urlparse

from playwright.async_api import async_playwright

STATIC = Path(__file__).resolve().parents[1] / "internal/web/static"
USER = {"id": 1, "name": "Тест", "email": "test@example.test"}
CALENDAR_DATA = {"Title": "Календарь", "Active": "calendar", "CalendarReact": True, "Zone": "UTC",
                 "Today": "2026-11-12", "NowLocal": "2026-11-12T10:00:00", "WeekStartsSunday": False,
                 "SyncProviders": [], "UserID": 1, "CanManage": False}
GRAPH_CHART = {"hasData": True, "hours": ["09", "10"], "series": [{"name": "Работа", "color": "#6366f1", "data": [30, 45], "total": 75}]}
GRAPH_DATA = {"Title": "По часам", "Active": "graph", "GraphReact": True,
              "Period": {"Label": "today", "Start": "2026-11-12T00:00:00Z", "End": "2026-11-13T00:00:00Z"},
              "PeriodStartInput": "", "PeriodEndInput": "",
              "Chart": {**GRAPH_CHART, "totalLabel": "1 ч 15 мин", "legend": [{"name": "Работа", "color": "#6366f1"}]},
              "ChartJSON": json.dumps(GRAPH_CHART), "ProjectFilter": "", "ProjectName": "", "TagFilter": "",
              "PersonFilter": 0, "PersonName": "", "RunningCount": 0, "RunningTotal": ""}
GOALS_DATA = {"Title": "Цели", "Active": "goals", "GoalsReact": True, "CanManage": True,
              "Activities": [{"ID": 1, "Name": "Чтение", "Color": "#6366f1", "ProjectID": 0, "Archived": False}],
              "Goals": []}
SEARCH = {"groups": [{"label": "Клиенты", "items": [
    {"title": "Альфа", "detail": "", "href": "/clients/1"},
    {"title": "Бета", "detail": "", "href": "/clients/2"}]}]}


def page_html(path, active, title, data):
    data = {"Lang": "ru", "CSRFToken": "test", **data}
    shell = {"title": title, "active": active, "lang": "ru", "user": USER, "team": {"id": 1, "name": "Моя команда"},
             "userTeams": [], "requestPath": path, "csrfToken": "test", "canManage": True, "mods": None, "tabs": []}
    payload = json.dumps({"data": data, "shell": shell})
    return f'''<!doctype html><html lang="ru" data-theme="paratrack-light"><head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width,initial-scale=1">
    <title>{title}</title>
    <link rel="stylesheet" href="/static/ui/app.css">
    <script type="module" blocking="render" src="/static/ui/app.js"></script></head><body data-i18n-undo="Отменить">
    <div id="react-page-data" hidden>{payload}</div><div id="paratrack-react-root"><div class="app-boot"><h1>Загрузка…</h1></div></div>
    </body></html>'''


def event(event_id, title, day):
    return {"id": event_id, "title": title, "starts_at": f"{day}T10:00:00Z", "ends_at": f"{day}T11:00:00Z",
            "location": "", "notes": "", "project_id": 0, "client_id": 0, "shared": True, "sync_push": False,
            "imported": False, "all_day": False, "participant_ids": [], "created_by": 1}


def json_reply(body):
    async def reply(route):
        await route.fulfill(content_type="application/json", body=json.dumps(body))
    return reply


async def open_page(browser, path, html, api, **options):
    """Opens one page. api maps a request path to a route handler; static files come from the build."""
    context = await browser.new_context(viewport={"width": 1280, "height": 900}, **options)
    page = await context.new_page()
    errors = []
    page.on("pageerror", lambda error: errors.append(str(error)))

    async def respond(route):
        request_path = urlparse(route.request.url).path
        if request_path in api:
            await api[request_path](route)
        elif request_path.startswith("/static/"):
            file = STATIC / request_path.removeprefix("/static/")
            if file.is_file():
                await route.fulfill(path=str(file))
            else:
                await route.fulfill(status=404)
        elif request_path == path:
            await route.fulfill(content_type="text/html", body=html)
        else:
            await route.fulfill(status=404)

    await context.route("**/*", respond)
    await page.goto(f"http://paratrack.test{path}")
    return context, page, errors


async def check_calendar_drops_a_late_reply(browser):
    release_first_week = asyncio.Event()
    first_week_asked = asyncio.Event()

    async def events(route):
        start = parse_qs(urlparse(route.request.url).query)["from"][0]
        if start == "2026-11-09":  # the week of the first load; its reply is held back
            first_week_asked.set()
            await release_first_week.wait()
            body = {"events": [event(1, "СТАРАЯ НЕДЕЛЯ", "2026-11-10")]}
        else:
            body = {"events": [event(2, "НОВАЯ НЕДЕЛЯ", "2026-11-17")]}
        await route.fulfill(content_type="application/json", body=json.dumps(body))

    html = page_html("/calendar", "calendar", "Календарь", CALENDAR_DATA)
    context, page, errors = await open_page(browser, "/calendar", html, {"/api/calendar/events": events})
    await asyncio.wait_for(first_week_asked.wait(), 15)
    await page.get_by_role("button", name="Следующая неделя").click()
    await page.get_by_text("НОВАЯ НЕДЕЛЯ").first.wait_for(timeout=10000)
    release_first_week.set()
    await page.wait_for_timeout(600)
    assert await page.get_by_text("НОВАЯ НЕДЕЛЯ").count() >= 1, "the week on screen lost its events to the old reply"
    assert await page.get_by_text("СТАРАЯ НЕДЕЛЯ").count() == 0, "the old week's reply replaced the week on screen"
    assert not errors, errors
    await context.close()
    print("PASS calendar: a late reply for the week already left is dropped")


async def check_search_keeps_focus(browser):
    html = page_html("/calendar", "calendar", "Календарь", CALENDAR_DATA)
    api = {"/api/calendar/events": json_reply({"events": []}), "/api/search": json_reply(SEARCH)}
    context, page, errors = await open_page(browser, "/calendar", html, api)
    opener = page.get_by_role("button", name="Эта неделя")
    await opener.wait_for()
    await opener.focus()
    await page.keyboard.press("/")
    dialog = page.locator('[role="dialog"]')
    await dialog.wait_for()
    await page.locator('[role="dialog"] input').fill("ал")
    await page.get_by_role("link", name="Альфа").wait_for()

    inside = "el => !!el.closest('[role=\"dialog\"]')"
    leaks = []
    for _ in range(8):
        await page.keyboard.press("Tab")
        if not await page.evaluate(f"({inside})(document.activeElement)"):
            leaks.append(await page.evaluate("document.activeElement.outerHTML.slice(0, 80)"))
    for _ in range(8):
        await page.keyboard.press("Shift+Tab")
        if not await page.evaluate(f"({inside})(document.activeElement)"):
            leaks.append(await page.evaluate("document.activeElement.outerHTML.slice(0, 80)"))
    assert not leaks, f"Tab left the search box: {leaks}"

    await page.keyboard.press("Escape")
    await dialog.wait_for(state="detached", timeout=3000)
    focused = await page.evaluate("document.activeElement && document.activeElement.textContent.trim()")
    assert focused == "Эта неделя", f"focus should return to the opener, got {focused!r}"
    assert not errors, errors
    await context.close()
    print("PASS search: focus stays inside, Escape closes, focus returns to the opener")


async def check_graph_reports_a_failed_chart(browser):
    async def abort(route):
        await route.abort()

    for mode in ("abort", "ok"):
        api = {"/static/js/echarts.min.js": abort} if mode == "abort" else {}
        html = page_html("/graph", "graph", "По часам", GRAPH_DATA)
        context, page, errors = await open_page(browser, "/graph", html, api)
        if mode == "abort":
            alert = page.locator('[role="alert"]')
            await alert.wait_for(timeout=10000)
            assert await alert.inner_text() == "Что-то пошло не так. Попробуйте ещё раз.", "the failed chart must say so"
            assert await page.locator("#echart-skeleton").count() == 0, "the skeleton must not stay after a failed load"
        else:
            await page.wait_for_function("() => !document.querySelector('#echart-skeleton')", timeout=10000)
            assert await page.locator('[role="alert"]').count() == 0
            assert await page.evaluate("() => !!document.querySelector('#echart-canvas canvas')"), "the chart should draw"
        assert not errors, errors
        await context.close()
    print("PASS graph: a failed ECharts load says so, a good load draws")


async def check_goal_dates_follow_the_workspace_zone(browser):
    # The workspace is in Tokyo (+09:00): its day runs from 00:00 there, which is 15:00 UTC the day before.
    # The browser is in Los Angeles, where that instant is still the 11th.
    progress = {"progress": [{"goal": {"id": 1, "activity_id": 1, "period": "daily", "target_minutes": 120},
                              "activity_name": "Чтение", "achieved_minutes": 30, "percent_complete": 25,
                              "period_start": "2026-11-12T00:00:00+09:00", "period_end": "2026-11-13T00:00:00+09:00"}]}

    async def save(route):
        await route.fulfill(content_type="application/json", body="{}")

    html = page_html("/goals", "goals", "Цели", GOALS_DATA)
    api = {"/api/goals": save, "/api/goals/progress": json_reply(progress)}
    context, page, errors = await open_page(browser, "/goals", html, api, timezone_id="America/Los_Angeles")
    await page.locator("#goal-activity").fill("Чтение")
    await page.locator("#goal-minutes").fill("120")
    await page.get_by_role("button", name="Задать цель").click()
    await page.get_by_text("(12 нояб. – 12 нояб.)").wait_for(timeout=10000)
    assert not errors, errors
    await context.close()
    print("PASS goals: the period reads the workspace day in a browser west of UTC")


async def main():
    async with async_playwright() as playwright:
        browser = await playwright.chromium.launch()
        await check_calendar_drops_a_late_reply(browser)
        await check_search_keeps_focus(browser)
        await check_graph_reports_a_failed_chart(browser)
        await check_goal_dates_follow_the_workspace_zone(browser)
        await browser.close()


if __name__ == "__main__":
    asyncio.run(main())
