// Per-app tools state, shared by the app's Ask-AI chat (which authors tools by
// calling create_tool) and its Tools tab (which lists them). Scoped to one app
// detail mount, so switching apps re-seeds. With a REAL agent id (app_…) the
// initial tools hydrate from the agent service's persisted store
// (GET /agent/tools?agent=…), falling back to the local seeds when the service
// is unreachable. See docs/specs/operator-workflows-as-tools.md.

import {
  createContext,
  type ReactNode,
  useContext,
  useEffect,
  useMemo,
  useState,
} from "react";

import {
  tryListAgentTools,
  type WireTool,
  type WireToolInput,
} from "../agents/agentStateClient";
import { isRealAppId } from "../apps/useOperatorApps";
import {
  seedToolsForApp,
  type Tool,
  type ToolCall,
  type ToolInput,
} from "./mockTools";

interface AppToolsValue {
  tools: Tool[];
  /** The REAL agent id (app_…) when this provider is bound to one — the chat
   * passes it as the `app` field on /tools/build so the service persists
   * per-agent. Undefined for mock agents. */
  agentId?: string;
  /** Register a tool (re-teaching a workflow updates it in place, by name). */
  addTool: (tool: Tool) => void;
  /** Record a completed call on a tool's history (shows as "Last run"). */
  logCall: (toolId: string, call: ToolCall) => void;
}

const Ctx = createContext<AppToolsValue | null>(null);

// The wire carries a loose optional input type; the FE shape is a closed union.
function feInput(input: WireToolInput): ToolInput {
  return {
    name: input.name,
    type:
      input.type === "number" || input.type === "record"
        ? input.type
        : "string",
  };
}

let hydratedSeq = 0;

// Map a persisted wire tool onto the FE Tool shape: `script` is the wire
// `code`; run history is session-local, so hydrated tools start with no calls.
function feTool(w: WireTool): Tool {
  hydratedSeq += 1;
  return {
    id: `tool_p${hydratedSeq}`,
    title: w.title,
    name: w.name,
    purpose: w.purpose,
    inputs: (w.inputs ?? []).map(feInput),
    script: w.code,
    createdFrom: "taught in chat",
    calls: [],
  };
}

export function ToolsProvider({
  appName,
  agentId,
  children,
}: {
  appName: string;
  /** Real agent id (app_…); hydrates tools from the agent service when set. */
  agentId?: string;
  children: ReactNode;
}) {
  const [tools, setTools] = useState<Tool[]>(() => seedToolsForApp(appName));

  const realId = isRealAppId(agentId) ? agentId : undefined;

  useEffect(() => {
    if (!realId) return;
    let cancelled = false;
    void tryListAgentTools(realId).then((wire) => {
      if (cancelled || !wire) return; // unreachable — keep the seeded state
      // The service's answer IS the agent's toolbox — honest even when empty.
      setTools(wire.map(feTool));
    });
    return () => {
      cancelled = true;
    };
  }, [realId]);

  const value = useMemo<AppToolsValue>(
    () => ({
      tools,
      agentId: realId,
      addTool: (tool) =>
        setTools((prev) => {
          // Re-teaching a same-named tool replaces it but keeps its run
          // history, so "Last run" survives an update to the tool.
          const prevSameName = prev.find((t) => t.name === tool.name);
          return [
            ...prev.filter((t) => t.name !== tool.name),
            { ...tool, calls: prevSameName?.calls ?? tool.calls },
          ];
        }),
      logCall: (toolId, call) =>
        setTools((prev) =>
          prev.map((t) =>
            t.id === toolId ? { ...t, calls: [...t.calls, call] } : t,
          ),
        ),
    }),
    [tools, realId],
  );

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useAppTools(): AppToolsValue {
  const value = useContext(Ctx);
  if (!value) {
    throw new Error("useAppTools must be used within a ToolsProvider");
  }
  return value;
}
