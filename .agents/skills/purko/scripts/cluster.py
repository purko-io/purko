"""kubectl helpers: context discovery, purko detection, token secret, port-forward."""
import base64
import json
import os
import socket
import subprocess
import time

KUBECTL = os.environ.get("PURKO_SKILL_KUBECTL", "kubectl")


def _run(args, timeout=10):
    return subprocess.run(
        [KUBECTL, *args], capture_output=True, text=True, timeout=timeout
    )


def list_contexts():
    r = _run(["config", "get-contexts", "-o", "name"])
    if r.returncode != 0:
        return []
    return [line.strip() for line in r.stdout.splitlines() if line.strip()]


def has_purko(context):
    r = _run(["--context", context, "get", "crd", "agents.purko.io", "-o", "name"])
    return r.returncode == 0 and "agents.purko.io" in r.stdout


def context_reachable(context, timeout=5):
    """Return True if the cluster for `context` responds to a /readyz probe."""
    try:
        r = _run(["--context", context, "get", "--raw", "/readyz"], timeout=timeout)
        return r.returncode == 0
    except subprocess.TimeoutExpired:
        return False


def purko_ready(context, namespace="purko-system", timeout=10):
    """Return the readiness state of purko-operator in the given namespace.

    Returns:
        'ready'     – deployment exists and has readyReplicas >= 1
        'not-ready' – deployment exists but readyReplicas == 0
        'absent'    – deployment not found or cluster unresponsive (timeout)
    """
    try:
        r = _run([
            "--context", context, "-n", namespace,
            "get", "deployment", "purko-operator",
            "-o", "jsonpath={.status.readyReplicas}",
        ], timeout=timeout)
    except subprocess.TimeoutExpired:
        return "absent"  # cluster unresponsive — treat as absent
    if r.returncode != 0:
        return "absent"
    try:
        return "ready" if int(r.stdout.strip() or "0") >= 1 else "not-ready"
    except ValueError:
        return "not-ready"


def read_dashboard_token(context, namespace="purko-system"):
    r = _run([
        "--context", context, "-n", namespace,
        "get", "secret", "purko-dashboard-token",
        "-o", "jsonpath={.data.token}",
    ])
    if r.returncode != 0 or not r.stdout.strip():
        return None
    return base64.b64decode(r.stdout.strip()).decode()


def free_port():
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


class PortForward:
    """Manages a `kubectl port-forward` child; restartable via ensure()."""

    def __init__(self, context, namespace="purko-system",
                 target="deploy/purko-operator", remote_port=8082, local_port=0):
        self.context = context
        self.namespace = namespace
        self.target = target
        self.remote_port = remote_port
        self.local_port = local_port or free_port()
        self.proc = None

    def _connectable(self):
        try:
            with socket.create_connection(("127.0.0.1", self.local_port), timeout=0.5):
                return True
        except OSError:
            return False

    def start(self, timeout=15):
        self.proc = subprocess.Popen(
            [KUBECTL, "--context", self.context, "-n", self.namespace,
             "port-forward", self.target,
             f"{self.local_port}:{self.remote_port}"],
            stdout=subprocess.DEVNULL, stderr=subprocess.PIPE,
        )
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            if self.proc.poll() is not None:
                stderr_tail = self.proc.stderr.read(2048).decode(errors="replace").strip()
                detail = f": {stderr_tail}" if stderr_tail else ""
                raise RuntimeError(
                    f"kubectl port-forward exited with {self.proc.returncode}{detail}"
                )
            if self._connectable():
                return
            time.sleep(0.2)
        self.stop()
        raise RuntimeError("port-forward did not become ready in time")

    def alive(self):
        return self.proc is not None and self.proc.poll() is None

    def ensure(self):
        if not self.alive() or not self._connectable():
            self.stop()
            self.start()

    def stop(self):
        if self.proc is not None and self.proc.poll() is None:
            self.proc.terminate()
            try:
                self.proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                self.proc.kill()
                self.proc.wait(timeout=5)
        self.proc = None


# ---------------------------------------------------------------------------
# Live-query helpers for guided create/manage flows (M2b Task 3)
# ---------------------------------------------------------------------------

def list_llmproviders(context, namespace="purko-system"):
    """List LLMProvider CRs. Returns [{name, type, default}].

    Used by Flow H to offer ONLY real provider names — never invented ones.
    The `default` flag marks the provider configured as the cluster default.
    """
    r = _run(["--context", context, "-n", namespace, "get", "llmproviders", "-o", "json"])
    if r.returncode != 0:
        return []
    try:
        data = json.loads(r.stdout)
        result = []
        for item in data.get("items", []):
            result.append({
                "name": item["metadata"]["name"],
                "type": item.get("spec", {}).get("type", ""),
                "default": bool(item.get("spec", {}).get("default", False)),
            })
        return result
    except (json.JSONDecodeError, KeyError):
        return []


def list_mcpservers(context, namespace="ai-agents"):
    """List MCPServer CRs. Returns [{name}].

    Used by Flow H to show available MCP tools when interviewing the user.
    """
    r = _run(["--context", context, "-n", namespace, "get", "mcpservers", "-o", "json"])
    if r.returncode != 0:
        return []
    try:
        data = json.loads(r.stdout)
        return [{"name": item["metadata"]["name"]} for item in data.get("items", [])]
    except (json.JSONDecodeError, KeyError):
        return []


def list_skills(context, namespace="ai-agents"):
    """List Skill CRs. Returns [{name}].

    Used by Flow H to show skills available for attachment (AG-013 rejects
    references to non-existent Skills).
    """
    r = _run(["--context", context, "-n", namespace, "get", "skills", "-o", "json"])
    if r.returncode != 0:
        return []
    try:
        data = json.loads(r.stdout)
        return [{"name": item["metadata"]["name"]} for item in data.get("items", [])]
    except (json.JSONDecodeError, KeyError):
        return []


def crd_version(context):
    """Return a stable CRD version string for skew detection, or None.

    Tries spec.versions[0].name (e.g. 'v1alpha1') via jsonpath. Returns None
    if the CRD is absent or the query fails. Used only for a non-gating WARNING
    comparing against the SHA in references/PURKO_REF_VERSION.
    """
    r = _run([
        "--context", context,
        "get", "crd", "agents.purko.io",
        "-o", "jsonpath={.spec.versions[0].name}",
    ])
    if r.returncode == 0 and r.stdout.strip():
        return r.stdout.strip()
    return None


def dry_run_apply(context, yaml_text, namespace):
    """Validate yaml_text via kubectl server dry-run. Returns (ok: bool, message: str).

    Pipes yaml_text to stdin — never writes a temp file. Invokes purko's
    validating webhook. On AG-xxx / WF-xxx rejection, ok=False and message
    contains the webhook error. Always call this before apply_manifest.
    """
    try:
        r = subprocess.run(
            [KUBECTL, "--context", context, "-n", namespace,
             "apply", "--dry-run=server", "-f", "-"],
            input=yaml_text, capture_output=True, text=True, timeout=30,
        )
        return r.returncode == 0, (r.stdout + r.stderr).strip()
    except subprocess.TimeoutExpired:
        return False, "dry-run timed out"


def apply_manifest(context, yaml_text, namespace):
    """Apply yaml_text to the cluster. Returns (ok: bool, message: str).

    Pipes yaml_text to stdin. Call dry_run_apply first and only apply after
    the user has given explicit consent.
    """
    try:
        r = subprocess.run(
            [KUBECTL, "--context", context, "-n", namespace,
             "apply", "-f", "-"],
            input=yaml_text, capture_output=True, text=True, timeout=60,
        )
        return r.returncode == 0, (r.stdout + r.stderr).strip()
    except subprocess.TimeoutExpired:
        return False, "apply timed out"


def wait_ready(context, kind, name, namespace, timeout=60):
    """Poll resource status.phase until Running/Ready/Succeeded or timeout.

    Returns (ready: bool, phase: str). Uses _run so the fake-kubectl shim
    (FAKE_WAIT_PHASE) works in tests. Poll interval is 2 s; pass a short
    timeout in tests to keep them fast.
    """
    ready_phases = {"Running", "Ready", "Succeeded"}
    deadline = time.monotonic() + timeout
    phase = "Unknown"
    while time.monotonic() < deadline:
        r = _run(
            ["--context", context, "-n", namespace,
             "get", kind, name,
             "-o", "jsonpath={.status.phase}"],
            timeout=10,
        )
        phase = r.stdout.strip() if r.returncode == 0 else "Unknown"
        if phase in ready_phases:
            return True, phase
        remaining = deadline - time.monotonic()
        time.sleep(max(0.0, min(2.0, remaining)))
    return False, phase or "Unknown"
