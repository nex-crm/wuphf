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
