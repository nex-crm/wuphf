# wuphf-harness

The operator harness: **agentic-build / deterministic-execute**. One agent turns an
operator's plain-language description into a deterministic `WorkflowSpec`; a
deterministic executor runs it. No broker, no office, no multi-agent coordination.

This is the clean-start backend for the operator product. See
`docs/specs/operator-harness-clean-start.md`.

## Layout

```
src/harness/
  wire.py         FE<->harness contract (WorkflowSpec/Step, Build/Run requests; schema_version, extra=forbid)
  build_agent.py  BUILD: message -> WorkflowSpec. S0 = deterministic stub compiler; S2 = LangChain deep agent (same contract)
  executor.py     EXECUTE: run a spec deterministically; a gated step halts for the human approval card (CQ1)
  providers.py    multi-provider inference detection + BYOK status (no keys returned)
  mcp.py          MCP tool wiring: env-NAME->value config + allowed-tools grant (salvaged seam)
  service.py      FastAPI: /health, /providers, POST /build/stream (SSE), POST /run
tests/            wire / build_agent / executor / service
```

## Run

```bash
python3 -m venv .venv
.venv/bin/python -m pip install -e '.[dev]'      # add '.[agent]' for the real deep agent (S2)
.venv/bin/python -m pytest -q
.venv/bin/uvicorn harness.service:app --app-dir src --port 8810   # local service
```

## Smoke

`bash scripts/smoke.sh` boots the service and exercises the FE-facing endpoints
(`/health`, `/providers`, `/build/stream` SSE, `/run` incl. the approval-gate halt).

## Status (S0)

The clean-start scaffold: the FE-facing API is real end to end with a deterministic
stub build agent and a simulated executor. Slice S2 swaps the stub for the real
LangChain deep agent (planning + gbrain/browsersniff tools over MCP) behind the same
wire contract; S3 replaces the simulated executor with API-first replay. Runs
key-free; `build_agent()` degrades to the stub unless the `agent` extra is installed
and a supported model credential is available.
