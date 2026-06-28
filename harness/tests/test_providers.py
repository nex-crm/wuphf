from harness import providers as pr
from harness.providers import detect_providers, providers_payload

_ALLOWED_VIA = {"subscription_cli", "api_key", "local", "none"}


def _clear_keys(monkeypatch):
    for name in ("ANTHROPIC_API_KEY", "OPENAI_API_KEY", "WUPHF_OPENAI_API_KEY"):
        monkeypatch.delenv(name, raising=False)


def test_three_canonical_ids_always_present(monkeypatch):
    # The FE Settings surface needs a stable list: always exactly these three rows,
    # never dropped, regardless of env/PATH. (codex id is "codex", never "openai".)
    _clear_keys(monkeypatch)
    monkeypatch.setattr(pr.shutil, "which", lambda _bin: None)
    provs = detect_providers()
    assert [p.id for p in provs] == ["anthropic", "codex", "ollama"]


def test_via_values_from_allowed_set(monkeypatch):
    _clear_keys(monkeypatch)
    monkeypatch.setattr(pr.shutil, "which", lambda _bin: None)
    for p in detect_providers():
        assert p.via in _ALLOWED_VIA


def test_api_key_takes_api_key_via(monkeypatch):
    _clear_keys(monkeypatch)
    monkeypatch.setenv("ANTHROPIC_API_KEY", "sk-test")
    monkeypatch.setattr(pr.shutil, "which", lambda _bin: None)
    anthropic = next(p for p in detect_providers() if p.id == "anthropic")
    assert anthropic.available is True
    assert anthropic.via == "api_key"


def test_subscription_cli_via_when_only_binary(monkeypatch):
    _clear_keys(monkeypatch)
    monkeypatch.setattr(pr.shutil, "which", lambda bin_: bin_ if bin_ == "claude" else None)
    anthropic = next(p for p in detect_providers() if p.id == "anthropic")
    assert anthropic.available is True
    assert anthropic.via == "subscription_cli"


def test_payload_reports_any_available(monkeypatch):
    _clear_keys(monkeypatch)
    monkeypatch.setattr(pr.shutil, "which", lambda _bin: None)
    payload = providers_payload()
    assert payload["any_available"] is False
    assert len(payload["providers"]) == 3
