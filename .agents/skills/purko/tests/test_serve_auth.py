import importlib
import json
import threading
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

WEBAPP = str(Path(__file__).resolve().parent.parent / "webapp")


class WhoamiUpstream(BaseHTTPRequestHandler):
    def log_message(self, *a):
        pass

    def do_GET(self):
        if self.path == "/api/whoami":
            if self.headers.get("Authorization") == "Bearer good":
                body = json.dumps({"user": "token-user", "role": "admin",
                                   "mode": "token"}).encode()
                self.send_response(200)
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)
                return
            self.send_response(401)
            self.end_headers()


def _boot():
    up = ThreadingHTTPServer(("127.0.0.1", 0), WhoamiUpstream)
    threading.Thread(target=up.serve_forever, daemon=True).start()
    import serve
    importlib.reload(serve)
    srv = serve.make_server(WEBAPP, up.server_address[1])
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    return up, srv, f"http://127.0.0.1:{srv.server_address[1]}"


def _post(url, obj):
    req = urllib.request.Request(
        url, data=json.dumps(obj).encode(),
        headers={"Content-Type": "application/json"}, method="POST")
    try:
        with urllib.request.urlopen(req, timeout=5) as r:
            return r.status
    except urllib.error.HTTPError as e:
        return e.code


def test_valid_token_stored_and_status_logged_in():
    up, srv, base = _boot()
    try:
        assert _post(base + "/local/token", {"token": "good"}) == 204
        assert srv.state.token == "good"
        assert srv.state.auth_mode == "token"
        with urllib.request.urlopen(base + "/local/status", timeout=5) as r:
            assert json.loads(r.read())["loggedIn"] is True
    finally:
        srv.shutdown(); up.shutdown()


def test_invalid_token_rejected_and_not_stored():
    up, srv, base = _boot()
    try:
        assert _post(base + "/local/token", {"token": "bad"}) == 401
        assert srv.state.token is None
    finally:
        srv.shutdown(); up.shutdown()


def test_malformed_body_400():
    up, srv, base = _boot()
    try:
        req = urllib.request.Request(base + "/local/token", data=b"nope",
                                     method="POST")
        try:
            urllib.request.urlopen(req, timeout=5)
            code = 200
        except urllib.error.HTTPError as e:
            code = e.code
        assert code == 400
    finally:
        srv.shutdown(); up.shutdown()


def test_cross_origin_token_post_rejected_403_no_upstream_call():
    """POST /local/token with a cross-origin Origin header → 403, upstream never called."""
    up_calls = []

    class TrackingUpstream(BaseHTTPRequestHandler):
        def log_message(self, *a):
            pass

        def do_GET(self):
            up_calls.append(self.path)
            self.send_response(200)
            body = json.dumps({"user": "u", "role": "admin", "mode": "token"}).encode()
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

    up = ThreadingHTTPServer(("127.0.0.1", 0), TrackingUpstream)
    threading.Thread(target=up.serve_forever, daemon=True).start()
    import serve
    importlib.reload(serve)
    srv = serve.make_server(WEBAPP, up.server_address[1])
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    base = f"http://127.0.0.1:{srv.server_address[1]}"
    try:
        req = urllib.request.Request(
            base + "/local/token",
            data=json.dumps({"token": "good"}).encode(),
            headers={
                "Content-Type": "application/json",
                "Origin": "https://evil.example",
            },
            method="POST",
        )
        try:
            urllib.request.urlopen(req, timeout=5)
            code = 200
        except urllib.error.HTTPError as e:
            code = e.code
        assert code == 403
        assert up_calls == [], f"upstream was called unexpectedly: {up_calls}"
        assert srv.state.token is None
    finally:
        srv.shutdown(); up.shutdown()


def test_local_token_no_origin_still_works():
    """POST /local/token with no Origin header (e.g. curl/skill) → 204."""
    up, srv, base = _boot()
    try:
        assert _post(base + "/local/token", {"token": "good"}) == 204
        assert srv.state.token == "good"
    finally:
        srv.shutdown(); up.shutdown()


def test_upstream_server_error_returns_502_not_401():
    """Upstream 500 on /api/whoami must yield 502 (not 401) and not store the token."""
    class Error500Upstream(BaseHTTPRequestHandler):
        def log_message(self, *a):
            pass

        def do_GET(self):
            self.send_response(500)
            self.end_headers()

    up = ThreadingHTTPServer(("127.0.0.1", 0), Error500Upstream)
    threading.Thread(target=up.serve_forever, daemon=True).start()
    import serve
    importlib.reload(serve)
    srv = serve.make_server(WEBAPP, up.server_address[1])
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    base = f"http://127.0.0.1:{srv.server_address[1]}"
    try:
        assert _post(base + "/local/token", {"token": "any"}) == 502
        assert srv.state.token is None
    finally:
        srv.shutdown(); up.shutdown()
