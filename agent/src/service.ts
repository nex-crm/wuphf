// Thin HTTP/SSE service the operator FE talks to (no broker). Bun.serve, no
// framework:
//   GET  /health        liveness
//   GET  /providers     which inference paths are available (subscription/BYOK/local)
//   POST /build/stream  description -> the pi-mono agent assembles a WorkflowSpec (SSE)
//   POST /run           execute a compiled spec deterministically (gated step -> CQ1)

import { streamWorkflow } from "./buildAgent.js";
import { runWorkflow } from "./executor.js";
import { providersPayload } from "./providers.js";
import { buildTool } from "./tools.js";
import { type BuildRequest, type RunRequest, SCHEMA_VERSION, type ToolBuildRequest, type WorkflowSpec } from "./wire.js";

type BuildEvent = { type: "step"; step: WorkflowSpec["steps"][number] } | { type: "spec"; spec: WorkflowSpec };
type BuildStream = (message: string, opts: { toolId?: string; signal?: AbortSignal }) => AsyncGenerator<BuildEvent>;

export interface ServerOptions {
	port?: number;
	// Override the build engine in tests so they never hit a live model.
	buildStream?: BuildStream;
}

function json(data: unknown, status = 200): Response {
	return new Response(JSON.stringify(data), { status, headers: { "content-type": "application/json" } });
}

function sse(name: string, data: unknown): string {
	return `event: ${name}\ndata: ${JSON.stringify(data)}\n\n`;
}

function schemaMismatch(v: number | undefined): boolean {
	return v != null && v !== SCHEMA_VERSION;
}

export function createServer(opts: ServerOptions = {}) {
	const buildStream: BuildStream = opts.buildStream ?? (streamWorkflow as BuildStream);

	return Bun.serve({
		port: opts.port ?? Number(process.env.PORT ?? 8820),
		async fetch(req) {
			const url = new URL(req.url);
			const { pathname } = url;

			if (req.method === "GET" && pathname === "/health") return json({ status: "ok", version: "0.0.1" });
			if (req.method === "GET" && pathname === "/providers") return json(providersPayload());

			if (req.method === "POST" && pathname === "/build/stream") {
				// Guard the parse BEFORE the stream starts: an unparseable body must be a
				// plain 400, not a 500 (and never a half-open SSE response).
				let body: BuildRequest;
				try {
					body = (await req.json()) as BuildRequest;
				} catch {
					return json({ error: "invalid JSON body" }, 400);
				}
				// Parseable JSON is not enough: null/[]/"x" cast to BuildRequest then read
				// as undefined. Validate the shape before dereferencing.
				if (!body || typeof body !== "object" || typeof body.message !== "string") {
					return json({ error: "invalid build request: message (string) required" }, 400);
				}
				if (schemaMismatch(body.schema_version)) return json({ error: "schema_version mismatch" }, 400);
				const stream = new ReadableStream({
					async start(controller) {
						const enc = new TextEncoder();
						controller.enqueue(enc.encode(sse("start", { message: body.message })));
						try {
							// Thread the request's abort signal so a dropped client tears the build down.
							for await (const ev of buildStream(body.message, { toolId: body.tool_id, signal: req.signal })) {
								controller.enqueue(enc.encode(sse(ev.type === "spec" ? "spec" : "step", ev)));
							}
						} catch (e) {
							controller.enqueue(enc.encode(sse("error", { message: String(e) })));
						}
						controller.close();
					},
				});
				return new Response(stream, { headers: { "content-type": "text/event-stream", "cache-control": "no-cache" } });
			}

			if (req.method === "POST" && pathname === "/tools/build") {
				// The app's chat teaches a workflow; the agent calls create_tool and
				// returns the tool it made, so the FE renders the call and lists it.
				let body: ToolBuildRequest;
				try {
					body = (await req.json()) as ToolBuildRequest;
				} catch {
					return json({ error: "invalid JSON body" }, 400);
				}
				if (!body || typeof body !== "object" || typeof body.message !== "string") {
					return json({ error: "invalid tool build request: message (string) required" }, 400);
				}
				if (schemaMismatch(body.schema_version)) return json({ error: "schema_version mismatch" }, 400);
				return json(buildTool(body.message));
			}

			if (req.method === "POST" && pathname === "/run") {
				let body: RunRequest;
				try {
					body = (await req.json()) as RunRequest;
				} catch {
					return json({ error: "invalid JSON body" }, 400);
				}
				if (!body || typeof body !== "object" || !body.spec || typeof body.spec !== "object") {
					return json({ error: "invalid run request: spec (object) required" }, 400);
				}
				if (schemaMismatch(body.schema_version)) return json({ error: "schema_version mismatch" }, 400);
				return json(await runWorkflow(body.spec, body.input ?? {}));
			}

			return json({ error: "not found" }, 404);
		},
	});
}

if (import.meta.main) {
	const server = createServer();
	console.log(`wuphf-agent service on ${server.url}`);
}
