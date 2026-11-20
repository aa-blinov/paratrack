"""One SSO round trip against the local Dex provider: start at paratrack's
/sso/login, land on Dex's login form, sign in, land back wherever paratrack
sends. Prints the final URL, whether a session cookie exists and the title."""
import sys
from playwright.sync_api import sync_playwright

base = "http://127.0.0.1:8890"
email = sys.argv[1]
password = sys.argv[2]

with sync_playwright() as p:
    browser = p.chromium.launch()
    ctx = browser.new_context()
    page = ctx.new_page()
    page.goto(base + "/sso/login")
    try:
        page.wait_for_selector("input#login, input[name='login']", timeout=15000)
    except Exception:
        print("NO_DEX_LOGIN_PAGE url=" + page.url)
        sys.exit(2)
    for sel in ["input#login", "input[name='login']"]:
        if page.locator(sel).count():
            page.fill(sel, email)
            break
    for sel in ["input#password", "input[name='password']"]:
        if page.locator(sel).count():
            page.fill(sel, password)
            break
    page.click("button[type='submit']")
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(700)
    print("FINAL_URL: " + page.url)
    cookies = {c["name"] for c in ctx.cookies()}
    print("HAS_SESSION: " + str("paratrack_session" in cookies))
    print("TITLE: " + page.title())
    browser.close()
