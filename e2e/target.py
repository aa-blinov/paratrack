"""Shared target configuration for mutating QA scripts."""
from __future__ import annotations

import os

DEFAULT_BASE_URL = "http://127.0.0.1:8888"
BASE_URL = os.environ.get("PARATRACK_BASE", DEFAULT_BASE_URL).rstrip("/")
