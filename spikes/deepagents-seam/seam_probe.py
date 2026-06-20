"""
Spike probe: prove the Go-broker <-> Python-deepagents seam.

Decision under test (D3=A): keep the Go broker as coordinator; run the agent
INNER loop in Python deepagents (LangGraph), reusing WUPHF's existing tools by
connecting to the Go `teammcp` MCP server. This script measures whether that
seam actually works and how much it costs.

Layers (each independent; each prints a real number even if a later layer fails):

  L1  MCP interop      Python MCP client spawns real `wuphf mcp-team` over stdio,
                       handshakes, lists tools.  -> initialize ms, tools/list ms, tool count
  L1b MCP round-trip   N calls to a same-SDK Go echo server.  -> median / p95 round-trip ms
                       (isolates OUR seam cost from broker + LLM cost)
  L2  deepagents build create_deep_agent(tools=<teammcp tools over MCP>) and capture
                       exactly which tools deepagents binds. -> proves deepagents wraps
                       teammcp tools AND injects planning/filesystem/subagent tools.
  L3  HITL             interrupt_on={tool: True} pauses before a tool call.
                       -> proves human-in-the-loop maps onto the broker's approval gate.

No model API key needed: deterministic fake chat models drive any live turn, so we
measure the SEAM, not vendor LLM latency.
"""

import asyncio
import json
import os
import pathlib
import statistics
import time
import traceback
from typing import Any, List

HERE = pathlib.Path(__file__).parent
WUPHF = HERE / "bin" / "wuphf"
ECHO = HERE / "bin" / "echo-server"

results: dict[str, Any] = {}


def teammcp_env() -> dict:
    # markdown backend surfaces notebook tools; everything else inherited.
    return {**os.environ, "WUPHF_MEMORY_BACKEND": "markdown"}


# --------------------------------------------------------------------------- #
# Fake chat models (no API key) — just enough to drive the LangGraph loop.
# --------------------------------------------------------------------------- #
def _make_models():
    from langchain_core.language_models.chat_models import BaseChatModel
    from langchain_core.messages import AIMessage
    from langchain_core.outputs import ChatGeneration, ChatResult
    from pydantic import Field

    class CaptureModel(BaseChatModel):
        """Records the tool surface deepagents binds, then ends the turn."""

        bound_tool_names: List[str] = Field(default_factory=list)
        n_calls: int = 0

        def bind_tools(self, tools, **kwargs):
            names = []
            for tl in tools:
                n = getattr(tl, "name", None)
                if n is None and isinstance(tl, dict):
                    n = tl.get("name") or tl.get("function", {}).get("name")
                if n is None:
                    n = getattr(tl, "__name__", "<unknown>")
                names.append(n)
            self.bound_tool_names = names
            return self

        def _generate(self, messages, stop=None, run_manager=None, **kwargs):
            self.n_calls += 1
            return ChatResult(generations=[ChatGeneration(message=AIMessage(content="spike-final"))])

        @property
        def _llm_type(self):
            return "capture-fake"

    class ToolCallOnceModel(BaseChatModel):
        """Emits exactly one tool call (to trigger an interrupt), then finishes."""

        tool_name: str = "echo"
        n_calls: int = 0

        def bind_tools(self, tools, **kwargs):
            return self

        def _generate(self, messages, stop=None, run_manager=None, **kwargs):
            self.n_calls += 1
            if self.n_calls == 1:
                msg = AIMessage(
                    content="",
                    tool_calls=[{"name": self.tool_name, "args": {"text": "hi"}, "id": "call_1", "type": "tool_call"}],
                )
            else:
                msg = AIMessage(content="done")
            return ChatResult(generations=[ChatGeneration(message=msg)])

        @property
        def _llm_type(self):
            return "toolcall-once"

    return CaptureModel, ToolCallOnceModel


# --------------------------------------------------------------------------- #
# L1: real teammcp interop
# --------------------------------------------------------------------------- #
async def layer1_teammcp_interop():
    from mcp import ClientSession, StdioServerParameters
    from mcp.client.stdio import stdio_client

    params = StdioServerParameters(command=str(WUPHF), args=["mcp-team"], env=teammcp_env())
    async with stdio_client(params) as (r, w):
        async with ClientSession(r, w) as s:
            t0 = time.perf_counter()
            await s.initialize()
            init_ms = (time.perf_counter() - t0) * 1000
            t0 = time.perf_counter()
            tl = await s.list_tools()
            list_ms = (time.perf_counter() - t0) * 1000
            names = [t.name for t in tl.tools]
            return {
                "ok": True,
                "server": "wuphf mcp-team (real teammcp)",
                "initialize_ms": round(init_ms, 1),
                "list_tools_ms": round(list_ms, 1),
                "tool_count": len(names),
                "sample_tools": sorted(names)[:18],
            }


# --------------------------------------------------------------------------- #
# L1b: clean round-trip against same-SDK echo server
# --------------------------------------------------------------------------- #
async def layer1b_echo_roundtrip(n: int = 30):
    from mcp import ClientSession, StdioServerParameters
    from mcp.client.stdio import stdio_client

    params = StdioServerParameters(command=str(ECHO), args=[], env={**os.environ})
    async with stdio_client(params) as (r, w):
        async with ClientSession(r, w) as s:
            await s.initialize()
            await s.call_tool("echo", {"text": "warmup"})  # discard cold start
            times = []
            for i in range(n):
                t0 = time.perf_counter()
                await s.call_tool("echo", {"text": f"ping-{i}"})
                times.append((time.perf_counter() - t0) * 1000)
            times.sort()
            return {
                "ok": True,
                "server": "echo-server (same Go MCP SDK, no broker)",
                "calls": n,
                "median_ms": round(statistics.median(times), 2),
                "min_ms": round(times[0], 2),
                "p95_ms": round(times[min(int(n * 0.95), n - 1)], 2),
            }


# --------------------------------------------------------------------------- #
# L2: deepagents builds over teammcp tools; capture the bound tool surface
# --------------------------------------------------------------------------- #
async def layer2_deepagents_over_teammcp():
    from langchain_mcp_adapters.client import MultiServerMCPClient

    client = MultiServerMCPClient(
        {"teammcp": {"command": str(WUPHF), "args": ["mcp-team"], "transport": "stdio", "env": teammcp_env()}}
    )
    mcp_tools = await client.get_tools()

    from deepagents import create_deep_agent

    CaptureModel, _ = _make_models()
    cm = CaptureModel()
    agent = create_deep_agent(model=cm, tools=mcp_tools)
    await agent.ainvoke({"messages": [{"role": "user", "content": "ping"}]})

    bound = list(cm.bound_tool_names)
    deep_builtins = sorted(
        n for n in bound if n in {"write_todos", "ls", "read_file", "write_file", "edit_file", "glob", "grep", "task"}
    )
    teammcp_bound = sorted(n for n in bound if n not in set(deep_builtins))
    return {
        "ok": True,
        "mcp_tools_loaded": len(mcp_tools),
        "total_tools_bound_by_deepagents": len(bound),
        "deepagents_builtins_present": deep_builtins,
        "teammcp_tools_reused_sample": teammcp_bound[:12],
        "teammcp_tools_reused_count": len(teammcp_bound),
    }


# --------------------------------------------------------------------------- #
# L3: HITL interrupt before a tool call
# --------------------------------------------------------------------------- #
async def layer3_hitl_interrupt():
    # Use the echo MCP tool so the interrupt fires without needing a broker.
    from langchain_mcp_adapters.client import MultiServerMCPClient

    client = MultiServerMCPClient(
        {"echo": {"command": str(ECHO), "args": [], "transport": "stdio", "env": {**os.environ}}}
    )
    tools = await client.get_tools()

    from deepagents import create_deep_agent
    from langgraph.checkpoint.memory import MemorySaver

    _, ToolCallOnceModel = _make_models()
    model = ToolCallOnceModel(tool_name="echo")
    agent = create_deep_agent(
        model=model,
        tools=tools,
        interrupt_on={"echo": True},
        checkpointer=MemorySaver(),
    )
    cfg = {"configurable": {"thread_id": "spike-hitl-1"}}
    state = await agent.ainvoke({"messages": [{"role": "user", "content": "echo hi"}]}, config=cfg)
    interrupted = "__interrupt__" in state if isinstance(state, dict) else False
    return {
        "ok": True,
        "interrupted_before_tool": bool(interrupted),
        "mechanism": "deepagents interrupt_on -> LangGraph interrupt; maps to broker approval gate",
    }


# --------------------------------------------------------------------------- #
async def run_layer(name, coro_fn, *args):
    print(f"\n=== {name} ===", flush=True)
    try:
        out = await coro_fn(*args)
        results[name] = out
        print(json.dumps(out, indent=2), flush=True)
    except Exception as e:  # noqa: BLE001 — spike wants the failure recorded, not raised
        results[name] = {"ok": False, "error": f"{type(e).__name__}: {e}", "trace": traceback.format_exc()[-800:]}
        print(f"FAILED: {type(e).__name__}: {e}", flush=True)


async def main():
    print(f"wuphf binary : {WUPHF} ({'present' if WUPHF.exists() else 'MISSING'})")
    print(f"echo binary  : {ECHO} ({'present' if ECHO.exists() else 'MISSING'})")
    await run_layer("L1_mcp_interop", layer1_teammcp_interop)
    await run_layer("L1b_echo_roundtrip", layer1b_echo_roundtrip)
    await run_layer("L2_deepagents_over_teammcp", layer2_deepagents_over_teammcp)
    await run_layer("L3_hitl_interrupt", layer3_hitl_interrupt)
    (HERE / "results.json").write_text(json.dumps(results, indent=2))
    print("\nWrote results.json")


if __name__ == "__main__":
    asyncio.run(main())
