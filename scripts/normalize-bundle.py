#!/usr/bin/env python3
"""Make the generated UI bundle meet the house style: no trailing whitespace, a final newline.

The bundle is committed and checked by the house-style workflow (HS042, HS043). Vite
writes it as it is; `make ui` runs this once after the build, so the committed files
always pass the check. Whitespace at the end of a line is insignificant in the CSS and
JS the bundle holds, which is why this changes nothing the browser sees.

Usage: scripts/normalize-bundle.py [directory]
"""
import pathlib
import sys

SUFFIXES = {".js", ".css"}


def normalize(text: str) -> str:
    lines = [line.rstrip(" \t") for line in text.split("\n")]
    out = "\n".join(lines)
    if not out.endswith("\n"):
        out += "\n"
    return out


def main() -> int:
    root = pathlib.Path(sys.argv[1] if len(sys.argv) > 1 else "internal/web/static/ui")
    changed = 0
    for path in sorted(root.rglob("*")):
        if not path.is_file() or path.suffix not in SUFFIXES:
            continue
        text = path.read_text(encoding="utf-8")
        fixed = normalize(text)
        if fixed != text:
            path.write_text(fixed, encoding="utf-8")
            changed += 1
    print(f"normalized {changed} bundle file(s) under {root}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
