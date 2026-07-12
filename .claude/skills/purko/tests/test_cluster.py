import base64
import importlib
import json as _json
from pathlib import Path

FAKE = str(Path(__file__).resolve().parent / "fakebin" / "kubectl")


def _fresh(monkeypatch):
    monkeypatch.setenv("PURKO_SKILL_KUBECTL", FAKE)
    import cluster
    importlib.reload(cluster)
    return cluster


def test_list_contexts(monkeypatch):
    c = _fresh(monkeypatch)
    monkeypatch.setenv("FAKE_CONTEXTS", "minikube\nkind-kind")
    assert c.list_contexts() == ["minikube", "kind-kind"]


def test_has_purko_true_and_false(monkeypatch):
    c = _fresh(monkeypatch)
    monkeypatch.setenv("FAKE_HAS_CRD", "yes")
    assert c.has_purko("minikube") is True
    monkeypatch.setenv("FAKE_HAS_CRD", "no")
    assert c.has_purko("minikube") is False


def test_read_dashboard_token_decodes_base64(monkeypatch):
    c = _fresh(monkeypatch)
    monkeypatch.setenv("FAKE_TOKEN_B64", base64.b64encode(b"s3cret-token").decode())
    assert c.read_dashboard_token("minikube") == "s3cret-token"


def test_read_dashboard_token_missing_returns_none(monkeypatch):
    c = _fresh(monkeypatch)
    monkeypatch.delenv("FAKE_TOKEN_B64", raising=False)
    assert c.read_dashboard_token("minikube") is None


def test_context_reachable_true(monkeypatch):
    c = _fresh(monkeypatch)
    monkeypatch.setenv("FAKE_REACHABLE", "yes")
    assert c.context_reachable("minikube") is True


def test_context_reachable_false(monkeypatch):
    c = _fresh(monkeypatch)
    monkeypatch.setenv("FAKE_REACHABLE", "no")
    assert c.context_reachable("minikube") is False


def test_purko_ready_ready(monkeypatch):
    c = _fresh(monkeypatch)
    monkeypatch.setenv("FAKE_PURKO_READY", "ready")
    assert c.purko_ready("minikube") == "ready"


def test_purko_ready_not_ready(monkeypatch):
    c = _fresh(monkeypatch)
    monkeypatch.setenv("FAKE_PURKO_READY", "not-ready")
    assert c.purko_ready("minikube") == "not-ready"


def test_purko_ready_absent(monkeypatch):
    c = _fresh(monkeypatch)
    monkeypatch.delenv("FAKE_PURKO_READY", raising=False)
    assert c.purko_ready("minikube") == "absent"


def test_purko_ready_timeout_returns_absent(monkeypatch):
    c = _fresh(monkeypatch)
    monkeypatch.setenv("FAKE_HANG", "yes")
    # Use a very short timeout so the sleeping fake kubectl triggers TimeoutExpired quickly.
    assert c.purko_ready("minikube", timeout=0.1) == "absent"


# ---------------------------------------------------------------------------
# Tests for live-query helpers (M2b Task 3)
# ---------------------------------------------------------------------------

def test_list_llmproviders_parses(monkeypatch):
    c = _fresh(monkeypatch)
    items = _json.dumps([
        {"metadata": {"name": "anthropic"}, "spec": {"type": "anthropic", "default": True}},
        {"metadata": {"name": "openai"}, "spec": {"type": "openai", "default": False}},
    ])
    monkeypatch.setenv("FAKE_LLMPROVIDERS", items)
    result = c.list_llmproviders("ctx")
    assert result == [
        {"name": "anthropic", "type": "anthropic", "default": True},
        {"name": "openai", "type": "openai", "default": False},
    ]


def test_list_llmproviders_empty_when_absent(monkeypatch):
    c = _fresh(monkeypatch)
    monkeypatch.delenv("FAKE_LLMPROVIDERS", raising=False)
    assert c.list_llmproviders("ctx") == []


def test_list_mcpservers_parses(monkeypatch):
    c = _fresh(monkeypatch)
    items = _json.dumps([
        {"metadata": {"name": "github"}},
        {"metadata": {"name": "jira"}},
    ])
    monkeypatch.setenv("FAKE_MCPSERVERS", items)
    assert c.list_mcpservers("ctx") == [{"name": "github"}, {"name": "jira"}]


def test_list_mcpservers_empty_when_absent(monkeypatch):
    c = _fresh(monkeypatch)
    monkeypatch.delenv("FAKE_MCPSERVERS", raising=False)
    assert c.list_mcpservers("ctx") == []


def test_list_skills_parses(monkeypatch):
    c = _fresh(monkeypatch)
    items = _json.dumps([{"metadata": {"name": "research-assistant"}}])
    monkeypatch.setenv("FAKE_SKILLS", items)
    assert c.list_skills("ctx") == [{"name": "research-assistant"}]


def test_list_skills_empty_when_absent(monkeypatch):
    c = _fresh(monkeypatch)
    monkeypatch.delenv("FAKE_SKILLS", raising=False)
    assert c.list_skills("ctx") == []


def test_crd_version_returns_value(monkeypatch):
    c = _fresh(monkeypatch)
    monkeypatch.setenv("FAKE_CRD_VERSION", "v1alpha1")
    assert c.crd_version("ctx") == "v1alpha1"


def test_crd_version_absent_returns_none(monkeypatch):
    c = _fresh(monkeypatch)
    monkeypatch.delenv("FAKE_CRD_VERSION", raising=False)
    assert c.crd_version("ctx") is None


def test_dry_run_apply_ok(monkeypatch):
    c = _fresh(monkeypatch)
    monkeypatch.setenv("FAKE_DRYRUN_OK", "yes")
    monkeypatch.setenv("FAKE_DRYRUN_MSG", "agent.purko.io/my-agent configured (dry run)")
    ok, msg = c.dry_run_apply("ctx", "apiVersion: purko.io/v1alpha1\nkind: Agent\n", "ai-agents")
    assert ok is True
    assert "dry run" in msg


def test_dry_run_apply_rejected(monkeypatch):
    c = _fresh(monkeypatch)
    monkeypatch.setenv("FAKE_DRYRUN_OK", "no")
    monkeypatch.setenv("FAKE_DRYRUN_MSG", "admission webhook denied: AG-001 unknown type")
    ok, msg = c.dry_run_apply("ctx", "apiVersion: purko.io/v1alpha1\nkind: Agent\n", "ai-agents")
    assert ok is False
    assert "AG-001" in msg


def test_apply_manifest_ok(monkeypatch):
    c = _fresh(monkeypatch)
    monkeypatch.setenv("FAKE_DRYRUN_OK", "yes")
    monkeypatch.setenv("FAKE_DRYRUN_MSG", "agent.purko.io/my-agent created")
    ok, msg = c.apply_manifest("ctx", "apiVersion: purko.io/v1alpha1\nkind: Agent\n", "ai-agents")
    assert ok is True
    assert "created" in msg


def test_apply_manifest_rejected(monkeypatch):
    c = _fresh(monkeypatch)
    monkeypatch.setenv("FAKE_DRYRUN_OK", "no")
    monkeypatch.setenv("FAKE_DRYRUN_MSG", "Error from server: agents.purko.io is forbidden")
    ok, msg = c.apply_manifest("ctx", "apiVersion: purko.io/v1alpha1\nkind: Agent\n", "ai-agents")
    assert ok is False
    assert "forbidden" in msg


def test_wait_ready_running(monkeypatch):
    c = _fresh(monkeypatch)
    monkeypatch.setenv("FAKE_WAIT_PHASE", "Running")
    ready, phase = c.wait_ready("ctx", "agent", "my-agent", "ai-agents", timeout=5)
    assert ready is True
    assert phase == "Running"


def test_wait_ready_timeout_returns_false(monkeypatch):
    c = _fresh(monkeypatch)
    monkeypatch.setenv("FAKE_WAIT_PHASE", "Pending")
    # Very short timeout: loop exits before next 2s sleep would fire
    ready, phase = c.wait_ready("ctx", "agent", "my-agent", "ai-agents", timeout=0.05)
    assert ready is False
    # phase should be whatever the fake returned ("Pending"), not "Unknown"
    assert phase == "Pending"
