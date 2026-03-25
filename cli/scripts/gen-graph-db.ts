/**
 * Generate graph HTML directly from the local Postgres database.
 */
import { execFileSync } from "node:child_process";
import { writeFileSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { spawn } from "node:child_process";
import { generateGraphHtml } from "../src/lib/graph-html.js";
import type { GraphData, ContextEdge, InsightNode, EntityInsightSummary } from "../src/lib/graph-html.js";

const WORKSPACE_ID = process.argv[2] || "62190363569316105"; // wuphf-1 by default

function pgQuery(sql: string): string {
  return execFileSync("docker", [
    "exec", "wuphf-postgres",
    "psql", "-U", "postgres", "-d", "main", "-t", "-A", "-c", sql,
  ], { encoding: "utf-8", timeout: 15_000 }).trim();
}

// Single query to get all graph data
const raw = pgQuery(`
SELECT json_build_object(
  'nodes', (
    SELECT COALESCE(json_agg(json_build_object(
      'id', e.id::text,
      'name', COALESCE(
        (SELECT a.value->>'text' FROM attribute a WHERE a.entity_id = e.id AND a.slug = 'name' AND a.value->>'text' IS NOT NULL LIMIT 1),
        (SELECT TRIM(COALESCE(a.value->'full_name'->>'first_name', '') || ' ' || COALESCE(a.value->'full_name'->>'last_name', '')) FROM attribute a WHERE a.entity_id = e.id AND a.slug = 'name' AND a.value->'full_name' IS NOT NULL LIMIT 1),
        (SELECT a.value->>'first_name' FROM attribute a WHERE a.entity_id = e.id AND a.slug = 'name' AND a.value->>'first_name' IS NOT NULL LIMIT 1),
        'Unknown'
      ),
      'type', e.definition_slug,
      'definition_slug', e.definition_slug,
      'primary_attribute', COALESCE(
        (SELECT a.value->>'email' FROM attribute a WHERE a.entity_id = e.id AND a.slug = 'email_addresses' LIMIT 1),
        (SELECT a.value->>'domain' FROM attribute a WHERE a.entity_id = e.id AND a.slug = 'domains' LIMIT 1),
        ''
      ),
      'created_at', e.created_at
    )), '[]'::json)
    FROM entity e WHERE e.workspace_id = ${WORKSPACE_ID} AND e.archived_at IS NULL
  ),
  'edges', COALESCE((
    SELECT json_agg(json_build_object(
      'id', r.id::text,
      'source', r.entity_1_id::text,
      'target', r.entity_2_id::text,
      'label', rd.entity_1_to_2_predicate,
      'definition_id', r.definition_id::text
    ))
    FROM relationship r
    JOIN relationship_definition rd ON r.definition_id = rd.id
    WHERE r.workspace_id = ${WORKSPACE_ID}
  ), '[]'::json),
  'insights_raw', COALESCE((
    SELECT json_agg(json_build_object(
      'id', ei.id::text,
      'content', ei.content,
      'type', ei.type,
      'confidence', ei.confidence,
      'target_id', ei.target_id::text,
      'hint', ec.hint,
      'canonical_entity_id', ec.canonical_entity_id::text
    ))
    FROM entity_insight ei
    LEFT JOIN entity_context2 ec ON ei.target_id = ec.id
    WHERE ei.workspace_id = ${WORKSPACE_ID} AND ei.state = 'active'
  ), '[]'::json),
  'ghosts', COALESCE((
    SELECT json_agg(json_build_object(
      'id', 'ghost:' || ec.id::text,
      'name', COALESCE(ec.hint, 'Unknown'),
      'type', 'ghost',
      'definition_slug', 'ghost',
      'primary_attribute', '',
      'created_at', ec.created_at
    ))
    FROM entity_context2 ec
    WHERE ec.workspace_id = ${WORKSPACE_ID}
    AND ec.canonical_entity_id IS NULL
    AND ec.hint IS NOT NULL
    AND ec.hint != ''
  ), '[]'::json),
  'relationship_definitions', COALESCE((
    SELECT json_agg(json_build_object(
      'id', rd.id::text,
      'name', rd.entity_1_to_2_predicate || ' / ' || rd.entity_2_to_1_predicate,
      'entity_1_to_2', rd.entity_1_to_2_predicate,
      'entity_2_to_1', rd.entity_2_to_1_predicate
    ))
    FROM relationship_definition rd
    WHERE rd.workspace_id = ${WORKSPACE_ID}
  ), '[]'::json)
);
`);

const dbData = JSON.parse(raw);

// Merge ghost nodes into the node list
const allNodes = [...dbData.nodes, ...(dbData.ghosts ?? [])];

// Build entity ID set and name→id mapping for hint matching
const entityIds = new Set(allNodes.map((n: any) => n.id));
const nameToEntityId: Record<string, string> = {};
for (const n of allNodes) {
  nameToEntityId[n.name.toLowerCase()] = n.id;
  if (n.primary_attribute) {
    nameToEntityId[n.primary_attribute.toLowerCase()] = n.id;
  }
}

// Also map context IDs to ghost IDs for insight targeting
const contextIdToGhostId: Record<string, string> = {};
for (const g of dbData.ghosts ?? []) {
  // ghost id format: "ghost:contextId"
  const contextId = g.id.replace("ghost:", "");
  contextIdToGhostId[contextId] = g.id;
}

// Build insight nodes and context edges
const insightNodes: InsightNode[] = [];
const contextEdges: ContextEdge[] = [];
const insightsByEntity: Record<string, EntityInsightSummary[]> = {};

for (const ins of dbData.insights_raw ?? []) {
  // Resolve target: canonical_entity_id → hint match → ghost context ID
  let targetId = ins.canonical_entity_id ? String(ins.canonical_entity_id) : null;
  if (!targetId || !entityIds.has(targetId)) {
    const hint = (ins.hint ?? "").toLowerCase();
    targetId = nameToEntityId[hint] ?? null;
    // Partial match on hint
    if (!targetId && hint) {
      for (const [key, id] of Object.entries(nameToEntityId)) {
        if (hint.includes(key) || key.includes(hint)) {
          targetId = id;
          break;
        }
      }
    }
  }
  // Fallback: link to ghost entity via context ID
  if (!targetId || !entityIds.has(targetId)) {
    const ctxId = String(ins.target_id);
    targetId = contextIdToGhostId[ctxId] ?? null;
  }
  if (!targetId || !entityIds.has(targetId)) continue;

  // Panel insights
  if (!insightsByEntity[targetId]) insightsByEntity[targetId] = [];
  insightsByEntity[targetId].push({
    content: ins.content,
    confidence: ins.confidence,
    type: ins.type,
  });

  // Graph insight nodes
  const insightId = `ins:${ins.id}`;
  insightNodes.push({
    id: insightId,
    content: ins.content,
    type: ins.type,
    confidence: ins.confidence,
    source: "entity_insight",
  });
  contextEdges.push({
    id: `ce:${ins.id}`,
    source: insightId,
    target: targetId,
    label: ins.type,
    edge_type: "insight",
    confidence: ins.confidence,
  });
}

// Build triplet context edges — resolve context IDs to entity IDs
try {
  const tripletRaw = pgQuery(`
    SELECT COALESCE(json_agg(json_build_object(
      'id', t.id::text,
      'subject_entity', sc.canonical_entity_id::text,
      'object_entity', oc.canonical_entity_id::text,
      'predicate', t.predicate
    )), '[]'::json)
    FROM entity_insight_triplet t
    JOIN entity_insight ei ON t.entity_insight_id = ei.id
    LEFT JOIN entity_context2 sc ON t.subject_context_id = sc.id
    LEFT JOIN entity_context2 oc ON t.object_context_id = oc.id
    WHERE ei.workspace_id = ${WORKSPACE_ID} AND ei.state = 'active'
    AND sc.canonical_entity_id IS NOT NULL AND oc.canonical_entity_id IS NOT NULL;
  `);

  const triplets = tripletRaw ? JSON.parse(tripletRaw) ?? [] : [];
  const seenTrips = new Set<string>();
  for (const t of triplets) {
    const sid = String(t.subject_entity);
    const oid = String(t.object_entity);
    if (sid === oid) continue;
    const key = `${sid}-${oid}-${t.predicate}`;
    if (seenTrips.has(key)) continue;
    seenTrips.add(key);
    if (entityIds.has(sid) && entityIds.has(oid)) {
      contextEdges.push({
        id: `trip:${t.id}`,
        source: sid,
        target: oid,
        label: t.predicate,
        edge_type: "triplet",
      });
    }
  }
} catch { /* no triplets */ }

const graphData: GraphData = {
  nodes: allNodes,
  edges: dbData.edges,
  context_edges: contextEdges,
  insight_nodes: insightNodes,
  relationship_definitions: dbData.relationship_definitions,
  insights: insightsByEntity,
  total_nodes: allNodes.length,
  total_edges: dbData.edges.length,
  total_context_edges: contextEdges.length,
};

const html = generateGraphHtml(graphData);
const outPath = join(tmpdir(), `wuphf-graph-${Date.now()}.html`);
writeFileSync(outPath, html, "utf-8");

const totalInsights = insightNodes.length;
console.error(`Graph: ${graphData.total_nodes} entities, ${graphData.total_edges + contextEdges.length} connections, ${totalInsights} insights`);
console.error(`Saved to: ${outPath}`);

spawn("open", [`file://${outPath}`], { stdio: "ignore", detached: true }).unref();
