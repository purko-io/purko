import importlib
import json
import threading
import urllib.request
from pathlib import Path

WEBAPP = str(Path(__file__).resolve().parent.parent / "webapp")


def _boot(upstream_port=None):
    import serve
    importlib.reload(serve)
    srv = serve.make_server(WEBAPP, upstream_port)
    t = threading.Thread(target=srv.serve_forever, daemon=True)
    t.start()
    return srv, f"http://127.0.0.1:{srv.server_address[1]}"


def _get(url):
    with urllib.request.urlopen(url, timeout=5) as r:
        return r.status, r.read().decode()


def test_serves_index_html():
    srv, base = _boot()
    try:
        status, body = _get(base + "/")
        assert status == 200 and "purko" in body
    finally:
        srv.shutdown()


def test_local_status_reports_no_upstream_no_login():
    srv, base = _boot()
    try:
        status, body = _get(base + "/local/status")
        assert status == 200
        data = json.loads(body)
        assert data == {
            "upstream": False, "authMode": None, "loggedIn": False,
            "dashboardUrl": None,  # no upstream → no dashboard to manage in
        }
    finally:
        srv.shutdown()


def test_binds_localhost_only():
    srv, _ = _boot()
    try:
        assert srv.server_address[0] == "127.0.0.1"
    finally:
        srv.shutdown()
