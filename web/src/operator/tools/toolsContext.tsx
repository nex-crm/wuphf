// Per-app tools state, shared by the app's Ask-AI chat (which authors tools by
// calling create_tool) and its Tools tab (which lists them). Scoped to one app
// detail mount, so switching apps re-seeds. This is the FE seam that mirrors the
// harness ToolStore; when the real chat agent is wired, this provider is fed from
// it. See docs/specs/operator-workflows-as-tools.md.

import {
  createContext,
  type ReactNode,
  useContext,
  useMemo,
  useState,
} from "react";

import { seedToolsForApp, type Tool, type ToolCall } from "./mockTools";

interface AppToolsValue {
  tools: Tool[];
  /** Register a tool (re-teaching a workflow updates it in place, by name). */
  addTool: (tool: Tool) => void;
  /** Record a completed call on a tool's history (shows as "Last run"). */
  logCall: (toolId: string, call: ToolCall) => void;
}

const Ctx = createContext<AppToolsValue | null>(null);

export function ToolsProvider({
  appName,
  children,
}: {
  appName: string;
  children: ReactNode;
}) {
  const [tools, setTools] = useState<Tool[]>(() => seedToolsForApp(appName));

  const value = useMemo<AppToolsValue>(
    () => ({
      tools,
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
    [tools],
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
