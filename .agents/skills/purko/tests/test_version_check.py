"""Tests for the interim skill self-version check (scripts/version_check.py)."""

import http.server
import json
import os
import socketserver
import sys
import threading
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "scripts"))
import version_check as vc  # noqa: E402


def test_is_newer():
    assert vc.is_newer("1.1.0", "1.0.0")
    assert vc.is_newer("2.0.0", "1.9.9")
    assert vc.is_newer("1.0.1", "1.0.0")
    assert not vc.is_newer("1.0.0", "1.0.0")
    assert not vc.is_newer("1.0.0", "1.1.0")
    # v-prefix and ragged lengths are tolerated
    assert vc.is_newer("v1.2", "1.1.9")
    assert not vc.is_newer("1.0", "1.0.0")


def _serve(body, tmp_path):
    """Start a one-file HTTP server returning `body`; return (url, stop)."""
    d = tmp_path / "srv"
    d.mkdir()
    (d / "VERSION").write_text(body)

    handler = lambda *a, **k: http.server.SimpleHTTPRequestHandler(*a, directory=str(d), **k)
    httpd = socketserver.TCPServer(("127.0.0.1", 0), handler)
    port = httpd.server_address[1]
    threading.Thread(target=httpd.serve_forever, daemon=True).start()
    return f"http://127.0.0.1:{port}/VERSION", httpd.shutdown


def test_update_available_then_cached(tmp_path, monkeypatch):
    url, stop = _serve("9.9.9\n", tmp_path)
    try:
        monkeypatch.setenv("PURKO_SKILL_CONFIG_DIR", str(tmp_path / "cfg"))
        monkeypatch.setattr(vc, "VERSION_URL", url)
        monkeypatch.setattr(vc, "CACHE_DIR", tmp_path / "cfg")
        monkeypatch.setattr(vc, "CACHE_PATH", tmp_path / "cfg" / "version-cache.json")
        monkeypatch.setattr(vc, "local_version", lambda: "1.0.0")

        r = vc.check(force=True)
        assert r["update_available"] is True
        assert r["latest"] == "9.9.9"
        assert r["source"] == "network"

        # second call (no force) is served from cache, no network needed
        r2 = vc.check()
        assert r2["source"] == "cache"
        assert r2["update_available"] is True
    finally:
        stop()


def test_no_update_when_current(tmp_path, monkeypatch):
    url, stop = _serve("1.0.0\n", tmp_path)
    try:
        monkeypatch.setattr(vc, "VERSION_URL", url)
        monkeypatch.setattr(vc, "CACHE_PATH", tmp_path / "nocache.json")
        monkeypatch.setattr(vc, "CACHE_DIR", tmp_path)
        monkeypatch.setattr(vc, "local_version", lambda: "1.0.0")
        r = vc.check(force=True)
        assert r["update_available"] is False
        assert r["latest"] == "1.0.0"
    finally:
        stop()


def test_offline_is_silent(tmp_path, monkeypatch):
    monkeypatch.setattr(vc, "VERSION_URL", "http://127.0.0.1:1/nope")
    monkeypatch.setattr(vc, "CACHE_PATH", tmp_path / "c.json")
    monkeypatch.setattr(vc, "CACHE_DIR", tmp_path)
    monkeypatch.setattr(vc, "local_version", lambda: "1.0.0")
    r = vc.check(force=True)
    assert r["update_available"] is False
    assert r["latest"] is None
    assert r["source"] == "offline"


def test_version_file_matches_changelog_top():
    """The shipped VERSION must have a matching CHANGELOG entry."""
    root = Path(__file__).resolve().parent.parent
    v = (root / "VERSION").read_text().strip()
    changelog = (root / "CHANGELOG.md").read_text()
    assert f"## {v}" in changelog, f"CHANGELOG.md missing an entry for {v}"
