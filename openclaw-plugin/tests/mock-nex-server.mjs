/**
 * Mock WUPHF Developer API server for plugin testing.
 * Responds to /ask and /text with realistic payloads.
 * Run: node mock-wuphf-server.mjs
 */

import http from "node:http";

const PORT = 31999;
const VALID_KEY = "sk-test-mock-key";

/** In-memory store of ingested content. */
const memories = [];
let artifactCounter = 1000;
let sessionCounter = 1;

function json(res, status, body) {
  res.writeHead(status, { "Content-Type": "application/json" });
  res.end(JSON.stringify(body));
}

function readBody(req) {
  return new Promise((resolve) => {
    const chunks = [];
    req.on("data", (c) => chunks.push(c));
    req.on("end", () => {
      try { resolve(JSON.parse(Buffer.concat(chunks).toString())); }
      catch { resolve(null); }
    });
  });
}

const server = http.createServer(async (req, res) => {
  // Auth check
  const auth = req.headers.authorization;
  if (!auth || auth !== `Bearer ${VALID_KEY}`) {
    return json(res, 401, { error: "unauthorized" });
  }

  const url = new URL(req.url, `http://localhost:${PORT}`);
  const path = url.pathname;

  // POST /api/developers/v1/context/text — ingest
  if (req.method === "POST" && path === "/api/developers/v1/context/text") {
    const body = await readBody(req);
    if (!body?.content) {
      return json(res, 400, { error: "content required" });
    }
    const id = ++artifactCounter;
    memories.push({ id, content: body.content, context: body.context, ts: Date.now() });
    console.log(`[ingest] artifact_id=${id} content="${body.content.slice(0, 80)}..." (${memories.length} total)`);
    return json(res, 200, { artifact_id: id });
  }

  // POST /api/developers/v1/context/ask — recall
  if (req.method === "POST" && path === "/api/developers/v1/context/ask") {
    const body = await readBody(req);
    if (!body?.query) {
      return json(res, 400, { error: "query required" });
    }

    const query = body.query.toLowerCase();
    console.log(`[recall] query="${body.query}" session_id=${body.session_id || "new"}`);

    // Search memories for matches
    const matches = memories.filter((m) =>
      m.content.toLowerCase().includes(query) ||
      query.split(" ").some((w) => w.length > 3 && m.content.toLowerCase().includes(w))
    );

    const sessionId = body.session_id || `session-${++sessionCounter}`;
    const entityRefs = [];

    let answer;
    if (matches.length > 0) {
      const evidence = matches.map((m) => m.content).join("\n\n");
      answer = `Based on what I know: ${evidence}`;
      // Extract simple entity references from matched content
      const namePattern = /(?:^|\s)([A-Z][a-z]+ [A-Z][a-z]+)/g;
      for (const m of matches) {
        let match;
        while ((match = namePattern.exec(m.content)) !== null) {
          entityRefs.push({ name: match[1].trim(), type: "person", count: 1 });
        }
      }
    } else {
      answer = `I don't have specific information about "${body.query}" in the knowledge base yet.`;
    }

    console.log(`[recall] -> ${matches.length} matches, ${entityRefs.length} entities`);

    return json(res, 200, {
      answer,
      session_id: sessionId,
      entity_references: entityRefs,
    });
  }

  json(res, 404, { error: "not found" });
});

server.listen(PORT, () => {
  console.log(`Mock WUPHF API running on http://localhost:${PORT}`);
  console.log(`Auth key: ${VALID_KEY}`);
  console.log(`Endpoints: POST /api/developers/v1/context/text, POST /api/developers/v1/context/ask`);
  console.log(`Memories stored: ${memories.length}`);
  console.log("---");
});
