"""Tests for remote upstream-URL mode (spec §7b)."""
import http.client
import importlib
import json
import threading
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

import pytest

WEBAPP = str(Path(__file__).resolve().parent.parent / "webapp")


class EchoUpstream(BaseHTTPRequestHandler):
    """Echoes method, path, Host, Authorization, and X-Purko-CSRF as JSON."""

    def log_message(self, *a):
        pass

    def _echo(self):
        length = int(self.headers.get("Content-Length", 0) or 0)
        body = self.rfile.read(length).decode() if length else ""
        payload = json.dumps({
            "method": self.command,
            "path": self.path,
            "host": self.headers.get("Host", ""),
            "auth": self.headers.get("Authorization", ""),
            "csrf": self.headers.get("X-Purko-CSRF", ""),
            "body": body,
        }).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    do_GET = do_POST = do_PUT = do_DELETE = _echo


def _upstream():
    srv = ThreadingHTTPServer(("127.0.0.1", 0), EchoUpstream)
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    return srv


def _local(upstream_url=None, upstream_port=None):
    import serve
    importlib.reload(serve)
    srv = serve.make_server(WEBAPP, upstream_port=upstream_port, upstream_url=upstream_url)
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    return srv, f"http://127.0.0.1:{srv.server_address[1]}"


# (a) URL-mode GET: base path prepended; Host header set to the URL's host
def test_url_mode_get_prepends_base_path_and_sets_host():
    up = _upstream()
    up_port = up.server_address[1]
    # Use 127.0.0.1 as URL host so the TCP connection actually reaches the fake upstream.
    url = f"http://127.0.0.1:{up_port}/base"
    srv, local_base = _local(upstream_url=url)
    try:
        with urllib.request.urlopen(local_base + "/api/agents", timeout=5) as r:
            data = json.loads(r.read())
        assert data["path"] == "/base/api/agents", f"path was {data['path']!r}"
        # Host must be the URL's host, not stripped (ingress routing depends on it).
        assert data["host"].startswith("127.0.0.1"), f"host was {data['host']!r}"
    finally:
        srv.shutdown()
        up.shutdown()


# (b) Bearer token and CSRF header injected in URL mode when state.token is set
def test_url_mode_bearer_and_csrf_injected():
    up = _upstream()
    up_port = up.server_address[1]
    url = f"http://127.0.0.1:{up_port}/base"
    srv, local_base = _local(upstream_url=url)
    srv.state.token = "remote-tok"
    try:
        with urllib.request.urlopen(local_base + "/api/agents", timeout=5) as r:
            data = json.loads(r.read())
        assert data["auth"] == "Bearer remote-tok"
        assert data["csrf"] == "1"
    finally:
        srv.shutdown()
        up.shutdown()


# (c) https scheme selects HTTPSConnection (monkeypatched — no real TLS)
def test_https_scheme_uses_https_connection(monkeypatch):
    import serve
    importlib.reload(serve)

    captured = []

    class FakeHTTPSConn:
        def __init__(self, host, port, **kwargs):
            captured.append(("HTTPSConnection", host, port))

        def request(self, *a, **kw):
            raise OSError("no real TLS in test")

        def close(self):
            pass

    # serve.py accesses http.client.HTTPSConnection dynamically, so patching the
    # module attribute is sufficient — no re-import needed.
    monkeypatch.setattr(http.client, "HTTPSConnection", FakeHTTPSConn)

    srv = serve.make_server(WEBAPP, upstream_url="https://remote.example.com:8443")
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    local_port = srv.server_address[1]
    try:
        with pytest.raises(urllib.error.HTTPError) as exc_info:
            urllib.request.urlopen(
                f"http://127.0.0.1:{local_port}/api/ping", timeout=5)
        assert exc_info.value.code == 502
    finally:
        srv.shutdown()

    assert captured == [("HTTPSConnection", "remote.example.com", 8443)]


# (d) ftp:// (or any non-http/https scheme) rejected at make_server with a clear error
def test_invalid_scheme_rejected_at_make_server():
    import serve
    importlib.reload(serve)
    with pytest.raises(ValueError, match="scheme"):
        serve.make_server(WEBAPP, upstream_url="ftp://example.com/files")


# (e) setting both upstream_port and upstream_url is a ValueError
def test_port_and_url_mutually_exclusive():
    import serve
    importlib.reload(serve)
    with pytest.raises(ValueError):
        serve.make_server(WEBAPP, upstream_port=8080, upstream_url="http://example.com")


# /local/status reports upstream=True when URL mode is active
def test_url_mode_status_reports_upstream_true():
    up = _upstream()
    up_port = up.server_address[1]
    srv, local_base = _local(upstream_url=f"http://127.0.0.1:{up_port}")
    try:
        with urllib.request.urlopen(local_base + "/local/status", timeout=5) as r:
            data = json.loads(r.read())
        assert data["upstream"] is True
    finally:
        srv.shutdown()
        up.shutdown()
