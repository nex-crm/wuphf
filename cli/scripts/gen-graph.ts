/**
 * Temporary script to generate graph HTML from workspace data via CLI commands.
 * Used while /v1/graph endpoint is not deployed yet.
 */
import { execFileSync } from "node:child_process";
import { writeFileSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { spawn } from "node:child_process";
import { generateGraphHtml } from "../src/lib/graph-html.js";
import type { GraphData, GraphNode, GraphEdge, ContextEdge, InsightNode, EntityInsightSummary } from "../src/lib/graph-html.js";

const cwd = join(import.meta.dirname, "..");

function cli(args: string[]): string {
  return execFileSync("npx", ["tsx", "src/index.ts", ...args, "--format", "json"], {
    cwd,
    encoding: "utf-8",
    timeout: 30_000,
  });
}

function getName(attrs: Record<string, any>): string {
  const n = attrs?.name;
  if (!n) return "Unknown";
  if (n.type === "full_name") {
    return [n.first_name, n.last_name].filter(Boolean).join(" ");
  }
  return n.text ?? "Unknown";
}

function getPrimary(attrs: Record<string, any>, type: string): string {
  if (type === "person") {
    return attrs?.email_addresses?.[0]?.email ?? attrs?.job_title?.text ?? "";
  }
  if (type === "company") {
    return attrs?.domains?.[0]?.domain ?? "";
  }
  return "";
}

// 1. Fetch all object types
const objectsRes = JSON.parse(cli(["object", "list"]));
const objectDefs: Record<string, { id: string; slug: string; type: string }> = {};
for (const obj of objectsRes.data) {
  objectDefs[obj.id] = obj;
}

// 2. Fetch all records per object type
const allNodes: GraphNode[] = [];
const recordMap: Record<string, { type: string; name: string }> = {};

for (const obj of objectsRes.data) {
  const res = JSON.parse(cli(["record", "list", obj.slug, "--limit", "200", "--attributes", "all"]));
  for (const rec of res.data) {
    const name = getName(rec.attributes);
    const primary = getPrimary(rec.attributes, obj.type);
    allNodes.push({
      id: rec.id,
      name,
      type: obj.type,
      definition_slug: obj.slug,
      primary_attribute: primary,
      created_at: rec.created_at,
    });
    recordMap[rec.id] = { type: obj.type, name };
  }
}

// 3. Fetch relationship definitions
const relDefsRes = JSON.parse(cli(["rel", "list-defs"]));
const relDefs = relDefsRes.data;

// Build object ID → slug map
const objIdToSlug: Record<string, string> = {};
for (const obj of objectsRes.data) {
  objIdToSlug[obj.id] = obj.slug;
}

// 4. Build edges — connect Najmuzzaman to WUPHF (known from data)
const allEdges: GraphEdge[] = [];
const seenEdges = new Set<string>();

const knownRelationships = [
  {
    source: "62869717286693892", // Najmuzzaman
    target: "62869717437688836", // WUPHF
    label: "works at",
    defId: relDefs.find((d: any) => d.entity_1_to_2_predicate === "employs")?.id ?? "",
  },
];

for (const rel of knownRelationships) {
  const key = `${rel.source}-${rel.target}`;
  if (!seenEdges.has(key)) {
    seenEdges.add(key);
    allEdges.push({
      id: `rel:${key}`,
      source: rel.source,
      target: rel.target,
      label: rel.label,
      definition_id: rel.defId,
    });
  }
}

// 5. Fetch insights
const insightsRes = JSON.parse(cli(["insight", "list"]));
const insightsByEntity: Record<string, EntityInsightSummary[]> = {};
const contextEdges: ContextEdge[] = [];
const insightNodes: InsightNode[] = [];

// Map insight targets to record IDs using domain/name hints
const hintToRecordId: Record<string, string> = {};
for (const node of allNodes) {
  const nameLower = node.name.toLowerCase();
  hintToRecordId[nameLower] = node.id;
  if (node.primary_attribute) {
    hintToRecordId[node.primary_attribute.toLowerCase()] = node.id;
  }
}

for (const insight of insightsRes.insights ?? []) {
  const hint = (insight.target?.hint ?? "").toLowerCase();
  let entityId = hintToRecordId[hint];

  // Try matching by signal values
  if (!entityId && insight.target?.signals) {
    for (const sig of insight.target.signals) {
      const val = (sig.value ?? "").toLowerCase();
      if (hintToRecordId[val]) {
        entityId = hintToRecordId[val];
        break;
      }
    }
  }

  if (entityId) {
    if (!insightsByEntity[entityId]) insightsByEntity[entityId] = [];
    insightsByEntity[entityId].push({
      content: insight.content,
      confidence: insight.confidence,
      type: insight.type,
    });

    // Create insight nodes + context edges
    const insightId = `ins:${insightNodes.length}`;
    insightNodes.push({
      id: insightId,
      content: insight.content,
      type: insight.type,
      confidence: insight.confidence,
      source: "entity_insight",
    });
    contextEdges.push({
      id: `ce:${contextEdges.length}`,
      source: insightId,
      target: entityId,
      label: insight.type,
      edge_type: "insight",
      confidence: insight.confidence,
    });
  }
}

// 6. Create context edges between companies sharing "software" industry
const companyIndustries: Record<string, string[]> = {};
for (const node of allNodes) {
  if (node.type === "company") {
    try {
      const recRes = JSON.parse(cli(["record", "get", node.id]));
      const industries = recRes.attributes?.industries;
      if (industries) {
        companyIndustries[node.id] = industries.map((i: any) => i.option_id);
      }
    } catch { /* skip */ }
  }
}

const softwareCompanies = Object.entries(companyIndustries)
  .filter(([_, industries]) => industries.includes("software"))
  .map(([id]) => id);

for (let i = 0; i < softwareCompanies.length; i++) {
  for (let j = i + 1; j < softwareCompanies.length; j++) {
    const key = `${softwareCompanies[i]}-${softwareCompanies[j]}`;
    if (!seenEdges.has(key)) {
      seenEdges.add(key);
      contextEdges.push({
        id: `ctx:${key}`,
        source: softwareCompanies[i],
        target: softwareCompanies[j],
        label: "same industry",
        edge_type: "context",
      });
    }
  }
}

// 7. Assemble graph data
const graphData: GraphData = {
  nodes: allNodes,
  edges: allEdges,
  context_edges: contextEdges,
  insight_nodes: insightNodes,
  relationship_definitions: relDefs.map((d: any) => ({
    id: d.id,
    name: `${objIdToSlug[d.entity_definition_1_id] ?? "?"} → ${objIdToSlug[d.entity_definition_2_id] ?? "?"}`,
    entity_1_to_2: d.entity_1_to_2_predicate,
    entity_2_to_1: d.entity_2_to_1_predicate,
  })),
  insights: insightsByEntity,
  total_nodes: allNodes.length,
  total_edges: allEdges.length,
  total_context_edges: contextEdges.length,
};

// 8. Generate HTML and open
const html = generateGraphHtml(graphData);
const outPath = join(tmpdir(), `wuphf-graph-${Date.now()}.html`);
writeFileSync(outPath, html, "utf-8");

console.error(`Graph: ${allNodes.length} entities, ${allEdges.length + contextEdges.length} connections, ${insightNodes.length} insights`);
console.error(`Saved to: ${outPath}`);

// Open in browser
spawn("open", [`file://${outPath}`], { stdio: "ignore", detached: true }).unref();
