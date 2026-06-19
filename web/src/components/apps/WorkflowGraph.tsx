import { useMemo } from "react";
import {
  Background,
  type Edge,
  Handle,
  type Node,
  type NodeProps,
  Position,
  ReactFlow,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";

import type {
  RunRecord,
  WorkflowSpec,
  WorkflowTrigger,
} from "../../api/workflows";

/**
 * WorkflowGraph renders a frozen contract as nodes: triggers -> states ->
 * actions, laid out left-to-right by longest-path depth. The most recent run's
 * path (states entered + actions fired) is highlighted so you can see what the
 * last execution actually did.
 */

type NodeKind = "trigger" | "state" | "action";

interface WfNodeData extends Record<string, unknown> {
  label: string;
  kind: NodeKind;
  sub?: string;
  initial?: boolean;
  terminal?: boolean;
  active?: boolean;
}

const COL_W = 210;
const ROW_H = 84;

const KIND_ACCENT: Record<string, string> = {
  deterministic: "var(--text-secondary)",
  llm: "var(--accent)",
  external: "var(--amber, #b26b00)",
};

function WfNode({ data }: NodeProps<Node<WfNodeData>>) {
  const isAction = data.kind === "action";
  const isTrigger = data.kind === "trigger";
  const border = data.active
    ? "var(--green)"
    : isTrigger
      ? "var(--accent)"
      : "var(--border)";
  return (
    <div
      style={{
        minWidth: 150,
        maxWidth: 180,
        padding: "8px 11px",
        borderRadius: isAction ? 8 : 999,
        border: `${data.terminal ? 2.5 : 1.5}px solid ${border}`,
        background: data.active
          ? "var(--success-100, #e9fbef)"
          : "var(--bg-card)",
        boxShadow: data.initial ? "0 0 0 2px var(--accent) inset" : "none",
        fontSize: 12.5,
        color: "var(--text)",
        textAlign: "center",
      }}
    >
      <Handle type="target" position={Position.Left} style={{ opacity: 0 }} />
      <div style={{ fontWeight: 600, wordBreak: "break-word" }}>
        {isTrigger ? "▶ " : ""}
        {data.label}
      </div>
      {data.sub && (
        <div
          style={{
            fontSize: 10.5,
            color: data.active
              ? "var(--green)"
              : (KIND_ACCENT[data.sub] ?? "var(--text-secondary)"),
            marginTop: 2,
            textTransform: "uppercase",
            letterSpacing: ".04em",
          }}
        >
          {data.sub}
        </div>
      )}
      <Handle type="source" position={Position.Right} style={{ opacity: 0 }} />
    </div>
  );
}

const nodeTypes = { wf: WfNode };

interface WorkflowGraphProps {
  spec: WorkflowSpec;
  triggers: WorkflowTrigger[];
  lastRun?: RunRecord;
}

export default function WorkflowGraph({
  spec,
  triggers,
  lastRun,
}: WorkflowGraphProps) {
  const { nodes, edges } = useMemo(
    () => buildGraph(spec, triggers, lastRun),
    [spec, triggers, lastRun],
  );

  // fitView only runs on mount, so when a live edit grows the contract the new
  // right-most nodes sit off-canvas until reload. Keying ReactFlow to the node
  // signature remounts it on a shape change, re-fitting the whole graph.
  const shapeKey = useMemo(() => nodes.map((n) => n.id).join("|"), [nodes]);

  return (
    <div
      style={{
        height: 280,
        border: "1px solid var(--border)",
        borderRadius: 10,
        background: "var(--bg-warm)",
        overflow: "hidden",
      }}
    >
      <ReactFlow
        key={shapeKey}
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        fitView={true}
        fitViewOptions={{ padding: 0.12 }}
        // Default minZoom is 0.5, which leaves a long chain clipped because
        // fitView can't zoom out far enough; allow it to fit the whole contract.
        minZoom={0.2}
        proOptions={{ hideAttribution: true }}
        nodesDraggable={false}
        nodesConnectable={false}
        elementsSelectable={false}
        zoomOnScroll={false}
        panOnDrag={true}
      >
        <Background gap={18} color="var(--border)" />
      </ReactFlow>
    </div>
  );
}

function buildGraph(
  spec: WorkflowSpec,
  triggers: WorkflowTrigger[],
  lastRun?: RunRecord,
): { nodes: Node<WfNodeData>[]; edges: Edge[] } {
  const actionById = new Map(spec.actions.map((a) => [a.id, a]));
  const terminal = new Set(spec.terminal ?? []);
  const ranStates = new Set(lastRun?.result.state_seq ?? []);
  const ranActions = new Set(lastRun?.result.actions_fired ?? []);

  const nodes: Node<WfNodeData>[] = [];
  const edges: Edge[] = [];
  const adj: Record<string, string[]> = {};
  const addEdge = (from: string, to: string, active: boolean) => {
    (adj[from] ??= []).push(to);
    edges.push({
      id: `${from}->${to}`,
      source: from,
      target: to,
      animated: active,
      style: {
        stroke: active ? "var(--green)" : "var(--border)",
        strokeWidth: active ? 2 : 1.5,
      },
    });
  };

  // One trigger node feeding the initial state (keeps the canvas readable; the
  // trigger list below the graph enumerates schedule/event detail).
  const triggerLabel =
    triggers
      .map((t) => t.label)
      .slice(0, 2)
      .join(" · ") || "Trigger";
  const triggerId = "__trigger";
  nodes.push(nodeOf(triggerId, { label: triggerLabel, kind: "trigger" }));

  // State nodes.
  for (const s of spec.states) {
    nodes.push(
      nodeOf(`s:${s.id}`, {
        label: s.label || s.id,
        kind: "state",
        initial: s.id === spec.initial,
        terminal: terminal.has(s.id),
        active: ranStates.has(s.id),
      }),
    );
  }
  addEdge(triggerId, `s:${spec.initial}`, ranStates.has(spec.initial));

  // Expand each transition into a chain through its action nodes.
  spec.transitions.forEach((t, ti) => {
    const acts = t.actions ?? [];
    const transitionRan =
      ranStates.has(t.from) && ranStates.has(t.to) && lastRun != null;
    let prev = `s:${t.from}`;
    acts.forEach((aid, ai) => {
      const nid = `a:${ti}:${ai}:${aid}`;
      const a = actionById.get(aid);
      nodes.push(
        nodeOf(nid, {
          label: aid,
          kind: "action",
          sub: a?.kind,
          active: transitionRan && ranActions.has(aid),
        }),
      );
      addEdge(prev, nid, transitionRan && ranActions.has(aid));
      prev = nid;
    });
    addEdge(prev, `s:${t.to}`, transitionRan);
  });

  layout(nodes, adj, triggerId);
  return { nodes, edges };
}

function nodeOf(id: string, data: WfNodeData): Node<WfNodeData> {
  return { id, type: "wf", position: { x: 0, y: 0 }, data };
}

// layout assigns x by longest-path depth from the trigger and y by order within
// a depth. Back-edges (cycles from exception handling) do not deepen, so it
// terminates on any graph.
function layout(
  nodes: Node<WfNodeData>[],
  adj: Record<string, string[]>,
  root: string,
) {
  const depth: Record<string, number> = {};
  const visiting = new Set<string>();
  const dfs = (id: string): number => {
    if (depth[id] !== undefined) return depth[id];
    if (visiting.has(id)) return 0; // cycle guard
    visiting.add(id);
    let d = 0;
    for (const nxt of adj[id] ?? []) {
      d = Math.max(d, dfs(nxt) + 1);
    }
    visiting.delete(id);
    depth[id] = d;
    return d;
  };
  // Depth as longest path TO a node: invert by computing from root forward.
  const fwd: Record<string, number> = { [root]: 0 };
  const order: string[] = [root];
  for (let i = 0; i < order.length; i++) {
    const cur = order[i];
    for (const nxt of adj[cur] ?? []) {
      const cand = (fwd[cur] ?? 0) + 1;
      if (fwd[nxt] === undefined || cand > fwd[nxt]) {
        fwd[nxt] = cand;
        order.push(nxt);
      }
    }
  }
  // Any node never reached (isolated) gets depth from its own dfs.
  for (const n of nodes) if (fwd[n.id] === undefined) fwd[n.id] = dfs(n.id);

  const byCol: Record<number, Node<WfNodeData>[]> = {};
  for (const n of nodes) {
    const c = fwd[n.id] ?? 0;
    (byCol[c] ??= []).push(n);
  }
  for (const [col, group] of Object.entries(byCol)) {
    const c = Number(col);
    group.forEach((n, i) => {
      n.position = {
        x: c * COL_W,
        y: i * ROW_H - ((group.length - 1) * ROW_H) / 2,
      };
    });
  }
}
