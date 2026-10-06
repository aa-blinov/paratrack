"""A link the reader can follow, and one home per object.

The navigation map was audited by crawling every screen of a live stand. Two
dead links turned up for a studio member, three objects were reachable from the
wrong place, and saved reports had no screen of their own. This locks the
repairs: nothing links where the role is refused, an entity row leads to that
entity, and the saved-report list lives on /reports only.

Runs against the built UI with deterministic page data — no server, account, or
database.

Run after `cd web && npm run build`: python3 e2e/test_navigation_map_by_role.py
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


def projects_document(can_manage, projects=None):
    rows = projects if projects is not None else []
    return boot('/projects', {
        'Lang': 'ru', 'Active': 'projects', 'CanManage': can_manage, 'ShowArchived': False,
        'Flash': '', 'FlashOK': True, 'Projects': rows,
    }, can_manage)


def help_document(can_manage):
    return boot('/help', {'Lang': 'ru', 'Active': 'help', 'HelpReact': True, 'CanManage': can_manage}, can_manage)


def reports_document(reports):
    return boot('/reports', {
        'Lang': 'ru', 'Active': 'reports', 'ReportsReact': True, 'CanManage': True, 'MeID': 1,
        'CSRFToken': 'test', 'DefFrom': '2026-09-06', 'DefTo': '2026-10-06',
        'Templates': [{'ID': 'byProject', 'Name': 'По проектам', 'Blurb': 'Сколько часов на каждый проект.',
                       'Icon': 'folder'}],
        'SavedReports': reports,
    }, True)


def stats_document(saved_reports):
    return boot('/stats', {
        'Lang': 'ru', 'Active': 'stats', 'CSRFToken': 'test', 'CanManage': True, 'Mods': {'graph': True},
        'Period': {'Start': '2026-10-06T00:00:00+03:00', 'End': '2026-10-06T16:00:00+03:00', 'Label': 'week'},
        'Aggregated': [], 'ByProject': [], 'Projects': [], 'ProjectFilter': '', 'People': [],
        'PersonFilter': 0, 'Sessions': [], 'Total': '0 мин', 'SessionCount': 0, 'SessionsCut': False,
        'MeID': 1, 'ShowAllURL': '/stats?log=all', 'TagFilter': '', 'AllTagNames': [],
        'SavedReports': saved_reports,
    }, True)


def tags_document(tags):
    return boot('/tags', {
        'Lang': 'ru', 'Active': 'tags', 'ReactTags': True, 'CSRFToken': 'test', 'CanManage': True,
        'Tags': tags, 'AllTagNames': [tag['Name'] for tag in tags],
    }, True)


def timesheet_document(slugs):
    def row(activity_id, name, project_id, minutes):
        secs = [0, minutes * 60, 0, 0, 0, 0, 0]
        return {'ActivityID': activity_id, 'ActivityName': name, 'ProjectID': project_id, 'Color': '#8b5cf6',
                'Cells': [{'Index': d['Index'], 'ISO': d['ISO'], 'Secs': secs[d['Index']],
                           'Min': secs[d['Index']] // 60, 'IsToday': d['IsToday']} for d in WEEK],
                'RowTotal': sum(secs), 'RowTotalLabel': f'{minutes} мин'}
    days = [dict(d, Secs=2700, Min=45, Total='45 мин') for d in WEEK]
    return boot('/timesheet', {
        'Lang': 'ru', 'Active': 'timesheet', 'TimesheetReact': True, 'CSRFToken': 'test', 'Days': days,
        'Rows': [row(4, 'Вёрстка главной', 0, 45), row(1, 'Правка макета', 3, 30)],
        'ProjectNames': {'3': 'Внутренние задачи'}, 'ProjectSlugs': slugs,
        'DayTotalLabels': ['0 мин', '1 ч 15 мин'] + ['0 мин'] * 5,
        'GrandTotal': 4500, 'GrandTotalLabel': '1 ч 15 мин', 'Others': [], 'Added': [],
        'DateISO': '2026-10-05', 'PrevWeek': '2026-09-28', 'NextWeek': '2026-10-12',
        'WeekLabel': '5 окт – 11 окт',
    }, True)


async def open_page(browser, html, path):
    context = await browser.new_context(viewport={'width': 1440, 'height': 900})
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


async def check_projects_member(browser):
    context, page, errors = await open_page(browser, projects_document(False), '/projects')
    await page.get_by_role('heading', name='Проекты').wait_for()
    assert await page.locator('a[href="/projects/new"]').count() == 0, \
        'A member must not be offered "Новый проект": /projects/new answers 403'
    assert '/projects/new' not in await page.locator('main').inner_html(), \
        'Nothing on the projects screen may point at the manager-only create page'
    body = await page.locator('main').inner_text()
    assert 'руководитель' in body, f'An empty projects list must say who creates projects: {body}'
    assert not errors, errors
    await context.close()


async def check_projects_owner(browser):
    projects = [{'ID': 7, 'Slug': 'nordwind', 'Name': 'Сайт для Nordwind', 'Color': '#7c3aed',
                 'Archived': False, 'Activities': 3, 'TodayLabel': '1 ч', 'MonthLabel': '20 ч'}]
    context, page, errors = await open_page(browser, projects_document(True, projects), '/projects')
    await page.get_by_role('heading', name='Проекты').wait_for()
    assert await page.locator('a[href="/projects/new"]').count() == 1, \
        'A manager still creates projects from this screen'
    assert await page.locator('a[href="/projects/nordwind"]').count() == 1, \
        'A project card leads to that project'
    assert not errors, errors
    await context.close()


async def check_help_member(browser):
    context, page, errors = await open_page(browser, help_document(False), '/help')
    await page.get_by_role('heading', name='Справка').wait_for()
    hrefs = await page.locator('main a').evaluate_all('links => links.map(a => a.getAttribute("href"))')
    assert '/settings/webhooks' not in hrefs, \
        f'Help must not offer a screen the reader is refused: {hrefs}'
    assert '/settings/tokens' in hrefs and '/import' in hrefs, f'Reachable destinations stay: {hrefs}'
    assert not errors, errors
    await context.close()


async def check_help_owner(browser):
    context, page, errors = await open_page(browser, help_document(True), '/help')
    await page.get_by_role('heading', name='Справка').wait_for()
    assert await page.locator('main a[href="/settings/webhooks"]').count() == 1, \
        'A manager still finds webhooks from help'
    assert not errors, errors
    await context.close()


async def check_reports_own_saved_list(browser):
    reports = [
        {'ID': 3, 'Name': 'Время по Nordwind за неделю', 'Period': 'week', 'ProjectSlug': 'nordwind',
         'Tag': 'Разработка', 'CreatedBy': 1},
        {'ID': 4, 'Name': 'Просто всё за месяц', 'Period': 'month', 'ProjectSlug': '', 'Tag': '',
         'CreatedBy': 1},
    ]
    context, page, errors = await open_page(browser, reports_document(reports), '/reports')
    await page.get_by_role('heading', name='Отчёты').wait_for()
    items = page.locator('main ul[aria-label="Сохранённые отчёты"] li')
    assert await items.count() == 2, 'Every saved report has a row on the reports screen'
    href = await items.first.locator('a').get_attribute('href')
    assert href == '/stats?period=week&project=nordwind&tag=%D0%A0%D0%B0%D0%B7%D1%80%D0%B0%D0%B1%D0%BE%D1%82%D0%BA%D0%B0', href
    plain = await items.nth(1).locator('a').get_attribute('href')
    assert plain == '/stats?period=month', plain
    assert await page.locator('form[action="/api/reports/3/delete"]').count() == 1, \
        'A saved report can be removed from its own screen'
    assert not errors, errors
    await page.screenshot(path='/tmp/paratrack-reports-saved.png', full_page=True)
    await context.close()


async def check_reports_empty(browser):
    context, page, errors = await open_page(browser, reports_document([]), '/reports')
    await page.get_by_role('heading', name='Отчёты').wait_for()
    body = await page.locator('main').inner_text()
    assert 'Сохранённые отчёты (0)' in body, body
    assert not errors, errors
    await context.close()


async def check_stats_points_at_reports(browser):
    reports = [{'ID': 3, 'Name': 'Время по Nordwind за неделю', 'Period': 'week', 'ProjectSlug': 'nordwind',
                'Tag': '', 'CreatedBy': 1}]
    context, page, errors = await open_page(browser, stats_document(reports), '/stats')
    await page.get_by_role('heading', name='Статистика').wait_for()
    # The save action stays where the view is; the list has one home.
    assert await page.locator('form[action="/api/reports/save"]').count() == 1, \
        'The current view can still be saved from the screen it was set on'
    assert await page.locator('form[action="/api/reports/3/delete"]').count() == 0, \
        'The list and its delete action live on /reports only'
    link = page.get_by_role('link', name='Сохранённые отчёты (1)')
    assert await link.count() == 1, 'The screen that saves a report points at the list'
    assert await link.get_attribute('href') == '/reports', 'The count on the stats screen leads to the reports screen'
    assert not errors, errors
    await context.close()


async def check_tag_chip_opens_time(browser):
    tags = [{'ID': 5, 'Name': 'Разработка', 'SessionCount': 12, 'Lang': 'ru'},
            {'ID': 6, 'Name': 'Срочное', 'SessionCount': 3, 'Lang': 'ru'}]
    context, page, errors = await open_page(browser, tags_document(tags), '/tags')
    await page.get_by_role('heading', name='Теги').wait_for()
    chips = page.locator('main ul li a')
    assert await chips.count() == 2, 'Both tags lead somewhere'
    href = await chips.first.get_attribute('href')
    assert 'period=week' in href and 'tag=' in href, f'A tag opens the time it carries: {href}'
    assert 'Срочное' not in href, href
    text = await page.locator('main').inner_text()
    assert 'Нажмите на сам тег' in text, f'The hint says the chip is the way in: {text}'
    assert not errors, errors
    await context.close()


async def check_timesheet_project_link(browser):
    context, page, errors = await open_page(browser, timesheet_document({'3': 'vnutrennie'}), '/timesheet')
    await page.locator('#ts-body').wait_for()
    assert await page.locator('#ts-row-1 th a[href="/projects/vnutrennie"]').count() == 1, \
        'A project named in the sheet leads to that project'
    assert await page.locator('#ts-row-4 th a').count() == 0, \
        'A projectless row has no project to link to'
    assert not errors, errors
    await context.close()


async def check_timesheet_without_slugs(browser):
    # The sheet degrades to plain text rather than a broken link when the
    # server sends names without slugs.
    context, page, errors = await open_page(browser, timesheet_document({}), '/timesheet')
    await page.locator('#ts-body').wait_for()
    assert await page.locator('#ts-row-1 th a').count() == 0, 'No slug, no link'
    assert 'Внутренние задачи' in await page.locator('#ts-row-1 th').inner_text(), 'The name is still shown'
    assert not errors, errors
    await context.close()


async def main():
    async with async_playwright() as p:
        browser = await p.chromium.launch()
        await check_projects_member(browser)
        print('PASS projects as member: no create link, the empty state names who creates projects')
        await check_projects_owner(browser)
        print('PASS projects as manager: create link and project cards work')
        await check_help_member(browser)
        print('PASS help as member: webhooks is not offered')
        await check_help_owner(browser)
        print('PASS help as manager: webhooks is still one click away')
        await check_reports_own_saved_list(browser)
        print('PASS reports: saved reports live here and open the pinned statistics view')
        await check_reports_empty(browser)
        print('PASS reports: the empty saved list is stated, not blank')
        await check_stats_points_at_reports(browser)
        print('PASS stats: the view is saved here, the list lives on /reports')
        await check_tag_chip_opens_time(browser)
        print('PASS tags: a tag opens the time it carries')
        await check_timesheet_project_link(browser)
        print('PASS timesheet: a project in the sheet leads to that project')
        await check_timesheet_without_slugs(browser)
        print('PASS timesheet: without a slug the name stays plain text')
        await browser.close()


if __name__ == '__main__':
    asyncio.run(main())