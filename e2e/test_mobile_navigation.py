"""The phone shell: Material's bottom navigation and touch metrics.

Measured on a real stand at 320/390/768 and found missing: no bottom
navigation at all (every hop cost two taps through a 28px hamburger), every
field 14px (iOS zooms a focused field under 16px and does not always zoom
back), and icon buttons narrower than their height. This locks the phone
frame and the touch floor so a future screen cannot quietly undo them.

Runs against the built UI with deterministic page data — no server, account,
or database.

Run after `cd web && npm run build`: python3 e2e/test_mobile_navigation.py
"""
import asyncio
import json

from playwright.async_api import async_playwright

from test_react_navigation import document, STATIC

BAR_ITEMS = ["Обзор", "Табель", "Статистика", "По часам", "Ещё"]
# Material's minimum for anything a finger hits.
TOUCH = 48

MEASURE = """() => {
  const visible = el => {
    const r = el.getBoundingClientRect();
    const cs = getComputedStyle(el);
    return r.width > 0 && r.height > 0 && cs.visibility !== 'hidden';
  };
  const out = {bar: null, fab: null, small: [], tinyFont: [], narrow: []};
  const bar = document.querySelector('[data-mobile-nav]');
  if (bar && visible(bar)) {
    const r = bar.getBoundingClientRect();
    out.bar = {
      height: Math.round(r.height),
      pinnedToBottom: Math.abs(r.bottom - innerHeight) < 2,
      items: [...bar.querySelectorAll('a, button')].filter(visible).map(el => {
        const box = el.getBoundingClientRect();
        return {text: (el.innerText || '').trim().split('\\n')[0], w: Math.round(box.width), h: Math.round(box.height)};
      }),
    };
  }
  const fab = document.querySelector('.mobile-nav-fab');
  if (fab && visible(fab)) {
    const r = fab.getBoundingClientRect();
    out.fab = {w: Math.round(r.width), h: Math.round(r.height), label: fab.getAttribute('aria-label')};
  }
  document.querySelectorAll('#paratrack-react-root input, #paratrack-react-root textarea, #paratrack-react-root select').forEach(el => {
    if (!visible(el)) return;
    const fs = parseFloat(getComputedStyle(el).fontSize);
    if (fs < 16) out.tinyFont.push({tag: el.tagName, font: fs});
  });
  document.querySelectorAll('#paratrack-react-root button').forEach(el => {
    if (!visible(el)) return;
    const r = el.getBoundingClientRect();
    if (Math.max(r.width, r.height) < %d) out.small.push((el.innerText || el.getAttribute('aria-label') || '').trim().slice(0, 18));
    if (el.getAttribute('aria-label') && r.width < %d) out.narrow.push((el.getAttribute('aria-label') || '').slice(0, 18));
  });
  return out;
}""" % (TOUCH - 8, TOUCH)


def phone_document(path, lang="ru", can_manage=True):
    """The built shell booted at a phone width, with `pointer: coarse`."""
    html = document(path, lang)
    start = html.index('<div id="react-page-data" hidden>') + len('<div id="react-page-data" hidden>')
    end = html.index('</div>', start)
    payload = json.loads(html[start:end])
    payload['shell'].update(active=path.strip('/'), requestPath=path, canManage=can_manage)
    payload['data'].update(Lang=lang, CanManage=can_manage)
    return html[:start] + json.dumps(payload, ensure_ascii=False) + html[end:]


async def open_phone(browser, path, lang="ru", can_manage=True):
    context = await browser.new_context(
        viewport={'width': 390, 'height': 844}, locale='ru-RU', is_mobile=True, has_touch=True)
    page = await context.new_page()
    errors = []
    page.on('pageerror', lambda error: errors.append(str(error)))

    async def respond(route):
        served = route.request.url.split('/static/', 1)[-1]
        if '/static/' in route.request.url:
            await route.fulfill(path=str(STATIC / served))
        else:
            await route.fulfill(content_type='text/html; charset=utf-8', body=phone_document(path, lang, can_manage))
    await context.route('**/*', respond)
    await page.goto(f'http://paratrack.test{path}')
    return context, page, errors


async def check_bar_present(browser):
    for width in (320, 390, 768):
        context, page, errors = await open_phone(browser, '/')
        await page.locator('[data-mobile-nav]').wait_for()
        await page.set_viewport_size({'width': width, 'height': 844})
        await page.wait_for_timeout(300)
        bar = (await page.evaluate(MEASURE))['bar']
        assert bar['height'] >= 56, f'{width}px: the bar is {bar["height"]}px tall, Material asks for 64'
        assert bar['pinnedToBottom'], f'{width}px: the bar is not pinned to the bottom'
        assert [item['text'] for item in bar['items']] == BAR_ITEMS, bar['items']
        for item in bar['items']:
            assert item['h'] >= TOUCH, f'{width}px: bar item "{item["text"]}" is {item["h"]}px tall'
        if width == 390:
            await page.screenshot(path='/tmp/paratrack-mobile-bar.png')
        assert not errors, errors
        await context.close()


async def check_bar_navigation(browser):
    context, page, errors = await open_phone(browser, '/')
    await page.locator('[data-mobile-nav]').wait_for()
    await page.locator('[data-mobile-nav] a', has_text='Табель').click()
    await page.wait_for_url('**/timesheet')
    assert not errors, errors
    await context.close()

    # Booting the screen is how the real shell arrives, and the bar must say
    # where the person is.
    context, page, errors = await open_phone(browser, '/timesheet')
    await page.locator('[data-mobile-nav]').wait_for()
    active = await page.locator('[data-mobile-nav] [aria-current="page"]').inner_text()
    assert active.startswith('Табель'), f'The destination you are on is marked: {active}'
    assert not errors, errors
    await context.close()


async def check_more_sheet(browser):
    context, page, errors = await open_phone(browser, '/')
    await page.locator('[data-mobile-nav]').wait_for()
    await page.locator('[data-mobile-nav] button').click()
    sheet = page.locator('[role="dialog"]')
    await sheet.wait_for()
    height = await sheet.evaluate('el => Math.round(el.getBoundingClientRect().height)')
    assert height < 700, f'The sheet covers {height}px of an 844px screen'
    links = await sheet.locator('a').evaluate_all('els => els.map(e => (e.innerText || "").trim())')
    for label in ('Проекты', 'Отчёты', 'Расписание', 'Теги', 'Экспорт', 'Справка', 'API-токены', 'Профиль и пароль'):
        assert label in links, f'The sheet must carry the desktop footer items too: {label} missing from {links}'
    await sheet.locator('form[action="/api/logout"]').wait_for()
    assert await sheet.locator('form[action="/api/logout"]').count() == 1, 'Signing out is reachable from a phone'
    await page.screenshot(path='/tmp/paratrack-mobile-more.png')
    await sheet.locator('a', has_text='Теги').click()
    await page.wait_for_url('**/tags')
    await page.locator('[data-mobile-nav]').wait_for()
    assert not errors, errors
    await context.close()


async def check_more_sheet_follows_role(browser):
    context, page, errors = await open_phone(browser, '/', can_manage=False)
    await page.locator('[data-mobile-nav]').wait_for()
    await page.locator('[data-mobile-nav] button').click()
    links = await page.locator('[role="dialog"] a').evaluate_all('els => els.map(e => (e.innerText || "").trim())')
    assert 'Настройки команды' not in links, f'A member is not offered a screen that answers 403: {links}'
    assert 'API-токены' in links, 'The member keeps their own settings'
    assert not errors, errors
    await context.close()


async def check_fab_where_it_helps(browser):
    # The overview keeps its own wide «Старт»; the floating button belongs to
    # the screens whose job is to look at time, and nowhere else — on a screen
    # with its own primary action it would cover the control underneath.
    for path, expected in [('/', False), ('/stats', True), ('/timesheet', True), ('/graph', True),
                           ('/projects', False), ('/invoices', False), ('/reports', False),
                           ('/tags', False), ('/schedule', False), ('/settings/members', False)]:
        context, page, errors = await open_phone(browser, path)
        await page.wait_for_timeout(400)
        found = await page.locator('.mobile-nav-fab').count() > 0
        assert found == expected, f'{path}: floating action present={found}, expected={expected}'
        if found:
            box = await page.locator('.mobile-nav-fab').bounding_box()
            assert box['width'] >= TOUCH and box['height'] >= TOUCH, f'{path}: FAB is {box}'
            assert await page.locator('.mobile-nav-fab').get_attribute('aria-label'), f'{path}: the FAB must say what it does'
        assert not errors, errors
        await context.close()


async def check_touch_metrics(browser):
    for path in ('/', '/timesheet', '/stats'):
        context, page, errors = await open_phone(browser, path)
        await page.locator('[data-mobile-nav]').wait_for()
        await page.set_viewport_size({'width': 320, 'height': 844})
        await page.wait_for_timeout(400)
        measured = await page.evaluate(MEASURE)
        assert not measured['tinyFont'], f'{path} at 320px: fields under 16px make iOS zoom — {measured["tinyFont"]}'
        assert not measured['small'], f'{path} at 320px: controls under the touch floor — {measured["small"]}'
        assert not measured['narrow'], f'{path} at 320px: labelled buttons narrower than {TOUCH}px — {measured["narrow"]}'
        assert not errors, errors
        await context.close()


async def check_desktop_is_untouched(browser):
    # The phone frame belongs to a phone: at desktop width there is no bar and
    # no floating action, and the drawer trigger is back.
    context = await browser.new_context(viewport={'width': 1440, 'height': 900})
    page = await context.new_page()

    async def respond(route):
        if '/static/' in route.request.url:
            served = route.request.url.split('/static/', 1)[-1]
            await route.fulfill(path=str(STATIC / served))
        else:
            await route.fulfill(content_type='text/html; charset=utf-8', body=phone_document('/'))
    await context.route('**/*', respond)
    await page.goto('http://paratrack.test/')
    await page.locator('[data-sidebar="trigger"]').wait_for()
    # The phone furniture stays in the document but hidden, the same way the
    # header account menu already does — what matters is that a person cannot
    # see it, click it, or tab into it.
    assert not await page.locator('[data-mobile-nav]').is_visible(), 'No bottom bar on a desktop'
    assert not await page.locator('.mobile-nav-fab').is_visible(), 'No floating action on a desktop'
    assert await page.locator('[data-sidebar="trigger"]').is_visible(), 'The drawer is the desktop navigation'
    await context.close()


MEMBERS = [
    {'UserID': 1, 'Name': 'Пётр Владелец', 'Email': 'owner2@x.test', 'Role': 'owner'},
    {'UserID': 2, 'Name': 'Маша Участница', 'Email': 'member2@x.test', 'Role': 'member'},
]


def members_document(path='/settings/members', lang='ru'):
    """The members screen as it arrives in the browser: a card whose pay form is
    the widest thing on a narrow phone."""
    html = document(path, lang)
    start = html.index('<div id="react-page-data" hidden>') + len('<div id="react-page-data" hidden>')
    end = html.index('</div>', start)
    payload = json.loads(html[start:end])
    payload['shell'].update(active='members', requestPath=path, canManage=True)
    payload['data'].update(Lang=lang, CanManage=True, IsOwner=True, MembersReact=True, Members=MEMBERS,
                           Pay={'1': {'Rate': '', 'Capacity': 480}, '2': {'Rate': '', 'Capacity': 480}},
                           User={'ID': 1}, Flash='', FlashOK=False, CSRFToken='test')
    return html[:start] + json.dumps(payload, ensure_ascii=False) + html[end:]


async def open_html(browser, html, path, width, height=844):
    context = await browser.new_context(viewport={'width': width, 'height': height}, locale='ru-RU',
                                  is_mobile=True, has_touch=True)
    page = await context.new_page()
    errors = []
    page.on('pageerror', lambda error: errors.append(str(error)))

    async def respond(route):
        served = route.request.url.split('/static/', 1)[-1]
        if '/static/' in route.request.url:
            file = STATIC / served
            await route.fulfill(path=str(file)) if file.is_file() else await route.fulfill(status=404)
        else:
            await route.fulfill(content_type='text/html; charset=utf-8', body=html)
    await context.route('**/*', respond)
    await page.goto(f'http://paratrack.test{path}', wait_until='domcontentloaded')
    return context, page, errors


LONG_MESSAGE = 'Удалить проект? ' + 'Этот проект занимает существенную долю времени команды. ' * 24


async def check_confirmation_button_stays_reachable(browser):
    """A long confirmation message must not push the action off the screen.

    The dialog used to grow without a limit, so on a 844px phone the confirm
    button sat at y≈1000 — below the fold, with the top of the dialog cut off
    above the viewport. Title and buttons stay put; the message scrolls."""
    for width in (320, 390):
        context, page, errors = await open_html(browser, phone_document('/'), '/', width)
        await page.locator('#main h1').wait_for()
        await page.evaluate("""(message) => document.dispatchEvent(new CustomEvent('paratrack:confirm', {
            detail: {message, resolve: () => {}, trigger: document.activeElement}}))""", LONG_MESSAGE)
        dialog = page.locator('[role="alertdialog"], [role="dialog"]').last
        await dialog.wait_for()
        state = await dialog.evaluate("""el => {
          const box = el.getBoundingClientRect();
          const buttons = [...el.querySelectorAll('button')];
          const action = buttons[buttons.length - 1].getBoundingClientRect();
          return {top: Math.round(box.top), bottom: Math.round(box.bottom), height: Math.round(box.height),
                  actionBottom: Math.round(action.bottom), actionTop: Math.round(action.top), viewport: innerHeight};
        }""")
        assert state['top'] >= 0, f'{width}px: the dialog starts above the screen at y={state["top"]}'
        assert state['bottom'] <= state['viewport'], f'{width}px: the dialog ends below the screen at y={state["bottom"]}'
        assert 0 < state['actionTop'] and state['actionBottom'] <= state['viewport'], \
            f'{width}px: the confirm button is off screen at y={state["actionTop"]}–{state["actionBottom"]}'
        if width == 320:
            await page.screenshot(path='/tmp/paratrack-confirm-long.png')
        assert not errors, errors
        await context.close()


async def check_member_card_fits(browser):
    """Nothing on a member card may stick out of it.

    The pay form asked for two 5rem columns plus the button — 272px inside a
    224px box on a 320px phone, so «Сохранить» and the caption under it were cut
    off at the card edge."""
    for width in (320, 390):
        context, page, errors = await open_html(browser, members_document(), '/settings/members', width)
        await page.locator('main article').first.wait_for()
        await page.set_viewport_size({'width': width, 'height': 844})
        await page.wait_for_timeout(400)
        overflow = await page.evaluate("""() => {
          const card = document.querySelector('main article');
          const right = card.getBoundingClientRect().right;
          return [...card.querySelectorAll('*')]
            .filter(el => el.getBoundingClientRect().right > right + 1)
            .map(el => (el.innerText || el.value || el.tagName).trim().replace(/\\s+/g, ' ').slice(0, 24));
        }""")
        assert not overflow, f'{width}px: these leave the member card — {overflow}'
        save = page.get_by_role('button', name='Сохранить').first
        assert await save.is_visible(), f'{width}px: «Сохранить» must stay visible'
        box = await save.bounding_box()
        assert box['x'] >= 0 and box['x'] + box['width'] <= width, f'{width}px: «Сохранить» is cut at {box}'
        assert not errors, errors
        await context.close()


async def main():
    async with async_playwright() as p:
        browser = await p.chromium.launch()
        await check_bar_present(browser)
        print('PASS bottom bar at 320/390/768: five destinations, 64px, pinned')
        await check_bar_navigation(browser)
        print('PASS a tap on a destination goes there and marks itself')
        await check_more_sheet(browser)
        print('PASS «Ещё» opens a capped sheet with the rest, the account items and sign-out')
        await check_more_sheet_follows_role(browser)
        print('PASS «Ещё» follows the role: no team settings for a member')
        await check_fab_where_it_helps(browser)
        print('PASS floating action only where there is no primary action of its own')
        await check_touch_metrics(browser)
        print('PASS 320px: fields are 16px and every control reaches the 48px floor')
        await check_desktop_is_untouched(browser)
        print('PASS desktop keeps the drawer and gains no phone furniture')
        await check_confirmation_button_stays_reachable(browser)
        print('PASS a long confirmation keeps its button on screen and scrolls the message')
        await check_member_card_fits(browser)
        print('PASS 320/390px: nothing leaves a member card, «Сохранить» stays whole')
        await browser.close()


if __name__ == '__main__':
    asyncio.run(main())