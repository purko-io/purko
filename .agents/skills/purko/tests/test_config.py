import importlib


def _fresh(monkeypatch, tmp_path):
    monkeypatch.setenv("PURKO_SKILL_CONFIG_DIR", str(tmp_path))
    import purko_config
    importlib.reload(purko_config)
    return purko_config


def test_load_returns_defaults_when_no_file(monkeypatch, tmp_path):
    pc = _fresh(monkeypatch, tmp_path)
    assert pc.load() == {"active": None, "targets": {}}


def test_save_then_load_roundtrips_and_is_0600(monkeypatch, tmp_path):
    pc = _fresh(monkeypatch, tmp_path)
    cfg = pc.load()
    pc.upsert_target(cfg, "minikube", mode="cluster", context="minikube")
    pc.save(cfg)
    assert pc.load()["targets"]["minikube"]["context"] == "minikube"
    assert (pc.CONFIG_PATH.stat().st_mode & 0o777) == 0o600


def test_load_preserves_unknown_top_level_keys(monkeypatch, tmp_path):
    pc = _fresh(monkeypatch, tmp_path)
    pc.CONFIG_PATH.parent.mkdir(parents=True, exist_ok=True)
    pc.CONFIG_PATH.write_text('{"context": "kind", "extra": 1}')
    cfg = pc.load()
    assert cfg["extra"] == 1
    assert cfg["targets"]["default"]["context"] == "kind"
    assert cfg["targets"]["default"]["namespace"] == "purko-system"


def test_legacy_flat_config_migrates_to_default_target(monkeypatch, tmp_path):
    pc = _fresh(monkeypatch, tmp_path)
    pc.CONFIG_PATH.parent.mkdir(parents=True, exist_ok=True)
    pc.CONFIG_PATH.write_text(
        '{"mode": "cluster", "context": "minikube", "namespace": "purko-system",'
        ' "agent_namespace": "ai-agents", "workspace_url": null}'
    )
    cfg = pc.load()
    assert cfg["active"] == "default"
    assert cfg["targets"]["default"]["context"] == "minikube"
    assert cfg["targets"]["default"]["mode"] == "cluster"


def test_active_target_returns_name_and_fields(monkeypatch, tmp_path):
    pc = _fresh(monkeypatch, tmp_path)
    cfg = pc.load()
    pc.upsert_target(cfg, "minikube", mode="cluster", context="minikube")
    pc.upsert_target(cfg, "gcp", mode="hosted", workspace_url=None)
    assert cfg["active"] == "minikube"  # first target becomes active
    name, t = pc.active_target(cfg)
    assert name == "minikube" and t["context"] == "minikube"
    assert t["namespace"] == "purko-system"  # target field defaults applied


def test_set_active_switches_and_rejects_unknown(monkeypatch, tmp_path):
    pc = _fresh(monkeypatch, tmp_path)
    cfg = pc.load()
    pc.upsert_target(cfg, "minikube", mode="cluster", context="minikube")
    pc.upsert_target(cfg, "gcp", mode="hosted")
    pc.set_active(cfg, "gcp")
    assert pc.active_target(cfg)[0] == "gcp"
    try:
        pc.set_active(cfg, "nope")
        raised = False
    except KeyError:
        raised = True
    assert raised


def test_active_target_none_when_no_targets(monkeypatch, tmp_path):
    pc = _fresh(monkeypatch, tmp_path)
    cfg = pc.load()
    assert pc.active_target(cfg) == (None, None)


def test_targets_roundtrip_through_save(monkeypatch, tmp_path):
    pc = _fresh(monkeypatch, tmp_path)
    cfg = pc.load()
    pc.upsert_target(cfg, "minikube", mode="cluster", context="minikube")
    pc.save(cfg)
    name, t = pc.active_target(pc.load())
    assert name == "minikube" and t["mode"] == "cluster"
