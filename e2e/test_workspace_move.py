"""Moving a workspace: download it on one account, restore it on another.

The first account downloads the whole workspace from the Export page. The second
account uploads that file on the Import page. The file names the first account's
email, which is registered, so the restore goes through; the second account owns
the restored workspace, and it shows the project the first one had. A file that
is not an export is refused on the page with a reason. The first account's webhook
comes back on the second account with a new secret, shown once, and never the one
the first account typed.
"""
import sys
import time
from pathlib import Path

from playwright.sync_api import expect, sync_playwright

from browser import launch
from target import BASE_URL as BASE
from test_clients import register

failures: list[str] = []
WORK = Path(__file__).resolve().parents[1] / "e2e" / "screenshots"


def check(name: str, ok: bool, detail: str = "") -> None:
    print(f"  [{'PASS' if ok else 'FAIL'}] {name}" + (f" — {detail}" if not ok and detail else ""))
    if not ok:
        failures.append(name)


def main() -> int:
    stamp = int(time.time())
    project = f"Переезд {stamp}"
    hook_url = f"https://hooks.example.com/move-{stamp}"
    typed_secret = f"typed-secret-{stamp}"
    download = Path(f"/tmp/paratrack-move-{stamp}.json")
    with sync_playwright() as p:
        browser = launch(p)

        print("\n== 1. The first account downloads its workspace")
        first = browser.new_context(viewport={"width": 1280, "height": 900})
        page = first.new_page()
        register(page, first, "Первый владелец", f"move-first{stamp}@example.com", "move-qa-pass-2026")
        page.goto(f"{BASE}/projects/new", wait_until="networkidle")
        page.fill('form[action="/projects/new"] input[name="name"]', project)
        page.locator('form[action="/projects/new"] button[type="submit"]').first.click()
        page.wait_for_load_state("networkidle")
        page.goto(f"{BASE}/settings/webhooks", wait_until="networkidle")
        page.fill('form[action="/api/webhooks"] input[name="url"]', hook_url)
        page.fill('form[action="/api/webhooks"] input[name="secret"]', typed_secret)
        page.locator('form[action="/api/webhooks"] button[type="submit"]').first.click()
        page.wait_for_load_state("networkidle")
        check("the first account's webhook is listed", page.get_by_text(hook_url).count() > 0)
        page.goto(f"{BASE}/export", wait_until="networkidle")
        expect(page.get_by_text("Всё пространство")).to_be_visible()
        with page.expect_download() as info:
            page.get_by_role("link", name="Скачать JSON").click()
        info.value.save_as(str(download))
        check("the download is a file of the workspace", download.exists() and download.stat().st_size > 200)
        page.screenshot(path=str(WORK / "tasks-move-export.png"), full_page=True)

        print("\n== 2. The second account restores it from the Import page")
        second = browser.new_context(viewport={"width": 1280, "height": 900})
        target = second.new_page()
        register(target, second, "Второй владелец", f"move-second{stamp}@example.com", "move-qa-pass-2026")
        target.goto(f"{BASE}/import", wait_until="networkidle")
        expect(target.get_by_text("Восстановить пространство из файла")).to_be_visible()
        target.set_input_files("#workspace-file", str(Path("/tmp") / f"paratrack-move-{stamp}.json"))
        target.get_by_role("button", name="Восстановить").click()
        expect(target.get_by_text("Вебхуки восстановлены с новыми секретами")).to_be_visible(timeout=30000)
        secret = target.locator("code").first.inner_text().strip()
        check("the restored webhook has a new secret, not the typed one", len(secret) >= 32 and typed_secret not in secret)
        check("the secret screen names the restored webhook", target.get_by_text(hook_url).count() > 0)
        target.screenshot(path=str(WORK / "tasks-move-secrets.png"), full_page=True)
        target.get_by_role("button", name="Открыть пространство").click()
        target.wait_for_url(lambda url: "/import" not in url, timeout=30000)
        target.wait_for_load_state("networkidle")
        check("the restored workspace opens from the secrets screen", "/import" not in target.url, target.url)
        target.goto(f"{BASE}/projects", wait_until="networkidle")
        check("the restored workspace has the project", target.get_by_text(project).count() > 0)
        target.screenshot(path=str(WORK / "tasks-move-restored.png"), full_page=True)

        print("\n== 3. A file that is not an export is refused on the page")
        bad = Path("/tmp") / f"paratrack-move-bad-{stamp}.json"
        bad.write_text('{"hello": "world"}', encoding="utf-8")
        target.goto(f"{BASE}/import", wait_until="networkidle")
        target.set_input_files("#workspace-file", str(bad))
        target.get_by_role("button", name="Восстановить").click()
        expect(target.get_by_role("alert")).to_contain_text("Это не файл выгрузки")
        check("a refused file says why on the page", target.get_by_role("alert").count() > 0)

        browser.close()

    print(f"\n{len(failures)} failure(s)" if failures else "\nall move checks passed")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
