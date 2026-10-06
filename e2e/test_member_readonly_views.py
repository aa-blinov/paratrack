"""A member's read-only views say what they can do and where the time is.

Covers the three screens a studio member without management rights reads:
the schedule stops promising edits it cannot make, an empty stats period
names the periods that do hold time, and the timesheet says what "Без
проекта" costs. Runs against the built UI with deterministic page data —
no server, account, or database.

Run after `cd web && npm run build:react`: python3 e2e/test_member_readonly_views.py
"""
import asyncio
import json
from urllib.parse import urlparse
from playwright.async_api import async_playwright
from test_react_navigation import document, STATIC

WEEK = [{'Index': i, 'Label': 'Пн Вт Ср Чт Пт Сб Вс'.split()[i], 'Date': str(5 + i),
         'ISO': f'2026-10-{5 + i:02d}', 'IsToday': i == 1} for i in range(7)]


def boot(path, data, can_manage):
    """Wrap page data into the shell document the built UI boots from."""
    html = document(path)
    start = html.index('<div id="react-page-data" hidden>') + len('<div id="react-page-data" hidden>')
    end = html.index('</div>', start)
    payload = json.loads(html[start:end])
    payload['data'] = data
    payload['shell'].update(active=path.strip('/'), requestPath=path, canManage=can_manage, title='Проверка')
    return html[:start] + json.dumps(payload, ensure_ascii=False) + html[end:]


def schedule_document(can_manage):
    cells = [{'Index': d['Index'], 'ISO': d['ISO'], 'Min': 120 if d['Index'] == 1 else 0,
              'Total': '2 ч' if d['Index'] == 1 else '0 мин', 'IsToday': d['IsToday']} for d in WEEK]
    return boot('/schedule', {
        'Lang': 'ru', 'Active': 'schedule', 'ScheduleReact': True, 'CSRFToken': 'test', 'CanManage': can_manage,
        'WeekLabel': '5 окт – 11 окт', 'PrevWeek': '2026-09-28', 'NextWeek': '2026-10-12',
        'ThisWeek': '2026-10-05', 'ProjectID': 7, 'Days': WEEK,
        'Projects': [{'ID': 7, 'Slug': 'nordwind', 'Name': 'Сайт для Nordwind', 'Color': '#7c3aed',
                      'Archived': False, 'Billable': True}],
        'Rows': [{'UserID': 2, 'UserName': 'Маша Участница', 'Capacity': 480, 'Cells': cells,
                  'Total': '2 ч', 'TotalMin': 120, 'LoadPct': 3},
                 {'UserID': 1, 'UserName': 'Пётр Владелец', 'Capacity': 480, 'Cells': cells,
                  'Total': '2 ч', 'TotalMin': 120, 'LoadPct': 3}],
        'GrandTotal': '4 ч', 'GrandMin': 240,
    }, can_manage)


def stats_document():
    return boot('/stats', {
        'Lang': 'ru', 'Active': 'stats', 'CSRFToken': 'test', 'CanManage': False, 'Mods': {'graph': True},
        'Period': {'Start': '2026-10-06T00:00:00+03:00', 'End': '2026-10-06T16:00:00+03:00', 'Label': 'today'},
        'Aggregated': [], 'ByProject': [], 'Projects': [], 'ProjectFilter': '', 'People': [],
        'PersonFilter': 0, 'Sessions': [], 'Total': '0 мин', 'SessionCount': 0, 'SessionsCut': False,
        'MeID': 2, 'ShowAllURL': '/stats?log=all', 'TagFilter': '', 'AllTagNames': [], 'SavedReports': [],
        'Elsewhere': [{'Label': 'yesterday', 'Count': 1, 'Total': '45 мин'},
                      {'Label': 'week', 'Count': 3, 'Total': '2 ч 30 мин'}],
    }, False)


def timesheet_document(linked=False):
    def row(activity_id, name, project_id, minutes):
        secs = [0, minutes * 60, 0, 0, 0, 0, 0]
        return {'ActivityID': activity_id, 'ActivityName': name, 'ProjectID': project_id, 'Color': '#8b5cf6',
                'Cells': [{'Index': d['Index'], 'ISO': d['ISO'], 'Secs': secs[d['Index']],
                           'Min': secs[d['Index']] // 60, 'IsToday': d['IsToday']} for d in WEEK],
                'RowTotal': sum(secs), 'RowTotalLabel': f'{minutes} мин'}
    days = [dict(d, Secs=2700, Min=45, Total='45 мин') for d in WEEK]
    names = {'3': 'Внутренние задачи'}
    if linked:
        names['0'] = 'Внутренние задачи'
    return boot('/timesheet', {
        'Lang': 'ru', 'Active': 'timesheet', 'TimesheetReact': True, 'CSRFToken': 'test', 'Days': days,
        'Rows': [row(4, 'Вёрстка главной', 0, 45), row(1, 'Правка макета', 3, 30)],
        'ProjectNames': names, 'DayTotalLabels': ['0 мин', '1 ч 15 мин'] + ['0 мин'] * 5,
        'GrandTotal': 4500, 'GrandTotalLabel': '1 ч 15 мин', 'Others': [], 'Added': [],
        'DateISO': '2026-10-05', 'PrevWeek': '2026-09-28', 'NextWeek': '2026-10-12',
        'WeekLabel': '5 окт – 11 окт',
    }, False)


async def open_page(browser, html, path, width=1440):
    context = await browser.new_context(viewport={'width': width, 'height': 900})
    page = await context.new_page()
    errors = []
    page.on('pageerror', lambda error: errors.append(str(error)))

    async def respond(route):
        served = urlparse(route.request.url).path
        if served.startswith('/static/'):
            await route.fulfill(path=str(STATIC / served.removeprefix('/static/')))
        else:
            await route.fulfill(content_type='text/html; charset=utf-8', body=html)
    await context.route('**/*', respond)
    await page.goto(f'http://paratrack.test{path}')
    return context, page, errors


async def check_schedule_member(browser):
    context, page, errors = await open_page(browser, schedule_document(False), '/schedule')
    await page.locator('table.week-grid').wait_for()
    assert await page.get_by_role('spinbutton').count() == 0, 'A member must see the plan, not editable cells'
    assert await page.locator('[data-readonly-note]').count() == 1, \
        'The member must be told who owns the plan and who may change it'
    assert '/settings/members' not in await page.locator('main').inner_html(), \
        'No link into the members settings a member cannot open'
    assert not errors, errors
    await page.screenshot(path='/tmp/paratrack-member-schedule.png', full_page=True)
    await context.close()


async def check_schedule_owner(browser):
    context, page, errors = await open_page(browser, schedule_document(True), '/schedule')
    await page.locator('table.week-grid').wait_for()
    assert await page.get_by_role('spinbutton').count() == 14, 'An owner still edits the plan'
    assert await page.locator('[data-readonly-note]').count() == 0, \
        'An owner is not told to ask someone else for the plan'
    assert not errors, errors
    await context.close()


async def check_stats_empty_period(browser):
    context, page, errors = await open_page(browser, stats_document(), '/stats')
    await page.get_by_role('link', name='На обзор').wait_for()
    hint = page.locator('[data-elsewhere]')
    assert await hint.count() == 1, 'An empty period must point at the periods that do hold time'
    links = hint.locator('a')
    assert await links.count() == 2, 'One link per switcher period that holds time'
    hrefs = await links.evaluate_all('items => items.map(link => link.getAttribute("href"))')
    assert any('period=yesterday' in href for href in hrefs), hrefs
    assert any('period=week' in href for href in hrefs), hrefs
    # The totals come from the server, so they hold whatever the dictionary says.
    hint_text = await hint.inner_text()
    assert '45 мин' in hint_text and '2 ч 30 мин' in hint_text, hint_text
    assert not errors, errors
    await page.screenshot(path='/tmp/paratrack-member-stats.png', full_page=True)
    await context.close()


async def check_timesheet_projectless_row(browser):
    context, page, errors = await open_page(browser, timesheet_document(), '/timesheet')
    await page.locator('#ts-body').wait_for()
    assert await page.locator('#ts-row-4 th').inner_text() == 'Вёрстка главной\nБез проекта', \
        'An activity with no project keeps the label it always had'
    assert await page.locator('#ts-row-1 th').inner_text() == 'Правка макета\nВнутренние задачи', \
        'A row tied to a project names that project'
    assert await page.locator('[data-no-project-hint]').count() == 1, \
        'A projectless activity must say where its time does not land'
    assert not errors, errors
    await page.screenshot(path='/tmp/paratrack-member-timesheet.png', full_page=True)
    await context.close()


async def check_timesheet_all_rows_linked(browser):
    context, page, errors = await open_page(browser, timesheet_document(linked=True), '/timesheet')
    await page.locator('#ts-body').wait_for()
    assert await page.locator('[data-no-project-hint]').count() == 0, \
        'The note is about projectless rows and stays off when there are none'
    assert not errors, errors
    await context.close()


async def main():
    async with async_playwright() as p:
        browser = await p.chromium.launch()
        await check_schedule_member(browser)
        print('PASS schedule as member: read-only grid, owner named, no link into manager-only settings')
        await check_schedule_owner(browser)
        print('PASS schedule as owner: editable grid, no read-only note')
        await check_stats_empty_period(browser)
        print('PASS stats: empty today offers yesterday and this week as one-click links')
        await check_timesheet_projectless_row(browser)
        print('PASS timesheet: the "Без проекта" note appears and tied rows still name their project')
        await check_timesheet_all_rows_linked(browser)
        print('PASS timesheet: the note is absent when every row has a project')
        await browser.close()


if __name__ == '__main__':
    asyncio.run(main())
