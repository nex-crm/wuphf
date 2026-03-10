/**
 * Self-contained HTML graph visualization using D3.js with SVG rendering.
 * Exact clone of Zep's graph approach, adapted for Nex entity/insight structure.
 * Generates a single HTML string that can be opened in any browser.
 */

export interface GraphNode {
  id: string;
  name: string;
  type: string;
  definition_slug: string;
  primary_attribute?: string;
  created_at?: string;
}

export interface GraphEdge {
  id: string;
  source: string;
  target: string;
  label: string;
  definition_id: string;
}

export interface ContextEdge {
  id: string;
  source: string;
  target: string;
  label: string;
  edge_type: "context" | "triplet" | "insight";
  confidence?: number;
}

export interface InsightNode {
  id: string;
  content: string;
  type: string;
  confidence: number;
  source: "entity_insight" | "knowledge_insight";
}

export interface EntityInsightSummary {
  content: string;
  confidence: number;
  type: string;
}

export interface RelationshipDefinition {
  id: string;
  name: string;
  entity_1_to_2: string;
  entity_2_to_1: string;
}

export interface GraphData {
  nodes: GraphNode[];
  edges: GraphEdge[];
  context_edges: ContextEdge[];
  insight_nodes: InsightNode[];
  relationship_definitions: RelationshipDefinition[];
  insights: Record<string, EntityInsightSummary[]>;
  total_nodes: number;
  total_edges: number;
  total_context_edges: number;
}

const NODE_COLORS: Record<string, string> = {
  person: "#EC4899",     // pink-500
  company: "#3B82F6",    // blue-500
  deal: "#F59E0B",       // amber-500
  lead: "#EF4444",       // red-500
  contact: "#06B6D4",    // cyan-500
  opportunity: "#F97316", // orange-500
  task: "#10B981",       // emerald-500
  note: "#84CC16",       // lime-500
  event: "#8B5CF6",      // violet-500
  topic: "#14B8A6",      // teal-500
  product: "#A855F7",    // purple-500
  project: "#6366F1",    // indigo-500
  ghost: "#8B5CF6",      // violet-500
  entity_insight: "#EC4899",  // pink-500
  knowledge_insight: "#06B6D4", // cyan-500
};
const DEFAULT_COLOR = "#EC4899";

function escapeHtml(str: string): string {
  return str
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

function escapeJsonForHtml(data: unknown): string {
  return JSON.stringify(data)
    .replace(/</g, "\\u003c")
    .replace(/>/g, "\\u003e")
    .replace(/&/g, "\\u0026");
}

export function generateGraphHtml(data: GraphData): string {
  const nodeColors = { ...NODE_COLORS };
  const typeSet = new Set<string>();
  for (const n of data.nodes) typeSet.add(n.type);
  for (const n of (data.insight_nodes ?? [])) typeSet.add(n.source);

  const legendTypes = Array.from(typeSet).sort();

  // Count total insights
  let totalInsights = 0;
  for (const key of Object.keys(data.insights ?? {})) {
    totalInsights += (data.insights[key] ?? []).length;
  }
  totalInsights += (data.insight_nodes ?? []).length;

  const totalConnections = data.total_edges + data.total_context_edges;

  return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Nex — Workspace Graph</title>
<style>
*{margin:0;padding:0;box-sizing:border-box}
body{background:#0f172a;color:#e2e8f0;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;overflow:hidden}
svg{width:100%;height:100vh;cursor:grab;border-radius:0;position:fixed;top:0;left:0;z-index:1}
svg:active{cursor:grabbing}
#search-box{position:fixed;top:16px;left:16px;z-index:10;background:rgba(30,41,59,0.9);border:1px solid #334155;border-radius:10px;padding:8px 14px;color:#e2e8f0;font-size:14px;width:240px;outline:none;backdrop-filter:blur(12px);-webkit-backdrop-filter:blur(12px)}
#search-box::placeholder{color:#64748b}
#search-box:focus{border-color:#EC4899;box-shadow:0 0 0 2px rgba(236,72,153,.2)}
#stats{position:fixed;top:16px;right:16px;z-index:10;background:rgba(30,41,59,0.9);border:1px solid #334155;border-radius:10px;padding:8px 14px;font-size:13px;color:#94a3b8;backdrop-filter:blur(12px);-webkit-backdrop-filter:blur(12px)}
#legend{position:fixed;bottom:16px;left:16px;z-index:10;background:rgba(30,41,59,0.9);border:1px solid #334155;border-radius:10px;padding:10px 14px;font-size:13px;max-height:50vh;overflow-y:auto;backdrop-filter:blur(12px);-webkit-backdrop-filter:blur(12px)}
.legend-item{display:flex;align-items:center;gap:8px;margin:4px 0;color:#94a3b8}
.legend-dot{width:10px;height:10px;border-radius:50%;flex-shrink:0}
.legend-line{width:20px;height:2px;flex-shrink:0;border-radius:1px}
.legend-sep{border-top:1px solid #334155;margin:6px 0}
#detail-panel{position:fixed;top:0;right:-380px;width:380px;height:100vh;background:rgba(30,41,59,0.95);border-left:1px solid #334155;z-index:20;padding:20px;overflow-y:auto;transition:right .25s cubic-bezier(.4,0,.2,1);backdrop-filter:blur(12px);-webkit-backdrop-filter:blur(12px)}
#detail-panel.open{right:0}
#detail-panel h3{margin-bottom:12px;font-size:16px;color:#f1f5f9;font-weight:600}
#detail-panel .field{margin-bottom:10px;font-size:13px}
#detail-panel .field .label{color:#64748b;font-size:11px;text-transform:uppercase;letter-spacing:.5px;font-weight:500}
#detail-panel .field .value{color:#cbd5e1;margin-top:2px;word-wrap:break-word;overflow-wrap:break-word;white-space:pre-wrap}
.insight-card{background:#1e293b;border:1px solid #334155;border-radius:8px;padding:10px 12px;margin-top:6px}
.insight-card .insight-type{display:inline-block;background:#334155;border-radius:4px;padding:2px 8px;font-size:10px;text-transform:uppercase;letter-spacing:.5px;color:#94a3b8;margin-right:6px;font-weight:500}
.insight-card .insight-confidence{font-size:11px;color:#64748b;float:right}
.insight-card .insight-content{margin-top:6px;font-size:12px;color:#94a3b8;line-height:1.5;word-wrap:break-word;overflow-wrap:break-word;white-space:pre-wrap}
#detail-close{position:absolute;top:12px;right:12px;background:none;border:none;color:#64748b;cursor:pointer;font-size:18px;transition:color .15s}
#detail-close:hover{color:#e2e8f0}
</style>
</head>
<body>
<input id="search-box" type="text" placeholder="Search entities..." autocomplete="off">
<div id="stats">${escapeHtml(String(data.total_nodes))} entities &middot; ${escapeHtml(String(totalConnections))} connections &middot; ${escapeHtml(String(totalInsights))} insights</div>
<div id="legend">
${legendTypes
  .map(
    (t) =>
      `<div class="legend-item"><span class="legend-dot" style="background:${escapeHtml(nodeColors[t] ?? DEFAULT_COLOR)}"></span>${escapeHtml(t)}</div>`
  )
  .join("\n")}
<div class="legend-sep"></div>
<div class="legend-item"><span class="legend-line" style="background:#475569"></span>Formal</div>
<div class="legend-item"><span class="legend-line" style="background:#8B5CF6"></span>Context</div>
<div class="legend-item"><span class="legend-line" style="background:#F59E0B"></span>Triplet</div>
<div class="legend-item"><span class="legend-line" style="background:#EC4899"></span>Insight</div>
</div>
<div id="detail-panel">
<button id="detail-close">&times;</button>
<h3 id="detail-name"></h3>
<div id="detail-body"></div>
</div>

<!-- D3.js v7 — same as Zep's force-graph approach -->
<script src="https://cdn.jsdelivr.net/npm/d3@7"></script>

<script id="graph-data" type="application/json">${escapeJsonForHtml(data)}</script>

<script>
(function(){
  var data = JSON.parse(document.getElementById("graph-data").textContent);
  var width = window.innerWidth;
  var height = window.innerHeight;

  var nodeColors = {
    person:"#EC4899", company:"#3B82F6", deal:"#F59E0B",
    lead:"#EF4444", contact:"#06B6D4", opportunity:"#F97316",
    task:"#10B981", note:"#84CC16", event:"#8B5CF6",
    topic:"#14B8A6", product:"#A855F7", project:"#6366F1",
    ghost:"#8B5CF6", entity_insight:"#EC4899", knowledge_insight:"#06B6D4"
  };
  var defaultColor = "#EC4899";
  var edgeTypeColors = {formal:"#475569", context:"#8B5CF6", triplet:"#F59E0B", insight:"#EC4899"};

  // ── Build entity ID set (entities + ghosts, NO insights yet) ──
  var entityIdSet = {};
  data.nodes.forEach(function(n){ entityIdSet[n.id] = true; });

  // ── Index insight nodes by target entity for on-demand expansion ──
  var insightsByEntity = {};
  (data.context_edges || []).forEach(function(e){
    if (e.edge_type === "insight") {
      var targetId = entityIdSet[e.target] ? e.target : (entityIdSet[e.source] ? e.source : null);
      var insightId = e.target === targetId ? e.source : e.target;
      if (targetId) {
        if (!insightsByEntity[targetId]) insightsByEntity[targetId] = [];
        var insData = null;
        (data.insight_nodes || []).forEach(function(ins){ if (ins.id === insightId) insData = ins; });
        if (insData) insightsByEntity[targetId].push({node: insData, edge: e});
      }
    }
  });

  // ── Count insights per entity ──
  var insightCount = {};
  // Use insightsByEntity as authoritative count (same data as insightsMap, avoid double-counting)
  for (var eid in insightsByEntity) insightCount[eid] = insightsByEntity[eid].length;
  var insightsMap = data.insights || {};
  // Only count from insightsMap for entities NOT already counted via insightsByEntity
  for (var eid2 in insightsMap) {
    if (!insightCount[eid2]) insightCount[eid2] = (insightsMap[eid2] || []).length;
  }

  // ── Build links from formal edges + non-insight context edges ──
  // Group links by source-target pair for curve offset (like Zep)
  var linkGroupMap = {};
  function addLink(source, target, label, edgeType) {
    if (!entityIdSet[source] || !entityIdSet[target]) return;
    var key = source < target ? source + "-" + target : target + "-" + source;
    if (!linkGroupMap[key]) linkGroupMap[key] = [];
    linkGroupMap[key].push({source: source, target: target, label: label, edgeType: edgeType, color: edgeTypeColors[edgeType] || "#475569"});
  }
  (data.edges || []).forEach(function(e){ addLink(e.source, e.target, e.label, "formal"); });
  (data.context_edges || []).forEach(function(e){
    if (e.edge_type === "insight") return;
    addLink(e.source, e.target, e.label, e.edge_type);
  });

  // Assign curve strengths per group (Zep's pattern)
  var baseLinks = [];
  for (var gk in linkGroupMap) {
    var group = linkGroupMap[gk];
    var count = group.length;
    var baseStrength = 0.2;
    group.forEach(function(lnk, idx){
      lnk.curveStrength = count > 1 ? (-baseStrength + idx * (baseStrength * 2) / (count - 1)) : 0;
      baseLinks.push(lnk);
    });
  }

  // ── Build degree map ──
  var degree = {};
  data.nodes.forEach(function(n){ degree[n.id] = 0; });
  baseLinks.forEach(function(l){
    degree[l.source] = (degree[l.source] || 0) + 1;
    degree[l.target] = (degree[l.target] || 0) + 1;
  });

  // ── Identify isolated & ghost nodes ──
  var linkedNodeIds = {};
  baseLinks.forEach(function(l){ linkedNodeIds[l.source] = true; linkedNodeIds[l.target] = true; });
  var isolatedNodeIds = {};
  data.nodes.forEach(function(n){ if (!linkedNodeIds[n.id]) isolatedNodeIds[n.id] = true; });

  // ── Build D3 node objects ──
  var entityNodes = data.nodes.map(function(n){
    return {
      id: n.id, name: n.name, type: n.type, nodeKind: "entity",
      primary_attribute: n.primary_attribute || "", created_at: n.created_at || "",
      color: nodeColors[n.type] || defaultColor,
      insightCount: insightCount[n.id] || 0
    };
  });

  // ── Track expanded insights ──
  var expandedEntities = {};

  // ── Current working nodes/links arrays ──
  var currentNodes = entityNodes.slice();
  var currentLinks = baseLinks.map(function(l){ return {source: l.source, target: l.target, label: l.label, edgeType: l.edgeType, color: l.color, curveStrength: l.curveStrength}; });

  function rebuildGraphArrays() {
    currentNodes = entityNodes.slice();
    currentLinks = baseLinks.map(function(l){ return {source: l.source, target: l.target, label: l.label, edgeType: l.edgeType, color: l.color, curveStrength: l.curveStrength}; });
    for (var entId in expandedEntities) {
      var items = insightsByEntity[entId] || [];
      var entNode = null;
      entityNodes.forEach(function(n){ if (n.id === entId) entNode = n; });
      items.forEach(function(item, i){
        var ins = item.node;
        var fullContent = ins.content || "";
        var label = fullContent.length > 50 ? fullContent.substring(0, 48) + ".." : fullContent;
        var angle = (2 * Math.PI * i) / items.length;
        var dist = 60;
        currentNodes.push({
          id: ins.id, name: label, fullContent: fullContent,
          type: ins.source, nodeKind: "insight",
          primary_attribute: ins.type, created_at: "",
          color: nodeColors[ins.source] || "#EC4899",
          insightCount: 0, confidence: ins.confidence,
          x: entNode ? entNode.x + Math.cos(angle) * dist : width/2,
          y: entNode ? entNode.y + Math.sin(angle) * dist : height/2
        });
        currentLinks.push({
          source: ins.id, target: entId, label: ins.type,
          edgeType: "insight", color: "#EC4899", curveStrength: 0
        });
      });
    }
  }

  // ── Focus/search/hover state ──
  var focusedNodeId = null;
  var focusedSet = {};
  var searchActive = false;
  var searchMatches = {};

  // ══════════════════════════════════════════════════════════
  //  SVG SETUP — exact Zep pattern: <svg> → <g> for zoom
  // ══════════════════════════════════════════════════════════
  var svg = d3.select("body").append("svg")
    .attr("width", width)
    .attr("height", height)
    .attr("viewBox", "0 0 " + width + " " + height)
    .style("background-color", "#0f172a");

  // Drop-shadow filter (Zep: drop-shadow(0 2px 4px rgba(0,0,0,0.2)))
  var defs = svg.append("defs");
  var filter = defs.append("filter").attr("id", "drop-shadow").attr("x", "-50%").attr("y", "-50%").attr("width", "200%").attr("height", "200%");
  filter.append("feDropShadow").attr("dx", 0).attr("dy", 2).attr("stdDeviation", 2).attr("flood-color", "rgba(0,0,0,0.3)");

  var g = svg.append("g");

  // ── Zoom behavior — Zep: scaleExtent [0.1, 4] ──
  var currentZoomScale = 0.8;
  var LABEL_ZOOM_THRESHOLD = 0.6;   // node labels appear above this
  var LINK_LABEL_ZOOM_THRESHOLD = 0.9; // link labels appear above this

  var zoomBehavior = d3.zoom()
    .scaleExtent([0.1, 4])
    .on("zoom", function(event){
      g.attr("transform", event.transform);
      var newScale = event.transform.k;
      // Toggle label visibility on scale change
      if ((newScale >= LABEL_ZOOM_THRESHOLD) !== (currentZoomScale >= LABEL_ZOOM_THRESHOLD)) {
        nodeLayer.selectAll("g.node-group > text:not(.insight-badge)")
          .attr("display", newScale >= LABEL_ZOOM_THRESHOLD ? null : "none");
      }
      if ((newScale >= LINK_LABEL_ZOOM_THRESHOLD) !== (currentZoomScale >= LINK_LABEL_ZOOM_THRESHOLD)) {
        linkLayer.selectAll(".link-label")
          .attr("display", newScale >= LINK_LABEL_ZOOM_THRESHOLD ? null : "none");
      }
      currentZoomScale = newScale;
    });
  svg.call(zoomBehavior).call(zoomBehavior.transform, d3.zoomIdentity.scale(0.8).translate(width * 0.1, height * 0.1));

  // Click on SVG background → unfocus
  svg.on("click", function(event){
    if (event.target.tagName === "svg" || event.target === svg.node()) {
      unfocus();
    }
  });

  // ── Layer groups (links below nodes) ──
  var linkLayer = g.append("g").attr("class", "links");
  var nodeLayer = g.append("g").attr("class", "nodes");

  // ══════════════════════════════════════════════════════════
  //  D3 FORCE SIMULATION — exact Zep parameters
  // ══════════════════════════════════════════════════════════
  var simulation = d3.forceSimulation(currentNodes)
    .force("link", d3.forceLink(currentLinks).id(function(d){ return d.id; }).distance(200).strength(0.2))
    .force("charge", d3.forceManyBody()
      .strength(function(d){ return isolatedNodeIds[d.id] ? -500 : -3000; })
      .distanceMin(20).distanceMax(500).theta(0.8))
    .force("center", d3.forceCenter(width / 2, height / 2).strength(0.05))
    .force("collide", d3.forceCollide().radius(50).strength(0.3).iterations(5))
    .force("isolatedGravity", d3.forceRadial(100, width / 2, height / 2)
      .strength(function(d){ return isolatedNodeIds[d.id] ? 0.15 : (d.type === "ghost" ? 0.03 : 0.01); }))
    .velocityDecay(0.4)
    .alphaDecay(0.05)
    .alphaMin(0.001);

  // ══════════════════════════════════════════════════════════
  //  RENDER FUNCTIONS
  // ══════════════════════════════════════════════════════════
  var linkSelection, nodeSelection;

  function render() {
    // ── LINKS ──
    linkLayer.selectAll("g.link-group").remove();
    linkSelection = linkLayer.selectAll("g.link-group")
      .data(currentLinks, function(d){ return d.source.id + "-" + d.target.id + "-" + d.label; })
      .join("g")
      .attr("class", "link-group");

    // Path for each link
    linkSelection.append("path")
      .attr("stroke", function(d){ return d.color; })
      .attr("stroke-opacity", 0.6)
      .attr("stroke-width", 1)
      .attr("fill", "none")
      .attr("cursor", "pointer");

    // Label group (rect + text) — Zep pattern
    var labelG = linkSelection.append("g")
      .attr("class", "link-label")
      .attr("cursor", "pointer")
      .attr("display", currentZoomScale >= LINK_LABEL_ZOOM_THRESHOLD ? null : "none");

    labelG.append("rect")
      .attr("fill", "#1e293b")
      .attr("rx", 4).attr("ry", 4)
      .attr("opacity", 0.9);

    labelG.append("text")
      .attr("fill", "#94a3b8")
      .attr("font-size", "8px")
      .attr("text-anchor", "middle")
      .attr("dominant-baseline", "middle")
      .attr("pointer-events", "none")
      .text(function(d){ return d.label || ""; });

    // ── NODES ──
    nodeLayer.selectAll("g.node-group").remove();
    nodeSelection = nodeLayer.selectAll("g.node-group")
      .data(currentNodes, function(d){ return d.id; })
      .join("g")
      .attr("class", "node-group")
      .attr("cursor", "pointer")
      .call(dragBehavior);

    // Circle — Zep: r=10, drop-shadow, stroke-width 1
    nodeSelection.append("circle")
      .attr("r", function(d){
        if (d.nodeKind === "insight") return 6;
        if (d.type === "ghost") return 7;
        return 10;
      })
      .attr("fill", function(d){ return d.color; })
      .attr("stroke", "#e2e8f0")
      .attr("stroke-width", function(d){ return d.type === "ghost" ? 1 : 1; })
      .attr("filter", "url(#drop-shadow)")
      .attr("stroke-dasharray", function(d){ return d.type === "ghost" ? "2,2" : null; });

    // Insight count badge (white text inside node)
    nodeSelection.filter(function(d){ return d.nodeKind !== "insight" && d.insightCount > 0; })
      .append("text")
      .attr("class", "insight-badge")
      .attr("text-anchor", "middle")
      .attr("dominant-baseline", "central")
      .attr("font-size", "8px")
      .attr("font-weight", "700")
      .attr("fill", "#fff")
      .attr("pointer-events", "none")
      .text(function(d){ return d.insightCount; });

    // Label — Zep: x=15, font-size 12px, font-weight 500
    // Hidden until zoom >= LABEL_ZOOM_THRESHOLD
    nodeSelection.append("text")
      .attr("x", 15)
      .attr("y", "0.3em")
      .attr("text-anchor", "start")
      .attr("fill", "#e2e8f0")
      .attr("font-weight", "500")
      .attr("font-size", "12px")
      .attr("pointer-events", "none")
      .attr("display", currentZoomScale >= LABEL_ZOOM_THRESHOLD ? null : "none")
      .text(function(d){
        var name = d.name || d.id;
        return name.length > 28 ? name.substring(0, 26) + ".." : name;
      });

    // Click handler
    nodeSelection.on("click", function(event, d){
      event.stopPropagation();
      handleNodeClick(d);
    });

    // Hover handlers
    nodeSelection.on("mouseenter", function(event, d){
      d3.select(this).select("circle")
        .attr("stroke", "#60a5fa")
        .attr("stroke-width", 3);
    }).on("mouseleave", function(event, d){
      d3.select(this).select("circle")
        .attr("stroke", "#e2e8f0")
        .attr("stroke-width", 1);
      applyVisualState();
    });
  }

  // ── Drag behavior — exact Zep pattern ──
  var dragBehavior = d3.drag()
    .on("start", function(event){
      if (!event.active) simulation.velocityDecay(0.7).alphaDecay(0.1).alphaTarget(0.1).restart();
      event.subject.fx = event.subject.x;
      event.subject.fy = event.subject.y;
      d3.select(this).select("circle").attr("stroke", "#60a5fa").attr("stroke-width", 3);
    })
    .on("drag", function(event){
      event.subject.fx = event.x;
      event.subject.fy = event.y;
    })
    .on("end", function(event){
      if (!event.active) simulation.velocityDecay(0.4).alphaDecay(0.05).alphaTarget(0);
      event.subject.fx = event.x;
      event.subject.fy = event.y;
      d3.select(this).select("circle").attr("stroke", "#e2e8f0").attr("stroke-width", 1);
    });

  // ══════════════════════════════════════════════════════════
  //  TICK — update positions (Zep: quadratic Bézier paths)
  // ══════════════════════════════════════════════════════════
  simulation.on("tick", function(){
    if (!linkSelection || !nodeSelection) return;

    linkSelection.each(function(d){
      var el = d3.select(this);
      var path = el.select("path");
      var labelGroup = el.select(".link-label");

      var sx = d.source.x, sy = d.source.y, tx = d.target.x, ty = d.target.y;
      if (sx == null || tx == null) return;

      // Self-referencing loop
      if (d.source.id === d.target.id) {
        var rx = 40, ry = 90, offset = ry + 20;
        var cx = sx, cy = sy - offset;
        path.attr("d", "M" + sx + "," + sy + " C" + (cx-rx) + "," + cy + " " + (cx+rx) + "," + cy + " " + sx + "," + sy);
        labelGroup.attr("transform", "translate(" + cx + "," + (cy-10) + ")");
      } else {
        // Quadratic Bézier — Zep's curve calculation
        var dx = tx - sx, dy = ty - sy;
        var dr = Math.sqrt(dx*dx + dy*dy) || 1;
        var midX = (sx + tx) / 2, midY = (sy + ty) / 2;
        var normalX = -dy / dr, normalY = dx / dr;
        var curveMag = dr * (d.curveStrength || 0);
        var controlX = midX + normalX * curveMag;
        var controlY = midY + normalY * curveMag;
        path.attr("d", "M" + sx + "," + sy + " Q" + controlX + "," + controlY + " " + tx + "," + ty);

        // Position label at midpoint of Bézier — Zep's getPointAtLength pattern
        var pathNode = path.node();
        if (pathNode && pathNode.getTotalLength) {
          var pathLen = pathNode.getTotalLength();
          var midPt = pathNode.getPointAtLength(pathLen / 2);
          if (midPt) {
            var angle = Math.atan2(ty - sy, tx - sx) * 180 / Math.PI;
            var rot = (angle > 90 || angle < -90) ? angle - 180 : angle;
            labelGroup.attr("transform", "translate(" + midPt.x + "," + midPt.y + ") rotate(" + rot + ")");
          }
        }
      }

      // Size the label background rect
      var text = labelGroup.select("text");
      var rect = labelGroup.select("rect");
      var textNode = text.node();
      if (textNode) {
        var bbox = textNode.getBBox();
        if (bbox.width > 0) {
          rect.attr("x", -bbox.width/2 - 6).attr("y", -bbox.height/2 - 4)
            .attr("width", bbox.width + 12).attr("height", bbox.height + 8);
        }
      }
    });

    // Update node positions — Zep: translate(x,y)
    nodeSelection.attr("transform", function(d){ return "translate(" + d.x + "," + d.y + ")"; });
  });

  // ── Initial render ──
  render();

  // ── Zoom-to-fit after simulation settles ──
  var hasZoomed = false;
  simulation.on("end", function(){
    if (hasZoomed) return;
    hasZoomed = true;
    zoomToFit();
  });
  // Fallback zoom-to-fit
  setTimeout(function(){ if (!hasZoomed) { hasZoomed = true; zoomToFit(); } }, 2000);

  function zoomToFit(duration) {
    duration = duration || 750;
    var bounds = g.node().getBBox();
    if (!bounds || bounds.width === 0) return;
    var fullW = width, fullH = height;
    var midX = bounds.x + bounds.width / 2;
    var midY = bounds.y + bounds.height / 2;
    var scale = 0.65 * Math.min(fullW / bounds.width, fullH / bounds.height);
    if (!isFinite(scale) || !isFinite(midX)) return;
    var transform = d3.zoomIdentity
      .translate(fullW/2 - midX*scale, fullH/2 - midY*scale)
      .scale(scale);
    svg.transition().duration(duration).ease(d3.easeCubicInOut).call(zoomBehavior.transform, transform);
  }

  // ══════════════════════════════════════════════════════════
  //  VISUAL STATE — focus/search dimming
  // ══════════════════════════════════════════════════════════
  function applyVisualState() {
    if (!nodeSelection || !linkSelection) return;

    nodeSelection.select("circle")
      .attr("fill", function(d){
        if (focusedNodeId && !focusedSet[d.id]) return "#1e293b";
        if (searchActive && !searchMatches[d.id]) return "#1e293b";
        return d.color;
      })
      .attr("stroke", function(d){
        if (focusedNodeId && focusedSet[d.id]) return "#60a5fa";
        return "#e2e8f0";
      })
      .attr("stroke-width", function(d){
        if (focusedNodeId && d.id === focusedNodeId) return 3;
        if (focusedNodeId && focusedSet[d.id]) return 2;
        return 1;
      })
      .attr("opacity", function(d){
        if (focusedNodeId && !focusedSet[d.id]) return 0.15;
        if (searchActive && !searchMatches[d.id]) return 0.15;
        return 1;
      });

    nodeSelection.selectAll("text")
      .attr("opacity", function(d){
        if (focusedNodeId && !focusedSet[d.id]) return 0.1;
        if (searchActive && !searchMatches[d.id]) return 0.1;
        return 1;
      });

    linkSelection.select("path")
      .attr("stroke", function(d){
        var sid = typeof d.source === "object" ? d.source.id : d.source;
        var tid = typeof d.target === "object" ? d.target.id : d.target;
        if (focusedNodeId) {
          if (focusedSet[sid] && focusedSet[tid]) return d.color;
          return "#0f172a";
        }
        return d.color;
      })
      .attr("stroke-opacity", function(d){
        var sid = typeof d.source === "object" ? d.source.id : d.source;
        var tid = typeof d.target === "object" ? d.target.id : d.target;
        if (focusedNodeId && !(focusedSet[sid] && focusedSet[tid])) return 0.05;
        if (searchActive && !(searchMatches[sid] && searchMatches[tid])) return 0.05;
        return 0.6;
      })
      .attr("stroke-width", function(d){
        var sid = typeof d.source === "object" ? d.source.id : d.source;
        var tid = typeof d.target === "object" ? d.target.id : d.target;
        if (focusedNodeId && focusedSet[sid] && focusedSet[tid]) return 2;
        return 1;
      });

    linkSelection.select(".link-label")
      .attr("opacity", function(d){
        var sid = typeof d.source === "object" ? d.source.id : d.source;
        var tid = typeof d.target === "object" ? d.target.id : d.target;
        if (focusedNodeId && !(focusedSet[sid] && focusedSet[tid])) return 0;
        if (searchActive && !(searchMatches[sid] && searchMatches[tid])) return 0;
        return 1;
      });
  }

  // ══════════════════════════════════════════════════════════
  //  NODE CLICK — focus, expand insights, zoom to neighborhood
  // ══════════════════════════════════════════════════════════
  var panel = document.getElementById("detail-panel");
  var detailName = document.getElementById("detail-name");
  var detailBody = document.getElementById("detail-body");
  document.getElementById("detail-close").addEventListener("click", function(){ unfocus(); });

  function unfocus() {
    panel.classList.remove("open");
    expandedEntities = {};
    focusedNodeId = null;
    focusedSet = {};
    searchActive = false;
    searchMatches = {};
    rebuildAndRestart();
    applyVisualState();
  }

  function handleNodeClick(d) {
    // Toggle focus
    if (focusedNodeId === d.id) {
      unfocus();
      return;
    }

    focusedNodeId = d.id;
    focusedSet = {};
    focusedSet[d.id] = true;

    // Add 1st-degree neighbors
    currentLinks.forEach(function(l){
      var sid = typeof l.source === "object" ? l.source.id : l.source;
      var tid = typeof l.target === "object" ? l.target.id : l.target;
      if (sid === d.id) focusedSet[tid] = true;
      if (tid === d.id) focusedSet[sid] = true;
    });

    if (d.nodeKind === "insight") {
      showInsightDetail(d);
    } else {
      // Toggle insight expansion
      expandedEntities = {};
      if (insightsByEntity[d.id] && insightsByEntity[d.id].length > 0) {
        expandedEntities[d.id] = true;
      }
      showEntityDetail(d);
      rebuildAndRestart();
      // After rebuild, include new insight nodes in focusedSet
      currentNodes.forEach(function(n){
        if (n.nodeKind === "insight") focusedSet[n.id] = true;
      });
    }

    applyVisualState();
    zoomToNeighborhood(d);
  }

  function rebuildAndRestart() {
    rebuildGraphArrays();
    simulation.nodes(currentNodes);
    simulation.force("link", d3.forceLink(currentLinks).id(function(d){ return d.id; }).distance(200).strength(0.2));
    simulation.alpha(0.3).restart();
    render();
  }

  // ── Zoom to neighborhood — exact Zep pattern ──
  function zoomToNeighborhood(d) {
    var connectedNodes = [d];
    currentNodes.forEach(function(n){
      if (n.id !== d.id && focusedSet[n.id] && n.x != null) connectedNodes.push(n);
    });
    if (connectedNodes.length < 1) return;

    var padding = 80;
    var minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
    connectedNodes.forEach(function(n){
      if (n.x < minX) minX = n.x;
      if (n.y < minY) minY = n.y;
      if (n.x > maxX) maxX = n.x;
      if (n.y > maxY) maxY = n.y;
    });
    minX -= padding; minY -= padding; maxX += padding; maxY += padding;
    var bw = maxX - minX, bh = maxY - minY;
    var scale = 0.9 * Math.min(width / bw, height / bh);
    var midX = (minX + maxX) / 2, midY = (minY + maxY) / 2;
    if (!isFinite(scale) || !isFinite(midX)) return;
    var transform = d3.zoomIdentity
      .translate(width/2 - midX*scale, height/2 - midY*scale)
      .scale(scale);
    svg.transition().duration(750).ease(d3.easeCubicInOut).call(zoomBehavior.transform, transform);
  }

  // ══════════════════════════════════════════════════════════
  //  DETAIL PANEL
  // ══════════════════════════════════════════════════════════
  function addField(parent, labelText, valueText) {
    var field = document.createElement("div"); field.className = "field";
    var lbl = document.createElement("div"); lbl.className = "label"; lbl.textContent = labelText;
    var val = document.createElement("div"); val.className = "value"; val.textContent = valueText;
    field.appendChild(lbl); field.appendChild(val); parent.appendChild(field);
  }

  function showInsightDetail(d) {
    detailName.textContent = d.primary_attribute || "Insight";
    while (detailBody.firstChild) detailBody.removeChild(detailBody.firstChild);
    addField(detailBody, "Kind", "Insight (" + d.type + ")");
    addField(detailBody, "Type", d.primary_attribute || "");
    if (d.confidence != null) addField(detailBody, "Confidence", Math.round(d.confidence * 100) + "%");
    addField(detailBody, "Content", d.fullContent || d.name);
    panel.classList.add("open");
  }

  function showEntityDetail(d) {
    detailName.textContent = d.name || d.id;
    while (detailBody.firstChild) detailBody.removeChild(detailBody.firstChild);
    addField(detailBody, "Type", d.type || "");
    if (d.primary_attribute) addField(detailBody, "Primary", d.primary_attribute);
    if (d.created_at) addField(detailBody, "Created", d.created_at);

    // Connections
    var connCount = 0;
    var connectedList = [];
    var nodeMap = {};
    entityNodes.forEach(function(n){ nodeMap[n.id] = n; });
    baseLinks.forEach(function(l){
      if (l.source === d.id || (typeof l.source === "object" && l.source.id === d.id)) { connCount++; connectedList.push(typeof l.target === "object" ? l.target.id : l.target); }
      if (l.target === d.id || (typeof l.target === "object" && l.target.id === d.id)) { connCount++; connectedList.push(typeof l.source === "object" ? l.source.id : l.source); }
    });
    addField(detailBody, "Connections", String(connCount));

    if (connectedList.length > 0) {
      var field = document.createElement("div"); field.className = "field";
      var lbl = document.createElement("div"); lbl.className = "label"; lbl.textContent = "Connected to";
      field.appendChild(lbl);
      var val = document.createElement("div"); val.className = "value";
      connectedList.slice(0, 20).forEach(function(cid){
        var line = document.createElement("div");
        var cn = nodeMap[cid];
        line.textContent = cn ? cn.name : cid;
        val.appendChild(line);
      });
      if (connectedList.length > 20) {
        var more = document.createElement("em");
        more.textContent = "...and " + (connectedList.length - 20) + " more";
        val.appendChild(more);
      }
      field.appendChild(val); detailBody.appendChild(field);
    }

    // Insights in panel
    var panelInsights = insightsMap[d.id] || [];
    if (panelInsights.length > 0) {
      var insightField = document.createElement("div"); insightField.className = "field";
      var insightLbl = document.createElement("div"); insightLbl.className = "label";
      insightLbl.textContent = "Insights (" + panelInsights.length + ")";
      insightField.appendChild(insightLbl);
      panelInsights.forEach(function(ins){
        var card = document.createElement("div"); card.className = "insight-card";
        var badge = document.createElement("span"); badge.className = "insight-type"; badge.textContent = ins.type;
        card.appendChild(badge);
        var conf = document.createElement("span"); conf.className = "insight-confidence";
        conf.textContent = Math.round(ins.confidence * 100) + "%";
        card.appendChild(conf);
        var content = document.createElement("div"); content.className = "insight-content"; content.textContent = ins.content;
        card.appendChild(content);
        insightField.appendChild(card);
      });
      detailBody.appendChild(insightField);
    }

    panel.classList.add("open");
  }

  // ══════════════════════════════════════════════════════════
  //  SEARCH — highlight + zoom
  // ══════════════════════════════════════════════════════════
  var searchBox = document.getElementById("search-box");
  var searchResultsEl = document.createElement("div");
  searchResultsEl.id = "search-results";
  searchResultsEl.style.cssText = "position:fixed;top:52px;left:16px;z-index:10;font-size:12px;color:#64748b;padding:0 4px;display:none";
  document.body.appendChild(searchResultsEl);

  var searchTimer = null;
  searchBox.addEventListener("input", function(){
    if (searchTimer) clearTimeout(searchTimer);
    searchTimer = setTimeout(function(){
      var q = searchBox.value.trim().toLowerCase();
      searchMatches = {};
      searchActive = false;
      focusedNodeId = null;
      focusedSet = {};
      searchResultsEl.style.display = "none";
      if (q) {
        searchActive = true;
        var directMatches = [];
        currentNodes.forEach(function(n){
          var name = (n.name || n.id || "").toLowerCase();
          var type = (n.type || "").toLowerCase();
          var primary = (n.primary_attribute || "").toLowerCase();
          if (name.indexOf(q) >= 0 || type.indexOf(q) >= 0 || primary.indexOf(q) >= 0) {
            searchMatches[n.id] = true;
            directMatches.push(n);
          }
        });
        // 1st-degree neighbors
        currentLinks.forEach(function(l){
          var sid = typeof l.source === "object" ? l.source.id : l.source;
          var tid = typeof l.target === "object" ? l.target.id : l.target;
          if (searchMatches[sid]) searchMatches[tid] = true;
          if (searchMatches[tid]) searchMatches[sid] = true;
        });
        searchResultsEl.textContent = directMatches.length + " result" + (directMatches.length !== 1 ? "s" : "");
        searchResultsEl.style.display = "block";
        // Zoom to matches
        if (directMatches.length === 1 && directMatches[0].x != null) {
          var t = d3.zoomIdentity.translate(width/2 - directMatches[0].x*2, height/2 - directMatches[0].y*2).scale(2);
          svg.transition().duration(400).call(zoomBehavior.transform, t);
        } else if (directMatches.length > 1) {
          var minX = Infinity, maxX = -Infinity, minY = Infinity, maxY = -Infinity;
          directMatches.forEach(function(n){
            if (n.x != null) { if (n.x < minX) minX = n.x; if (n.x > maxX) maxX = n.x; if (n.y < minY) minY = n.y; if (n.y > maxY) maxY = n.y; }
          });
          var cx = (minX+maxX)/2, cy = (minY+maxY)/2;
          var bw = maxX-minX+200, bh = maxY-minY+200;
          var z = Math.min(width/bw, height/bh, 4);
          var t2 = d3.zoomIdentity.translate(width/2-cx*z, height/2-cy*z).scale(Math.max(z, 0.3));
          svg.transition().duration(400).call(zoomBehavior.transform, t2);
        }
      }
      applyVisualState();
    }, 150);
  });

  // ── Handle window resize ──
  window.addEventListener("resize", function(){
    width = window.innerWidth;
    height = window.innerHeight;
    svg.attr("width", width).attr("height", height).attr("viewBox", "0 0 " + width + " " + height);
  });
})();
</script>
</body>
</html>`;
}
