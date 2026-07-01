// Client for the pi-mono agent's /tools/build endpoint — the app's chat teaches a
// workflow, the agent's create_tool tool authors it, and this returns the tool it
// made. Proxied via /agent in dev (see vite.config.ts). Mirrors the agent wire
// (agent/src/wire.ts ToolBuildRequest/ToolBuildResult).

import {
  authorToolFromDescription,
  type Tool,
  type ToolInput,
} from "./mockTools";

const SCHEMA_VERSION = 1;

interface HarnessTool {
  name: string;
  title: string;
  purpose: string;
  inputs: ToolInput[];
  code: string;
}

interface ToolBuildResult {
  tool: HarnessTool | null;
  narration: string;
}

let seq = 0;
function nextId(): string {
  seq += 1;
  return `tool_h${seq}`;
}

// Map the harness Tool (no FE-only id/calls/script naming) onto the FE Tool.
function toFeTool(h: HarnessTool, createdFrom: string): Tool {
  return {
    id: nextId(),
    title: h.title,
    name: h.name,
    purpose: h.purpose,
    inputs: h.inputs,
    script: h.code,
    createdFrom,
    calls: [],
  };
}

export interface BuiltTool {
  tool: Tool;
  narration: string;
  /** True when the harness was unreachable and we fell back to the local mock. */
  offline: boolean;
}

/**
 * Ask the harness chat agent to build a tool for a described workflow. Falls back
 * to the local deterministic mock when the harness is unreachable, so the FE keeps
 * working offline (the mock and the harness stub share the same shapes).
 */
export async function buildToolFromChat(
  message: string,
  app: string,
): Promise<BuiltTool> {
  try {
    const res = await fetch("/agent/tools/build", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ schema_version: SCHEMA_VERSION, message, app }),
    });
    if (!res.ok) throw new Error(`harness ${res.status}`);
    const data = (await res.json()) as ToolBuildResult;
    if (!data.tool) throw new Error("harness returned no tool");
    return {
      tool: toFeTool(data.tool, message),
      narration: data.narration,
      offline: false,
    };
  } catch {
    const tool = authorToolFromDescription(message);
    return { tool, narration: `Built ${tool.title}.`, offline: true };
  }
}

// --- Calling a tool (POST /tools/call) --------------------------------------
// Mirrors agent/src/wire.ts ToolCallRequest/ToolCallResult. Executing is the
// AGENT's job: there is no mock execution fallback — offline is an error.

export interface ToolCallGate {
  capability: string;
  detail: string;
}

export interface ToolCallOutcome {
  status: "ok" | "needs_approval" | "error";
  result?: string;
  detail?: string;
  gate?: ToolCallGate;
  /** Every capability call the tool made, e.g. crm.deals({"since":"7d"}). */
  actions: string[];
}

/**
 * The app's chat calls a saved tool via the agent. `approved` carries the
 * human's answer to the approval card (gated capabilities default-deny).
 */
export async function callToolViaAgent(
  tool: Tool,
  args: Record<string, string>,
  approved = false,
): Promise<ToolCallOutcome> {
  try {
    const res = await fetch("/agent/tools/call", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        schema_version: SCHEMA_VERSION,
        // FE Tool stores the code as `script`; the wire field is `code`.
        tool: {
          name: tool.name,
          title: tool.title,
          purpose: tool.purpose,
          inputs: tool.inputs,
          code: tool.script,
        },
        args,
        approved,
      }),
    });
    if (!res.ok) throw new Error(`agent ${res.status}`);
    const data = (await res.json()) as ToolCallOutcome;
    return {
      ...data,
      actions: Array.isArray(data.actions) ? data.actions : [],
    };
  } catch {
    return {
      status: "error",
      detail: "agent offline — start the agent service",
      actions: [],
    };
  }
}
