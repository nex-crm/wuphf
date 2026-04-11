# MCP Server — Multi-Tenant Isolation Audit (W9)

**Date:** 2026-04-11  
**Branch:** `nazz/feat/managed-agents`  
**Auditor:** W9 automated audit pass  
**SDK version:** `@modelcontextprotocol/sdk` v1.27.1 (node_modules: 1.27.0)

---

## 1. Isolation model

### How workspace isolation is enforced

Isolation is **per-API-key at the application layer**, not per-session at the MCP transport layer.

The critical path is:

```
Managed Agents session (workspace A)
  → injects vault API key into MCP HTTP request Authorization header
  → createServer("key-A") called at process start (stdio) OR per-deploy (HTTP)
  → NexApiClient("key-A") constructed and bound to all tool handlers
  → every tool handler calls client.request() which sets Authorization: Bearer key-A
  → Nex API backend enforces workspace isolation via this key
```

Key code: `dist/server.js`
```js
export function createServer(apiKey) {
  const server = new McpServer({ name: "wuphf", version: "0.1.0" });
  const client = new NexApiClient(apiKey);   // <-- key bound here, once
  // all 17 tool modules receive this same client instance
  registerContextTools(server, client);
  registerSearchTools(server, client);
  // ... 15 more tool registrations ...
  return server;
}
```

Key code: `dist/client.js`
```js
async request(method, path, body) {
  this.requireAuth();
  const headers = { Authorization: `Bearer ${this.apiKey}` };
  // ... fetch call using this header on every request
}
```

The `NexApiClient` instance holds exactly one `apiKey`. There is no shared singleton, no global state, no session store shared between tool calls. The backend treats each API key as scoped to a single workspace.

---

## 2. Concurrent session behaviour

### stdio mode (default for Claude Desktop / local Managed Agents)

In stdio mode, a new OS process is spawned per MCP connection. Full OS-level isolation. The API key is loaded once at process start via `loadApiKey()` (env var `WUPHF_API_KEY` or `~/.wuphf/config.json`). Two parallel sessions run in two separate processes with separate memory spaces.

**Isolation verdict: STRONG — OS-level process isolation.**

### HTTP mode (`MCP_TRANSPORT=http`)

In HTTP mode, a single Node.js process serves all MCP requests on `/mcp`. The current `index.js` implementation loads the API key **once at process start** and passes it to a single `createServer()` call:

```js
const apiKey = loadApiKey();          // loaded once from env/config
const server = createServer(apiKey);  // single NexApiClient instance
await server.connect(httpTransport);
```

The `StreamableHTTPServerTransport` is configured as stateless (`sessionIdGenerator: undefined`), meaning no server-side session state is maintained between requests.

**Implication for Managed Agents (HTTP mode):**
- If the server is deployed as a single shared HTTP process, all HTTP requests use the same `NexApiClient` and therefore the same API key.
- This is safe only when each Managed Agents deployment is its own isolated container/process, each started with its own `WUPHF_API_KEY` env var. This is the expected Anthropic Managed Agents deployment pattern (vault-injected credentials at container creation time, not per-request header injection).
- If two workspaces were to share a single HTTP process (not the intended deployment model), they would share credentials — this would be a security violation.

**Isolation verdict for HTTP mode: SAFE under per-process deployment; unsafe under shared-process deployment.**

---

## 3. OAuth / credential injection model (AC-W9-2)

Managed Agents injects credentials per-session via **Anthropic's vault** at `CreateSession` time (W0 contract). The flow is:

1. At session creation, Anthropic's platform reads the workspace's stored API key from the vault.
2. The key is injected as `WUPHF_API_KEY` into the MCP server's process environment (or as an HTTP header if using remote HTTP transport).
3. `loadApiKey()` in `config.js` reads `process.env.WUPHF_API_KEY` first, making vault injection the highest-priority credential source.
4. The `NexApiClient` is constructed with this key, binding all tool calls to that workspace.

No OAuth flow is involved. The credential type is a Nex developer API key (`sk-...`), which maps 1:1 to a workspace.

---

## 4. What is NOT isolated at the MCP layer

| Concern | Status |
|---|---|
| Tool registration | Shared per McpServer instance — not a security boundary (same tools, different data) |
| `SessionStore` (`~/.wuphf/mcp-sessions.json`) | File on disk, scoped to the OS user running the server. In stdio (per-process) mode, each process has its own home dir mount. In shared HTTP mode, multiple workspaces could collide on this file — but `session-store.js` is not currently used in the active HTTP transport path |
| `skill-sync.ts` local file writes | Writes to `.nex/skills/` relative to `process.cwd()`. Safe when each deployment has its own working directory |
| Rate limiter (`rate-limiter.js`) | Per-process in-memory — no cross-workspace leakage |
| `~/.wuphf/config.json` | Loaded at startup only; not written during request handling in Managed Agents flows |

---

## 5. W8 finding: no mcp-tools version bump needed (AC-W9-3)

W8 audited the compiled tool implementations and confirmed that all required tools exist in `dist/tools/` as compiled-only files. W8 did NOT publish a new npm package version — the tools ship as pre-compiled `dist/*.js` files alongside the TypeScript source. There is no `mcp-tools` npm package to version-bump. This is noted explicitly to close AC-W9-3.

---

## 6. Test results (AC-W9-4 / AC-W9-5)

```
bun test v1.3.9 (cf6cdbbb)

 8 pass
 0 fail
 24 expect() calls
Ran 8 tests across 1 file. [92.00ms]
```

Test file: `mcp/src/isolation.test.ts`

Tests cover:
- Two server instances with different API keys are independent objects (not singletons)
- `NexApiClient` stores and isolates the key it was constructed with
- Unauthenticated states (no key, empty string)
- `setApiKey()` post-construction (supports vault-key injection patterns)
- Server instantiation in registration-only mode (no key)
- Multiple sequential instantiations do not interfere

---

## 7. Recommendations

1. **Enforce per-process HTTP deployments.** Document (or enforce via health check) that the HTTP-mode MCP server must never be shared across workspaces. One container = one workspace = one `WUPHF_API_KEY`.

2. **Consider per-request key injection for HTTP mode.** If multi-tenant HTTP hosting ever becomes a requirement, refactor `index.js` (HTTP branch) to call `createServer(extractKeyFromAuthHeader(req))` inside the request handler, not at process start. This would make the isolation model consistent between stdio and HTTP modes.

3. **Add `SessionStore` path isolation.** In containerized deployments, mount `/root/.wuphf/` as an ephemeral volume per-container to prevent any possibility of cross-session session ID leakage.

4. **No immediate action required.** For the current Managed Agents deployment model (vault-injected key per container), the existing isolation is correct and safe.
