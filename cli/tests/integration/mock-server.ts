/**
 * Mock HTTP server for nex-as-a-skill integration tests.
 * Same data shapes as nex-cli tests for cross-validation.
 */

import { createServer, type IncomingMessage, type ServerResponse } from "node:http";

// ── Mock data ───────────────────────────────────────────────────────────

const OBJECTS = [
  {
    id: "obj-1", name: "Company", name_plural: "Companies", slug: "company",
    type: "company", created_at: "2025-01-01T00:00:00Z",
    attributes: [
      { id: "ad-1", name: "Name", slug: "name", type: "text" },
      { id: "ad-2", name: "Domains", slug: "domains", type: "domain" },
    ],
  },
  {
    id: "obj-2", name: "Person", name_plural: "People", slug: "person",
    type: "person", created_at: "2025-01-01T00:00:00Z",
    attributes: [{ id: "ad-3", name: "Name", slug: "name", type: "text" }],
  },
];

const COMPANY_RECORDS = [
  {
    id: "rec-1", object_id: "obj-1", workspace_id: "ws-1", type: "company",
    attributes: {
      name: [{ id: "a1", type: "text", text: "Acme Corp" }],
      domains: [{ id: "a2", type: "domain", domain: "acme.com" }],
    },
    created_at: "2025-01-01T00:00:00Z", updated_at: "2025-06-15T12:00:00Z",
  },
  {
    id: "rec-2", object_id: "obj-1", workspace_id: "ws-1", type: "company",
    attributes: { name: [{ id: "a3", type: "text", text: "Globex" }] },
    created_at: "2025-01-01T00:00:00Z", updated_at: "2025-06-15T12:00:00Z",
  },
];

const PERSON_RECORDS = [
  {
    id: "rec-3", object_id: "obj-2", workspace_id: "ws-1", type: "person",
    attributes: { name: [{ id: "a4", type: "text", text: "John Doe" }] },
    created_at: "2025-01-01T00:00:00Z", updated_at: "2025-06-15T12:00:00Z",
  },
];

const RECORDS_BY_SLUG: Record<string, typeof COMPANY_RECORDS> = {
  company: COMPANY_RECORDS,
  person: PERSON_RECORDS,
};

const ALL_RECORDS = [...COMPANY_RECORDS, ...PERSON_RECORDS];
const RECORDS_BY_ID: Record<string, typeof ALL_RECORDS[0]> = {};
for (const r of ALL_RECORDS) RECORDS_BY_ID[r.id] = r;

const TASK = {
  id: "task-1", title: "Fix bug", description: "Fix the login bug",
  status: "todo", priority: "high", due_date: null, is_completed: false,
  record_id: "", record_name: "", assignee_id: "",
  created_at: "2025-01-01T00:00:00Z", updated_at: "2025-01-01T00:00:00Z",
};

const NOTE = {
  id: "note-1", title: "Project note", body: "A note about the project",
  record_id: "", author_id: "user-1",
  created_at: "2025-01-01T00:00:00Z", updated_at: "2025-01-01T00:00:00Z",
};

const CONTEXT_TEXT = { status: "completed", artifact_id: "art-1" };

const CONTEXT_ASK = {
  answer: "The answer is 42", sources: [], record_ids: [],
  entityCount: 1, sessionId: "sess-1",
};

const ARTIFACT = {
  id: "art-1", content: "Previously remembered context",
  status: "completed", created_at: "2025-01-01T00:00:00Z",
};

const SEARCH = {
  results: [{
    id: "rec-1", name: "Acme Corp", primary_value: "Acme Corp",
    matched_value: "Acme Corp", score: 0.95, type: "company",
    entity_definition_id: "obj-1",
  }],
};

const TIMELINE = {
  events: [{
    id: "evt-1", type: "created", summary: "Record created", details: {},
    actor_id: "user-1", actor_name: "System",
    created_at: "2025-01-01T00:00:00Z",
  }],
};

const GRAPH = {
  nodes: [
    { id: "rec-1", name: "Acme Corp", type: "company", definition_slug: "company", primary_attribute: "Acme Corp", created_at: "2025-01-01T00:00:00Z" },
    { id: "rec-3", name: "John Doe", type: "person", definition_slug: "person", primary_attribute: "John Doe", created_at: "2025-01-01T00:00:00Z" },
  ],
  edges: [{ source: "rec-1", target: "rec-3", relationship_id: "rel-inst-1", relationship_name: "employs", predicate: "employs" }],
  context_edges: [],
  relationship_definitions: [{ id: "rel-1", type: "one_to_many", predicate: "employs" }],
  total_nodes: 2, total_edges: 1,
};

const RELATIONSHIPS = {
  data: [{
    id: "rel-1", type: "one_to_many",
    entity_definition_1_id: "obj-1", entity_definition_2_id: "obj-2",
    entity_1_to_2_predicate: "employs", entity_2_to_1_predicate: "works at",
    created_at: "2025-01-01T00:00:00Z",
  }],
};

const INSIGHTS = {
  insights: [{
    id: "insight-1", title: "Key trend",
    body: "Revenue is increasing across all segments",
    category: "growth", priority: "high", record_ids: ["rec-1"],
    created_at: "2025-01-01T00:00:00Z",
  }],
  insight_count: 1,
};

// ── Helpers ─────────────────────────────────────────────────────────────

async function readBody(req: IncomingMessage): Promise<string> {
  const chunks: Buffer[] = [];
  for await (const chunk of req) chunks.push(chunk as Buffer);
  return Buffer.concat(chunks).toString("utf-8");
}

function json(res: ServerResponse, data: unknown, status = 200): void {
  res.writeHead(status, { "Content-Type": "application/json" });
  res.end(JSON.stringify(data));
}

// ── Server ──────────────────────────────────────────────────────────────

export function startMockServer(): { url: string; close: () => void } {
  const server = createServer(async (req, res) => {
    const url = new URL(req.url!, `http://localhost`);
    const path = url.pathname;
    const method = req.method!;
    const auth = req.headers.authorization;

    // Auth check
    if (path.startsWith("/api/developers") && auth !== "Bearer test-key") {
      return json(res, { error: "Unauthorized", detail: "Invalid API key" }, 401);
    }

    try {
      // ── GET ─────────────────────────────────────────────────
      if (method === "GET") {
        if (path === "/api/developers/v1/objects") {
          return json(res, { data: OBJECTS });
        }

        // GET /v1/objects/:slug
        let m = path.match(/^\/api\/developers\/v1\/objects\/([^/]+)$/);
        if (m) {
          const obj = OBJECTS.find((o) => o.slug === m![1]);
          return obj ? json(res, obj) : json(res, { error: "Not found" }, 404);
        }

        // GET /v1/records/:id/timeline
        m = path.match(/^\/api\/developers\/v1\/records\/([^/]+)\/timeline$/);
        if (m) return json(res, TIMELINE);

        // GET /v1/records/:id
        m = path.match(/^\/api\/developers\/v1\/records\/([^/]+)$/);
        if (m) {
          const rec = RECORDS_BY_ID[m[1]];
          return rec ? json(res, rec) : json(res, { error: "Not found" }, 404);
        }

        if (path === "/api/developers/v1/tasks") return json(res, { data: [TASK], has_more: false, total: 1 });

        // GET /v1/tasks/:id
        m = path.match(/^\/api\/developers\/v1\/tasks\/([^/]+)$/);
        if (m) return m[1] === "task-1" ? json(res, TASK) : json(res, { error: "Not found" }, 404);

        if (path === "/api/developers/v1/notes") return json(res, { data: [NOTE] });

        // GET /v1/notes/:id
        m = path.match(/^\/api\/developers\/v1\/notes\/([^/]+)$/);
        if (m) return m[1] === "note-1" ? json(res, NOTE) : json(res, { error: "Not found" }, 404);

        if (path === "/api/developers/v1/graph") return json(res, GRAPH);
        if (path === "/api/developers/v1/relationships") return json(res, RELATIONSHIPS);
        if (path === "/api/developers/v1/insights") return json(res, INSIGHTS);

        // GET /v1/context/artifacts/:id
        m = path.match(/^\/api\/developers\/v1\/context\/artifacts\/([^/]+)$/);
        if (m) return json(res, ARTIFACT);
      }

      // ── POST ────────────────────────────────────────────────
      if (method === "POST") {
        const body = await readBody(req);
        const parsed = body ? JSON.parse(body) : {};

        // POST /v1/objects/:slug/records (list records)
        let m = path.match(/^\/api\/developers\/v1\/objects\/([^/]+)\/records$/);
        if (m) {
          const records = RECORDS_BY_SLUG[m[1]] ?? [];
          return json(res, { data: records, limit: 25, offset: 0, has_more: false, total: records.length });
        }

        // POST /v1/objects (create object definition)
        if (path === "/api/developers/v1/objects") {
          return json(res, {
            id: "obj-new", name: parsed.name ?? "New", name_plural: (parsed.name ?? "New") + "s",
            slug: parsed.slug ?? "new", type: parsed.type ?? "custom",
            created_at: "2025-01-01T00:00:00Z", attributes: [],
          });
        }

        // POST /v1/objects/:slug (create record)
        m = path.match(/^\/api\/developers\/v1\/objects\/([^/]+)$/);
        if (m) {
          const name = typeof parsed.attributes?.name === "string" ? parsed.attributes.name : "NewRecord";
          return json(res, {
            id: `rec-new-${Date.now()}`, object_id: "obj-1", workspace_id: "ws-1",
            type: m[1],
            attributes: { name: [{ id: "a-new", type: "text", text: name }] },
            created_at: "2025-01-01T00:00:00Z", updated_at: "2025-01-01T00:00:00Z",
          });
        }

        if (path === "/api/developers/v1/search") return json(res, SEARCH);
        if (path === "/api/developers/v1/tasks") {
          return json(res, { ...TASK, id: "task-new", title: parsed.title ?? "" });
        }
        if (path === "/api/developers/v1/notes") {
          return json(res, { ...NOTE, id: "note-new", title: parsed.title ?? "" });
        }
        if (path === "/api/developers/v1/context/ask") return json(res, CONTEXT_ASK);
        if (path === "/api/developers/v1/context/text") return json(res, CONTEXT_TEXT);
        if (path === "/api/developers/v1/relationships") {
          return json(res, { ...RELATIONSHIPS.data[0], id: "rel-new" });
        }

        // POST /v1/records/:id/relationships
        m = path.match(/^\/api\/developers\/v1\/records\/([^/]+)\/relationships$/);
        if (m) return json(res, { id: "rel-inst-new" });
      }

      // ── PUT ─────────────────────────────────────────────────
      if (method === "PUT") {
        const body = await readBody(req);
        const parsed = body ? JSON.parse(body) : {};

        const m = path.match(/^\/api\/developers\/v1\/objects\/([^/]+)$/);
        if (m) {
          const name = typeof parsed.attributes?.name === "string" ? parsed.attributes.name : "Upserted";
          return json(res, {
            id: "rec-upserted", object_id: "obj-1", workspace_id: "ws-1",
            type: m[1],
            attributes: { name: [{ id: "a-up", type: "text", text: name }] },
            created_at: "2025-01-01T00:00:00Z", updated_at: "2025-01-01T00:00:00Z",
          });
        }
      }

      // ── PATCH ───────────────────────────────────────────────
      if (method === "PATCH") {
        const body = await readBody(req);
        const parsed = body ? JSON.parse(body) : {};

        // PATCH /v1/objects/:slug
        let m = path.match(/^\/api\/developers\/v1\/objects\/([^/]+)$/);
        if (m) {
          const obj = OBJECTS.find((o) => o.slug === m![1]);
          return obj
            ? json(res, { ...obj, ...parsed })
            : json(res, { error: "Not found" }, 404);
        }

        // PATCH /v1/records/:id
        m = path.match(/^\/api\/developers\/v1\/records\/([^/]+)$/);
        if (m) {
          const rec = RECORDS_BY_ID[m[1]];
          return rec ? json(res, rec) : json(res, { error: "Not found" }, 404);
        }

        // PATCH /v1/tasks/:id
        m = path.match(/^\/api\/developers\/v1\/tasks\/([^/]+)$/);
        if (m) return json(res, { ...TASK, ...parsed });

        // PATCH /v1/notes/:id
        m = path.match(/^\/api\/developers\/v1\/notes\/([^/]+)$/);
        if (m) return json(res, { ...NOTE, ...parsed });
      }

      // ── DELETE ──────────────────────────────────────────────
      if (method === "DELETE") {
        let m = path.match(/^\/api\/developers\/v1\/records\/([^/]+)$/);
        if (m) return json(res, {});

        m = path.match(/^\/api\/developers\/v1\/objects\/([^/]+)$/);
        if (m) return json(res, {});

        m = path.match(/^\/api\/developers\/v1\/tasks\/([^/]+)$/);
        if (m) return json(res, {});

        m = path.match(/^\/api\/developers\/v1\/notes\/([^/]+)$/);
        if (m) return json(res, {});

        m = path.match(/^\/api\/developers\/v1\/relationships\/([^/]+)$/);
        if (m) return json(res, {});
      }

      json(res, { error: "Not found", detail: `No route for ${method} ${path}` }, 404);
    } catch (err) {
      json(res, { error: "Internal error", detail: String(err) }, 500);
    }
  });

  server.listen(0);
  const addr = server.address() as { port: number };
  return {
    url: `http://localhost:${addr.port}`,
    close: () => server.close(),
  };
}
