/**
 * Entity-brief API client — thin wrapper over `client.ts` covering the v1.2
 * entity-brief surface: facts log + brief synthesis.
 *
 * Unlike `api/notebook.ts` this module has no mock mode — errors propagate so
 * empty-state / error-state UI surfaces real backend problems instead of
 * hiding them (same pattern v1.1 reviews adopted).
 *
 * SSE: the shared broker stream at `/events` emits named events. We use
 * `addEventListener` on the two entity event names so we do not pay the cost
 * of parsing every broker message (office changes, wiki writes, notebook
 * writes, etc.) in every article view.
 */

import { get, post, sseURL } from "./client";

// ── Types ────────────────────────────────────────────────────────

export type EntityKind = "people" | "companies" | "customers";

/** Schema-aligned kind enum per docs/specs/WIKI-SCHEMA.md §4.1. */
export type SchemaKind =
  | "person"
  | "company"
  | "project"
  | "team"
  | "workspace";

/** Bridge helper — maps the legacy plural (people/companies/customers) to
 *  the schema singular (person/company). customers → company per §4.1 (no
 *  separate "customer" kind at the schema level; customers are companies
 *  with a sales-relationship signal). */
export function toSchemaKind(k: EntityKind): SchemaKind {
  switch (k) {
    case "people":
      return "person";
    case "companies":
      return "company";
    case "customers":
      return "company";
  }
}

/** Bridge helper — maps a schema singular back to the nearest legacy plural.
 *  project, team, and workspace have no legacy v1.2 mapping and throw. */
export function fromSchemaKind(k: SchemaKind): EntityKind {
  switch (k) {
    case "person":
      return "people";
    case "company":
      return "companies";
    case "project":
    case "team":
    case "workspace":
      throw new Error(`Schema kind "${k}" has no legacy v1.2 mapping`);
  }
}

/**
 * Triplet is the subject/predicate/object shape from docs/specs/WIKI-SCHEMA.md §4.2.
 * `object` is a slug, a literal, or `{kind}:{slug}` when the object is itself
 * an entity reference.
 */
export interface Triplet {
  subject: string;
  predicate: string;
  object: string;
}

/**
 * FactType is the §4.3 enum. Legacy rows without a type parse as `undefined`;
 * the UI treats that the same as `"observation"` (the §4.3 default).
 */
export type FactType = "status" | "observation" | "relationship" | "background";

export interface Fact {
  id: string;
  kind: EntityKind;
  slug: string;
  text: string;
  source_path?: string;
  recorded_by: string;
  created_at: string;
  // Typed fields from docs/specs/WIKI-SCHEMA.md §4.2. All optional so legacy
  // v1.2 rows parse with zero values and render without these fields.
  type?: FactType;
  triplet?: Triplet;
  confidence?: number;
  valid_from?: string;
  valid_until?: string | null;
  supersedes?: string[];
  contradicts_with?: string[];
  source_type?: string;
  sentence_offset?: number;
  reinforced_at?: string | null;
}

export interface BriefSummary {
  kind: EntityKind;
  slug: string;
  title: string;
  fact_count: number;
  last_synthesized_ts: string;
  last_synthesized_sha: string;
  pending_delta: number;
}

export interface RecordFactRequest {
  entity_kind: EntityKind;
  entity_slug: string;
  fact: string;
  source_path?: string;
  recorded_by?: string;
}

export interface RecordFactResponse {
  fact_id: string;
  fact_count: number;
  threshold_crossed: boolean;
}

export interface SynthesizeRequest {
  entity_kind: EntityKind;
  entity_slug: string;
  actor_slug?: string;
}

export interface SynthesizeResponse {
  synthesis_id: string;
  queued_at: string;
}

export interface FactRecordedEvent {
  kind: EntityKind;
  slug: string;
  fact_id: string;
  recorded_by: string;
  fact_count: number;
  threshold_crossed: boolean;
  timestamp: string;
}

export interface BriefSynthesizedEvent {
  kind: EntityKind;
  slug: string;
  commit_sha: string;
  fact_count: number;
  synthesized_ts: string;
}

export type GraphDirection = "out" | "in" | "both";

export interface GraphEdge {
  from_kind: EntityKind;
  from_slug: string;
  to_kind: EntityKind;
  to_slug: string;
  first_seen_fact_id: string;
  last_seen_ts: string;
  occurrence_count: number;
}

export interface GraphQueryResponse {
  kind: EntityKind;
  slug: string;
  direction: GraphDirection;
  edges: GraphEdge[];
}

export interface GraphNode {
  kind: EntityKind;
  slug: string;
  title: string;
}

export interface GraphAllResponse {
  nodes: GraphNode[];
  edges: GraphEdge[];
}

// ── HTTP ─────────────────────────────────────────────────────────

/** `GET /entity/facts?kind=&slug=` — newest-first. */
export async function fetchFacts(
  kind: EntityKind,
  slug: string,
): Promise<Fact[]> {
  const res = await get<{ facts: Fact[] }>(
    `/entity/facts?kind=${encodeURIComponent(kind)}&slug=${encodeURIComponent(slug)}`,
  );
  return Array.isArray(res?.facts) ? res.facts : [];
}

/** `GET /entity/briefs` — returns every brief's status row. */
export async function fetchBriefs(): Promise<BriefSummary[]> {
  // Broker wraps the array in `{ briefs: [...] }`. Unwrap here so callers
  // can stay array-oriented. Tolerate both shapes so a broker that changes
  // the envelope doesn't blank the UI.
  const res = await get<{ briefs?: BriefSummary[] } | BriefSummary[]>(
    "/entity/briefs",
  );
  if (Array.isArray(res)) return res;
  return Array.isArray(res?.briefs) ? res.briefs : [];
}

/** `POST /entity/fact`. Not currently called from the UI (MCP-only in v1.2)
 *  but exported so tests + future wiring can reach it. */
export function recordFact(
  req: RecordFactRequest,
): Promise<RecordFactResponse> {
  return post<RecordFactResponse>("/entity/fact", req);
}

/** `POST /entity/brief/synthesize`. Returns 503 if the worker is not attached. */
export function requestBriefSynthesis(
  req: SynthesizeRequest,
): Promise<SynthesizeResponse> {
  return post<SynthesizeResponse>("/entity/brief/synthesize", req);
}

/**
 * `GET /entity/graph?kind=&slug=&direction=` — returns coalesced edges
 * touching the given entity. `direction` defaults to `'out'` (who this
 * entity mentions).
 */
export async function fetchEntityGraph(
  kind: EntityKind,
  slug: string,
  direction: GraphDirection = "out",
): Promise<GraphEdge[]> {
  const q = new URLSearchParams({ kind, slug, direction });
  const res = await get<GraphQueryResponse | { edges?: GraphEdge[] }>(
    `/entity/graph?${q.toString()}`,
  );
  if (!res) return [];
  if (Array.isArray((res as GraphQueryResponse).edges)) {
    return (res as GraphQueryResponse).edges;
  }
  const maybe = (res as { edges?: GraphEdge[] }).edges;
  return Array.isArray(maybe) ? maybe : [];
}

/**
 * `GET /entity/graph/all` — returns the full cross-entity graph: every brief
 * + every coalesced edge, in one payload. Used by the Graph app view.
 */
export async function fetchEntityGraphAll(): Promise<GraphAllResponse> {
  const res = await get<GraphAllResponse>("/entity/graph/all");
  return {
    nodes: Array.isArray(res?.nodes) ? res.nodes : [],
    edges: Array.isArray(res?.edges) ? res.edges : [],
  };
}

// ── SSE ──────────────────────────────────────────────────────────

/**
 * Subscribe to the shared broker `/events` SSE stream filtered to one
 * specific entity (kind + slug). Returns an unsubscribe function that
 * tears down the underlying EventSource.
 *
 * Each caller opens its own EventSource — EntityBriefBar + FactsOnFile on
 * the same page will hold two connections. That matches how the rest of
 * the app consumes broker events today (useAgentStream, subscribeEditLog)
 * and avoids a shared singleton that would race between articles.
 *
 * Failure is silent — if EventSource is undefined (tests, non-SSE envs)
 * the unsubscribe is still a valid no-op.
 */
export function subscribeEntityEvents(
  kind: EntityKind,
  slug: string,
  onFact: (ev: FactRecordedEvent) => void,
  onSynth: (ev: BriefSynthesizedEvent) => void,
): () => void {
  let closed = false;
  let source: EventSource | null = null;

  const factHandler = (ev: MessageEvent) => {
    if (closed) return;
    try {
      const data = JSON.parse(ev.data) as FactRecordedEvent;
      if (data && data.kind === kind && data.slug === slug) {
        onFact(data);
      }
    } catch {
      // ignore malformed events
    }
  };
  const synthHandler = (ev: MessageEvent) => {
    if (closed) return;
    try {
      const data = JSON.parse(ev.data) as BriefSynthesizedEvent;
      if (data && data.kind === kind && data.slug === slug) {
        onSynth(data);
      }
    } catch {
      // ignore malformed events
    }
  };

  try {
    // EventSource may be undefined in tests that stub SSE away.
    const ES = (globalThis as { EventSource?: typeof EventSource }).EventSource;
    if (!ES) return () => {};
    source = new ES(sseURL("/events"));
    source.addEventListener(
      "entity:fact_recorded",
      factHandler as EventListener,
    );
    source.addEventListener(
      "entity:brief_synthesized",
      synthHandler as EventListener,
    );
    source.onerror = () => {
      // Keep the source open — EventSource auto-reconnects. Closing here
      // would drop live fact updates after the first transient blip.
    };
  } catch {
    source = null;
  }

  return () => {
    closed = true;
    if (source) {
      source.removeEventListener(
        "entity:fact_recorded",
        factHandler as EventListener,
      );
      source.removeEventListener(
        "entity:brief_synthesized",
        synthHandler as EventListener,
      );
      source.close();
      source = null;
    }
  };
}
