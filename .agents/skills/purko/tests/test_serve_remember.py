"""Tests for POST /local/token/remember in scripts/serve.py."""
import importlib
import json
import threading
import urllib.error
import urllib.request
from pathlib import Path

WEBAPP = str(Path(__file__).resolve().parent.parent / "webapp")


def _boot():
    """Boot a serve instance with no upstream (remember only needs state.token)."""
    import serve
    importlib.reload(serve)
    srv = serve.make_server(WEBAPP)
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    return srv, f"http://127.0.0.1:{srv.server_address[1]}"


def _post(url, obj):
    req = urllib.request.Request(
        url, data=json.dumps(obj).encode(),
        headers={"Content-Type": "application/json"}, method="POST")
    try:
        with urllib.request.urlopen(req, timeout=5) as r:
            return r.status, r.read()
    except urllib.error.HTTPError as e:
        return e.code, e.read()


def test_remember_saves_token_to_file_backend(monkeypatch, tmp_path):
    monkeypatch.setenv("PURKO_SKILL_KEYCHAIN_BACKEND", "file")
    monkeypatch.setenv("PURKO_SKILL_CONFIG_DIR", str(tmp_path))
    import keychain
    importlib.reload(keychain)

    srv, base = _boot()
    srv.state.token = "saved-token-xyz"
    try:
        code, body = _post(base + "/local/token/remember", {"target": "myws"})
        assert code == 204
        assert body == b""  # no token in response body
        assert keychain.load_token("myws") == "saved-token-xyz"
    finally:
        srv.shutdown()


def test_remember_409_when_not_logged_in():
    srv, base = _boot()
    try:
        assert srv.state.token is None
        code, _ = _post(base + "/local/token/remember", {"target": "myws"})
        assert code == 409
    finally:
        srv.shutdown()


def test_remember_path_traversal_target_returns_400(monkeypatch, tmp_path):
    monkeypatch.setenv("PURKO_SKILL_KEYCHAIN_BACKEND", "file")
    monkeypatch.setenv("PURKO_SKILL_CONFIG_DIR", str(tmp_path))
    import keychain
    importlib.reload(keychain)

    srv, base = _boot()
    srv.state.token = "some-token"
    try:
        code, body = _post(base + "/local/token/remember", {"target": "../../pwned"})
        assert code == 400
        assert json.loads(body) == {"error": "invalid target name"}
        # No file should have been written outside the token dir
        assert not any((tmp_path / "tokens").parent.glob("*.token"))
    finally:
        srv.shutdown()


def test_remember_no_token_in_response_body(monkeypatch, tmp_path):
    monkeypatch.setenv("PURKO_SKILL_KEYCHAIN_BACKEND", "file")
    monkeypatch.setenv("PURKO_SKILL_CONFIG_DIR", str(tmp_path))
    import keychain
    importlib.reload(keychain)

    srv, base = _boot()
    srv.state.token = "top-secret-value"
    try:
        code, body = _post(base + "/local/token/remember", {"target": "ws"})
        assert code == 204
        assert b"top-secret-value" not in body
    finally:
        srv.shutdown()
