"""Keyboard menus and confirmation cancel/accept. All requests mocked."""
import asyncio,json
from urllib.parse import urlparse
from playwright.async_api import async_playwright,expect
from test_react_navigation import document,STATIC
from test_screen_layout import invoice_document

async def main():
 async with async_playwright() as p:
  browser=await p.chromium.launch()
  for width in [1440,390]:
   ctx=await browser.new_context(viewport={'width':width,'height':900});page=await ctx.new_page();posts=[];errors=[]
   page.on('pageerror',lambda e:errors.append(str(e)))
   async def route(r):
    path=urlparse(r.request.url).path
    if path.startswith('/static/'):await r.fulfill(path=str(STATIC/path.removeprefix('/static/')))
    elif r.request.method=='POST':posts.append(path);await r.fulfill(status=204)
    else:
     html=invoice_document();start=html.index('<div id="react-page-data" hidden>')+len('<div id="react-page-data" hidden>');end=html.index('</div>',start);boot=json.loads(html[start:end]);boot['shell']['userTeams']=[{'id':1,'name':'Моя команда','role':'owner'},{'id':2,'name':'Вторая команда','role':'member'}];html=html[:start]+json.dumps(boot)+html[end:];await r.fulfill(content_type='text/html',body=html)
   await ctx.route('**/*',route);await page.goto('http://paratrack.test/invoices');await page.locator('#main h1').wait_for()
   await page.add_script_tag(url='http://paratrack.test/static/js/app-form-actions.js')
   # Open account menu with keyboard and return focus on Escape.
   if width==1440:
    account=page.locator('.app-shell-actions button');await account.focus();await page.keyboard.press('Enter')
    await page.get_by_role('menu').wait_for();await page.keyboard.press('ArrowDown');assert await page.get_by_role('menuitem').evaluate_all('a=>a.some(e=>e===document.activeElement)')
    await page.keyboard.press('Escape');assert await account.evaluate('e=>e===document.activeElement')
   await page.evaluate("""() => {const f=document.createElement('form');f.style.cssText='position:fixed;top:70px;right:16px;z-index:40';f.method='POST';f.action='/invoices/1/delete';f.dataset.confirm='Удалить счёт?';f.innerHTML='<input type="hidden" name="csrf_token" value="test"><button type="submit">Удалить счёт</button>';document.querySelector('#paratrack-react-root').append(f)}""")
   delete=page.get_by_role('button',name='Удалить счёт',exact=True)
   await delete.click();dialog=page.get_by_role('alertdialog');await dialog.wait_for();await page.screenshot(path=f'/tmp/paratrack-dialog-{width}.png')
   await dialog.get_by_role('button',name='Отмена',exact=True).click();await dialog.wait_for(state='hidden');assert not posts
   await delete.click();await expect(dialog.get_by_role('button',name='Отмена',exact=True)).to_be_focused();await page.keyboard.press('Escape');await dialog.wait_for(state='hidden');assert not posts
   await delete.click();await dialog.get_by_role('button',name='Подтвердить',exact=True).click()
   await page.wait_for_timeout(150);assert posts==['/invoices/1/delete'],posts
   # On a phone the drawer is gone: the switcher lives in the «Ещё» sheet, and
   # there the spaces are listed outright instead of behind a menu.
   if width==390:
    await page.get_by_role('button',name='Ещё',exact=True).click()
    await page.locator('[role="dialog"]').wait_for()
    await page.locator('[role="dialog"] form[action="/api/team/switch"] button',has_text='Вторая команда').click()
   else:
    await page.get_by_role('button',name='Моя команда',exact=True).click()
    await page.get_by_role('menuitem',name='Вторая команда',exact=False).click()
   await page.wait_for_timeout(150);assert posts==['/invoices/1/delete','/api/team/switch'],posts
   assert not errors,errors
   print(f'PASS {width}: keyboard menu, cancelled/accepted confirmation, one POST')
   await ctx.close()
  await browser.close()
asyncio.run(main())
