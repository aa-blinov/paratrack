"""Seeds the owner account that the tag, notification and webhook e2e scripts sign in with.

The three scripts default to one owner address with a studio set of sections.
A stand that was rebuilt has no such account, so every one of them fails on the
sign-in form. Run this first, against the same stand:

    PARATRACK_BASE=http://127.0.0.1:8890 .venv/bin/python e2e/seed_qa_accounts.py

Running it again is safe: an account that exists is signed into instead.
"""

from __future__ import annotations

import os

from playwright.sync_api import sync_playwright

from seed_member2 import choose_set, register_or_sign_in

OWNER = os.environ.get("PARATRACK_OWNER_EMAIL", "doc-1791228444@x.test")


def main() -> int:
    with sync_playwright() as p:
        browser = p.chromium.launch()
        page = browser.new_context().new_page()
        register_or_sign_in(page, "Doc QA", OWNER)
        choose_set(page)
        print(f"owner ready: {OWNER}")
        browser.close()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
