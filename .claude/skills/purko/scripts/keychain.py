"""OS keychain integration for purko-skill token storage.

macOS: uses the `security` CLI (Keychain Services) via subprocess arg list —
never through shell interpolation.

Non-macOS / env override (PURKO_SKILL_KEYCHAIN_BACKEND=file): stores tokens
as 0600 files under ~/.config/purko-skill/tokens/<target>.token.

Security note: on macOS the subprocess argv is briefly visible in `ps` output
while the `security` process runs. This is an acceptable local trade-off —
the OS keychain still beats an on-disk plaintext file for credential at rest.
"""
import os
import subprocess
import sys
from pathlib import Path

# Env var to force the file backend (used by tests to avoid touching the real keychain).
_BACKEND = os.environ.get("PURKO_SKILL_KEYCHAIN_BACKEND", "")

_TOKEN_DIR = Path(
    os.environ.get("PURKO_SKILL_CONFIG_DIR",
                   str(Path.home() / ".config" / "purko-skill"))
) / "tokens"

_ACCOUNT = "purko-skill"
_SERVICE_PREFIX = "purko-"

import re as _re

_TARGET_RE = _re.compile(r'^[A-Za-z0-9._-]+$')


def _validate(target_name):
    """Raise ValueError if target_name is not a safe identifier.

    Allowed: characters matching ^[A-Za-z0-9._-]+$
    Rejected: empty, contains '..', or any character outside that set.
    """
    if not _TARGET_RE.match(target_name) or ".." in target_name:
        raise ValueError("invalid target name")


def _use_keychain():
    return sys.platform == "darwin" and _BACKEND != "file"


def _service(target_name):
    return f"{_SERVICE_PREFIX}{target_name}"


# ── macOS Keychain ──────────────────────────────────────────────────────────

def _kc_save(target_name, token):
    # -U: update if the entry already exists (upsert semantics).
    # argv path: token is passed as an argument directly to the subprocess —
    # no shell is involved, so no shell-injection risk. The argv IS briefly
    # visible in `ps` (see module docstring).
    r = subprocess.run(
        ["security", "add-generic-password",
         "-U",
         "-a", _ACCOUNT,
         "-s", _service(target_name),
         "-w", token],
        capture_output=True,
    )
    if r.returncode != 0:
        # Error message intentionally omits the argv (which contains the token).
        raise RuntimeError(f"keychain save failed (security exited {r.returncode})")


def _kc_load(target_name):
    r = subprocess.run(
        ["security", "find-generic-password",
         "-a", _ACCOUNT,
         "-s", _service(target_name),
         "-w"],
        capture_output=True, text=True,
    )
    if r.returncode != 0:
        return None
    value = r.stdout.strip()
    return value if value else None


def _kc_delete(target_name):
    subprocess.run(
        ["security", "delete-generic-password",
         "-a", _ACCOUNT,
         "-s", _service(target_name)],
        capture_output=True,
    )


# ── File backend ────────────────────────────────────────────────────────────

def _file_path(target_name):
    return _TOKEN_DIR / f"{target_name}.token"


def _file_save(target_name, token):
    _TOKEN_DIR.mkdir(parents=True, exist_ok=True)
    os.chmod(_TOKEN_DIR, 0o700)  # ensure dir is not group/world readable
    p = _file_path(target_name)
    # Atomic create: open with mode 0o600 from the start — no write_text-then-chmod race.
    fd = os.open(str(p), os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    try:
        os.write(fd, token.encode())
    finally:
        os.close(fd)


def _file_load(target_name):
    try:
        value = _file_path(target_name).read_text().strip()
        return value if value else None
    except FileNotFoundError:
        return None


def _file_delete(target_name):
    try:
        _file_path(target_name).unlink()
    except FileNotFoundError:
        pass


# ── Public API ──────────────────────────────────────────────────────────────

def save_token(target_name, token):
    """Persist a bearer token for the named target in the OS keychain (or file fallback)."""
    _validate(target_name)
    if _use_keychain():
        _kc_save(target_name, token)
    else:
        _file_save(target_name, token)


def load_token(target_name):
    """Return the stored token for the named target, or None if not found."""
    _validate(target_name)
    if _use_keychain():
        return _kc_load(target_name)
    return _file_load(target_name)


def delete_token(target_name):
    """Remove the stored token for the named target (no-op if absent)."""
    _validate(target_name)
    if _use_keychain():
        _kc_delete(target_name)
    else:
        _file_delete(target_name)
