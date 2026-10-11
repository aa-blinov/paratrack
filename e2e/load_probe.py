"""Load probe: what does the server do when several people arrive at once?

Deliberately not a browser. A browser probe measures Playwright and page
rendering as much as the server — every "visitor" is a full page with its own
render cost, so the numbers describe the harness, not the application. This
uses the standard library only, keeps a real session cookie per simulated
person, and reports what the server actually does under concurrency.

It answers three questions: does anything break, how slow does it get, and
how many requests per second it absorbs.

    python e2e/load_probe.py --users 25 --rounds 5

Run it against a stand, never production: it creates accounts and sessions.
"""

from __future__ import annotations

import argparse
import http.cookiejar
import json
import os
import re
import statistics
import threading
import time
import urllib.error
import urllib.parse
import urllib.request

# What a first-time visitor reads before doing anything, and the write every
# visitor performs. A dashboard alone would not exercise the write path.
READS = ["/", "/stats?period=month", "/timesheet", "/graph", "/projects"]

CSRF_RE = re.compile(r'name="csrf_token"\s+value="([^"]+)"')


class Visitor(threading.Thread):
    """One simulated person: register, then read screens and start timers."""

    def __init__(self, base: str, index: int, rounds: int, out: list, lock: threading.Lock):
        super().__init__(daemon=True)
        self.base = base
        self.index = index
        self.rounds = rounds
        self.out = out
        self.lock = lock
        self.jar = http.cookiejar.CookieJar()
        self.opener = urllib.request.build_opener(
            urllib.request.HTTPCookieProcessor(self.jar),
            NoRedirect(),
        )
        self.token = ""

    def _get(self, path: str):
        req = urllib.request.Request(self.base + path, headers={"Accept": "text/html"})
        try:
            with self.opener.open(req, timeout=30) as resp:
                return resp.status, resp.read().decode("utf-8", "replace")
        except urllib.error.HTTPError as exc:
            # A brand-new account answers 303 to /welcome. That is the
            # expected redirect, not a failure — NoRedirect is here so the
            # status stays visible, so read it rather than raise.
            return exc.code, exc.read().decode("utf-8", "replace")

    def _post(self, path: str, form: dict):
        data = urllib.parse.urlencode(form).encode()
        req = urllib.request.Request(
            self.base + path, data=data, method="POST",
            headers={"Content-Type": "application/x-www-form-urlencoded"},
        )
        try:
            with self.opener.open(req, timeout=30) as resp:
                return resp.status, resp.read().decode("utf-8", "replace")
        except urllib.error.HTTPError as exc:
            return exc.code, exc.read().decode("utf-8", "replace")

    def record(self, name: str, ms: float, status: int):
        with self.lock:
            self.out.append((name, ms, status))

    def run(self):
        try:
            stamp = f"{self.index}-{int(time.time() * 1000) % 10_000_000}"
            t = time.perf_counter()
            _, body = self._get("/register")
            m = CSRF_RE.search(body)
            self.token = m.group(1) if m else ""
            status, _ = self._post("/api/register", {
                "csrf_token": self.token, "name": f"Load {self.index}",
                "email": f"load-{stamp}@load.test", "password": "longenoughpw",
            })
            self.record("регистрация", (time.perf_counter() - t) * 1000, status)

            for path in READS:
                t = time.perf_counter()
                status, _ = self._get(path)
                self.record(path, (time.perf_counter() - t) * 1000, status)

            for _ in range(self.rounds):
                t = time.perf_counter()
                self._get("/")
                status, _ = self._post("/api/start", {
                    "csrf_token": self.token, "activity": f"нагрузка-{self.index}",
                })
                self.record("старт таймера", (time.perf_counter() - t) * 1000, status)
        except Exception as exc:  # noqa: BLE001 - a probe records failures, not raises
            with self.lock:
                self.out.append((f"СБОЙ visitor {self.index}: {exc}", 0.0, 0))


class NoRedirect(urllib.request.HTTPRedirectHandler):
    """Registration answers 303; following it would hide the real status."""

    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def percentile(values, q):
    ordered = sorted(values)
    return ordered[min(len(ordered) - 1, int(len(ordered) * q))]


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--users", type=int, default=20)
    ap.add_argument("--rounds", type=int, default=5)
    ap.add_argument("--base", default=os.environ.get("PARATRACK_BASE", "http://127.0.0.1:8888"))
    args = ap.parse_args()
    base = args.base.rstrip("/")

    print(f"нагрузка: {args.users} одновременных, {args.rounds} раундов записи")
    print(f"база: {base}\n")

    results: list = []
    lock = threading.Lock()
    started = time.perf_counter()
    threads = [Visitor(base, i, args.rounds, results, lock) for i in range(args.users)]
    for th in threads:
        th.start()
    for th in threads:
        th.join()
    elapsed = time.perf_counter() - started

    failures = [r for r in results if r[0].startswith("СБОЙ")]
    errors = [r for r in results if not r[0].startswith("СБОЙ") and r[2] >= 500]
    good = [r for r in results if not r[0].startswith("СБОЙ") and r[2] < 500]

    print(f"за {elapsed:.1f}с: {len(good)} успешных, 5xx: {len(errors)}, сбоев: {len(failures)}")
    print(f"пропускная способность: {len(good) / elapsed:.0f} запросов/с\n")

    groups: dict[str, list[float]] = {}
    for name, ms, _ in good:
        groups.setdefault(name, []).append(ms)
    print(f"{'операция':<26} {'n':>4} {'p50':>9} {'p95':>9} {'max':>9}")
    for name, values in sorted(groups.items(), key=lambda kv: -len(kv[1])):
        print(f"{name:<26} {len(values):>4} {statistics.median(values):>8.0f}мс "
              f"{percentile(values, 0.95):>8.0f}мс {max(values):>8.0f}мс")

    for f in (failures + errors)[:10]:
        print(f"   ! {f[0]} -> {f[2]}")
    slow = [r for r in good if r[1] > 2000 and not r[0].startswith("регистрация")]
    if slow:
        print(f"\n⚠ медленных ответов (>2с): {len(slow)} из {len(good)}")
    return 1 if (failures or errors) else 0


if __name__ == "__main__":
    raise SystemExit(main())