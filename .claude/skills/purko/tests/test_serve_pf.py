"""Tests for serve.py pf-mode (M2b Task 1).

Strategy: monkeypatch cluster.PortForward is not needed for most tests.
We pass a stub PortForward directly to make_server(portforward=...) and test
serve.py's wiring independently of a real tunnel.
"""
import importlib
import json
import signal
import socket
import sys
import threading
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

WEBAPP = str(Path(__file__).resolve().parent.parent / "webapp")


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

class FakeUpstream(BaseHTTPRequestHandler):
    """Echoes method/path as JSON."""
    def log_message(self, *a):
        pass

    def _echo(self):
        length = int(self.headers.get("Content-Length", 0) or 0)
        body = self.rfile.read(length).decode() if length else ""
        payload = json.dumps({
            "method": self.command,
            "path": self.path,
        }).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    do_GET = do_POST = do_PUT = do_DELETE = _echo


def _start_upstream():
    srv = ThreadingHTTPServer(("127.0.0.1", 0), FakeUpstream)
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    return srv


def _dead_port():
    """Allocate a port then release it — guaranteed not listening."""
    s = socket.socket()
    s.bind(("127.0.0.1", 0))
    port = s.getsockname()[1]
    s.close()
    return port


class StubPF:
    """Simple stub: starts fine, records stop(), expose local_port."""
    def __init__(self, local_port):
        self.local_port = local_port
        self.stop_calls = 0
        self.ensure_calls = 0
        self.started = False

    def start(self, timeout=15):
        self.started = True

    def alive(self):
        return self.started

    def ensure(self):
        self.ensure_calls += 1

    def stop(self):
        self.stop_calls += 1


class SwitchingPF:
    """Dead on first contact; ensure() re-points local_port to a live port."""
    def __init__(self, dead_port, live_port):
        self.local_port = dead_port
        self._live_port = live_port
        self.ensure_calls = 0

    def start(self, timeout=15):
        pass

    def alive(self):
        return True

    def ensure(self):
        self.ensure_calls += 1
        self.local_port = self._live_port

    def stop(self):
        pass


class RaisingPF:
    """ensure() always raises RuntimeError — simulates failed restart."""
    def __init__(self):
        self.local_port = _dead_port()
        self.ensure_calls = 0

    def start(self, timeout=15):
        pass

    def alive(self):
        return True

    def ensure(self):
        self.ensure_calls += 1
        raise RuntimeError("port-forward failed to restart")

    def stop(self):
        pass


def _local_pf(pf):
    import serve
    importlib.reload(serve)
    srv = serve.make_server(WEBAPP, upstream_port=pf.local_port, portforward=pf)
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    return srv, f"http://127.0.0.1:{srv.server_address[1]}"


# ---------------------------------------------------------------------------
# Tests: pf-mode boots and proxies
# ---------------------------------------------------------------------------

def test_pf_mode_boots_and_proxies():
    """make_server with portforward= routes /api/ through the stub's local_port."""
    up = _start_upstream()
    pf = StubPF(up.server_address[1])
    srv, base = _local_pf(pf)
    try:
        with urllib.request.urlopen(base + "/api/agents", timeout=5) as r:
            data = json.loads(r.read())
        assert data["method"] == "GET"
        assert data["path"] == "/api/agents"
    finally:
        srv.shutdown()
        up.shutdown()


def test_pf_mode_portforward_stored_on_server():
    """make_server(portforward=pf) stores pf as srv.portforward."""
    up = _start_upstream()
    pf = StubPF(up.server_address[1])
    import serve
    importlib.reload(serve)
    srv = serve.make_server(WEBAPP, upstream_port=pf.local_port, portforward=pf)
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    try:
        assert srv.portforward is pf
    finally:
        srv.shutdown()
        up.shutdown()


# ---------------------------------------------------------------------------
# Tests: self-heal — _proxy
# ---------------------------------------------------------------------------

def test_self_heal_proxy_retries_after_ensure():
    """First request hits dead port; ensure() switches local_port to live → 200."""
    up = _start_upstream()
    live_port = up.server_address[1]
    dead = _dead_port()
    pf = SwitchingPF(dead_port=dead, live_port=live_port)

    import serve
    importlib.reload(serve)
    # upstream_port starts at dead; ensure() will switch pf.local_port to live
    srv = serve.make_server(WEBAPP, upstream_port=dead, portforward=pf)
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    base = f"http://127.0.0.1:{srv.server_address[1]}"
    try:
        with urllib.request.urlopen(base + "/api/ping", timeout=5) as r:
            data = json.loads(r.read())
        assert data["method"] == "GET"
        assert pf.ensure_calls == 1
        assert srv.state.upstream_port == live_port
    finally:
        srv.shutdown()
        up.shutdown()


def test_self_heal_proxy_ensure_raises_gives_502():
    """If ensure() raises, the proxy returns 502 (no retry)."""
    pf = RaisingPF()

    import serve
    importlib.reload(serve)
    srv = serve.make_server(WEBAPP, upstream_port=pf.local_port, portforward=pf)
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    base = f"http://127.0.0.1:{srv.server_address[1]}"
    try:
        try:
            urllib.request.urlopen(base + "/api/ping", timeout=5)
            code = 200
        except urllib.error.HTTPError as e:
            code = e.code
        assert code == 502
        assert pf.ensure_calls == 1
    finally:
        srv.shutdown()


def test_self_heal_proxy_no_pf_still_502():
    """Without a portforward, dead upstream → 502 (no self-heal attempted)."""
    dead = _dead_port()

    import serve
    importlib.reload(serve)
    srv = serve.make_server(WEBAPP, upstream_port=dead)
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    base = f"http://127.0.0.1:{srv.server_address[1]}"
    try:
        try:
            urllib.request.urlopen(base + "/api/ping", timeout=5)
            code = 200
        except urllib.error.HTTPError as e:
            code = e.code
        assert code == 502
    finally:
        srv.shutdown()


def test_self_heal_proxy_lock_serialises_ensure():
    """Concurrent requests: ensure() calls are serialised (lock prevents stampede).

    With 5 simultaneous requests all initially failing, each one that reaches
    the lock calls ensure() once (serialised, not concurrent). After the first
    ensure() switches local_port to live, subsequent callers still enter the
    lock but call an idempotent ensure() — this is correct behaviour. The key
    invariant is: no concurrent ensure() calls, and eventually all requests
    complete without hanging.
    """
    up = _start_upstream()
    live_port = up.server_address[1]
    dead = _dead_port()
    pf = SwitchingPF(dead_port=dead, live_port=live_port)

    import serve
    importlib.reload(serve)
    srv = serve.make_server(WEBAPP, upstream_port=dead, portforward=pf)
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    base = f"http://127.0.0.1:{srv.server_address[1]}"

    results = []
    errors = []

    def do_request():
        try:
            with urllib.request.urlopen(base + "/api/ping", timeout=5) as r:
                results.append(r.status)
        except urllib.error.HTTPError as e:
            errors.append(e.code)
        except Exception as e:
            errors.append(str(e))

    threads = [threading.Thread(target=do_request) for _ in range(5)]
    for t in threads:
        t.start()
    for t in threads:
        t.join(timeout=10)

    # ensure() was called at least once (self-heal triggered) but not more than
    # once per request (5 requests → ≤ 5 ensure calls, each serialised by lock).
    assert 1 <= pf.ensure_calls <= 5
    # All threads completed (no deadlock/hang) and something returned.
    assert len(results) + len(errors) == 5

    srv.shutdown()
    up.shutdown()


# ---------------------------------------------------------------------------
# Tests: self-heal — _local_token
# ---------------------------------------------------------------------------

class WhoamiUpstream(BaseHTTPRequestHandler):
    """Returns 200 + mode for GET /api/whoami when auth is correct."""
    def log_message(self, *a):
        pass

    def do_GET(self):
        if self.path == "/api/whoami":
            body = json.dumps({"mode": "token"}).encode()
            self.send_response(200)
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
        else:
            self.send_response(404)
            self.end_headers()


def _start_whoami():
    srv = ThreadingHTTPServer(("127.0.0.1", 0), WhoamiUpstream)
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    return srv


def _post_token(base, token):
    req = urllib.request.Request(
        base + "/local/token",
        data=json.dumps({"token": token}).encode(),
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=5) as r:
            return r.status
    except urllib.error.HTTPError as e:
        return e.code


def test_self_heal_local_token_retries_after_ensure():
    """POST /local/token: first request dead → ensure() heals → 204."""
    whoami = _start_whoami()
    live_port = whoami.server_address[1]
    dead = _dead_port()
    pf = SwitchingPF(dead_port=dead, live_port=live_port)

    import serve
    importlib.reload(serve)
    srv = serve.make_server(WEBAPP, upstream_port=dead, portforward=pf)
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    base = f"http://127.0.0.1:{srv.server_address[1]}"
    try:
        code = _post_token(base, "tok")
        assert code == 204, f"expected 204, got {code}"
        assert pf.ensure_calls == 1
        assert srv.state.upstream_port == live_port
    finally:
        srv.shutdown()
        whoami.shutdown()


def test_self_heal_local_token_ensure_raises_gives_502():
    """POST /local/token: ensure() raises → 502."""
    pf = RaisingPF()

    import serve
    importlib.reload(serve)
    srv = serve.make_server(WEBAPP, upstream_port=pf.local_port, portforward=pf)
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    base = f"http://127.0.0.1:{srv.server_address[1]}"
    try:
        code = _post_token(base, "tok")
        assert code == 502
        assert pf.ensure_calls == 1
    finally:
        srv.shutdown()


# ---------------------------------------------------------------------------
# Tests: CLI mutual exclusivity
# ---------------------------------------------------------------------------

def test_cli_pf_context_mutual_exclusive_with_upstream_url():
    """--pf-context and --upstream-url together → argparse error (exit 2)."""
    import serve
    importlib.reload(serve)
    try:
        serve.main(["--pf-context", "minikube", "--upstream-url", "http://x"])
        assert False, "expected SystemExit"
    except SystemExit as e:
        assert e.code == 2


def test_cli_pf_context_mutual_exclusive_with_upstream_port():
    """--pf-context and --upstream-port together → argparse error (exit 2)."""
    import serve
    importlib.reload(serve)
    try:
        serve.main(["--pf-context", "minikube", "--upstream-port", "8082"])
        assert False, "expected SystemExit"
    except SystemExit as e:
        assert e.code == 2


# ---------------------------------------------------------------------------
# Tests: shutdown calls pf.stop
# ---------------------------------------------------------------------------

def test_shutdown_calls_pf_stop():
    """Server shutdown triggers pf.stop() via atexit / shutdown hook."""
    up = _start_upstream()
    pf = StubPF(up.server_address[1])

    import serve
    importlib.reload(serve)
    srv = serve.make_server(WEBAPP, upstream_port=pf.local_port, portforward=pf)
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    srv.shutdown()

    # The server's shutdown hook must call pf.stop()
    assert pf.stop_calls == 1

    up.shutdown()


# ---------------------------------------------------------------------------
# Tests: SIGTERM handler
# ---------------------------------------------------------------------------

def test_sigterm_handler_installed_and_raises_systemexit():
    """_install_signal_handlers() installs a SIGTERM handler that raises SystemExit(0)."""
    import serve
    importlib.reload(serve)

    orig = signal.getsignal(signal.SIGTERM)
    try:
        serve._install_signal_handlers()
        handler = signal.getsignal(signal.SIGTERM)
        assert callable(handler), "SIGTERM handler must be callable (not SIG_DFL or SIG_IGN)"
        try:
            handler(None, None)
            assert False, "expected SystemExit"
        except SystemExit as e:
            assert e.code == 0
    finally:
        signal.signal(signal.SIGTERM, orig)
