"""Tests for scripts/keychain.py — always uses the file backend via env override."""
import importlib
import unittest.mock as mock

import pytest


def _fresh(monkeypatch, tmp_path):
    """Reload keychain with the file backend rooted at tmp_path."""
    monkeypatch.setenv("PURKO_SKILL_KEYCHAIN_BACKEND", "file")
    monkeypatch.setenv("PURKO_SKILL_CONFIG_DIR", str(tmp_path))
    import keychain
    importlib.reload(keychain)
    return keychain


def test_roundtrip(monkeypatch, tmp_path):
    k = _fresh(monkeypatch, tmp_path)
    k.save_token("myworkspace", "tok123")
    assert k.load_token("myworkspace") == "tok123"


def test_file_permissions(monkeypatch, tmp_path):
    k = _fresh(monkeypatch, tmp_path)
    k.save_token("myworkspace", "tok123")
    p = tmp_path / "tokens" / "myworkspace.token"
    assert p.exists(), "token file was not created"
    assert oct(p.stat().st_mode & 0o777) == oct(0o600)


def test_delete(monkeypatch, tmp_path):
    k = _fresh(monkeypatch, tmp_path)
    k.save_token("myworkspace", "tok123")
    k.delete_token("myworkspace")
    assert k.load_token("myworkspace") is None


def test_missing_returns_none(monkeypatch, tmp_path):
    k = _fresh(monkeypatch, tmp_path)
    assert k.load_token("nonexistent") is None


def test_overwrite_updates_value(monkeypatch, tmp_path):
    k = _fresh(monkeypatch, tmp_path)
    k.save_token("ws", "old-token")
    k.save_token("ws", "new-token")
    assert k.load_token("ws") == "new-token"


def test_delete_nonexistent_is_noop(monkeypatch, tmp_path):
    k = _fresh(monkeypatch, tmp_path)
    k.delete_token("ghost")  # should not raise


def test_separate_targets_are_isolated(monkeypatch, tmp_path):
    k = _fresh(monkeypatch, tmp_path)
    k.save_token("alpha", "token-a")
    k.save_token("beta", "token-b")
    assert k.load_token("alpha") == "token-a"
    assert k.load_token("beta") == "token-b"
    k.delete_token("alpha")
    assert k.load_token("alpha") is None
    assert k.load_token("beta") == "token-b"


def test_file_save_dir_mode_is_0700(monkeypatch, tmp_path):
    k = _fresh(monkeypatch, tmp_path)
    k.save_token("ws", "tok")
    d = tmp_path / "tokens"
    assert oct(d.stat().st_mode & 0o777) == oct(0o700)


def test_path_traversal_save_raises(monkeypatch, tmp_path):
    k = _fresh(monkeypatch, tmp_path)
    with pytest.raises(ValueError, match="invalid target name"):
        k.save_token("../../x", "tok")


def test_path_traversal_load_raises(monkeypatch, tmp_path):
    k = _fresh(monkeypatch, tmp_path)
    with pytest.raises(ValueError, match="invalid target name"):
        k.load_token("../../x")


def test_path_traversal_delete_raises(monkeypatch, tmp_path):
    k = _fresh(monkeypatch, tmp_path)
    with pytest.raises(ValueError, match="invalid target name"):
        k.delete_token("../../x")


def test_valid_dotted_name_accepted(monkeypatch, tmp_path):
    k = _fresh(monkeypatch, tmp_path)
    k.save_token("my.cluster-1", "tok123")
    assert k.load_token("my.cluster-1") == "tok123"


def test_kc_save_failure_raises_without_token(monkeypatch):
    """_kc_save raises RuntimeError on non-zero exit; message must not contain the token."""
    # Force the keychain backend regardless of platform.
    monkeypatch.setenv("PURKO_SKILL_KEYCHAIN_BACKEND", "keychain")
    import keychain
    importlib.reload(keychain)
    monkeypatch.setattr(keychain, "_use_keychain", lambda: True)

    fake_result = mock.MagicMock()
    fake_result.returncode = 1
    monkeypatch.setattr(keychain.subprocess, "run", lambda *a, **kw: fake_result)

    token = "super-secret-token-abc123"
    with pytest.raises(RuntimeError) as exc_info:
        keychain.save_token("myws", token)

    msg = str(exc_info.value)
    assert "1" in msg          # exit code present
    assert token not in msg    # token NOT leaked in message
