import importlib
import json
import threading
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

WEBAPP = str(Path(__file__).resolve().parent.parent / "webapp")


class FakeUpstream(BaseHTTPRequestHandler):
    """Echoes method, path, selected headers, and body as JSON."""
    def log_message(self, *a):
        pass

    def _echo(self):
        length = int(self.headers.get("Content-Length", 0) or 0)
        body = self.rfile.read(length).decode() if length else ""
        payload = json.dumps({
            "method": self.command,
            "path": self.path,
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
    srv = ThreadingHTTPServer(("127.0.0.1", 0), FakeUpstream)
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    return srv


def _local(upstream_port):
    import serve
    importlib.reload(serve)
    srv = serve.make_server(WEBAPP, upstream_port)
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    return srv, f"http://127.0.0.1:{srv.server_address[1]}"


def test_get_is_proxied_with_path():
    up = _upstream()
    srv, base = _local(up.server_address[1])
    try:
        with urllib.request.urlopen(base + "/api/agents?x=1", timeout=5) as r:
            data = json.loads(r.read())
        assert data["method"] == "GET" and data["path"] == "/api/agents?x=1"
        assert data["auth"] == ""  # no token set
    finally:
        srv.shutdown(); up.shutdown()


def test_post_body_and_bearer_and_csrf_injected():
    up = _upstream()
    srv, base = _local(up.server_address[1])
    srv.state.token = "tok123"
    try:
        req = urllib.request.Request(
            base + "/api/approve/wf/step", data=b"", method="POST")
        with urllib.request.urlopen(req, timeout=5) as r:
            data = json.loads(r.read())
        assert data["auth"] == "Bearer tok123"
        assert data["csrf"] == "1"
        assert data["method"] == "POST"
    finally:
        srv.shutdown(); up.shutdown()


def test_upstream_down_gives_502():
    up = _upstream()
    dead_port = up.server_address[1]
    up.shutdown()
    up.server_close()  # actually free the port so the proxy gets ConnectionRefused
    srv, base = _local(dead_port)
    try:
        try:
            urllib.request.urlopen(base + "/api/agents", timeout=5)
            code = 200
        except urllib.error.HTTPError as e:
            code = e.code
        assert code == 502
    finally:
        srv.shutdown()


def test_no_upstream_gives_503():
    srv, base = _local(None)
    try:
        try:
            urllib.request.urlopen(base + "/api/agents", timeout=5)
            code = 200
        except urllib.error.HTTPError as e:
            code = e.code
        assert code == 503
    finally:
        srv.shutdown()


class ChunkedUpstream(BaseHTTPRequestHandler):
    """HTTP/1.1 upstream that sends a two-chunk chunked-encoded body."""
    protocol_version = "HTTP/1.1"

    def log_message(self, *a):
        pass

    def do_GET(self):
        chunk1 = b"hello"
        chunk2 = b" world"
        body = (
            f"{len(chunk1):x}\r\n".encode() + chunk1 + b"\r\n"
            + f"{len(chunk2):x}\r\n".encode() + chunk2 + b"\r\n"
            + b"0\r\n\r\n"
        )
        self.send_response(200)
        self.send_header("Transfer-Encoding", "chunked")
        self.send_header("Content-Type", "text/plain")
        self.end_headers()
        self.wfile.write(body)
        self.wfile.flush()


def test_chunked_upstream_body_is_decoded():
    up = ThreadingHTTPServer(("127.0.0.1", 0), ChunkedUpstream)
    threading.Thread(target=up.serve_forever, daemon=True).start()
    srv, base = _local(up.server_address[1])
    try:
        with urllib.request.urlopen(base + "/api/data", timeout=5) as r:
            body = r.read()
        # Body must be the decoded payload — no hex size markers or bare CRLFs
        assert body == b"hello world", repr(body)
        assert b"\r\n" not in body
        assert b"5\r\n" not in body  # no chunk-size lines leaked through
    finally:
        srv.shutdown(); up.shutdown()


class RecordingUpstream(BaseHTTPRequestHandler):
    """Records calls so tests can assert the upstream was (not) reached."""
    received = []

    def log_message(self, *a):
        pass

    def _record(self):
        length = int(self.headers.get("Content-Length", 0) or 0)
        body = self.rfile.read(length).decode() if length else ""
        RecordingUpstream.received.append({
            "method": self.command,
            "path": self.path,
        })
        payload = json.dumps({"ok": True}).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    do_GET = do_POST = do_PUT = do_DELETE = _record


def test_post_cross_origin_rejected_upstream_never_called():
    RecordingUpstream.received = []
    up = ThreadingHTTPServer(("127.0.0.1", 0), RecordingUpstream)
    threading.Thread(target=up.serve_forever, daemon=True).start()
    srv, base = _local(up.server_address[1])
    srv.state.token = "tok"
    try:
        req = urllib.request.Request(
            base + "/api/approve/x/y", data=b"", method="POST",
            headers={"Origin": "https://evil.example"},
        )
        try:
            urllib.request.urlopen(req, timeout=5)
            code = 200
        except urllib.error.HTTPError as e:
            code = e.code
        assert code == 403
        assert RecordingUpstream.received == [], "upstream must not be called for cross-origin request"
    finally:
        srv.shutdown(); up.shutdown()


def test_post_same_origin_proxied():
    up = _upstream()
    srv, base = _local(up.server_address[1])
    srv.state.token = "tok123"
    local_port = srv.server_address[1]
    try:
        req = urllib.request.Request(
            base + "/api/approve/x/y", data=b"", method="POST",
            headers={"Origin": f"http://127.0.0.1:{local_port}"},
        )
        with urllib.request.urlopen(req, timeout=5) as r:
            data = json.loads(r.read())
        assert data["method"] == "POST"
        assert data["auth"] == "Bearer tok123"
        assert data["csrf"] == "1"
    finally:
        srv.shutdown(); up.shutdown()


def test_post_no_origin_proxied():
    up = _upstream()
    srv, base = _local(up.server_address[1])
    srv.state.token = "tok123"
    try:
        req = urllib.request.Request(
            base + "/api/approve/x/y", data=b"", method="POST")
        # explicitly remove Origin if urllib adds one (it doesn't, but be explicit)
        req.remove_header("Origin")
        with urllib.request.urlopen(req, timeout=5) as r:
            data = json.loads(r.read())
        assert data["method"] == "POST"
        assert data["auth"] == "Bearer tok123"
    finally:
        srv.shutdown(); up.shutdown()


class KeepAliveUpstream(BaseHTTPRequestHandler):
    """HTTP/1.1 upstream that sends Content-Length with keep-alive."""
    protocol_version = "HTTP/1.1"

    def log_message(self, *a):
        pass

    def do_GET(self):
        payload = b"pong"
        self.send_response(200)
        self.send_header("Content-Type", "text/plain")
        self.send_header("Content-Length", str(len(payload)))
        self.send_header("Connection", "keep-alive")
        self.end_headers()
        self.wfile.write(payload)
        self.wfile.flush()


def test_keepalive_content_length_body_not_overread():
    up = ThreadingHTTPServer(("127.0.0.1", 0), KeepAliveUpstream)
    threading.Thread(target=up.serve_forever, daemon=True).start()
    srv, base = _local(up.server_address[1])
    try:
        # Must complete well within timeout — hangs if proxy over-reads past Content-Length
        with urllib.request.urlopen(base + "/api/ping", timeout=5) as r:
            body = r.read()
        assert body == b"pong", repr(body)
    finally:
        srv.shutdown(); up.shutdown()
