/**
 * Self-contained HTML graph visualization using Sigma.js v3 + Graphology.
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

export interface RelationshipDefinition {
  id: string;
  name: string;
  entity_1_to_2: string;
  entity_2_to_1: string;
}

export interface GraphData {
  nodes: GraphNode[];
  edges: GraphEdge[];
  relationship_definitions: RelationshipDefinition[];
  total_nodes: number;
  total_edges: number;
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
  for (const n of data.nodes) {
    typeSet.add(n.type);
  }

  // Build degree map
  const degree: Record<string, number> = {};
  for (const n of data.nodes) degree[n.id] = 0;
  for (const e of data.edges) {
    degree[e.source] = (degree[e.source] ?? 0) + 1;
    degree[e.target] = (degree[e.target] ?? 0) + 1;
  }
  const maxDeg = Math.max(1, ...Object.values(degree));

  // Build safe node data for embedding
  const safeNodes = data.nodes.map((n) => ({
    id: n.id,
    name: escapeHtml(n.name),
    rawName: n.name,
    type: n.type,
    primary_attribute: n.primary_attribute ? escapeHtml(n.primary_attribute) : undefined,
    color: nodeColors[n.type] ?? DEFAULT_COLOR,
    size: 5 + ((degree[n.id] ?? 0) / maxDeg) * 15,
    created_at: n.created_at,
  }));

  const safeEdges = data.edges.map((e) => ({
    id: e.id,
    source: e.source,
    target: e.target,
    label: escapeHtml(e.label),
    definition_id: e.definition_id,
  }));

  // Build legend from actual types
  const legendTypes = Array.from(typeSet).sort();

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
#detail-panel{position:fixed;top:0;right:-320px;width:320px;height:100vh;background:#25253e;border-left:1px solid #3a3a5c;z-index:20;padding:20px;overflow-y:auto;transition:right .2s ease}
#detail-panel.open{right:0}
#detail-panel h3{margin-bottom:12px;font-size:16px;color:#fff}
#detail-panel .field{margin-bottom:8px;font-size:13px}
#detail-panel .field .label{color:#6a6a8a;font-size:11px;text-transform:uppercase;letter-spacing:.5px}
#detail-panel .field .value{color:#e0e0e0;margin-top:2px}
#detail-close{position:absolute;top:12px;right:12px;background:none;border:none;color:#6a6a8a;cursor:pointer;font-size:18px}
#detail-close:hover{color:#e0e0e0}
</style>
</head>
<body>
<input id="search-box" type="text" placeholder="Search entities..." autocomplete="off">
<div id="stats">${escapeHtml(String(data.total_nodes))} entities &middot; ${escapeHtml(String(data.total_edges))} relationships</div>
<div id="legend">
${legendTypes
  .map(
    (t) =>
      `<div class="legend-item"><span class="legend-dot" style="background:${escapeHtml(nodeColors[t] ?? DEFAULT_COLOR)}"></span>${escapeHtml(t)}</div>`
  )
  .join("\n")}
</div>
<div id="detail-panel">
<button id="detail-close">&times;</button>
<h3 id="detail-name"></h3>
<div id="detail-body"></div>
</div>
<div id="graph-container"></div>

<script src="https://unpkg.com/graphology@0.25.4/dist/graphology.umd.min.js"></script>
<script src="https://unpkg.com/graphology-layout-forceatlas2@0.10.1/dist/graphology-layout-forceatlas2.min.js"></script>
<script src="https://unpkg.com/sigma@3.0.0-beta.9/build/sigma.min.js"></script>

<script id="graph-data" type="application/json">${escapeJsonForHtml({ nodes: safeNodes, edges: safeEdges })}</script>

<script>
(function(){
  var raw = JSON.parse(document.getElementById("graph-data").textContent);
  var graph = new graphology.Graph({multi:true});

  raw.nodes.forEach(function(n){
    graph.addNode(n.id,{
      label:n.name,
      x:Math.random()*100,
      y:Math.random()*100,
      size:n.size,
      color:n.color,
      type:n.type,
      rawName:n.rawName,
      primary_attribute:n.primary_attribute,
      created_at:n.created_at
    });
  });

  raw.edges.forEach(function(e){
    if(graph.hasNode(e.source)&&graph.hasNode(e.target)){
      graph.addEdge(e.source,e.target,{label:e.label,size:1,color:"#3a3a5c"});
    }
  });

  // ForceAtlas2 layout
  var fa2=graphologyLayoutForceAtlas2;
  var settings=fa2.inferSettings(graph);
  settings.gravity=1;
  settings.scalingRatio=10;
  fa2.assign(graph,{settings:settings,iterations:150});

  var container=document.getElementById("graph-container");
  var renderer=new Sigma(graph,container,{
    renderEdgeLabels:true,
    labelColor:{color:"#c0c0d0"},
    labelSize:12,
    labelRenderedSizeThreshold:8,
    edgeLabelColor:{color:"#6a6a8a"},
    edgeLabelSize:10,
    defaultEdgeType:"line",
    stagePadding:40
  });

  // Hover: highlight neighbors
  var hoveredNode=null;
  var hoveredNeighbors=new Set();

  renderer.on("enterNode",function(e){
    hoveredNode=e.node;
    hoveredNeighbors=new Set(graph.neighbors(e.node));
    renderer.refresh();
  });
  renderer.on("leaveNode",function(){
    hoveredNode=null;
    hoveredNeighbors.clear();
    renderer.refresh();
  });

  renderer.setSetting("nodeReducer",function(node,data){
    var res=Object.assign({},data);
    if(hoveredNode){
      if(node===hoveredNode){
        res.highlighted=true;
      }else if(!hoveredNeighbors.has(node)){
        res.color="#2a2a3e";
        res.label=null;
      }
    }
    if(searchQuery){
      var name=(graph.getNodeAttribute(node,"rawName")||"").toLowerCase();
      if(!name.includes(searchQuery)){
        res.color="#2a2a3e";
        res.label=null;
      }
    }
    return res;
  });

  renderer.setSetting("edgeReducer",function(edge,data){
    var res=Object.assign({},data);
    if(hoveredNode){
      var src=graph.source(edge);
      var tgt=graph.target(edge);
      if(src!==hoveredNode&&tgt!==hoveredNode){
        res.hidden=true;
      }else{
        res.color="#6a6a8a";
        res.size=2;
      }
    }
    return res;
  });

  // Search
  var searchQuery="";
  var searchBox=document.getElementById("search-box");
  searchBox.addEventListener("input",function(){
    searchQuery=searchBox.value.trim().toLowerCase();
    if(searchQuery){
      graph.forEachNode(function(node,attrs){
        if((attrs.rawName||"").toLowerCase().includes(searchQuery)){
          var pos=renderer.getNodeDisplayData(node);
          if(pos)renderer.getCamera().animate({x:pos.x,y:pos.y,ratio:0.3},{duration:300});
          return true;
        }
      });
    }
    renderer.refresh();
  });

  // Detail panel — uses textContent and DOM methods to avoid innerHTML XSS
  var panel=document.getElementById("detail-panel");
  var detailName=document.getElementById("detail-name");
  var detailBody=document.getElementById("detail-body");
  document.getElementById("detail-close").addEventListener("click",function(){
    panel.classList.remove("open");
  });

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

  renderer.on("clickNode",function(e){
    var attrs=graph.getNodeAttributes(e.node);
    var neighbors=graph.neighbors(e.node);
    detailName.textContent=attrs.rawName||attrs.label||e.node;
    detailBody.textContent="";
    addField(detailBody, "Type", attrs.type||"");
    if(attrs.primary_attribute) addField(detailBody, "Primary", attrs.primary_attribute);
    if(attrs.created_at) addField(detailBody, "Created", attrs.created_at);
    addField(detailBody, "Connections", String(neighbors.length));
    if(neighbors.length>0){
      var field=document.createElement("div");
      field.className="field";
      var lbl=document.createElement("div");
      lbl.className="label";
      lbl.textContent="Connected to";
      field.appendChild(lbl);
      var val=document.createElement("div");
      val.className="value";
      neighbors.slice(0,20).forEach(function(n){
        var line=document.createElement("div");
        line.textContent=graph.getNodeAttribute(n,"rawName")||n;
        val.appendChild(line);
      });
      if(neighbors.length>20){
        var more=document.createElement("em");
        more.textContent="...and "+(neighbors.length-20)+" more";
        val.appendChild(more);
      }
      field.appendChild(val);
      detailBody.appendChild(field);
    }
    panel.classList.add("open");
  });

  renderer.on("clickStage",function(){
    panel.classList.remove("open");
  });
})();
</script>
</body>
</html>`;
}
