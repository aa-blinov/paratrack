"""One place to choose the browser an audit runs in.

Everything under e2e/ used to call `p.chromium.launch()` directly, which meant
Firefox was never measured — not once, not on any screen, for the whole life of
the project. The setting existed only as an assumption nobody had checked.

Set PARATRACK_BROWSER to `firefox`, `webkit` or `chromium` (default). The audits
then run wherever asked, and a second engine costs one env var rather than a
rewrite:

    PARATRACK_BROWSER=firefox python e2e/a11y_check.py

Engines differ in ways that matter here and that no amount of Chromium testing
would have caught: Gecko and WebKit resolve `getComputedStyle` differently for
`oklch()`, and Radix data attributes, focus-visible and animation timing are
not the same code in all three.
"""

from __future__ import annotations

import os

ENGINES = ("chromium", "firefox", "webkit")


def engine_name() -> str:
    """The engine to launch, from PARATRACK_BROWSER, validated."""
    name = os.environ.get("PARATRACK_BROWSER", "chromium").strip().lower()
    if name not in ENGINES:
        raise SystemExit(
            f"PARATRACK_BROWSER={name!r} is not one of {ENGINES}. "
            "Install the engine first: python -m playwright install <name>"
        )
    return name


def launch(playwright, **kwargs):
    """Launch the configured engine."""
    return getattr(playwright, engine_name()).launch(**kwargs)


def new_context_kwargs(**overrides) -> dict:
    """
    Defaults every engine can honour.

    Chromium is the only one of the three that takes `is_mobile`; Firefox and
    WebKit reject it. Passing it blindly is how cross-browser runs die on an
    option rather than on a finding.
    """
    kwargs = {"viewport": {"width": 1280, "height": 900}}
    kwargs.update(overrides)
    if engine_name() != "chromium":
        for chromium_only in ("is_mobile", "has_touch", "device_scale_factor"):
            kwargs.pop(chromium_only, None)
    return kwargs