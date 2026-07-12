"""Interim skill self-version check (stdlib only).

Compares the skill's local VERSION against the canonical copy published in the
purko-oss repo, so an installed skill can tell the user when it is outdated —
until the plugin-marketplace distribution lands and Claude Code handles this
natively.

Design:
  - Best-effort and SILENT-FAIL: any network/parse error yields
    update_available=False. A version check must never block /purko.
  - Cached for CACHE_HOURS in ~/.config/purko-skill/version-cache.json so a
    normal session hits the network at most once a day.
  - The remote URL points at the skill's canonical home
    (.claude/skills/purko/VERSION in purko-io/purko). Override with
    PURKO_SKILL_VERSION_URL for testing or an alternate channel (e.g. a future
    SaaS-hosted endpoint).

CLI: `python3 scripts/version_check.py` prints the result as JSON.
"""

import json
import os
import time
import urllib.request
from pathlib import Path

SKILL_ROOT = Path(__file__).resolve().parent.parent
VERSION_FILE = SKILL_ROOT / "VERSION"

# Canonical published copy (post-move location in purko-io/purko).
DEFAULT_VERSION_URL = (
    "https://raw.githubusercontent.com/purko-io/purko/main/.claude/skills/purko/VERSION"
)
VERSION_URL = os.environ.get("PURKO_SKILL_VERSION_URL", DEFAULT_VERSION_URL)
# Where a human reads what changed.
CHANGELOG_URL = os.environ.get(
    "PURKO_SKILL_CHANGELOG_URL",
    "https://github.com/purko-io/purko/blob/main/.claude/skills/purko/CHANGELOG.md",
)

CACHE_DIR = Path(
    os.environ.get("PURKO_SKILL_CONFIG_DIR", str(Path.home() / ".config" / "purko-skill"))
)
CACHE_PATH = CACHE_DIR / "version-cache.json"
CACHE_HOURS = 24
TIMEOUT_SECONDS = 3


def local_version():
    """The installed skill's version, or '0.0.0' if the VERSION file is absent."""
    try:
        return VERSION_FILE.read_text().strip() or "0.0.0"
    except OSError:
        return "0.0.0"


def _parse(v):
    """Parse a dotted version into a comparable tuple; non-numeric parts sort as -1."""
    out = []
    for part in str(v).strip().lstrip("v").split("."):
        try:
            out.append(int(part))
        except ValueError:
            out.append(-1)
    return tuple(out)


def is_newer(latest, current):
    """True when latest is a strictly higher version than current."""
    return _parse(latest) > _parse(current)


def _read_cache():
    try:
        data = json.loads(CACHE_PATH.read_text())
        if time.time() - data.get("checked_at", 0) < CACHE_HOURS * 3600:
            return data
    except (OSError, ValueError):
        pass
    return None


def _write_cache(latest):
    try:
        CACHE_DIR.mkdir(parents=True, exist_ok=True)
        CACHE_PATH.write_text(json.dumps({"latest": latest, "checked_at": time.time()}))
    except OSError:
        pass  # cache is an optimization; never fatal


def _fetch_latest():
    """Fetch the published VERSION; None on any failure (silent-fail)."""
    try:
        req = urllib.request.Request(VERSION_URL, headers={"User-Agent": "purko-skill"})
        with urllib.request.urlopen(req, timeout=TIMEOUT_SECONDS) as resp:
            return resp.read().decode("utf-8", "replace").strip()[:32] or None
    except Exception:
        return None


def check(force=False):
    """Return the version status. Never raises.

    {
      "local": "1.0.0",
      "latest": "1.1.0" | null,          # null when the remote is unreachable
      "update_available": bool,
      "changelog_url": "...",
      "source": "cache" | "network" | "offline",
    }
    """
    current = local_version()
    result = {
        "local": current,
        "latest": None,
        "update_available": False,
        "changelog_url": CHANGELOG_URL,
        "source": "offline",
    }

    if not force:
        cached = _read_cache()
        if cached is not None:
            latest = cached.get("latest")
            result["latest"] = latest
            result["source"] = "cache"
            result["update_available"] = bool(latest) and is_newer(latest, current)
            return result

    latest = _fetch_latest()
    if latest is None:
        return result  # offline / unreachable → no update surfaced
    _write_cache(latest)
    result["latest"] = latest
    result["source"] = "network"
    result["update_available"] = is_newer(latest, current)
    return result


if __name__ == "__main__":
    print(json.dumps(check(force=bool(os.environ.get("PURKO_VERSION_FORCE")))))
