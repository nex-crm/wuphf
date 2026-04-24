/**
 * GraphApp — cross-entity knowledge graph view.
 *
 * Port of the TUI /graph surface (internal/tui/render/graph.go). The broker
 * exposes every brief + every coalesced edge at GET /entity/graph/all; this
 * component reads that payload and lays it out with a tiny hand-rolled
 * force-directed simulation in SVG. No external graph libraries are
 * imported — the whole view adds ~10kb to the bundle instead of ~200kb for
 * react-force-graph / d3.
 *
 * Interactions:
 *  - Hover a node → highlight + show tooltip
 *  - Hover an edge → show the fact id that first produced it
 *  - Click a node → open its wiki page
 *  - Legend (bottom-right) shows kind counts
 */

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";

import {
  type EntityKind,
  fetchEntityGraphAll,
  type GraphAllResponse,
} from "../../api/entity";
import { useAppStore } from "../../stores/app";

// ── Types ────────────────────────────────────────────────────────

interface SimNode {
  id: string;
  kind: EntityKind;
  slug: string;
  title: string;
  x: number;
  y: number;
  vx: number;
  vy: number;
}

interface SimEdge {
  from: string;
  to: string;
  label: string;
  occurrenceCount: number;
}

// ── Visual tokens ────────────────────────────────────────────────
//
// Three kinds drive the node palette. The TUI uses more (deals, tasks,
// tickets, …) but the cross-entity graph built in v1.2 only tracks the
// three `ValidEntityKinds` (people | companies | customers). Any future
// kinds the broker emits will fall back to the "other" style instead of
// breaking the render.

const NODE_STYLES: Record<
  string,
  { fill: string; stroke: string; icon: string; label: string }
> = {
  people: {
    fill: "#EDE9FE",
    stroke: "#7C3AED",
    icon: "👤", // 👤
    label: "People",
  },
  companies: {
    fill: "#DBEAFE",
    stroke: "#2563EB",
    icon: "🏢", // 🏢
    label: "Companies",
  },
  customers: {
    fill: "#DCFCE7",
    stroke: "#059669",
    icon: "🤝", // handshake — customer = relationship
    label: "Customers",
  },
};

function styleFor(kind: string) {
  return (
    NODE_STYLES[kind] ?? {
      fill: "#F3F4F6",
      stroke: "#6B7280",
      icon: "◆",
      label: kind,
    }
  );
}

const NODE_RADIUS = 28;
const LABEL_MAX_CHARS = 18;

function truncateLabel(label: string): string {
  if (label.length <= LABEL_MAX_CHARS) return label;
  return `${label.slice(0, LABEL_MAX_CHARS - 1)}…`;
}

// ── Force simulation ─────────────────────────────────────────────
//
// Barebones velocity-Verlet style loop. Good enough for <100 nodes, which
// is the realistic ceiling for a v1 cross-entity brief catalog. If the
// graph ever exceeds that, swap in d3-force (still bundle-cheap at ~30kb).

interface SimOpts {
  width: number;
  height: number;
  iterations: number;
}

function runSimulation(
  nodes: SimNode[],
  edges: SimEdge[],
  { width, height, iterations }: SimOpts,
): void {
  if (nodes.length === 0) return;
  const cx = width / 2;
  const cy = height / 2;
  // Scale forces with node count so a 6-node graph spreads across the canvas
  // the same way a 30-node graph does. Empirically: repulse grows with n,
  // link length with √n.
  const n = nodes.length;
  const idealLink = Math.min(280, 140 + n * 8);
  const repulse = 18000 + n * 600;
  const centerPull = 0.008;

  // Adjacency for quick neighbor lookup.
  const adjacency = new Map<string, string[]>();
  for (const n of nodes) adjacency.set(n.id, []);
  for (const e of edges) {
    adjacency.get(e.from)?.push(e.to);
    adjacency.get(e.to)?.push(e.from);
  }

  for (let step = 0; step < iterations; step++) {
    const cooling = 1 - step / iterations;
    // Reset forces.
    const fx = new Map<string, number>();
    const fy = new Map<string, number>();
    for (const n of nodes) {
      fx.set(n.id, 0);
      fy.set(n.id, 0);
    }

    // Repulsion between every pair of nodes (O(n²) — fine for v1 scale).
    for (let i = 0; i < nodes.length; i++) {
      for (let j = i + 1; j < nodes.length; j++) {
        const a = nodes[i];
        const b = nodes[j];
        let dx = a.x - b.x;
        let dy = a.y - b.y;
        let d2 = dx * dx + dy * dy;
        if (d2 < 1) {
          dx = Math.random() - 0.5;
          dy = Math.random() - 0.5;
          d2 = 1;
        }
        const f = repulse / d2;
        const dist = Math.sqrt(d2);
        const nx = dx / dist;
        const ny = dy / dist;
        fx.set(a.id, (fx.get(a.id) ?? 0) + nx * f);
        fy.set(a.id, (fy.get(a.id) ?? 0) + ny * f);
        fx.set(b.id, (fx.get(b.id) ?? 0) - nx * f);
        fy.set(b.id, (fy.get(b.id) ?? 0) - ny * f);
      }
    }

    // Attraction along edges (Hooke's law toward idealLink).
    const byId = new Map<string, SimNode>();
    for (const n of nodes) byId.set(n.id, n);
    for (const e of edges) {
      const a = byId.get(e.from);
      const b = byId.get(e.to);
      if (!(a && b)) continue;
      const dx = b.x - a.x;
      const dy = b.y - a.y;
      const dist = Math.sqrt(dx * dx + dy * dy) || 1;
      const f = (dist - idealLink) * 0.05;
      const nx = dx / dist;
      const ny = dy / dist;
      fx.set(a.id, (fx.get(a.id) ?? 0) + nx * f);
      fy.set(a.id, (fy.get(a.id) ?? 0) + ny * f);
      fx.set(b.id, (fx.get(b.id) ?? 0) - nx * f);
      fy.set(b.id, (fy.get(b.id) ?? 0) - ny * f);
    }

    // Center pull keeps disconnected nodes from drifting off-canvas.
    for (const n of nodes) {
      fx.set(n.id, (fx.get(n.id) ?? 0) + (cx - n.x) * centerPull);
      fy.set(n.id, (fy.get(n.id) ?? 0) + (cy - n.y) * centerPull);
    }

    // Integrate + clamp to canvas.
    const damping = 0.85;
    const maxVel = 30;
    for (const n of nodes) {
      n.vx = (n.vx + (fx.get(n.id) ?? 0)) * damping * cooling;
      n.vy = (n.vy + (fy.get(n.id) ?? 0)) * damping * cooling;
      if (n.vx > maxVel) n.vx = maxVel;
      if (n.vx < -maxVel) n.vx = -maxVel;
      if (n.vy > maxVel) n.vy = maxVel;
      if (n.vy < -maxVel) n.vy = -maxVel;
      n.x += n.vx;
      n.y += n.vy;
      const pad = NODE_RADIUS + 8;
      if (n.x < pad) n.x = pad;
      if (n.x > width - pad) n.x = width - pad;
      if (n.y < pad) n.y = pad;
      if (n.y > height - pad) n.y = height - pad;
    }
  }
}

// ── Component ────────────────────────────────────────────────────

export function GraphApp() {
  const setCurrentApp = useAppStore((s) => s.setCurrentApp);
  const setWikiPath = useAppStore((s) => s.setWikiPath);
  const containerRef = useRef<HTMLDivElement>(null);
  const [size, setSize] = useState({ width: 800, height: 600 });
  const [hoveredNode, setHoveredNode] = useState<string | null>(null);
  const [hoveredEdge, setHoveredEdge] = useState<number | null>(null);

  const { data, isLoading, error } = useQuery<GraphAllResponse>({
    queryKey: ["entity-graph-all"],
    queryFn: fetchEntityGraphAll,
    refetchInterval: 15_000,
  });

  // Responsive canvas sizing — recompute on container resize.
  useEffect(() => {
    if (!containerRef.current) return;
    const el = containerRef.current;
    const update = () => {
      const rect = el.getBoundingClientRect();
      setSize({
        width: Math.max(400, rect.width),
        height: Math.max(400, rect.height),
      });
    };
    update();
    const ro = new ResizeObserver(update);
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  // Seed layout + run sim whenever the payload or canvas size changes.
  const simResult = useMemo(() => {
    if (!data) return null;
    const nodes: SimNode[] = data.nodes.map((n, i) => {
      // Arrange seeds on a golden-ratio spiral so the sim starts from a
      // reasonable non-collinear layout. Random seeds caused the first few
      // frames to look like an exploding spider.
      const angle = i * 2.399963229728653; // 2π / φ²
      const radius = 30 + Math.sqrt(i) * 35;
      return {
        id: `${n.kind}/${n.slug}`,
        kind: n.kind,
        slug: n.slug,
        title: n.title || n.slug,
        x: size.width / 2 + Math.cos(angle) * radius,
        y: size.height / 2 + Math.sin(angle) * radius,
        vx: 0,
        vy: 0,
      };
    });
    const edges: SimEdge[] = data.edges.map((e) => ({
      from: `${e.from_kind}/${e.from_slug}`,
      to: `${e.to_kind}/${e.to_slug}`,
      label: e.first_seen_fact_id || "",
      occurrenceCount: e.occurrence_count,
    }));
    runSimulation(nodes, edges, {
      width: size.width,
      height: size.height,
      iterations: 380,
    });
    return { nodes, edges };
  }, [data, size.width, size.height]);

  const nodesById = useMemo(() => {
    const m = new Map<string, SimNode>();
    for (const n of simResult?.nodes ?? []) m.set(n.id, n);
    return m;
  }, [simResult]);

  const legendCounts = useMemo(() => {
    const counts = new Map<string, number>();
    for (const n of simResult?.nodes ?? []) {
      counts.set(n.kind, (counts.get(n.kind) ?? 0) + 1);
    }
    return counts;
  }, [simResult]);

  const handleNodeClick = useCallback(
    (node: SimNode) => {
      setCurrentApp("wiki");
      setWikiPath(`team/${node.kind}/${node.slug}.md`);
    },
    [setCurrentApp, setWikiPath],
  );

  const totalNodes = simResult?.nodes.length ?? 0;
  const totalEdges = simResult?.edges.length ?? 0;

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        height: "100%",
        width: "100%",
        background: "var(--bg)",
      }}
    >
      <header
        style={{
          padding: "16px 24px",
          borderBottom: "1px solid var(--border)",
          display: "flex",
          alignItems: "baseline",
          justifyContent: "space-between",
          gap: 16,
          flexShrink: 0,
        }}
      >
        <div>
          <h3 style={{ fontSize: 18, fontWeight: 600, margin: 0 }}>
            Entity Graph
          </h3>
          <p
            style={{
              fontSize: 13,
              color: "var(--text-tertiary)",
              margin: "4px 0 0",
            }}
          >
            People, companies, and customers the team has written facts about —
            and how they connect.
          </p>
        </div>
        <div
          style={{
            fontSize: 12,
            color: "var(--text-tertiary)",
            fontVariantNumeric: "tabular-nums",
          }}
        >
          {totalNodes} node{totalNodes === 1 ? "" : "s"} · {totalEdges} edge
          {totalEdges === 1 ? "" : "s"}
        </div>
      </header>

      <div
        ref={containerRef}
        style={{
          position: "relative",
          flex: 1,
          overflow: "hidden",
          background:
            "radial-gradient(circle at 50% 40%, var(--bg-subtle) 0%, var(--bg) 70%)",
        }}
      >
        {isLoading ? (
          <div
            style={{
              position: "absolute",
              inset: 0,
              display: "flex",
              alignItems: "center",
              justifyContent: "center",
              color: "var(--text-tertiary)",
              fontSize: 14,
            }}
          >
            Loading graph...
          </div>
        ) : error ? (
          <div
            style={{
              position: "absolute",
              inset: 0,
              display: "flex",
              alignItems: "center",
              justifyContent: "center",
              color: "var(--text-tertiary)",
              fontSize: 14,
            }}
          >
            Could not load graph: {(error as Error).message}
          </div>
        ) : !simResult || simResult.nodes.length === 0 ? (
          <EmptyState />
        ) : (
          <>
            <svg
              width={size.width}
              height={size.height}
              style={{ display: "block", userSelect: "none" }}
            >
              <defs>
                <marker
                  id="graph-arrow"
                  viewBox="0 0 10 10"
                  refX="9"
                  refY="5"
                  markerWidth="6"
                  markerHeight="6"
                  orient="auto-start-reverse"
                >
                  <path d="M 0 0 L 10 5 L 0 10 z" fill="var(--text-tertiary)" />
                </marker>
              </defs>

              {/* Edges first so nodes paint on top. */}
              {simResult.edges.map((e, i) => {
                const a = nodesById.get(e.from);
                const b = nodesById.get(e.to);
                if (!(a && b)) return null;
                const active =
                  hoveredEdge === i ||
                  hoveredNode === a.id ||
                  hoveredNode === b.id;
                // Shrink the line ends so the arrow doesn't plunge into the node.
                const dx = b.x - a.x;
                const dy = b.y - a.y;
                const dist = Math.sqrt(dx * dx + dy * dy) || 1;
                const shrink = NODE_RADIUS + 2;
                const x1 = a.x + (dx / dist) * shrink;
                const y1 = a.y + (dy / dist) * shrink;
                const x2 = b.x - (dx / dist) * shrink;
                const y2 = b.y - (dy / dist) * shrink;
                return (
                  <g
                    key={i}
                    onMouseEnter={() => setHoveredEdge(i)}
                    onMouseLeave={() => setHoveredEdge(null)}
                  >
                    <line
                      x1={x1}
                      y1={y1}
                      x2={x2}
                      y2={y2}
                      stroke={
                        active
                          ? "var(--accent, #612a92)"
                          : "var(--border-dark, #cfd1d2)"
                      }
                      strokeWidth={active ? 2 : 1.25}
                      markerEnd="url(#graph-arrow)"
                      opacity={active ? 1 : 0.65}
                    />
                    {/* Invisible wide stroke for easier hover pickup. */}
                    <line
                      x1={x1}
                      y1={y1}
                      x2={x2}
                      y2={y2}
                      stroke="transparent"
                      strokeWidth={12}
                      style={{ cursor: "default" }}
                    />
                    {hoveredEdge === i && (
                      <g>
                        <rect
                          x={(x1 + x2) / 2 - 60}
                          y={(y1 + y2) / 2 - 12}
                          width={120}
                          height={20}
                          rx={4}
                          fill="var(--text)"
                          opacity={0.9}
                        />
                        <text
                          x={(x1 + x2) / 2}
                          y={(y1 + y2) / 2 + 2}
                          fontSize={10}
                          fill="#fff"
                          textAnchor="middle"
                          dominantBaseline="middle"
                        >
                          {e.occurrenceCount}× ·{" "}
                          {e.label ? e.label.slice(0, 12) : "mention"}
                        </text>
                      </g>
                    )}
                  </g>
                );
              })}

              {/* Nodes. */}
              {simResult.nodes.map((n) => {
                const s = styleFor(n.kind);
                const active = hoveredNode === n.id;
                return (
                  <g
                    key={n.id}
                    transform={`translate(${n.x},${n.y})`}
                    style={{ cursor: "pointer" }}
                    onMouseEnter={() => setHoveredNode(n.id)}
                    onMouseLeave={() => setHoveredNode(null)}
                    onClick={() => handleNodeClick(n)}
                  >
                    <circle
                      r={NODE_RADIUS}
                      fill={s.fill}
                      stroke={s.stroke}
                      strokeWidth={active ? 3 : 2}
                      filter={
                        active
                          ? "drop-shadow(0 4px 12px rgba(0,0,0,0.12))"
                          : undefined
                      }
                    />
                    <text
                      y={-2}
                      textAnchor="middle"
                      dominantBaseline="middle"
                      fontSize={20}
                      pointerEvents="none"
                    >
                      {s.icon}
                    </text>
                    <text
                      y={NODE_RADIUS + 14}
                      textAnchor="middle"
                      dominantBaseline="middle"
                      fontSize={11}
                      fontWeight={active ? 600 : 500}
                      fill="var(--text)"
                      style={{ pointerEvents: "none" }}
                    >
                      {truncateLabel(n.title)}
                    </text>
                  </g>
                );
              })}
            </svg>

            <Legend counts={legendCounts} />
          </>
        )}
      </div>
    </div>
  );
}

// ── Supporting views ─────────────────────────────────────────────

function EmptyState() {
  return (
    <div
      style={{
        position: "absolute",
        inset: 0,
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "center",
        color: "var(--text-tertiary)",
        gap: 8,
        fontSize: 14,
        padding: "0 40px",
        textAlign: "center",
      }}
    >
      <div style={{ fontSize: 48, opacity: 0.7 }}>{"\u{1F578}"}</div>
      <div style={{ fontWeight: 600, color: "var(--text-secondary)" }}>
        No entities yet.
      </div>
      <div>
        Record facts about people, companies, or customers (via the MCP surface
        or
        <code style={{ padding: "0 4px" }}>POST /entity/fact</code>) and they'll
        appear here with every wikilink they mention.
      </div>
    </div>
  );
}

function Legend({ counts }: { counts: Map<string, number> }) {
  const entries = Array.from(counts.entries()).sort((a, b) =>
    a[0].localeCompare(b[0]),
  );
  if (entries.length === 0) return null;
  return (
    <div
      style={{
        position: "absolute",
        right: 16,
        bottom: 16,
        background: "var(--bg-card)",
        border: "1px solid var(--border)",
        borderRadius: 8,
        padding: "10px 12px",
        boxShadow: "0 4px 16px rgba(0,0,0,0.06)",
        fontSize: 12,
        display: "flex",
        flexDirection: "column",
        gap: 6,
        minWidth: 150,
      }}
    >
      <div
        style={{
          fontSize: 11,
          color: "var(--text-tertiary)",
          textTransform: "uppercase",
          letterSpacing: 0.5,
          fontWeight: 600,
        }}
      >
        Legend
      </div>
      {entries.map(([kind, count]) => {
        const s = styleFor(kind);
        return (
          <div
            key={kind}
            style={{ display: "flex", alignItems: "center", gap: 8 }}
          >
            <span
              style={{
                width: 16,
                height: 16,
                borderRadius: "50%",
                background: s.fill,
                border: `2px solid ${s.stroke}`,
                flexShrink: 0,
                display: "inline-flex",
                alignItems: "center",
                justifyContent: "center",
                fontSize: 10,
              }}
            >
              {s.icon}
            </span>
            <span style={{ flex: 1, color: "var(--text)" }}>{s.label}</span>
            <span
              style={{
                color: "var(--text-tertiary)",
                fontVariantNumeric: "tabular-nums",
              }}
            >
              {count}
            </span>
          </div>
        );
      })}
    </div>
  );
}

export default GraphApp;
