"""The dashboard intro must promise only what this person can actually open.

A member has no «Участники», «Выплаты», «Приглашения» or «Разделы», so the
owner's line sent her straight into «Нет доступа».

Run after `cd web && npm run build:react`: python3 e2e/test_member_dashboard_blurb.py
No server, account, or database is required.
"""
import asyncio
import json
from urllib.parse import urlparse
from playwright.async_api import async_playwright
from test_react_navigation import document, STATIC

MANAGER_STUDIO = "Таймеры и быстрый переход к планированию команды, участникам и выплатам."
MEMBER_PLAN = "Учитывайте своё время и смотрите план работы команды."
MEMBER_PLAIN = "Учитывайте своё время по проектам команды: таймеры, табель и итоги дня."
MEMBER_BILLING = "Учёт работы по проектам и часы, которые ещё нужно выставить клиентам."


def dashboard_document(mode, manager, mods):
    html = document('/')
    start = html.index('<div id="react-page-data" hidden>') + len('<div id="react-page-data" hidden>')
    end = html.index('</div>', start)
    bootstrap = json.loads(html[start:end])
    bootstrap['shell'].update(canManage=manager, mods=mods)
    bootstrap['data'].update(Mode=mode, CanManage=manager, Mods=mods, RunningCount=0, PausedCount=0)
    return html[:start] + json.dumps(bootstrap) + html[end:]


async def route_html(context, html):
    async def respond(route):
        path = urlparse(route.request.url).path
        if path.startswith('/static/'):
            await route.fulfill(path=str(STATIC / path.removeprefix('/static/')))
        else:
            await route.fulfill(content_type='text/html', body=html)
    await context.route('**/*', respond)


async def intro(browser, mode, manager, mods, expected):
    context = await browser.new_context(viewport={'width': 1440, 'height': 900})
    page = await context.new_page()
    errors = []
    page.on('pageerror', lambda error: errors.append(str(error)))
    html = dashboard_document(mode, manager, mods)
    await route_html(context, html)
    await page.goto('http://paratrack.test/')
    await page.locator('[data-dashboard-blurb]').wait_for()
    text = (await page.locator('[data-dashboard-blurb]').inner_text()).strip()
    assert text == expected, f'{mode} manager={manager} mods={mods}: got {text!r}'
    # The intro and the menu must agree: whatever the line points at is
    # reachable, whatever is hidden is not promised.
    hrefs = await page.locator('nav.app-shell-desktop-nav a').evaluate_all(
        'links=>links.map(a=>a.getAttribute("href"))')
    for promised in ('/settings/members', '/payroll', '/settings/sections'):
        if not manager:
            assert promised not in hrefs, f'{mode} member must not be pointed at {promised}'
    if not manager:
        if text == MEMBER_PLAN:
            assert '/schedule' in hrefs, 'The team plan is promised, so the plan must be in the menu'
        else:
            assert '/schedule' not in hrefs, 'The team plan is not in the menu, so it must not be promised'
    assert not errors, errors
    print(f'PASS {mode} manager={manager} schedule={bool(mods and mods.get("schedule"))}: {text}')
    await context.close()


async def main():
    async with async_playwright() as p:
        browser = await p.chromium.launch()
        studio = {'goals': True, 'schedule': True, 'payroll': True, 'invoices': True, 'tags': True}
        no_plan = {key: value for key, value in studio.items() if key != 'schedule'}
        freelance = {'goals': True, 'invoices': True, 'tags': True}
        await intro(browser, 'studio', True, studio, MANAGER_STUDIO)
        await intro(browser, 'studio', False, studio, MEMBER_PLAN)
        await intro(browser, 'studio', False, no_plan, MEMBER_PLAIN)
        await intro(browser, 'freelance', False, freelance, MEMBER_PLAIN)
        await intro(browser, 'solo', False, {'goals': True}, MEMBER_PLAIN)
        # Guard the line this package exists to replace.
        context = await browser.new_context(viewport={'width': 1440, 'height': 900})
        page = await context.new_page()
        await route_html(context, document('/'))
        await page.goto('http://paratrack.test/')
        await page.locator('[data-dashboard-blurb]').wait_for()
        text = (await page.locator('[data-dashboard-blurb]').inner_text()).strip()
        assert text != MEMBER_BILLING, 'A member must not be told about hours left to bill'
        await context.close()
        print('PASS no member sees the billing line')
        await browser.close()


if __name__ == '__main__':
    asyncio.run(main())