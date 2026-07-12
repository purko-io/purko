"""Persisted skill configuration (~/.config/purko-skill/config.json).

Schema (v2): named targets with one active.
  {"active": "minikube",
   "targets": {"minikube": {"mode": "cluster", "context": "minikube",
                            "namespace": "purko-system",
                            "agent_namespace": "ai-agents",
                            "workspace_url": null}}}
Legacy flat configs (v1: mode/context at the top level) migrate on load.
"""
import json
import os
from pathlib import Path

CONFIG_DIR = Path(
    os.environ.get("PURKO_SKILL_CONFIG_DIR", str(Path.home() / ".config" / "purko-skill"))
)
CONFIG_PATH = CONFIG_DIR / "config.json"

TARGET_DEFAULTS = {
    "mode": None,            # "cluster" | "remote" | "hosted" ("hosted" is an alias for "remote")
    "context": None,         # kubectl context name (cluster mode only)
    "namespace": "purko-system",
    "agent_namespace": "ai-agents",
    "workspace_url": None,   # remote/hosted mode: upstream dashboard URL
}

DEFAULTS = {
    "active": None,
    "targets": {},
}

_LEGACY_KEYS = tuple(TARGET_DEFAULTS)


def _migrate(cfg):
    if "targets" in cfg:
        return cfg
    legacy = {k: cfg.pop(k) for k in _LEGACY_KEYS if k in cfg}
    out = dict(DEFAULTS)
    out.update(cfg)
    out["targets"] = {}
    if any(v is not None for v in legacy.values()):
        target = dict(TARGET_DEFAULTS)
        target.update(legacy)
        out["targets"]["default"] = target
        out["active"] = "default"
    return out


def load():
    cfg = dict(DEFAULTS)
    try:
        cfg = _migrate(json.loads(CONFIG_PATH.read_text()))
    except (FileNotFoundError, json.JSONDecodeError):
        pass
    cfg.setdefault("active", None)
    cfg.setdefault("targets", {})
    return cfg


def save(cfg):
    CONFIG_DIR.mkdir(parents=True, exist_ok=True)
    tmp = CONFIG_PATH.with_suffix(".tmp")
    tmp.write_text(json.dumps(cfg, indent=2) + "\n")
    os.replace(tmp, CONFIG_PATH)
    os.chmod(CONFIG_PATH, 0o600)


def upsert_target(cfg, name, **fields):
    """Add or update a named target; the first target added becomes active."""
    target = dict(TARGET_DEFAULTS)
    target.update(cfg["targets"].get(name, {}))
    target.update(fields)
    cfg["targets"][name] = target
    if cfg.get("active") is None:
        cfg["active"] = name
    return target


def set_active(cfg, name):
    if name not in cfg["targets"]:
        raise KeyError(f"unknown target {name!r}; known: {sorted(cfg['targets'])}")
    cfg["active"] = name


def active_target(cfg):
    """Return (name, target-dict) for the active target, or (None, None)."""
    name = cfg.get("active")
    if not name or name not in cfg.get("targets", {}):
        return (None, None)
    target = dict(TARGET_DEFAULTS)
    target.update(cfg["targets"][name])
    return (name, target)
