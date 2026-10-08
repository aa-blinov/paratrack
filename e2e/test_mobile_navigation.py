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

from test_navigation_map_by_role import timesheet_document, stats_document, boot
from test_react_navigation import document, STATIC

BAR_ITEMS = ["Обзор", "Табель", "Статистика", "По часам", "Ещё"]
# Material's minimum for anything a finger hits, and the size the FAB is
# drawn at.
TOUCH = 48
FAB = 56
# The names that broke «Идут сейчас»: one short, one that exactly filled the old
# 96px lane, one far past any lane.
PROJECTS = [{"ID": 1, "Name": "Ремонт"},
            {"ID": 2, "Name": "Сайт для Nordwind"},
            {"ID": 3, "Name": "Проектирование и техническое сопровождение интеграционного контура учёта рабочего времени"}]
PROJECTS_BY_NAME = {project["Name"] for project in PROJECTS}
ACTIVITIES = ["Разбор сметы", "Созвон с клиентом", "Правка макета"]

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
    payload['data'].update(Lang=lang, CanManage=can_manage, SectionsOpen=5, SectionsTotal=9)
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
            # Material 3 sizes the FAB at 56dp; it was only checked for being
            # touchable, so a drift to any other round number passed silently.
            assert box['width'] == FAB and box['height'] == FAB, f'{path}: FAB is {box}, Material 3 says 56dp'
            assert await page.locator('.mobile-nav-fab').get_attribute('aria-label'), f'{path}: the FAB must say what it does'
        assert not errors, errors
        await context.close()


async def check_one_touch_floor_everywhere(browser):
    """The phone had two floors: 48dp inside the React shell and 44dp in the
    server-rendered one, so the running-timer bar's pause and stop sat 4dp
    under Material's minimum — the only real touch-target violation on the
    phone. The bar is server-rendered outside the React root, so its markup is
    injected here exactly as the template writes it; otherwise the check would
    pass on a page that simply has no bar and prove nothing."""
    context, page, errors = await open_phone(browser, '/timesheet')
    await page.locator('#main h1').wait_for()
    await page.wait_for_timeout(500)
    await page.evaluate("""() => {
      const bar = document.createElement('aside');
      bar.id = 'minibar';
      bar.className = 'minibar';
      bar.innerHTML = `
        <div class="minibar-card">
          <a href="/" class="minibar-main" aria-label="Обзор">
            <span class="minibar-dot is-live" style="background-color: #16a34a"></span>
            <span class="min-w-0 flex-1">
              <span class="block truncate font-medium">Правка макета</span>
              <span class="block text-sm opacity-70 font-mono">00:06:48</span>
            </span>
          </a>
          <button class="btn btn-ghost btn-circle" aria-label="Пауза"></button>
          <button class="btn btn-stop btn-circle" aria-label="Стоп"></button>
        </div>`;
      document.body.appendChild(bar);
    }""")
    await page.wait_for_timeout(300)
    measured = await page.evaluate("""() => {
      const out = [];
      for (const el of document.querySelectorAll('#minibar button')) {
        const r = el.getBoundingClientRect();
        out.push({label: el.getAttribute('aria-label'), w: Math.round(r.width), h: Math.round(r.height)});
      }
      return out;
    }""")
    assert len(measured) == 2, f'the injected bar rendered its two controls — {measured}'
    for control in measured:
        assert max(control['w'], control['h']) >= TOUCH, \
            f'the timer bar control «{control["label"]}» is {control["w"]}×{control["h"]}px, under Material\'s {TOUCH}dp'
    assert not errors, errors
    await context.close()


async def check_press_is_a_state_layer(browser):
    """Material presses with a state layer, never by scaling or dimming the
    target. The phone did both: `transform: scale(0.96)` in the server-rendered
    shell and `opacity: .55` in the React one, so the same tap felt different
    depending on where you were. Now both paint the current ink at 12% and
    leave the box alone."""
    for path in ('/', '/stats'):
        context, page, errors = await open_phone(browser, path)
        await page.locator('#main h1').wait_for()
        await page.wait_for_timeout(500)
        button = page.locator('#paratrack-react-root button:visible').first
        box = await button.bounding_box()
        assert box, f'{path}: a visible button to press'
        await page.mouse.move(box['x'] + box['width'] / 2, box['y'] + box['height'] / 2)
        await page.mouse.down()
        await page.wait_for_timeout(250)
        state = await button.evaluate("""e => {
          const c = getComputedStyle(e);
          return {image: c.backgroundImage, transform: c.transform, opacity: c.opacity};
        }""")
        await page.mouse.up()
        assert state['transform'] == 'none', f'{path}: a press must not move the target — {state}'
        assert state['opacity'] == '1', f'{path}: a press must not dim the target — {state}'
        assert 'linear-gradient' in state['image'], f'{path}: a press paints a state layer — {state}'
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


LONG_ACTIVITY = 'Проектирование и техническое сопровождение интеграционного контура учёта рабочего времени сразу для нескольких подразделов'


async def check_one_navigation_at_every_width(browser):
    """The drawer and the bottom bar must never be on screen together.

    The sidebar switched to a docked rail at 768px while the phone shell began
    at 1024px, so a tablet in portrait carried two navigations and the bar lay
    over the desktop shell."""
    for width, docked in ((767, False), (768, False), (900, False), (1023, False),
                          (1024, True), (1440, True)):
        context, page, errors = await open_html(browser, phone_document('/'), '/', width, 900)
        await page.locator('#main h1').wait_for()
        rail = await page.locator('[data-sidebar="sidebar"]').is_visible()
        bar = await page.locator('[data-mobile-nav]').is_visible()
        assert rail == docked, f'{width}px: the sidebar is {"docked" if rail else "an overlay"}, expected {"docked" if docked else "an overlay"}'
        assert bar == (not docked), f'{width}px: the bottom bar must show exactly when the rail does not'
        assert not (rail and bar), f'{width}px: two navigations at once'
        assert not errors, errors
        await context.close()


async def check_long_names_stay_readable(browser):
    """A long activity or project name may wrap or be cut with an ellipsis, but
    it must never be silently sliced by a fixed-height box."""
    html = phone_document('/')
    start = html.index('<div id="react-page-data" hidden>') + len('<div id="react-page-data" hidden>')
    end = html.index('</div>', start)
    payload = json.loads(html[start:end])
    payload['data'].update(
        Recent=[{'ID': 1, 'Activity': LONG_ACTIVITY, 'ActivityID': 1, 'Project': LONG_ACTIVITY,
                 'ProjectSlug': 'x', 'Start': '09:00', 'End': '10:00', 'Seconds': 3600,
                 'Duration': '1 ч', 'Note': '', 'Tags': [], 'User': '', 'Me': True}],
        Projects=[{'ID': 1, 'Slug': 'x', 'Name': LONG_ACTIVITY, 'Color': '#7c3aed'}])
    html = html[:start] + json.dumps(payload, ensure_ascii=False) + html[end:]

    for width in (320, 390, 768, 1440):
        context, page, errors = await open_html(browser, html, '/', width, 900)
        await page.locator('#main h1').wait_for()
        await page.wait_for_timeout(500)
        sliced = await page.evaluate("""() => {
          const out = [];
          document.querySelectorAll('#main *').forEach(el => {
            const cs = getComputedStyle(el);
            if (cs.display === 'none' || cs.overflow !== 'hidden' || cs.overflowY !== 'hidden') return;
            if (el.scrollHeight <= el.clientHeight + 1 || el.clientHeight === 0) return;
            if (el.className.toString().includes('sr-only') || el.closest('.sr-only')) return;
            if (!el.textContent.trim()) return;
            out.push((el.innerText || '').trim().replace(/\\s+/g, ' ').slice(0, 40));
          });
          return [...new Set(out)];
        }""")
        assert not sliced, f'{width}px: text sliced by a fixed box — {sliced}'
        assert not errors, errors
        await context.close()


async def check_the_week_never_scrolls_sideways(browser):
    """The week is a grid or a list of days — never a sideways scroll.

    Measured on a stand: the grid needs 768px of content and does not get it at
    640 (608px), 768 (736px) or 1024 (720px, the rail takes 256), while 900 and
    1100+ fit. The rule therefore sits at 1100px."""
    for width in (320, 640, 768, 900, 1024):
        context, page, errors = await open_html(browser, timesheet_document({"3": "vnutrennie"}), '/timesheet', width)
        await page.locator('#main h1').wait_for()
        await page.wait_for_timeout(500)
        shape = await page.evaluate("""() => {
          const grid = document.querySelector('table.week-grid');
          const scroller = grid ? grid.closest('[data-slot="card-content"]') : null;
          const days = [...document.querySelectorAll('main [data-slot="card"]')]
            .filter(card => card.querySelector('input[type="number"]'));
          return {page: document.documentElement.scrollWidth, viewport: innerWidth,
                  gridVisible: grid ? grid.getBoundingClientRect().width > 0 : false,
                  gridScrolls: scroller ? scroller.scrollWidth > scroller.clientWidth + 1 : false,
                  dayCards: days.length};
        }""")
        assert shape['page'] <= shape['viewport'] + 1, f'{width}px: the sheet scrolls sideways ({shape})'
        assert not shape['gridVisible'], f'{width}px: the grid does not fit here and must give way to the day list'
        # A phone opens on today; the whole week is one tap away.
        assert shape['dayCards'] <= 2, f'{width}px: today alone until asked for the week, got {shape["dayCards"]}'
        toggle = page.locator('.ts-day-toggle-item')
        assert await toggle.count() == 2, f'{width}px: the week is reachable by one tap'
        await toggle.nth(1).click()
        await page.wait_for_timeout(300)
        assert await page.locator('[data-slot="card"]').filter(has=page.locator('input[type="number"]')).count() >= 7, \
            f'{width}px: the whole week is seven day blocks'
        assert not errors, errors
        await context.close()

    for width in (1280, 1440):
        context, page, errors = await open_html(browser, timesheet_document({"3": "vnutrennie"}), '/timesheet', width)
        await page.locator('#main h1').wait_for()
        await page.wait_for_timeout(500)
        shape = await page.evaluate("""() => {
          const grid = document.querySelector('table.week-grid');
          const scroller = grid.closest('[data-slot="card-content"]');
          return {visible: grid.getBoundingClientRect().width > 0,
                  scrolls: scroller.scrollWidth > scroller.clientWidth + 1,
                  days: document.querySelectorAll('main [data-slot="card"]').length};
        }""")
        assert shape['visible'], f'{width}px: the grid belongs here, it fits'
        assert not shape['scrolls'], f'{width}px: the grid fits without a sideways scroll ({shape})'
        assert not errors, errors
        await context.close()


async def check_the_mode_says_what_it_opens(browser):
    """«Для себя» named a mode and said nothing about its cost. The dashboard
    line now carries the count: 5 of 9 sections."""
    context, page, errors = await open_html(browser, phone_document('/'), '/', 1440)
    await page.locator('#main h1').wait_for()
    await page.wait_for_timeout(400)
    line = page.locator('main a[href="/settings/sections"], main span').filter(has_text='Режим').first
    text = await line.inner_text()
    assert 'открыто 5 из 9' in text, f'the mode line must say what it opens: {text}'
    assert await page.locator('main a[href="/settings/sections"]').count() == 1, \
        'a manager can go straight to the sections from that line'
    assert not errors, errors
    await context.close()

async def check_collapsed_rail_scrolls(browser):
    """Collapsed to icons the rail hid its overflow, so on a 720px screen the
    bottom icons were out of reach: hidden stops wheel and touch scrolling."""
    for width, height in ((1280, 720), (1440, 900)):
        context, page, errors = await open_html(browser, phone_document('/'), '/', width, height)
        await page.locator('#main h1').wait_for()
        await page.locator('.app-shell-sidebar-trigger').click()
        await page.wait_for_timeout(600)
        state = await page.evaluate("""() => {
          const rail = document.querySelector('[data-sidebar="sidebar"]');
          const box = rail.querySelector('[data-sidebar="content"]');
          const items = [...rail.querySelectorAll('[data-sidebar="menu-button"]')];
          const bottom = box.getBoundingClientRect().bottom;
          const lastBefore = items[items.length - 1].getBoundingClientRect().bottom;
          box.scrollTop = 9999;
          const lastAfter = items[items.length - 1].getBoundingClientRect().bottom;
          box.scrollTop = 0;
          return {overflow: getComputedStyle(box).overflowY, items: items.length,
                  clipped: lastBefore > bottom + 1, reachable: lastAfter <= bottom + 1,
                  marker: !!rail.querySelector('[data-nav-more]')};
        }""")
        assert state['overflow'] == 'auto', f'{width}x{height}: the rail cannot scroll — {state}'
        assert not state['clipped'] or state['reachable'], f'{width}x{height}: an icon is out of reach — {state}'
        if state['clipped']:
            assert state['marker'], f'{width}x{height}: the list continues and must say so — {state}'
        assert not errors, errors
        await context.close()


def running_document(path, projects):
    """The shell booted with timers already running, each on its own project."""
    html = document(path)
    start = html.index('<div id="react-page-data" hidden>') + len('<div id="react-page-data" hidden>')
    end = html.index('</div>', start)
    payload = json.loads(html[start:end])
    payload['shell'].update(active=path.strip('/'), requestPath=path)
    payload['data'].update(
        Projects=projects,
        RunningCount=len(ACTIVITIES),
        ActiveSessions=[
            {"ID": index + 1, "ActivityID": index + 1, "ActivityName": activity,
             "Color": "#16a34a", "ProjectID": projects[index]["ID"],
             "ProjectName": projects[index]["Name"], "ProjectSlug": "",
             "StartLocal": "сегодня 18:20", "StartISO": "2026-10-07T18:20:00Z",
             "ResumeISO": "2026-10-07T18:20:00Z", "Clock": "00:06:48",
             "Paused": False, "AccumulatedSeconds": 408}
            for index, activity in enumerate(ACTIVITIES)])
    return html[:start] + json.dumps(payload, ensure_ascii=False) + html[end:]


async def check_running_projects_are_readable(browser):
    """«Идут сейчас» named the project in a chip that could not be read. Measured
    on a phone: the chip took `min-height: 48px` from the coarse-pointer rule
    and set the row's height, and `.ledger-project` held it at 96px of a 123px
    name, cut mid-word with no ellipsis — «Сайт для» instead of «Сайт для
    Nordwind». Both are checked here: the chip must not dictate the row, and a
    name that has to be shortened must at least say that it was."""
    for width in (320, 390):
        context = await browser.new_context(
            viewport={'width': width, 'height': 844}, locale='ru-RU', is_mobile=True, has_touch=True)
        page = await context.new_page()
        errors = []
        page.on('pageerror', lambda error: errors.append(str(error)))

        async def respond(route):
            served = route.request.url.split('/static/', 1)[-1]
            if '/static/' in route.request.url:
                await route.fulfill(path=str(STATIC / served))
            else:
                await route.fulfill(content_type='text/html; charset=utf-8', body=running_document('/', PROJECTS))
        await context.route('**/*', respond)
        await page.goto('http://paratrack.test/')
        await page.locator('#main h1').wait_for()
        await page.wait_for_timeout(500)

        rows = await page.evaluate("""() => {
          const cards = [...document.querySelectorAll('#active-list [data-session-id]')];
          return cards.map(card => {
            const name = card.querySelector('strong');
            const chip = card.querySelector('button.ledger-project');
            const value = chip && chip.querySelector('[data-slot="select-value"]');
            const clipped = el => !!el && el.scrollWidth > el.clientWidth + 1;
            return {activity: name ? name.textContent.trim() : '',
                    activityClipped: clipped(name),
                    chipHeight: Math.round(chip.getBoundingClientRect().height),
                    chipLeft: Math.round(chip.getBoundingClientRect().left),
                    valueClipped: clipped(value),
                    textOverflow: value ? getComputedStyle(value).textOverflow : '',
                    project: value ? value.textContent.trim() : ''};
          });
        }""")
        assert rows, f'{width}px: the running timers are on the dashboard'
        for row in rows:
            assert not row['activityClipped'], \
                f'{width}px: «{row["activity"]}» is sliced to make room for its project'
            if row['valueClipped']:
                assert row['textOverflow'] == 'ellipsis', \
                    f'{width}px: «{row["project"]}» is cut with nothing to say so — it needs an ellipsis'
            else:
                assert row['project'] in PROJECTS_BY_NAME, \
                    f'{width}px: «{row["project"]}» fits, so it must be the whole name'
        assert len({row['chipLeft'] for row in rows}) == 1, \
            f'{width}px: every project chip starts on the same line — {[r["chipLeft"] for r in rows]}'
        assert max(row['chipHeight'] for row in rows) < TOUCH, \
            f'{width}px: the chip must not set the row height — {[r["chipHeight"] for r in rows]}px of a {TOUCH}px target'
        assert not errors, errors
        await context.close()


def schedule_document():
    """The schedule booted with a week and two people to plan."""
    days = [{'Index': index, 'Label': ('Пн', 'Вт', 'Ср', 'Чт', 'Пт', 'Сб', 'Вс')[index],
             'Date': 5 + index, 'ISO': f'2026-10-{5 + index:02d}', 'Min': 0,
             'Total': '0 мин', 'IsToday': index == 2} for index in range(7)]
    cells = lambda: [dict(day) for day in days]
    return boot('/schedule', {
        'Lang': 'ru', 'Active': 'schedule', 'ScheduleReact': True, 'CSRFToken': 'test',
        'CanManage': True, 'WeekLabel': '5 окт – 11 окт', 'PrevWeek': '2026-09-28',
        'NextWeek': '2026-10-12', 'ThisWeek': '2026-10-05', 'ProjectID': 1, 'Days': days,
        'Rows': [{'UserID': index + 1,
                  'UserName': name,
                  'Capacity': 2400,
                  'Cells': cells(),
                  'Total': '0 мин',
                  'TotalMin': 0,
                  'LoadPct': 0} for index, name in enumerate(['Пётр Владелец', 'Маша Участница'])],
        'Projects': [{'ID': 1, 'Slug': 'nordwind', 'Name': 'Сайт для Nordwind', 'Color': '#7c3aed'}],
        'GrandTotal': '0 мин', 'GrandMin': 0,
    }, True)


async def check_the_minutes_column_lines_up(browser):
    """A wrapped activity name made its row taller, and the minutes input was
    centred in the row — so the column stepped down 4px at every name that
    wrapped, and the week read as a staircase. Measured on a 390px stand with
    seven rows: `inputTop` was 0 for one-line names and 4 for two-line ones."""
    for width in (320, 390):
        html = boot('/timesheet', {
            'Lang': 'ru', 'Active': 'timesheet', 'TimesheetReact': True, 'CSRFToken': 'test',
            'Days': [{'Index': index, 'Label': 'Пн', 'Date': 5 + index, 'ISO': f'2026-10-{5 + index:02d}',
                      'Secs': 2700, 'Min': 45, 'Total': '45 мин', 'IsToday': index == 0}
                     for index in range(7)],
            'Rows': [{'ActivityID': index + 1, 'ActivityName': name, 'ProjectID': 0, 'Color': '#16a34a',
                      'Cells': [{'Index': index, 'ISO': f'2026-10-{5 + index:02d}', 'Secs': 2700,
                                 'Min': 45, 'IsToday': index == 0} for index in range(7)],
                      'RowTotal': 2700, 'RowTotalLabel': '45 мин'}
                     for index, name in enumerate(['Кнопки', 'Проверка перекрытия полосы у таймера',
                                                  'Правка макета', 'Разбор сметы за неделю'])],
            'ProjectNames': {}, 'ProjectSlugs': {},
            'DayTotalLabels': ['3 ч'] * 7, 'GrandTotal': 16200, 'GrandTotalLabel': '18 ч',
            'Others': [], 'Added': [], 'DateISO': '2026-10-05', 'PrevWeek': '2026-09-28',
            'NextWeek': '2026-10-12', 'WeekLabel': '5 окт – 11 окт',
        }, True)
        context, page, errors = await open_html(browser, html, '/timesheet', width)
        await page.locator('#main h1').wait_for()
        await page.wait_for_timeout(600)
        rows = await page.evaluate("""() => {
          const cards = [...document.querySelectorAll('.ts-day-list input')]
            .map(i => i.parentElement).filter(Boolean);
          return [...new Set(cards)].map(row => {
            const r = row.getBoundingClientRect();
            const label = row.querySelector('.grid-name [class*="line-clamp"]');
            const lines = label ? Math.round(label.getBoundingClientRect().height /
                                 parseFloat(getComputedStyle(label).lineHeight)) : 1;
            return {lines, name: label ? label.textContent.trim() : '',
                    inputTop: Math.round(row.querySelector('input').getBoundingClientRect().top - r.top),
                    trashTop: Math.round(row.querySelector('button').getBoundingClientRect().top - r.top)};
          });
        }""")
        assert len(rows) >= 4, f'{width}px: the week is on screen — {len(rows)} rows'
        assert len({row['lines'] for row in rows}) > 1, \
            f'{width}px: some names must wrap, or this check proves nothing — {[r["name"] for r in rows]}'
        assert len({row['inputTop'] for row in rows}) == 1, \
            f'{width}px: the minutes column must line up whatever the name does — {[(r["name"], r["lines"], r["inputTop"]) for r in rows]}'
        assert len({row['trashTop'] for row in rows}) == 1, \
            f'{width}px: the delete button must line up too — {[(r["name"], r["trashTop"]) for r in rows]}'
        assert not errors, errors
        await context.close()


async def check_the_schedule_never_scrolls_sideways(browser):
    """The schedule was the only screen in the app whose week scrolled sideways:
    768px of grid in a 358px screen, three of seven days ever visible, and at
    320px a week nav that did not wrap pushed the whole page 31px wider than the
    screen — which the bottom bar and the running-timer bar inherited. Same rule
    as the timesheet: a grid that fits, or a list of days."""
    for width in (320, 390, 768, 1024):
        context, page, errors = await open_html(browser, schedule_document(), '/schedule', width)
        await page.locator('#main h1').wait_for()
        await page.wait_for_timeout(600)
        state = await page.evaluate("""() => {
          const grid = document.querySelector('table.week-grid');
          const list = document.querySelector('[data-sched-days]');
          const over = [];
          for (const el of document.querySelectorAll('body *')) {
            const r = el.getBoundingClientRect();
            if (r.width > 0 && r.right > document.documentElement.clientWidth + 1) over.push(el.tagName);
          }
          return {gridVisible: !!(grid && grid.getClientRects().length),
                  listVisible: !!(list && list.getClientRects().length),
                  docOver: document.documentElement.scrollWidth - document.documentElement.clientWidth,
                  scrollers: [...document.querySelectorAll('#paratrack-react-root *')]
                    .filter(el => /auto|scroll/.test(getComputedStyle(el).overflowX) &&
                                  el.scrollWidth > el.clientWidth + 1).length,
                  overflowing: over.length};
        }""")
        assert state['listVisible'], f'{width}px: a phone gets one block per day — {state}'
        assert not state['gridVisible'], f'{width}px: the grid does not fit here, so it must step aside — {state}'
        assert state['docOver'] <= 1, f'{width}px: the page must not scroll sideways — {state}'
        assert state['scrollers'] == 0, f'{width}px: nothing inside may scroll sideways — {state}'
        assert state['overflowing'] == 0, f'{width}px: nothing may leave the screen — {state}'
        assert not errors, errors
        await context.close()

    for width in (1280, 1440):
        context, page, errors = await open_html(browser, schedule_document(), '/schedule', width)
        await page.locator('#main h1').wait_for()
        await page.wait_for_timeout(600)
        state = await page.evaluate("""() => {
          const grid = document.querySelector('table.week-grid');
          const box = grid.closest('.overflow-x-auto');
          return {gridVisible: !!grid.getClientRects().length,
                  days: grid.querySelectorAll('thead th').length,
                  fits: box.scrollWidth <= box.clientWidth + 1};
        }""")
        assert state['gridVisible'], f'{width}px: the grid belongs here — {state}'
        assert state['days'] == 9, f'{width}px: member, seven days and a total — {state}'
        assert state['fits'], f'{width}px: the grid must fit without a sideways scroll — {state}'
        assert not errors, errors
        await context.close()


async def check_the_breakdown_holds_a_long_name(browser):
    """One long activity name blew the whole breakdown out of the screen. The
    name sat in a flex row with `min-width: auto`, so a 60-character unbroken
    word kept its full 487px and pushed the section to 641px inside a 288px
    card — and the card clipped, taking every row's duration with it. Nothing
    scrolled: the figures were simply gone."""
    long_name = 'Отладкаинтеграционногоконтураучётарабочевременипереносданных'
    html = stats_document([])
    start = html.index('<div id="react-page-data" hidden>') + len('<div id="react-page-data" hidden>')
    end = html.index('</div>', start)
    payload = json.loads(html[start:end])
    payload['data']['ByProject'] = [{
        'ProjectID': 1, 'ProjectName': 'Сайт для Nordwind', 'Slug': 'nordwind',
        'Color': '#7c3aed', 'Duration': '3 ч', 'Share': 100,
        'Activities': [
            {'ActivityName': 'Проверка заголовков', 'Color': '#84cc16', 'Duration': '1 ч', 'Share': 33.3},
            {'ActivityName': long_name, 'Color': '#6366f1', 'Duration': '2 ч', 'Share': 66.7},
        ]}]
    html = html[:start] + json.dumps(payload, ensure_ascii=False) + html[end:]

    for width in (320, 390):
        context, page, errors = await open_html(browser, html, '/stats', width)
        await page.get_by_role('heading', name='Статистика').wait_for()
        await page.wait_for_timeout(600)
        state = await page.evaluate("""(needle) => {
          const nameEl = [...document.querySelectorAll('main span')]
            .find(el => el.textContent.trim() === needle);
          if (!nameEl) return {missing: true};
          const row = nameEl.closest('div.flex');
          const section = row.parentElement;
          const card = section.closest('[data-slot="card-content"]');
          const rows = [...section.children].filter(el => el.tagName === 'DIV');
          const limit = document.documentElement.clientWidth;
          const names = rows.map(r => r.firstElementChild.querySelector('span:last-child'));
          return {sectionW: Math.round(section.getBoundingClientRect().width),
                  cardW: Math.round(card.getBoundingClientRect().width),
                  docOver: document.documentElement.scrollWidth - limit,
                  rows: rows.length,
                  valuesVisible: rows.every(r => {
                    const v = r.lastElementChild.getBoundingClientRect();
                    return v.width > 0 && v.right <= limit + 1;
                  }),
                  values: rows.map(r => (r.lastElementChild.textContent || '').trim()),
                  clipped: names.some(n => n && n.scrollWidth > n.clientWidth + 1),
                  lines: names.map(n => n ? Math.round(n.getBoundingClientRect().height /
                                     parseFloat(getComputedStyle(n).lineHeight)) : 0)};
        }""", long_name)
        assert not state.get('missing'), f'{width}px: the breakdown rendered — {state}'
        assert state['rows'] == 3, f'{width}px: project header plus two activities — {state}'
        assert state['sectionW'] <= state['cardW'], \
            f'{width}px: the section must fit its card — {state}'
        assert state['docOver'] <= 1, f'{width}px: the page must not scroll sideways — {state}'
        assert state['valuesVisible'], \
            f'{width}px: every row keeps its duration next to a long name — {state["values"]}'
        assert not state['clipped'], f'{width}px: the long name wraps, it is not cut — {state}'
        assert state['lines'][-1] > 1, \
            f'{width}px: the unbroken word really had to wrap — {state["lines"]}'
        assert state['values'] == ['3 ч, 100.0%', '1 ч, 33.3%', '2 ч, 66.7%'], \
            f'{width}px: every row reads its own duration and share — {state["values"]}'
        assert not errors, errors
        await context.close()


async def check_the_new_project_gets_a_free_color(browser):
    """The form opened on one constant colour, so every project created
    without touching the picker came out the same purple and the dot beside
    its name identified nothing. The form now opens on the first palette
    colour this team is not already wearing — and the swatch shows exactly
    what will be saved."""
    in_use = '#7c3aed'
    for offered in ('#6366f1', '#14b8a6'):
        html = boot('/projects/new', {
            'Lang': 'ru', 'Active': 'projects', 'NewProject': True, 'ReactApp': True,
            'CSRFToken': 'test', 'Currencies': [], 'TeamCurrency': 'RUB',
            'SuggestedColor': offered,
        }, True)
        context, page, errors = await open_html(browser, html, '/projects/new', 1440)
        await page.locator('#main h1').wait_for()
        await page.wait_for_timeout(400)
        shown = await page.evaluate("""() => {
          const hex = document.querySelector('input[name="color"][pattern]');
          const pick = document.querySelector('input[type="color"]');
          return {hex: hex && hex.value, picker: pick && pick.value};
        }""")
        assert shown['hex'] == offered, f'the form opens on the offered colour — {shown}'
        assert shown['picker'] == offered, f'the swatch shows the colour that will be saved — {shown}'
        assert shown['hex'] != in_use, \
            f'a new project must not open on {in_use}, the colour already in use'
        assert not errors, errors
        await context.close()


async def check_colour_dots_sit_on_the_first_line(browser):
    """The activity dot is a label for the name beside it, so it belongs on the
    first line — where the eye starts. Inside a centred flex row it floated to
    the middle of the block: measured on a three-line name, 20px below the
    first line. The palette itself is fine — no pair under ΔE 10 — so this is
    placement, not colour choice."""
    long_name = 'Отладкаинтеграционногоконтураучётарабочевременипереносданных'
    html = stats_document([])
    start = html.index('<div id="react-page-data" hidden>') + len('<div id="react-page-data" hidden>')
    end = html.index('</div>', start)
    payload = json.loads(html[start:end])
    payload['data']['ByProject'] = [{
        'ProjectID': 1, 'ProjectName': 'Сайт для Nordwind', 'Slug': 'nordwind',
        'Color': '#7c3aed', 'Duration': '3 ч', 'Share': 100,
        'Activities': [
            {'ActivityName': 'Проверка заголовков', 'Color': '#84cc16', 'Duration': '1 ч', 'Share': 33.3},
            {'ActivityName': long_name, 'Color': '#6366f1', 'Duration': '2 ч', 'Share': 66.7},
        ]}]
    html = html[:start] + json.dumps(payload, ensure_ascii=False) + html[end:]

    for width in (320, 390, 1440):
        context, page, errors = await open_html(browser, html, '/stats', width)
        await page.get_by_role('heading', name='Статистика').wait_for()
        await page.wait_for_timeout(600)
        dots = await page.evaluate("""() => {
          const out = [];
          for (const section of document.querySelectorAll('main .rounded-md.border')) {
            for (const row of section.children) {
              if (row.tagName !== 'DIV') continue;
              const label = row.firstElementChild;
              if (!label) continue;
              const dot = label.querySelector('span[style*="background-color"]');
              const name = label.querySelector('span:last-child, a:last-child');
              if (!dot || !name) continue;
              const d = dot.getBoundingClientRect();
              const n = name.getBoundingClientRect();
              const line = parseFloat(getComputedStyle(name).lineHeight);
              out.push({name: name.textContent.trim().slice(0, 24),
                        lines: Math.round(n.height / line),
                        offset: Math.round(d.top + d.height / 2 - n.top - line / 2)});
            }
          }
          return out;
        }""")
        assert dots, f'{width}px: the rows are on screen'
        for dot in dots:
            assert abs(dot['offset']) <= 1, \
                f'{width}px: «{dot["name"]}» — the dot sits {dot["offset"]}px off the first line — {dot}'
        assert any(dot['lines'] > 1 for dot in dots) or width >= 1440, \
            f'{width}px: a name really did wrap, or this check proves nothing — {dots}'
        assert not errors, errors
        await context.close()


def goals_document(name):
    """Goals booted with one goal on a name long enough to wrap."""
    return boot('/goals', {
        'Lang': 'ru', 'Active': 'goals', 'CSRFToken': 'test', 'CanManage': True,
        'GoalsReact': True,
        'Activities': [{'ID': 1, 'Name': name, 'Color': '#10b981', 'ProjectID': 0}],
        'Goals': [{'ID': 1, 'ActivityName': name, 'Period': 'weekly',
                   'TargetMinutes': 120, 'TargetLabel': '2 ч',
                   'AchievedMinutes': 0, 'AchievedLabel': '0 мин', 'Percent': 0,
                   'PeriodStartLabel': '5 окт', 'PeriodEndLabel': '12 окт',
                   'PeriodRangeLabel': 'на этой неделе', 'Color': '#10b981'}],
    }, True)


def week_grid_document(name):
    """The week grid on a wide screen, with one name long enough to wrap."""
    html = timesheet_document({"3": "vnutrennie"})
    start = html.index('<div id="react-page-data" hidden>') + len('<div id="react-page-data" hidden>')
    end = html.index('</div>', start)
    payload = json.loads(html[start:end])
    payload['data']['Rows'][0]['ActivityName'] = name
    return html[:start] + json.dumps(payload, ensure_ascii=False) + html[end:]


DOT_OFFSET = """(needle) => {
  const out = [];
  for (const dot of document.querySelectorAll('#paratrack-react-root span[style*="background-color"]')) {
    const row = dot.parentElement;
    if (!row) continue;
    // текст, который помечает кружок: первый крупный элемент рядом либо
    // прямой текстовый узел родителя (как в целях)
    let text = null;
    for (const k of row.querySelectorAll('*')) {
      if (k === dot) continue;
      const r = k.getBoundingClientRect();
      if (r.width > 0 && r.height > 0 && (!text || r.height > text.h)) text = {el: k, h: r.height};
    }
    const bare = [...row.childNodes].find(n => n.nodeType === 3 && n.textContent.trim());
    if (!text && bare) {
      const r = document.createRange(); r.selectNodeContents(bare);
      const box = r.getBoundingClientRect();
      text = {el: null, h: box.height, box};
    } else if (text) {
      text.box = text.el.getBoundingClientRect();
    }
    if (!text || text.h === 0) continue;
    const line = parseFloat(getComputedStyle(text.el || row).lineHeight) || text.h;
    const d = dot.getBoundingClientRect();
    out.push({label: ((text.el || bare).textContent || '').trim().slice(0, 24),
              lines: Math.round(text.h / line),
              offset: Math.round(d.top + d.height / 2 - text.box.top - line / 2)});
  }
  return out;
}"""


async def check_the_dot_sits_on_the_first_line_everywhere(browser):
    """The dot labels the name beside it, so it belongs on the first line —
    where the eye starts. Inside a centred flex row it floated to the middle
    of the block: measured at 20px below the first line on a three-line name.
    All three places that had it centred are checked here — the breakdown,
    the week grid and the goals — so a later edit cannot quietly return one of
    them to the middle."""
    long_name = 'Ревизия технического задания и согласование сметы по интеграционному контуру учёта рабочего времени'
    cases = [
        ('разбивка', stats_document([]), '/stats', (320, 390, 1440)),
        ('неделя', week_grid_document(long_name), '/timesheet', (1280, 1440)),
        ('цели', goals_document(long_name), '/goals', (320, 390, 1440)),
    ]
    for label, html, path, widths in cases:
        if label == 'разбивка':
            start = html.index('<div id="react-page-data" hidden>') + len('<div id="react-page-data" hidden>')
            end = html.index('</div>', start)
            payload = json.loads(html[start:end])
            payload['data']['ByProject'] = [{
                'ProjectID': 1, 'ProjectName': 'Сайт для Nordwind', 'Slug': 'nordwind',
                'Color': '#7c3aed', 'Duration': '3 ч', 'Share': 100,
                'Activities': [{'ActivityName': long_name, 'Color': '#6366f1',
                                'Duration': '2 ч', 'Share': 100}]}]
            html = html[:start] + json.dumps(payload, ensure_ascii=False) + html[end:]
        for width in widths:
            context, page, errors = await open_html(browser, html, path, width)
            await page.locator('#main h1').wait_for()
            await page.wait_for_timeout(700)
            dots = await page.evaluate(DOT_OFFSET, long_name)
            assert dots, f'{label} @ {width}px: dots are on screen'
            for dot in dots:
                assert abs(dot['offset']) <= 1, \
                    f'{label} @ {width}px: «{dot["label"]}» — the dot sits {dot["offset"]}px off the first line'
            assert not errors, errors
            await context.close()


async def check_material_motion_and_active_state(browser):
    """Two more Material 3 rules the phone was not following.

    Motion: every transition ran on the Material 2 curve
    `cubic-bezier(0.4, 0, 0.2, 1)`, inherited from the component defaults
    rather than chosen here. M3 replaced it with emphasized easing.

    The active destination: M3 marks it twice — the icon takes weight and the
    item takes an indicator pill (56×32dp in Expressive, 64dp in the baseline).
    The app only coloured the label, and every icon stayed the same weight."""
    for path in ('/', '/stats'):
        context, page, errors = await open_html(browser, document(path), path, 390)
        await page.locator('#main h1').wait_for()
        await page.wait_for_timeout(600)
        state = await page.evaluate("""() => {
          const curves = new Set();
          for (const el of document.querySelectorAll('#paratrack-react-root *')) {
            const cs = getComputedStyle(el);
            if (cs.transitionDuration && cs.transitionDuration !== '0s') {
              curves.add(cs.transitionTimingFunction);
            }
          }
          const items = [...document.querySelectorAll('[data-mobile-nav] a, [data-mobile-nav] button')];
          const isActive = i => i.getAttribute('aria-current') === 'page' || i.className.includes('active');
          const active = items.find(isActive);
          const inactive = items.find(i => !isActive(i));
          const svg = el => el.querySelector('svg');
          const pill = active ? getComputedStyle(active, '::before') : null;
          return {curves: [...curves],
                  activeLabel: active ? (active.innerText || '').trim() : null,
                  activeStroke: active && svg(active) ? getComputedStyle(svg(active)).strokeWidth : null,
                  inactiveStroke: inactive && svg(inactive) ? getComputedStyle(svg(inactive)).strokeWidth : null,
                  pillWidth: pill ? pill.width : null, pillHeight: pill ? pill.height : null,
                  pillContent: pill ? pill.content : null};
        }""")
        assert state['curves'], f'{path}: the screen has transitions'
        for curve in state['curves']:
            assert curve == 'cubic-bezier(0.2, 0, 0, 1)', \
                f'{path}: Material 3 emphasized easing, not {curve}'
        assert state['activeLabel'], f'{path}: one destination is active'
        assert state['pillContent'] != 'none', \
            f'{path}: the active destination needs its indicator pill — {state}'
        assert state['pillWidth'] == '56px' and state['pillHeight'] == '32px', \
            f'{path}: the indicator is 56×32dp in Expressive — {state["pillWidth"]}×{state["pillHeight"]}'
        heavier = float(state['activeStroke'].replace('px', ''))
        lighter = float(state['inactiveStroke'].replace('px', ''))
        assert heavier > lighter, \
            f'{path}: the active icon carries weight, not just colour — {heavier} vs {lighter}'
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
        print('PASS floating action only where there is no primary action of its own, 56dp')
        await check_one_touch_floor_everywhere(browser)
        print('PASS 48dp everywhere, including the bar that lives outside the React root')
        await check_press_is_a_state_layer(browser)
        print('PASS a press is a state layer, never a scale or a dim')
        await check_material_motion_and_active_state(browser)
        print('PASS Material 3 easing, and the active destination takes weight plus a 56×32dp pill')
        await check_touch_metrics(browser)
        print('PASS 320px: fields are 16px and every control reaches the 48px floor')
        await check_desktop_is_untouched(browser)
        print('PASS desktop keeps the drawer and gains no phone furniture')
        await check_confirmation_button_stays_reachable(browser)
        print('PASS a long confirmation keeps its button on screen and scrolls the message')
        await check_member_card_fits(browser)
        print('PASS 320/390px: nothing leaves a member card, «Сохранить» stays whole')
        await check_one_navigation_at_every_width(browser)
        print('PASS 767–1023px: the drawer is an overlay, so exactly one navigation is on screen')
        await check_long_names_stay_readable(browser)
        print('PASS 131-character names stay readable: wrapped or ellipsis, never sliced')
        await check_running_projects_are_readable(browser)
        print('PASS 320/390px: «Идут сейчас» names its activity and its project in full')
        await check_the_minutes_column_lines_up(browser)
        print('PASS 320/390px: the minutes column lines up however the name wraps')
        await check_the_schedule_never_scrolls_sideways(browser)
        print('PASS the schedule is a grid or a list of days — the last sideways scroll is gone')
        await check_the_breakdown_holds_a_long_name(browser)
        print('PASS 320/390px: a 60-character name keeps every row\'s duration on screen')
        await check_the_new_project_gets_a_free_color(browser)
        print('PASS a new project opens on a colour the team is not already wearing')
        await check_the_dot_sits_on_the_first_line_everywhere(browser)
        print('PASS breakdown, week and goals: the dot sits on the first line, whatever the name does')
        await check_the_week_never_scrolls_sideways(browser)
        print('PASS the week is a grid or a list of days — never a sideways scroll, at any width')
        await check_the_mode_says_what_it_opens(browser)
        print('PASS the mode line says what it opens: 5 of 9 sections')
        await check_collapsed_rail_scrolls(browser)
        print('PASS collapsed rail at 720px tall: every icon reachable, continuation announced')
        await browser.close()


if __name__ == '__main__':
    asyncio.run(main())