/**
 * Self-contained HTML graph visualization using force-graph (vasturiano).
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
  edge_type: "context" | "triplet";
  confidence?: number;
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
  relationship_definitions: RelationshipDefinition[];
  insights: Record<string, EntityInsightSummary[]>;
  total_nodes: number;
  total_edges: number;
  total_context_edges: number;
}

const NODE_COLORS: Record<string, string> = {
  person: "#4F87E0",
  company: "#34A853",
  deal: "#F4A236",
};
const DEFAULT_COLOR = "#9B59B6";

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

  const legendTypes = Array.from(typeSet).sort();

  // Count total insights
  let totalInsights = 0;
  for (const key of Object.keys(data.insights ?? {})) {
    totalInsights += (data.insights[key] ?? []).length;
  }

  const totalConnections = data.total_edges + data.total_context_edges;

  return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Nex — Workspace Graph</title>
<style>
*{margin:0;padding:0;box-sizing:border-box}
body{background:#1a1a2e;color:#e0e0e0;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;overflow:hidden}
#graph-container{width:100vw;height:100vh}
#search-box{position:fixed;top:16px;left:16px;z-index:10;background:#25253e;border:1px solid #3a3a5c;border-radius:8px;padding:8px 12px;color:#e0e0e0;font-size:14px;width:240px;outline:none}
#search-box::placeholder{color:#6a6a8a}
#search-box:focus{border-color:#4F87E0}
#stats{position:fixed;top:16px;right:16px;z-index:10;background:#25253e;border:1px solid #3a3a5c;border-radius:8px;padding:8px 14px;font-size:13px;color:#9a9ab0}
#legend{position:fixed;bottom:16px;left:16px;z-index:10;background:#25253e;border:1px solid #3a3a5c;border-radius:8px;padding:10px 14px;font-size:13px}
.legend-item{display:flex;align-items:center;gap:8px;margin:4px 0}
.legend-dot{width:10px;height:10px;border-radius:50%;flex-shrink:0}
.legend-line{width:20px;height:2px;flex-shrink:0;border-radius:1px}
#detail-panel{position:fixed;top:0;right:-320px;width:320px;height:100vh;background:#25253e;border-left:1px solid #3a3a5c;z-index:20;padding:20px;overflow-y:auto;transition:right .2s ease}
#detail-panel.open{right:0}
#detail-panel h3{margin-bottom:12px;font-size:16px;color:#fff}
#detail-panel .field{margin-bottom:8px;font-size:13px}
#detail-panel .field .label{color:#6a6a8a;font-size:11px;text-transform:uppercase;letter-spacing:.5px}
#detail-panel .field .value{color:#e0e0e0;margin-top:2px}
.insight-card{background:#2a2a45;border-radius:6px;padding:8px 10px;margin-top:6px}
.insight-card .insight-type{display:inline-block;background:#3a3a5c;border-radius:3px;padding:1px 6px;font-size:10px;text-transform:uppercase;letter-spacing:.5px;color:#9a9ab0;margin-right:6px}
.insight-card .insight-confidence{font-size:11px;color:#6a6a8a;float:right}
.insight-card .insight-content{margin-top:4px;font-size:12px;color:#c0c0d0;line-height:1.4}
#detail-close{position:absolute;top:12px;right:12px;background:none;border:none;color:#6a6a8a;cursor:pointer;font-size:18px}
#detail-close:hover{color:#e0e0e0}
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
<div class="legend-item"><span class="legend-line" style="background:#555"></span>Formal</div>
<div class="legend-item"><span class="legend-line" style="background:#7c5cbf"></span>Context</div>
<div class="legend-item"><span class="legend-line" style="background:#c4813a"></span>Triplet</div>
</div>
<div id="detail-panel">
<button id="detail-close">&times;</button>
<h3 id="detail-name"></h3>
<div id="detail-body"></div>
</div>
<div id="graph-container"></div>

<script src="https://cdn.jsdelivr.net/npm/force-graph"></script>

<script id="graph-data" type="application/json">${escapeJsonForHtml(data)}</script>

<script>
(function(){
  var data = JSON.parse(document.getElementById("graph-data").textContent);

  var nodeColors = {person:"#4F87E0",company:"#34A853",deal:"#F4A236"};
  var defaultColor = "#9B59B6";

  // Build node ID set for filtering dangling edges
  var nodeIdSet = {};
  data.nodes.forEach(function(n){ nodeIdSet[n.id] = true; });

  // Merge edges + context_edges into links
  var links = [];
  (data.edges || []).forEach(function(e){
    if (nodeIdSet[e.source] && nodeIdSet[e.target]) {
      links.push({source:e.source, target:e.target, label:e.label, color:"#555", edgeType:"formal"});
    }
  });
  (data.context_edges || []).forEach(function(e){
    if (nodeIdSet[e.source] && nodeIdSet[e.target]) {
      var c = e.edge_type === "triplet" ? "#c4813a" : "#7c5cbf";
      links.push({source:e.source, target:e.target, label:e.label, color:c, edgeType:e.edge_type});
    }
  });

  // Build degree map across all link types
  var degree = {};
  data.nodes.forEach(function(n){ degree[n.id] = 0; });
  links.forEach(function(l){
    degree[l.source] = (degree[l.source] || 0) + 1;
    degree[l.target] = (degree[l.target] || 0) + 1;
  });
  var maxDeg = 1;
  for (var k in degree) { if (degree[k] > maxDeg) maxDeg = degree[k]; }

  // Build force-graph nodes
  var fgNodes = data.nodes.map(function(n){
    return {
      id: n.id,
      name: n.name,
      type: n.type,
      primary_attribute: n.primary_attribute || "",
      created_at: n.created_at || "",
      color: nodeColors[n.type] || defaultColor,
      size: 2 + ((degree[n.id] || 0) / maxDeg) * 8
    };
  });

  // Hover highlight state
  var hoveredNode = null;
  var connectedNodes = {};

  function highlightNode(node) {
    if (node) {
      hoveredNode = node;
      connectedNodes = {};
      connectedNodes[node.id] = true;
      links.forEach(function(l){
        var sid = typeof l.source === "object" ? l.source.id : l.source;
        var tid = typeof l.target === "object" ? l.target.id : l.target;
        if (sid === node.id) connectedNodes[tid] = true;
        if (tid === node.id) connectedNodes[sid] = true;
      });
    } else {
      hoveredNode = null;
      connectedNodes = {};
    }
    graph.nodeColor(graph.nodeColor());
  }

  // Detail panel
  var panel = document.getElementById("detail-panel");
  var detailName = document.getElementById("detail-name");
  var detailBody = document.getElementById("detail-body");

  document.getElementById("detail-close").addEventListener("click", function(){
    panel.classList.remove("open");
  });

  function closeDetail() {
    panel.classList.remove("open");
  }

  function addField(parent, labelText, valueText) {
    var field = document.createElement("div");
    field.className = "field";
    var lbl = document.createElement("div");
    lbl.className = "label";
    lbl.textContent = labelText;
    var val = document.createElement("div");
    val.className = "value";
    val.textContent = valueText;
    field.appendChild(lbl);
    field.appendChild(val);
    parent.appendChild(field);
  }

  function showDetail(node) {
    detailName.textContent = node.name || node.id;
    while (detailBody.firstChild) detailBody.removeChild(detailBody.firstChild);
    addField(detailBody, "Type", node.type || "");
    if (node.primary_attribute) addField(detailBody, "Primary", node.primary_attribute);
    if (node.created_at) addField(detailBody, "Created", node.created_at);

    // Count connections
    var connCount = 0;
    var connectedList = [];
    var nodeMap = {};
    fgNodes.forEach(function(n){ nodeMap[n.id] = n; });
    links.forEach(function(l){
      var sid = typeof l.source === "object" ? l.source.id : l.source;
      var tid = typeof l.target === "object" ? l.target.id : l.target;
      if (sid === node.id) { connCount++; connectedList.push(tid); }
      if (tid === node.id) { connCount++; connectedList.push(sid); }
    });
    addField(detailBody, "Connections", String(connCount));

    // Connected entities
    if (connectedList.length > 0) {
      var field = document.createElement("div");
      field.className = "field";
      var lbl = document.createElement("div");
      lbl.className = "label";
      lbl.textContent = "Connected to";
      field.appendChild(lbl);
      var val = document.createElement("div");
      val.className = "value";
      var shown = connectedList.slice(0, 20);
      shown.forEach(function(cid){
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
      field.appendChild(val);
      detailBody.appendChild(field);
    }

    // Insights section
    var insights = data.insights && data.insights[node.id];
    if (insights && insights.length > 0) {
      var insightField = document.createElement("div");
      insightField.className = "field";
      var insightLbl = document.createElement("div");
      insightLbl.className = "label";
      insightLbl.textContent = "Insights";
      insightField.appendChild(insightLbl);
      insights.forEach(function(ins){
        var card = document.createElement("div");
        card.className = "insight-card";
        var badge = document.createElement("span");
        badge.className = "insight-type";
        badge.textContent = ins.type;
        card.appendChild(badge);
        var conf = document.createElement("span");
        conf.className = "insight-confidence";
        conf.textContent = Math.round(ins.confidence * 100) + "%";
        card.appendChild(conf);
        var content = document.createElement("div");
        content.className = "insight-content";
        content.textContent = ins.content;
        card.appendChild(content);
        insightField.appendChild(card);
      });
      detailBody.appendChild(insightField);
    }

    panel.classList.add("open");
  }

  // Init force-graph
  var graph = ForceGraph()(document.getElementById("graph-container"))
    .graphData({nodes: fgNodes, links: links})
    .nodeLabel("name")
    .nodeColor(function(node){
      if (hoveredNode && !connectedNodes[node.id]) return "#2a2a45";
      return node.color;
    })
    .nodeVal("size")
    .linkColor(function(link){
      if (hoveredNode) {
        var sid = typeof link.source === "object" ? link.source.id : link.source;
        var tid = typeof link.target === "object" ? link.target.id : link.target;
        if (connectedNodes[sid] && connectedNodes[tid]) return link.color;
        return "#1f1f35";
      }
      return link.color;
    })
    .linkLabel("label")
    .linkDirectionalArrowLength(4)
    .linkDirectionalArrowRelPos(1)
    .linkWidth(function(link){
      if (hoveredNode) {
        var sid = typeof link.source === "object" ? link.source.id : link.source;
        var tid = typeof link.target === "object" ? link.target.id : link.target;
        if (connectedNodes[sid] && connectedNodes[tid]) return 2;
        return 0.3;
      }
      return 1;
    })
    .backgroundColor("#1a1a2e")
    .onNodeClick(function(node){ showDetail(node); })
    .onNodeHover(function(node){ highlightNode(node); })
    .onBackgroundClick(function(){ closeDetail(); });

  // Search
  var searchBox = document.getElementById("search-box");
  searchBox.addEventListener("input", function(){
    var q = searchBox.value.trim().toLowerCase();
    if (!q) {
      graph.nodeColor(function(node){
        if (hoveredNode && !connectedNodes[node.id]) return "#2a2a45";
        return node.color;
      });
      return;
    }
    var matches = fgNodes.filter(function(n){
      return n.name.toLowerCase().indexOf(q) >= 0;
    });
    if (matches.length > 0) {
      graph.centerAt(matches[0].x, matches[0].y, 300).zoom(3, 300);
    }
  });
})();
</script>
</body>
</html>`;
}
