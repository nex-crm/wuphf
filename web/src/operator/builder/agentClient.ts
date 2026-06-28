// agentClient — talks to the real pi-mono build agent (agent/ service) over the
// same WorkflowSpec contract the mock produces. Frontend-first + graceful: if the
// service is unreachable, fall back to the deterministic mock planWorkflow so the
// prototype always works. When the service is up, the operator gets the real
// engine (key-free via the operator's subscription /login, or local Ollama).
//
// The build streams over SSE: intermediate `status` events carry the agent's
// workings (its thinking, the tools it touches, results) so the UI can show what
// is happening live, then a terminal `spec` event carries the WorkflowPlan.

import type { WorkflowStep } from "../mock/data";
import {
  type ClarifyQuestion,
  planWorkflow,
  type WorkflowPlan,
} from "./planWorkflow";

// Vite env; defaults to the local agent service.
const AGENT_URL =
  (import.meta as unknown as { env?: Record<string, string> }).env
    ?.VITE_AGENT_URL ?? "http://127.0.0.1:8820";

/** A line in the agent's live activity trace, mirroring pi's own client. */
export interface BuildActivity {
  kind: "status" | "thinking" | "tool" | "tool_result" | "submitted";
  text: string;
  tool?: string;
}

export type OnActivity = (activity: BuildActivity) => void;

interface WireSpec {
  name?: string;
  tool_id?: string;
  steps?: WorkflowStep[];
  narration?: string;
  clarify?: { field?: string; prompt?: string; step_id?: string } | null;
}

function toPlan(spec: WireSpec): WorkflowPlan {
  const clarify: ClarifyQuestion | null =
    spec.clarify &&
    (spec.clarify.field === "threshold" || spec.clarify.field === "channel")
      ? {
          field: spec.clarify.field,
          prompt: String(spec.clarify.prompt ?? ""),
          stepId: String(spec.clarify.step_id ?? ""),
        }
      : null;
  return {
    name: String(spec.name ?? "Untitled workflow"),
    toolId: String(spec.tool_id ?? "inbound-routing"),
    steps: Array.isArray(spec.steps) ? spec.steps : [],
    narration: String(spec.narration ?? ""),
    clarify,
  };
}

/** Handle one parsed SSE event. Returns a spec when the terminal event lands. */
function handleEvent(
  event: string,
  dataRaw: string,
  onActivity?: OnActivity,
): WireSpec | null {
  let data: unknown;
  try {
    data = JSON.parse(dataRaw);
  } catch {
    return null;
  }
  if (event === "status") {
    const a = data as BuildActivity;
    if (a && typeof a.text === "string")
      onActivity?.({ kind: a.kind, text: a.text, tool: a.tool });
    return null;
  }
  if (event === "spec") {
    const { spec } = data as { spec?: WireSpec };
    return spec ?? null;
  }
  if (event === "error") {
    throw new Error(
      String((data as { error?: string }).error ?? "build failed"),
    );
  }
  return null;
}

/** Pull the event + data lines out of one SSE block. Comment lines are ignored. */
function parseSseBlock(block: string): { event: string; dataRaw: string } {
  let event = "";
  let dataRaw = "";
  for (const line of block.split("\n")) {
    if (line.startsWith("event:")) event = line.slice(6).trim();
    else if (line.startsWith("data:")) dataRaw = line.slice(5).trim();
  }
  return { event, dataRaw };
}

/** Drain whole SSE events (blank-line separated) from the buffer; return the rest. */
function drainSse(
  buffer: string,
  onActivity: OnActivity | undefined,
  onSpec: (spec: WireSpec) => void,
): string {
  let rest = buffer;
  let sep = rest.indexOf("\n\n");
  while (sep !== -1) {
    const { event, dataRaw } = parseSseBlock(rest.slice(0, sep));
    rest = rest.slice(sep + 2);
    if (event) {
      const found = handleEvent(event, dataRaw, onActivity);
      if (found) onSpec(found);
    }
    sep = rest.indexOf("\n\n");
  }
  return rest;
}

/** Stream /build/stream, forwarding activity, and resolve the terminal spec. */
async function buildPlanViaService(
  description: string,
  onActivity?: OnActivity,
): Promise<WorkflowPlan> {
  const res = await fetch(`${AGENT_URL}/build/stream`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ schema_version: 1, message: description }),
  });
  const { ok, body, status } = res;
  if (!(ok && body)) throw new Error(`agent service ${status}`);

  const reader = body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  let spec: WireSpec | null = null;

  // Parse SSE incrementally so activity surfaces as it streams.
  for (;;) {
    const { done, value } = await reader.read();
    if (value) buffer += decoder.decode(value, { stream: true });
    buffer = drainSse(buffer, onActivity, (found) => {
      spec = found;
    });
    if (done) break;
  }

  if (!spec) throw new Error("no spec event in build stream");
  return toPlan(spec);
}

// `fetch` rejects with a TypeError only when the request never reached a
// responding service (DNS failure, connection refused, offline). Once the
// service answers, every later failure — a non-2xx status, malformed SSE, a
// schema mismatch, or a terminal `error` event — is a real protocol error the
// operator must see, because runWorkflowViaService() still talks to the live
// backend. Falling back to the mock in those cases would let the UI "build"
// against a fake plan and then fail on run, so we only treat an unreachable
// service as fallback-worthy.
function isServiceUnreachable(err: unknown): boolean {
  return err instanceof TypeError;
}

/** Real engine when the service is reachable, else the deterministic mock. */
export async function buildPlanSmart(
  description: string,
  onActivity?: OnActivity,
): Promise<WorkflowPlan> {
  try {
    return await buildPlanViaService(description, onActivity);
  } catch (err) {
    if (isServiceUnreachable(err)) {
      return planWorkflow(description);
    }
    // Reachable service returned an HTTP/protocol/schema error — surface it
    // rather than silently masking it with a mock plan that cannot run.
    throw err instanceof Error
      ? err
      : new Error("agent build failed", { cause: err });
  }
}

/** Execute a built workflow on the agent service; returns the run result JSON. */
export async function runWorkflowViaService(
  spec: {
    name: string;
    tool_id: string;
    steps: WorkflowStep[];
    narration?: string;
    clarify?: unknown;
  },
  input: Record<string, unknown> = {},
): Promise<unknown> {
  const res = await fetch(`${AGENT_URL}/run`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ schema_version: 1, spec, input }),
  });
  if (!res.ok) throw new Error(`agent service ${res.status}`);
  return res.json();
}
