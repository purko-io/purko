import importlib
import socket
from pathlib import Path

FAKE = str(Path(__file__).resolve().parent / "fakebin" / "kubectl")


def _fresh(monkeypatch):
    monkeypatch.setenv("PURKO_SKILL_KUBECTL", FAKE)
    import cluster
    importlib.reload(cluster)
    return cluster


def test_start_waits_until_port_connectable(monkeypatch):
    c = _fresh(monkeypatch)
    pf = c.PortForward("minikube")
    try:
        pf.start(timeout=10)
        assert pf.alive()
        with socket.create_connection(("127.0.0.1", pf.local_port), timeout=2):
            pass
    finally:
        pf.stop()
    assert not pf.alive()


def test_start_raises_when_kubectl_dies(monkeypatch):
    c = _fresh(monkeypatch)
    monkeypatch.setenv("FAKE_PF_DIE", "yes")
    pf = c.PortForward("minikube")
    try:
        try:
            pf.start(timeout=3)
            raised = False
        except RuntimeError:
            raised = True
        assert raised
    finally:
        pf.stop()


def test_ensure_restarts_dead_forward(monkeypatch):
    c = _fresh(monkeypatch)
    pf = c.PortForward("minikube")
    try:
        pf.start(timeout=10)
        pf.proc.kill()
        pf.proc.wait()
        assert not pf.alive()
        pf.ensure()
        assert pf.alive()
    finally:
        pf.stop()


def test_ensure_restarts_when_alive_but_not_connectable(monkeypatch):
    """ensure() restarts a live process whose port is no longer connectable (stale tunnel)."""
    c = _fresh(monkeypatch)
    stop_calls = []
    start_calls = []

    monkeypatch.setattr(c.PortForward, "alive", lambda self: True)
    monkeypatch.setattr(c.PortForward, "_connectable", lambda self: False)
    monkeypatch.setattr(c.PortForward, "stop", lambda self: stop_calls.append(1))
    monkeypatch.setattr(c.PortForward, "start", lambda self, timeout=15: start_calls.append(1))

    pf = c.PortForward("minikube")
    pf.ensure()

    assert stop_calls == [1], "ensure() must call stop() for the stale tunnel"
    assert start_calls == [1], "ensure() must call start() to restart the tunnel"
